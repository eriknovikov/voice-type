package voicetype

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalTranscriptionFailureNeverCallsCloudEndpoint(t *testing.T) {
	setupEngineTest(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":"unexpected cloud request"}`))
	}))
	defer server.Close()

	manager := newLocalModelManagerAt(t.TempDir())
	client := newSTTClientWithModels(sttSettings{
		Provider:      providerLocal,
		Model:         localModelUnifiedEnglish,
		StreamingMode: streamingModeNone,
	}, manager)
	// Keep an observable cloud endpoint attached to prove that the local path
	// never uses the cloud request machinery after a missing-model failure.
	client.endpoint = server.URL
	client.http = server.Client()
	_, err := client.TranscribeContext(context.Background(), WavEncode([]int16{1, 2, 3}))
	if err == nil {
		t.Fatal("local transcription unexpectedly succeeded without an installed model")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("local failure issued %d cloud requests", got)
	}
	if _, err := client.buildRequest(context.Background(), WavEncode([]int16{1, 2, 3})); err == nil {
		t.Fatal("local provider can construct a cloud request")
	}
}

func TestLocalTranscriptionErrorReportsRetainedAudioPath(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	engine := &engine{}
	wav := WavEncode([]int16{1, -2, 3})
	localErr := errors.New("stream transcript disagreed")
	got := engine.handleSTTError(localErr, wav, true)
	if !errors.Is(got, localErr) || !strings.Contains(got.Error(), "audio retained at ") {
		t.Fatalf("local failure omitted the retained audio location: %v", got)
	}
	location := strings.TrimSuffix(strings.SplitN(got.Error(), "audio retained at ", 2)[1], ")")
	data, err := os.ReadFile(location)
	if err != nil || string(data) != string(wav) {
		t.Fatalf("retained WAV is unavailable or changed: %q %v", location, err)
	}
	info, err := os.Stat(location)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("retained WAV permissions are not private: info=%v err=%v", info, err)
	}

	cloudErr := errors.New("cloud failure")
	if got := engine.handleSTTError(cloudErr, wav, false); got != cloudErr {
		t.Fatalf("cloud failure message changed: %v", got)
	}
}

func TestLiveStreamFailureRetainsRecordedWAVThroughEngine(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{
		Revision:      settings.Revision,
		Name:          "Local live",
		Provider:      providerLocal,
		ModelID:       localModelUnifiedEnglish,
		StreamingMode: streamingModeLive,
	}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	samples := []int16{17, -29, 300, -400}
	service.recorder = &failureRecorder{samples: samples}
	streamErr := errors.New("final transcript disagreed with committed text")
	stream := newLocalStreamSession(context.Background(), streamingModeLive, &fakeLocalStream{
		feedSnapshots: []fakeStreamSnapshot{{err: streamErr}},
	}, nil)
	stream.OfferSamples(samples)
	select {
	case <-stream.done:
	case <-time.After(time.Second):
		t.Fatal("failed live stream did not stop")
	}
	service.mu.Lock()
	service.state = stateRecording
	service.sessionCtx = context.Background()
	service.activeStream = stream
	service.mu.Unlock()

	operationErr := service.stopSessionMatching(0, false)
	if !errors.Is(operationErr, streamErr) || !strings.Contains(operationErr.Error(), "audio retained at ") {
		t.Fatalf("live streaming failure was not reported with retained audio: %v", operationErr)
	}
	location := strings.TrimSuffix(strings.SplitN(operationErr.Error(), "audio retained at ", 2)[1], ")")
	data, err := os.ReadFile(location)
	if err != nil || !bytes.Equal(data, WavEncode(samples)) {
		t.Fatalf("recorded audio was not retained intact: %v", err)
	}
	info, err := os.Stat(location)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("retained WAV is not private: info=%v err=%v", info, err)
	}
	if service.Status().State != "idle" || !service.Status().LastAttemptFailed {
		t.Fatalf("streaming failure was not visible after recovery: %+v", service.Status())
	}
}

func TestLocalFinalInsertionFailureRetainsAudioAndTranscript(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	view := readSettingsTest(t)
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Name: "Local", Provider: providerLocal, ModelID: localModelUnifiedEnglish, StreamingMode: streamingModeProcess}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	oldFactory := textInserterBackends[InsertBackendIBus]
	textInserterBackends[InsertBackendIBus] = func(context.Context) (TextInserter, error) {
		return &recordingInserter{err: errors.New("test insertion failure")}, nil
	}
	defer func() { textInserterBackends[InsertBackendIBus] = oldFactory }()
	service.recorder = &fixtureRecorder{}
	stream := newLocalStreamSession(context.Background(), streamingModeProcess, &fakeLocalStream{finalSnapshot: fakeStreamSnapshot{full: "retained transcript", committed: "retained transcript"}}, nil)
	service.mu.Lock()
	service.state = stateRecording
	service.sessionCtx = context.Background()
	service.activeStream = stream
	service.mu.Unlock()
	operationErr := service.stopSessionMatching(0, false)
	if operationErr == nil || !strings.Contains(operationErr.Error(), "transcript retained at ") {
		t.Fatalf("lost transcript: %v", operationErr)
	}
	path := strings.SplitN(operationErr.Error(), "transcript retained at ", 2)[1]
	text, err := os.ReadFile(path)
	if err != nil || string(text) != "retained transcript" {
		t.Fatalf("private transcript not retained: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("transcript permissions not private")
	}
}

func TestLocalInternalCancellationIsNotMistakenForAppShutdown(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	view := readSettingsTest(t)
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Name: "Local", Provider: providerLocal, ModelID: localModelUnifiedEnglish, StreamingMode: streamingModeLive}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &fixtureRecorder{}
	stream := newLocalStreamSession(context.Background(), streamingModeLive, &fakeLocalStream{finalSnapshot: fakeStreamSnapshot{err: context.Canceled}}, nil)
	service.mu.Lock()
	service.state = stateRecording
	service.sessionCtx = context.Background()
	service.activeStream = stream
	service.mu.Unlock()
	operationErr := service.stopSessionMatching(0, false)
	if operationErr == nil || !strings.Contains(operationErr.Error(), "audio retained at ") || !service.Status().LastAttemptFailed {
		t.Fatalf("internal stream cancellation silently discarded speech: %v", operationErr)
	}
}

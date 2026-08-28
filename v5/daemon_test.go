package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These cover the guard clauses, which run before any audio hardware is
// touched, so the state machine stays testable without a microphone.

type fakeRecorder struct {
	startEntered chan struct{}
	startRelease chan struct{}
	startCalls   atomic.Int32
	stopCalls    atomic.Int32
	samples      []int16
	stopErr      error
}

func (r *fakeRecorder) Start() error {
	r.startCalls.Add(1)
	if r.startEntered != nil {
		close(r.startEntered)
	}
	if r.startRelease != nil {
		<-r.startRelease
	}
	return nil
}

func (r *fakeRecorder) Stop() ([]int16, error) {
	r.stopCalls.Add(1)
	return r.samples, r.stopErr
}

func (r *fakeRecorder) Close() {}

func assumeOnline(d *daemon) {
	d.checkOnline = func(context.Context) error { return nil }
}

func TestStartRejectsWhenBusy(t *testing.T) {
	for _, state := range []sessionState{stateChecking, stateRecording, stateTranscribing} {
		d := &daemon{cfg: configDefaults(), state: state}
		status, msg := d.startSession()
		if status != http.StatusConflict {
			t.Errorf("state %v: status = %d, want 409", state, status)
		}
		if msg == "" {
			t.Errorf("state %v: expected a message", state)
		}
		if d.currentState() != state {
			t.Errorf("state %v: a rejected start must not change state", state)
		}
	}
}

func TestOfflineStartTypesMarkerWithoutOpeningRecorder(t *testing.T) {
	rec := &fakeRecorder{}
	kb := newFakeKeyboard()
	d := newDaemon(configDefaults(), &resources{Recorder: rec, Typer: &Typer{kb: kb}})
	d.checkOnline = func(context.Context) error { return errors.New("network unreachable") }

	status, message := d.startSession()
	if status != http.StatusServiceUnavailable || message != "offline" {
		t.Fatalf("start = %d/%q, want 503/offline", status, message)
	}
	if rec.startCalls.Load() != 0 || rec.stopCalls.Load() != 0 {
		t.Fatalf("offline start touched recorder: starts=%d stops=%d", rec.startCalls.Load(), rec.stopCalls.Load())
	}
	question := typeKeyMap['?']
	want := []testKeyEvent{
		{op: "down", key: question.modifiers[0]},
		{op: "down", key: question.key},
		{op: "up", key: question.modifiers[0]},
		{op: "up", key: question.key},
	}
	if len(kb.events) != len(want) {
		t.Fatalf("offline marker events = %#v, want %#v", kb.events, want)
	}
	for i := range want {
		if kb.events[i] != want[i] {
			t.Fatalf("offline marker events = %#v, want %#v", kb.events, want)
		}
	}
	if state := d.currentState(); state != stateIdle {
		t.Errorf("state = %s, want idle", state)
	}
}

func TestShutdownCancelsConnectivityCheckWithoutTypingMarker(t *testing.T) {
	entered := make(chan struct{})
	rec := &fakeRecorder{}
	kb := newFakeKeyboard()
	d := newDaemon(configDefaults(), &resources{Recorder: rec, Typer: &Typer{kb: kb}})
	d.checkOnline = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}

	startDone := make(chan int, 1)
	go func() {
		status, _ := d.startSession()
		startDone <- status
	}()
	<-entered
	d.shutdown()

	if status := <-startDone; status != http.StatusConflict {
		t.Errorf("cancelled start status = %d, want 409", status)
	}
	if rec.startCalls.Load() != 0 {
		t.Errorf("cancelled connectivity check opened recorder %d times", rec.startCalls.Load())
	}
	if len(kb.events) != 0 {
		t.Errorf("shutdown typed offline marker: %#v", kb.events)
	}
}

func TestStopIsNoOpUnlessRecording(t *testing.T) {
	for _, state := range []sessionState{stateIdle, stateTranscribing} {
		d := &daemon{cfg: configDefaults(), state: state}
		status, _ := d.stopSession()
		if status != http.StatusOK {
			t.Errorf("state %v: status = %d, want 200", state, status)
		}
		if d.currentState() != state {
			t.Errorf("state %v: a no-op stop must not change state", state)
		}
	}
}

func TestStopCancelsRecorderStart(t *testing.T) {
	rec := &fakeRecorder{
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})
	assumeOnline(d)

	startDone := make(chan int, 1)
	go func() {
		status, _ := d.startSession()
		startDone <- status
	}()
	<-rec.startEntered

	stopDone := make(chan int, 1)
	go func() {
		status, _ := d.stopSession()
		stopDone <- status
	}()

	if status := <-stopDone; status != http.StatusOK {
		t.Fatalf("stop status = %d, want 200", status)
	}
	close(rec.startRelease)
	if status := <-startDone; status != http.StatusConflict {
		t.Fatalf("cancelled start status = %d, want 409", status)
	}
	if calls := rec.stopCalls.Load(); calls != 1 {
		t.Errorf("recorder stops = %d, want 1", calls)
	}
	if state := d.currentState(); state != stateIdle {
		t.Errorf("state = %s, want idle", state)
	}
}

func TestShutdownCanCancelBlockedRecorderStart(t *testing.T) {
	rec := &fakeRecorder{
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})
	assumeOnline(d)

	startDone := make(chan int, 1)
	go func() {
		status, _ := d.startSession()
		startDone <- status
	}()
	<-rec.startEntered

	shutdownDone := make(chan struct{})
	go func() {
		d.shutdown()
		close(shutdownDone)
	}()

	deadline := time.Now().Add(time.Second)
	for d.currentState() != stateIdle && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if state := d.currentState(); state != stateIdle {
		t.Fatalf("state while recorder start blocked = %s, want idle", state)
	}
	close(rec.startRelease)

	if status := <-startDone; status != http.StatusConflict {
		t.Errorf("start after shutdown = %d, want 409", status)
	}
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown remained blocked after recorder start returned")
	}
	if rec.stopCalls.Load() != 1 {
		t.Errorf("cancelled recorder start stopped %d times, want 1", rec.stopCalls.Load())
	}
}

func TestHealthReportsState(t *testing.T) {
	d := &daemon{cfg: configDefaults(), state: stateRecording}

	rec := httptest.NewRecorder()
	d.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
	if body["state"] != "recording" {
		t.Errorf("state = %v, want recording", body["state"])
	}
}

// /toggle while recording must not be routed to start.
func TestToggleWhileRecordingDoesNotStart(t *testing.T) {
	d := &daemon{cfg: configDefaults(), state: stateTranscribing}

	rec := httptest.NewRecorder()
	d.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/toggle", nil))

	// transcribing is not recording, so toggle tries to start and is refused
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestExitShutsDown(t *testing.T) {
	d := newDaemon(configDefaults(), &resources{})

	rec := httptest.NewRecorder()
	d.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/exit", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	select {
	case <-d.done:
	default:
		t.Error("/exit did not signal shutdown")
	}

	// A second /exit must not panic on a closed channel.
	d.shutdown()
}

func TestShutdownStopsRecordingAndRejectsRestart(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"text": "must not appear"})
	}))
	defer srv.Close()

	rec := &fakeRecorder{samples: speech(1, 5000)}
	kb := newFakeKeyboard()
	d := newDaemon(configDefaults(), &resources{Recorder: rec, Typer: &Typer{kb: kb}})
	d.stt.endpoint = srv.URL
	assumeOnline(d)
	if status, _ := d.startSession(); status != http.StatusOK {
		t.Fatalf("start status = %d, want 200", status)
	}

	d.shutdown()

	if calls := rec.stopCalls.Load(); calls != 1 {
		t.Errorf("recorder stops = %d, want 1", calls)
	}
	if state := d.currentState(); state != stateIdle {
		t.Errorf("state = %s, want idle", state)
	}
	if status, _ := d.startSession(); status != http.StatusConflict {
		t.Errorf("start during shutdown = %d, want 409", status)
	}
	if requests.Load() != 0 {
		t.Errorf("shutdown sent %d transcription requests, want 0", requests.Load())
	}
	if len(kb.events) != 0 {
		t.Errorf("shutdown produced keyboard events: %#v", kb.events)
	}
}

func TestExpiredTimerDoesNotStopNewSession(t *testing.T) {
	rec := &fakeRecorder{}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})
	d.state = stateRecording
	d.sessionID = 2
	d.sessionCtx = context.Background()

	status, _ := d.stopSessionFor(1)
	if status != http.StatusOK {
		t.Fatalf("stale stop status = %d, want 200", status)
	}
	if calls := rec.stopCalls.Load(); calls != 0 {
		t.Errorf("stale timer stopped recorder %d times", calls)
	}
	if state := d.currentState(); state != stateRecording {
		t.Errorf("state = %s, want recording", state)
	}

	d.stopSession()
}

func TestCaptureErrorIsNotReportedAsSilence(t *testing.T) {
	rec := &fakeRecorder{samples: speech(1, 5000), stopErr: errors.New("connection closed")}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})
	d.state = stateRecording
	d.sessionCtx = context.Background()

	status, message := d.stopSession()
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	if !strings.Contains(message, "connection closed") {
		t.Errorf("message = %q, want capture error", message)
	}
}

func TestShutdownWaitsForTranscription(t *testing.T) {
	d := newDaemon(configDefaults(), &resources{})
	d.state = stateTranscribing
	d.sessionWG.Add(1)

	shutdownDone := make(chan struct{})
	go func() {
		d.shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned while a session was in flight")
	case <-time.After(20 * time.Millisecond):
	}

	d.mu.Lock()
	d.state = stateIdle
	d.mu.Unlock()
	d.sessionWG.Done()
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after the session completed")
	}
}

func TestShutdownCancelsTranscriptionAndSkipsOutput(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-releaseRequest
	}))
	defer srv.Close()

	rec := &fakeRecorder{samples: concat(silence(0.1, 0), speech(0.6, 5000))}
	kb := newFakeKeyboard()
	d := newDaemon(configDefaults(), &resources{Recorder: rec, Typer: &Typer{kb: kb}})
	d.stt.endpoint = srv.URL
	assumeOnline(d)
	if status, message := d.startSession(); status != http.StatusOK {
		t.Fatalf("start = %d/%q", status, message)
	}

	type stopResult struct {
		status  int
		message string
	}
	stopDone := make(chan stopResult, 1)
	go func() {
		status, message := d.stopSession()
		stopDone <- stopResult{status: status, message: message}
	}()
	<-requestStarted

	started := time.Now()
	d.shutdown()
	close(releaseRequest)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("shutdown cancellation took %v", elapsed)
	}
	result := <-stopDone
	if result.status != http.StatusOK || result.message != "cancelled" {
		t.Errorf("stop after shutdown = %d/%q, want 200/cancelled", result.status, result.message)
	}
	if len(kb.events) != 0 {
		t.Errorf("cancelled transcription reached keyboard: %#v", kb.events)
	}
}

func TestLongRoomToneSkipsTranscription(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"text": "hallucination"})
	}))
	defer srv.Close()

	kb := newFakeKeyboard()
	d := newDaemon(configDefaults(), &resources{
		Recorder: &fakeRecorder{samples: silence(30, 300)},
		Typer:    &Typer{kb: kb},
	})
	d.stt.endpoint = srv.URL
	d.state = stateRecording
	d.sessionCtx = context.Background()

	status, message := d.stopSession()
	if status != http.StatusOK || message != "no speech detected" {
		t.Fatalf("stop = %d/%q, want 200/no speech detected", status, message)
	}
	if requests.Load() != 0 {
		t.Errorf("room tone sent %d transcription requests", requests.Load())
	}
	if len(kb.events) != 0 {
		t.Errorf("room tone produced keyboard output: %#v", kb.events)
	}
}

func TestSTTErrorsRetainAudio(t *testing.T) {
	cases := map[string]error{
		"unauthorized": &sttError{Status: http.StatusUnauthorized, Message: "bad key"},
		"forbidden":    &sttError{Status: http.StatusForbidden, Message: "forbidden"},
		"too large":    &sttError{Status: http.StatusRequestEntityTooLarge, Message: "large"},
		"bad request":  &sttError{Status: http.StatusBadRequest, Message: "bad"},
		"retryable":    &sttError{Status: http.StatusServiceUnavailable, Message: "busy"},
		"transport":    errors.New("connection reset"),
	}
	wav := []byte("irreplaceable audio")

	for name, sttErr := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			d := &daemon{}
			d.handleSTTError(sttErr, wav)

			matches, err := filepath.Glob(filepath.Join(os.TempDir(), "voice-type", "*.wav"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 1 {
				t.Fatalf("retained files = %d, want 1", len(matches))
			}
			got, err := os.ReadFile(matches[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(wav) {
				t.Errorf("retained audio = %q, want %q", got, wav)
			}
			info, err := os.Stat(matches[0])
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("retained mode = %o, want 600", perm)
			}
		})
	}
}

func TestRetainedAudioNamesDoNotCollide(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	for range 2 {
		if _, err := retainWAV([]byte("audio")); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "voice-type", "*.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("retained files = %d, want 2", len(matches))
	}
}

// A hand edit has to reach the next dictation without a restart. That is the
// reload at the top of startSession.
func TestDaemonReloadsConfigBetweenDictations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("OPENROUTER_API_KEY", "")
	path := filepath.Join(home, "voice-type.jsonc")
	if err := os.WriteFile(path, []byte(`{
    "api_key": "first",
    "port": 3232,
    "model": "parakeet-v3"
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	d := newDaemon(configLoad(), &resources{Recorder: &fakeRecorder{}})
	assumeOnline(d)
	beforeModel, beforeHTTP := d.stt.model.ID, d.stt.http
	if beforeModel != "parakeet-v3" || len(d.stt.vocabulary) != 0 {
		t.Fatalf("started with model=%s vocabulary=%q", beforeModel, d.stt.vocabulary)
	}

	if err := os.WriteFile(path, []byte(`{
    "api_key": "second",
    "port": 9999,
    "model": "gpt-4o-transcribe",
    "vocabulary": ["Numbero"]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if status, message := d.startSession(); status != http.StatusOK {
		t.Fatalf("start after hand edit = %d/%q", status, message)
	}

	after := d.stt
	if after.model.ID != defaultModelID || after.apiKey != "second" || len(after.vocabulary) != 1 {
		t.Errorf("after reload: model=%s key=%s vocabulary=%q", after.model.ID, after.apiKey, after.vocabulary)
	}
	// The listener is already bound, so a port change waits for a restart.
	if d.cfg.Port != 3232 {
		t.Errorf("port = %d, want the bound 3232", d.cfg.Port)
	}
	// Reusing the transport keeps the warm TLS connection to OpenRouter, which
	// is worth more than the handful of bytes a fresh client would cost.
	if after.http != beforeHTTP {
		t.Error("reload replaced the HTTP client, dropping the connection pool")
	}

	health := httptest.NewRecorder()
	d.routes().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body map[string]any
	if err := json.Unmarshal(health.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["state"] != "recording" || body["model"] != defaultModelID || body["vocabulary"] != float64(1) {
		t.Errorf("health after reload = %#v", body)
	}
	d.stopSession()
}

func TestVocabularyEchoRetainsAudioAndSkipsOutput(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	vocabulary := []string{"Numbero", "Erik Novikov"}
	prompt := sttVocabularyPrompt(testSpec(defaultModelID), vocabulary)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"text": prompt})
	}))
	defer srv.Close()

	kb := newFakeKeyboard()
	samples := concat(speech(0.5, 2500), speech(2, 5000))
	cfg := configDefaults()
	cfg.Vocabulary = vocabulary
	d := newDaemon(cfg, &resources{
		Recorder: &fakeRecorder{samples: samples},
		Typer:    &Typer{kb: kb},
	})
	d.stt.endpoint = srv.URL
	d.state = stateRecording
	d.sessionCtx = context.Background()

	status, message := d.stopSession()
	if status != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; message=%q", status, http.StatusBadGateway, message)
	}
	if !strings.Contains(message, "vocabulary prompt") || !strings.Contains(message, "audio retained at") {
		t.Errorf("message = %q", message)
	}
	if len(kb.events) != 0 {
		t.Fatalf("echo reached keyboard: %#v", kb.events)
	}
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "voice-type", "*.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("retained WAVs = %d, want 1", len(matches))
	}
	wav, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(wav) < 4 || string(wav[:4]) != "RIFF" {
		t.Errorf("retained file is not a WAV")
	}
}

func TestVocabularyInLegitimateTranscriptIsOutput(t *testing.T) {
	// No clipboard tools in PATH forces the production fallback to direct typing.
	t.Setenv("PATH", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"text": "Numbero is spelled correctly."})
	}))
	defer srv.Close()

	kb := newFakeKeyboard()
	cfg := configDefaults()
	cfg.Vocabulary = []string{"Numbero"}
	d := newDaemon(cfg, &resources{
		Recorder: &fakeRecorder{samples: concat(speech(0.5, 2500), speech(2, 5000))},
		Typer:    &Typer{kb: kb},
	})
	d.stt.endpoint = srv.URL
	d.state = stateRecording
	d.sessionCtx = context.Background()

	status, message := d.stopSession()
	if status != http.StatusOK || message != "Numbero is spelled correctly." {
		t.Fatalf("status/message = %d/%q", status, message)
	}
	if len(kb.events) == 0 {
		t.Fatal("legitimate transcript was not output")
	}
}

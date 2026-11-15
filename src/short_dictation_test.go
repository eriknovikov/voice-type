package voicetype

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type delayedShortRecorder struct {
	streamingFixtureRecorder
	entered  chan struct{}
	release  chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

func (r *delayedShortRecorder) Start() error {
	if err := r.streamingFixtureRecorder.Start(); err != nil {
		return err
	}
	close(r.entered)
	<-r.release
	return nil
}
func (r *delayedShortRecorder) Stop() ([]int16, error) {
	samples, err := r.streamingFixtureRecorder.Stop()
	r.stopOnce.Do(func() { close(r.stopped) })
	return samples, err
}

// Delay the HTTP Upgrade itself, not only session.updated. Also hold the
// microphone Start return after it has begun delivering audio. A quick Stop
// in either phase must preserve the same session until its final transcript.
func TestQuickWordStopDuringStartupBeforeWebSocketOpens(t *testing.T) {
	setupEngineTest(t)
	view := readSettingsTest(t)
	if err := SaveAPIKey(providerOpenAI, "test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: providerOpenAI, Name: "Streaming"}); err != nil {
		t.Fatal(err)
	}
	dialEntered, allowSocket := make(chan struct{}), make(chan struct{})
	var dialOnce, socketOnce sync.Once
	captured := make(chan []int16, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dialOnce.Do(func() { close(dialEntered) })
		select {
		case <-allowSocket:
		case <-r.Context().Done():
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := readTestJSON(ctx, conn); err != nil {
			return
		}
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		var received []int16
		committed := false
		for {
			event, err := readTestJSON(ctx, conn)
			if err != nil {
				if !committed {
					t.Error("WebSocket closed before transcription completed")
				}
				return
			}
			switch event["type"] {
			case "input_audio_buffer.append":
				if committed {
					t.Error("audio after commit")
				}
				pcm, err := base64.StdEncoding.DecodeString(event["audio"].(string))
				if err != nil {
					t.Error(err)
					return
				}
				for i := 0; i < len(pcm); i += 2 {
					received = append(received, int16(binary.LittleEndian.Uint16(pcm[i:])))
				}
			case "input_audio_buffer.commit":
				if committed {
					t.Error("duplicate commit")
				}
				committed = true
				captured <- received
				_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "short"})
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "short", "transcript": "Yes."})
			}
		}
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = redirectedRealtimeTransport{target: server.URL, original: original}
	defer func() { http.DefaultTransport = original }()
	inserter := &recordingInserter{}
	previous := textInserterBackends[InsertBackendIBus]
	textInserterBackends[InsertBackendIBus] = func(context.Context) (TextInserter, error) { return inserter, nil }
	defer func() { textInserterBackends[InsertBackendIBus] = previous }()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	samples := make([]int16, 6400) // 400 ms, one-word interaction
	for i := range samples {
		samples[i] = int16(i*31 - 16000)
	}
	recorder := &delayedShortRecorder{streamingFixtureRecorder: streamingFixtureRecorder{initial: samples}, entered: make(chan struct{}), release: make(chan struct{}), stopped: make(chan struct{})}
	var microphoneOnce sync.Once
	defer func() {
		microphoneOnce.Do(func() { close(recorder.release) })
		socketOnce.Do(func() { close(allowSocket) })
	}()
	service.recorder = recorder
	started := service.ToggleAsync()
	select {
	case <-recorder.entered:
	case <-time.After(time.Second):
		t.Fatal("microphone did not start")
	}
	select {
	case <-dialEntered:
	case <-time.After(time.Second):
		t.Fatal("connection did not start")
	}
	if err := <-service.ToggleAsync(); err != nil {
		t.Fatal("quick Stop rejected:", err)
	}
	microphoneOnce.Do(func() { close(recorder.release) })
	select {
	case <-recorder.stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop cancelled startup instead of freezing the captured word")
	}
	if service.Status().State != "transcribing" {
		t.Fatal("pending connection was not allowed to finish after Stop")
	}
	service.mu.Lock()
	cancelled := service.sessionCtx == nil || service.sessionCtx.Err() != nil
	service.mu.Unlock()
	if cancelled {
		t.Fatal("Stop cancelled the pending connection")
	}
	socketOnce.Do(func() { close(allowSocket) })
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("quick take did not finish")
	}
	var resampler pcmResampler
	want := append(resampler.Process(samples), resampler.Flush()...)
	select {
	case got := <-captured:
		if !reflect.DeepEqual(got, want) {
			t.Fatal("quick take lost or duplicated audio")
		}
	case <-time.After(time.Second):
		t.Fatal("buffered recording never uploaded")
	}
	if inserter.inserted() != "Yes." || service.Status().State != "idle" {
		t.Fatal("short final transcript not inserted exactly once")
	}
	// The pending Stop must never leak into a later dictation.
	service.recorder = &streamingFixtureRecorder{initial: samples}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	if service.Status().State != "recording" {
		t.Fatal("previous quick Stop stopped the next recording")
	}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	if inserter.inserted() != "Yes.Yes." {
		t.Fatal("next recording missing or duplicated")
	}
}

func TestEmptyCloudCompletionWarnsAndLogsCapturedSignal(t *testing.T) {
	setupEngineTest(t)
	view := readSettingsTest(t)
	if err := SaveAPIKey(providerOpenAI, "test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: providerOpenAI, Name: "Streaming"}); err != nil {
		t.Fatal(err)
	}
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		_, _ = readTestJSON(ctx, conn)
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		for {
			event, err := readTestJSON(ctx, conn)
			if err != nil {
				return
			}
			if event["type"] == "input_audio_buffer.commit" {
				_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "empty"})
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "empty", "transcript": ""})
			}
		}
	})
	original := http.DefaultTransport
	http.DefaultTransport = redirectedRealtimeTransport{target: endpoint, original: original}
	defer func() { http.DefaultTransport = original }()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &streamingFixtureRecorder{initial: []int16{32767, -32768, 5000, -5000}}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	err = service.Toggle()
	if err != nil || service.Status().Warning != noSpeechWarning || service.Status().LastAttemptFailed {
		t.Fatalf("misreported empty final response: %v", err)
	}
	records, err := service.Logs("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 1 || records.Records[0].Outcome != "no_speech" || records.Records[0].Level != "warn" || records.Records[0].Error != "" || !strings.Contains(records.Records[0].Message, "RMS") || !strings.Contains(records.Records[0].Message, "peak") {
		t.Fatal("empty-result signal diagnostics missing")
	}
}

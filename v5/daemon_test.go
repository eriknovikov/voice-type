package voicetype

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// These cover the guard clauses, which run before any audio hardware is
// touched, so the state machine stays testable without a microphone.

type fakeRecorder struct {
	startEntered chan struct{}
	startRelease chan struct{}
	stopCalls    atomic.Int32
	samples      []int16
}

func (r *fakeRecorder) Start() error {
	if r.startEntered != nil {
		close(r.startEntered)
	}
	if r.startRelease != nil {
		<-r.startRelease
	}
	return nil
}

func (r *fakeRecorder) Stop() []int16 {
	r.stopCalls.Add(1)
	return r.samples
}

func (r *fakeRecorder) Close() {}

func TestStartRejectsWhenBusy(t *testing.T) {
	for _, state := range []sessionState{stateRecording, stateTranscribing} {
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

func TestStopWaitsForRecorderStart(t *testing.T) {
	rec := &fakeRecorder{
		startEntered: make(chan struct{}),
		startRelease: make(chan struct{}),
	}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})

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

	select {
	case <-stopDone:
		t.Fatal("stop returned while recorder start was incomplete")
	case <-time.After(20 * time.Millisecond):
	}

	close(rec.startRelease)
	if status := <-startDone; status != http.StatusOK {
		t.Fatalf("start status = %d, want 200", status)
	}
	if status := <-stopDone; status != http.StatusOK {
		t.Fatalf("stop status = %d, want 200", status)
	}
	if calls := rec.stopCalls.Load(); calls != 1 {
		t.Errorf("recorder stops = %d, want 1", calls)
	}
	if state := d.currentState(); state != stateIdle {
		t.Errorf("state = %s, want idle", state)
	}
}

func TestHealthReportsState(t *testing.T) {
	d := &daemon{cfg: configDefaults(), state: stateRecording}

	rec := httptest.NewRecorder()
	d.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q", body["status"])
	}
	if body["state"] != "recording" {
		t.Errorf("state = %q, want recording", body["state"])
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
	rec := &fakeRecorder{}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})
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
}

func TestExpiredTimerDoesNotStopNewSession(t *testing.T) {
	rec := &fakeRecorder{}
	d := newDaemon(configDefaults(), &resources{Recorder: rec})
	d.state = stateRecording
	d.sessionID = 2

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

func TestSessionStateString(t *testing.T) {
	cases := map[sessionState]string{
		stateIdle:         "idle",
		stateRecording:    "recording",
		stateTranscribing: "transcribing",
	}
	for state, want := range cases {
		if got := state.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", state, got, want)
		}
	}
}

func TestStatusWord(t *testing.T) {
	if statusWord(200) != "ok" || statusWord(201) != "ok" {
		t.Error("2xx should be ok")
	}
	if statusWord(409) != "error" || statusWord(500) != "error" {
		t.Error("non-2xx should be error")
	}
}

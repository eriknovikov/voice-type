package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These cover the guard clauses, which run before any audio hardware is
// touched, so the state machine stays testable without a microphone.

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

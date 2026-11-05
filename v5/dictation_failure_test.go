package voicetype

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type failureRecorder struct {
	startErr, stopErr error
	samples           []int16
}

func (r *failureRecorder) Start() error           { return r.startErr }
func (r *failureRecorder) Stop() ([]int16, error) { return r.samples, r.stopErr }
func (r *failureRecorder) Close()                 {}
func TestDictationFailureStaysVisibleUntilNextAttempt(t *testing.T) {
	for _, kind := range []string{"configuration_error", "capture_start", "capture_stop", "no_audio", "auth_error", "empty_transcript"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("OPENROUTER_API_KEY", "")
			config := `{"provider":"openai","api_key":"fixture"}`
			if kind == "configuration_error" {
				config = `{"provider":"openai"}`
			}
			if err := os.WriteFile(configFilePath(), []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			rec := &failureRecorder{samples: make([]int16, 3200)}
			switch kind {
			case "capture_start":
				rec.startErr = errors.New("capture failed")
			case "capture_stop":
				rec.stopErr = errors.New("capture failed")
			case "no_audio":
				rec.samples = nil
			}
			cfg, _ := configReadStrict()
			settings, _ := cfg.resolveSTT("", "")
			d := newDaemon(cfg, &resources{Recorder: rec}, settings, "")
			d.runLog = newRunLog("test")
			defer d.runLog.close("ok")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "auth_error" {
					w.WriteHeader(401)
					w.Write([]byte(`{"error":{"message":"fixture"}}`))
					return
				}
				w.Write([]byte(`{"text":""}`))
			}))
			defer server.Close()
			code, _ := d.startSession()
			if code == 200 {
				d.stt.endpoint = server.URL
				code, _ = d.stopSession()
			}
			if code < 400 {
				t.Fatal("failed attempt reported success")
			}
			if !d.lastAttemptFailed || d.currentState() != stateIdle {
				t.Fatal("failure did not persist while idle")
			}
			w := httptest.NewRecorder()
			d.managementRoutes().ServeHTTP(w, httptest.NewRequest("GET", "/status", nil))
			var status struct {
				Failed bool `json:"last_attempt_failed"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if !status.Failed {
				t.Fatal("desktop cannot observe failure")
			}
			records, err := d.runLog.records()
			if err != nil || len(records) == 0 {
				t.Fatal("failure absent from logs")
			}
			if err := os.WriteFile(configFilePath(), []byte(`{"provider":"openai","api_key":"fixture"}`), 0600); err != nil {
				t.Fatal(err)
			}
			rec.startErr = nil
			if code, _ := d.startSession(); code != 200 {
				t.Fatal("retry failed to start")
			}
			if d.lastAttemptFailed {
				t.Fatal("new attempt did not clear failure")
			}
			d.shutdown()
		})
	}
}

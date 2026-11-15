package voicetype

import (
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
	for _, kind := range []string{"configuration_error", "capture_start", "capture_stop", "capture_stop_empty", "auth_error", "provider_error", "missing_transcript"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("TMPDIR", t.TempDir())
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("OPENROUTER_API_KEY", "")
			useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
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
			case "capture_stop", "capture_stop_empty":
				rec.stopErr = errors.New("capture failed")
				if kind == "capture_stop_empty" {
					rec.samples = nil
				}
			}
			cfg, _ := configReadStrict()
			settings, _ := cfg.resolveSTT("")
			d := newEngine(cfg, rec, settings)
			d.runLog = newRunLog("test")
			defer d.runLog.close("ok")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "auth_error" {
					w.WriteHeader(401)
					w.Write([]byte(`{"error":{"message":"fixture"}}`))
					return
				}
				if kind == "provider_error" {
					w.WriteHeader(http.StatusBadRequest)
					w.Write([]byte(`{"error":{"message":"fixture"}}`))
					return
				}
				if kind == "missing_transcript" {
					w.Write([]byte(`{}`))
					return
				}
				w.Write([]byte(`{"text":""}`))
			}))
			defer server.Close()
			operationErr := d.Toggle()
			if operationErr == nil {
				d.stt.endpoint = server.URL
				operationErr = d.Toggle()
			}
			if operationErr == nil {
				t.Fatal("failed attempt reported success")
			}
			if !d.lastAttemptFailed || d.Status().State != "idle" {
				t.Fatal("failure did not persist while idle")
			}
			if !d.Status().LastAttemptFailed {
				t.Fatal("desktop cannot observe failure")
			}
			if d.Status().Warning != "" || trayState(d.Status()) != "error" {
				t.Fatal("real failure was downgraded to a warning")
			}
			records, err := d.runLog.records()
			if err != nil || len(records) == 0 {
				t.Fatal("failure absent from logs")
			}
			if err := os.WriteFile(configFilePath(), []byte(`{"provider":"openai","api_key":"fixture"}`), 0600); err != nil {
				t.Fatal(err)
			}
			rec.startErr = nil
			if err := d.Toggle(); err != nil {
				t.Fatal("retry failed to start")
			}
			if d.lastAttemptFailed {
				t.Fatal("new attempt did not clear failure")
			}
			d.Close()
		})
	}
}

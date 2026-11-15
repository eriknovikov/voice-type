package voicetype

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func warningTestEngine(t *testing.T, provider string, recorder audioRecorder) *engine {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
	if err := os.WriteFile(configFilePath(), []byte(fmt.Sprintf(`{"provider":%q,"api_key":"fixture"}`, provider)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := cfg.resolveSTT("")
	if err != nil {
		t.Fatal(err)
	}
	service := newEngine(cfg, recorder, settings)
	service.runLog = newRunLog("test")
	t.Cleanup(service.Close)
	return service
}

func TestNoSpeechIsAWarningNotAFailedDictation(t *testing.T) {
	for _, test := range []struct {
		name, provider, response string
		samples                  []int16
	}{
		{"no_audio", providerOpenAI, `{"text":""}`, nil},
		{"empty_openai", providerOpenAI, `{"text":""}`, []int16{0, 0, 0, 0}},
		{"whitespace_openai", providerOpenAI, `{"text":"  \n "}`, []int16{100, -100}},
		{"empty_openrouter", providerOpenRouter, `{"text":""}`, []int16{100, -100}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := warningTestEngine(t, test.provider, &failureRecorder{samples: test.samples})
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			if err := service.Toggle(); err != nil {
				t.Fatal(err)
			}
			service.stt.endpoint = server.URL
			if err := <-service.ToggleAsync(); err != nil {
				t.Fatalf("warning reached desktop error handler: %v", err)
			}
			status := service.Status()
			if status.State != "idle" || status.Error != "" || status.LastAttemptFailed || status.Warning != noSpeechWarning || status.WarningVersion != 1 || trayState(status) != "warning" {
				t.Fatalf("incorrect no-speech status: %+v", status)
			}
			if len(test.samples) == 0 && requests.Load() != 0 {
				t.Fatal("empty capture was uploaded")
			}
			warnings, err := service.Logs("warnings", 0)
			if err != nil || len(warnings.Records) != 1 {
				t.Fatalf("warning log missing: %+v %v", warnings, err)
			}
			record := warnings.Records[0]
			if record.Outcome != "no_speech" || record.Level != "warn" || record.Error != "" || !strings.Contains(record.Message, noSpeechWarning) {
				t.Fatalf("warning logged as an error: %+v", record)
			}
			errors, err := service.Logs("errors", 0)
			if err != nil || errors.Total != 0 {
				t.Fatalf("no speech appeared in error filter: %+v %v", errors, err)
			}
			var exported bytes.Buffer
			if err := service.WriteLogs("warnings", &exported); err != nil || !strings.Contains(exported.String(), `"level":"warn"`) {
				t.Fatalf("warning export missing: %v", err)
			}
			if check := lastDictationCheck(); check.Label != "Last dictation" || check.Status != "warn" {
				t.Fatalf("warning described as a recent failure: %+v", check)
			}
			if files, _ := filepath.Glob(filepath.Join(os.TempDir(), "voice-type", "*.wav")); len(files) != 0 {
				t.Fatal("empty speech created failure-recovery audio")
			}
			if err := service.Toggle(); err != nil {
				t.Fatal(err)
			}
			if next := service.Status(); next.Warning != "" || trayState(next) != "recording" {
				t.Fatalf("new recording did not override warning: %+v", next)
			}
			service.stt.endpoint = server.URL
			if err := service.Toggle(); err != nil {
				t.Fatal(err)
			}
			if service.Status().WarningVersion != 2 {
				t.Fatal("repeated warnings share a dismissal identity")
			}
			service.mu.Lock()
			expires := service.warningUntil
			expiresCorrectly := service.currentWarning(expires.Add(-time.Nanosecond)) == noSpeechWarning && service.currentWarning(expires) == ""
			service.warningUntil = time.Now().Add(-time.Second)
			service.mu.Unlock()
			if !expiresCorrectly {
				t.Fatal("warning does not expire at its deadline")
			}
			if expired := service.Status(); expired.Warning != "" || trayState(expired) != "idle" || expired.LastAttemptFailed {
				t.Fatalf("warning did not return to idle: %+v", expired)
			}
		})
	}
}

func TestEmptyLocalStreamIsANoSpeechWarning(t *testing.T) {
	setupEngineTest(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
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
	service.recorder = &failureRecorder{samples: []int16{0, 0, 0, 0}}
	service.state = stateRecording
	service.sessionCtx = context.Background()
	service.activeStream = newLocalStreamSession(context.Background(), streamingModeProcess, &fakeLocalStream{}, nil)
	if err := service.stopSessionMatching(0, false); err != nil {
		t.Fatal(err)
	}
	if status := service.Status(); status.Warning != noSpeechWarning || status.LastAttemptFailed {
		t.Fatalf("local empty result was treated as a failure: %+v", status)
	}
}

func TestEmptyCloudStreamWarnsWithoutSendingACommit(t *testing.T) {
	setupEngineTest(t)
	useCredentialStore(t, &memoryCredentials{values: map[string]string{}})
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
				t.Error("empty recording sent a commit")
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
	service.recorder = &streamingFixtureRecorder{initial: []int16{}}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	stream := service.activeStream.(*cloudStreamSession)
	select {
	case <-stream.ready:
	case <-time.After(time.Second):
		t.Fatal("WebSocket did not become ready")
	}
	if err := service.Toggle(); err != nil {
		t.Fatalf("empty cloud capture surfaced as an error: %v", err)
	}
	if status := service.Status(); status.Warning != noSpeechWarning || status.LastAttemptFailed {
		t.Fatalf("incorrect empty cloud warning: %+v", status)
	}
}

func TestNoSpeechWarningDoesNotHideARealFailureInDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	log := newRunLog("test")
	defer log.close("ok")
	log.complete(providerOpenAI, defaultModelID, "", "auth_error", 0, 0, nil, &sttError{Provider: providerOpenAI, Status: http.StatusUnauthorized})
	log.complete(providerOpenAI, defaultModelID, "", "no_speech", 0, 0, nil, errNoSpeech)
	if check := lastDictationCheck(); check.Label != "Recent failure" || !strings.Contains(check.Detail, "auth_error") {
		t.Fatalf("warning hid genuine failure: %+v", check)
	}
	var decoded runLogRecord
	data, err := os.ReadFile(runLogFilePath())
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if err := json.Unmarshal(lines[len(lines)-1], &decoded); err != nil || decoded.Error != "" || decoded.Level != "warn" {
		t.Fatalf("warning serialization lost severity: %+v %v", decoded, err)
	}
}

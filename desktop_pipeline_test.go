package voicetype

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type fixtureRecorder struct{}

func (*fixtureRecorder) Start() error           { return nil }
func (*fixtureRecorder) Stop() ([]int16, error) { return make([]int16, 3200), nil }
func (*fixtureRecorder) Close()                 {}

// This uses real IBus on an isolated bus and a controlled provider endpoint.
// It verifies that GUI-managed settings feed the existing recording pipeline.
func TestDesktopPipeline(t *testing.T) {
	executable := os.Getenv("VOICE_TYPE_TEST_IBUS_DAEMON")
	if executable == "" {
		t.Skip("set VOICE_TYPE_TEST_IBUS_DAEMON for isolated end-to-end insertion")
	}
	address, _ := startTestIBus(t, executable)
	control := testIBusConnection(t, address)
	const baseline = "desktop-testbaseline"
	registerTestIBusEngine(t, control, baseline)
	testIBusCall(t, control, ibusPath, ibusService+".SetGlobalEngine", baseline)
	client := newTestIBusClient(t, address, "desktop-pipeline-client", 0)
	client.focus(t)
	setupEngineTest(t)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	for _, provider := range []string{providerOpenAI, providerOpenRouter} {
		t.Run(provider, func(t *testing.T) {
			settings, err := ReadSettings()
			if err != nil {
				t.Fatal(err)
			}
			key := "test-credential"
			if err := SaveAPIKey(provider, key); err != nil {
				t.Fatal(err)
			}
			settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: provider + " 1", Provider: provider, StreamingMode: streamingModeNone})
			if err != nil {
				t.Fatal(err)
			}
			settings, err = SelectProfile(settings.Revision, settings.Profiles[len(settings.Profiles)-1].ID)
			if err != nil {
				t.Fatal(err)
			}
			vocab := []VocabularyList{{Name: "general", Terms: []string{"Wails"}}, {Name: "project", Terms: []string{"PipeWire"}}}
			_, err = SaveSettings(SettingsUpdate{Revision: settings.Revision, Vocabulary: &vocab})
			if err != nil {
				t.Fatal(err)
			}
			if err := SelectWorkspace("project"); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-credential" {
					t.Error("provider credential missing")
				}
				if provider == providerOpenAI {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if r.FormValue("model") != providerModel(provider) {
						t.Error("wrong model")
					}
					if strings.Join(r.MultipartForm.Value["keywords[]"], ",") != "Wails,PipeWire" {
						t.Error("keywords not sourced from selected workspace")
					}
					file, _, err := r.FormFile("file")
					if err != nil {
						t.Error(err)
					} else {
						data, _ := io.ReadAll(file)
						file.Close()
						if len(data) < 44 || string(data[:4]) != "RIFF" {
							t.Error("invalid audio upload")
						}
					}
				} else {
					var payload map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					var audio map[string]string
					_ = json.Unmarshal(payload["input_audio"], &audio)
					wav, err := base64.StdEncoding.DecodeString(audio["data"])
					if err != nil || len(wav) < 44 {
						t.Error("invalid OpenRouter audio")
					}
					if !strings.Contains(string(payload["provider"]), "Wails, PipeWire") {
						t.Error("vocabulary prompt missing")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"text":"Desktop insertion verified."}`)
			}))
			defer server.Close()
			service, err := startEngine("test")
			if err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			d := service
			d.recorder = &fixtureRecorder{}
			if err := service.Toggle(); err != nil {
				t.Fatal("start:", err)
			}
			d.stt.endpoint = server.URL
			if err := <-service.ToggleAsync(); err != nil {
				t.Fatal("stop:", err)
			}
			client.wantCommit(t, "Desktop insertion verified.")
			assertTestIBusRestored(t, control, baseline)
			logs, err := d.runLog.records()
			if err != nil {
				t.Fatal(err)
			}
			if logs[0].Outcome != "ok" {
				t.Fatal("completion not recorded")
			}
			if provider == providerOpenAI {
				protected := newTestIBusClient(t, address, "desktop-protected-client", 8)
				protected.focus(t)
				if err := d.Toggle(); err != nil {
					t.Fatal("second recording did not start")
				}
				d.stt.endpoint = server.URL
				if err := d.Toggle(); err == nil {
					t.Fatal("protected field insertion was accepted")
				}
				protected.wantNoCommit(t)
				logs, err = d.runLog.records()
				if err != nil {
					t.Fatal(err)
				}
				if !d.lastAttemptFailed {
					t.Fatal("insertion failure did not latch tray error")
				}
				if logs[0].Outcome != "insertion_error" {
					t.Fatal("insertion failure absent from logs")
				}
				client.focus(t)
			}
			raw, _ := json.Marshal(logs)
			if strings.Contains(string(raw), "Desktop insertion") || strings.Contains(string(raw), "test-credential") {
				t.Fatal("sensitive content in logs")
			}
		})
	}
}

// The caller supplies a synthetic test recording, never captured user speech.
func TestLiveProviders(t *testing.T) {
	path := os.Getenv("VOICE_TYPE_TEST_LIVE_WAV")
	if path == "" {
		t.Skip("set VOICE_TYPE_TEST_LIVE_WAV for live provider verification")
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal("cannot read local provider configuration")
	}
	for _, profile := range cfg.Profiles {
		t.Run(profile.Name, func(t *testing.T) {
			provider := profile.Provider
			key := ""
			if provider != providerLocal {
				var saved bool
				key, saved, err = providerAPIKey(provider)
				if err != nil || !saved {
					t.Fatal("cannot read configured keyring credential", err)
				}
			}
			client := newSTTClient(sttSettings{Provider: provider, APIKey: key, Model: providerModel(provider)})
			result, err := client.TranscribeContext(context.Background(), wav)
			if err != nil {
				t.Fatalf("provider request failed: %s", safeRequestError(err))
			}
			if !strings.Contains(strings.ToLower(result.Text), "voice") {
				t.Fatal("synthetic test phrase was not recognized")
			}
			t.Log("synthetic speech transcribed successfully; transcript and credentials omitted")
		})
	}
}

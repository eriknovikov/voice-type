package voicetype

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// These tests inspect actual HTTP Authorization headers, not just the UI's
// selection: switching accounts must never reroute an in-flight recording.
func TestProfileSessionIsolation(t *testing.T) {
	for _, phase := range []string{"recording", "transcribing"} {
		for _, action := range []string{"select", "edit", "delete"} {
			t.Run(phase+"/"+action, func(t *testing.T) {
				setupEngineTest(t)
				settings, err := ReadSettings()
				if err != nil {
					t.Fatal(err)
				}
				firstKey, secondKey := "first-private-key", "second-private-key"
				if err := SaveAPIKey(providerOpenAI, firstKey); err != nil {
					t.Fatal(err)
				}
				settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Personal", Provider: "openai", StreamingMode: streamingModeNone})
				if err != nil {
					t.Fatal(err)
				}
				firstID := settings.ActiveProfile
				settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Work", Provider: "openai", StreamingMode: streamingModeNone})
				if err != nil {
					t.Fatal(err)
				}
				secondID := settings.Profiles[1].ID
				service, err := startEngine("test")
				if err != nil {
					t.Fatal(err)
				}
				defer service.Close()
				service.recorder = &fixtureRecorder{}
				entered, release := make(chan string, 1), make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					entered <- r.Header.Get("Authorization")
					if phase == "transcribing" {
						<-release
					}
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"text":""}`)) // No desktop insertion or paid provider call.
				}))
				defer server.Close()
				if err := service.Toggle(); err != nil {
					t.Fatal(err)
				}
				service.stt.endpoint = server.URL
				original := service.stt
				stopped := make(chan error, 1)
				if phase == "transcribing" {
					go func() { stopped <- service.Toggle() }()
					if got := <-entered; got != "Bearer "+firstKey {
						t.Fatal("wrong session credential")
					}
				}
				switch action {
				case "select":
					settings, err = SelectProfile(settings.Revision, secondID)
				case "edit":
					settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, ID: firstID, Name: "Renamed personal", Provider: "openai", StreamingMode: streamingModeNone})
				case "delete":
					settings, err = DeleteProfile(settings.Revision, firstID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if action != "delete" {
					if err := SaveAPIKey(providerOpenAI, secondKey); err != nil {
						t.Fatal(err)
					}
				}
				service.RefreshSettings()
				if service.stt != original {
					t.Fatal("in-flight client changed")
				}
				if phase == "recording" {
					go func() { stopped <- service.Toggle() }()
					if got := <-entered; got != "Bearer "+firstKey {
						t.Fatal("wrong session credential")
					}
				} else {
					close(release)
				}
				if err := <-stopped; err != nil {
					t.Fatalf("empty provider response should warn, not fail: %v", err)
				}
				if service.Status().LastAttemptFailed {
					t.Fatal("empty response marked the in-flight session as failed")
				}
				if action != "delete" && service.Status().Warning != noSpeechWarning {
					t.Fatal("empty response warning missing")
				}
				if action == "delete" {
					if service.Status().Error == "" {
						t.Fatal("missing selection not surfaced")
					}
					if err := service.Toggle(); err == nil {
						t.Fatal("deleted profile used for next recording")
					}
				} else {
					if service.stt.settings.APIKey != secondKey {
						t.Fatal("next session did not receive saved profile")
					}
					if err := service.Toggle(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestMicrophoneFallbackPersistsWithoutChangingOtherSettings(t *testing.T) {
	setupEngineTest(t)
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	id := "bluetooth-stable-id"
	settings, err = SaveSettings(SettingsUpdate{Revision: settings.Revision, Microphone: &id})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := reconcileMicrophone(id, []AudioSource{{ID: id, Name: "Bluetooth mic"}}); err != nil || changed {
		t.Fatal("present source replaced", err)
	}
	if changed, err := reconcileMicrophone(id, []AudioSource{{ID: "built-in", Name: "Built-in"}}); err != nil || !changed {
		t.Fatal("missing source not replaced", err)
	}
	saved, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Microphone != "" {
		t.Fatal("fallback not persisted or changed other settings")
	}
	if changed, err := reconcileMicrophone(saved.Microphone, []AudioSource{{ID: id}}); err != nil || changed {
		t.Fatal("reconnection restored prior selection", err)
	}
	// An old enumeration cannot override a newer device choice.
	newer := "usb-stable-id"
	_, err = SaveSettings(SettingsUpdate{Revision: saved.Revision, Microphone: &newer})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := reconcileMicrophone(id, nil); err != nil || changed {
		t.Fatal("stale enumeration replaced new device", err)
	}
}

func TestAudioServerFailurePreservesMicrophone(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("PULSE_SERVER", "unix:"+t.TempDir()+"/unavailable-pulse")
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	id := "bluetooth-saved"
	_, err = SaveSettings(SettingsUpdate{Revision: settings.Revision, Microphone: &id})
	if err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Microphones(); err == nil {
		t.Fatal("unavailable server did not report failure")
	}
	saved, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Microphone != id {
		t.Fatal("server failure reset source preference")
	}
	if err := service.StartMicrophoneTest(id); err == nil {
		t.Fatal("test started without audio server")
	}
	saved, err = ReadSettings()
	if err != nil || saved.Microphone != id {
		t.Fatal("capture failure reset source preference", err)
	}
}

func TestRunningServiceReportsEachConfigurationRecovery(t *testing.T) {
	setupEngineTest(t)
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	previous := service.Status().NoticeVersion
	for _, damage := range []string{"{", "deleted", "{\"profiles\":false}"} {
		if damage == "deleted" {
			err = os.Remove(configFilePath())
		} else {
			err = os.WriteFile(configFilePath(), []byte(damage), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ReadSettings(); err != nil {
			t.Fatal(err)
		}
		status := service.Status()
		if status.Notice == "" || status.NoticeVersion <= previous {
			t.Fatal("runtime recovery notice missing", status)
		}
		previous = status.NoticeVersion
		// Polling must not duplicate the notice or its diagnostic entry.
		if service.Status().NoticeVersion != previous {
			t.Fatal("notice repeated on status polling")
		}
	}
	records, err := service.Logs("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	recovered := 0
	for _, record := range records.Records {
		if record.Event == "configuration.recovered" {
			recovered++
			if record.Message == "" {
				t.Fatal("recovery diagnostic missing details")
			}
		}
	}
	if recovered != 3 {
		t.Fatalf("expected three recovery diagnostics, got %d", recovered)
	}
}

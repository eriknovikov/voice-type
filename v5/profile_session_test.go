package voicetype

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests inspect actual HTTP Authorization headers, not just the UI's
// selection: switching accounts must never reroute an in-flight recording.
func TestProfileSessionIsolation(t *testing.T) {
	for _, phase := range []string{"recording", "transcribing"} {
		for _, action := range []string{"select", "edit", "delete"} {
			t.Run(phase+"/"+action, func(t *testing.T) {
				setupServiceTest(t)
				settings, err := ReadSettings()
				if err != nil {
					t.Fatal(err)
				}
				firstKey, secondKey := "first-private-key", "second-private-key"
				settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Personal", Provider: "openai", APIKey: &firstKey})
				if err != nil {
					t.Fatal(err)
				}
				firstID := settings.ActiveProfile
				settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Work", Provider: "openai", APIKey: &secondKey})
				if err != nil {
					t.Fatal(err)
				}
				secondID := settings.Profiles[1].ID
				service, err := StartService("test")
				if err != nil {
					t.Fatal(err)
				}
				defer service.Close()
				service.d.res.Recorder = &fixtureRecorder{}
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
				service.d.stt.endpoint = server.URL
				original := service.d.stt
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
					settings, err = UpsertProfile(ProfileUpdate{Revision: settings.Revision, ID: firstID, Name: "Renamed personal", Provider: "openai", APIKey: &secondKey})
				case "delete":
					settings, err = DeleteProfile(settings.Revision, firstID)
				}
				if err != nil {
					t.Fatal(err)
				}
				service.RefreshSettings()
				if service.d.stt != original {
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
				if err := <-stopped; err == nil {
					t.Fatal("empty provider response should fail")
				}
				if action == "delete" {
					if service.Status().Error == "" {
						t.Fatal("missing selection not surfaced")
					}
					if err := service.Toggle(); err == nil {
						t.Fatal("deleted profile used for next recording")
					}
				} else {
					if service.d.stt.settings.APIKey != secondKey {
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
	setupServiceTest(t)
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
	if saved.Microphone != "" || saved.Port != settings.Port {
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

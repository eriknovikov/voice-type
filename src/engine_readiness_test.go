package voicetype

import (
	"fmt"
	"os"
	"testing"
)

// A cold --toggle must record based on real engine readiness, not the UI's
// setup_complete marker. These cases pin what "ready" means.
func TestEngineRecordingReadiness(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		setupEngineTest(t)
		if err := SaveAPIKey(providerOpenAI, "fixture-key"); err != nil {
			t.Fatal(err)
		}
		settings, _ := ReadSettings()
		if _, err := UpsertProfile(ProfileUpdate{Revision: settings.Revision, Name: "Test", Provider: providerOpenAI, StreamingMode: streamingModeNone}); err != nil {
			t.Fatal(err)
		}
		service, err := startEngine("test")
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		if !service.RecordingReady() {
			t.Fatalf("configured install not ready: %+v", service.Status())
		}
	})

	t.Run("missing key", func(t *testing.T) {
		port := setupEngineTest(t)
		config := fmt.Sprintf(`{"port":%d,"profiles":[{"id":"p","name":"Personal","provider":"openai","config":{}}],"active_profile":"p"}`, port)
		if err := os.WriteFile(configFilePath(), []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		service, err := startEngine("test")
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		if service.RecordingReady() {
			t.Fatalf("missing key reported ready: %+v", service.Status())
		}
		if service.Status().Error == "" {
			t.Fatal("missing key did not surface a configuration error")
		}
	})

	t.Run("no profile", func(t *testing.T) {
		port := setupEngineTest(t)
		if err := os.WriteFile(configFilePath(), []byte(fmt.Sprintf(`{"provider":"openai","port":%d,"profiles":[]}`, port)), 0600); err != nil {
			t.Fatal(err)
		}
		if err := SaveAPIKey(providerOpenAI, "fixture-key"); err != nil {
			t.Fatal(err)
		}
		service, err := startEngine("test")
		if err != nil {
			t.Fatal(err)
		}
		defer service.Close()
		if service.RecordingReady() {
			t.Fatalf("profile-less install reported ready: %+v", service.Status())
		}
		// A fresh install is not a load failure; the first-run UI stays clear.
		if status := service.Status(); status.Error != "" {
			t.Fatalf("profile-less install surfaced an error: %+v", status)
		}
	})
}

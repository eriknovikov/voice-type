package voicetype

import (
	"context"
	"os"
	"strings"
	"testing"
)

type fixtureRecorder struct{}

func (*fixtureRecorder) Start() error           { return nil }
func (*fixtureRecorder) Stop() ([]int16, error) { return make([]int16, 3200), nil }
func (*fixtureRecorder) Close()                 {}

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

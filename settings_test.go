package voicetype

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func settingsTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "environment-must-not-be-used")
	t.Setenv("OPENROUTER_API_KEY", "environment-must-not-be-used")
	keyring.MockInit()
}
func ptr[T any](v T) *T { return &v }
func readSettingsTest(t *testing.T) Settings {
	t.Helper()
	s, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func addProfileTest(t *testing.T, s Settings, provider, name, key string) Settings {
	t.Helper()
	if key != "" {
		if err := SaveAPIKey(provider, key); err != nil {
			t.Fatal(err)
		}
	}
	s, err := UpsertProfile(ProfileUpdate{Revision: s.Revision, Provider: provider, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestProfileMigrationAndConflict(t *testing.T) {
	settingsTestHome(t)
	original := []byte(`{ // comments are accepted during migration
 "provider":"openai", "api_key":"legacy-fallback", "api_keys":{"openai":"saved-openai-key","openrouter":"saved-router-key"},
 "future":{"enabled":true}, "vocabulary":{"general":["Go"],"work":["Wails"]},
 }`)
	if err := os.WriteFile(configFilePath(), original, 0600); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	if len(view.Profiles) != 2 || view.ActiveProfile != "legacy-openai" {
		t.Fatalf("migration: %+v", view)
	}
	if !view.APIKeys[providerOpenAI].Saved || !view.APIKeys[providerOpenRouter].Saved {
		t.Fatal("migrated keys missing from keyring")
	}
	encoded, _ := json.Marshal(view)
	for _, secret := range []string{"saved-openai-key", "saved-router-key", "environment-must-not-be-used", "legacy-fallback"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("routine settings contain a secret")
		}
	}
	key, _, err := providerAPIKey(providerOpenAI)
	if err != nil || key != "saved-openai-key" {
		t.Fatal("saved key was not migrated", err)
	}
	backupDir, _ := BackupDirectory()
	backup, _ := os.ReadFile(filepath.Join(backupDir, "config-before-profiles.jsonc"))
	for _, secret := range []string{"saved-openai-key", "saved-router-key", "legacy-fallback"} {
		if strings.Contains(string(backup), secret) {
			t.Fatal("migration backup retained a plaintext API key")
		}
	}
	var migratedBackup map[string]json.RawMessage
	if err := json.Unmarshal(backup, &migratedBackup); err != nil || migratedBackup["future"] == nil || migratedBackup["vocabulary"] == nil {
		t.Fatal("sanitized migration backup lost legacy settings")
	}
	data, _ := os.ReadFile(configFilePath())
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	if raw["future"] == nil || raw["api_key"] != nil || raw["api_keys"] != nil || raw["provider"] != nil {
		t.Fatal("unknown fields lost or legacy fields retained")
	}
	saved, err := SelectProfile(view.Revision, "legacy-openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SelectProfile(view.Revision, "legacy-openai"); err == nil || !strings.Contains(err.Error(), "changed elsewhere") {
		t.Fatal("stale revision accepted")
	}
	key, _, err = providerAPIKey(providerOpenRouter)
	if err != nil || key != "saved-router-key" || saved.ActiveProfile != "legacy-openrouter" {
		t.Fatal("selected profile not used")
	}
}
func TestProfilesCreationEditingAndDeletion(t *testing.T) {
	settingsTestHome(t)
	view := readSettingsTest(t)
	if len(view.Profiles) != 0 || view.ActiveProfile != "" {
		t.Fatal("environment credentials imported")
	}
	cfg, _ := configReadStrict()
	if _, err := cfg.resolveSTT(""); err == nil {
		t.Fatal("dictation allowed without selection")
	}
	view = addProfileTest(t, view, "openai", "", "first-api-secret")
	first := view.Profiles[0].ID
	if view.Profiles[0].Name != "OpenAI 1" || view.ActiveProfile != first {
		t.Fatal("first profile default")
	}
	view = addProfileTest(t, view, "openai", "", "first-api-secret")
	second := view.Profiles[1].ID
	if view.Profiles[1].Name != "OpenAI 2" || view.ActiveProfile != first {
		t.Fatal("second profile selected unexpectedly")
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: "openrouter", Name: "Missing key"}); err == nil {
		t.Fatal("cloud profile accepted without a shared provider key")
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: "openrouter", Name: "openai 1"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := SaveAPIKey(providerOpenRouter, "other-key"); err != nil {
		t.Fatal(err)
	}
	view, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: first, Provider: providerLocal, Name: "changed", ModelID: localModelUnifiedEnglish, StreamingMode: streamingModeProcess})
	if err != nil || view.Profiles[0].Provider != providerLocal {
		t.Fatal("provider could not be changed", err)
	}
	view, err = UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: first, Provider: "openai", Name: "Personal"})
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := providerAPIKey(providerOpenAI)
	if err != nil || key != "first-api-secret" {
		t.Fatal("shared provider key changed")
	}
	if err := SaveAPIKey(providerOpenAI, "replacement-secret"); err != nil {
		t.Fatal(err)
	}
	view, err = UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: first, Provider: "openai", Name: "Personal", StreamingMode: streamingModeProcess})
	if err != nil {
		t.Fatal(err)
	}
	key, _, _ = providerAPIKey(providerOpenAI)
	if key != "replacement-secret" {
		t.Fatal("key not replaced")
	}
	view, err = DeleteProfile(view.Revision, first)
	if err != nil {
		t.Fatal(err)
	}
	if view.ActiveProfile != "" || len(view.Profiles) != 1 || view.Profiles[0].ID != second {
		t.Fatal("deleting active profile selected another account")
	}
	key, _, err = providerAPIKey(providerOpenAI)
	if err != nil || key != "replacement-secret" {
		t.Fatal("deleting a profile removed the shared provider key")
	}
	view = addProfileTest(t, view, "openrouter", "", "new-router-secret")
	if view.ActiveProfile != "" {
		t.Fatal("adding after active deletion selected account")
	}
}

func TestLocalProfilesKeepModeAndVocabularyWithoutCloudCredentials(t *testing.T) {
	settingsTestHome(t)
	view := readSettingsTest(t)
	vocabulary := []VocabularyList{{Name: "general", Terms: []string{"Voice Type"}}}
	view, err := SaveSettings(SettingsUpdate{Revision: view.Revision, Vocabulary: &vocabulary})
	if err != nil {
		t.Fatal(err)
	}
	view, err = UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		Name:          "Private",
		Provider:      providerLocal,
		ModelID:       localModelUnifiedEnglish,
		StreamingMode: streamingModeProcess,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := view.Profiles[0]
	if profile.Provider != providerLocal || profile.ModelID != localModelUnifiedEnglish || profile.StreamingMode != streamingModeProcess {
		t.Fatalf("local profile summary: %+v", profile)
	}
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := cfg.resolveSTT("")
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != providerLocal || settings.Model != localModelUnifiedEnglish || settings.StreamingMode != streamingModeProcess || len(settings.Vocabulary) != 1 {
		t.Fatalf("local settings lost profile fields or vocabulary: %+v", settings)
	}
	if err := settings.validateCredentials(); err != nil {
		t.Fatalf("local profile should not require an API key: %v", err)
	}
	if _, err := UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		ID:            profile.ID,
		Name:          profile.Name,
		Provider:      providerLocal,
		ModelID:       localModelParakeetV3,
		StreamingMode: streamingModeLive,
	}); err == nil {
		t.Fatal("non-streaming Parakeet v3 accepted live insertion")
	}
	view, err = UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		ID:            profile.ID,
		Name:          profile.Name,
		Provider:      providerLocal,
		ModelID:       localModelParakeetV3,
		StreamingMode: streamingModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	settings, err = cfg.resolveSTT("")
	if err != nil || settings.Model != localModelParakeetV3 || settings.StreamingMode != streamingModeNone || len(settings.Vocabulary) != 1 {
		t.Fatalf("batch local profile round-trip lost settings: %+v %v", settings, err)
	}
}

func TestLocalProfileRejectsCloudKeyAndUnknownModel(t *testing.T) {
	settingsTestHome(t)
	view := readSettingsTest(t)
	secret := "must-not-be-used"
	if err := SaveAPIKey(providerLocal, secret); err == nil {
		t.Fatal("local provider accepted an API key")
	}
	if _, err := UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		Name:          "Local",
		Provider:      providerLocal,
		ModelID:       localModelUnifiedEnglish,
		StreamingMode: streamingModeProcess,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{
		Revision:      view.Revision,
		Name:          "Unknown",
		Provider:      providerLocal,
		ModelID:       "arbitrary-file.gguf",
		StreamingMode: streamingModeNone,
	}); err == nil {
		t.Fatal("local profile accepted an unlisted model")
	}
}

func TestMigrationDoesNotResurrectRemovedLegacyKey(t *testing.T) {
	settingsTestHome(t)
	data := []byte(`{"provider":"openai","api_key":"old-secret","api_keys":{"openai":"","openrouter":"other-secret"}}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	if len(view.Profiles) != 1 || view.Profiles[0].Provider != "openrouter" || view.ActiveProfile != "" {
		t.Fatal("removed legacy key resurrected or another profile selected")
	}
}
func TestSettingsFieldPatchesAndVocabularySelection(t *testing.T) {
	settingsTestHome(t)
	view := readSettingsTest(t)
	vocab := []VocabularyList{{Name: "general", Terms: []string{"Go"}}, {Name: "work", Terms: []string{"Wails"}}}
	var err error
	view, err = SaveSettings(SettingsUpdate{Revision: view.Revision, Vocabulary: &vocab})
	if err != nil {
		t.Fatal(err)
	}
	if err := SelectWorkspace("work"); err != nil {
		t.Fatal(err)
	}
	if err := SelectWorkspace("missing"); err == nil {
		t.Fatal("unknown vocabulary accepted")
	}
	view, err = SaveSettings(SettingsUpdate{Revision: view.Revision, Microphone: ptr("stable-source")})
	if err != nil {
		t.Fatal(err)
	}
	if view.Microphone != "stable-source" || len(view.Vocabulary) != 2 {
		t.Fatal("field patch overwrote unrelated fields")
	}
	vocab[1].Name, vocab[1].OriginalName = "renamed", "work"
	view, err = SaveSettings(SettingsUpdate{Revision: view.Revision, Vocabulary: &vocab})
	if err != nil || view.Workspace != "renamed" {
		t.Fatal("active rename lost", err)
	}
	vocab = vocab[:1]
	view, err = SaveSettings(SettingsUpdate{Revision: view.Revision, Vocabulary: &vocab})
	if err != nil || view.Workspace != "" {
		t.Fatal("deleted vocabulary remains selected", err)
	}
	bad := []VocabularyList{{Name: "general", Terms: []string{"secret\nterm"}}}
	if _, err := SaveSettings(SettingsUpdate{Revision: view.Revision, Vocabulary: &bad}); err == nil {
		t.Fatal("invalid term accepted")
	}
}
func TestConfigRecoveryAndPrivatePermissions(t *testing.T) {
	for _, damage := range []string{"missing", "truncated", "semantic", "damaged-backup"} {
		t.Run(damage, func(t *testing.T) {
			settingsTestHome(t)
			view := addProfileTest(t, readSettingsTest(t), "openai", "Personal", "private-key-for-recovery")
			if view.ActiveProfile == "" {
				t.Fatal("missing active")
			}
			backupDir, _ := BackupDirectory()
			broken := []byte(`{"profiles":`)
			switch damage {
			case "missing":
				if err := os.Remove(configFilePath()); err != nil {
					t.Fatal(err)
				}
			case "semantic":
				broken = []byte(`{"profiles":{},"active_profile":true}`)
				fallthrough
			default:
				if err := os.WriteFile(configFilePath(), broken, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if damage == "damaged-backup" {
				if err := os.WriteFile(filepath.Join(backupDir, "config.jsonc"), broken, 0600); err != nil {
					t.Fatal(err)
				}
			}
			view = readSettingsTest(t)
			if damage == "damaged-backup" {
				if len(view.Profiles) != 0 || view.ActiveProfile != "" {
					t.Fatal("invalid backup restored")
				}
			} else if len(view.Profiles) != 1 || view.ActiveProfile == "" {
				t.Fatal("last valid profile not restored")
			}
			if view.Notice == "" {
				t.Fatal("recovery not reported")
			}
			if damage != "missing" {
				files, _ := filepath.Glob(filepath.Join(backupDir, "config.jsonc.corrupt-*"))
				if len(files) != 1 {
					t.Fatal("corrupt original not preserved")
				}
				preserved, _ := os.ReadFile(files[0])
				if string(preserved) != string(broken) {
					t.Fatal("corrupt backup changed")
				}
			}
			for _, path := range []string{configFilePath(), filepath.Join(backupDir, "config.jsonc")} {
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("file permissions", err)
				}
			}
			info, _ := os.Stat(backupDir)
			if info.Mode().Perm() != 0700 {
				t.Fatal("backup directory not private")
			}
			view2 := readSettingsTest(t)
			if view2.Revision != view.Revision {
				t.Fatal("repeated startup changed recovered settings")
			}
		})
	}
}
func TestUnreadableConfigIsNotReplaced(t *testing.T) {
	settingsTestHome(t)
	if err := os.Mkdir(configFilePath(), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSettings(); err == nil {
		t.Fatal("filesystem failure hidden")
	}
	info, err := os.Stat(configFilePath())
	if err != nil || !info.IsDir() {
		t.Fatal("unreadable config replaced")
	}
}
func TestAtomicPrivateWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config")
	if err := AtomicPrivateWrite(path, []byte("complete")); err != nil {
		t.Fatal(err)
	}
	if err := AtomicPrivateWrite(path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatal("atomic write", err)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".voice-type-*"))
	if len(files) != 0 {
		t.Fatal("temporary files left behind")
	}
}

func TestMissingHomeReturnsActionableError(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := ReadSettings(); err == nil || !strings.Contains(err.Error(), "HOME") {
		t.Fatal("missing HOME should be reported without terminating the process")
	}
}

func TestProfileProviderConfigurationUnknownFieldsSurviveEdit(t *testing.T) {
	settingsTestHome(t)
	data := []byte(`{"profiles":[{"id":"p","name":"Personal","provider":"openai","config":{"api_key":"private-key","future_option":{"enabled":true}}}],"active_profile":"p","future_top_level":[1,2]}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "p", Name: "Renamed", Provider: "openai"}); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(configFilePath())
	if err != nil || !strings.Contains(string(encoded), "future_option") || !strings.Contains(string(encoded), "future_top_level") {
		t.Fatal("editing a profile lost unknown configuration", err)
	}
}

func TestUnknownProfileFieldsSurviveMigrationAndEdits(t *testing.T) {
	settingsTestHome(t)
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","future_profile":{"keep":true},"config":{"api_key":"old-key","future_config":true}}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "one", Name: "Updated", Provider: providerOpenAI}); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(configFilePath())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []map[string]json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(encoded, &doc); err != nil || len(doc.Profiles) != 1 || doc.Profiles[0]["future_profile"] == nil {
		t.Fatal("unknown profile fields were lost during migration or edit")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(doc.Profiles[0]["config"], &config); err != nil || config["future_config"] == nil || config["api_key"] != nil {
		t.Fatal("profile config was not preserved and sanitized")
	}
}

func TestEditingProfileWithNullConfig(t *testing.T) {
	settingsTestHome(t)
	data := []byte(`{"profiles":[{"id":"one","name":"One","provider":"openai","config":null}],"active_profile":"one"}`)
	if err := os.WriteFile(configFilePath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	updated, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "one", Name: "One", Provider: providerOpenAI, StreamingMode: streamingModeProcess})
	if err != nil || updated.Profiles[0].StreamingMode != streamingModeProcess {
		t.Fatal("editing a profile with an empty config failed", err)
	}
}

func TestOpenAIModesLanguagesAndRecordingSnapshot(t *testing.T) {
	settingsTestHome(t)
	legacy := []byte(`{"profiles":[{"id":"old","name":"Old","provider":"openai","config":{"api_key":"old-key","future":{"keep":true}}}],"active_profile":"old","vocabulary":{"general":["Voice Type"],"work":["Parakeet"]}}`)
	if err := os.WriteFile(configFilePath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	view := readSettingsTest(t)
	if view.Profiles[0].StreamingMode != streamingModeNone {
		t.Fatal("existing OpenAI profile no longer batch")
	}
	if err := SaveAPIKey(providerOpenAI, "new-key"); err != nil {
		t.Fatal(err)
	}
	view, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Name: "New", Provider: providerOpenAI})
	if err != nil {
		t.Fatal(err)
	}
	if view.Profiles[1].StreamingMode != streamingModeProcess || view.Profiles[1].ModelID != defaultModelID {
		t.Fatal("new profile did not default to WebSocket GPT Transcribe")
	}
	view, err = UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "old", Name: "Old", Provider: providerOpenAI, StreamingMode: streamingModeProcess, Languages: ptr([]string{"en", "es", "de", "yue", "zh-cn"})})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := cfg.resolveSTT("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Vocabulary) != 2 || len(snapshot.Languages) != 5 || snapshot.APIKey != "new-key" || snapshot.StreamingMode != streamingModeProcess {
		t.Fatalf("wrong recording snapshot: %#v", snapshot)
	}
	if err := SaveAPIKey(providerOpenAI, "replacement-key"); err != nil {
		t.Fatal(err)
	}
	view, err = UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "old", Name: "Changed", Provider: providerOpenAI, StreamingMode: streamingModeNone, Languages: ptr([]string{})})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Languages) != 5 || snapshot.APIKey != "new-key" || snapshot.StreamingMode != streamingModeProcess {
		t.Fatal("saved change mutated recording snapshot")
	}
	cfg, err = configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cfg.resolveSTT("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Languages) != 0 || next.StreamingMode != streamingModeNone || next.APIKey != "replacement-key" || cfg.Profiles[0].Config["future"] == nil {
		t.Fatal("next snapshot or unknown fields lost")
	}
	for _, bad := range [][]string{{"English"}, {"en", "en"}, {"en-us"}, {""}} {
		if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "old", Name: "Changed", Provider: providerOpenAI, Languages: &bad}); err == nil {
			t.Fatalf("invalid hints accepted: %v", bad)
		}
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: "old", Name: "Changed", Provider: providerOpenAI, StreamingMode: streamingModeLive}); err == nil {
		t.Fatal("OpenAI live insertion accepted")
	}
}

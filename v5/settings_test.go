package voicetype

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func settingsTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "environment-must-not-be-used")
	t.Setenv("OPENROUTER_API_KEY", "environment-must-not-be-used")
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
	s, err := UpsertProfile(ProfileUpdate{Revision: s.Revision, Provider: provider, Name: name, APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestProfileMigrationAndConflict(t *testing.T) {
	settingsTestHome(t)
	original := []byte(`{ // exact original remains in the private migration backup
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
	for _, p := range view.Profiles {
		if p.MaskedKey == "" {
			t.Fatal("missing key mask")
		}
	}
	encoded, _ := json.Marshal(view)
	for _, secret := range []string{"saved-openai-key", "saved-router-key", "environment-must-not-be-used", "legacy-fallback"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("routine settings contain a secret")
		}
	}
	cfg, err := configReadStrict()
	if err != nil || configAPIKey(cfg) != "saved-openai-key" {
		t.Fatal("saved key precedence changed", err)
	}
	backupDir, _ := BackupDirectory()
	backup, _ := os.ReadFile(filepath.Join(backupDir, "config-before-profiles.jsonc"))
	if string(backup) != string(original) {
		t.Fatal("migration backup changed original JSONC")
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
	cfg, _ = configReadStrict()
	if configAPIKey(cfg) != "saved-router-key" || saved.ActiveProfile != "legacy-openrouter" {
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
	if _, err := cfg.resolveSTT("", ""); err == nil {
		t.Fatal("dictation allowed without selection")
	}
	view = addProfileTest(t, view, "openai", "", "first-api-secret")
	first := view.Profiles[0].ID
	if view.Profiles[0].Name != "OpenAI 1" || view.ActiveProfile != first {
		t.Fatal("first profile default")
	}
	view = addProfileTest(t, view, "openai", "", "second-api-secret")
	second := view.Profiles[1].ID
	if view.Profiles[1].Name != "OpenAI 2" || view.ActiveProfile != first {
		t.Fatal("second profile selected unexpectedly")
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: "openrouter", Name: "openai 1", APIKey: ptr("other-key")}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: "openrouter", Name: "new", APIKey: ptr("")}); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: first, Provider: "openrouter", Name: "changed"}); err == nil {
		t.Fatal("provider mutated")
	}
	var err error
	view, err = UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: first, Provider: "openai", Name: "Personal"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := RevealProfileKey(first)
	if err != nil || key != "first-api-secret" {
		t.Fatal("name edit replaced key")
	}
	view, err = UpsertProfile(ProfileUpdate{Revision: view.Revision, ID: first, Provider: "openai", Name: "Personal", APIKey: ptr("replacement-secret")})
	if err != nil {
		t.Fatal(err)
	}
	key, _ = RevealProfileKey(first)
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
	cfg, _ = configReadStrict()
	if configAPIKey(cfg) != "" {
		t.Fatal("key selected after active deletion")
	}
	if _, err := RevealProfileKey(first); err == nil {
		t.Fatal("deleted secret still accessible")
	}
	view = addProfileTest(t, view, "openrouter", "", "new-router-secret")
	if view.ActiveProfile != "" {
		t.Fatal("adding after active deletion selected account")
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
	if _, err := SaveSettings(SettingsUpdate{Revision: view.Revision, Port: ptr(80)}); err == nil {
		t.Fatal("invalid port accepted")
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
				broken = []byte(`{"profiles":[],"port":-1}`)
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

package voicetype

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopSettingsMigrationAndConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	original := []byte(`{ // keep the original comments in backup
 "provider":"openai", "api_key":"private-openai", "future":{"enabled":true},
 "vocabulary":{"general":["Go"],"work":["Wails"]},
 }`)
	if err := os.WriteFile(configFilePath(), original, 0600); err != nil {
		t.Fatal(err)
	}
	view, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "private-openai") {
		t.Fatal("read exposes credentials")
	}
	update := SettingsUpdate{Revision: view.Revision, Provider: "openrouter", Port: 3232, Vocabulary: view.Vocabulary, APIKeys: map[string]string{"openrouter": "private-router"}}
	saved, err := SaveSettings(update)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Credentials["openai"].Saved || !saved.Credentials["openrouter"].Saved {
		t.Fatal("provider keys not retained")
	}
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal(err)
	}
	if configAPIKey(cfg) != "private-router" {
		t.Fatal("wrong provider key")
	}
	cfg.Provider = "openai"
	if configAPIKey(cfg) != "private-openai" {
		t.Fatal("legacy key not migrated")
	}
	data, _ := os.ReadFile(configFilePath())
	if !strings.Contains(string(data), `"future"`) || strings.Contains(string(data), `"api_key"`) {
		t.Fatal("unknown setting lost or legacy key retained")
	}
	backup, _ := os.ReadFile(configFilePath() + ".before-desktop")
	if string(backup) != string(original) {
		t.Fatal("backup did not retain original JSONC")
	}
	info, _ := os.Stat(configFilePath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
	if _, err := SaveSettings(update); err == nil {
		t.Fatal("stale update accepted")
	}
	update.Revision = saved.Revision
	update.APIKeys = map[string]string{"openai": ""}
	if _, err := SaveSettings(update); err != nil {
		t.Fatal(err)
	}
	cfg, _ = configReadStrict()
	cfg.Provider = "openai"
	if configAPIKey(cfg) != "" {
		t.Fatal("removed key resurrected")
	}
	t.Setenv("OPENAI_API_KEY", "environment-key")
	if configAPIKey(cfg) != "environment-key" {
		t.Fatal("environment precedence changed")
	}
}

func TestDesktopSettingsValidationAndWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	view, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	update := SettingsUpdate{Revision: view.Revision, Provider: "openai", Port: 3232, Vocabulary: []VocabularyList{{Name: "general", Terms: []string{"Go"}}, {Name: "work", Terms: []string{"Wails"}}}}
	if _, err := SaveSettings(update); err != nil {
		t.Fatal(err)
	}
	if err := SelectWorkspace("work"); err != nil {
		t.Fatal(err)
	}
	if workspaceLoad() != "work" {
		t.Fatal("workspace not persisted")
	}
	if err := SelectWorkspace("missing"); err == nil {
		t.Fatal("unknown workspace accepted")
	}
	if err := SelectWorkspace(""); err != nil {
		t.Fatal(err)
	}
	view, _ = ReadSettings()
	update.Revision = view.Revision
	update.Vocabulary = []VocabularyList{{Name: "general", Terms: []string{"secret\nterm"}}}
	if _, err := SaveSettings(update); err == nil {
		t.Fatal("invalid keyword accepted")
	}
	if err := os.WriteFile(configFilePath(), []byte(`{"provider":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveSettings(update); err == nil {
		t.Fatal("malformed config overwritten")
	}
}

func TestDesktopWorkspaceRenameAndDeletion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	view, _ := ReadSettings()
	update := SettingsUpdate{Revision: view.Revision, Provider: "openai", Port: 3232, Vocabulary: []VocabularyList{{Name: "general"}, {Name: "work", Terms: []string{"Wails"}}}}
	view, err := SaveSettings(update)
	if err != nil {
		t.Fatal(err)
	}
	if err := SelectWorkspace("work"); err != nil {
		t.Fatal(err)
	}
	update.Revision = view.Revision
	update.Vocabulary[1].Name = "renamed"
	update.Vocabulary[1].OriginalName = "work"
	view, err = SaveSettings(update)
	if err != nil {
		t.Fatal(err)
	}
	if view.Workspace != "renamed" {
		t.Fatal("renamed active workspace lost selection")
	}
	update.Revision = view.Revision
	update.Vocabulary = update.Vocabulary[:1]
	view, err = SaveSettings(update)
	if err != nil {
		t.Fatal(err)
	}
	if view.Workspace != "" {
		t.Fatal("deleted workspace is still selected")
	}
}

func TestRecoverMalformedSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	broken := []byte(`{"api_key":"keep-in-backup",`)
	if err := os.WriteFile(configFilePath(), broken, 0600); err != nil {
		t.Fatal(err)
	}
	revision, err := RecoveryRevision()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResetBrokenSettings("stale"); err == nil {
		t.Fatal("stale reset accepted")
	}
	if _, err := ResetBrokenSettings(revision); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".voice-type-config-recovery-*.jsonc"))
	if err != nil || len(files) != 1 {
		t.Fatal("recovery backup missing")
	}
	data, _ := os.ReadFile(files[0])
	if string(data) != string(broken) {
		t.Fatal("recovery backup changed content")
	}
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("recovery backup not private")
	}
	revision, _ = RecoveryRevision()
	if _, err := ResetBrokenSettings(revision); err == nil {
		t.Fatal("valid config was reset")
	}
}

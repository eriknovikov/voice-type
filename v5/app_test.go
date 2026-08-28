package voicetype

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintConfigReportsResolvedValuesWithoutKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("OPENROUTER_API_KEY", "sk-or-environment-secret")
	config := `{
    "api_key": "sk-or-file-secret",
    "port": 4321,
    "model": "whisper-large-v3",
    "vocabulary": ["Numbero", "Erik Novikov"]
}`
	if err := os.WriteFile(filepath.Join(home, "voice-type.jsonc"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := printConfig(&out); err != nil {
		t.Fatalf("printConfig: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"api_key:    set (from OPENROUTER_API_KEY)",
		"port:       4321",
		"model:      whisper-large-v3  ($0.027/hour, reads vocabulary)",
		"vocabulary: 2 terms",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{"sk-or-environment-secret", "sk-or-file-secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("output exposed API key %q", secret)
		}
	}
}

func TestPrintConfigRejectsMalformedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	path := filepath.Join(home, "voice-type.jsonc")
	if err := os.WriteFile(path, []byte(`{"port":`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := printConfig(&bytes.Buffer{})
	if err == nil {
		t.Fatal("printConfig accepted malformed JSONC")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "could not parse config file") {
		t.Errorf("error = %q, want path and parse context", err)
	}
}

func TestPrintModelsListsAllowlistAndDefault(t *testing.T) {
	var out bytes.Buffer
	printModels(&out)
	got := out.String()
	for _, model := range sttModels {
		if !strings.Contains(got, model.ID) {
			t.Errorf("models output lacks %q", model.ID)
		}
	}
	defaultLine := ""
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, defaultModelID) {
			defaultLine = line
		}
	}
	if !strings.Contains(defaultLine, "0.224") || !strings.Contains(defaultLine, "yes") || !strings.Contains(defaultLine, "(default)") {
		t.Errorf("default model not identified:\n%s", got)
	}
}

package voicetype

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type diagnosticCredentials struct {
	memoryCredentials
	reads int
}

func (s *diagnosticCredentials) Get(provider string) (string, error) {
	s.reads++
	return s.memoryCredentials.Get(provider)
}

func writeDiagnosticFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func diagnosticFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(os.Getenv("HOME"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[path] = string(data) + "|" + info.Mode().String() + "|" + info.ModTime().String()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestMacDiagnosticsAreReadOnlyAndDoNotLookUpCredentials(t *testing.T) {
	settingsTestHome(t)
	store := &diagnosticCredentials{memoryCredentials: memoryCredentials{values: map[string]string{providerOpenAI: "private-key-marker"}}}
	useCredentialStore(t, store)
	writeDiagnosticFixture(t, configFilePath(), `{"profiles":[{"id":"local","name":"Local","provider":"local","config":{"model_id":"parakeet-unified-en","streaming_mode":"live"}}],"active_profile":"local","api_key":"private-key-marker","vocabulary":{"general":["private-vocabulary-marker"]}}`)
	writeDiagnosticFixture(t, preferencesPath(), `{"shortcut":"Ctrl+Alt+Space","output_mode":"paste"}`)
	record, err := json.Marshal(runLogRecord{Event: "dictation.completed", Provider: providerOpenAI, ConfiguredModel: defaultModelID, Outcome: "auth_error", Error: "private-error-marker"})
	if err != nil {
		t.Fatal(err)
	}
	writeDiagnosticFixture(t, runLogFilePath(), string(record)+"\n")
	before := diagnosticFiles(t)
	enumerations := 0
	environment := macDiagnosticEnvironment{
		System: "macOS fixture", BundlePath: "/Applications/Voice Type.app", BundleID: "io.github.eriknovikov.voice-type", BundleVersion: "6.4.0", MinimumOS: "13.0", MemoryBytes: 8 << 30,
		Permissions: PermissionStatus{Platform: "darwin", Microphone: PermissionDenied, Accessibility: PermissionGranted}, Login: LoginStartupStatus{State: "requires_approval"},
		Sources: func() ([]AudioSource, error) {
			enumerations++
			return []AudioSource{{ID: "built-in", Name: "Built-in"}}, nil
		},
	}
	report := macDiagnostics("test", environment).Text()
	for _, text := range []string{"darwin/", "macOS fixture", "minimum macOS 13.0", "denied", "requires_approval", "Core Audio", "paste compatibility does not support live", "auth_error", "Keychain is not queried"} {
		if !strings.Contains(report, text) {
			t.Errorf("missing %q: %s", text, report)
		}
	}
	for _, text := range []string{"private-key-marker", "private-vocabulary-marker", "private-error-marker", "IBus", "PulseAudio", "AVX2", "GNOME"} {
		if strings.Contains(report, text) {
			t.Errorf("unsafe/platform-inaccurate report %q", text)
		}
	}
	if store.reads != 0 || enumerations != 1 || !reflect.DeepEqual(before, diagnosticFiles(t)) {
		t.Fatal("diagnostics looked up credentials or changed files")
	}
}

func TestDiagnosticConfigDoesNotRepairMigrateOrCreateFiles(t *testing.T) {
	for _, data := range []string{"", "{", `{"provider":"openai","api_key":"private-key-marker"}`} {
		t.Run(data, func(t *testing.T) {
			settingsTestHome(t)
			if data != "" {
				writeDiagnosticFixture(t, configFilePath(), data)
			}
			writeDiagnosticFixture(t, configFilePath()+".backup", `{"profiles":[]}`)
			before := diagnosticFiles(t)
			_, check, _ := readConfigForDiagnostics()
			if data == "{" && check.Status != "fail" {
				t.Fatal("invalid config silently recovered", check)
			}
			if data == "" && check.Status != "info" {
				t.Fatal("missing config was created", check)
			}
			if strings.Contains(check.Detail, "private-key-marker") || !reflect.DeepEqual(before, diagnosticFiles(t)) {
				t.Fatal("read-only config check mutated or leaked data")
			}
		})
	}
}

func TestMacDiagnosticAudioAndBundleFailures(t *testing.T) {
	for _, test := range []struct {
		saved   string
		sources []AudioSource
		err     error
		status  string
	}{
		{"", nil, errors.New("private-native-error-marker"), "warn"},
		{"", nil, nil, "info"},
		{"saved", []AudioSource{{ID: "saved", Name: "USB"}}, nil, "ok"},
		{"missing", []AudioSource{{ID: "saved", Name: "USB"}}, nil, "warn"},
	} {
		checks := macAudioChecks(test.saved, func() ([]AudioSource, error) { return test.sources, test.err })
		if checks[len(checks)-1].Status != test.status {
			t.Fatal(checks)
		}
		for _, check := range checks {
			if strings.Contains(check.Detail, "private-native-error-marker") {
				t.Fatal("native failure data leaked")
			}
		}
	}
	if check := macBundleCheck(macDiagnosticEnvironment{}); check.Status != "warn" {
		t.Fatal("unbundled executable reported ready")
	}
}

func TestDiagnosticLogsRejectSymlinksAndIncludeRotatedFailure(t *testing.T) {
	settingsTestHome(t)
	writeDiagnosticFixture(t, runLogFilePath()+".1", `{"event":"dictation.completed","provider":"openai","outcome":"auth_error"}`+"\n")
	writeDiagnosticFixture(t, runLogFilePath(), `{"event":"dictation.completed","outcome":"ok"}`+"\n")
	if check := lastDictationCheck(); check.Label != "Recent failure" || !strings.Contains(check.Detail, "auth_error") {
		t.Fatal("rotation hid a failure", check)
	}
	if err := os.Remove(runLogFilePath()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "private")
	writeDiagnosticFixture(t, target, "private-target-marker")
	if err := os.Symlink(target, runLogFilePath()); err != nil {
		t.Fatal(err)
	}
	if check := lastDictationCheck(); check.Status != "warn" || strings.Contains(check.Detail, "private-target-marker") {
		t.Fatal("diagnostics followed a symlink", check)
	}
}

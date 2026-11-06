package voicetype

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerCreatesProfilesWithoutImportingEnvironment(t *testing.T) {
	source, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	end := strings.Index(string(source), "\nwhile [ $# -gt 0 ]; do")
	if end < 0 {
		t.Fatal("installer entrypoint not found")
	}
	for _, tt := range []struct{ name, provider, key string }{
		{"openai", "openai", `test-key-with-quote"-and-backslash\`},
		{"openrouter", "openrouter", "test-router-key"},
		{"setup", "openai", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settingsTestHome(t)
			script := string(source[:end]) + `
has_tty() { return 1; }
PROVIDER="$TEST_INSTALL_PROVIDER"
API_KEY="$TEST_INSTALL_KEY"
write_config
print_summary
`
			path := filepath.Join(t.TempDir(), "installer-functions.sh")
			if err := os.WriteFile(path, []byte(script), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", path)
			cmd.Env = append(os.Environ(), "TEST_INSTALL_PROVIDER="+tt.provider, "TEST_INSTALL_KEY="+tt.key)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("installer failed: %v", err)
			}
			if tt.key != "" && strings.Contains(string(output), tt.key) {
				t.Fatal("installer printed key")
			}
			data, err := os.ReadFile(configFilePath())
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := configParse(data)
			if err != nil {
				t.Fatal(err)
			}
			if tt.key == "" {
				if len(cfg.Profiles) != 0 || cfg.ActiveProfile != "" {
					t.Fatal("installer imported environment key")
				}
			} else {
				if len(cfg.Profiles) != 1 || cfg.ActiveProfile != cfg.Profiles[0].ID || cfg.Profiles[0].Provider != tt.provider || configAPIKey(cfg) != tt.key {
					t.Fatal("installer profile invalid")
				}
			}
			info, _ := os.Stat(configFilePath())
			if info.Mode().Perm() != 0600 {
				t.Fatal("installer config is not private")
			}
		})
	}
}

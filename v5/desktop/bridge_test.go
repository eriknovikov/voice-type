package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDesktopAutostartPreferences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := application.New(application.Options{Name: "Voice Type"})
	// An unchanged shortcut does not contact the live desktop portal in this test.
	prefs := Preferences{Shortcut: "Ctrl+Alt+Space", SetupComplete: true}
	b := &Bridge{app: app, prefs: prefs}
	prefs.Autostart = true
	if err := b.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart", "voice-type.desktop")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "--background") {
		t.Fatal("login launch would open a window")
	}
	if got := readPreferences(); got != prefs {
		t.Fatalf("preferences round trip: %+v", got)
	}
	info, _ := os.Stat(preferencesPath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("preferences not private")
	}
	prefs.Autostart = false
	if err := b.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("autostart entry retained after disabling")
	}
}

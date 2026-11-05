package desktop

import (
	"errors"
	"io"
	"net/http"
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
	prefs := Preferences{Shortcut: "Ctrl+Space", DaemonShortcut: "Ctrl+Alt+Space", SetupComplete: true}
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

func TestShortcutDefaultsAndMigration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	prefs := readPreferences()
	if prefs.Shortcut != "Ctrl+Space" || prefs.DaemonShortcut != "Ctrl+Alt+Space" {
		t.Fatal("incorrect defaults")
	}
	if err := os.MkdirAll(filepath.Dir(preferencesPath()), 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ old, want string }{{"Ctrl+Alt+Space", "Ctrl+Space"}, {"Ctrl+F8", "Ctrl+F8"}} {
		if err := os.WriteFile(preferencesPath(), []byte(`{"shortcut":"`+test.old+`","setup_complete":true}`), 0600); err != nil {
			t.Fatal(err)
		}
		got := readPreferences()
		if got.Shortcut != test.want || got.DaemonShortcut != "Ctrl+Alt+Space" {
			t.Fatalf("migration: %+v", got)
		}
	}
}

func TestDaemonToggleStopsOnlyDaemonAndRejectsActiveCapture(t *testing.T) {
	for _, state := range []string{"idle", "recording", "testing", "transcribing"} {
		t.Run(state, func(t *testing.T) {
			quit := false
			client := &http.Client{Transport: desktopTestTransport(func(r *http.Request) (*http.Response, error) {
				if quit && r.URL.Path == "/status" {
					return nil, errors.New("offline")
				}
				payload := `{"state":"` + state + `"}`
				if r.URL.Path == "/quit" {
					quit = true
					payload = `{"ok":true}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
			})}
			b := &Bridge{client: client}
			err := b.ToggleDaemon()
			if state == "idle" {
				if err != nil || !quit {
					t.Fatalf("idle toggle: %v", err)
				}
			} else if err == nil || quit {
				t.Fatal("active capture was interrupted")
			}
			if b.closing {
				t.Fatal("desktop app was marked for shutdown")
			}
		})
	}
}

type desktopTestTransport func(*http.Request) (*http.Response, error)

func (f desktopTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

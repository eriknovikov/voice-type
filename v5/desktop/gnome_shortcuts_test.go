package desktop

import (
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

type memoryShortcutSettings struct {
	entries    []string
	fields     map[string]map[string]string
	listWrites int
	readError  error
	beforeSet  func()
}

func (s *memoryShortcutSettings) paths() ([]string, error) {
	return slices.Clone(s.entries), s.readError
}
func (s *memoryShortcutSettings) setPaths(paths []string) error {
	s.entries = slices.Clone(paths)
	s.listWrites++
	return nil
}
func (s *memoryShortcutSettings) binding(path string) (string, error) {
	return s.fields[path]["binding"], nil
}
func (s *memoryShortcutSettings) set(path, field, value string) error {
	if !ownedShortcutPath(path) {
		return errors.New("attempted to write another app's shortcut")
	}
	if s.beforeSet != nil {
		fn := s.beforeSet
		s.beforeSet = nil
		fn()
	}
	if s.fields[path] == nil {
		s.fields[path] = map[string]string{}
	}
	s.fields[path][field] = value
	return nil
}
func (s *memoryShortcutSettings) reset(path string) error {
	if !ownedShortcutPath(path) {
		return errors.New("attempted to reset another app's shortcut")
	}
	delete(s.fields, path)
	return nil
}

func TestGNOMEShortcutsPreserveOtherSettings(t *testing.T) {
	gradia := "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/be.alexandervanhee.gradia/"
	unusual := "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/User's café/"
	fields := map[string]string{"name": "Gradia screenshot", "command": "flatpak run be.alexandervanhee.gradia --screenshot=INTERACTIVE", "binding": "Print"}
	s := &memoryShortcutSettings{entries: []string{gradia, unusual, shortcutBase + "daemon/"}, fields: map[string]map[string]string{gradia: maps.Clone(fields), unusual: {"name": "other", "command": "other --flag", "binding": "<Super>p"}}}
	prefs := Preferences{Shortcut: "Ctrl+Space"}
	for _, suspended := range []bool{false, false, true, false, false} {
		if err := applyGNOMEShortcutsTo(s, prefs, suspended, "/app/voice-type"); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.fields[gradia], fields) || !slices.Contains(s.entries, gradia) || !slices.Contains(s.entries, unusual) {
			t.Fatal("changed or dropped another shortcut")
		}
	}
	if got := s.fields[shortcutBase+"transcription/"]["command"]; got != strconv.Quote("/app/voice-type")+" --toggle" {
		t.Fatalf("shortcut command: %q", got)
	}
	if s.listWrites != 1 {
		t.Fatalf("startup/capture rewrote the shared list: %d writes", s.listWrites)
	}
	disabled := prefs
	disabled.Shortcut = ""
	if err := applyGNOMEShortcutsTo(s, disabled, false, "/app/voice-type"); err != nil {
		t.Fatal(err)
	}
	if s.fields[shortcutBase+"transcription/"]["binding"] != "" || s.listWrites != 1 {
		t.Fatal("disable changed registrations")
	}
	if err := removeGNOMEShortcutsFrom(s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.entries, []string{gradia, unusual}) || !reflect.DeepEqual(s.fields[gradia], fields) {
		t.Fatal("uninstall changed another shortcut")
	}
}

func TestGNOMEShortcutConflictDoesNotChangeSettings(t *testing.T) {
	path := "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/gradia/"
	s := &memoryShortcutSettings{entries: []string{path}, fields: map[string]map[string]string{path: {"binding": "Print"}}}
	if err := applyGNOMEShortcutsTo(s, Preferences{Shortcut: "Print"}, false, "/app/voice-type"); err == nil {
		t.Fatal("accepted another application's key")
	}
	if len(s.fields) != 1 || s.listWrites != 0 || !slices.Equal(s.entries, []string{path}) {
		t.Fatal("conflict changed settings")
	}
	s.readError = errors.New("read failed")
	if err := applyGNOMEShortcutsTo(s, Preferences{Shortcut: "Ctrl+Space"}, false, "/app/voice-type"); err == nil {
		t.Fatal("ignored read failure")
	}
	if s.listWrites != 0 || len(s.fields) != 1 {
		t.Fatal("read failure changed settings")
	}
	if err := removeGNOMEShortcutsFrom(s); err == nil {
		t.Fatal("uninstall ignored read failure")
	}
}

func TestGNOMEShortcutsMergeFreshRegistrations(t *testing.T) {
	first := "/custom/one/"
	added := "/custom/added-during-registration/"
	s := &memoryShortcutSettings{entries: []string{first}, fields: map[string]map[string]string{}}
	s.beforeSet = func() { s.entries = append(s.entries, added) }
	if err := applyGNOMEShortcutsTo(s, Preferences{Shortcut: "Ctrl+Space"}, false, "/app/voice-type"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.entries, []string{first, added, shortcutBase + "transcription/"}) {
		t.Fatal("overwrote another registration with a stale snapshot")
	}
	s = &memoryShortcutSettings{fields: map[string]map[string]string{}}
	if err := applyGNOMEShortcutsTo(s, Preferences{}, false, "/app/voice-type"); err != nil {
		t.Fatal(err)
	}
	if s.listWrites != 0 {
		t.Fatal("disabled shortcut created a registration")
	}
}

// GLib chooses its backend before its first initialization. Run its real API in
// a fresh process with a keyfile backend, which cannot contact the user's dconf.
func TestGNOMEShortcutsWithRealSettings(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cmd := exec.Command(exe, "-test.run=^TestGNOMEShortcutsKeyfileHelper$")
	cmd.Env = append(os.Environ(), "VOICE_TYPE_SHORTCUT_TEST_ROOT="+root, "GSETTINGS_BACKEND=keyfile", "XDG_CURRENT_DESKTOP=GNOME", "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated settings helper: %v\n%s", err, out)
	}
}

func TestGNOMEShortcutsKeyfileHelper(t *testing.T) {
	root := os.Getenv("VOICE_TYPE_SHORTCUT_TEST_ROOT")
	if root == "" {
		t.Skip("isolated subprocess only")
	}
	if os.Getenv("GSETTINGS_BACKEND") != "keyfile" || os.Getenv("XDG_CONFIG_HOME") != filepath.Join(root, "config") {
		t.Fatal("unsafe test environment")
	}
	gradia := "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/be.alexandervanhee.gradia/"
	schema := customKeySchema + ":" + gradia
	foreign := map[string]string{"name": "Gradia user's screenshot", "command": "flatpak run be.alexandervanhee.gradia --screenshot=INTERACTIVE", "binding": "Print"}
	for field, value := range foreign {
		if out, err := exec.Command("gsettings", "set", schema, field, strconv.Quote(value)).CombinedOutput(); err != nil {
			t.Fatalf("seed Gradia: %v %s", err, out)
		}
	}
	settings := nativeShortcutSettings{}
	if err := settings.setPaths([]string{gradia}); err != nil {
		t.Fatal(err)
	}
	prefs := Preferences{Shortcut: "Ctrl+Space"}
	for i := 0; i < 2; i++ {
		if err := applyGNOMEShortcuts(prefs, false); err != nil {
			t.Fatal(err)
		}
	}
	b := &Bridge{prefs: prefs}
	if err := b.BeginShortcutCapture(); err != nil {
		t.Fatal(err)
	}
	if value, _ := settings.binding(shortcutBase + "transcription/"); value != "" {
		t.Fatal("shortcut active during capture")
	}
	if err := b.EndShortcutCapture(); err != nil {
		t.Fatal(err)
	}
	if value, _ := settings.binding(shortcutBase + "transcription/"); value != "<control>space" {
		t.Fatal("shortcut not restored")
	}
	if err := applyGNOMEShortcuts(Preferences{Shortcut: "Print"}, false); err == nil {
		t.Fatal("accepted conflicting key")
	}
	paths, err := settings.paths()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(paths, []string{gradia, shortcutBase + "transcription/"}) {
		t.Fatalf("Gradia registration lost: %v", paths)
	}
	if err := settings.set(gradia, "binding", ""); err == nil {
		t.Fatal("allowed a write to another application's settings")
	}
	if err := settings.reset(gradia); err == nil {
		t.Fatal("allowed another application's shortcut to be reset")
	}
	if err := RemoveGNOMEShortcuts(); err != nil {
		t.Fatal(err)
	}
	paths, err = settings.paths()
	if err != nil || !slices.Equal(paths, []string{gradia}) {
		t.Fatalf("uninstall lost Gradia: %v %v", paths, err)
	}
	for field, value := range foreign {
		out, err := exec.Command("gsettings", "get", schema, field).Output()
		if err != nil {
			t.Fatal(err)
		}
		reference := customKeySchema + ":/reference/"
		if out, err := exec.Command("gsettings", "set", reference, field, strconv.Quote(value)).CombinedOutput(); err != nil {
			t.Fatalf("reference: %v %s", err, out)
		}
		want, err := exec.Command("gsettings", "get", reference, field).Output()
		if err != nil || string(out) != string(want) {
			t.Fatalf("changed Gradia %s", field)
		}
	}
}

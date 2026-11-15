package voicetype

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const mediaKeysSchema = "org.gnome.settings-daemon.plugins.media-keys"
const customKeySchema = mediaKeysSchema + ".custom-keybinding"
const shortcutBase = "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/voice-type-"

var gnomeShortcutsMu sync.Mutex
var errGNOMESettingsUnavailable = errors.New("GNOME desktop shortcut settings are unavailable")

type shortcutSettings interface {
	paths() ([]string, error)
	setPaths([]string) error
	binding(string) (string, error)
	set(string, string, string) error
	reset(string) error
}

func ownedShortcutPath(path string) bool {
	return path == shortcutBase+"transcription/" || path == shortcutBase+"daemon/"
}

func isGNOME() bool {
	return strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME")
}
func gtkShortcut(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	parts := strings.Split(value, "+")
	mods := ""
	seen := map[string]bool{}
	for _, part := range parts[:len(parts)-1] {
		key := strings.ToLower(part)
		if seen[key] {
			return "", errors.New("duplicate shortcut modifier")
		}
		seen[key] = true
		switch key {
		case "ctrl", "control":
			mods += "<Control>"
		case "alt":
			mods += "<Alt>"
		case "shift":
			mods += "<Shift>"
		case "super", "meta":
			mods += "<Super>"
		default:
			return "", errors.New("unknown shortcut modifier")
		}
	}
	key := parts[len(parts)-1]
	aliases := map[string]string{"Space": "space", "Enter": "Return", "Backspace": "BackSpace", "PageUp": "Page_Up", "PageDown": "Page_Down", "Plus": "plus"}
	if alias, ok := aliases[key]; ok {
		key = alias
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(key) && len([]rune(key)) != 1 {
		return "", errors.New("invalid shortcut key")
	}
	if len([]rune(key)) == 1 {
		key = strings.ToLower(key)
	}
	return canonicalGTKShortcut(mods + key), nil
}
func canonicalGTKShortcut(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "<primary>", "<control>")
	mods := regexp.MustCompile(`<[^>]+>`).FindAllString(value, -1)
	sort.Strings(mods)
	key := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(value, "")
	return strings.Join(mods, "") + key
}

// The shared array is touched only when adding/removing an owned path. Read it
// again immediately before that edit: key-capture and normal launches never
// replace it with a stale snapshot taken before individual settings writes.
func updateOwnedShortcutPaths(settings shortcutSettings, include bool) error {
	paths, err := settings.paths()
	if err != nil {
		return err
	}
	next := make([]string, 0, len(paths)+1)
	found := false
	for _, path := range paths {
		if path == shortcutBase+"daemon/" {
			continue
		}
		if path == shortcutBase+"transcription/" {
			if !include {
				continue
			}
			found = true
		}
		next = append(next, path)
	}
	if include && !found {
		next = append(next, shortcutBase+"transcription/")
	}
	if slices.Equal(paths, next) {
		return nil
	}
	return settings.setPaths(next)
}

func applyGNOMEShortcuts(prefs Preferences, suspended bool) error {
	gnomeShortcutsMu.Lock()
	defer gnomeShortcutsMu.Unlock()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return applyGNOMEShortcutsTo(nativeShortcutSettings{}, prefs, suspended, exe)
}

func applyGNOMEShortcutsTo(settings shortcutSettings, prefs Preferences, suspended bool, exe string) error {
	key, err := gtkShortcut(prefs.Shortcut)
	if err != nil {
		return err
	}
	path := shortcutBase + "transcription/"
	// Temporarily releasing our key never edits registrations or another action.
	if suspended {
		return settings.set(path, "binding", "")
	}
	paths, err := settings.paths()
	if err != nil {
		return err
	}
	if key != "" {
		for _, other := range paths {
			if ownedShortcutPath(other) {
				continue
			}
			binding, err := settings.binding(other)
			if err != nil {
				return err
			}
			if canonicalGTKShortcut(binding) == key {
				return errors.New("that shortcut is already assigned to another desktop action")
			}
		}
	}
	for _, field := range []struct{ name, value string }{
		{"name", "Voice Type: transcription"},
		{"command", strconv.Quote(exe) + " --toggle"},
		{"binding", key},
	} {
		if err := settings.set(path, field.name, field.value); err != nil {
			return err
		}
	}
	// A disabled shortcut need not create a registration. Existing registrations
	// can remain: their empty binding is harmless and avoids a shared-list write.
	registered := key != "" || slices.Contains(paths, path)
	if err := updateOwnedShortcutPaths(settings, registered); err != nil {
		return err
	}
	return settings.reset(shortcutBase + "daemon/")
}

func RemoveGNOMEShortcuts() error {
	gnomeShortcutsMu.Lock()
	defer gnomeShortcutsMu.Unlock()
	err := removeGNOMEShortcutsFrom(nativeShortcutSettings{})
	if errors.Is(err, errGNOMESettingsUnavailable) {
		return nil
	}
	return err
}

func removeGNOMEShortcutsFrom(settings shortcutSettings) error {
	if err := updateOwnedShortcutPaths(settings, false); err != nil {
		return err
	}
	for _, action := range []string{"transcription", "daemon"} {
		if err := settings.reset(shortcutBase + action + "/"); err != nil {
			return err
		}
	}
	return nil
}

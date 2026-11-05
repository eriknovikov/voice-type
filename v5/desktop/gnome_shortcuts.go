package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	vt "github.com/eriknovikov/voice-type"
)

const mediaKeysSchema = "org.gnome.settings-daemon.plugins.media-keys"
const customKeySchema = mediaKeysSchema + ".custom-keybinding"
const shortcutBase = "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/voice-type-"

var shortcutPathPattern = regexp.MustCompile(`['"](/[A-Za-z0-9_/-]+/)['"]`)

func isGNOME() bool {
	return strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "GNOME")
}
func gsettings(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "gsettings", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("could not update desktop shortcuts: %s", strings.TrimSpace(string(data)))
	}
	return strings.TrimSpace(string(data)), nil
}
func variantString(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "\\'") + "'"
}
func shortcutPaths(value string) []string {
	out := []string{}
	for _, match := range shortcutPathPattern.FindAllStringSubmatch(value, -1) {
		out = append(out, match[1])
	}
	return out
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
func writeShortcutPaths(paths []string) error {
	items := make([]string, len(paths))
	for i, path := range paths {
		items[i] = variantString(path)
	}
	_, err := gsettings("set", mediaKeysSchema, "custom-keybindings", "["+strings.Join(items, ",")+"]")
	return err
}

func canonicalGTKShortcut(value string) string {
	value = strings.ToLower(strings.Trim(value, "'\""))
	value = strings.ReplaceAll(value, "<primary>", "<control>")
	mods := regexp.MustCompile(`<[^>]+>`).FindAllString(value, -1)
	sort.Strings(mods)
	key := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(value, "")
	return strings.Join(mods, "") + key
}

// Only our two stable paths are managed. Other user shortcuts are preserved.
func applyGNOMEShortcuts(prefs Preferences, suspended bool) error {
	first, err := gtkShortcut(prefs.Shortcut)
	if err != nil {
		return err
	}
	second, err := gtkShortcut(prefs.DaemonShortcut)
	if err != nil {
		return err
	}
	if first != "" && strings.EqualFold(first, second) {
		return errors.New("choose different keys for the two shortcuts")
	}
	raw, err := gsettings("get", mediaKeysSchema, "custom-keybindings")
	if err != nil {
		return err
	}
	paths := shortcutPaths(raw)
	if !suspended {
		for _, path := range paths {
			if path == shortcutBase+"transcription/" || path == shortcutBase+"daemon/" {
				continue
			}
			value, err := gsettings("get", customKeySchema+":"+path, "binding")
			if err != nil {
				return err
			}
			existing := canonicalGTKShortcut(value)
			if existing != "" && ((first != "" && existing == first) || (second != "" && existing == second)) {
				return errors.New("that shortcut is already assigned to another desktop action")
			}
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, binding := range []struct{ action, name, key string }{{"transcription", "Voice Type: transcription", first}, {"daemon", "Voice Type: daemon", second}} {
		path := shortcutBase + binding.action + "/"
		key := binding.key
		if suspended {
			key = ""
		}
		if _, err := gsettings("set", customKeySchema+":"+path, "name", variantString(binding.name)); err != nil {
			return err
		}
		if _, err := gsettings("set", customKeySchema+":"+path, "command", variantString(strconv.Quote(exe)+" --shortcut-action "+binding.action)); err != nil {
			return err
		}
		if _, err := gsettings("set", customKeySchema+":"+path, "binding", variantString(key)); err != nil {
			return err
		}
		exists := false
		for _, other := range paths {
			if other == path {
				exists = true
			}
		}
		if !exists {
			paths = append(paths, path)
		}
	}
	return writeShortcutPaths(paths)
}

// This command path never creates a Wails window or registers portal shortcuts.
func RunShortcutAction(action string) error {
	b := &Bridge{client: vt.NewManagementClient()}
	switch action {
	case "transcription":
		return b.Toggle()
	case "daemon":
		return b.ToggleDaemon()
	default:
		return errors.New("unknown shortcut action")
	}
}

func RemoveGNOMEShortcuts() error {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return nil
	}
	raw, err := gsettings("get", mediaKeysSchema, "custom-keybindings")
	if err != nil {
		return err
	}
	paths := []string{}
	for _, path := range shortcutPaths(raw) {
		if path == shortcutBase+"transcription/" || path == shortcutBase+"daemon/" {
			_, _ = gsettings("reset-recursively", customKeySchema+":"+path)
			continue
		}
		paths = append(paths, path)
	}
	return writeShortcutPaths(paths)
}

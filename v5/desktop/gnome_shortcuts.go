package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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

// Only our stable transcription path and retired daemon path are managed.
func applyGNOMEShortcuts(prefs Preferences, suspended bool) error {
	first, err := gtkShortcut(prefs.Shortcut)
	if err != nil {
		return err
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
			if existing != "" && first != "" && existing == first {
				return errors.New("that shortcut is already assigned to another desktop action")
			}
		}
	}
	// Migrate away from the old daemon lifecycle binding, preserving other keys.
	legacy := shortcutBase + "daemon/"
	kept := paths[:0]
	for _, path := range paths {
		if path == legacy {
			if _, err := gsettings("reset-recursively", customKeySchema+":"+path); err != nil {
				return err
			}
		} else {
			kept = append(kept, path)
		}
	}
	paths = kept
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	path := shortcutBase + "transcription/"
	key := first
	if suspended {
		key = ""
	}
	for field, value := range map[string]string{
		"name":    "Voice Type: transcription",
		"command": strconv.Quote(exe) + " --shortcut-action transcription",
		"binding": key,
	} {
		if _, err := gsettings("set", customKeySchema+":"+path, field, variantString(value)); err != nil {
			return err
		}
	}
	exists := false
	for _, other := range paths {
		if other == path {
			exists = true
			break
		}
	}
	if !exists {
		paths = append(paths, path)
	}
	return writeShortcutPaths(paths)
}

// This command path never creates a Wails window or registers portal shortcuts.
func RunShortcutAction(action string) error {
	if action != "transcription" {
		return errors.New("unknown shortcut action")
	}
	settings, err := vt.ReadSettings()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 12 * time.Minute, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/toggle", settings.Port), "application/json", nil)
	if err != nil {
		return errors.New("Voice Type is not running; open the app first")
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		// Never print a response body: control responses can include transcripts.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Voice Type could not toggle recording (HTTP %d); open settings for details", response.StatusCode)
	}
	return nil
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

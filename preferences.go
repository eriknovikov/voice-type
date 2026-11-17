package voicetype

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var preferencesFileMu sync.Mutex

func parsePreferences(data []byte) (Preferences, error) {
	return parsePreferencesForPlatform(data, runtime.GOOS)
}

func defaultPreferences(platform string) Preferences {
	shortcut := "Ctrl+Space"
	if platform == "darwin" {
		shortcut = "Ctrl+Alt+Space"
	}
	return Preferences{Shortcut: shortcut, Autostart: true}
}

func parsePreferencesForPlatform(data []byte, platform string) (Preferences, error) {
	prefs := defaultPreferences(platform)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return prefs, errors.New("desktop preferences must be a JSON object")
	}
	for key, dest := range map[string]any{"shortcut": &prefs.Shortcut, "autostart": &prefs.Autostart, "setup_complete": &prefs.SetupComplete, "output_mode": &prefs.OutputMode} {
		if raw, ok := fields[key]; ok {
			if string(raw) == "null" || json.Unmarshal(raw, dest) != nil {
				return prefs, fmt.Errorf("invalid desktop preference: %s", key)
			}
		}
	}
	if _, exists := fields["daemon_shortcut"]; platform == "linux" && !exists && fields["preferences_version"] == nil && prefs.Shortcut == "Ctrl+Alt+Space" {
		prefs.Shortcut = "Ctrl+Space"
	}
	if _, err := gtkShortcut(prefs.Shortcut); err != nil {
		return prefs, errors.New("invalid saved keyboard shortcut")
	}
	if err := validateOutputMode(prefs.OutputMode); err != nil {
		return prefs, err
	}
	return prefs, nil
}

func loadPreferences() (Preferences, string, error) {
	preferencesFileMu.Lock()
	defer preferencesFileMu.Unlock()
	if err := protectPreferencesDirectory(); err != nil {
		return defaultPreferences(runtime.GOOS), "", err
	}
	data, notice, err := ReadPrivateDocument(preferencesPath(), "desktop.json", func(data []byte) error { _, err := parsePreferences(data); return err })
	if err != nil {
		return defaultPreferences(runtime.GOOS), notice, err
	}
	if len(data) == 0 {
		if notice != "" {
			notice = "Preferences were reset."
		}
		return defaultPreferences(runtime.GOOS), notice, nil
	}
	prefs, err := parsePreferences(data)
	if notice != "" {
		notice = "Preferences restored."
	}
	return prefs, notice, err
}

func readPreferences() Preferences { prefs, _, _ := loadPreferences(); return prefs }

func writePreferences(prefs Preferences) error {
	preferencesFileMu.Lock()
	defer preferencesFileMu.Unlock()
	if err := protectPreferencesDirectory(); err != nil {
		return err
	}
	data, _, err := ReadPrivateDocument(preferencesPath(), "desktop.json", func(data []byte) error { _, err := parsePreferences(data); return err })
	if err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &fields); err != nil {
			return err
		}
	}
	for key, value := range map[string]any{"shortcut": prefs.Shortcut, "autostart": prefs.Autostart, "setup_complete": prefs.SetupComplete, "output_mode": prefs.OutputMode, "preferences_version": 1} {
		fields[key], _ = json.Marshal(value)
	}
	data, err = json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	if _, err = parsePreferences(data); err != nil {
		return err
	}
	return WritePrivateDocument(preferencesPath(), "desktop.json", append(data, '\n'))
}

func (b *Bridge) applyAutostart(enabled bool) error {
	return b.loginStartup().setEnabled(enabled)
}

func (b *Bridge) initializePreferences() {
	b.initializePreferencesForPlatform(runtime.GOOS)
}

func (b *Bridge) initializePreferencesForPlatform(platform string) {
	if b.preferencesError != "" {
		b.recordPreferenceError("Preferences could not be loaded.")
		return
	} // Do not change startup settings when preferences are unreadable.
	if b.notice != "" {
		b.engine.RecordDiagnostic("preferences.recovered", "ok", "Desktop preferences were restored or reset after damage.")
	}
	if platform != "darwin" {
		if err := b.applyAutostart(b.prefs.Autostart); err != nil {
			b.preferencesError = "Could not update Start at login: " + err.Error()
			b.recordPreferenceError("Could not register login startup")
			return
		}
	}
	if err := writePreferences(b.prefs); err != nil {
		b.preferencesError = "Could not save desktop preferences: " + err.Error()
		b.recordPreferenceError("Could not save desktop preferences")
	}
}

func (b *Bridge) SavePreferences(prefs Preferences) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.savePreferences(prefs, runtime.GOOS != "darwin" || prefs.Autostart != b.prefs.Autostart)
}

func (b *Bridge) savePreferences(prefs Preferences, updateLogin bool) error {
	if err := validateOutputMode(prefs.OutputMode); err != nil {
		return err
	}
	if _, err := gtkShortcut(prefs.Shortcut); err != nil {
		return err
	}
	old := b.prefs
	login := b.loginStartup().status()
	if updateLogin && login.Error != "" {
		b.preferencesError = "Could not read Start at login: " + login.Error
		return errors.New(b.preferencesError)
	}
	changed := prefs.Shortcut != old.Shortcut || b.shortcutError != ""
	loginTouched := false
	rollback := func() {
		if changed {
			_ = b.releaseShortcuts()
			_ = b.registerShortcuts(old)
		}
		if loginTouched {
			_ = b.applyAutostart(login.Enabled)
		}
	}
	fail := func(err error) error {
		rollback()
		b.preferencesError = err.Error()
		b.recordPreferenceError("Could not save desktop preferences")
		return err
	}
	if changed {
		_ = b.releaseShortcuts()
		if err := b.registerShortcuts(prefs); err != nil {
			b.shortcutError = err.Error()
			return fail(err)
		}
	}
	if updateLogin {
		loginTouched = true
		if err := b.applyAutostart(prefs.Autostart); err != nil {
			return fail(fmt.Errorf("could not update Start at login: %w", err))
		}
	}
	if err := writePreferences(prefs); err != nil {
		return fail(fmt.Errorf("could not save desktop preferences: %w", err))
	}
	b.prefs = prefs
	b.preferencesError = ""
	if changed {
		b.shortcutError = ""
	}
	return nil
}

func (b *Bridge) recordPreferenceError(message string) {
	if b.engine != nil {
		b.engine.RecordDiagnostic("preferences", "error", message)
	} else {
		RecordDiagnostic(b.version, "preferences", "error", message)
	}
}

func protectPreferencesDirectory() error {
	if err := prepareStorageParent(preferencesPath()); err != nil {
		return err
	}
	dir := filepath.Dir(preferencesPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create desktop preferences directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("protect desktop preferences directory: %w", err)
	}
	return nil
}

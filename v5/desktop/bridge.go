package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	vt "github.com/eriknovikov/voice-type"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type Status = vt.ServiceStatus
type Preferences struct {
	Shortcut      string `json:"shortcut"`
	Autostart     bool   `json:"autostart"`
	SetupComplete bool   `json:"setup_complete"`
}
type DesktopInfo struct {
	LastShortcutAt string      `json:"last_shortcut_at"`
	Preferences    Preferences `json:"preferences"`
	TrayAvailable  bool        `json:"tray_available"`
	ShortcutError  string      `json:"shortcut_error"`
	Version        string      `json:"version"`
}
type Bridge struct {
	app               *application.App
	window            *application.WebviewWindow
	service           *vt.Service
	mu                sync.Mutex
	capturingShortcut bool
	prefs             Preferences
	shortcutError     string
	lastShortcutAt    string
	version           string
	meterMu           sync.Mutex
	meterCancel       context.CancelFunc
}

func (b *Bridge) Settings() (vt.Settings, error) { return vt.ReadSettings() }
func (b *Bridge) SaveSettings(update vt.SettingsUpdate) (vt.Settings, error) {
	return b.service.SaveSettings(update)
}
func (b *Bridge) SelectWorkspace(name string) error {
	if err := vt.SelectWorkspace(name); err != nil {
		return err
	}
	b.app.Event.Emit("workspace-changed", name)
	return nil
}
func (b *Bridge) Status() Status                         { return b.service.Status() }
func (b *Bridge) Microphones() ([]vt.AudioSource, error) { return vt.ListAudioSources() }
func (b *Bridge) StartMicrophoneTest(id string) error {
	if err := b.service.StartMicrophoneTest(id); err != nil {
		return err
	}
	b.startLevelStream()
	return nil
}
func (b *Bridge) StopMicrophoneTest() error {
	b.cancelLevelStream()
	b.service.StopMicrophoneTest()
	return nil
}
func (b *Bridge) Toggle() error                        { return b.service.Toggle() }
func (b *Bridge) Logs() ([]vt.DiagnosticRecord, error) { return b.service.Logs() }
func (b *Bridge) ClearLogs() error                     { return b.service.ClearLogs() }
func (b *Bridge) Quit() error {
	status := b.service.Status()
	if status.State != "idle" && status.State != "testing" {
		return errors.New("finish the current dictation before quitting")
	}
	b.app.Quit()
	return nil
}
func preferencesPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "voice-type", "desktop.json")
}
func readPreferences() Preferences {
	prefs := Preferences{Shortcut: "Ctrl+Space"}
	data, err := os.ReadFile(preferencesPath())
	if err == nil {
		_ = json.Unmarshal(data, &prefs)
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) == nil {
			if _, exists := fields["daemon_shortcut"]; !exists && prefs.Shortcut == "Ctrl+Alt+Space" {
				prefs.Shortcut = "Ctrl+Space"
			}
		}
	}
	return prefs
}
func writePreferences(prefs Preferences) error {
	path := preferencesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".desktop-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (b *Bridge) SavePreferences(prefs Preferences) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	old := b.prefs

	changed := prefs.Shortcut != old.Shortcut || b.shortcutError != ""
	if changed {
		_ = b.releaseShortcuts()
		if err := b.registerShortcuts(prefs); err != nil {
			_ = b.releaseShortcuts()
			_ = b.registerShortcuts(old)
			b.shortcutError = err.Error()
			return err
		}
		b.shortcutError = ""
	}

	var err error
	if prefs.Autostart {
		err = b.app.Autostart.EnableWithOptions(application.AutostartOptions{Arguments: []string{"--background"}})
	} else {
		err = b.app.Autostart.Disable()
	}
	if err != nil {
		return err
	}
	if err := writePreferences(prefs); err != nil {
		return err
	}
	b.prefs = prefs
	return nil
}
func (b *Bridge) Info() DesktopInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return DesktopInfo{LastShortcutAt: b.lastShortcutAt, Preferences: b.prefs, TrayAvailable: trayAvailable(), ShortcutError: b.shortcutError, Version: b.version}
}
func (b *Bridge) toggleFromTray() {
	if err := b.Toggle(); err != nil {
		b.app.Event.Emit("operation-error", err.Error())
	}
}

// ExportLogs uses a native save dialog; webview download handling varies by OS.
func (b *Bridge) ExportLogs(query, outcome string) error {
	records, err := b.Logs()
	if err != nil {
		return err
	}
	var data bytes.Buffer
	for _, record := range records {
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		ok := record.Outcome == "ok"
		if outcome == "errors" && ok || outcome == "ok" && !ok {
			continue
		}
		if !strings.Contains(strings.ToLower(string(raw)), strings.ToLower(query)) {
			continue
		}
		data.Write(raw)
		data.WriteByte('\n')
	}
	path, err := b.app.Dialog.SaveFile().SetFilename("voice-type-logs.jsonl").AddFilter("Diagnostic logs", "*.jsonl").AttachToWindow(b.window).PromptForSingleSelection()
	if err != nil || path == "" {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (b *Bridge) activateShortcut() {
	b.mu.Lock()
	if b.capturingShortcut {
		b.mu.Unlock()
		return
	}
	b.lastShortcutAt = time.Now().Format(time.RFC3339)
	b.mu.Unlock()
	b.toggleFromTray()
}

func (b *Bridge) RecoveryRevision() (string, error) {
	return vt.RecoveryRevision()
}
func (b *Bridge) ResetBrokenSettings(revision string) (vt.Settings, error) {
	return vt.ResetBrokenSettings(revision)
}

func (b *Bridge) registerShortcuts(prefs Preferences) error {
	if isGNOME() {
		return applyGNOMEShortcuts(prefs, false)
	}
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return errors.New("configure global shortcuts in your desktop on this Wayland session")
	}

	if prefs.Shortcut != "" {
		if err := b.app.GlobalShortcut.Register(prefs.Shortcut, b.activateShortcut); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bridge) BeginShortcutCapture() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.capturingShortcut = true
	if err := b.releaseShortcuts(); err != nil {
		b.capturingShortcut = false
		return err
	}
	return nil
}
func (b *Bridge) EndShortcutCapture() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.capturingShortcut {
		return nil
	}
	b.capturingShortcut = false
	_ = b.releaseShortcuts()
	err := b.registerShortcuts(b.prefs)
	if err != nil {
		b.shortcutError = err.Error()
	}
	return err
}

func (b *Bridge) releaseShortcuts() error {
	if isGNOME() {
		return applyGNOMEShortcuts(b.prefs, true)
	}
	return b.app.GlobalShortcut.UnregisterAll()
}

func (b *Bridge) cancelLevelStream() {
	b.meterMu.Lock()
	defer b.meterMu.Unlock()
	if b.meterCancel != nil {
		b.meterCancel()
		b.meterCancel = nil
	}
}

// Read the meter at 30 Hz without serialization, sockets or React renders.
func (b *Bridge) startLevelStream() {
	b.meterMu.Lock()
	if b.meterCancel != nil {
		b.meterCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.meterCancel = cancel
	b.meterMu.Unlock()
	go func() {
		defer cancel()
		ticker := time.NewTicker(time.Second / 30)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			status := b.service.Status()
			b.app.Event.Emit("microphone-level", status.Level)
			if status.State != "testing" {
				return
			}
		}
	}()
}

package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
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
	LastShortcutAt   string      `json:"last_shortcut_at"`
	Preferences      Preferences `json:"preferences"`
	TrayAvailable    bool        `json:"tray_available"`
	ShortcutError    string      `json:"shortcut_error"`
	Version          string      `json:"version"`
	PreferencesError string      `json:"preferences_error"`
	Notice           string      `json:"notice"`
}
type Bridge struct {
	app                 *application.App
	window              *application.WebviewWindow
	service             *vt.Service
	mu                  sync.Mutex
	capturingShortcut   bool
	prefs               Preferences
	shortcutError       string
	lastShortcutAt      string
	version             string
	preferencesError    string
	notice              string
	frontendReady       bool
	pendingWindowAction string
	meterMu             sync.Mutex
	meterCancel         context.CancelFunc
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
func (b *Bridge) UpsertProfile(update vt.ProfileUpdate) (vt.Settings, error) {
	value, err := vt.UpsertProfile(update)
	if err == nil {
		b.service.RefreshSettings()
	}
	return value, err
}
func (b *Bridge) DeleteProfile(revision, id string) (vt.Settings, error) {
	value, err := vt.DeleteProfile(revision, id)
	if err == nil {
		b.service.RefreshSettings()
	}
	return value, err
}
func (b *Bridge) SelectProfile(revision, id string) (vt.Settings, error) {
	value, err := vt.SelectProfile(revision, id)
	if err == nil {
		b.service.RefreshSettings()
	}
	return value, err
}
func (b *Bridge) RevealProfileKey(id string) (string, error) { return vt.RevealProfileKey(id) }
func (b *Bridge) CopyProfileKey(id string) error {
	key, err := vt.RevealProfileKey(id)
	if err != nil {
		return err
	}
	if !b.app.Clipboard.SetText(key) {
		return errors.New("could not copy the API key")
	}
	return nil
}
func (b *Bridge) Status() Status                         { return b.service.Status() }
func (b *Bridge) Microphones() ([]vt.AudioSource, error) { return b.service.Microphones() }
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

// Window actions wait for the frontend's serialized autosave queue. External
// signals still shut down directly; a failed save keeps settings open.
func (b *Bridge) RequestHide() { b.requestWindowAction("hide") }
func (b *Bridge) RequestQuit() { b.requestWindowAction("quit") }
func (b *Bridge) Quit() error  { b.RequestQuit(); return nil }
func (b *Bridge) requestWindowAction(action string) {
	b.mu.Lock()
	b.pendingWindowAction = action
	ready := b.frontendReady
	b.mu.Unlock()
	if b.window != nil {
		b.window.Show()
		b.window.Focus()
	}
	if ready {
		b.app.Event.Emit("flush-settings", map[string]string{"action": action})
	}
}
func (b *Bridge) FrontendReady() {
	b.mu.Lock()
	b.frontendReady = true
	action := b.pendingWindowAction
	b.mu.Unlock()
	if action != "" {
		b.app.Event.Emit("flush-settings", map[string]string{"action": action})
	}
}
func (b *Bridge) CompleteWindowAction(action string) error {
	b.mu.Lock()
	if action != b.pendingWindowAction || (action != "hide" && action != "quit") {
		b.mu.Unlock()
		return errors.New("window action is no longer pending")
	}
	b.pendingWindowAction = ""
	b.mu.Unlock()
	if action == "quit" {
		status := b.service.Status()
		if status.State != "idle" && status.State != "testing" {
			return errors.New("finish the current dictation before quitting")
		}
		b.app.Quit()
	} else {
		_ = b.StopMicrophoneTest()
		_ = b.EndShortcutCapture()
		b.app.Event.Emit("cancel-shortcut-capture")
		b.window.Hide()
	}
	return nil
}
func (b *Bridge) Info() DesktopInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	return DesktopInfo{LastShortcutAt: b.lastShortcutAt, Preferences: b.prefs, TrayAvailable: trayAvailable(), ShortcutError: b.shortcutError, Version: b.version, PreferencesError: b.preferencesError, Notice: b.notice}
}

// Tray and forwarded shortcuts share one nonblocking recording action.
func (b *Bridge) toggleRecording() {
	result := b.service.ToggleAsync()
	go func() {
		if err := <-result; err != nil {
			b.app.Event.Emit("operation-error", err.Error())
		}
	}()
}
func (b *Bridge) dispatchCommand(command Command) {
	switch command {
	case CommandShow:
		b.window.Show()
		b.window.Focus()
	case CommandToggle:
		b.activateShortcut()
	case CommandQuit:
		go b.RequestQuit()
	}
}

// ExportLogs uses a native save dialog; webview download handling varies by OS.
func (b *Bridge) ExportLogs(outcome string) error {
	records, err := b.Logs()
	if err != nil {
		return err
	}
	data, err := encodeDiagnosticLogs(records, outcome)
	if err != nil {
		return err
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
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func encodeDiagnosticLogs(records []vt.DiagnosticRecord, outcome string) ([]byte, error) {
	if outcome != "all" && outcome != "errors" && outcome != "ok" {
		return nil, errors.New("choose a valid log outcome")
	}
	var data bytes.Buffer
	for _, record := range records {
		ok := record.Outcome == "ok"
		if outcome == "errors" && ok || outcome == "ok" && !ok {
			continue
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		data.Write(raw)
		data.WriteByte('\n')
	}
	return data.Bytes(), nil
}

func (b *Bridge) activateShortcut() {
	// Preferences may wait for GTK while replacing native bindings. A shortcut
	// must not wait on that lock from GTK's thread or replay after the edit.
	if !b.mu.TryLock() {
		return
	}
	if b.capturingShortcut {
		b.mu.Unlock()
		return
	}
	b.lastShortcutAt = time.Now().Format(time.RFC3339)
	b.mu.Unlock()
	b.toggleRecording()
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

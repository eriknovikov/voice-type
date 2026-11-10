package voicetype

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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
	copyText            func(string) bool
	window              *application.WebviewWindow
	engine              *engine
	mu                  sync.Mutex
	capturingShortcut   atomic.Bool
	prefs               Preferences
	shortcutError       string
	lastShortcutAt      atomic.Int64
	version             string
	preferencesError    string
	notice              string
	frontendReady       bool
	pendingWindowAction string
	meterMu             sync.Mutex
	meterCancel         context.CancelFunc
}

func (b *Bridge) Settings() (Settings, error) { return ReadSettings() }
func (b *Bridge) SaveSettings(update SettingsUpdate) (Settings, error) {
	return b.engine.SaveSettings(update)
}
func (b *Bridge) SelectWorkspace(name string) error {
	if err := SelectWorkspace(name); err != nil {
		return err
	}
	b.app.Event.Emit("workspace-changed", name)
	return nil
}
func (b *Bridge) UpsertProfile(update ProfileUpdate) (Settings, error) {
	value, err := UpsertProfile(update)
	if err == nil {
		b.engine.RefreshSettings()
	}
	return value, err
}
func (b *Bridge) DeleteProfile(revision, id string) (Settings, error) {
	value, err := DeleteProfile(revision, id)
	if err == nil {
		b.engine.RefreshSettings()
	}
	return value, err
}
func (b *Bridge) SelectProfile(revision, id string) (Settings, error) {
	value, err := SelectProfile(revision, id)
	if err == nil {
		b.engine.RefreshSettings()
	}
	return value, err
}
func (b *Bridge) SaveAPIKey(provider, key string) (Settings, error) {
	if err := SaveAPIKey(provider, key); err != nil {
		return Settings{}, err
	}
	b.engine.RefreshSettings()
	return ReadSettings()
}
func (b *Bridge) DeleteAPIKey(provider string) (Settings, error) {
	if err := DeleteAPIKey(provider); err != nil {
		return Settings{}, err
	}
	b.engine.RefreshSettings()
	return ReadSettings()
}
func (b *Bridge) CopyAPIKey(provider string) error {
	key, saved, err := providerAPIKey(provider)
	if err != nil {
		return err
	}
	if !saved {
		return fmt.Errorf("no %s API key is saved", providerLabel(provider))
	}
	copyText := b.copyText
	if copyText == nil {
		copyText = b.app.Clipboard.SetText
	}
	if !copyText(key) {
		return errors.New("could not copy the API key")
	}
	return nil
}
func (b *Bridge) Status() Status                      { return b.engine.Status() }
func (b *Bridge) Microphones() ([]AudioSource, error) { return b.engine.Microphones() }
func (b *Bridge) LocalModels() ([]LocalModel, error)  { return b.engine.LocalModels() }
func (b *Bridge) InstallLocalModel(id string) error   { return b.engine.InstallLocalModel(id) }
func (b *Bridge) ResumeLocalModel(id string) error    { return b.engine.ResumeLocalModel(id) }
func (b *Bridge) RetryLocalModel(id string) error     { return b.engine.RetryLocalModel(id) }
func (b *Bridge) PauseLocalModel(id string) error     { return b.engine.PauseLocalModel(id) }
func (b *Bridge) CancelLocalModel(id string) error    { return b.engine.CancelLocalModel(id) }
func (b *Bridge) RemoveLocalModel(id string) error    { return b.engine.RemoveLocalModel(id) }
func (b *Bridge) StartMicrophoneTest(id string) error {
	if err := b.engine.StartMicrophoneTest(id); err != nil {
		return err
	}
	b.startLevelStream()
	return nil
}
func (b *Bridge) StopMicrophoneTest() error {
	b.cancelLevelStream()
	b.engine.StopMicrophoneTest()
	return nil
}
func (b *Bridge) Toggle() error                     { return b.engine.Toggle() }
func (b *Bridge) Logs() ([]DiagnosticRecord, error) { return b.engine.Logs() }
func (b *Bridge) ClearLogs() error                  { return b.engine.ClearLogs() }

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
		status := b.engine.Status()
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
	info := DesktopInfo{Preferences: b.prefs, ShortcutError: b.shortcutError, Version: b.version, PreferencesError: b.preferencesError, Notice: b.notice}
	b.mu.Unlock()
	// A slow desktop bus must not hold up preference saves or shortcut handling.
	info.TrayAvailable = trayAvailable()
	if last := b.lastShortcutAt.Load(); last != 0 {
		info.LastShortcutAt = time.Unix(0, last).Format(time.RFC3339)
	}
	return info
}

// Tray and forwarded shortcuts share one nonblocking recording action.
func (b *Bridge) toggleRecording() {
	result := b.engine.ToggleAsync()
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

func encodeDiagnosticLogs(records []DiagnosticRecord, outcome string) ([]byte, error) {
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
	// Native preference registration can wait for GTK. Shortcut activation
	// neither waits on that lock nor drops an ordinary keypress because of it.
	if b.capturingShortcut.Load() {
		return
	}
	b.lastShortcutAt.Store(time.Now().UnixNano())
	b.toggleRecording()
}

func (b *Bridge) RecoveryRevision() (string, error) {
	return RecoveryRevision()
}
func (b *Bridge) ResetBrokenSettings(revision string) (Settings, error) {
	return ResetBrokenSettings(revision)
}

func (b *Bridge) registerShortcuts(prefs Preferences) error {
	if developmentMode() {
		// Development runs must not replace the installed app's shortcut.
		return nil
	}
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
	b.capturingShortcut.Store(true)
	if err := b.releaseShortcuts(); err != nil {
		b.capturingShortcut.Store(false)
		return err
	}
	return nil
}
func (b *Bridge) EndShortcutCapture() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.capturingShortcut.Load() {
		return nil
	}
	b.capturingShortcut.Store(false)
	_ = b.releaseShortcuts()
	err := b.registerShortcuts(b.prefs)
	if err != nil {
		b.shortcutError = err.Error()
	}
	return err
}

func (b *Bridge) releaseShortcuts() error {
	if developmentMode() {
		// No shortcut was registered by a development run.
		return nil
	}
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
			status := b.engine.Status()
			b.app.Event.Emit("microphone-level", status.Level)
			if status.State != "testing" {
				return
			}
		}
	}()
}

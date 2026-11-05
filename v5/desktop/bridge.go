package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	vt "github.com/eriknovikov/voice-type"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type Status struct {
	LastAttemptFailed bool    `json:"last_attempt_failed"`
	State             string  `json:"state"`
	Error             string  `json:"error"`
	Port              int     `json:"port"`
	Level             float64 `json:"level"`
}
type Preferences struct {
	Shortcut       string `json:"shortcut"`
	DaemonShortcut string `json:"daemon_shortcut"`
	Autostart      bool   `json:"autostart"`
	SetupComplete  bool   `json:"setup_complete"`
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
	client            *http.Client
	mu                sync.Mutex
	connectMu         sync.Mutex
	closing           bool
	capturingShortcut bool
	prefs             Preferences
	shortcutError     string
	lastShortcutAt    string
	version           string
	meterMu           sync.Mutex
	meterCancel       context.CancelFunc
}

func (b *Bridge) request(method, path string, body, out any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://desktop"+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if path == "/toggle" {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 12*time.Minute)
	}
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := b.client.Do(req)
	if err != nil {
		return errors.New("cannot connect to Voice Type; reconnect or restart the daemon")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var failure struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&failure) == nil && failure.Error != "" {
			return errors.New(failure.Error)
		}
		return fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 12<<20)).Decode(out)
}
func (b *Bridge) Settings() (vt.Settings, error) {
	var out vt.Settings
	err := b.request("GET", "/settings", nil, &out)
	return out, err
}
func (b *Bridge) SaveSettings(update vt.SettingsUpdate) (vt.Settings, error) {
	var out vt.Settings
	err := b.request("PUT", "/settings", update, &out)
	return out, err
}
func (b *Bridge) SelectWorkspace(name string) error {
	if err := b.request("PUT", "/workspace", map[string]string{"name": name}, nil); err != nil {
		return err
	}
	b.app.Event.Emit("workspace-changed", name)
	return nil
}
func (b *Bridge) Status() (Status, error) {
	var out Status
	err := b.request("GET", "/status", nil, &out)
	return out, err
}
func (b *Bridge) Microphones() ([]vt.AudioSource, error) {
	var out []vt.AudioSource
	err := b.request("GET", "/microphones", nil, &out)
	return out, err
}
func (b *Bridge) StartMicrophoneTest(id string) error {
	if err := b.request("POST", "/microphone-test", map[string]string{"id": id}, nil); err != nil {
		return err
	}
	b.startLevelStream()
	return nil
}
func (b *Bridge) StopMicrophoneTest() error {
	b.cancelLevelStream()
	return b.request("DELETE", "/microphone-test", nil, nil)
}
func (b *Bridge) Toggle() error { return b.request("POST", "/toggle", nil, nil) }
func (b *Bridge) Logs() ([]map[string]any, error) {
	var out []map[string]any
	err := b.request("GET", "/logs", nil, &out)
	return out, err
}
func (b *Bridge) ClearLogs() error { return b.request("DELETE", "/logs", nil, nil) }
func (b *Bridge) Connect() error {
	b.connectMu.Lock()
	defer b.connectMu.Unlock()
	if b.closing {
		return errors.New("Voice Type is shutting down")
	}
	if _, err := b.Status(); err == nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binary := filepath.Join(filepath.Dir(executable), "voice-type")
	if _, err := os.Stat(binary); err != nil {
		binary, err = exec.LookPath("voice-type")
		if err != nil {
			return errors.New("voice-type daemon is not installed alongside the app")
		}
	}
	// Structured diagnostics live in the daemon's bounded log; do not keep a
	// second unbounded stderr file, which may contain provider response bodies.
	command := exec.Command(binary, "start")
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	for i := 0; i < 50; i++ {
		if _, err := b.Status(); err == nil {
			return nil
		}
		select {
		case <-done:
			return errors.New("daemon did not start; an older version may already be using its port")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("daemon startup timed out")
}
func (b *Bridge) Restart() error {
	status, err := b.Status()
	if err != nil {
		return b.Connect()
	}
	if status.State != "idle" {
		return errors.New("finish recording or microphone testing before restarting")
	}
	if err := b.request("POST", "/quit", nil, nil); err != nil {
		return err
	}
	for i := 0; i < 50; i++ {
		if _, err := b.Status(); err != nil {
			return b.Connect()
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("daemon is still shutting down")
}
func (b *Bridge) Quit() error {
	b.connectMu.Lock()
	defer b.connectMu.Unlock()
	status, err := b.Status()
	if err == nil && status.State != "idle" && status.State != "testing" {
		return errors.New("finish the current dictation before quitting")
	}
	if err == nil {
		if err = b.request("POST", "/quit", nil, nil); err != nil {
			return err
		}
	}
	b.closing = true
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
	prefs := Preferences{Shortcut: "Ctrl+Space", DaemonShortcut: "Ctrl+Alt+Space"}
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

	changed := prefs.Shortcut != old.Shortcut || prefs.DaemonShortcut != old.DaemonShortcut || b.shortcutError != ""
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
		ok := record["outcome"] == "ok"
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
	var revision string
	err := b.request("GET", "/recovery", nil, &revision)
	return revision, err
}
func (b *Bridge) ResetBrokenSettings(revision string) (vt.Settings, error) {
	var settings vt.Settings
	err := b.request("POST", "/recovery", map[string]string{"revision": revision}, &settings)
	return settings, err
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
	if prefs.DaemonShortcut == "" {
		return nil
	}
	return b.app.GlobalShortcut.Register(prefs.DaemonShortcut, func() {
		b.mu.Lock()
		capturing := b.capturingShortcut
		b.mu.Unlock()
		if capturing {
			return
		}
		if err := b.ToggleDaemon(); err != nil {
			b.app.Event.Emit("operation-error", err.Error())
		}
	})
}

// The desktop app stays alive so its shortcut can start the daemon again.
func (b *Bridge) ToggleDaemon() error {
	b.connectMu.Lock()
	status, err := b.Status()
	if err != nil {
		b.connectMu.Unlock()
		return b.Connect()
	}
	defer b.connectMu.Unlock()
	if status.State != "idle" {
		return errors.New("stop recording or microphone testing before stopping the daemon")
	}
	if err := b.request("POST", "/quit", nil, nil); err != nil {
		return err
	}
	for i := 0; i < 50; i++ {
		if _, err := b.Status(); err != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("daemon is still shutting down")
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
		req, err := http.NewRequestWithContext(ctx, "GET", "http://desktop/microphone-levels", nil)
		if err != nil {
			return
		}
		resp, err := b.client.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				b.app.Event.Emit("operation-error", "Microphone meter disconnected.")
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b.app.Event.Emit("operation-error", "Microphone meter unavailable. Restart the daemon and try again.")
			return
		}
		decoder := json.NewDecoder(resp.Body)
		for {
			var frame struct {
				Level   float64 `json:"level"`
				Testing bool    `json:"testing"`
			}
			if err := decoder.Decode(&frame); err != nil {
				if ctx.Err() == nil && err != io.EOF {
					b.app.Event.Emit("operation-error", "Microphone meter disconnected.")
				}
				return
			}
			if ctx.Err() != nil {
				return
			}
			b.app.Event.Emit("microphone-level", frame.Level)
			if !frame.Testing {
				return
			}
		}
	}()
}

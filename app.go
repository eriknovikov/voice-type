package voicetype

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"math"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed frontend/dist
var assets embed.FS

//go:embed icon.png
var appIcon []byte

func trayAvailable() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var owner bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&owner)
	return err == nil && owner
}
func trayIcon(state string) []byte {
	canvas := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	c := color.RGBA{145, 150, 158, 255}
	if state == "disconnected" || state == "setup needed" {
		c = color.RGBA{145, 150, 158, 255}
	}
	if state == "error" {
		c = color.RGBA{245, 110, 106, 255}
	}
	if state == "recording" {
		c = color.RGBA{116, 195, 170, 255}
	} else if state == "transcribing" || state == "checking" || state == "warning" {
		c = color.RGBA{237, 188, 105, 255}
	}
	// Use the launcher-like five-bar mark with rounded capsule ends.
	for i, h := range []float64{7, 16, 24, 16, 7} {
		centerX := float64(4 + i*6)
		for y := 0; y < 32; y++ {
			dy := math.Max(math.Abs(float64(y)+0.5-16)-(h/2-2), 0)
			for x := 0; x < 32; x++ {
				dx := math.Abs(float64(x) + 0.5 - centerX)
				distance := math.Hypot(dx, dy) - 2
				coverage := math.Max(0, math.Min(1, 0.5-distance))
				if coverage > 0 {
					canvas.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8(coverage * float64(c.A))})
				}
			}
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, canvas)
	return out.Bytes()
}
func Run(version string, command Command) error {
	appID, appName := applicationIdentity()
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	b := &Bridge{version: version}
	router := &commandRouter{}
	// Never wait for GTK while holding the router lock: shutdown also runs on
	// GTK's thread. Queued callbacks recheck shutdown before touching the bridge.
	router.dispatch = func(command Command) {
		application.InvokeAsync(func() {
			if router.active() {
				b.dispatchCommand(command)
			}
		})
	}
	defer router.close()
	done := make(chan struct{})
	app := application.New(application.Options{
		Name: appName, Icon: appIcon, Description: "System-wide voice dictation",
		Linux:    application.LinuxOptions{ApplicationID: appID},
		Services: []application.Service{application.NewService(b)},
		Assets:   application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: appID, OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
			if len(data.Args) == 0 {
				return
			}
			command, err := ParseCommand(data.Args[1:])
			if err == nil && command != CommandRemoveShortcuts && command != CommandHelp && command != CommandVersion && command != CommandDoctor && command != CommandUpdate {
				router.submit(command)
			}
		}},
		ErrorHandler: func(err error) {
			if strings.Contains(err.Error(), "shortcut") {
				b.mu.Lock()
				b.shortcutError = err.Error()
				b.mu.Unlock()
			}
		},
		OnShutdown: func() {
			router.close()
			close(done)
			b.cancelLevelStream()
			if b.engine != nil {
				b.engine.Close()
			}
		},
	})
	b.app = app
	// Secondary launches exit inside application.New, before any preferences,
	// credentials, shortcuts or engine resources are touched here.
	if command == CommandQuit {
		return nil
	}
	prefs, notice, preferencesErr := loadPreferences()
	b.prefs, b.notice = prefs, notice
	if developmentMode() {
		devNotice := "Development build: desktop shortcuts and Start at login are disabled."
		if b.notice != "" {
			b.notice += " "
		}
		b.notice += devNotice
	}
	if preferencesErr != nil {
		b.preferencesError = preferencesErr.Error()
	}
	b.engine, err = startEngine(version)
	if err != nil {
		router.close()
		startupErr := err
		log.Printf("Voice Type startup failed: %v", startupErr)
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			go func() {
				dialog := app.Dialog.Error().SetTitle("Voice Type could not start").SetMessage(startupErr.Error())
				dialog.AddButton("Close").SetAsDefault().SetAsCancel().OnClick(func() { app.Quit() })
				dialog.Show()
			}()
		})
		if err := app.Run(); err != nil {
			return err
		}
		return startupErr
	}
	defer b.engine.Close()
	if b.notice != "" {
		b.engine.RecordDiagnostic("preferences.recovered", "ok", "Desktop preferences were recovered")
	}
	if preferencesErr != nil {
		b.engine.RecordDiagnostic("preferences.invalid", "error", "Could not read desktop preferences; check file permissions")
	}
	recordingReady := b.engine.RecordingReady()
	if recordingReady && !b.prefs.SetupComplete {
		// A configured install may predate the settings save that normally sets
		// this marker. Keep it truthful so it never blocks a hotkey launch.
		b.prefs.SetupComplete = true
	}
	b.initializePreferences()
	hidden := startupWindowHidden(command, recordingReady, b.trayStatus())
	// Without a tray icon settings is the only discoverable way to finish
	// setup, so an unusable hotkey launch falls back to opening it.
	initial := command
	if command == CommandToggle && !recordingReady && !hidden {
		initial = CommandShow
	}
	router.submit(initial)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		router.start(recordingReady)
		b.engine.RecordDiagnostic("desktop.started", "ok", "Voice Type started.")
	})
	// The settings window is created on first use for background and toggle
	// launches. A hidden start then keeps WebKit and its helper processes out of
	// memory until the user actually opens settings.
	b.newWindow = func() *application.WebviewWindow {
		window := app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Voice Type", Frameless: true, Width: 1040, Height: 740, MinWidth: 760, MinHeight: 560, URL: "/"})
		window.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) { b.setWindowVisible(true) })
		window.OnWindowEvent(events.Common.WindowHide, func(*application.WindowEvent) { b.setWindowVisible(false) })
		window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			e.Cancel()
			go b.RequestQuit()
		})
		return window
	}
	if !hidden {
		b.setWindow(b.newWindow())
	}
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon("idle"))
	tray.SetTooltip("Voice Type")
	show := func() {
		if window := b.ensureWindow(); window != nil {
			window.Show()
			window.Focus()
			b.setWindowVisible(true)
		}
	}
	tray.OnClick(show)
	// Keep action IDs stable across state changes. A tray host can deliver a
	// click from a layout it fetched just before a status update.
	menu := app.NewMenu()
	statusItem := menu.Add("Idle").SetEnabled(false)
	actionItem := menu.Add("Record").OnClick(func(_ *application.Context) { b.toggleRecording() })
	menu.Add("Settings").OnClick(func(_ *application.Context) { show() })
	menu.Add("Quit").OnClick(func(_ *application.Context) { go b.RequestQuit() })
	tray.SetMenu(menu)
	updateMenu := func(state string) {
		statusLabel, action := "Idle", "Record"
		switch state {
		case "warning":
			statusLabel = "No speech detected"
		case "checking":
			statusLabel, action = "Preparing", "Cancel"
		case "recording":
			statusLabel, action = "Recording", "Stop"
		case "transcribing":
			statusLabel, action = "Transcribing", "Transcribing"
		case "testing":
			statusLabel, action = "Testing", "Testing"
		}
		statusItem.SetLabel(statusLabel)
		actionItem.SetLabel(action).SetEnabled(state != "transcribing" && state != "testing")
		tray.SetIcon(trayIcon(state))
		tooltip := "Voice Type · " + statusLabel
		if state == "error" {
			tooltip = "Voice Type · Dictation failed"
		}
		tray.SetTooltip(tooltip)
	}
	updateMenu("idle")

	if err := b.registerShortcuts(b.prefs); err != nil {
		b.shortcutError = err.Error()
	}
	go func() {
		last := ""
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			state := trayState(b.Status())
			if state != last {
				last = state
				application.InvokeSync(func() { updateMenu(state) })
			}
		}
	}()
	return app.Run()
}

// startupWindowHidden reports whether a launch starts without opening settings.
// Background and hotkey launches stay out of the way: only an explicit settings
// request (the tray, or no arguments) may show the window. Without a tray icon,
// an unusable hotkey launch falls back to settings so setup stays reachable.
func startupWindowHidden(command Command, recordingReady, trayAvailable bool) bool {
	switch command {
	case CommandBackground, CommandToggle:
		return recordingReady || trayAvailable
	}
	return false
}

func trayState(status Status) string {
	if status.State == "checking" || status.State == "recording" || status.State == "transcribing" {
		return status.State
	}
	if status.LastAttemptFailed {
		return "error"
	}
	if status.State == "idle" && status.Error != "" {
		return "setup needed"
	}
	if status.State == "idle" && status.Warning != "" {
		return "warning"
	}
	return status.State
}

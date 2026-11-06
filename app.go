package voicetype

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"log"
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
	canvas := image.NewRGBA(image.Rect(0, 0, 32, 32))
	c := color.RGBA{145, 150, 158, 255}
	if state == "disconnected" || state == "setup needed" {
		c = color.RGBA{145, 150, 158, 255}
	}
	if state == "error" {
		c = color.RGBA{245, 110, 106, 255}
	}
	if state == "recording" {
		c = color.RGBA{116, 195, 170, 255}
	} else if state == "transcribing" {
		c = color.RGBA{237, 188, 105, 255}
	}
	// Align bars and gaps to whole pixels at the panel's 16px icon size.
	for i, h := range []int{7, 16, 24, 16, 7} {
		x := 2 + i*6
		draw.Draw(canvas, image.Rect(x, (32-h)/2, x+4, (32+h)/2), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	var out bytes.Buffer
	_ = png.Encode(&out, canvas)
	return out.Bytes()
}
func Run(version string, command Command) error {
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
		Name: "Voice Type", Icon: appIcon, Description: "System-wide voice dictation",
		Linux:    application.LinuxOptions{ApplicationID: "io.github.eriknovikov.voice-type"},
		Services: []application.Service{application.NewService(b)},
		Assets:   application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "io.github.eriknovikov.voice-type", OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
			if len(data.Args) == 0 {
				return
			}
			command, err := ParseCommand(data.Args[1:])
			if err == nil && command != CommandRemoveShortcuts && command != CommandHelp && command != CommandVersion {
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
	b.initializePreferences()
	startupReady := b.prefs.SetupComplete && b.Status().Error == "" && b.preferencesError == "" && b.notice == "" && ConfigNotice() == "" && b.Status().Notice == ""
	hidden := (command == CommandBackground || command == CommandToggle) && startupReady
	// Recovery/setup feedback takes precedence over an automatic first recording.
	initial := command
	if command == CommandToggle && !startupReady {
		initial = CommandShow
	}
	router.submit(initial)
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		router.start(startupReady)
		b.engine.RecordDiagnostic("desktop.started", "ok", "Voice Type started.")
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Voice Type", Frameless: true, Width: 1040, Height: 740, MinWidth: 760, MinHeight: 560, URL: "/", Hidden: hidden})
	b.window = window
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		go b.RequestHide()
	})
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon("idle"))
	tray.SetTooltip("Voice Type")
	show := func() { window.Show(); window.Focus() }
	tray.OnClick(show)
	updateMenu := func(state string) {
		menu := app.NewMenu()
		statusLabel, action := "Idle", "Start recording"
		if state == "recording" {
			statusLabel, action = "Recording", "Stop recording"
		}
		menu.Add(statusLabel).SetEnabled(false)
		menu.Add(action).OnClick(func(_ *application.Context) { b.toggleRecording() })
		menu.Add("Open settings").OnClick(func(_ *application.Context) { show() })
		menu.Add("Quit").OnClick(func(_ *application.Context) { go b.RequestQuit() })
		tray.SetMenu(menu)
		tray.SetIcon(trayIcon(state))
		tooltip := "Voice Type · " + statusLabel
		if state == "error" {
			tooltip = "Voice Type · Dictation failed — open settings to view logs"
		}
		tray.SetTooltip(tooltip)
	}
	updateMenu("idle")

	if err := b.registerShortcuts(b.prefs); err != nil {
		b.shortcutError = err.Error()
	}
	go func() {
		last := ""
		ticker := time.NewTicker(time.Second)
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

func trayState(status Status) string {
	if status.State == "recording" || status.State == "transcribing" {
		return status.State
	}
	if status.LastAttemptFailed {
		return "error"
	}
	if status.State == "idle" && status.Error != "" {
		return "setup needed"
	}
	return status.State
}

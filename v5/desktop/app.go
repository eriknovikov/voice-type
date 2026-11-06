package desktop

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/fs"
	"os"
	"strings"
	"time"

	vt "github.com/eriknovikov/voice-type"
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
	for i, h := range []int{10, 22, 16, 26, 10} {
		x := 3 + i*6
		draw.Draw(canvas, image.Rect(x, (32-h)/2, x+3, (32+h)/2), &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	var out bytes.Buffer
	_ = png.Encode(&out, canvas)
	return out.Bytes()
}
func Run(version string) error {
	frontend, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	prefs, notice, preferencesErr := loadPreferences()
	b := &Bridge{prefs: prefs, version: version, notice: notice}
	if preferencesErr != nil {
		b.preferencesError = preferencesErr.Error()
	}
	done := make(chan struct{})
	app := application.New(application.Options{
		Name: "Voice Type", Icon: appIcon, Description: "System-wide voice dictation",
		Linux:    application.LinuxOptions{ApplicationID: "io.github.eriknovikov.voice-type"},
		Services: []application.Service{application.NewService(b)},
		Assets:   application.AssetOptions{Handler: application.BundledAssetFileServer(frontend)},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "io.github.eriknovikov.voice-type", OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
			if b.window != nil {
				b.window.Show()
				b.window.Focus()
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
			close(done)
			b.cancelLevelStream()
			if b.service != nil {
				b.service.Close()
			}
		},
	})
	b.app = app
	// application.New has already handed secondary launches to the first instance.
	b.service, err = vt.StartService(version)
	if err != nil {
		startupErr := err
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
	defer b.service.Close()
	if b.notice != "" {
		b.service.RecordDiagnostic("preferences.recovered", "ok", "Desktop preferences were recovered")
	}
	if preferencesErr != nil {
		b.service.RecordDiagnostic("preferences.invalid", "error", "Could not read desktop preferences; check file permissions")
	}
	b.initializePreferences()
	hidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--background" && b.prefs.SetupComplete && b.Status().Error == "" && b.preferencesError == "" && b.notice == "" && vt.ConfigNotice() == "" && b.Status().Notice == "" {
			hidden = true
		}
	}
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "Voice Type", Frameless: true, Width: 1040, Height: 740, MinWidth: 760, MinHeight: 560, URL: "/", Hidden: hidden})
	b.window = window
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		b.RequestHide()
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
		menu.Add(action).OnClick(func(_ *application.Context) { go b.toggleFromTray() })
		menu.Add("Open settings").OnClick(func(_ *application.Context) { show() })
		menu.Add("Quit").OnClick(func(_ *application.Context) { b.RequestQuit() })
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
			case <-b.service.Done():
				app.Quit()
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

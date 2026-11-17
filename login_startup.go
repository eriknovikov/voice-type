package voicetype

import (
	"errors"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type LoginStartupStatus struct {
	Enabled bool   `json:"enabled"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

type loginStartupControl interface {
	status() LoginStartupStatus
	setEnabled(bool) error
}

type systemLoginStartup struct{ app *application.App }

func (s systemLoginStartup) status() LoginStartupStatus {
	if developmentMode() {
		return LoginStartupStatus{State: "development"}
	}
	return platformLoginStartup(s.app)
}

func (s systemLoginStartup) setEnabled(enabled bool) error {
	if developmentMode() {
		return nil
	}
	if runtime.GOOS == "darwin" && s.status().State == "unavailable" {
		return errors.New("Start at login requires the installed Voice Type .app bundle")
	}
	if enabled {
		return s.app.Autostart.EnableWithOptions(application.AutostartOptions{Arguments: []string{"--background"}})
	}
	return s.app.Autostart.Disable()
}

func (b *Bridge) loginStartup() loginStartupControl {
	if b.loginControl != nil {
		return b.loginControl
	}
	return systemLoginStartup{app: b.app}
}

func (b *Bridge) SetLoginStartup(enabled bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefs := b.prefs
	prefs.Autostart, prefs.SetupComplete = enabled, true
	return b.savePreferences(prefs, true)
}

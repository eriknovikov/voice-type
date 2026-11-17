package voicetype

import "github.com/wailsapp/wails/v3/pkg/application"

func platformLoginStartup(app *application.App) LoginStartupStatus {
	if app == nil {
		return LoginStartupStatus{State: "unavailable"}
	}
	enabled, err := app.Autostart.IsEnabled()
	if err != nil {
		return LoginStartupStatus{State: "unavailable", Error: err.Error()}
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	return LoginStartupStatus{State: state, Enabled: enabled}
}

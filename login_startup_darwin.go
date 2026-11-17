package voicetype

/*
#cgo darwin LDFLAGS: -framework ServiceManagement
int vt_login_startup_status(void);
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func platformLoginStartup(*application.App) LoginStartupStatus {
	switch C.vt_login_startup_status() {
	case 1:
		return LoginStartupStatus{State: "enabled", Enabled: true}
	case 2:
		return LoginStartupStatus{State: "requires_approval"}
	case 0:
		return LoginStartupStatus{State: "disabled"}
	default:
		return LoginStartupStatus{State: "unavailable", Error: "Start at login requires the installed Voice Type .app bundle"}
	}
}

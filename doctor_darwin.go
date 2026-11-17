package voicetype

/*
#cgo darwin LDFLAGS: -framework Foundation
#include <stdlib.h>
char *vt_doctor_environment(void);
*/
import "C"

import (
	"encoding/json"
	"unsafe"
)

func runDiagnostics(version string) DiagnosticsReport {
	environment := macDiagnosticEnvironment{System: "macOS (version unavailable)", Sources: ListAudioSources}
	native := C.vt_doctor_environment()
	if native != nil {
		_ = json.Unmarshal([]byte(C.GoString(native)), &environment)
		C.free(unsafe.Pointer(native))
	}
	environment.Permissions = newPermissionAccess().status()
	environment.Login = (systemLoginStartup{}).status()
	return macDiagnostics(version, environment)
}

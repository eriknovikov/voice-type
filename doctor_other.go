//go:build !linux && !darwin

package voicetype

import (
	"fmt"
	"runtime"
)

func runDiagnostics(version string) DiagnosticsReport {
	return newDiagnosticsReport(version, []DiagnosticCheck{
		diagnosticCheck("Voice Type", "info", fmt.Sprintf("v%s · %s/%s", version, runtime.GOOS, runtime.GOARCH)),
		diagnosticCheck("Diagnostics", "warn", "environment checks are only implemented on Linux"),
	})
}

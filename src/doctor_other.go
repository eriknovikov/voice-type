//go:build !linux

package voicetype

import (
	"fmt"
	"io"
	"runtime"
)

func runDiagnostics(version string) DiagnosticsReport {
	return newDiagnosticsReport(version, []DiagnosticCheck{
		diagnosticCheck("Voice Type", "info", fmt.Sprintf("v%s · %s/%s", version, runtime.GOOS, runtime.GOARCH)),
		diagnosticCheck("Diagnostics", "warn", "environment checks are only implemented on Linux"),
	})
}

// RunDoctor prints the diagnostics available on this platform.
func RunDoctor(version string, out io.Writer) int {
	fmt.Fprint(out, runDiagnostics(version).Text())
	return 0
}

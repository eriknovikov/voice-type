package voicetype

import (
	"fmt"
	"strings"
	"time"
)

// DiagnosticCheck is one user-readable line of the diagnostics report. Status
// is one of ok, warn, fail, or info.
type DiagnosticCheck struct {
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// DiagnosticsReport is the same snapshot printed by `voice-type doctor` and
// shown in General. It never contains credentials, transcripts, or audio.
type DiagnosticsReport struct {
	Version     string            `json:"version"`
	GeneratedAt string            `json:"generated_at"`
	Checks      []DiagnosticCheck `json:"checks"`
}

// Text is the report in plain text for clipboard and terminal use.
func (r DiagnosticsReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Voice Type diagnostics v%s\n%s\n\n", r.Version, r.GeneratedAt)
	for _, check := range r.Checks {
		fmt.Fprintf(&b, "[%-4s] %s: %s\n", check.Status, check.Label, check.Detail)
	}
	return b.String()
}

func newDiagnosticsReport(version string, checks []DiagnosticCheck) DiagnosticsReport {
	return DiagnosticsReport{
		Version:     version,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Checks:      checks,
	}
}

func diagnosticCheck(label, status, detail string) DiagnosticCheck {
	return DiagnosticCheck{Label: label, Status: status, Detail: detail}
}

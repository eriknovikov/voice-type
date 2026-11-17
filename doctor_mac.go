package voicetype

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type macDiagnosticEnvironment struct {
	System, BundlePath, BundleID, BundleVersion, MinimumOS string
	MemoryBytes                                            uint64
	Permissions                                            PermissionStatus
	Login                                                  LoginStartupStatus
	Sources                                                func() ([]AudioSource, error)
}

func macDiagnostics(version string, environment macDiagnosticEnvironment) DiagnosticsReport {
	checks := []DiagnosticCheck{
		diagnosticCheck("Voice Type", "info", fmt.Sprintf("v%s · darwin/%s", version, runtime.GOARCH)),
		diagnosticCheck("System", "info", environment.System),
		macBundleCheck(environment),
	}
	for _, permission := range []struct {
		name  string
		state PermissionState
	}{
		{"Microphone", environment.Permissions.Microphone}, {"Accessibility", environment.Permissions.Accessibility},
	} {
		status, detail := "ok", string(permission.state)
		if permission.state != PermissionGranted {
			status = "warn"
			detail += "; open General to allow access or open System Settings > Privacy & Security > " + permission.name
		}
		checks = append(checks, diagnosticCheck(permission.name+" permission", status, detail))
	}
	loginStatus := "info"
	if environment.Login.Error != "" || environment.Login.State == "requires_approval" {
		loginStatus = "warn"
	}
	checks = append(checks, diagnosticCheck("Start at login", loginStatus, environment.Login.State))
	config, configCheck, microphone := readConfigForDiagnostics()
	checks = append(checks, configCheck, profileCheck(config))
	checks = append(checks, macAudioChecks(microphone, environment.Sources)...)
	checks = append(checks, macOutputCheck(config))
	checks = append(checks, diagnosticCheck("Credentials", "info", "storage: "+currentCredentialStorage()+"; Keychain is not queried by diagnostics"))
	checks = append(checks, diagnosticCheck("CPU", "info", fmt.Sprintf("%d logical cores; %s", runtime.NumCPU(), runtime.GOARCH)))
	if environment.MemoryBytes > 0 {
		checks = append(checks, diagnosticCheck("Memory", "info", fmt.Sprintf("%.1f GiB physical memory", float64(environment.MemoryBytes)/(1<<30))))
	}
	checks = append(checks, modelChecks(config)...)
	checks = append(checks, macStorageChecks()...)
	checks = append(checks, diskCheck(), logCheck(), lastDictationCheck())
	return newDiagnosticsReport(version, checks)
}

func macBundleCheck(environment macDiagnosticEnvironment) DiagnosticCheck {
	if !strings.HasSuffix(environment.BundlePath, ".app") || environment.BundleID == "" {
		return diagnosticCheck("Application bundle", "warn", "unbundled executable; use the Voice Type .app for permissions and login startup")
	}
	detail := environment.BundlePath + " · " + environment.BundleID + " · " + environment.BundleVersion + " · minimum macOS " + environment.MinimumOS
	return diagnosticCheck("Application bundle", "info", detail+"; signing/notarization not assessed by doctor")
}

func macAudioChecks(saved string, enumerate func() ([]AudioSource, error)) []DiagnosticCheck {
	sources, err := enumerate()
	if err != nil {
		return []DiagnosticCheck{diagnosticCheck("Audio devices", "warn", "Core Audio enumeration failed; no capture was attempted")}
	}
	status := "ok"
	if len(sources) == 0 {
		status = "warn"
	}
	checks := []DiagnosticCheck{diagnosticCheck("Audio devices", status, fmt.Sprintf("Core Audio · %d input device(s); enumeration only", len(sources)))}
	if saved == "" {
		return append(checks, diagnosticCheck("Microphone", "info", "System default"))
	}
	for _, source := range sources {
		if source.ID == saved {
			return append(checks, diagnosticCheck("Microphone", "ok", source.Name))
		}
	}
	return append(checks, diagnosticCheck("Microphone", "warn", "saved device is disconnected; recording will fall back to the system default"))
}

func macOutputCheck(config Config) DiagnosticCheck {
	prefs := defaultPreferences("darwin")
	data, err := readPrivateFile(preferencesPath())
	if err != nil && !os.IsNotExist(err) {
		return diagnosticCheck("Text insertion", "warn", "could not read private desktop preferences")
	}
	if data != nil {
		prefs, err = parsePreferencesForPlatform(data, "darwin")
		if err != nil {
			return diagnosticCheck("Text insertion", "warn", "invalid desktop preferences; open General to repair them")
		}
	}
	var mode streamingMode
	if profile, ok := config.activeProfile(); ok {
		mode = profile.StreamingMode()
	}
	backend, err := insertionBackendForPreferences("darwin", prefs, mode)
	if err != nil {
		return diagnosticCheck("Text insertion", "fail", err.Error())
	}
	if backend == InsertBackendMacPaste {
		return diagnosticCheck("Text insertion", "info", "opt-in paste compatibility; clipboard exposure, guarded restoration, no delivery confirmation")
	}
	return diagnosticCheck("Text insertion", "info", "direct AX/Unicode; clipboard unchanged; app compatibility is not guaranteed")
}

func macStorageChecks() []DiagnosticCheck {
	paths, err := currentStorageLocations()
	if err != nil {
		return []DiagnosticCheck{diagnosticCheck("Storage", "fail", err.Error())}
	}
	checks := []DiagnosticCheck{}
	for _, path := range []string{paths.data, paths.state, filepath.Dir(paths.logs)} {
		info, err := os.Lstat(path)
		status, detail := "ok", path
		switch {
		case os.IsNotExist(err):
			status, detail = "info", path+" (not created yet)"
		case err != nil:
			status, detail = "warn", path+" (unreadable)"
		case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
			status, detail = "fail", path+" (must be a real directory, not a symlink)"
		case info.Mode().Perm()&0077 != 0:
			status, detail = "warn", path+" (directory is not private)"
		}
		checks = append(checks, diagnosticCheck("Storage", status, detail))
	}
	return checks
}

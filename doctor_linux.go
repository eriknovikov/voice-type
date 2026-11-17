//go:build linux

package voicetype

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

func runDiagnostics(version string) DiagnosticsReport {
	checks := []DiagnosticCheck{
		diagnosticCheck("Voice Type", "info", fmt.Sprintf("v%s · %s/%s", version, runtime.GOOS, runtime.GOARCH)),
	}
	checks = append(checks, systemCheck())
	checks = append(checks, desktopCheck())
	checks = append(checks, installCheck())

	config, configCheck, microphone := readConfigForDiagnostics()
	checks = append(checks, configCheck)
	checks = append(checks, profileCheck(config))
	checks = append(checks, audioChecks(microphone)...)
	checks = append(checks, ibusCheck())
	checks = append(checks, credentialChecks()...)
	checks = append(checks, cpuCheck())
	checks = append(checks, memoryCheck())
	checks = append(checks, modelChecks(config)...)
	checks = append(checks, diskCheck())
	checks = append(checks, logCheck())
	checks = append(checks, lastDictationCheck())
	return newDiagnosticsReport(version, checks)
}

func systemCheck() DiagnosticCheck {
	name := ""
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			if value, ok := strings.CutPrefix(scanner.Text(), "PRETTY_NAME="); ok {
				name = strings.Trim(strings.TrimSpace(value), `"`)
				break
			}
		}
	}
	if name == "" {
		name = "unknown distribution"
	}
	return diagnosticCheck("System", "info", name)
}

func desktopCheck() DiagnosticCheck {
	session := strings.TrimSpace(os.Getenv("XDG_SESSION_TYPE"))
	desktop := strings.TrimSpace(os.Getenv("XDG_CURRENT_DESKTOP"))
	if desktop == "" {
		desktop = strings.TrimSpace(os.Getenv("DESKTOP_SESSION"))
	}
	if desktop == "" {
		desktop = "unknown desktop"
	}
	if session == "" {
		return diagnosticCheck("Desktop", "warn",
			desktop+" · no Wayland/X11 session detected; shortcuts and text insertion may not work")
	}
	return diagnosticCheck("Desktop", "ok", session+" · "+desktop)
}

// installCheck reports a copy of the application that a desktop shortcut starts
// instead of this one. The session PATH prefers /usr/local/bin, so a build left
// there by an older installer wins over the packaged /usr/bin binary.
func installCheck() DiagnosticCheck {
	dirs := launchSearchDirs()
	self, err := os.Executable()
	if err != nil {
		return diagnosticCheck("Installation", "info", "could not resolve the running executable")
	}
	if resolved, resolveErr := filepath.EvalSymlinks(self); resolveErr == nil {
		self = resolved
	}
	selfInfo, selfErr := os.Stat(self)
	for _, dir := range dirs {
		candidate := filepath.Join(dir, "voice-type")
		info, statErr := os.Stat(candidate)
		if statErr != nil || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if selfErr == nil && os.SameFile(selfInfo, info) {
			return diagnosticCheck("Installation", "ok", "started from "+candidate)
		}
		return diagnosticCheck("Installation", "warn",
			candidate+" starts instead of "+self+" when Voice Type is opened from the launcher; remove the older copy")
	}
	return diagnosticCheck("Installation", "info",
		"no installed binary found in "+strings.Join(dirs, ", "))
}

// launchSearchDirs is the order a desktop session resolves `voice-type` in: the
// system directories first, then the inherited PATH.
func launchSearchDirs() []string {
	dirs := []string{"/usr/local/bin", "/usr/bin"}
	seen := map[string]bool{dirs[0]: true, dirs[1]: true}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

func audioChecks(saved string) []DiagnosticCheck {
	device, err := newCaptureDevice()
	if err != nil {
		return []DiagnosticCheck{diagnosticCheck("Audio server", "fail", err.Error())}
	}
	defer device.close()
	sources, listErr := device.client.ListSources()
	if listErr != nil {
		return []DiagnosticCheck{diagnosticCheck("Audio server", "fail", "connect to PulseAudio/PipeWire: "+listErr.Error())}
	}
	defaultSource, defaultErr := device.client.DefaultSource()
	detail := fmt.Sprintf("%d input device(s)", len(sources))
	if defaultErr == nil {
		detail += " · default: " + defaultSource.Name()
	}
	checks := []DiagnosticCheck{diagnosticCheck("Audio server", "ok", detail)}

	if saved == "" {
		checks = append(checks, diagnosticCheck("Microphone", "info", "System default"))
		return checks
	}
	for _, source := range sources {
		if source.ID() == saved {
			checks = append(checks, diagnosticCheck("Microphone", "ok", source.Name()+" ("+saved+")"))
			return checks
		}
	}
	checks = append(checks, diagnosticCheck("Microphone", "warn",
		saved+" is not connected; recording will fall back to the system default"))
	return checks
}

func ibusCheck() DiagnosticCheck {
	_, err := ibusAddress(os.Getenv, readIBusMetadata, func(pid int) bool {
		return syscall.Kill(pid, 0) == nil
	})
	if err != nil {
		return diagnosticCheck("Text insertion", "warn", err.Error()+"; IBus must run for dictation to be inserted")
	}
	return diagnosticCheck("Text insertion", "ok", "IBus address is live")
}

func credentialChecks() []DiagnosticCheck {
	mode := currentCredentialStorage()
	service := secretService()
	detail := "storage: " + mode + " · keyring: " + service
	if mode == storageFile {
		return []DiagnosticCheck{diagnosticCheck("Credentials", "info", detail)}
	}
	switch {
	case service == "absent", strings.HasPrefix(service, "unavailable"):
		return []DiagnosticCheck{diagnosticCheck("Credentials", "warn",
			detail+"; save keys or switch to a local file in API Keys")}
	default:
		return []DiagnosticCheck{diagnosticCheck("Credentials", "ok", detail)}
	}
}

func cpuCheck() DiagnosticCheck {
	flags := ""
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "flags") {
				if _, value, ok := strings.Cut(line, ":"); ok {
					flags = value
				}
				break
			}
		}
	}
	features := make([]string, 0, 3)
	for _, feature := range []string{"avx2", "fma", "avx512f"} {
		if strings.Contains(" "+flags+" ", " "+feature+" ") {
			features = append(features, feature)
		}
	}
	detail := fmt.Sprintf("%d cores", runtime.NumCPU())
	if len(features) > 0 {
		detail += " · " + strings.Join(features, " ")
	}
	if !strings.Contains(" "+flags+" ", " avx2 ") {
		return diagnosticCheck("CPU", "warn", detail+" · no AVX2; local transcription will be slow")
	}
	return diagnosticCheck("CPU", "ok", detail)
}

func memoryCheck() DiagnosticCheck {
	total, available, err := readMemInfo()
	if err != nil {
		return diagnosticCheck("Memory", "info", "could not read /proc/meminfo")
	}
	detail := fmt.Sprintf("%.1f GiB total · %.1f GiB available",
		float64(total)/(1<<30), float64(available)/(1<<30))
	if available < 3<<30 {
		return diagnosticCheck("Memory", "warn",
			detail+" · a local model needs about 1.2 GiB while recording")
	}
	return diagnosticCheck("Memory", "ok", detail)
}

func readMemInfo() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	var total, available uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var value uint64
		if _, err := fmt.Sscan(fields[1], &value); err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = value * 1024
		case "MemAvailable:":
			available = value * 1024
		}
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("no memory information")
	}
	return total, available, nil
}

// secretService reports the owner of org.freedesktop.secrets without reading
// any stored secret.
func secretService() string {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	defer conn.Close()
	var owner string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, "org.freedesktop.secrets").Store(&owner); err != nil {
		return "absent"
	}
	var pid uint32
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid); err != nil {
		return owner
	}
	comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return owner + " (" + strings.TrimSpace(string(comm)) + ")"
}

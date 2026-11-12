//go:build linux

package voicetype

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// RunDoctor prints the same read-only report shown in General › Diagnostics.
// It never reads secrets.
func RunDoctor(version string, out io.Writer) int {
	fmt.Fprint(out, runDiagnostics(version).Text())
	return 0
}

func runDiagnostics(version string) DiagnosticsReport {
	checks := []DiagnosticCheck{
		diagnosticCheck("Voice Type", "info", fmt.Sprintf("v%s · %s/%s", version, runtime.GOOS, runtime.GOARCH)),
	}
	checks = append(checks, systemCheck())
	checks = append(checks, desktopCheck())

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

func readConfigForDiagnostics() (Config, DiagnosticCheck, string) {
	path := configFilePath()
	if _, err := os.Stat(path); err != nil {
		return Config{}, diagnosticCheck("Configuration", "info",
			path+" (not created yet)"), ""
	}
	_, raw, err := readConfigDocument()
	if err != nil {
		return Config{}, diagnosticCheck("Configuration", "fail", path+": "+err.Error()), ""
	}
	config, err := configValidate(raw)
	if err != nil {
		return Config{}, diagnosticCheck("Configuration", "fail", path+": "+err.Error()), ""
	}
	return config, diagnosticCheck("Configuration", "ok", path+" (valid)"), config.Microphone
}

func profileCheck(config Config) DiagnosticCheck {
	if len(config.Profiles) == 0 {
		return diagnosticCheck("Active profile", "warn", "no profiles yet; add one in General")
	}
	profile, ok := config.activeProfile()
	if !ok {
		return diagnosticCheck("Active profile", "warn", "none selected")
	}
	detail := profile.Name + " · " + profile.Provider
	if profile.Provider == providerLocal {
		detail += " · " + profile.ModelID()
		if mode := profile.StreamingMode(); mode != "" {
			detail += " · " + string(mode)
		}
	}
	return diagnosticCheck("Active profile", "ok", detail)
}

func audioChecks(saved string) []DiagnosticCheck {
	recorder, err := NewRecorder()
	if err != nil {
		return []DiagnosticCheck{diagnosticCheck("Audio server", "fail", err.Error())}
	}
	defer recorder.Close()
	sources, listErr := recorder.client.ListSources()
	if listErr != nil {
		return []DiagnosticCheck{diagnosticCheck("Audio server", "fail", "connect to PulseAudio/PipeWire: "+listErr.Error())}
	}
	defaultSource, defaultErr := recorder.client.DefaultSource()
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

func modelChecks(config Config) []DiagnosticCheck {
	directory, err := modelDataDirectory()
	if err != nil {
		return []DiagnosticCheck{diagnosticCheck("Local models", "fail", err.Error())}
	}
	selected := ""
	if profile, ok := config.activeProfile(); ok && profile.Provider == providerLocal {
		selected = profile.ModelID()
	}
	checks := make([]DiagnosticCheck, 0, len(localModelCatalog))
	for _, model := range localModelCatalog {
		label := "Model · " + model.Name
		if model.ID == selected {
			label += " (active)"
		}
		final := filepath.Join(directory, model.ID+".gguf")
		info, statErr := os.Lstat(final)
		switch {
		case statErr == nil && info.Size() == model.Bytes:
			checks = append(checks, diagnosticCheck(label, "ok",
				fmt.Sprintf("installed (%.0f MB)", float64(info.Size())/(1<<20))))
		case statErr == nil:
			checks = append(checks, diagnosticCheck(label, "warn",
				fmt.Sprintf("file is %.0f MB, expected %.0f MB; reinstall it in Local Models",
					float64(info.Size())/(1<<20), float64(model.Bytes)/(1<<20))))
		default:
			partial := filepath.Join(directory, model.ID+".partial")
			if partialInfo, partialErr := os.Lstat(partial); partialErr == nil && partialInfo.Size() > 0 {
				percent := float64(partialInfo.Size()) / float64(model.Bytes) * 100
				status := "warn"
				detail := fmt.Sprintf("download incomplete (%.0f%%)", percent)
				if model.ID == selected {
					status = "fail"
					detail += "; the active profile cannot record"
				}
				checks = append(checks, diagnosticCheck(label, status, detail))
			} else if model.ID == selected {
				checks = append(checks, diagnosticCheck(label, "fail",
					"not installed; download it in Local Models before recording"))
			} else {
				checks = append(checks, diagnosticCheck(label, "info", "not installed"))
			}
		}
	}
	return checks
}

func diskCheck() DiagnosticCheck {
	directory, err := modelDataDirectory()
	if err == nil {
		if free, freeErr := freeDiskBytes(directory); freeErr == nil {
			detail := fmt.Sprintf("%.1f GiB free under %s", float64(free)/(1<<30), directory)
			if free < 2<<30 {
				return diagnosticCheck("Disk space", "warn", detail+" · models need about 740 MB each")
			}
			return diagnosticCheck("Disk space", "ok", detail)
		}
	}
	return diagnosticCheck("Disk space", "info", "could not determine free space")
}

func freeDiskBytes(path string) (uint64, error) {
	for {
		var stat syscall.Statfs_t
		err := syscall.Statfs(path, &stat)
		if err == nil {
			return stat.Bavail * uint64(stat.Bsize), nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, err
		}
		path = parent
	}
}

func logCheck() DiagnosticCheck {
	path := runLogFilePath()
	total := int64(0)
	for _, candidate := range []string{path, path + ".1"} {
		if info, err := os.Stat(candidate); err == nil {
			total += info.Size()
		}
	}
	if total == 0 {
		return diagnosticCheck("Logs", "info", path+" (empty)")
	}
	if total < 1<<20 {
		return diagnosticCheck("Logs", "info", fmt.Sprintf("%s (%d KB)", path, (total+1023)/1024))
	}
	return diagnosticCheck("Logs", "info", fmt.Sprintf("%s (%.1f MB)", path, float64(total)/(1<<20)))
}

func lastDictationCheck() DiagnosticCheck {
	data, err := os.ReadFile(runLogFilePath())
	if err != nil || len(data) == 0 {
		return diagnosticCheck("Last dictation", "info", "none recorded yet")
	}
	lines := bytes.Split(data, []byte("\n"))
	var latest, failure *runLogRecord
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"dictation.completed"`)) {
			continue
		}
		var record runLogRecord
		if json.Unmarshal(lines[i], &record) != nil {
			continue
		}
		if latest == nil {
			copy := record
			latest = &copy
		}
		if failure == nil && record.Outcome != "ok" && record.Outcome != "cancelled" {
			copy := record
			failure = &copy
		}
		if latest != nil && failure != nil {
			break
		}
	}
	format := func(record *runLogRecord) string {
		detail := record.Timestamp + " · " + record.Provider + "/" + record.ConfiguredModel + " · " + record.Outcome
		if record.Error != "" {
			detail += " · " + record.Error
		}
		return detail
	}
	if failure != nil {
		return diagnosticCheck("Recent failure", "warn", format(failure))
	}
	if latest != nil {
		return diagnosticCheck("Last dictation", "ok", format(latest))
	}
	return diagnosticCheck("Last dictation", "info", "no completed dictation in the current log")
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

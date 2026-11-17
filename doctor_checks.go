package voicetype

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func readConfigForDiagnostics() (Config, DiagnosticCheck, string) {
	path := configFilePath()
	file, err := openPrivateFile(path, os.O_RDONLY)
	if os.IsNotExist(err) {
		return Config{}, diagnosticCheck("Configuration", "info", path+" (not created yet)"), ""
	}
	if err != nil {
		return Config{}, diagnosticCheck("Configuration", "fail", path+": could not read private configuration"), ""
	}
	defer file.Close()
	const limit = 4 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(data) > limit {
		return Config{}, diagnosticCheck("Configuration", "fail", path+": could not read configuration within size limit"), ""
	}
	raw, err := decodeConfigDocument(data)
	if err == nil {
		delete(raw, "api_key")
		delete(raw, "api_keys")
	}
	var config Config
	if err == nil {
		config, err = configValidate(raw)
	}
	if err != nil {
		return Config{}, diagnosticCheck("Configuration", "fail", path+": invalid; open General to repair it"), ""
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
		info, statErr := os.Lstat(filepath.Join(directory, model.ID+".gguf"))
		switch {
		case statErr == nil && !info.Mode().IsRegular():
			checks = append(checks, diagnosticCheck(label, "fail", "not a regular model file; reinstall it in Local Models"))
		case statErr == nil && info.Size() == model.Bytes:
			checks = append(checks, diagnosticCheck(label, "ok", fmt.Sprintf("installed (%.0f MB)", float64(info.Size())/(1<<20))))
		case statErr == nil:
			checks = append(checks, diagnosticCheck(label, "warn", fmt.Sprintf("file is %.0f MB, expected %.0f MB; reinstall it in Local Models", float64(info.Size())/(1<<20), float64(model.Bytes)/(1<<20))))
		default:
			partialInfo, partialErr := os.Lstat(filepath.Join(directory, model.ID+".partial"))
			if partialErr == nil && partialInfo.Mode().IsRegular() && partialInfo.Size() > 0 {
				status := "warn"
				detail := fmt.Sprintf("download incomplete (%.0f%%)", float64(partialInfo.Size())/float64(model.Bytes)*100)
				if model.ID == selected {
					status = "fail"
					detail += "; the active profile cannot record"
				}
				checks = append(checks, diagnosticCheck(label, status, detail))
			} else if model.ID == selected {
				checks = append(checks, diagnosticCheck(label, "fail", "not installed; download it in Local Models before recording"))
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
			return uint64(stat.Bavail) * uint64(stat.Bsize), nil
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
		if info, err := os.Lstat(candidate); err == nil && info.Mode().IsRegular() {
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
	var latest, failure *runLogRecord
	err := scanLogRecords(func(record runLogRecord) error {
		if record.Event != "dictation.completed" {
			return nil
		}
		copy := record
		latest = &copy
		if record.Outcome != "ok" && record.Outcome != "cancelled" && !record.isWarning() {
			failure = &copy
		}
		return nil
	})
	if err != nil {
		return diagnosticCheck("Last dictation", "warn", "could not read private diagnostic logs")
	}
	format := func(record *runLogRecord) string {
		return record.Timestamp + " · " + record.Provider + "/" + record.ConfiguredModel + " · " + record.Outcome
	}
	if failure != nil {
		return diagnosticCheck("Recent failure", "warn", format(failure))
	}
	if latest != nil {
		status := "ok"
		if latest.isWarning() {
			status = "warn"
		}
		return diagnosticCheck("Last dictation", status, format(latest))
	}
	return diagnosticCheck("Last dictation", "info", "none recorded yet")
}

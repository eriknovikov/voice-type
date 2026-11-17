package voicetype

import (
	"errors"
	"runtime"
)

func validateOutputMode(mode string) error {
	if mode != "" && mode != "direct" && mode != "paste" {
		return errors.New("choose direct insertion or paste compatibility")
	}
	return nil
}

func insertionBackendForPreferences(platform string, prefs Preferences, mode streamingMode) (InsertBackend, error) {
	if platform != "darwin" {
		return defaultInsertionBackend(platform), nil
	}
	if err := validateOutputMode(prefs.OutputMode); err != nil {
		return "", err
	}
	if prefs.OutputMode == "paste" {
		if mode == streamingModeLive {
			return "", errors.New("paste compatibility does not support live insertion; choose direct insertion or change this profile to process while recording or insert at stop")
		}
		return InsertBackendMacPaste, nil
	}
	return InsertBackendMacDirect, nil
}

func recordingOutputBackend(cfg Config) (InsertBackend, error) {
	if runtime.GOOS != "darwin" {
		return DefaultInsertBackend(), nil
	}
	prefs, _, err := loadPreferences()
	if err != nil {
		return "", err
	}
	var mode streamingMode
	if profile, ok := cfg.activeProfile(); ok {
		mode = profile.StreamingMode()
	}
	return insertionBackendForPreferences(runtime.GOOS, prefs, mode)
}

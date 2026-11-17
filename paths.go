package voicetype

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type storageLocations struct {
	config, state, data, logs, credentials, credentialPreference string
}

func currentStorageLocations() (storageLocations, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return storageLocations{}, errors.New("cannot locate your home directory")
	}
	return storageLocationsFor(runtime.GOOS, home, os.Getenv, developmentMode()), nil
}

func storageLocationsFor(platform, home string, getenv func(string) string, development bool) storageLocations {
	if platform == "darwin" {
		name := "Voice Type"
		if development {
			name += " Dev"
		}
		root := filepath.Join(home, "Library", "Application Support", name)
		return storageLocations{
			config: filepath.Join(root, "config.jsonc"), state: filepath.Join(root, "state"), data: root,
			logs:        filepath.Join(home, "Library", "Logs", name, "events.jsonl"),
			credentials: filepath.Join(root, "credentials"), credentialPreference: filepath.Join(root, "state", "credentials-storage"),
		}
	}
	base := func(key, fallback string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return filepath.Join(home, fallback)
	}
	name := "voice-type"
	if development {
		name += "-dev"
	}
	data := filepath.Join(base("XDG_DATA_HOME", ".local/share"), name)
	return storageLocations{
		config: filepath.Join(home, "."+name+"-config.jsonc"), state: filepath.Join(base("XDG_STATE_HOME", ".local/state"), name), data: data,
		logs: filepath.Join(home, "."+name+"-logs.jsonl"), credentials: data,
		credentialPreference: filepath.Join(base("XDG_CONFIG_HOME", ".config"), name, "credentials-storage"),
	}
}

func configFilePath() string {
	paths, _ := currentStorageLocations()
	return paths.config
}

func runLogFilePath() string {
	paths, _ := currentStorageLocations()
	return paths.logs
}

func stateFilePath(name string) string {
	paths, err := currentStorageLocations()
	if err != nil {
		return ""
	}
	return filepath.Join(paths.state, name)
}

func workspaceFilePath() string { return stateFilePath("workspace") }
func preferencesPath() string   { return stateFilePath("desktop.json") }

func modelDataDirectory() (string, error) {
	paths, err := currentStorageLocations()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(paths.data) {
		return "", errors.New("XDG_DATA_HOME must be an absolute path")
	}
	return filepath.Join(paths.data, "models"), nil
}

func credentialDirectory() (string, error) {
	paths, err := currentStorageLocations()
	return paths.credentials, err
}

func credentialStoragePath() (string, error) {
	paths, err := currentStorageLocations()
	return paths.credentialPreference, err
}

func engineRuntimeDir() string {
	if runtime.GOOS == "darwin" {
		return stateFilePath("runtime")
	}
	name := "voice-type"
	if developmentMode() {
		name += "-dev"
	}
	if base := os.Getenv("XDG_RUNTIME_DIR"); base != "" {
		return filepath.Join(base, name)
	}
	if developmentMode() {
		return filepath.Join(filepath.Dir(workspaceFilePath()), name)
	}
	return filepath.Dir(workspaceFilePath())
}

// Only application-owned directories are made private, never Library or HOME.
// Checking every application directory rejects symlinked roots and subfolders.
func prepareStorageParent(path string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	paths, err := currentStorageLocations()
	if err != nil {
		return err
	}
	for _, root := range []string{paths.data, filepath.Dir(paths.logs)} {
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if err := ensurePrivateDirectory(root); err != nil {
			return err
		}
		current := root
		if relative != "." {
			for _, part := range strings.Split(relative, string(filepath.Separator)) {
				current = filepath.Join(current, part)
				if err := ensurePrivateDirectory(current); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// Generic private-document helpers also accept caller-supplied file paths.
	return nil
}

func migrateLegacyConfiguration() error {
	if runtime.GOOS != "darwin" || developmentMode() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := prepareStorageParent(configFilePath()); err != nil {
		return err
	}
	return copyLegacyConfig(filepath.Join(home, ".voice-type-config.jsonc"), configFilePath())
}

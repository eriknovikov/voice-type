package voicetype

import (
	"os"
	"path/filepath"
	"strings"
)

// Which list is active is machine state, not configuration -- the terms
// themselves are content the user writes -- so the name lives in the state
// directory. The daemon re-reads it on every dictation, which is what makes a
// `vocab set` land without a restart.

// workspaceFilePath honours XDG_STATE_HOME, the same way configFilePath honours
// XDG_CONFIG_HOME.
func workspaceFilePath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "voice-type-workspace"
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "voice-type", "workspace")
}

// workspaceLoad returns the active workspace name, or "" for general only. An
// unreadable or missing file is not an error: it is the default state.
func workspaceLoad() string {
	data, _ := os.ReadFile(workspaceFilePath())
	return strings.TrimSpace(string(data))
}

// workspaceSave records the active workspace, or clears it when name is empty.
// It writes a temp file and renames, so a daemon reading mid-set sees either the
// old name or the new one, never half of either.
func workspaceSave(name string) error {
	path := workspaceFilePath()
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(name+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// workspaceLabel names the active workspace in output meant for a human.
func workspaceLabel(name string) string {
	if name == "" {
		return "none"
	}
	return name
}

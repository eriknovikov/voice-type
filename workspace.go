package voicetype

import (
	"os"
	"strings"
)

// Which list is active is machine state, not configuration -- the terms
// themselves are content the user writes -- so the name lives in the state
// directory. The engine re-reads it before every dictation.

// workspaceLoad returns the active workspace name, or "" for general only. An
// unreadable or missing file is not an error: it is the default state.
func workspaceLoad() string {
	data, _ := os.ReadFile(workspaceFilePath())
	return strings.TrimSpace(string(data))
}

// workspaceSave records the active workspace, or clears it when name is empty.
// It writes a temp file and renames, so the engine reading mid-save sees either the
// old name or the new one, never half of either.
func workspaceSave(name string) error {
	path := workspaceFilePath()
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return atomicPrivateWrite(path, []byte(name+"\n"))
}

// workspaceLabel names the active workspace in output meant for a human.
func workspaceLabel(name string) string {
	if name == "" {
		return "none"
	}
	return name
}

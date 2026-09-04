package voicetype

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points XDG_STATE_HOME at a scratch directory for the whole package:
// no test should read -- or switch -- the workspace of the machine running it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "voice-type-state-")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestWorkspaceFilePathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got, want := workspaceFilePath(), "/xdg/voice-type/workspace"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWorkspaceSaveLoadAndClear(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	// No file yet is the default state, not an error.
	if got := workspaceLoad(); got != "" {
		t.Errorf("with no state file, got %q", got)
	}

	if err := workspaceSave("project-one"); err != nil {
		t.Fatalf("workspaceSave: %v", err)
	}
	if got := workspaceLoad(); got != "project-one" {
		t.Errorf("got %q, want project-one", got)
	}

	info, err := os.Stat(workspaceFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}

	if err := workspaceSave("voice-type"); err != nil {
		t.Fatalf("workspaceSave: %v", err)
	}
	if got := workspaceLoad(); got != "voice-type" {
		t.Errorf("got %q, want voice-type", got)
	}

	// Clearing removes the file rather than leaving an empty one behind.
	if err := workspaceSave(""); err != nil {
		t.Fatalf("workspaceSave(clear): %v", err)
	}
	if _, err := os.Stat(workspaceFilePath()); !os.IsNotExist(err) {
		t.Errorf("state file survived a clear: %v", err)
	}
	if got := workspaceLoad(); got != "" {
		t.Errorf("after clearing, got %q", got)
	}
	// Clearing twice is not an error either.
	if err := workspaceSave(""); err != nil {
		t.Errorf("clearing an already-clear workspace: %v", err)
	}

	// The switch leaves nothing behind in the state directory.
	entries, err := os.ReadDir(filepath.Dir(workspaceFilePath()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("state directory holds %d leftovers", len(entries))
	}
}

func TestWorkspaceLoadTrimsTrailingNewline(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Dir(workspaceFilePath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Hand-written by `echo project-one > .../workspace`, which is a fair way to set it.
	if err := os.WriteFile(workspaceFilePath(), []byte("project-one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := workspaceLoad(); got != "project-one" {
		t.Errorf("got %q, want project-one", got)
	}
}

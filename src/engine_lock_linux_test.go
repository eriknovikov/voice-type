package voicetype

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A real process holds the same lock across session buses; the kernel releases it on a crash.
func TestEngineOwnerProcess(t *testing.T) {
	if os.Getenv("VOICE_TYPE_TEST_ENGINE_OWNER") == "" {
		return
	}
	engine, err := startEngine("test")
	if err != nil {
		os.Exit(1)
	}
	println("ready")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	engine.Close()
	os.Exit(0)
}
func TestEngineOwnershipAcrossProcesses(t *testing.T) {
	for _, kill := range []bool{false, true} {
		t.Run(fmt.Sprint("kill=", kill), func(t *testing.T) {
			setupEngineTest(t)
			cmd := exec.Command(os.Args[0], "-test.run=^TestEngineOwnerProcess$")
			cmd.Env = append(os.Environ(), "VOICE_TYPE_TEST_ENGINE_OWNER=1")
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				lock, err := acquireEngineLock()
				if err != nil {
					break
				}
				lock.Close()
				if time.Now().After(deadline) {
					t.Fatal("child did not acquire engine")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, err := startEngine("second"); err == nil {
				t.Fatal("second engine acquired ownership")
			}
			if kill {
				cmd.Process.Kill()
			} else {
				input.Close()
			}
			if err := cmd.Wait(); !kill && err != nil {
				t.Fatal(err)
			}
			replacement, err := startEngine("replacement")
			if err != nil {
				t.Fatal("ownership not released", err)
			}
			replacement.Close()
			info, err := os.Stat(filepath.Join(engineRuntimeDir(), "engine.lock"))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("lock permissions", err)
			}
		})
	}
}
func TestEngineLockRejectsUnsafePaths(t *testing.T) {
	for _, kind := range []string{"symlink", "public-file", "public-directory"} {
		t.Run(kind, func(t *testing.T) {
			setupEngineTest(t)
			dir := engineRuntimeDir()
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "engine.lock")
			switch kind {
			case "symlink":
				os.Symlink(filepath.Join(t.TempDir(), "foreign"), path)
			case "public-file":
				os.WriteFile(path, []byte("untouched"), 0644)
			case "public-directory":
				os.Chmod(dir, 0755)
			}
			if lock, err := acquireEngineLock(); err == nil {
				lock.Close()
				t.Fatal("unsafe path accepted")
			}
		})
	}
}

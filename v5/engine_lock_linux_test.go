package voicetype

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Real child process ownership tests both entry points and kernel crash cleanup.
func TestEngineOwnerProcess(t *testing.T) {
	mode := os.Getenv("VOICE_TYPE_TEST_ENGINE_OWNER")
	if mode == "" {
		return
	}
	if mode == "cli" {
		os.Exit(runDaemon("test", ""))
	}
	service, err := StartService("test")
	if err != nil {
		os.Exit(1)
	}
	println("ready")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	service.Close()
	os.Exit(0)
}
func TestEngineOwnershipAcrossProcesses(t *testing.T) {
	for _, owner := range []string{"desktop", "cli"} {
		for _, kill := range []bool{false, true} {
			t.Run(owner+map[bool]string{false: "/close", true: "/kill"}[kill], func(t *testing.T) {
				port := setupServiceTest(t)
				cmd := exec.Command(os.Args[0], "-test.run=^TestEngineOwnerProcess$")
				cmd.Env = append(os.Environ(), "VOICE_TYPE_TEST_ENGINE_OWNER="+owner)
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
				if _, err := StartService("second"); err == nil {
					t.Fatal("second desktop acquired engine")
				}
				if code := runDaemon("second", ""); code != 1 {
					t.Fatalf("second CLI exit %d", code)
				}
				// CLI becomes healthy only after its HTTP loop has started.
				if owner == "cli" {
					for !portInUse(port) {
						if time.Now().After(deadline) {
							t.Fatal("CLI listener did not start")
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
				if owner == "cli" {
					client := &http.Client{Timeout: time.Second}
					for _, check := range []struct {
						path string
						code int
					}{{"/health", 200}, {"/start", 503}, {"/toggle", 503}, {"/stop", 200}} {
						response, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d%s", port, check.path), "application/json", nil)
						if err != nil {
							t.Fatal(err)
						}
						io.Copy(io.Discard, response.Body)
						response.Body.Close()
						if response.StatusCode != check.code {
							t.Fatalf("%s: %d", check.path, response.StatusCode)
						}
					}
				}
				if kill {
					cmd.Process.Kill()
				} else if owner == "cli" {
					response, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/exit", port), "application/json", nil)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
				} else {
					input.Close()
				}
				err = cmd.Wait()
				if !kill && err != nil {
					t.Fatal(err)
				}
				service, err := StartService("replacement")
				if err != nil {
					t.Fatal("ownership not released", err)
				}
				service.Close()
				info, err := os.Stat(filepath.Join(filepath.Dir(ManagementSocketPath()), "engine.lock"))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("lock permissions", err)
				}
			})
		}
	}
}
func TestEngineLockRejectsUnsafePaths(t *testing.T) {
	for _, kind := range []string{"symlink", "public-file", "public-directory"} {
		t.Run(kind, func(t *testing.T) {
			setupServiceTest(t)
			dir := filepath.Dir(ManagementSocketPath())
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

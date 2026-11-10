package voicetype

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func installerFixture(t *testing.T) (string, string) {
	t.Helper()
	settingsTestHome(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	source := t.TempDir()
	bin := filepath.Join(t.TempDir(), "a space & quote's $bin")
	t.Setenv("VOICE_TYPE_BIN_DIR", bin)
	for _, directory := range []string{source, filepath.Join(source, "build"), filepath.Join(source, "tools"), bin} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"install.sh", "uninstall.sh"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name), data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	fixture := `#!/bin/sh
case "$1" in
--version) echo "${TEST_APP_VERSION:-6.0.0}" ;;
--quit) [ "${TEST_REFUSE_QUIT:-0}" = 0 ] ;;
--remove-shortcuts) : ;;
*) exit 2 ;;
esac
`
	for path, data := range map[string]string{
		"build/voice-type": fixture, "build/LICENSE": "voice-type license", "build/THIRD-PARTY-LICENSES.md": "third-party license",
		"go.mod": "module example.test/app\n", "icon.png": "fixture-icon",
		"tools/make": "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(source, path), []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Join(source, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"))
	return source, bin
}
func runInstaller(t *testing.T, source string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(filepath.Join(source, "install.sh"), args...)
	output, err := command.CombinedOutput()
	return string(output), err
}
func TestInstallerCleanUpgradeUninstallPreservesData(t *testing.T) {
	source, bin := installerFixture(t)
	dataPaths := []string{configFilePath(), runLogFilePath(), workspaceFilePath(), preferencesPath(), filepath.Join(os.Getenv("XDG_DATA_HOME"), "voice-type/backups/config.jsonc")}
	for _, path := range dataPaths {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("private-data-to-preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for iteration := range 2 {
		if iteration == 1 {
			if err := os.WriteFile(filepath.Join(bin, "voice-type-app"), []byte("old-companion"), 0755); err != nil {
				t.Fatal(err)
			}
			autostart := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart/voice-type.desktop")
			if err := os.MkdirAll(filepath.Dir(autostart), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(autostart, []byte("old-executable"), 0644); err != nil {
				t.Fatal(err)
			}
		}
		if output, err := runInstaller(t, source, "--local"); err != nil {
			t.Fatalf("install: %v %s", err, output)
		}
		executable := filepath.Join(bin, "voice-type")
		escapedExecutable := strings.ReplaceAll(executable, "$", `\$`)
		if info, err := os.Stat(executable); err != nil || info.Mode().Perm() != 0755 {
			t.Fatal("missing executable", err)
		}
		entry, err := os.ReadFile(filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications/io.github.eriknovikov.voice-type.desktop"))
		if err != nil || !strings.Contains(string(entry), `Exec="`+escapedExecutable+`"`) {
			t.Fatalf("launcher: %s %v", entry, err)
		}
		if _, err := os.Stat(filepath.Join(bin, "voice-type-app")); !os.IsNotExist(err) {
			t.Fatal("old executable survived")
		}
		for _, license := range []string{"LICENSE", "THIRD-PARTY-LICENSES.md"} {
			if _, err := os.Stat(filepath.Join(os.Getenv("XDG_DATA_HOME"), "doc/voice-type", license)); err != nil {
				t.Fatalf("installed license %s is missing: %v", license, err)
			}
		}
		if iteration == 1 {
			entry, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart/voice-type.desktop"))
			if err != nil || !strings.Contains(string(entry), `Exec="`+escapedExecutable+`" --background`) {
				t.Fatalf("autostart: %s %v", entry, err)
			}
		}
	}
	if output, err := runInstaller(t, source, "--uninstall"); err != nil {
		t.Fatalf("uninstall: %v %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(bin, "voice-type")); !os.IsNotExist(err) {
		t.Fatal("binary remained")
	}
	for _, license := range []string{"LICENSE", "THIRD-PARTY-LICENSES.md"} {
		if _, err := os.Stat(filepath.Join(os.Getenv("XDG_DATA_HOME"), "doc/voice-type", license)); !os.IsNotExist(err) {
			t.Fatalf("installed license %s remained after uninstall", license)
		}
	}
	for _, path := range dataPaths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "private-data-to-preserve" {
			t.Fatalf("user data changed: %s %v", path, err)
		}
	}
}
func TestInstallerRefusesUnsafeReplacement(t *testing.T) {
	for _, kind := range []string{"old-daemon", "quit-refused", "engine-busy"} {
		t.Run(kind, func(t *testing.T) {
			source, bin := installerFixture(t)
			installed := filepath.Join(bin, "voice-type")
			if err := os.WriteFile(installed, []byte("original-binary"), 0755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "old-daemon":
				t.Setenv("TEST_APP_VERSION", "5.7.0")
			case "quit-refused":
				t.Setenv("TEST_REFUSE_QUIT", "1")
			case "engine-busy":
				lock, err := acquireEngineLock()
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			if output, err := runInstaller(t, source, "--local"); err == nil {
				t.Fatalf("unsafe upgrade accepted: %s", output)
			}
			data, err := os.ReadFile(installed)
			if err != nil || string(data) != "original-binary" {
				t.Fatal("running installation overwritten", err)
			}
		})
	}
}

func TestDownloadedApplicationIntegrity(t *testing.T) {
	for _, kind := range []string{"valid", "checksum", "layout", "version", "daemon-release"} {
		t.Run(kind, func(t *testing.T) {
			source, bin := installerFixture(t)
			release := t.TempDir()
			t.Setenv("TEST_RELEASE", release)
			// Match the installer curl call, without contacting the network.
			curl := `#!/bin/sh
cp "$TEST_RELEASE/$(basename "$2")" "$4"
`
			if err := os.WriteFile(filepath.Join(source, "tools/curl"), []byte(curl), 0755); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(source, "build")
			for _, name := range []string{"icon.png", "LICENSE", "THIRD-PARTY-LICENSES.md"} {
				if err := os.WriteFile(filepath.Join(payload, name), []byte(name), 0644); err != nil {
					t.Fatal(err)
				}
			}
			members := []string{"voice-type", "icon.png", "LICENSE", "THIRD-PARTY-LICENSES.md"}
			if kind == "layout" {
				members = append(members, "unexpected")
				os.WriteFile(filepath.Join(payload, "unexpected"), nil, 0644)
			}
			arch := "arm64"
			if output, err := exec.Command("uname", "-m").Output(); err == nil && strings.TrimSpace(string(output)) == "x86_64" {
				arch = "x64"
			}
			archiveName := "voice-type-linux-" + arch + ".tar.gz"
			archive := filepath.Join(release, archiveName)
			args := append([]string{"-czf", archive, "-C", payload}, members...)
			if output, err := exec.Command("tar", args...).CombinedOutput(); err != nil {
				t.Fatalf("archive: %v %s", err, output)
			}
			data, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), archiveName)
			if err := os.WriteFile(filepath.Join(release, "checksums.txt"), []byte(checksum), 0644); err != nil {
				t.Fatal(err)
			}
			tag := "v6.0.0"
			switch kind {
			case "checksum":
				os.WriteFile(archive, []byte("corrupt-download"), 0644)
			case "version":
				t.Setenv("TEST_APP_VERSION", "6.1.0")
			case "daemon-release":
				tag = "v5.7.0"
			}
			output, err := runInstaller(t, source, "--version", tag)
			if kind == "valid" {
				if err != nil {
					t.Fatalf("valid archive: %v %s", err, output)
				}
			} else if err == nil {
				t.Fatalf("invalid release accepted: %s", output)
			}
			_, statErr := os.Stat(filepath.Join(bin, "voice-type"))
			if (kind == "valid") != (statErr == nil) {
				t.Fatal("unexpected installation state", statErr)
			}
		})
	}
}

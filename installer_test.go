package voicetype

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
record() { [ -z "${TEST_CALLS:-}" ] || printf '%s\n' "$1" >> "$TEST_CALLS"; }
case "$1" in
--version) echo "${TEST_APP_VERSION:-6.0.0}" ;;
--quit) [ "${TEST_REFUSE_QUIT:-0}" = 0 ] || exit 1; record quit ;;
--remove-shortcuts) record remove-shortcuts ;;
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
	// A test must never see (or remove) the package manager state of the host.
	for _, name := range []string{"dpkg-query", "rpm", "pacman"} {
		writeInstallerTool(t, source, name, "#!/bin/sh\nexit 1\n")
	}
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("VOICE_TYPE_NO_KEYRING", "1")
	// Keep the legacy system-wide path inside the fixture: no host binary may be
	// quit, removed or inspected by a test.
	t.Setenv("VOICE_TYPE_LEGACY_BIN", filepath.Join(t.TempDir(), "usr/local/bin/voice-type"))
	return source, bin
}
func runInstaller(t *testing.T, source string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(filepath.Join(source, "install.sh"), args...)
	output, err := command.CombinedOutput()
	return string(output), err
}
func runUninstaller(t *testing.T, source string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command(filepath.Join(source, "uninstall.sh"), args...)
	output, err := command.CombinedOutput()
	return string(output), err
}
func uninstallFixturePaths() []string {
	return []string{
		configFilePath(), runLogFilePath(), workspaceFilePath(), preferencesPath(),
		filepath.Join(os.Getenv("XDG_DATA_HOME"), "voice-type/backups/config.jsonc"),
		filepath.Join(os.Getenv("XDG_DATA_HOME"), "voice-type/models/model.bin"),
		filepath.Join(os.Getenv("XDG_DATA_HOME"), "voice-type/openai.key"),
		filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "voice-type/credentials-storage"),
	}
}

func seedUninstallData(t *testing.T) []string {
	t.Helper()
	paths := uninstallFixturePaths()
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("private-data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func seedLegacyInstall(t *testing.T, source string) {
	t.Helper()
	bin := os.Getenv("VOICE_TYPE_BIN_DIR")
	data := os.Getenv("XDG_DATA_HOME")
	files := []string{
		filepath.Join(bin, "voice-type"),
		filepath.Join(bin, "voice-type-app"),
		filepath.Join(data, "applications/io.github.eriknovikov.voice-type.desktop"),
		filepath.Join(data, "applications/voice-type.desktop"),
		filepath.Join(data, "icons/hicolor/512x512/apps/io.github.eriknovikov.voice-type.png"),
		filepath.Join(data, "doc/voice-type/LICENSE"),
	}
	for _, path := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("legacy"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	legacySystem := os.Getenv("VOICE_TYPE_LEGACY_BIN")
	if err := os.MkdirAll(filepath.Dir(legacySystem), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySystem, []byte("legacy"), 0755); err != nil {
		t.Fatal(err)
	}
}

func legacyInstallPaths() []string {
	data := os.Getenv("XDG_DATA_HOME")
	return []string{
		filepath.Join(os.Getenv("VOICE_TYPE_BIN_DIR"), "voice-type"),
		filepath.Join(os.Getenv("VOICE_TYPE_BIN_DIR"), "voice-type-app"),
		os.Getenv("VOICE_TYPE_LEGACY_BIN"),
		filepath.Join(data, "applications/io.github.eriknovikov.voice-type.desktop"),
		filepath.Join(data, "applications/voice-type.desktop"),
		filepath.Join(data, "icons/hicolor/512x512/apps/io.github.eriknovikov.voice-type.png"),
		filepath.Join(data, "doc/voice-type"),
	}
}

func TestInstallerRemovesLegacyInstallation(t *testing.T) {
	source, _, _ := packageFixture(t, false)
	seedLegacyInstall(t, source)
	output, err := runInstaller(t, source, "--version", "v6.0.0")
	if err != nil {
		t.Fatalf("install: %v %s", err, output)
	}
	for _, path := range legacyInstallPaths() {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy installation remained: %s %v", path, err)
		}
	}
	if !strings.Contains(output, "system-wide") {
		t.Fatalf("missing system-wide result: %s", output)
	}
}

func TestInstallerRejectsPerUserModes(t *testing.T) {
	source, _ := installerFixture(t)
	for _, mode := range []string{"--user", "--local"} {
		output, err := runInstaller(t, source, mode)
		if err == nil {
			t.Fatalf("%s accepted: %s", mode, output)
		}
		if !strings.Contains(output, "system-wide only") {
			t.Fatalf("missing system-wide refusal for %s: %s", mode, output)
		}
	}
}

func TestInstallerRequiresRootOrSudo(t *testing.T) {
	source, _, _ := packageFixture(t, false)
	args := os.Getenv("TEST_PACKAGE_ARGS")
	writeInstallerTool(t, source, "id", "#!/bin/sh\necho 1000\n")
	writeInstallerTool(t, source, "uname", "#!/bin/sh\nexec /usr/bin/uname \"$@\"\n")
	os.Remove(filepath.Join(source, "tools", "sudo"))
	t.Setenv("PATH", filepath.Join(source, "tools"))
	output, err := runInstaller(t, source, "--version", "v6.0.0")
	if err == nil {
		t.Fatalf("install without root accepted: %s", output)
	}
	if !strings.Contains(output, "requires root or sudo") {
		t.Fatalf("missing root requirement: %s", output)
	}
	if recorded, readErr := os.ReadFile(args); readErr == nil && strings.Contains(string(recorded), "install -y") {
		t.Fatalf("package manager ran without root: %s", recorded)
	}
}

func TestInstallerRefusesShutdownRequest(t *testing.T) {
	source, _, _ := packageFixture(t, false)
	t.Setenv("TEST_REFUSE_QUIT", "1")
	output, err := runInstaller(t, source, "--version", "v6.0.0")
	if err == nil {
		t.Fatalf("refused shutdown accepted: %s", output)
	}
	if !strings.Contains(output, "Could not request shutdown") {
		t.Fatalf("missing shutdown refusal: %s", output)
	}
	if recorded, readErr := os.ReadFile(os.Getenv("TEST_PACKAGE_ARGS")); readErr == nil && strings.Contains(string(recorded), "install -y") {
		t.Fatalf("package manager ran while the application was running: %s", recorded)
	}
}

func TestInstallerRejectsLegacyUninstallFlag(t *testing.T) {
	source, _ := installerFixture(t)
	output, err := runInstaller(t, source, "--uninstall")
	if err == nil {
		t.Fatalf("legacy uninstall flag accepted: %s", output)
	}
	if !strings.Contains(output, "Usage: install.sh") {
		t.Fatalf("missing usage for rejected flag: %s", output)
	}
}

func TestUninstallerRemovesInstallationAndData(t *testing.T) {
	source, _, _ := packageFixture(t, false)
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("TEST_CALLS", calls)
	seedLegacyInstall(t, source)
	dataPaths := seedUninstallData(t)
	output, err := runUninstaller(t, source)
	if err != nil {
		t.Fatalf("uninstall: %v %s", err, output)
	}
	for _, path := range legacyInstallPaths() {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("installation file remained: %s %v", path, err)
		}
	}
	for _, path := range dataPaths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("user data remained after uninstall: %s %v", path, err)
		}
	}
	recorded, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"quit", "remove-shortcuts"} {
		if !strings.Contains(string(recorded), expected) {
			t.Fatalf("uninstall skipped %s: %s", expected, recorded)
		}
	}
}

func TestUninstallerKeepsDataOnRequest(t *testing.T) {
	source, _, _ := packageFixture(t, false)
	seedLegacyInstall(t, source)
	dataPaths := seedUninstallData(t)
	if output, err := runUninstaller(t, source, "--keep-data"); err != nil {
		t.Fatalf("uninstall: %v %s", err, output)
	}
	for _, path := range legacyInstallPaths() {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("installation file remained: %s %v", path, err)
		}
	}
	for _, path := range dataPaths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "private-data" {
			t.Fatalf("user data changed: %s %v", path, err)
		}
	}
}

func writeInstallerTool(t *testing.T, source, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(source, "tools", name), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
}

func packageFixture(t *testing.T, corrupt bool) (string, string, string) {
	t.Helper()
	source, _ := installerFixture(t)
	release := t.TempDir()
	t.Setenv("TEST_RELEASE", release)
	t.Setenv("VOICE_TYPE_INSTALLED_BIN", filepath.Join(source, "build/voice-type"))
	args := filepath.Join(t.TempDir(), "package-args")
	t.Setenv("TEST_PACKAGE_ARGS", args)
	writeInstallerTool(t, source, "curl", `#!/bin/sh
cp "$TEST_RELEASE/$(basename "$2")" "$4"
`)
	writeInstallerTool(t, source, "sudo", "#!/bin/sh\nexec \"$@\"\n")
	writeInstallerTool(t, source, "apt-get", `#!/bin/sh
printf '%s\n' "$*" >> "$TEST_PACKAGE_ARGS"
exit 0
`)
	arch := "arm64"
	if output, err := exec.Command("uname", "-m").Output(); err == nil && strings.TrimSpace(string(output)) == "x86_64" {
		arch = "amd64"
	}
	name := "voice-type_6.0.0_" + arch + ".deb"
	data := []byte("fake-package")
	if corrupt {
		data = []byte("corrupt-package")
	}
	if err := os.WriteFile(filepath.Join(release, name), data, 0644); err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("fake-package")), name)
	if err := os.WriteFile(filepath.Join(release, "checksums.txt"), []byte(checksum), 0644); err != nil {
		t.Fatal(err)
	}
	return source, args, name
}

func TestInstallerInstallsDistributionPackage(t *testing.T) {
	source, args, name := packageFixture(t, false)
	output, err := runInstaller(t, source, "--version", "v6.0.0")
	if err != nil {
		t.Fatalf("install: %v %s", err, output)
	}
	recorded, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	commands := string(recorded)
	if !strings.Contains(commands, "update") {
		t.Fatalf("package indexes were not refreshed: %s", commands)
	}
	if !strings.Contains(commands, "install -y") || !strings.Contains(commands, name) {
		t.Fatalf("distribution package was not installed: %s", commands)
	}
	if !strings.Contains(output, "system-wide") {
		t.Fatalf("missing system-wide result: %s", output)
	}
}

func TestInstallerRejectsCorruptDistributionPackage(t *testing.T) {
	source, args, _ := packageFixture(t, true)
	output, err := runInstaller(t, source, "--version", "v6.0.0")
	if err == nil {
		t.Fatalf("corrupt package accepted: %s", output)
	}
	if recorded, readErr := os.ReadFile(args); readErr == nil && strings.Contains(string(recorded), "install -y") {
		t.Fatalf("package manager ran after a checksum failure: %s", recorded)
	}
}

func TestUninstallerRemovesDistributionPackage(t *testing.T) {
	source, _ := installerFixture(t)
	t.Setenv("VOICE_TYPE_INSTALLED_BIN", filepath.Join(t.TempDir(), "voice-type"))
	args := filepath.Join(t.TempDir(), "package-args")
	t.Setenv("TEST_PACKAGE_ARGS", args)
	writeInstallerTool(t, source, "sudo", "#!/bin/sh\nexec \"$@\"\n")
	writeInstallerTool(t, source, "dpkg-query", "#!/bin/sh\nprintf '%s\\n' 'install ok installed'\n")
	writeInstallerTool(t, source, "apt-get", `#!/bin/sh
printf '%s\n' "$*" >> "$TEST_PACKAGE_ARGS"
exit 0
`)
	output, err := runUninstaller(t, source)
	if err != nil {
		t.Fatalf("uninstall: %v %s", err, output)
	}
	recorded, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recorded), "remove -y voice-type") {
		t.Fatalf("distribution package was not removed: %s", recorded)
	}
}

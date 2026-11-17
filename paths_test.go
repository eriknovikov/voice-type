package voicetype

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestMacStorageLocationsAndDevelopmentIsolation(t *testing.T) {
	home := t.TempDir()
	getenv := func(string) string { return "/ignored-xdg" }
	paths := storageLocationsFor("darwin", home, getenv, false)
	root := filepath.Join(home, "Library", "Application Support", "Voice Type")
	want := storageLocations{
		config: filepath.Join(root, "config.jsonc"), state: filepath.Join(root, "state"),
		data: root, logs: filepath.Join(home, "Library", "Logs", "Voice Type", "events.jsonl"),
		credentials: filepath.Join(root, "credentials"), credentialPreference: filepath.Join(root, "state", "credentials-storage"),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Mac paths = %+v, want %+v", paths, want)
	}
	dev := storageLocationsFor("darwin", home, getenv, true)
	if dev.config == paths.config || dev.data == paths.data || dev.state == paths.state || dev.logs == paths.logs || dev.credentials == paths.credentials {
		t.Fatal("development storage overlaps production", dev)
	}
	if filepath.Dir(dev.config) != filepath.Join(home, "Library", "Application Support", "Voice Type Dev") {
		t.Fatal("incorrect development identity", dev)
	}
}

func TestLinuxProductionPathsUnchanged(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"XDG_STATE_HOME": "/state", "XDG_DATA_HOME": "/data", "XDG_CONFIG_HOME": "/config"}
	paths := storageLocationsFor("linux", home, func(key string) string { return env[key] }, false)
	want := storageLocations{
		config: filepath.Join(home, ".voice-type-config.jsonc"), state: "/state/voice-type", data: "/data/voice-type",
		logs: filepath.Join(home, ".voice-type-logs.jsonl"), credentials: "/data/voice-type", credentialPreference: "/config/voice-type/credentials-storage",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("Linux paths = %+v, want %+v", paths, want)
	}
}

func TestDevelopmentStampDoesNotNeedEnvironment(t *testing.T) {
	t.Setenv(developmentEnvironment, "")
	previous := developmentBuild
	t.Cleanup(func() { developmentBuild = previous })
	developmentBuild = "true"
	if !developmentMode() {
		t.Fatal("Finder launch would lose development isolation")
	}
	if id, _ := applicationIdentity(); id != "io.github.eriknovikov.voice-type.dev" {
		t.Fatal("development app identity lost")
	}
	if credentialServiceName() != credentialService+".dev" {
		t.Fatal("development keys share the production service")
	}
	developmentBuild = "false"
	if developmentMode() || credentialServiceName() != credentialService {
		t.Fatal("production identity changed")
	}
}

func TestLegacyConfigCopyOnlyIfAbsent(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "legacy"), filepath.Join(dir, "native", "config.jsonc")
	if err := os.WriteFile(source, []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyConfig(source, destination); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(destination); string(data) != "legacy" {
		t.Fatal("legacy config not copied")
	}
	if info, _ := os.Stat(destination); info.Mode().Perm() != 0600 {
		t.Fatal("legacy config copy is not private")
	}
	if err := os.WriteFile(destination, []byte("newer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyConfig(source, destination); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(destination); string(data) != "newer" {
		t.Fatal("migration overwrote existing native config")
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyConfig(source, destination); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(destination); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("migration replaced existing symlink")
	}
}

func TestPrivateStorageRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	foreign := filepath.Join(dir, "foreign")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(foreign, alias); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(alias); err == nil {
		t.Fatal("symlinked private directory accepted")
	}
	if err := os.WriteFile(filepath.Join(foreign, "key"), []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	keyAlias := filepath.Join(dir, "key-alias")
	if err := os.Symlink(filepath.Join(foreign, "key"), keyAlias); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(keyAlias); err == nil {
		t.Fatal("symlinked credential/settings file accepted")
	}
	if err := copyLegacyConfig(keyAlias, filepath.Join(dir, "destination")); err == nil {
		t.Fatal("symlinked migration source accepted")
	}
}

func TestConcurrentLegacyCopiesNeverOverwrite(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "native", "config.jsonc")
	var workers sync.WaitGroup
	for _, content := range []string{"first", "second"} {
		source := filepath.Join(dir, content)
		if err := os.WriteFile(source, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		workers.Go(func() {
			if err := copyLegacyConfig(source, destination); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	data, err := os.ReadFile(destination)
	if err != nil || (string(data) != "first" && string(data) != "second") {
		t.Fatalf("migration published a partial file: %q, %v", data, err)
	}
}

func TestLogStorageRejectsSymlinks(t *testing.T) {
	settingsTestHome(t)
	foreign := filepath.Join(t.TempDir(), "foreign.jsonl")
	const content = `{"event":"private-foreign-event"}` + "\n"
	if err := os.WriteFile(foreign, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(runLogFilePath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, runLogFilePath()); err != nil {
		t.Fatal(err)
	}
	log := openRunLog("test")
	defer log.close("test")
	if log.file != nil {
		t.Fatal("logger opened a symlink")
	}
	log.diagnostic("must-not-write", "ok", "test")
	if _, err := log.records(); err == nil {
		t.Fatal("log reader followed a symlink")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != content {
		t.Fatal("log writer changed a symlink target", err)
	}
}

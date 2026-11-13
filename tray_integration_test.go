package voicetype

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

type trayRegistration struct {
	sender string
	path   dbus.ObjectPath
}
type testTrayHost struct{ registrations chan trayRegistration }

func (h *testTrayHost) RegisterStatusNotifierItem(sender dbus.Sender, path string) *dbus.Error {
	h.registrations <- trayRegistration{string(sender), dbus.ObjectPath(path)}
	return nil
}

type trayMenuLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

func findTrayItem(layout trayMenuLayout, label string) int32 {
	if v, ok := layout.Properties["label"]; ok && v.Value() == label {
		return layout.ID
	}
	for _, value := range layout.Children {
		var child trayMenuLayout
		if dbus.Store([]any{value.Value()}, &child) == nil {
			if id := findTrayItem(child, label); id >= 0 {
				return id
			}
		}
	}
	return -1
}

// Exercises the packaged native app's real StatusNotifierItem/DBusMenu exports
// on a private session bus. No extension or desktop setting is installed.
func TestNativeTray(t *testing.T) {
	binary := os.Getenv("VOICE_TYPE_DESKTOP_TEST_BINARY")
	if binary == "" {
		t.Skip("set VOICE_TYPE_DESKTOP_TEST_BINARY to the built native app")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	display := os.Getenv("WAYLAND_DISPLAY")
	if display != "" && !filepath.IsAbs(display) {
		t.Setenv("WAYLAND_DISPLAY", filepath.Join(runtime, display))
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(home, "runtime"))
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := os.MkdirAll(os.Getenv("XDG_RUNTIME_DIR"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GSETTINGS_BACKEND", "keyfile")
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	// The uninstaller also clears keyring entries; never touch the real keyring.
	t.Setenv("VOICE_TYPE_NO_KEYRING", "1")
	// Seed another app before launch and verify the real process preserves it.
	gradia := "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/be.alexandervanhee.gradia/"
	registered := "['" + gradia + "']"
	for _, args := range [][]string{
		{"set", mediaKeysSchema, "custom-keybindings", registered},
		{"set", customKeySchema + ":" + gradia, "name", "'Gradia'"},
		{"set", customKeySchema + ":" + gradia, "command", "'gradia --screenshot=INTERACTIVE'"},
		{"set", customKeySchema + ":" + gradia, "binding", "'Print'"},
	} {
		if output, err := exec.Command("gsettings", args...).CombinedOutput(); err != nil {
			t.Fatalf("seed private settings: %v %s", err, output)
		}
	}
	bus := exec.CommandContext(ctx, "dbus-daemon", "--session", "--nofork", "--print-address=1")
	bus.Env = os.Environ()
	stdout, err := bus.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = bus.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		bus.Process.Kill()
		bus.Wait()
		// GNOME may activate its document portal on the private bus. Its FUSE
		// mount must not outlive that bus or obstruct TempDir cleanup.
		for _, mount := range []string{"doc", "gvfs"} {
			_ = exec.Command("fusermount3", "-uz", filepath.Join(home, "runtime", mount)).Run()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("no private bus address")
	}
	address := scanner.Text()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	conn, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	host := &testTrayHost{registrations: make(chan trayRegistration, 10)}
	if err := conn.Export(host, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	defer listener.Close() // The legacy port remains deliberately occupied.
	config := []byte(`{"port":` + strconv.Itoa(port) + `}`)
	if err := os.WriteFile(filepath.Join(home, ".voice-type-config.jsonc"), config, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			command.Process.Kill()
			<-done
		}
	}()
	var registration trayRegistration
	select {
	case registration = <-host.registrations:
	case err := <-done:
		stopped = true
		t.Fatalf("app exited: %v %s", err, output.String())
	case <-ctx.Done():
		t.Fatal("tray did not register")
	}
	item := conn.Object(registration.sender, registration.path)
	var properties map[string]dbus.Variant
	if err := item.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.kde.StatusNotifierItem").Store(&properties); err != nil {
		t.Fatal(err)
	}
	if _, ok := properties["IconPixmap"]; !ok {
		t.Fatal("tray did not provide an icon")
	}
	menu := conn.Object(registration.sender, properties["Menu"].Value().(dbus.ObjectPath))
	var revision uint32
	var layout trayMenuLayout
	if err := menu.Call("com.canonical.dbusmenu.GetLayout", 0, int32(0), int32(-1), []string{"label", "enabled"}).Store(&revision, &layout); err != nil {
		t.Fatal(err)
	}
	if len(layout.Children) != 4 {
		t.Fatalf("expected four tray rows, got %d", len(layout.Children))
	}
	for _, label := range []string{"Idle", "Record", "Settings", "Quit"} {
		if findTrayItem(layout, label) < 0 {
			t.Fatalf("missing menu action %q", label)
		}
	}
	// Registration can arrive before startup finishes configuring the shortcut.
	expected := "['" + gradia + "', '" + shortcutBase + "transcription/']\n"
	for {
		value, err := exec.Command("gsettings", "get", mediaKeysSchema, "custom-keybindings").Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(value) == expected {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("app startup lost the other shortcut registration")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	// A tray registration precedes ApplicationStarted. Wait for the native event
	// loop rather than treating successful one-way forwarding as completed work.
	countEvent := func(event string) int {
		raw, err := os.ReadFile(filepath.Join(home, ".voice-type-logs.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, row := range bytes.Split(raw, []byte("\n")) {
			var record struct{ Event, Outcome string }
			if json.Unmarshal(row, &record) == nil && record.Event == event {
				if event == "dictation.completed" && record.Outcome != "configuration_error" {
					t.Fatal("unexpected provider call")
				}
				count++
			}
		}
		return count
	}
	waitEvent := func(event string, count int) {
		for countEvent(event) < count {
			if ctx.Err() != nil {
				t.Fatalf("timed out waiting for %s; native output: %s", event, output.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitEvent("desktop.started", 1)
	// Packaged secondary launches exit promptly and target this same engine.
	attempts := 0
	for _, args := range [][]string{{"--background"}, {}, {"--toggle"}, {"--toggle"}} {
		if value, err := exec.CommandContext(ctx, binary, args...).CombinedOutput(); err != nil {
			t.Fatalf("secondary command %v: %v %s", args, err, value)
		}
		if len(args) > 0 && (args[0] == "--toggle") {
			attempts++
			waitEvent("dictation.completed", attempts)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "runtime", "voice-type", "desktop.sock")); !os.IsNotExist(err) {
		t.Fatal("desktop created management socket")
	}
	// The desktop process must not own any TCP LISTEN descriptor.
	fds, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", command.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	owned := map[string]bool{}
	for _, fd := range fds {
		value, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", command.Process.Pid, fd.Name()))
		owned[value] = true
	}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		raw, err := os.ReadFile(table)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n")[1:] {
			columns := strings.Fields(line)
			if len(columns) > 9 && columns[3] == "0A" && owned["socket:["+columns[9]+"]"] {
				t.Fatal("desktop owns TCP listener")
			}
		}
	}
	// Configuration failures above change tray state. Wait for its updated menu
	// and ensure a click from the original layout still addresses the same action.
	for {
		var updatedRevision uint32
		var updated trayMenuLayout
		if err := menu.Call("com.canonical.dbusmenu.GetLayout", 0, int32(0), int32(-1), []string{"label", "enabled"}).Store(&updatedRevision, &updated); err != nil {
			t.Fatal(err)
		}
		if updatedRevision > revision {
			for _, label := range []string{"Settings", "Quit"} {
				if findTrayItem(layout, label) != findTrayItem(updated, label) {
					t.Fatalf("state update replaced stable action ID for %s", label)
				}
			}
			break
		}
		if ctx.Err() != nil {
			t.Fatal("tray state update was not published")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Trigger actual native menu callbacks through their published protocol.
	if err := menu.Call("com.canonical.dbusmenu.Event", 0, findTrayItem(layout, "Settings"), "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	if err := menu.Call("com.canonical.dbusmenu.Event", 0, findTrayItem(layout, "Quit"), "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatalf("quit: %v %s", err, output.String())
		}
	case <-ctx.Done():
		t.Fatal("tray quit did not stop app")
	}
	// Install the actual native binary into a private clean home, then upgrade
	// an old companion and uninstall while it is running. No rebuild is needed.
	source := filepath.Join(home, "source")
	if err := os.MkdirAll(filepath.Join(source, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []struct {
		from, to string
		mode     os.FileMode
	}{
		{"install.sh", filepath.Join(source, "install.sh"), 0755},
		{"uninstall.sh", filepath.Join(source, "uninstall.sh"), 0755},
		{"icon.png", filepath.Join(source, "icon.png"), 0644},
		{binary, filepath.Join(source, "build/voice-type"), 0755},
		{filepath.Join(filepath.Dir(binary), "LICENSE"), filepath.Join(source, "build/LICENSE"), 0644},
		{filepath.Join(filepath.Dir(binary), "THIRD-PARTY-LICENSES.md"), filepath.Join(source, "build/THIRD-PARTY-LICENSES.md"), 0644},
	} {
		data, err := os.ReadFile(asset.from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(asset.to, data, asset.mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module example.test/app\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "tools/make"), []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(source, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"))
	binDir := filepath.Join(home, "bin")
	t.Setenv("VOICE_TYPE_BIN_DIR", binDir)
	configFile := filepath.Join(home, ".voice-type-config.jsonc")
	if _, err := os.Stat(configFile); err != nil {
		t.Fatal(err)
	}
	for iteration := range 2 {
		if iteration == 1 {
			if err := os.WriteFile(filepath.Join(binDir, "voice-type-app"), []byte("old-companion"), 0755); err != nil {
				t.Fatal(err)
			}
		}
		value, err := exec.CommandContext(ctx, filepath.Join(source, "install.sh"), "--local").CombinedOutput()
		if err != nil {
			t.Fatalf("native install/upgrade: %v %s", err, value)
		}
	}
	if _, err := os.Stat(filepath.Join(binDir, "voice-type-app")); !os.IsNotExist(err) {
		t.Fatal("companion survived upgrade")
	}
	installed := exec.CommandContext(ctx, filepath.Join(binDir, "voice-type"), "--background")
	if err := installed.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { installed.Process.Kill(); installed.Wait() }()
	waitEvent("desktop.started", 2)
	if value, err := exec.CommandContext(ctx, filepath.Join(source, "uninstall.sh")).CombinedOutput(); err != nil {
		t.Fatalf("native uninstall: %v %s", err, value)
	}
	if err := installed.Wait(); err != nil {
		t.Fatal("uninstall did not quit the application", err)
	}
	if _, err := os.Stat(filepath.Join(binDir, "voice-type")); !os.IsNotExist(err) {
		t.Fatal("binary survived uninstall")
	}
	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		t.Fatal("uninstall kept the configuration file", err)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "voice-type")); !os.IsNotExist(err) {
		t.Fatal("uninstall kept the application data", err)
	}
	for _, check := range []struct{ schema, key, want string }{
		{mediaKeysSchema, "custom-keybindings", registered + "\n"},
		{customKeySchema + ":" + gradia, "name", "'Gradia'\n"},
		{customKeySchema + ":" + gradia, "command", "'gradia --screenshot=INTERACTIVE'\n"},
		{customKeySchema + ":" + gradia, "binding", "'Print'\n"},
	} {
		value, err := exec.Command("gsettings", "get", check.schema, check.key).Output()
		if err != nil || string(value) != check.want {
			t.Fatalf("another app's %s changed: %q %v", check.key, value, err)
		}
	}

}

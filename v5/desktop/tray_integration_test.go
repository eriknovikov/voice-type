package desktop

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
		_ = exec.Command("fusermount3", "-uz", filepath.Join(home, "runtime", "doc")).Run()
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
	listener.Close()
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
	for _, label := range []string{"Idle", "Start recording", "Open settings", "Quit"} {
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
	// Trigger actual native menu callbacks through their published protocol.
	if err := menu.Call("com.canonical.dbusmenu.Event", 0, findTrayItem(layout, "Open settings"), "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
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
	// Exercise the actual uninstaller entry point too, entirely in the keyfile.
	if output, err := exec.Command(binary, "--remove-shortcuts").CombinedOutput(); err != nil {
		t.Fatalf("remove shortcuts: %v %s", err, output)
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

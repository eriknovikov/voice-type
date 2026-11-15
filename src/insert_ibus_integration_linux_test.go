package voicetype

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// Opt in with VOICE_TYPE_TEST_IBUS_DAEMON=/absolute/path/to/ibus-daemon.
// The daemon may be an uninstalled libtool wrapper (which loads its matching
// libibus). Nothing connects to or replaces the desktop's session or IBus bus.
func TestIBusIntegration(t *testing.T) {
	daemon := os.Getenv("VOICE_TYPE_TEST_IBUS_DAEMON")
	if daemon == "" {
		t.Skip("set VOICE_TYPE_TEST_IBUS_DAEMON to test a real isolated IBus daemon")
	}
	if !filepath.IsAbs(daemon) {
		t.Fatal("VOICE_TYPE_TEST_IBUS_DAEMON must be an absolute executable path")
	}
	versionCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	version, err := exec.CommandContext(versionCtx, daemon, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("IBus version: %v: %s", err, version)
	}
	t.Log(strings.TrimSpace(string(version)))
	address, daemonProcess := startTestIBus(t, daemon)
	control := testIBusConnection(t, address)
	const baseline = "insert-pure-testbaseline"
	registerTestIBusEngine(t, control, baseline)
	testIBusCall(t, control, ibusPath, ibusService+".SetGlobalEngine", baseline)
	client := newTestIBusClient(t, address, "voice-type-test-client", 0)
	client.focus(t)

	t.Run("SetupDeadline", func(t *testing.T) {
		// Pause only our child daemon to hold authentication in flight. This
		// exercises deadline cancellation without timing setup against a sleep.
		if err := daemonProcess.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		defer daemonProcess.Signal(syscall.SIGCONT)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		started := time.Now()
		inserter, err := NewTextInserter(ctx, InsertBackendIBus)
		if inserter != nil {
			_ = inserter.Close()
		}
		// The socket deadline can fire just before the context's timer.
		// Both are valid ways to interrupt the blocked authentication.
		elapsed := time.Since(started)
		if err == nil || elapsed < 90*time.Millisecond || elapsed > time.Second {
			t.Fatalf("in-flight setup did not honor its deadline: %v after %v", err, elapsed)
		}
		if err := daemonProcess.Signal(syscall.SIGCONT); err != nil {
			t.Fatal(err)
		}
		assertTestIBusRestored(t, control, baseline)
		client.wantNoCommit(t)
	})

	t.Run("Delivery", func(t *testing.T) {
		inserter := testIBusInserter(t)
		for _, text := range []string{"hello", "日本語 café 🦊\nsecond line", strings.Repeat("long text ", 8192)} {
			engine := inserter.engine
			err := inserter.Insert(context.Background(), text)
			if err != nil {
				if engine != nil {
					engine.mu.Lock()
					t.Logf("focus=%q client=%q enabled=%v content=%+v", engine.focus, engine.client, engine.enabled, engine.content)
					engine.mu.Unlock()
				}
				t.Fatal(err)
			}
			client.wantCommit(t, text)
			assertTestIBusRestored(t, control, baseline)
		}
		if err := inserter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := inserter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := inserter.Insert(context.Background(), "closed"); err != ErrTextInserterClosed {
			t.Fatalf("closed Insert: %v", err)
		}
	})

	for _, purpose := range []uint32{8, 9} {
		t.Run(fmt.Sprintf("Protected%d", purpose), func(t *testing.T) {
			protected := newTestIBusClient(t, address, "voice-type-protected-client", purpose)
			protected.focus(t)
			inserter := testIBusInserter(t)
			if err := inserter.Insert(context.Background(), "must not insert"); err == nil {
				t.Fatal("protected field accepted insertion")
			}
			protected.wantNoCommit(t)
			assertTestIBusRestored(t, control, baseline)
			client.focus(t)
		})
	}

	t.Run("Canceled", func(t *testing.T) {
		inserter := testIBusInserter(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := inserter.Insert(ctx, "canceled"); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled insertion: %v", err)
		}
		client.wantNoCommit(t)
		assertTestIBusRestored(t, control, baseline)
		if err := inserter.Insert(context.Background(), "after cancellation"); err != nil {
			t.Fatal(err)
		}
		client.wantCommit(t, "after cancellation")
		assertTestIBusRestored(t, control, baseline)
	})

	t.Run("InvalidInputRestoresEngine", func(t *testing.T) {
		inserter := testIBusInserter(t)
		for _, text := range []string{"", "\xff", "a\x00b"} {
			if err := inserter.Insert(context.Background(), text); err == nil {
				t.Fatalf("invalid input accepted: %q", text)
			}
			client.wantNoCommit(t)
			assertTestIBusRestored(t, control, baseline)
		}
		if err := inserter.Insert(context.Background(), "after invalid input"); err != nil {
			t.Fatal(err)
		}
		client.wantCommit(t, "after invalid input")
		assertTestIBusRestored(t, control, baseline)
	})

	t.Run("FocusMovesBeforeInsert", func(t *testing.T) {
		inserter := testIBusInserter(t)
		other := newTestIBusClient(t, address, "voice-type-other-client", 0)
		other.focus(t)
		if err := inserter.Insert(context.Background(), "new focus"); err != nil {
			t.Fatal(err)
		}
		other.wantCommit(t, "new focus")
		client.wantNoCommit(t)
		assertTestIBusRestored(t, control, baseline)
		client.focus(t)
	})

	t.Run("FocusMovesToProtectedBeforeInsert", func(t *testing.T) {
		inserter := testIBusInserter(t)
		protected := newTestIBusClient(t, address, "voice-type-password-client", 8)
		protected.focus(t)
		if err := inserter.Insert(context.Background(), "must not leak"); err == nil {
			t.Fatal("focus transition to a password field accepted insertion")
		}
		protected.wantNoCommit(t)
		client.wantNoCommit(t)
		assertTestIBusRestored(t, control, baseline)
		client.focus(t)
	})

	t.Run("FocusCycleRetainsCachedState", func(t *testing.T) {
		inserter := testIBusInserter(t)
		testIBusCall(t, client.connection, client.path, ibusService+".InputContext.FocusOut")
		client.focus(t)
		if err := inserter.Insert(context.Background(), "focus cycle"); err != nil {
			t.Fatal(err)
		}
		client.wantCommit(t, "focus cycle")
		assertTestIBusRestored(t, control, baseline)
	})

	t.Run("ConcurrentClose", func(t *testing.T) {
		inserter := testIBusInserter(t)
		start := make(chan struct{})
		inserted := make(chan error, 1)
		closed := make(chan error, 2)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go func() { <-start; inserted <- inserter.Insert(ctx, "concurrent close") }()
		for range 2 {
			go func() { <-start; closed <- inserter.Close() }()
		}
		close(start)
		var insertErr error
		select {
		case insertErr = <-inserted:
		case <-ctx.Done():
			t.Fatal("Insert deadlocked with Close")
		}
		for range 2 {
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("Close deadlocked")
			}
		}
		if insertErr == nil {
			client.wantCommit(t, "concurrent close")
		} else if errors.Is(insertErr, ErrTextInserterClosed) {
			client.wantNoCommit(t)
		} else {
			t.Fatal(insertErr)
		}
		assertTestIBusRestored(t, control, baseline)
	})

	t.Run("PreserveUserEngine", func(t *testing.T) {
		other := testIBusConnection(t, address)
		const selected = "insert-pure-testselection"
		registerTestIBusEngine(t, other, selected)
		inserter := testIBusInserter(t)
		testIBusCall(t, control, ibusPath, ibusService+".SetGlobalEngine", selected)
		if err := inserter.Close(); err != nil {
			t.Fatal(err)
		}
		assertTestIBusRestored(t, control, selected)
		testIBusCall(t, control, ibusPath, ibusService+".SetGlobalEngine", baseline)
	})
}

func startTestIBus(t *testing.T, daemon string) (string, *os.Process) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"config", "cache", "runtime", "data", "components"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Older daemons treat unix:path's filename as a directory. unix:dir
	// works across versions; discover the resulting socket from metadata.
	listen := "unix:dir=" + filepath.Join(root, "runtime")
	// Set overrides before spawning either child. In particular an inherited
	// IBUS_ADDRESS_FILE or desktop address must never reach the test daemon.
	for name, value := range map[string]string{
		"HOME": root, "XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_CACHE_HOME": filepath.Join(root, "cache"), "XDG_RUNTIME_DIR": filepath.Join(root, "runtime"),
		"XDG_DATA_HOME": filepath.Join(root, "data"), "IBUS_COMPONENT_PATH": filepath.Join(root, "components"),
		"IBUS_ADDRESS": "", "IBUS_ADDRESS_FILE": filepath.Join(root, "address"),
		"DISPLAY": "", "WAYLAND_DISPLAY": "", "GIO_USE_VFS": "local",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=" + filepath.Join(root, "session"),
	} {
		t.Setenv(name, value)
	}
	// No standard service directories: test startup must not activate host
	// portal, GVfs, or desktop services even on this private session bus.
	config := filepath.Join(root, "session.conf")
	xml := fmt.Sprintf(`<busconfig><type>session</type><listen>%s</listen><auth>EXTERNAL</auth><policy context="default"><allow user="*"/><allow own="*"/><allow send_destination="*"/><allow receive_sender="*"/></policy></busconfig>`, os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
	if err := os.WriteFile(config, []byte(xml), 0600); err != nil {
		t.Fatal(err)
	}
	bus := exec.Command("dbus-daemon", "--config-file="+config, "--nofork", "--print-address=1")
	stdout, err := bus.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	startTestIBusProcess(t, bus)
	startup, cancelStartup := context.WithTimeout(context.Background(), 5*time.Second)
	stop := context.AfterFunc(startup, func() { _ = stdout.Close() })
	defer cancelStartup()
	defer stop()
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("private session bus: %v", err)
	}
	args := []string{"--single", "--panel=disable", "--config=disable", "--cache=none", "--address=" + listen}
	help, err := exec.CommandContext(startup, daemon, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("IBus options: %v: %s", err, help)
	}
	if strings.Contains(string(help), "--emoji-extension") {
		args = append(args, "--emoji-extension=disable")
	}
	cmd := exec.Command(daemon, args...)
	startTestIBusProcess(t, cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		address, err := ibusAddress(os.Getenv, readIBusMetadata, func(pid int) bool { return syscall.Kill(pid, 0) == nil })
		if err == nil {
			c, connectErr := connectIBus(ctx, address, nil)
			err = connectErr
			if err == nil {
				c.close()
				t.Setenv("IBUS_ADDRESS", address)
				return address, cmd.Process
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("private IBus did not start: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func startTestIBusProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	log, err := os.CreateTemp(t.TempDir(), "process-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Logf("%s: %s", cmd.Path, data)
		}
		_ = log.Close()
	})
	if cmd.Stdout == nil {
		cmd.Stdout = log
	}
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
}

func testIBusConnection(t *testing.T, address string) *ibusConnection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := connectIBus(ctx, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	return c
}

func testIBusCall(t *testing.T, c *ibusConnection, path dbus.ObjectPath, method string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Object(ibusService, path).CallWithContext(ctx, method, 0, args...).Store(); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

func registerTestIBusEngine(t *testing.T, c *ibusConnection, name string) {
	t.Helper()
	engine := &insertPureEngine{name: name, deadline: time.Now().Add(time.Hour)}
	if err := engine.export(c); err != nil {
		t.Fatal(err)
	}
	// Unlike the single-use insertion engine, the baseline can be recreated
	// whenever cleanup restores it. Its callbacks only need to reply.
	if err := c.ExportMethodTable(map[string]any{"CreateEngine": func(_ string) (dbus.ObjectPath, *dbus.Error) {
		return ibusEnginePath, nil
	}}, ibusFactoryPath, ibusService+".Factory"); err != nil {
		t.Fatal(err)
	}
	testIBusCall(t, c, ibusPath, ibusService+".RegisterComponent", insertPureComponent(name))
}

func testIBusInserter(t *testing.T) *ibusTextInserter {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inserter, err := NewTextInserter(ctx, InsertBackendIBus)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := inserter.Close(); err != nil {
			t.Error(err)
		}
	})
	return inserter.(*ibusTextInserter)
}

type testIBusClient struct {
	connection *ibusConnection
	path       dbus.ObjectPath
	signals    chan *dbus.Signal
}

func newTestIBusClient(t *testing.T, address, name string, purpose uint32) *testIBusClient {
	t.Helper()
	c := testIBusConnection(t, address)
	var path dbus.ObjectPath
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Object(ibusService, ibusPath).CallWithContext(ctx, ibusService+".CreateInputContext", 0, name).Store(&path); err != nil {
		t.Fatal(err)
	}
	client := &testIBusClient{connection: c, path: path, signals: make(chan *dbus.Signal, 64)}
	c.Signal(client.signals)
	if err := c.AddMatchSignalContext(ctx, dbus.WithMatchInterface(ibusService+".InputContext"), dbus.WithMatchObjectPath(path)); err != nil {
		t.Fatal(err)
	}
	testIBusCall(t, c, path, ibusService+".InputContext.SetCapabilities", uint32(1<<3)) // IBUS_CAP_FOCUS
	testIBusCall(t, c, path, ibusProperties+".Set", ibusService+".InputContext", "ContentType", dbus.MakeVariant(ibusContentType{Purpose: purpose}))
	return client
}

func (c *testIBusClient) focus(t *testing.T) {
	t.Helper()
	// Toolkits maintain each context's has_focus flag themselves. Merely
	// focusing a different context does not clear the previous client's flag;
	// reset it before returning to a client so FocusIn is not a daemon no-op.
	testIBusCall(t, c.connection, c.path, ibusService+".InputContext.FocusOut")
	testIBusCall(t, c.connection, c.path, ibusService+".InputContext.FocusIn")
}

func (c *testIBusClient) wantCommit(t *testing.T, text string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case signal, ok := <-c.signals:
			if !ok {
				t.Fatal("IBus input client disconnected")
			}
			if signal.Name != ibusService+".InputContext.CommitText" {
				continue
			}
			var got ibusText
			if len(signal.Body) != 1 || ibusStore(signal.Body[0], &got) != nil || got.Type != "IBusText" || got.Text != text {
				t.Fatalf("incorrect CommitText payload: %#v", signal.Body)
			}
			c.wantNoCommit(t)
			return
		case <-timer.C:
			t.Fatal("application did not receive CommitText")
		}
	}
}

func (c *testIBusClient) wantNoCommit(t *testing.T) {
	t.Helper()
	timer := time.NewTimer(30 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case signal, ok := <-c.signals:
			if !ok {
				t.Fatal("IBus input client disconnected")
			}
			if signal.Name == ibusService+".InputContext.CommitText" {
				t.Fatalf("unexpected or duplicate CommitText: %#v", signal.Body)
			}
		case <-timer.C:
			return
		}
	}
}

func assertTestIBusRestored(t *testing.T, c *ibusConnection, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	current, err := c.globalEngine(ctx)
	if err != nil || current != name {
		t.Fatalf("restored engine = %q, %v; want %q", current, err, name)
	}
	value, err := c.property(ctx, "ActiveEngines")
	var engines []dbus.Variant
	if err != nil || ibusStore(value, &engines) != nil {
		t.Fatalf("active engines: %v", err)
	}
	for _, value := range engines {
		active, err := ibusEngineName(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(active, "insert-pure-") && active != "insert-pure-testbaseline" && active != "insert-pure-testselection" {
			t.Fatalf("temporary engine remains registered: %s", active)
		}
	}
}

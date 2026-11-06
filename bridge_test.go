package voicetype

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestDesktopAutostartPreferences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app := application.New(application.Options{Name: "Voice Type"})
	// An unchanged shortcut does not contact the live desktop portal in this test.
	prefs := Preferences{Shortcut: "Ctrl+Space", SetupComplete: true}
	b := &Bridge{app: app, prefs: prefs}
	prefs.Autostart = true
	if err := b.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "autostart", "voice-type.desktop")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "--background") {
		t.Fatal("login launch would open a window")
	}
	if got := readPreferences(); got != prefs {
		t.Fatalf("preferences round trip: %+v", got)
	}
	info, _ := os.Stat(preferencesPath())
	if info.Mode().Perm() != 0600 {
		t.Fatal("preferences not private")
	}
	prefs.Autostart = false
	if err := b.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("autostart entry retained after disabling")
	}
}

func TestShortcutDefaultsAndMigration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	prefs := readPreferences()
	if prefs.Shortcut != "Ctrl+Space" {
		t.Fatal("incorrect defaults")
	}
	if err := os.MkdirAll(filepath.Dir(preferencesPath()), 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ old, want string }{{"Ctrl+Alt+Space", "Ctrl+Space"}, {"Ctrl+F8", "Ctrl+F8"}} {
		if err := os.WriteFile(preferencesPath(), []byte(`{"shortcut":"`+test.old+`","setup_complete":true}`), 0600); err != nil {
			t.Fatal(err)
		}
		got := readPreferences()
		if got.Shortcut != test.want {
			t.Fatalf("migration: %+v", got)
		}
	}
}

func isolatedPreferences(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestPreferencesDefaultsAndOptOut(t *testing.T) {
	isolatedPreferences(t)
	prefs, _, err := loadPreferences()
	if err != nil || !prefs.Autostart {
		t.Fatalf("default login startup: %+v %v", prefs, err)
	}
	if err := writePreferences(Preferences{Shortcut: "Ctrl+Space", Autostart: false}); err != nil {
		t.Fatal(err)
	}
	prefs, _, err = loadPreferences()
	if err != nil || prefs.Autostart {
		t.Fatalf("explicit opt out was lost: %+v %v", prefs, err)
	}
}

func TestPreferencesRecovery(t *testing.T) {
	for _, damaged := range []string{"", "{", `{"autostart":"yes"}`, `{"shortcut":42}`, `null`} {
		t.Run(damaged, func(t *testing.T) {
			isolatedPreferences(t)
			want := Preferences{Shortcut: "Ctrl+F8", Autostart: false, SetupComplete: true}
			if err := writePreferences(want); err != nil {
				t.Fatal(err)
			}
			if damaged == "" {
				if err := os.Remove(preferencesPath()); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(preferencesPath(), []byte(damaged), 0600); err != nil {
				t.Fatal(err)
			}
			got, notice, err := loadPreferences()
			if err != nil || got != want || notice == "" {
				t.Fatalf("recovery: %+v %q %v", got, notice, err)
			}
			again, notice, err := loadPreferences()
			if err != nil || again != want || notice != "" {
				t.Fatalf("repeated startup: %+v %q %v", again, notice, err)
			}
		})
	}
}

func TestInvalidPreferencesWithoutBackup(t *testing.T) {
	isolatedPreferences(t)
	if err := os.MkdirAll(filepath.Dir(preferencesPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preferencesPath(), []byte(`{"autostart":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, notice, err := loadPreferences()
	if err != nil || !got.Autostart || notice == "" {
		t.Fatalf("safe default: %+v %q %v", got, notice, err)
	}
}

func TestPreferencesPreserveUnknownFields(t *testing.T) {
	isolatedPreferences(t)
	if err := os.MkdirAll(filepath.Dir(preferencesPath()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preferencesPath(), []byte(`{"autostart":false,"future":{"enabled":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writePreferences(Preferences{Shortcut: "Ctrl+F9", Autostart: false}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(preferencesPath())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fields["future"]), `"enabled": true`) {
		t.Fatalf("unknown fields lost: %s", data)
	}
}

func TestPreferencesFailureRollsBackAutostart(t *testing.T) {
	isolatedPreferences(t)
	app := application.New(application.Options{Name: "Voice Type"})
	old := Preferences{Shortcut: "Ctrl+Space", Autostart: true}
	b := &Bridge{app: app, prefs: old}
	if err := b.applyAutostart(true); err != nil {
		t.Fatal(err)
	}
	// A directory in place of the preferences document must never be replaced.
	if err := os.MkdirAll(preferencesPath(), 0700); err != nil {
		t.Fatal(err)
	}
	next := old
	next.Autostart = false
	if err := b.SavePreferences(next); err == nil {
		t.Fatal("expected persistence error")
	}
	enabled, err := app.Autostart.IsEnabled()
	if err != nil || !enabled || b.prefs != old || b.preferencesError == "" {
		t.Fatalf("rollback: enabled=%v prefs=%+v error=%v", enabled, b.prefs, err)
	}
}

func TestWindowActionWaitsForFrontend(t *testing.T) {
	b := &Bridge{}
	b.RequestHide()
	if b.pendingWindowAction != "hide" {
		t.Fatal("hide was not queued")
	}
	b.RequestQuit()
	if b.pendingWindowAction != "quit" {
		t.Fatal("quit was not queued")
	}
	if err := b.CompleteWindowAction("hide"); err == nil {
		t.Fatal("accepted obsolete window action")
	}
}

func TestFreshPreferencesRegisterBackgroundStartup(t *testing.T) {
	isolatedPreferences(t)
	prefs, notice, err := loadPreferences()
	if err != nil {
		t.Fatal(err)
	}
	app := application.New(application.Options{Name: "Voice Type"})
	b := &Bridge{app: app, prefs: prefs, notice: notice}
	b.initializePreferences()
	if b.preferencesError != "" {
		t.Fatal(b.preferencesError)
	}
	enabled, err := app.Autostart.IsEnabled()
	if err != nil || !enabled {
		t.Fatalf("default was not registered: %v %v", enabled, err)
	}
	if got := readPreferences(); !got.Autostart {
		t.Fatal("default not persisted")
	}
}

func TestCurrentShortcutIsNotRemigrated(t *testing.T) {
	isolatedPreferences(t)
	want := Preferences{Shortcut: "Ctrl+Alt+Space", Autostart: false}
	if err := writePreferences(want); err != nil {
		t.Fatal(err)
	}
	if got := readPreferences(); got != want {
		t.Fatalf("current shortcut migrated again: %+v", got)
	}
}

func TestDiagnosticExportFollowsOutcome(t *testing.T) {
	records := []DiagnosticRecord{
		{Event: "dictation", Outcome: "ok"},
		{Event: "startup.failed", Outcome: "error", Message: "Another Voice Type engine is running."},
		{Event: "config.recovered", Outcome: "ok", Message: "Settings restored."},
	}
	for _, test := range []struct {
		filter string
		count  int
	}{{"all", 3}, {"errors", 1}, {"ok", 2}} {
		data, err := encodeDiagnosticLogs(records, test.filter)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(data), "\n"); got != test.count {
			t.Fatalf("%s: got %d records", test.filter, got)
		}
	}
	if _, err := encodeDiagnosticLogs(records, "query"); err == nil {
		t.Fatal("accepted unknown outcome")
	}
}

func TestShortcutDoesNotWaitForPreferenceChanges(t *testing.T) {
	isolatedPreferences(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	b := &Bridge{engine: service, app: application.New(application.Options{Name: "Voice Type"})}
	b.mu.Lock() // An Info request or preference change is in progress.
	done := make(chan struct{})
	go func() { b.activateShortcut(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		b.mu.Unlock()
		t.Fatal("shortcut blocked behind preference registration")
	}
	b.mu.Unlock()
	if b.lastShortcutAt.Load() == 0 {
		t.Fatal("ordinary shortcut was dropped")
	}
	deadline := time.Now().Add(time.Second)
	for {
		rows, err := service.Logs()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.Event == "dictation.completed" {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shortcut did not reach the engine")
		}
		time.Sleep(time.Millisecond)
	}
	last := b.lastShortcutAt.Load()
	b.capturingShortcut.Store(true)
	b.activateShortcut()
	if b.lastShortcutAt.Load() != last {
		t.Fatal("shortcut activated while capturing keys")
	}
}

func TestMicrophoneLevelsUseWailsEvents(t *testing.T) {
	isolatedPreferences(t)
	app := application.New(application.Options{Name: "Voice Type"})
	engine := newEngine(configDefaults(), nil, sttSettings{})
	recorder := &Recorder{meterOnly: true}
	recorder.acceptSamples(meterTone(16384, wavSampleRate/10, 0))
	engine.testRecorder, engine.state = recorder, stateTesting
	bridge := &Bridge{app: app, engine: engine}
	frames := make(chan float64, 32)
	unsubscribe := app.Event.On("microphone-level", func(event *application.CustomEvent) {
		if level, ok := event.Data.(float64); ok {
			select {
			case frames <- level:
			default:
			}
		}
	})
	defer unsubscribe()
	defer bridge.cancelLevelStream()
	defer engine.Close()
	bridge.startLevelStream()
	for range 2 {
		select {
		case level := <-frames:
			if level <= 0 || level > 1 {
				t.Fatal("invalid event level", level)
			}
		case <-time.After(time.Second):
			t.Fatal("meter event did not arrive")
		}
	}
	engine.StopMicrophoneTest()
	deadline := time.After(time.Second)
	for {
		select {
		case level := <-frames:
			if level == 0 {
				return
			}
		case <-deadline:
			t.Fatal("meter event did not reset after stopping")
		}
	}
}

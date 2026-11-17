package voicetype

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMenuBarStartupPresentation(t *testing.T) {
	for _, test := range []struct {
		platform      string
		command       Command
		ready, hidden bool
		initial       Command
	}{
		{"darwin", CommandShow, true, true, CommandBackground},
		{"darwin", CommandShow, false, false, CommandShow},
		{"darwin", CommandBackground, true, true, CommandBackground},
		{"darwin", CommandBackground, false, false, CommandBackground},
		{"darwin", CommandToggle, true, true, CommandToggle},
		{"darwin", CommandToggle, false, false, CommandShow},
		{"linux", CommandShow, true, false, CommandShow},
		{"linux", CommandBackground, true, true, CommandBackground},
		{"linux", CommandBackground, false, false, CommandBackground},
		{"linux", CommandToggle, false, false, CommandShow},
	} {
		hidden, initial := startupPresentation(test.platform, test.command, test.ready)
		if hidden != test.hidden || initial != test.initial {
			t.Fatalf("%+v: hidden=%v, initial=%v", test, hidden, initial)
		}
	}
}

func TestMacShortcutDefaultsNeverUseLinuxMigration(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		prefs, err := parsePreferencesForPlatform([]byte(`{}`), platform)
		if err != nil || prefs != defaultPreferences(platform) {
			t.Fatal("wrong platform defaults", platform, prefs, err)
		}
		prefs, err = parsePreferencesForPlatform([]byte(`{"shortcut":"Ctrl+Alt+Space"}`), platform)
		if err != nil || prefs.Shortcut != defaultPreferences(platform).Shortcut {
			t.Fatal("incorrect legacy platform migration", platform, prefs, err)
		}
		prefs, err = parsePreferencesForPlatform([]byte(`{"shortcut":"Ctrl+Space","preferences_version":1}`), platform)
		if err != nil || prefs.Shortcut != "Ctrl+Space" {
			t.Fatal("overwrote an explicitly saved shortcut", platform, prefs, err)
		}
	}
}

func TestNativeQuitWaitsForFlushAndDoesNotRecurse(t *testing.T) {
	bridge := &Bridge{}
	if !bridge.allowNativeQuit("darwin") {
		t.Fatal("startup failure could not exit")
	}
	bridge.engine = newEngine(configDefaults(), &fixtureRecorder{}, sttSettings{})
	if bridge.allowNativeQuit("darwin") {
		t.Fatal("native quit bypassed the frontend flush")
	}
	deadline := time.After(time.Second)
	for {
		bridge.mu.Lock()
		pending := bridge.pendingWindowAction
		bridge.mu.Unlock()
		if pending == "quit" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("native quit did not request a flush")
		case <-time.After(time.Millisecond):
		}
	}
	if err := bridge.CompleteWindowAction("quit"); err != nil {
		t.Fatal(err)
	}
	if !bridge.allowNativeQuit("darwin") || !bridge.finalQuit.Load() {
		t.Fatal("confirmed quit would recurse into another flush")
	}
	if !(&Bridge{}).allowNativeQuit("linux") {
		t.Fatal("Linux quit behavior changed")
	}
}

func TestSleepCancelsCaptureRetainsAudioAndNeverResumes(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.recorder = &fixtureRecorder{}
	service.mu.Lock()
	service.state, service.sessionCtx, service.sessionCancel = stateRecording, ctx, cancel
	service.mu.Unlock()
	service.setSuspended(true)
	deadline := time.After(2 * time.Second)
	for service.Status().State != "idle" {
		select {
		case <-deadline:
			t.Fatal("sleep did not stop capture")
		case <-time.After(time.Millisecond):
		}
	}
	if ctx.Err() == nil || !service.Status().LastAttemptFailed {
		t.Fatal("sleep did not cancel/mark the interrupted session")
	}
	paths, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "voice-type", "dictation-*.wav"))
	if err != nil || len(paths) != 1 {
		t.Fatal("interrupted audio was not retained once", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(data, WavEncode(make([]int16, 3200))) {
		t.Fatal("sleep recovery changed captured audio", err)
	}
	if err := service.Toggle(); err == nil || !strings.Contains(err.Error(), "asleep") {
		t.Fatal("capture restarted while asleep", err)
	}
	if err := service.StartMicrophoneTest(""); err == nil || !strings.Contains(err.Error(), "asleep") {
		t.Fatal("meter started while asleep", err)
	}
	service.setSuspended(false)
	if service.Status().State != "idle" || ctx.Err() == nil {
		t.Fatal("wake resumed a stale recording or insertion context")
	}
}

func TestNativeQuitRefusesBusySessionWithoutFinalGuard(t *testing.T) {
	bridge := &Bridge{engine: newEngine(configDefaults(), &fixtureRecorder{}, sttSettings{})}
	bridge.engine.state = stateRecording
	bridge.pendingWindowAction = "quit"
	if err := bridge.CompleteWindowAction("quit"); err == nil || bridge.finalQuit.Load() {
		t.Fatal("busy session bypassed quit protection", err)
	}
}

type sleepFinalizeStream struct {
	fakeLocalStream
	entered chan struct{}
	release chan struct{}
}

func (s *sleepFinalizeStream) finalize(context.Context) (string, string, error) {
	close(s.entered)
	<-s.release
	return "must not insert after wake", "must not insert after wake", nil
}

func TestSleepDuringTranscriptionCannotInsertAfterWake(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	native := &sleepFinalizeStream{entered: make(chan struct{}), release: make(chan struct{})}
	stream := newLocalStreamSession(ctx, streamingModeProcess, native, nil)
	service.mu.Lock()
	service.recorder = &fixtureRecorder{}
	service.state, service.sessionCtx, service.sessionCancel, service.activeStream = stateRecording, ctx, cancel, stream
	service.mu.Unlock()
	backend := DefaultInsertBackend()
	previous := textInserterBackends[backend]
	inserter := &recordingInserter{}
	textInserterBackends[backend] = func(context.Context) (TextInserter, error) { return inserter, nil }
	t.Cleanup(func() { textInserterBackends[backend] = previous })
	stopped := make(chan error, 1)
	go func() { stopped <- service.stopSessionMatching(0, false) }()
	<-native.entered
	service.setSuspended(true)
	service.setSuspended(false)
	close(native.release)
	if err := <-stopped; err == nil || !strings.Contains(err.Error(), "system sleep") {
		t.Fatal("sleep interruption was hidden", err)
	}
	if len(inserter.text) != 0 || ctx.Err() == nil || service.Status().State != "idle" {
		t.Fatal("transcription continued stale output after wake")
	}
}

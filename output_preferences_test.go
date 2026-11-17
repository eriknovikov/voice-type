package voicetype

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMacOutputPreferenceModeGuard(t *testing.T) {
	for _, mode := range []streamingMode{streamingModeNone, streamingModeProcess, streamingModeLive} {
		backend, err := insertionBackendForPreferences("darwin", Preferences{OutputMode: "paste"}, mode)
		if mode == streamingModeLive {
			if err == nil || !strings.Contains(err.Error(), "does not support live") || backend != "" {
				t.Fatal("paste silently downgraded live insertion", backend, err)
			}
		} else if err != nil || backend != InsertBackendMacPaste {
			t.Fatal("paste rejected a supported mode", backend, err)
		}
		for _, output := range []string{"", "direct"} {
			backend, err := insertionBackendForPreferences("darwin", Preferences{OutputMode: output}, mode)
			if err != nil || backend != InsertBackendMacDirect {
				t.Fatal("direct default changed", backend, err)
			}
		}
		backend, err = insertionBackendForPreferences("linux", Preferences{OutputMode: "paste"}, mode)
		if err != nil || backend != InsertBackendIBus {
			t.Fatal("Mac output preference changed Linux", backend, err)
		}
	}
	if _, err := insertionBackendForPreferences("darwin", Preferences{OutputMode: "unexpected"}, streamingModeNone); err == nil {
		t.Fatal("unknown output mode silently fell back")
	}
}

func TestOutputPreferenceParsingAndPersistence(t *testing.T) {
	settingsTestHome(t)
	for _, data := range []string{`{"output_mode":null}`, `{"output_mode":false}`, `{"output_mode":"unexpected"}`} {
		if _, err := parsePreferences([]byte(data)); err == nil {
			t.Fatal("invalid output preference accepted", data)
		}
	}
	for _, mode := range []string{"", "direct", "paste"} {
		prefs := Preferences{Shortcut: "Ctrl+Space", SetupComplete: true, OutputMode: mode}
		if err := writePreferences(prefs); err != nil {
			t.Fatal(err)
		}
		got, _, err := loadPreferences()
		if err != nil || got != prefs {
			t.Fatal("output preference lost", got, err)
		}
	}
}

func TestFinalInsertionUsesSnapshottedBackend(t *testing.T) {
	backend := InsertBackendMacPaste
	previous, existed := textInserterBackends[backend]
	t.Cleanup(func() {
		if existed {
			textInserterBackends[backend] = previous
		} else {
			delete(textInserterBackends, backend)
		}
	})
	want := errors.New("snapshotted backend called")
	textInserterBackends[backend] = func(context.Context) (TextInserter, error) { return nil, want }
	if err := insertTextBackendTimed(context.Background(), "text", backend, nil); !errors.Is(err, want) {
		t.Fatal("final insertion ignored snapshotted output method", err)
	}
}

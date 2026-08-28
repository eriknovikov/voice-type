package voicetype

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bendahl/uinput"
)

func TestClipboardKeep(t *testing.T) {
	cases := []struct {
		name  string
		types []string
		want  string
	}{
		// The owner picks the advertised order, and leading with an X11 alias
		// is routine -- first-wins would drop the image here.
		{"image behind text aliases", []string{"UTF8_STRING", "STRING", "text/plain", "image/png"}, "image/png"},
		{"png beats other image encodings", []string{"image/bmp", "image/tiff", "image/png"}, "image/png"},
		{"image beats text", []string{"text/uri-list", "image/tiff"}, "image/tiff"},
		{"utf-8 beats bare text/plain", []string{"text/plain", "text/plain;charset=utf-8"}, "text/plain;charset=utf-8"},
		{"aliases alone are not restorable", []string{"UTF8_STRING", "STRING", "TEXT"}, ""},
		{"unknown binary is not restorable", []string{"application/x-krita"}, ""},
		{"empty offer", nil, ""},
	}
	for _, tc := range cases {
		if got := clipboardKeep(tc.types); got != tc.want {
			t.Errorf("%s: clipboardKeep(%q) = %q, want %q", tc.name, tc.types, got, tc.want)
		}
	}
}

// GNOME's clipboard manager reads each new offer before the target application.
func TestClipboardSurvivesFirstRead(t *testing.T) {
	if os.Getenv("VOICE_TYPE_LIVE") != "1" {
		t.Skip("set VOICE_TYPE_LIVE=1 to run the live clipboard test")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no Wayland session")
	}
	for _, bin := range []string{"wl-copy", "wl-paste"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}

	old, ok := saveClipboardContext(context.Background())
	if !ok {
		t.Skip("clipboard holds something we would not restore")
	}
	t.Cleanup(func() { restoreClipboard(old) })

	const sentinel = "voice-type clipboard regression sentinel"
	copy, err := startClipboardOffer(sentinel)
	if err != nil {
		t.Fatalf("start wl-copy: %v", err)
	}
	defer copy.stop()

	if err := awaitClipboardContext(context.Background(), sentinel, copy); err != nil {
		t.Fatalf("awaitClipboard: %v", err)
	}

	// awaitClipboard already consumed one read. Everything past here is the
	// read the target application still owes us.
	for i := 0; i < 3; i++ {
		time.Sleep(clipboardPasteGrace / 3)
		out, err := exec.Command("wl-paste", "--no-newline", "--type", clipboardMIME).Output()
		if err != nil {
			t.Fatalf("read %d after the first: %v", i+1, err)
		}
		if string(out) != sentinel {
			t.Fatalf("read %d after the first: got %q, want the sentinel", i+1, out)
		}
	}

	select {
	case <-copy.done:
		t.Fatal("wl-copy gave up ownership before the paste grace elapsed")
	default:
	}
}

func TestClipboardFailureReportsProcessAndFallsBackToTyping(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, dir, "wl-paste", `#!/bin/sh
if [ "$1" = "--list-types" ]; then
    printf '%s\n' 'text/plain;charset=utf-8'
else
    printf 'old clipboard'
fi
`)
	writeTestCommand(t, dir, "wl-copy", `#!/bin/sh
printf 'selection denied\n' >&2
exit 7
`)
	t.Setenv("PATH", dir)

	kb := newFakeKeyboard()
	typer := &Typer{kb: kb}
	sent, err := typer.pasteClipboardContext(context.Background(), "new transcript", savedClipboard{mime: clipboardMIME, data: []byte("old clipboard")})
	if err == nil {
		t.Fatal("pasteClipboard returned nil")
	}
	if sent {
		t.Error("pasteClipboard reported a chord before wl-copy claimed CLIPBOARD")
	}
	for _, want := range []string{"reaped=true", "exit status 7", "selection denied"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}

	kb.events = nil
	if err := typer.Paste("a"); err != nil {
		t.Fatalf("Paste fallback: %v", err)
	}
	if len(kb.events) == 0 || kb.events[0].key != typeKeyMap['a'].key {
		t.Fatalf("fallback did not type transcript: %#v", kb.events)
	}
}

func TestAwaitClipboardBoundsHungReader(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, dir, "wl-paste", "#!/bin/sh\nexec /bin/sleep 5\n")
	writeTestCommand(t, dir, "wl-copy", "#!/bin/sh\nexec /bin/sleep 5\n")
	t.Setenv("PATH", dir)

	copy, err := startClipboardOffer("transcript")
	if err != nil {
		t.Fatal(err)
	}
	defer copy.stop()

	started := time.Now()
	err = awaitClipboardContext(context.Background(), "transcript", copy)
	if err == nil || !strings.Contains(err.Error(), "did not claim") {
		t.Fatalf("awaitClipboard error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("hung reader blocked for %v, want bounded near %v", elapsed, clipboardClaimTimeout)
	}
}

func TestPasteFallsBackWhenChordNeverReachesPrimaryKey(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "clipboard-read")
	writeTestCommand(t, dir, "wl-paste", `#!/bin/sh
if [ "$1" = "--list-types" ]; then
    printf '%s\n' 'text/plain;charset=utf-8'
elif [ ! -f "$TEST_CLIPBOARD_STATE" ]; then
    : > "$TEST_CLIPBOARD_STATE"
    printf 'old clipboard'
else
    printf 'a'
fi
`)
	writeTestCommand(t, dir, "wl-copy", `#!/bin/sh
if [ "$1" = "--foreground" ]; then
    exec /bin/sleep 5
fi
exit 0
`)
	t.Setenv("PATH", dir)
	t.Setenv("TEST_CLIPBOARD_STATE", state)

	kb := newFakeKeyboard()
	kb.failAt = 1 // Ctrl down fails; V is never sent.
	typer := &Typer{kb: kb}
	if err := typer.Paste("a"); err != nil {
		t.Fatalf("Paste: %v", err)
	}
	if !containsEvent(kb.events, testKeyEvent{op: "down", key: uinput.KeyA}) {
		t.Fatalf("transcript was not typed after modifier failure: %#v", kb.events)
	}
}

func TestSaveClipboardDistinguishesEmptyFromOperationalFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		ok     bool
	}{
		{name: "empty", script: "#!/bin/sh\nprintf 'Nothing is copied\\n' >&2\nexit 1\n", ok: true},
		{name: "failure", script: "#!/bin/sh\nprintf 'Wayland disconnected\\n' >&2\nexit 7\n", ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestCommand(t, dir, "wl-paste", tc.script)
			t.Setenv("PATH", dir)
			_, ok := saveClipboardContext(context.Background())
			if ok != tc.ok {
				t.Errorf("saveClipboard ok = %t, want %t", ok, tc.ok)
			}
		})
	}
}

func TestPasteBoundsHungSaveAndTypesInstead(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, dir, "wl-paste", "#!/bin/sh\nexec /bin/sleep 5\n")
	writeTestCommand(t, dir, "wl-copy", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	kb := newFakeKeyboard()
	typer := &Typer{kb: kb}
	started := time.Now()
	if err := typer.Paste("a"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("hung clipboard save blocked for %v", elapsed)
	}
	if !containsEvent(kb.events, testKeyEvent{op: "down", key: uinput.KeyA}) {
		t.Fatalf("transcript was not typed after save timeout: %#v", kb.events)
	}
}

func TestRestoreClipboardIsBounded(t *testing.T) {
	dir := t.TempDir()
	writeTestCommand(t, dir, "wl-copy", "#!/bin/sh\nexec /bin/sleep 5\n")
	t.Setenv("PATH", dir)

	started := time.Now()
	restoreClipboard(savedClipboard{mime: clipboardMIME, data: []byte("old")})
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("hung clipboard restore blocked for %v", elapsed)
	}
}

func writeTestCommand(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

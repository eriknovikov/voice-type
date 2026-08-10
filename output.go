package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/bendahl/uinput"
)

// keyChord is a set of modifiers held around a single key.
type keyChord struct {
	Modifiers []int
	Key       int
}

var chordModifiers = map[string]int{
	"ctrl":    uinput.KeyLeftctrl,
	"control": uinput.KeyLeftctrl,
	"shift":   uinput.KeyLeftshift,
	"alt":     uinput.KeyLeftalt,
	"super":   uinput.KeyLeftmeta,
	"meta":    uinput.KeyLeftmeta,
	"cmd":     uinput.KeyLeftmeta,
}

var chordKeys = map[string]int{
	"a": uinput.KeyA, "b": uinput.KeyB, "c": uinput.KeyC, "d": uinput.KeyD,
	"e": uinput.KeyE, "f": uinput.KeyF, "g": uinput.KeyG, "h": uinput.KeyH,
	"i": uinput.KeyI, "j": uinput.KeyJ, "k": uinput.KeyK, "l": uinput.KeyL,
	"m": uinput.KeyM, "n": uinput.KeyN, "o": uinput.KeyO, "p": uinput.KeyP,
	"q": uinput.KeyQ, "r": uinput.KeyR, "s": uinput.KeyS, "t": uinput.KeyT,
	"u": uinput.KeyU, "v": uinput.KeyV, "w": uinput.KeyW, "x": uinput.KeyX,
	"y": uinput.KeyY, "z": uinput.KeyZ,
	"insert": uinput.KeyInsert,
	"enter":  uinput.KeyEnter,
	"space":  uinput.KeySpace,
}

// parseKeyChord turns "ctrl+shift+v" into modifiers plus a final key.
func parseKeyChord(s string) (keyChord, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "+")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return keyChord{}, fmt.Errorf("empty key chord %q", s)
	}

	var chord keyChord
	for i, p := range parts {
		p = strings.TrimSpace(p)
		last := i == len(parts)-1

		if !last {
			mod, ok := chordModifiers[p]
			if !ok {
				return keyChord{}, fmt.Errorf("unknown modifier %q in %q", p, s)
			}
			chord.Modifiers = append(chord.Modifiers, mod)
			continue
		}

		key, ok := chordKeys[p]
		if !ok {
			return keyChord{}, fmt.Errorf("unknown key %q in %q", p, s)
		}
		chord.Key = key
	}

	return chord, nil
}

// clipboardTool describes how to read and write the selection for the current
// session type.
type clipboardTool struct {
	Name     string
	CopyCmd  []string // extended with --sensitive when supported
	PasteCmd []string
	Wayland  bool
}

var waylandClipboard = clipboardTool{
	Name:     "wl-copy",
	CopyCmd:  []string{"wl-copy"},
	PasteCmd: []string{"wl-paste", "--no-newline"},
	Wayland:  true,
}

var x11Clipboard = clipboardTool{
	Name:     "xclip",
	CopyCmd:  []string{"xclip", "-selection", "clipboard"},
	PasteCmd: []string{"xclip", "-selection", "clipboard", "-o"},
	Wayland:  false,
}

// detectClipboardTool prefers Wayland when the session advertises it.
func detectClipboardTool() clipboardTool {
	if os.Getenv("WAYLAND_DISPLAY") != "" || strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
		return waylandClipboard
	}
	return x11Clipboard
}

// clipboardCopyArgs builds the copy command. --sensitive asks clipboard managers
// that implement CLIPBOARD_STATE to keep the transcript out of history; it is
// Wayland-only. --paste-once is deliberately never used: it breaks pasting into
// XWayland windows (Electron, JetBrains, Steam).
func clipboardCopyArgs(tool clipboardTool, sensitive bool) []string {
	args := append([]string(nil), tool.CopyCmd...)
	if sensitive && tool.Wayland {
		args = append(args, "--sensitive")
	}
	return args
}

// Typer holds the virtual keyboard, created once at daemon start (~201 ms) and
// reused for every dictation.
type Typer struct {
	kb    uinput.Keyboard
	tool  clipboardTool
	chord keyChord
	delay time.Duration
}

func newTyper(cfg Config) (*Typer, error) {
	chord, err := parseKeyChord(cfg.PasteKey)
	if err != nil {
		return nil, err
	}

	kb, err := uinput.CreateKeyboard("/dev/uinput", []byte("voice-type-vkbd"))
	if err != nil {
		return nil, fmt.Errorf("create virtual keyboard: %w", err)
	}

	return &Typer{
		kb:    kb,
		tool:  detectClipboardTool(),
		chord: chord,
		delay: time.Duration(cfg.PasteDelayMs) * time.Millisecond,
	}, nil
}

func (t *Typer) Close() {
	if t.kb != nil {
		t.kb.Close()
	}
}

// clipboardTimeout bounds every clipboard subprocess. The Wayland clipboard is
// client-owned, so wl-paste blocks indefinitely if the owning process is mid
// handoff or wedged -- without this, a single bad handoff would freeze /stop
// forever. Observed in testing.
const clipboardTimeout = 3 * time.Second

func (t *Typer) clipboardRead() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, t.tool.PasteCmd[0], t.tool.PasteCmd[1:]...).Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("%s timed out after %v", t.tool.PasteCmd[0], clipboardTimeout)
	}
	if err != nil {
		// An empty clipboard makes wl-paste exit non-zero; that is not an error
		// worth failing the paste over.
		return "", err
	}
	return string(out), nil
}

// clipboardWrite sets the selection. On Wayland wl-copy forks a process that
// must stay alive to serve the selection, so this waits only for the parent.
func (t *Typer) clipboardWrite(s string, sensitive bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()

	// wl-copy refuses empty input, so clearing needs its own flag. This is the
	// restore path when the clipboard started out empty.
	if s == "" && t.tool.Wayland {
		return exec.CommandContext(ctx, "wl-copy", "--clear").Run()
	}

	args := clipboardCopyArgs(t.tool, sensitive)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(s)
	cmd.Stderr = os.Stderr

	// wl-copy forks a process that must stay alive to serve the selection.
	// Cancel must not kill that survivor, so only the parent is waited on and
	// the context is used purely as a deadline on the parent's own exit.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s timed out after %v", args[0], clipboardTimeout)
		}
		return err
	}
	return nil
}

// sendChord presses the configured paste keystroke.
func (t *Typer) sendChord() error {
	for _, m := range t.chord.Modifiers {
		if err := t.kb.KeyDown(m); err != nil {
			return fmt.Errorf("key down: %w", err)
		}
	}
	err := t.kb.KeyPress(t.chord.Key)
	// Release modifiers even if the key press failed, so the desktop is never
	// left with a stuck Ctrl.
	for i := len(t.chord.Modifiers) - 1; i >= 0; i-- {
		if uerr := t.kb.KeyUp(t.chord.Modifiers[i]); uerr != nil && err == nil {
			err = fmt.Errorf("key up: %w", uerr)
		}
	}
	if err != nil {
		return err
	}
	return nil
}

// Paste puts text on the clipboard, sends the paste keystroke, then restores
// whatever was on the clipboard before.
func (t *Typer) Paste(text string) error {
	if text == "" {
		return nil
	}

	original, readErr := t.clipboardRead()
	if readErr != nil {
		logf("OUTPUT", "could not read existing clipboard (treating as empty): %v", readErr)
	}

	if err := t.clipboardWrite(text, true); err != nil {
		return fmt.Errorf("set clipboard: %w", err)
	}

	// Give the clipboard owner a moment to be ready to serve the selection
	// before the target app asks for it.
	time.Sleep(50 * time.Millisecond)

	if err := t.sendChord(); err != nil {
		return err
	}

	// The receiving app fetches clipboard data asynchronously after the
	// keystroke; restoring too early hands it the old contents.
	time.Sleep(t.delay)

	if readErr == nil {
		if err := t.clipboardWrite(original, false); err != nil {
			logf("OUTPUT", "clipboard restore failed, retrying: %v", err)
			if err := t.clipboardWrite(original, false); err != nil {
				// Worst case the transcript stays on the clipboard.
				logf("OUTPUT", "clipboard restore failed again: %v", err)
			}
		}
	}

	return nil
}

package main

import (
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/bendahl/uinput"
)

type keyStroke struct {
	modifiers []int
	key       int
}

const (
	typeKeyHold  = 8 * time.Millisecond
	typeKeyDelay = 2 * time.Millisecond
)

// typeKeyMap is the fixed US layout used by direct typing. The map is built
// from the physical keyboard rows so every shifted symbol shares its key with
// the corresponding unshifted symbol.
var typeKeyMap = buildTypeKeyMap()

func buildTypeKeyMap() map[rune]keyStroke {
	type keyRow struct {
		unshifted string
		shifted   string
		keys      []int
	}

	rows := []keyRow{
		{
			unshifted: "`1234567890-=",
			shifted:   "~!@#$%^&*()_+",
			keys: []int{
				uinput.KeyGrave,
				uinput.Key1, uinput.Key2, uinput.Key3, uinput.Key4, uinput.Key5,
				uinput.Key6, uinput.Key7, uinput.Key8, uinput.Key9, uinput.Key0,
				uinput.KeyMinus, uinput.KeyEqual,
			},
		},
		{
			unshifted: "qwertyuiop[]\\",
			shifted:   "QWERTYUIOP{}|",
			keys: []int{
				uinput.KeyQ, uinput.KeyW, uinput.KeyE, uinput.KeyR, uinput.KeyT,
				uinput.KeyY, uinput.KeyU, uinput.KeyI, uinput.KeyO, uinput.KeyP,
				uinput.KeyLeftbrace, uinput.KeyRightbrace, uinput.KeyBackslash,
			},
		},
		{
			unshifted: "asdfghjkl;'",
			shifted:   "ASDFGHJKL:\"",
			keys: []int{
				uinput.KeyA, uinput.KeyS, uinput.KeyD, uinput.KeyF, uinput.KeyG,
				uinput.KeyH, uinput.KeyJ, uinput.KeyK, uinput.KeyL,
				uinput.KeySemicolon, uinput.KeyApostrophe,
			},
		},
		{
			unshifted: "zxcvbnm,./",
			shifted:   "ZXCVBNM<>?",
			keys: []int{
				uinput.KeyZ, uinput.KeyX, uinput.KeyC, uinput.KeyV, uinput.KeyB,
				uinput.KeyN, uinput.KeyM, uinput.KeyComma, uinput.KeyDot, uinput.KeySlash,
			},
		},
	}

	keyMap := make(map[rune]keyStroke, 95)
	shift := []int{uinput.KeyLeftshift}
	for _, row := range rows {
		unshifted := []rune(row.unshifted)
		shifted := []rune(row.shifted)
		if len(unshifted) != len(shifted) || len(unshifted) != len(row.keys) {
			panic("invalid static US keyboard row")
		}
		for i, key := range row.keys {
			keyMap[unshifted[i]] = keyStroke{key: key}
			keyMap[shifted[i]] = keyStroke{modifiers: shift, key: key}
		}
	}
	keyMap[' '] = keyStroke{key: uinput.KeySpace}

	if len(keyMap) != 95 {
		panic("incomplete static US keyboard map")
	}
	return keyMap
}

func typeCompile(text string) ([]keyStroke, error) {
	if text == "" {
		return nil, nil
	}
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("invalid UTF-8 input")
	}

	runes := []rune(text)
	strokes := make([]keyStroke, 0, len(runes))
	logicalPosition := 0
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		logicalPosition++

		if r == '\r' {
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			strokes = append(strokes, keyStroke{key: uinput.KeyEnter})
			continue
		}
		if r == '\n' {
			strokes = append(strokes, keyStroke{key: uinput.KeyEnter})
			continue
		}

		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return nil, fmt.Errorf("unsupported control code point %U at rune position %d", r, logicalPosition)
		}

		if r <= 0x7e {
			stroke, ok := typeKeyMap[r]
			if !ok {
				panic(fmt.Sprintf("missing US keyboard mapping for %U", r))
			}
			strokes = append(strokes, stroke)
			continue
		}

		strokes = append(strokes, keyStroke{
			modifiers: []int{uinput.KeyLeftctrl, uinput.KeyLeftshift},
			key:       uinput.KeyU,
		})
		for _, digit := range strconv.FormatInt(int64(r), 16) {
			stroke, ok := typeKeyMap[digit]
			if !ok {
				panic(fmt.Sprintf("missing hexadecimal US keyboard mapping for %q", digit))
			}
			strokes = append(strokes, stroke)
		}
		strokes = append(strokes, keyStroke{key: uinput.KeyEnter})
	}

	return strokes, nil
}

// Typer owns the virtual keyboard created during preflight and reuses it for
// every dictation. Timing is deliberately fixed rather than configurable.
type Typer struct {
	kb       uinput.Keyboard
	keyHold  time.Duration
	keyDelay time.Duration
}

func newTyper() (*Typer, error) {
	kb, err := uinput.CreateKeyboard("/dev/uinput", []byte("voice-type-vkbd"))
	if err != nil {
		return nil, fmt.Errorf("create virtual keyboard: %w", err)
	}

	return &Typer{kb: kb, keyHold: typeKeyHold, keyDelay: typeKeyDelay}, nil
}

func (t *Typer) Close() {
	if t.kb != nil {
		t.kb.Close()
	}
}

func (t *Typer) sendStroke(stroke keyStroke) error {
	type modifierState struct {
		key          int
		attempted    bool
		needsRelease bool
	}

	modifiers := make([]modifierState, len(stroke.modifiers))
	var firstErr error
	recordErr := func(operation string, key int, err error) {
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s key %d: %w", operation, key, err)
		}
	}

	for i, key := range stroke.modifiers {
		modifiers[i] = modifierState{key: key, attempted: true, needsRelease: true}
		if err := t.kb.KeyDown(key); err != nil {
			recordErr("key down", key, err)
			break
		}
	}

	primaryAttempted := false
	primaryNeedsRelease := false
	if firstErr == nil {
		primaryAttempted = true
		primaryNeedsRelease = true
		if err := t.kb.KeyDown(stroke.key); err != nil {
			recordErr("key down", stroke.key, err)
		} else {
			time.Sleep(t.keyHold)
		}
	}

	releasePrimary := func() {
		if !primaryAttempted || !primaryNeedsRelease {
			return
		}
		if err := t.kb.KeyUp(stroke.key); err != nil {
			recordErr("key up", stroke.key, err)
		} else {
			primaryNeedsRelease = false
		}
	}
	releaseModifier := func(mod *modifierState) {
		if !mod.attempted || !mod.needsRelease {
			return
		}
		if err := t.kb.KeyUp(mod.key); err != nil {
			recordErr("key up", mod.key, err)
		} else {
			mod.needsRelease = false
		}
	}

	if firstErr != nil {
		releasePrimary()
	}
	// Release modifiers before the chord key. This matches dotool's proven
	// sequence and lets terminal input methods finish Ctrl+Shift+U cleanly.
	for i := range modifiers {
		releaseModifier(&modifiers[i])
	}
	releasePrimary()

	// A failed release can leave a key held. Retry only keys still marked as
	// needing release; never retry the whole stroke or any already released key.
	if firstErr != nil {
		releasePrimary()
		for i := range modifiers {
			releaseModifier(&modifiers[i])
		}
	}

	return firstErr
}

func (t *Typer) Type(text string) error {
	strokes, err := typeCompile(text)
	if err != nil {
		return err
	}

	for i, stroke := range strokes {
		if err := t.sendStroke(stroke); err != nil {
			return fmt.Errorf("partial output at stroke %d: %w", i, err)
		}
		if i+1 < len(strokes) {
			time.Sleep(t.keyDelay)
		}
	}
	return nil
}

// Paste temporarily owns CLIPBOARD, pastes the transcript, and restores what
// was there. When those contents cannot be handed back -- an unrecognised or
// oversized flavour -- the clipboard is left untouched and the transcript is
// typed instead: losing what the user copied costs more than the extra second.
// Type remains the low-level keyboard compiler.
func (t *Typer) Paste(text string) error {
	old, ok := saveClipboard()
	if !ok {
		return t.Type(text)
	}
	return t.pasteClipboard(text, old)
}

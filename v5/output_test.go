package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bendahl/uinput"
)

type testKeyEvent struct {
	op  string
	key int
}

type fakeKeyboard struct {
	events []testKeyEvent
	held   map[int]bool
	failAt int
	calls  int
	closed bool
}

func newFakeKeyboard() *fakeKeyboard {
	return &fakeKeyboard{held: make(map[int]bool)}
}

func (f *fakeKeyboard) record(op string, key int) error {
	f.events = append(f.events, testKeyEvent{op: op, key: key})
	f.calls++
	if f.failAt == f.calls {
		return errors.New("injected keyboard failure")
	}
	if op == "down" {
		f.held[key] = true
	} else if op == "up" {
		delete(f.held, key)
	}
	return nil
}

func (f *fakeKeyboard) KeyPress(key int) error { return f.record("press", key) }
func (f *fakeKeyboard) KeyDown(key int) error  { return f.record("down", key) }
func (f *fakeKeyboard) KeyUp(key int) error    { return f.record("up", key) }

func (f *fakeKeyboard) FetchSyspath() (string, error) { return "", nil }

func (f *fakeKeyboard) Close() error {
	f.closed = true
	return nil
}

var _ uinput.Keyboard = (*fakeKeyboard)(nil)

func stroke(key int, modifiers ...int) keyStroke {
	return keyStroke{modifiers: modifiers, key: key}
}

func TestPrintableASCIICompleteness(t *testing.T) {
	if len(typeKeyMap) != 95 {
		t.Fatalf("US key map has %d entries, want 95", len(typeKeyMap))
	}
	for r := rune(0x20); r <= 0x7e; r++ {
		if _, ok := typeKeyMap[r]; !ok {
			t.Errorf("missing US key mapping for %q (U+%04X)", r, r)
		}
		strokes, err := typeCompile(string(r))
		if err != nil {
			t.Errorf("typeCompile(%q): %v", r, err)
		} else if len(strokes) != 1 {
			t.Errorf("typeCompile(%q) produced %d strokes, want 1", r, len(strokes))
		}
	}
}

func TestUSKeyMapping(t *testing.T) {
	cases := []struct {
		name string
		in   rune
		want keyStroke
	}{
		{name: "a", in: 'a', want: stroke(uinput.KeyA)},
		{name: "A", in: 'A', want: stroke(uinput.KeyA, uinput.KeyLeftshift)},
		{name: "1", in: '1', want: stroke(uinput.Key1)},
		{name: "!", in: '!', want: stroke(uinput.Key1, uinput.KeyLeftshift)},
		{name: "-", in: '-', want: stroke(uinput.KeyMinus)},
		{name: "_", in: '_', want: stroke(uinput.KeyMinus, uinput.KeyLeftshift)},
		{name: "apostrophe", in: '\'', want: stroke(uinput.KeyApostrophe)},
		{name: "quote", in: '"', want: stroke(uinput.KeyApostrophe, uinput.KeyLeftshift)},
		{name: "backslash", in: '\\', want: stroke(uinput.KeyBackslash)},
		{name: "pipe", in: '|', want: stroke(uinput.KeyBackslash, uinput.KeyLeftshift)},
		{name: "space", in: ' ', want: stroke(uinput.KeySpace)},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			strokes, err := typeCompile(string(c.in))
			if err != nil {
				t.Fatalf("typeCompile(%q): %v", c.in, err)
			}
			if !reflect.DeepEqual(strokes, []keyStroke{c.want}) {
				t.Errorf("strokes = %#v, want %#v", strokes, []keyStroke{c.want})
			}
		})
	}
}

func TestNewlineForms(t *testing.T) {
	for _, in := range []string{"\n", "\r\n", "\r"} {
		strokes, err := typeCompile(in)
		if err != nil {
			t.Fatalf("typeCompile(%q): %v", in, err)
		}
		want := []keyStroke{{key: uinput.KeyEnter}}
		if !reflect.DeepEqual(strokes, want) {
			t.Errorf("typeCompile(%q) = %#v, want %#v", in, strokes, want)
		}
	}
}

func unicodeStrokes(hex string) []keyStroke {
	strokes := []keyStroke{stroke(uinput.KeyU, uinput.KeyLeftctrl, uinput.KeyLeftshift)}
	for _, r := range hex {
		strokes = append(strokes, typeKeyMap[r])
	}
	return append(strokes, stroke(uinput.KeyEnter))
}

func TestUnicodeBMP(t *testing.T) {
	got, err := typeCompile("é")
	if err != nil {
		t.Fatalf("typeCompile: %v", err)
	}
	want := unicodeStrokes("e9")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strokes = %#v, want %#v", got, want)
	}
}

func TestUnicodeSupplementary(t *testing.T) {
	got, err := typeCompile("🙂")
	if err != nil {
		t.Fatalf("typeCompile: %v", err)
	}
	want := unicodeStrokes("1f642")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strokes = %#v, want %#v", got, want)
	}
}

func TestCombiningSequencePreservesOrder(t *testing.T) {
	got, err := typeCompile("e\u0301")
	if err != nil {
		t.Fatalf("typeCompile: %v", err)
	}
	want := append([]keyStroke{typeKeyMap['e']}, unicodeStrokes("301")...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strokes = %#v, want %#v", got, want)
	}
}

func TestEmptyInputEmitsNothing(t *testing.T) {
	kb := newFakeKeyboard()
	typer := &Typer{kb: kb}
	if err := typer.Type(""); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if len(kb.events) != 0 {
		t.Fatalf("events = %#v, want none", kb.events)
	}
}

func TestInvalidUTF8EmitsNothing(t *testing.T) {
	kb := newFakeKeyboard()
	typer := &Typer{kb: kb}
	if err := typer.Type(string([]byte{'a', 0xff})); err == nil {
		t.Fatal("invalid UTF-8 returned nil error")
	}
	if len(kb.events) != 0 {
		t.Fatalf("events = %#v, want none", kb.events)
	}
}

func TestControlRejectionPrevalidatesEntireInput(t *testing.T) {
	cases := []string{"\t", "\x00", "\x1f", "\x7f", "\u0085", "a\t", "valid\u0001"}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			kb := newFakeKeyboard()
			typer := &Typer{kb: kb}
			if err := typer.Type(in); err == nil {
				t.Fatal("control input returned nil error")
			}
			if len(kb.events) != 0 {
				t.Fatalf("events = %#v, want none", kb.events)
			}
		})
	}
}

func TestSendStrokeEventOrder(t *testing.T) {
	kb := newFakeKeyboard()
	typer := &Typer{kb: kb}
	if err := typer.sendStroke(stroke(uinput.KeyA, uinput.KeyLeftctrl, uinput.KeyLeftshift)); err != nil {
		t.Fatalf("sendStroke: %v", err)
	}
	want := []testKeyEvent{
		{op: "down", key: uinput.KeyLeftctrl},
		{op: "down", key: uinput.KeyLeftshift},
		{op: "down", key: uinput.KeyA},
		{op: "up", key: uinput.KeyLeftctrl},
		{op: "up", key: uinput.KeyLeftshift},
		{op: "up", key: uinput.KeyA},
	}
	if !reflect.DeepEqual(kb.events, want) {
		t.Errorf("events = %#v, want %#v", kb.events, want)
	}
	if len(kb.held) != 0 {
		t.Errorf("held keys = %#v, want none", kb.held)
	}
}

func TestKeyDownFailureReleasesPreviouslyPressedModifiers(t *testing.T) {
	kb := newFakeKeyboard()
	kb.failAt = 2
	typer := &Typer{kb: kb}
	if err := typer.sendStroke(stroke(uinput.KeyA, uinput.KeyLeftctrl, uinput.KeyLeftshift)); err == nil {
		t.Fatal("sendStroke returned nil")
	}
	want := []testKeyEvent{
		{op: "down", key: uinput.KeyLeftctrl},
		{op: "down", key: uinput.KeyLeftshift},
		{op: "up", key: uinput.KeyLeftctrl},
		{op: "up", key: uinput.KeyLeftshift},
	}
	if !reflect.DeepEqual(kb.events, want) {
		t.Errorf("events = %#v, want %#v", kb.events, want)
	}
	if len(kb.held) != 0 {
		t.Errorf("held keys = %#v, want none", kb.held)
	}
}

func TestKeyUpFailureStillReleasesModifiers(t *testing.T) {
	kb := newFakeKeyboard()
	kb.failAt = 4
	typer := &Typer{kb: kb}
	if err := typer.sendStroke(stroke(uinput.KeyA, uinput.KeyLeftctrl, uinput.KeyLeftshift)); err == nil {
		t.Fatal("sendStroke returned nil")
	}
	if len(kb.held) != 0 {
		t.Errorf("held keys = %#v, want none", kb.held)
	}
	if !containsEvent(kb.events, testKeyEvent{op: "up", key: uinput.KeyLeftshift}) || !containsEvent(kb.events, testKeyEvent{op: "up", key: uinput.KeyLeftctrl}) {
		t.Errorf("events = %#v, want both modifiers released", kb.events)
	}
}

func TestModifierReleaseFailureContinuesAndRetriesHeldKeys(t *testing.T) {
	kb := newFakeKeyboard()
	kb.failAt = 5
	typer := &Typer{kb: kb}
	if err := typer.sendStroke(stroke(uinput.KeyA, uinput.KeyLeftctrl, uinput.KeyLeftshift)); err == nil {
		t.Fatal("sendStroke returned nil")
	}
	if len(kb.held) != 0 {
		t.Errorf("held keys = %#v, want none", kb.held)
	}
	if !containsEvent(kb.events, testKeyEvent{op: "up", key: uinput.KeyLeftctrl}) {
		t.Errorf("events = %#v, want Ctrl released after Shift failure", kb.events)
	}
	if countEvent(kb.events, testKeyEvent{op: "up", key: uinput.KeyLeftshift}) != 2 {
		t.Errorf("events = %#v, want two Shift release attempts", kb.events)
	}
}

func TestTypeStopsAfterPartialOutputFailure(t *testing.T) {
	kb := newFakeKeyboard()
	// The first stroke is a; the first operation of b fails.
	kb.failAt = 3
	typer := &Typer{kb: kb}
	err := typer.Type("ab")
	if err == nil || !strings.Contains(err.Error(), "partial output") {
		t.Fatalf("Type error = %v, want partial output error", err)
	}
	if len(kb.events) != 4 {
		t.Fatalf("events = %#v, want first stroke plus cleanup", kb.events)
	}
	if kb.events[0] != (testKeyEvent{op: "down", key: uinput.KeyA}) || kb.events[1] != (testKeyEvent{op: "up", key: uinput.KeyA}) {
		t.Errorf("events = %#v, first stroke was not emitted completely", kb.events)
	}
	if kb.events[2] != (testKeyEvent{op: "down", key: uinput.KeyB}) || kb.events[3] != (testKeyEvent{op: "up", key: uinput.KeyB}) {
		t.Errorf("events = %#v, failed stroke cleanup was not emitted", kb.events)
	}
	if len(kb.held) != 0 {
		t.Errorf("held keys = %#v, want none", kb.held)
	}
}

func TestTyperCloseClosesKeyboard(t *testing.T) {
	kb := newFakeKeyboard()
	typer := &Typer{kb: kb}
	typer.Close()
	if !kb.closed {
		t.Error("Close did not close the underlying keyboard")
	}
}

func containsEvent(events []testKeyEvent, want testKeyEvent) bool {
	return countEvent(events, want) > 0
}

func countEvent(events []testKeyEvent, want testKeyEvent) int {
	count := 0
	for _, event := range events {
		if event == want {
			count++
		}
	}
	return count
}

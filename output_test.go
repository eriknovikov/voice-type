package main

import (
	"reflect"
	"testing"

	"github.com/bendahl/uinput"
)

func TestParseKeyChord(t *testing.T) {
	cases := []struct {
		in      string
		mods    []int
		key     int
		wantErr bool
	}{
		{in: "ctrl+v", mods: []int{uinput.KeyLeftctrl}, key: uinput.KeyV},
		{in: "ctrl+shift+v", mods: []int{uinput.KeyLeftctrl, uinput.KeyLeftshift}, key: uinput.KeyV},
		{in: "CTRL+V", mods: []int{uinput.KeyLeftctrl}, key: uinput.KeyV},
		{in: "shift+insert", mods: []int{uinput.KeyLeftshift}, key: uinput.KeyInsert},
		{in: "super+v", mods: []int{uinput.KeyLeftmeta}, key: uinput.KeyV},
		{in: "v", mods: nil, key: uinput.KeyV},
		{in: "ctrl+nope", wantErr: true},
		{in: "nope+v", wantErr: true},
		{in: "", wantErr: true},
		{in: "ctrl+", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseKeyChord(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", c.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseKeyChord(%q): %v", c.in, err)
			}
			if !reflect.DeepEqual(got.Modifiers, c.mods) {
				t.Errorf("modifiers = %v, want %v", got.Modifiers, c.mods)
			}
			if got.Key != c.key {
				t.Errorf("key = %d, want %d", got.Key, c.key)
			}
		})
	}
}

func TestClipboardCopyArgs(t *testing.T) {
	// --sensitive keeps the transcript out of clipboard-manager history.
	got := clipboardCopyArgs(waylandClipboard, true)
	want := []string{"wl-copy", "--sensitive"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wayland sensitive = %v, want %v", got, want)
	}

	// The restore write is not sensitive -- it is the user's own prior content.
	got = clipboardCopyArgs(waylandClipboard, false)
	want = []string{"wl-copy"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wayland plain = %v, want %v", got, want)
	}

	// xclip has no --sensitive equivalent, so the flag must never leak into it.
	got = clipboardCopyArgs(x11Clipboard, true)
	want = []string{"xclip", "-selection", "clipboard"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("x11 = %v, want %v", got, want)
	}
}

// --paste-once breaks pasting into XWayland windows, so it must never appear.
func TestClipboardNeverUsesPasteOnce(t *testing.T) {
	for _, sensitive := range []bool{true, false} {
		for _, arg := range clipboardCopyArgs(waylandClipboard, sensitive) {
			if arg == "--paste-once" {
				t.Fatal("--paste-once breaks XWayland targets and must not be used")
			}
		}
	}
}

func TestDetectClipboardTool(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_SESSION_TYPE", "wayland")
	if got := detectClipboardTool(); !got.Wayland {
		t.Error("expected the Wayland tool")
	}

	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_SESSION_TYPE", "x11")
	if got := detectClipboardTool(); got.Wayland {
		t.Error("expected the X11 tool")
	}
}

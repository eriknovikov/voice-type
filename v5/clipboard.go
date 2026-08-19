package voicetype

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bendahl/uinput"
)

const (
	clipboardPasteDelay = 100 * time.Millisecond
	// Past this the round trip through two pipes costs more than typing does.
	clipboardMaxBytes = 16 << 20
)

type savedClipboard struct {
	mime string
	data []byte
}

func (t *Typer) pasteClipboard(text string, old savedClipboard) error {
	copy := exec.Command("wl-copy", "--foreground", "--paste-once", "--type", "text/plain;charset=utf-8", text)
	copy.Stderr = os.Stderr
	if err := copy.Start(); err != nil {
		return fmt.Errorf("start wl-copy: %w", err)
	}
	// Registered before the kill so it runs after it: killing wl-copy leaves
	// CLIPBOARD empty, so every exit path from here has to hand it back.
	defer restoreClipboard(old)
	defer func() {
		_ = copy.Process.Kill()
		_ = copy.Wait()
	}()

	// wl-copy needs a short moment to claim CLIPBOARD before the key chord.
	time.Sleep(clipboardPasteDelay)
	if err := t.sendStroke(keyStroke{
		modifiers: []int{uinput.KeyLeftctrl, uinput.KeyLeftshift},
		key:       uinput.KeyV,
	}); err != nil {
		return fmt.Errorf("send paste shortcut: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- copy.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("wl-copy paste: %w", err)
		}
	case <-time.After(2 * time.Second):
		return fmt.Errorf("paste was not consumed")
	}
	return nil
}

// saveClipboard reads back the one flavour worth restoring, or reports false
// when nothing offered can be put back. wl-copy serves a single MIME type per
// invocation, so the others are lost either way; picking the richest beats
// first-wins, which drops an image whenever the owner advertises text first.
func saveClipboard() (savedClipboard, bool) {
	out, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return savedClipboard{}, true // non-zero exit means an empty clipboard
	}
	keep := clipboardKeep(strings.Fields(string(out)))
	if keep == "" {
		return savedClipboard{}, false
	}
	data, err := exec.Command("wl-paste", "--no-newline", "--type", keep).Output()
	if err != nil || len(data) > clipboardMaxBytes {
		return savedClipboard{}, false
	}
	return savedClipboard{mime: keep, data: data}, true
}

// clipboardKeep returns the richest offered flavour, or "" when none of them
// is one we would vouch for.
func clipboardKeep(types []string) string {
	keep := ""
	for _, mime := range types {
		if clipboardRank(mime) > clipboardRank(keep) {
			keep = mime
		}
	}
	return keep
}

// clipboardRank orders the flavours we are willing to vouch for. Zero covers
// the X11 atom aliases wl-copy regenerates for any text type (UTF8_STRING,
// STRING, TEXT) and anything we cannot claim to round-trip.
func clipboardRank(mime string) int {
	switch {
	case mime == "image/png":
		return 4
	case strings.HasPrefix(mime, "image/"):
		return 3
	case mime == "text/plain;charset=utf-8":
		return 2
	case strings.HasPrefix(mime, "text/"):
		return 1
	}
	return 0
}

func restoreClipboard(old savedClipboard) {
	restore := exec.Command("wl-copy", "--clear")
	if old.mime != "" {
		restore = exec.Command("wl-copy", "--type", old.mime)
		restore.Stdin = bytes.NewReader(old.data)
	}
	if err := restore.Run(); err != nil {
		logf("OUTPUT", "clipboard restore failed: %v", err)
	}
}

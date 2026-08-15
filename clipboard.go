package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bendahl/uinput"
)

const clipboardPasteDelay = 100 * time.Millisecond

type savedClipboard struct {
	mime string
	data []byte
}

func (t *Typer) pasteClipboard(text string) error {
	old, err := saveClipboard()
	if err != nil {
		return fmt.Errorf("save clipboard: %w", err)
	}

	copy := exec.Command("wl-copy", "--foreground", "--paste-once", "--type", "text/plain;charset=utf-8", text)
	copy.Stderr = os.Stderr
	if err := copy.Start(); err != nil {
		return fmt.Errorf("start wl-copy: %w", err)
	}
	defer func() {
		if copy.Process != nil {
			_ = copy.Process.Kill()
			_ = copy.Wait()
		}
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

	if old.mime == "" {
		return exec.Command("wl-copy", "--clear").Run()
	}
	restore := exec.Command("wl-copy", "--type", old.mime)
	restore.Stdin = bytes.NewReader(old.data)
	if err := restore.Run(); err != nil {
		return fmt.Errorf("restore clipboard: %w", err)
	}
	return nil
}

func saveClipboard() (savedClipboard, error) {
	types, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return savedClipboard{}, err
	}
	for _, mime := range strings.Fields(string(types)) {
		data, err := exec.Command("wl-paste", "--no-newline", "--type", mime).Output()
		if err != nil {
			return savedClipboard{}, fmt.Errorf("read %s: %w", mime, err)
		}
		return savedClipboard{mime: mime, data: data}, nil
	}
	return savedClipboard{}, nil
}

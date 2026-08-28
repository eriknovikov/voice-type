package voicetype

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/bendahl/uinput"
)

const (
	clipboardMIME = "text/plain;charset=utf-8"

	// Poll until wl-copy owns CLIPBOARD; normally 20-40 ms on GNOME.
	clipboardClaimTimeout = time.Second
	clipboardClaimPoll    = 10 * time.Millisecond
	clipboardReadTimeout  = 100 * time.Millisecond
	clipboardIOTimeout    = time.Second
	// wl-copy briefly takes focus to obtain a valid Wayland input serial. Give
	// the compositor time to return focus before injecting the paste chord.
	clipboardFocusSettle = 70 * time.Millisecond

	// Browsers may read CLIPBOARD asynchronously after handling the chord.
	clipboardPasteGrace = 800 * time.Millisecond

	// Past this the round trip through two pipes costs more than typing does.
	clipboardMaxBytes = 16 << 20
)

type savedClipboard struct {
	mime string
	data []byte
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type clipboardProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	stderr  lockedBuffer
	waitErr error
}

func startClipboardOffer(text string) (*clipboardProcess, error) {
	p := &clipboardProcess{done: make(chan struct{})}
	p.cmd = exec.Command("wl-copy", "--foreground", "--type", clipboardMIME, text)
	p.cmd.Stderr = &p.stderr
	if err := p.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start wl-copy: %w", err)
	}
	go func() {
		p.waitErr = p.cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func (p *clipboardProcess) stop() {
	_ = p.cmd.Process.Kill()
	<-p.done
}

func (p *clipboardProcess) failure(reason string) error {
	reaped := false
	status := "still running"
	select {
	case <-p.done:
		reaped = true
		status = "exit status 0"
		if p.waitErr != nil {
			status = p.waitErr.Error()
		}
	default:
	}
	stderr := strings.TrimSpace(p.stderr.String())
	if stderr == "" {
		stderr = "<empty>"
	}
	if reaped && p.waitErr == nil {
		reason += "; wl-copy exited cleanly, likely after losing the selection to another client"
	}
	return fmt.Errorf("%s (reaped=%t, %s, stderr=%q)", reason, reaped, status, stderr)
}

// Keep ownership through the paste: GNOME's clipboard manager consumes the
// first read, so wl-copy --paste-once retires before the target reads it.
func (t *Typer) pasteClipboardContext(ctx context.Context, text string, old savedClipboard) (sent bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	copy, err := startClipboardOffer(text)
	if err != nil {
		return false, err
	}

	// Defer order prevents restoration from racing wl-copy's teardown.
	defer restoreClipboard(old)
	defer copy.stop()

	if err := awaitClipboardContext(ctx, text, copy); err != nil {
		return false, err
	}
	timer := time.NewTimer(clipboardFocusSettle)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		return false, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	select {
	case <-copy.done:
		return false, copy.failure("wl-copy exited while focus was settling")
	default:
	}

	sent, err = t.sendStrokeTrackedContext(ctx, keyStroke{
		modifiers: []int{uinput.KeyLeftctrl, uinput.KeyLeftshift},
		key:       uinput.KeyV,
	})
	if err != nil {
		return sent, fmt.Errorf("send paste shortcut: %w", err)
	}

	timer = time.NewTimer(clipboardPasteGrace)
	select {
	case <-timer.C:
		return true, nil
	case <-ctx.Done():
		timer.Stop()
		return true, ctx.Err()
	}
}

// Wait until the transcript, rather than the previous contents, is on offer.
func awaitClipboardContext(ctx context.Context, text string, copy *clipboardProcess) error {
	deadline := time.Now().Add(clipboardClaimTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return copy.failure(fmt.Sprintf("wl-copy did not claim the clipboard within %v", clipboardClaimTimeout))
		}
		readCtx, cancel := context.WithTimeout(ctx, min(clipboardReadTimeout, remaining))
		out, err := exec.CommandContext(readCtx, "wl-paste", "--no-newline", "--type", clipboardMIME).Output()
		cancel()
		if err == nil && string(out) == text {
			return nil
		}
		select {
		case <-copy.done:
			return copy.failure("wl-copy exited before CLIPBOARD carried the transcript")
		default:
		}
		timer := time.NewTimer(clipboardClaimPoll)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

func clipboardOutputContext(parent context.Context, args ...string) (stdout, stderr []byte, err error) {
	ctx, cancel := context.WithTimeout(parent, clipboardIOTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wl-paste", args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	if ctx.Err() != nil {
		err = fmt.Errorf("wl-paste timed out after %v: %w", clipboardIOTimeout, ctx.Err())
	}
	return out.Bytes(), errOut.Bytes(), err
}

// saveClipboard reads back the one flavour worth restoring, or reports false
// when nothing offered can be put back. wl-copy serves a single MIME type per
// invocation, so the others are lost either way; picking the richest beats
// first-wins, which drops an image whenever the owner advertises text first.
func saveClipboardContext(ctx context.Context) (savedClipboard, bool) {
	out, stderr, err := clipboardOutputContext(ctx, "--list-types")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(string(stderr)) == "Nothing is copied" {
			return savedClipboard{}, true
		}
		logf("OUTPUT", "clipboard save failed: list types: %v (stderr=%q)", err, strings.TrimSpace(string(stderr)))
		return savedClipboard{}, false
	}
	keep := clipboardKeep(strings.Fields(string(out)))
	if keep == "" {
		return savedClipboard{}, false
	}
	data, stderr, err := clipboardOutputContext(ctx, "--no-newline", "--type", keep)
	if err != nil || len(data) > clipboardMaxBytes {
		if err != nil {
			logf("OUTPUT", "clipboard save failed: read %s: %v (stderr=%q)", keep, err, strings.TrimSpace(string(stderr)))
		}
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
	ctx, cancel := context.WithTimeout(context.Background(), clipboardIOTimeout)
	defer cancel()
	restore := exec.CommandContext(ctx, "wl-copy", "--clear")
	if old.mime != "" {
		restore = exec.CommandContext(ctx, "wl-copy", "--type", old.mime)
		restore.Stdin = bytes.NewReader(old.data)
	}
	if err := restore.Run(); err != nil {
		if ctx.Err() != nil {
			err = fmt.Errorf("timed out after %v: %w", clipboardIOTimeout, ctx.Err())
		}
		logf("OUTPUT", "clipboard restore failed: %v", err)
	}
}

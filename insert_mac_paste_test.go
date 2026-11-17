package voicetype

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeMacPasteTarget struct {
	fakeMacDirectTarget
	posts      int
	beforePost func()
	postResult macSubmission
	postErr    error
}

func (t *fakeMacPasteTarget) commandPaste(context.Context, macPasteboardTransaction) (macSubmission, error) {
	t.posts++
	if t.beforePost != nil {
		t.beforePost()
	}
	return t.postResult, t.postErr
}

type fakeMacPasteboard struct {
	contents      []map[string][]byte
	snapshot      []map[string][]byte
	owned         bool
	reads         uint64
	begins        int
	restores      int
	closes        int
	beginErr      error
	restoreErr    error
	statusErr     error
	beforeRestore func()
	afterBegin    func()
}

func (p *fakeMacPasteboard) begin(_ context.Context, text string) (macPasteboardTransaction, error) {
	p.begins++
	p.snapshot = p.contents
	p.contents = []map[string][]byte{{"public.utf8-plain-text": []byte(text), "org.nspasteboard.TransientType": {}, "org.nspasteboard.ConcealedType": {}}}
	p.owned = true
	if p.afterBegin != nil {
		p.afterBegin()
	}
	return p, p.beginErr
}

func (p *fakeMacPasteboard) state() (macPasteboardState, error) {
	return macPasteboardState{Owned: p.owned, ReadRequests: p.reads}, p.statusErr
}

func (p *fakeMacPasteboard) restore() error {
	p.restores++
	if p.beforeRestore != nil {
		p.beforeRestore()
	}
	if p.owned {
		p.contents = p.snapshot
	}
	return p.restoreErr
}

func (p *fakeMacPasteboard) Close() error { p.closes++; return nil }

func newTestMacPaste(t *testing.T) (*macPasteInserter, *fakeMacPasteTarget, *fakeMacPasteboard, *time.Time) {
	t.Helper()
	target := &fakeMacPasteTarget{postResult: macSubmitted}
	target.snapshot = macTargetState{FocusMatches: true, Safety: macTargetEditable}
	board := &fakeMacPasteboard{}
	target.beforePost = func() { board.reads++ }
	inserter := newMacPasteInserter(func(context.Context) (macPasteTarget, error) { target.captures++; return target, nil }, board.begin)
	inserter.coordinator = newMacPasteCoordinator()
	now := time.Unix(0, 0)
	inserter.now = func() time.Time { return now }
	inserter.pause = func(ctx context.Context, duration time.Duration) error { now = now.Add(duration); return ctx.Err() }
	t.Cleanup(func() { _ = inserter.Close() })
	return inserter, target, board, &now
}

func TestMacPastePreservesEveryClipboardItemAndFormat(t *testing.T) {
	for _, name := range []string{"empty", "text", "rich", "image"} {
		t.Run(name, func(t *testing.T) {
			inserter, target, board, clock := newTestMacPaste(t)
			switch name {
			case "text":
				board.contents = []map[string][]byte{{"public.utf8-plain-text": []byte("original")}}
			case "rich":
				board.contents = []map[string][]byte{{"public.rtf": {0, 1, 2}, "public.utf8-plain-text": []byte("original")}, {"custom.type": {3, 4}}}
			case "image":
				board.contents = []map[string][]byte{{"public.png": {0, 255, 0, 5}}}
			}
			original := board.contents
			start := *clock
			submitted := 0
			target.beforePost = func() {
				if string(board.contents[0]["public.utf8-plain-text"]) != "paste\n🙂" {
					t.Error("wrong transient clipboard text")
				}
				for _, marker := range []string{"org.nspasteboard.TransientType", "org.nspasteboard.ConcealedType"} {
					if _, exists := board.contents[0][marker]; !exists {
						t.Error("missing advisory privacy marker", marker)
					}
				}
				board.reads++
			}
			if err := inserter.InsertWithSubmission(context.Background(), "paste\n🙂", func() { submitted++ }); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(board.contents, original) || target.posts != 1 || submitted != 1 || board.restores != 1 || board.closes != 1 || clock.Sub(start) != macPasteReadQuiet {
				t.Fatal("clipboard/timing/at-most-once invariant failed")
			}
		})
	}
}

func TestMacPasteWaitsForRepeatedReadsButNeverBeyondTimeout(t *testing.T) {
	for _, continuous := range []bool{false, true} {
		inserter, target, board, clock := newTestMacPaste(t)
		start := *clock
		inserter.pause = func(ctx context.Context, duration time.Duration) error {
			*clock = clock.Add(duration)
			if continuous || clock.Sub(start) < 400*time.Millisecond {
				board.reads++
			}
			return ctx.Err()
		}
		err := inserter.Insert(context.Background(), "text")
		if continuous && !errors.Is(err, errMacPasteReadTimeout) || !continuous && err != nil {
			t.Fatal("read coordination result incorrect", err)
		}
		if clock.Sub(start) > macPasteTimeout || clock.Sub(start) < 500*time.Millisecond || board.restores != 1 || target.posts != 1 {
			t.Fatal("read requests evaded the time bound or caused a replay")
		}
	}
}

func TestMacPasteNoReadTimesOutWithoutRetry(t *testing.T) {
	inserter, target, board, clock := newTestMacPaste(t)
	target.beforePost = nil
	start := *clock
	if err := inserter.Insert(context.Background(), "unknown consumption"); !errors.Is(err, errMacPasteReadTimeout) {
		t.Fatal("missing clipboard read was silently accepted", err)
	}
	if clock.Sub(start) != macPasteTimeout || target.posts != 1 || board.restores != 1 || board.closes != 1 {
		t.Fatal("timeout failed to restore/release once")
	}
	if err := inserter.Insert(context.Background(), "no replay"); !errors.Is(err, errMacInsertionStopped) || target.posts != 1 {
		t.Fatal("timed-out paste was replayed", err)
	}
}

func TestMacPastePreservesConcurrentUserCopy(t *testing.T) {
	for _, duringRestore := range []bool{false, true} {
		inserter, target, board, _ := newTestMacPaste(t)
		newer := []map[string][]byte{{"user-copy": []byte("newer")}}
		copy := func() { board.contents, board.owned = newer, false }
		if duringRestore {
			board.beforeRestore = copy
		} else {
			target.beforePost = copy
		}
		err := inserter.Insert(context.Background(), "paste")
		if !duringRestore && !errors.Is(err, errMacPasteClipboardChanged) {
			t.Fatal("ownership loss was hidden", err)
		}
		if !reflect.DeepEqual(board.contents, newer) || board.restores != 1 || board.closes != 1 {
			t.Fatal("restoration overwrote a user's newer clipboard")
		}
	}
}

func TestMacPasteCancelsRestoresAndNeverRetargets(t *testing.T) {
	for _, when := range []string{"before", "during-begin", "after-post", "focus", "secure"} {
		t.Run(when, func(t *testing.T) {
			inserter, target, board, _ := newTestMacPaste(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch when {
			case "before":
				cancel()
			case "during-begin":
				board.afterBegin = cancel
			case "after-post":
				target.beforePost = cancel
			case "focus":
				board.afterBegin = func() { target.snapshot.FocusMatches = false }
			case "secure":
				board.afterBegin = func() { target.snapshot.Safety = macTargetSecure }
			}
			err := inserter.Insert(ctx, "paste")
			if err == nil {
				t.Fatal("unsafe/cancelled paste accepted")
			}
			wantPosts, wantBoard := 0, 1
			if when == "after-post" {
				wantPosts = 1
			}
			if when == "before" {
				wantBoard = 0
			}
			if target.posts != wantPosts || board.restores != wantBoard || board.closes != wantBoard || target.captures > 1 {
				t.Fatal("cancellation/focus change skipped restoration or submitted stale text")
			}
		})
	}
}

func TestMacPasteFailureRestorationAndBound(t *testing.T) {
	for _, submission := range []macSubmission{macNotSubmitted, macSubmissionUnknown, macSubmitted} {
		inserter, target, board, clock := newTestMacPaste(t)
		target.beforePost = nil
		target.postResult, target.postErr = submission, errors.New("native paste failed")
		start := *clock
		err := inserter.Insert(context.Background(), "paste")
		if !errors.Is(err, target.postErr) || target.posts != 1 || board.restores != 1 || board.closes != 1 {
			t.Fatal("failed injection was retried or failed to restore", err)
		}
		wantWait := macPasteFailureWait
		if submission == macNotSubmitted {
			wantWait = 0
		}
		if clock.Sub(start) != wantWait {
			t.Fatal("failed injection wait exceeded its separate bound", clock.Sub(start))
		}
	}
	inserter, target, board, _ := newTestMacPaste(t)
	board.beginErr = errors.New("pasteboard publish failed")
	board.restoreErr = errors.New("pasteboard restore failed")
	err := inserter.Insert(context.Background(), "paste")
	if !errors.Is(err, board.beginErr) || !errors.Is(err, board.restoreErr) || board.restores != 1 || board.closes != 1 || target.posts != 0 {
		t.Fatal("partial preparation error lost cleanup/evidence", err)
	}
}

func TestMacPasteSerializesAllClipboardTransactions(t *testing.T) {
	first, target, _, _ := newTestMacPaste(t)
	second, secondTarget, secondBoard, _ := newTestMacPaste(t)
	second.coordinator = first.coordinator
	entered, release := make(chan struct{}), make(chan struct{})
	target.beforePost = func() { close(entered); <-release }
	result := make(chan error, 1)
	go func() { result <- first.Insert(context.Background(), "first") }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := second.Insert(ctx, "second"); !errors.Is(err, context.Canceled) || secondTarget.posts != 0 || secondBoard.begins != 0 {
		t.Fatal("waiting/cancelled transaction changed the clipboard", err)
	}
	close(release)
	if err := <-result; !errors.Is(err, errMacPasteReadTimeout) {
		t.Fatal("first transaction failed unexpectedly", err)
	}
	if err := second.Insert(context.Background(), "second"); err != nil {
		t.Fatal("coordinator gate was not released", err)
	}
	if secondTarget.posts != 1 {
		t.Fatal("queued paste was duplicated")
	}
}

func TestMacPasteCloseAndInvalidInput(t *testing.T) {
	inserter, target, board, _ := newTestMacPaste(t)
	for _, text := range []string{"", "\x00", string([]byte{0xff})} {
		if err := inserter.Insert(context.Background(), text); err == nil || board.begins != 0 || target.captures != 0 {
			t.Fatal("invalid input touched desktop state")
		}
	}
	if err := inserter.Close(); err != nil || target.closes != 0 {
		t.Fatal("unused Close captured the target", err)
	}
	if err := inserter.Insert(context.Background(), "closed"); !errors.Is(err, ErrTextInserterClosed) {
		t.Fatal("closed paste inserter reused", err)
	}
}

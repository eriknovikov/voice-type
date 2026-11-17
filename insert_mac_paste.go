package voicetype

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	macPasteReadQuiet    = 200 * time.Millisecond
	macPasteTimeout      = 8 * time.Second
	macPasteFailureWait  = 500 * time.Millisecond
	macPastePollInterval = 20 * time.Millisecond
)

var (
	errMacPasteClipboardChanged = errors.New("clipboard changed during paste; newer clipboard contents were preserved")
	errMacPasteReadTimeout      = errors.New("paste submitted but clipboard consumption is uncertain; do not retry automatically")
)

type macPasteTarget interface {
	state(context.Context) (macTargetState, error)
	commandPaste(context.Context, macPasteboardTransaction) (macSubmission, error)
	Close() error
}

type macPasteboardState struct {
	Owned        bool
	ReadRequests uint64
}

// begin snapshots every item/format before replacement and publishes transient/
// concealed plain text. A partial failure returns its transaction for cleanup.
// restore must recheck ownership in the same native operation that restores the
// snapshot. Neither status nor read requests prove delivery to the intended app.
type macPasteboardTransaction interface {
	state() (macPasteboardState, error)
	restore() error
	Close() error
}

type macPasteCoordinator struct{ gate chan struct{} }

func newMacPasteCoordinator() *macPasteCoordinator {
	return &macPasteCoordinator{gate: make(chan struct{}, 1)}
}

var macPasteTransactions = newMacPasteCoordinator()

type macPasteInserter struct {
	mu          sync.Mutex
	coordinator *macPasteCoordinator
	capture     func(context.Context) (macPasteTarget, error)
	begin       func(context.Context, string) (macPasteboardTransaction, error)
	target      macPasteTarget
	failure     error
	closed      bool
	now         func() time.Time
	pause       func(context.Context, time.Duration) error
}

func newMacPasteInserter(capture func(context.Context) (macPasteTarget, error), begin func(context.Context, string) (macPasteboardTransaction, error)) *macPasteInserter {
	return &macPasteInserter{coordinator: macPasteTransactions, capture: capture, begin: begin, now: time.Now, pause: pauseMacPaste}
}

func pauseMacPaste(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (i *macPasteInserter) checkTarget(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := i.target.state(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.Safety != macTargetEditable {
		return errMacUnsafeTarget
	}
	if !state.FocusMatches {
		return errMacFocusChanged
	}
	return nil
}

func (i *macPasteInserter) Insert(ctx context.Context, text string) error {
	return i.InsertWithSubmission(ctx, text, nil)
}

func (i *macPasteInserter) InsertWithSubmission(ctx context.Context, text string, submitted func()) (err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return ErrTextInserterClosed
	}
	if i.failure != nil {
		return fmt.Errorf("%w: %w", errMacInsertionStopped, i.failure)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateInsertionText(text); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case i.coordinator.gate <- struct{}{}:
	}
	defer func() { <-i.coordinator.gate }()
	defer func() {
		if err != nil {
			i.failure = err
		}
	}()
	if i.target == nil {
		if i.capture == nil || i.begin == nil {
			return ErrInsertBackendUnavailable
		}
		i.target, err = i.capture(ctx)
		if err != nil {
			return err
		}
		if i.target == nil {
			return ErrInsertBackendUnavailable
		}
	}
	if err := i.checkTarget(ctx); err != nil {
		return err
	}
	transaction, beginErr := i.begin(ctx, text)
	if transaction != nil {
		defer func() {
			err = errors.Join(err, transaction.restore(), transaction.Close())
		}()
	}
	if beginErr != nil {
		return beginErr
	}
	if transaction == nil {
		return errors.New("pasteboard transaction was not prepared")
	}
	if err := i.checkTarget(ctx); err != nil {
		return err
	}
	board, err := transaction.state()
	if err != nil {
		return err
	}
	if !board.Owned {
		return errMacPasteClipboardChanged
	}
	submission, pasteErr := i.target.commandPaste(ctx, transaction)
	resultErr := finishMacDelivery(submission, pasteErr, func() {
		if submitted != nil {
			submitted()
		}
	})
	if submission == macNotSubmitted {
		return resultErr
	}
	if resultErr != nil {
		// Once posting starts, give delayed readers a short, bounded opportunity
		// before guarded restoration. Never queue a second Command-V.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), macPasteFailureWait)
		defer cancel()
		return errors.Join(resultErr, i.awaitReadQuiet(cleanupCtx, transaction, macPasteFailureWait))
	}
	return i.awaitReadQuiet(ctx, transaction, macPasteTimeout)
}

func (i *macPasteInserter) awaitReadQuiet(ctx context.Context, transaction macPasteboardTransaction, limit time.Duration) error {
	deadline := i.now().Add(limit)
	lastRead := i.now()
	var reads uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state, err := transaction.state()
		if err != nil {
			return err
		}
		if !state.Owned {
			return errMacPasteClipboardChanged
		}
		now := i.now()
		if state.ReadRequests != reads {
			reads, lastRead = state.ReadRequests, now
		}
		if reads > 0 && now.Sub(lastRead) >= macPasteReadQuiet {
			return nil
		}
		if !now.Before(deadline) {
			return errMacPasteReadTimeout
		}
		if err := i.pause(ctx, min(macPastePollInterval, deadline.Sub(now))); err != nil {
			return err
		}
	}
}

func (i *macPasteInserter) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	if i.target != nil {
		err := i.target.Close()
		i.target = nil
		return err
	}
	return nil
}

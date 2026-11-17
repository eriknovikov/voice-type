package voicetype

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unicode/utf16"
)

type macTargetSafety uint8

const (
	macTargetUnknown macTargetSafety = iota
	macTargetEditable
	macTargetSecure
	macTargetReadOnly
	macTargetNonText
)

type macSelectedTextAccess uint8

const (
	macSelectedTextUnknown macSelectedTextAccess = iota
	macSelectedTextWritable
	macSelectedTextUnsupported
)

type macTargetState struct {
	FocusMatches bool // The exact retained element AND its owning process still have focus.
	Safety       macTargetSafety
	SelectedText macSelectedTextAccess
}

// Unknown is deliberately zero. A native failure without delivery evidence must
// never authorize a second method or a replay. Submitted means a known handoff,
// not visible insertion; it can accompany an error after a partial submission.
type macSubmission uint8

const (
	macSubmissionUnknown macSubmission = iota
	macNotSubmitted
	macSubmitted
)

var (
	errMacUnsafeTarget            = errors.New("direct insertion requires a known editable, non-protected text field")
	errMacFocusChanged            = errors.New("focused target changed; direct insertion stopped")
	errMacEditingChanged          = errors.New("target editing capabilities changed; direct insertion stopped")
	errMacSelectedTextUnknown     = errors.New("selected-text capability is unknown; direct insertion stopped")
	errMacSelectedTextUnsupported = errors.New("selected-text replacement is unsupported before submission")
	errMacAmbiguousDelivery       = errors.New("direct insertion delivery is uncertain; do not retry automatically")
	errMacNoDelivery              = errors.New("direct insertion did not submit text")
	errMacInsertionStopped        = errors.New("direct insertion stopped after an earlier failure")
)

// A native adapter retains one target identity until Close, never following a
// new focused element. state is read-only and must fail closed on missing/revoked
// permission or unknown safety. Every mutator must recheck cancellation, exact
// focus and safety immediately before its native call, since these can also
// change after the policy's check. Posting is synchronous: no queued output or
// retained UTF-16 slice may outlive a mutator call.
//
// macNotSubmitted certifies that NO mutation was attempted. Once an AX setter or
// event post starts, failure is unknown/submitted, never "unsupported" plus none.
// Methods must not include surrounding text or transcript content in errors.
// There is intentionally no clipboard, whole-value rewrite or delivery-retry API.
type macDirectTarget interface {
	state(context.Context) (macTargetState, error)
	replaceSelectedText(context.Context, string) (macSubmission, error)
	postUnicode(context.Context, []uint16) (macSubmission, error)
	Close() error
}

type macDirectInserter struct {
	mu        sync.Mutex
	newTarget func(context.Context) (macDirectTarget, error)
	target    macDirectTarget
	failure   error
	closed    bool
}

var _ TextInserter = (*macDirectInserter)(nil)

func newMacDirectInserter(capture func(context.Context) (macDirectTarget, error)) *macDirectInserter {
	return &macDirectInserter{newTarget: capture}
}

func (i *macDirectInserter) Insert(ctx context.Context, text string) error {
	return i.InsertWithSubmission(ctx, text, nil)
}

func (i *macDirectInserter) InsertWithSubmission(ctx context.Context, text string, submitted func()) (err error) {
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
	defer func() {
		if err != nil {
			i.failure = err
		}
	}()
	if i.target == nil {
		if i.newTarget == nil {
			return ErrInsertBackendUnavailable
		}
		i.target, err = i.newTarget(ctx)
		if err != nil {
			return err // Close also releases partially prepared targets.
		}
		if i.target == nil {
			return ErrInsertBackendUnavailable
		}
	}
	access, err := i.checkTarget(ctx)
	if err != nil {
		return err
	}
	// Long Unicode output has multiple native handoffs, but one submission time
	// per Insert. Never report an uncertain handoff as confirmed submission.
	notified := false
	notify := func() {
		if !notified && submitted != nil {
			notified = true
			submitted()
		}
	}
	if access == macSelectedTextWritable {
		if err := i.checkAccess(ctx, macSelectedTextWritable); err != nil {
			return err
		}
		delivery, replaceErr := i.target.replaceSelectedText(ctx, text)
		if delivery != macNotSubmitted || !errors.Is(replaceErr, errMacSelectedTextUnsupported) {
			return finishMacDelivery(delivery, replaceErr, notify)
		}
		// Only an explicit unsupported result BEFORE any AX mutation allows
		// Unicode. The next check must confirm the target remains eligible.
	}
	return i.insertUnicode(ctx, text, notify)
}

func (i *macDirectInserter) checkTarget(ctx context.Context) (macSelectedTextAccess, error) {
	if err := ctx.Err(); err != nil {
		return macSelectedTextUnknown, err
	}
	state, err := i.target.state(ctx)
	if err != nil {
		return macSelectedTextUnknown, err
	}
	if err := ctx.Err(); err != nil {
		return macSelectedTextUnknown, err
	}
	if state.Safety != macTargetEditable {
		return macSelectedTextUnknown, errMacUnsafeTarget
	}
	if !state.FocusMatches {
		return macSelectedTextUnknown, errMacFocusChanged
	}
	if state.SelectedText != macSelectedTextWritable && state.SelectedText != macSelectedTextUnsupported {
		return macSelectedTextUnknown, errMacSelectedTextUnknown
	}
	return state.SelectedText, nil
}

func (i *macDirectInserter) checkAccess(ctx context.Context, want macSelectedTextAccess) error {
	access, err := i.checkTarget(ctx)
	if err != nil {
		return err
	}
	if access != want {
		return errMacEditingChanged
	}
	return nil
}

// Bound event payloads and the amount sent between focus/cancellation checks.
const macUnicodeChunkUnits = 20

func (i *macDirectInserter) insertUnicode(ctx context.Context, text string, notify func()) error {
	units := utf16.Encode([]rune(text))
	for len(units) > 0 {
		end := min(len(units), macUnicodeChunkUnits)
		if end < len(units) && units[end-1] >= 0xd800 && units[end-1] <= 0xdbff {
			end-- // Do not split a surrogate pair across native events.
		}
		if err := i.checkAccess(ctx, macSelectedTextUnsupported); err != nil {
			return err
		}
		delivery, err := i.target.postUnicode(ctx, units[:end])
		if err := finishMacDelivery(delivery, err, notify); err != nil {
			return err
		}
		units = units[end:]
	}
	return nil
}

func finishMacDelivery(delivery macSubmission, err error, notify func()) error {
	switch delivery {
	case macSubmitted:
		notify()
		return err
	case macNotSubmitted:
		if err == nil {
			return errMacNoDelivery
		}
		return err
	default:
		return errors.Join(errMacAmbiguousDelivery, err)
	}
}

func (i *macDirectInserter) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	if i.target == nil {
		return nil
	}
	err := i.target.Close()
	i.target = nil
	return err
}

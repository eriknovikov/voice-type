package voicetype

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf8"
)

// InsertBackend selects a text insertion backend at runtime.
type InsertBackend string

const (
	InsertBackendIBus  InsertBackend = "ibus"
	InsertBackendFcitx InsertBackend = "fcitx"
)

var (
	ErrInsertBackendUnavailable    = errors.New("text insertion backend is unavailable on this platform")
	ErrInsertBackendNotImplemented = errors.New("text insertion backend is not implemented")
	ErrTextInserterClosed          = errors.New("text inserter is closed")
)

// TextInserter submits text to the focused application. Preparation can
// temporarily replace the active input engine. Always defer Close, including
// when insertion is canceled or skipped. Each Insert call submits at most once
// and restores the input engine. Later calls prepare a fresh temporary engine.
// Close is idempotent and prevents further insertion. Success does not confirm
// application receipt; an application can reject an unacknowledged submission.
type TextInserter interface {
	Insert(ctx context.Context, text string) error
	Close() error
}

// Platform-specific files register constructors during package initialization.
// The registry is read-only after initialization. This file imports no backend
// libraries, so an unavailable platform does not compile those dependencies.
var textInserterBackends = map[InsertBackend]func(context.Context) (TextInserter, error){}

// NewTextInserter selects and prepares a backend. It does not submit text or
// provide a focus delay. The caller controls when to call Insert.
func NewTextInserter(ctx context.Context, backend InsertBackend) (TextInserter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch backend {
	case InsertBackendIBus, InsertBackendFcitx:
	default:
		return nil, fmt.Errorf("unknown text insertion backend %q", backend)
	}
	constructor, available := textInserterBackends[backend]
	if !available {
		return nil, fmt.Errorf("%w: %s on %s", ErrInsertBackendUnavailable, backend, runtime.GOOS)
	}
	if constructor == nil {
		return nil, fmt.Errorf("%w: %s", ErrInsertBackendNotImplemented, backend)
	}
	return constructor(ctx)
}

func validateInsertionText(text string) error {
	if text == "" {
		return errors.New("insertion text is empty")
	}
	if !utf8.ValidString(text) {
		return errors.New("insertion text is not valid UTF-8")
	}
	if strings.ContainsRune(text, 0) {
		return errors.New("insertion text contains a NUL character")
	}
	return nil
}

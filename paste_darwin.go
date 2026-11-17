package voicetype

/*
#include "paste_darwin.h"
#include "insert_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"
)

type darwinPasteboardTransaction struct{ native unsafe.Pointer }
type darwinPasteTarget struct{ *darwinDirectTarget }

func init() {
	textInserterBackends[InsertBackendMacPaste] = func(ctx context.Context) (TextInserter, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return newMacPasteInserter(captureDarwinPasteTarget, beginDarwinPasteboard), nil
	}
}

func captureDarwinPasteTarget(ctx context.Context) (macPasteTarget, error) {
	target, err := captureDarwinTarget(ctx)
	if err != nil {
		return nil, err
	}
	return &darwinPasteTarget{target.(*darwinDirectTarget)}, nil
}

func (t *darwinPasteTarget) commandPaste(ctx context.Context, transaction macPasteboardTransaction) (macSubmission, error) {
	board, ok := transaction.(*darwinPasteboardTransaction)
	if !ok || board.native == nil {
		return macNotSubmitted, errors.New("native pasteboard transaction is unavailable")
	}
	guard, release, err := darwinInsertGuard(ctx)
	if err != nil {
		return macNotSubmitted, err
	}
	defer release()
	result := C.vt_insert_paste(t.native, guard, board.native)
	return darwinSubmission(result), darwinInsertError(ctx, result)
}

func darwinPasteboardError(status C.int) error {
	switch status {
	case C.VT_PASTE_OK:
		return nil
	case C.VT_PASTE_CHANGED:
		return errMacPasteClipboardChanged
	case C.VT_PASTE_TOO_LARGE:
		return errors.New("clipboard is too large to preserve safely; paste compatibility did not change it")
	case C.VT_PASTE_SNAPSHOT_ERROR:
		return errors.New("could not preserve every clipboard item and format; clipboard was not changed")
	default:
		return errors.New("could not write or restore the clipboard; check its contents before continuing")
	}
}

func beginDarwinPasteboard(ctx context.Context, text string) (macPasteboardTransaction, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateInsertionText(text); err != nil {
		return nil, err
	}
	bytes := []byte(text)
	transaction := &darwinPasteboardTransaction{}
	status := C.vt_pasteboard_begin((*C.char)(unsafe.Pointer(&bytes[0])), C.size_t(len(bytes)), &transaction.native)
	if transaction.native == nil {
		return nil, darwinPasteboardError(status)
	}
	return transaction, darwinPasteboardError(status)
}

func (t *darwinPasteboardTransaction) state() (macPasteboardState, error) {
	var owned C.int
	var reads C.uint64_t
	status := C.vt_pasteboard_state(t.native, &owned, &reads)
	return macPasteboardState{Owned: owned != 0, ReadRequests: uint64(reads)}, darwinPasteboardError(status)
}

func (t *darwinPasteboardTransaction) restore() error {
	return darwinPasteboardError(C.vt_pasteboard_restore(t.native))
}

func (t *darwinPasteboardTransaction) Close() error {
	C.vt_pasteboard_close(t.native)
	t.native = nil
	return nil
}

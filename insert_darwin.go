package voicetype

/*
#cgo darwin LDFLAGS: -framework ApplicationServices -framework Carbon
#include "insert_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf16"
	"unsafe"
)

type darwinDirectTarget struct{ native *C.vt_insert_target }

func init() {
	textInserterBackends[InsertBackendMacDirect] = func(ctx context.Context) (TextInserter, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return newMacDirectInserter(captureDarwinTarget), nil
	}
}

func darwinInsertGuard(ctx context.Context) (*C.vt_insert_guard, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	guard := C.vt_insert_guard_create()
	if guard == nil {
		return nil, nil, errors.New("allocate direct insertion cancellation guard")
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		C.vt_insert_guard_cancel(guard)
		close(done)
	})
	return guard, func() {
		if !stop() {
			<-done
		}
		C.vt_insert_guard_free(guard)
	}, nil
}

func darwinInsertError(ctx context.Context, result C.vt_insert_result) error {
	var err error
	switch result.status {
	case C.VT_INSERT_OK:
		return nil
	case C.VT_INSERT_CANCELLED:
		err = ctx.Err()
		if err == nil {
			err = context.Canceled
		}
	case C.VT_INSERT_PERMISSION:
		err = errors.New("Accessibility access is required for direct insertion; allow Voice Type in System Settings > Privacy & Security > Accessibility")
	case C.VT_INSERT_FOCUS_CHANGED:
		err = errMacFocusChanged
	case C.VT_INSERT_UNSAFE:
		err = errMacUnsafeTarget
	case C.VT_INSERT_CAPABILITY_UNKNOWN:
		err = errMacSelectedTextUnknown
	case C.VT_INSERT_UNSUPPORTED:
		err = errMacSelectedTextUnsupported
	case C.VT_INSERT_CAPABILITY_CHANGED:
		err = errMacEditingChanged
	case C.VT_INSERT_ALLOCATION:
		err = errors.New("allocate native direct insertion resources")
	case C.VT_INSERT_CLIPBOARD_CHANGED:
		err = errMacPasteClipboardChanged
	default:
		err = errors.New("native direct insertion failed")
	}
	if result.ax_error != 0 {
		return fmt.Errorf("%w (AX error %d)", err, int(result.ax_error))
	}
	return err
}

func darwinSubmission(result C.vt_insert_result) macSubmission {
	switch result.submission {
	case C.VT_INSERT_NOT_SUBMITTED:
		return macNotSubmitted
	case C.VT_INSERT_SUBMITTED:
		return macSubmitted
	default:
		return macSubmissionUnknown
	}
}

func captureDarwinTarget(ctx context.Context) (macDirectTarget, error) {
	guard, release, err := darwinInsertGuard(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	target := &darwinDirectTarget{}
	result := C.vt_insert_capture(guard, &target.native)
	if err := darwinInsertError(ctx, result); err != nil {
		_ = target.Close()
		return nil, err
	}
	return target, nil
}

func (t *darwinDirectTarget) state(ctx context.Context) (macTargetState, error) {
	guard, release, err := darwinInsertGuard(ctx)
	if err != nil {
		return macTargetState{}, err
	}
	defer release()
	var writable C.int
	result := C.vt_insert_state(t.native, guard, &writable)
	if err := darwinInsertError(ctx, result); err != nil {
		return macTargetState{}, err
	}
	access := macSelectedTextUnsupported
	if writable != 0 {
		access = macSelectedTextWritable
	}
	return macTargetState{FocusMatches: true, Safety: macTargetEditable, SelectedText: access}, nil
}

func (t *darwinDirectTarget) replaceSelectedText(ctx context.Context, text string) (macSubmission, error) {
	if err := validateInsertionText(text); err != nil {
		return macNotSubmitted, err
	}
	guard, release, err := darwinInsertGuard(ctx)
	if err != nil {
		return macNotSubmitted, err
	}
	defer release()
	units := utf16.Encode([]rune(text))
	result := C.vt_insert_replace(t.native, guard, (*C.uint16_t)(unsafe.Pointer(&units[0])), C.size_t(len(units)))
	return darwinSubmission(result), darwinInsertError(ctx, result)
}

func (t *darwinDirectTarget) postUnicode(ctx context.Context, units []uint16) (macSubmission, error) {
	if len(units) == 0 || len(units) > macUnicodeChunkUnits {
		return macNotSubmitted, errors.New("invalid native Unicode event size")
	}
	guard, release, err := darwinInsertGuard(ctx)
	if err != nil {
		return macNotSubmitted, err
	}
	defer release()
	result := C.vt_insert_unicode(t.native, guard, (*C.uint16_t)(unsafe.Pointer(&units[0])), C.size_t(len(units)))
	return darwinSubmission(result), darwinInsertError(ctx, result)
}

func (t *darwinDirectTarget) Close() error {
	C.vt_insert_close(t.native)
	t.native = nil
	return nil
}

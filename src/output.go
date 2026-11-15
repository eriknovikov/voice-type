package voicetype

import (
	"context"
	"errors"
	"time"
)

func insertText(ctx context.Context, text string) error {
	return insertTextTimed(ctx, text, nil)
}
func insertTextTimed(ctx context.Context, text string, timings *dictationTiming) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateInsertionText(text); err != nil {
		return err
	}
	inserter, err := NewTextInserter(ctx, InsertBackendIBus)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, inserter.Close())
		if timings != nil {
			timings.InsertionCleanupCompleted = time.Now()
		}
	}()
	return submitText(ctx, inserter, text, func() {
		if timings != nil {
			timings.InsertionSubmitted = time.Now()
		}
	})
}

// The IBus backend reports the actual CommitText send before its daemon barrier
// and engine restoration. Other backends report successful submission on return.
func submitText(ctx context.Context, inserter TextInserter, text string, submitted func()) error {
	if timed, ok := inserter.(interface {
		InsertWithSubmission(context.Context, string, func()) error
	}); ok {
		return timed.InsertWithSubmission(ctx, text, submitted)
	}
	err := inserter.Insert(ctx, text)
	if err == nil && submitted != nil {
		submitted()
	}
	return err
}

package voicetype

import (
	"context"
	"errors"
)

func insertText(ctx context.Context, text string) (err error) {
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
	defer func() { err = errors.Join(err, inserter.Close()) }()
	return inserter.Insert(ctx, text)
}

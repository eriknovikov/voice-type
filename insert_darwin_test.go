package voicetype

import (
	"context"
	"errors"
	"testing"
)

func TestDarwinDirectRegistrationIsLazy(t *testing.T) {
	inserter, err := NewTextInserter(context.Background(), InsertBackendMacDirect)
	if err != nil {
		t.Fatal(err)
	}
	direct, ok := inserter.(*macDirectInserter)
	if !ok || direct.target != nil {
		t.Fatal("constructor inspected native focus")
	}
	if err := inserter.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewTextInserter(ctx, InsertBackendMacDirect); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled constructor accepted", err)
	}
}

func TestDarwinInsertGuardCancellationCleanup(t *testing.T) {
	for range 100 {
		ctx, cancel := context.WithCancel(context.Background())
		guard, release, err := darwinInsertGuard(ctx)
		if err != nil || guard == nil {
			t.Fatal("guard creation failed", err)
		}
		go cancel()
		release()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := darwinInsertGuard(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled guard allocated", err)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	voicetype "github.com/eriknovikov/voice-type"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "try:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	inserter, err := voicetype.NewTextInserter(ctx, voicetype.InsertBackendIBus)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, inserter.Close()) }()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return inserter.Insert(ctx, "Hello Insertion")
}

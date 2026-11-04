package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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

func run(ctx context.Context) error {
	recorder, err := voicetype.NewRecorder()
	name := os.Args[1]
	if err != nil {
		return err
	}
	defer recorder.Close()

	if err := recorder.Start(); err != nil {
		return err
	}
	fmt.Println("Recording for 15 seconds...")
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}

	samples, err := recorder.Stop()
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return fmt.Errorf("no audio captured")
	}

	rawPath := filepath.Join(os.TempDir(), fmt.Sprintf("raw_%s.wav", name))
	if err := os.WriteFile(rawPath, voicetype.WavEncode(samples), 0o600); err != nil {
		return fmt.Errorf("save raw WAV: %w", err)
	}
	fmt.Println("Saved", rawPath)
	return nil
}

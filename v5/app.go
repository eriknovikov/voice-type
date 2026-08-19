package voicetype

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
}

// logf writes a tagged line to stderr. No file logger, no rotation -- stderr is
// the whole logging story in v5.
func logf(tag, format string, args ...any) {
	log.Printf("[%s] %s: %s", time.Now().Format("15:04:05"), tag, fmt.Sprintf(format, args...))
}

// retainWAV saves audio that could not be transcribed, so an API failure never
// silently discards speech the user already gave.
func retainWAV(wav []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "voice-type")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "dictation-*.wav")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(wav); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// Run is the whole application: flag dispatch, preflight, daemon loop. It
// lives in the library package so the entry point under cmd/ stays a stub
// and this stays reachable from in-package tests.
//
// version arrives from package main, which is where the linker stamps it --
// keeping that variable in main is what lets -X main.version stay unchanged.
func Run(version string) {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	switch flag.Arg(0) {
	case "", "start":
	case "version":
		fmt.Println(version)
		return
	case "config-port":
		fmt.Println(configLoad().Port)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\nusage: voice-type [start|version|config-port]\n", flag.Arg(0))
		os.Exit(2)
	}

	cfg := configLoad()

	result, res, err := runPreflight(cfg)
	switch result {
	case preflightAlreadyRunning:
		// Matches v4: an existing daemon is a successful no-op, which is what
		// makes the F10 `curl /exit || voice-type` toggle work.
		logf("PREFLIGHT", "%v", err)
		os.Exit(0)
	case preflightFailed:
		logf("PREFLIGHT", "%v", err)
		os.Exit(1)
	}
	defer res.Close()

	d := newDaemon(cfg, res)

	// Ctrl-C and SIGTERM shut down the same way /exit does.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		d.shutdown()
	}()

	if err := d.Run(); err != nil {
		logf("DAEMON", "%v", err)
		res.Close()
		os.Exit(1)
	}
}

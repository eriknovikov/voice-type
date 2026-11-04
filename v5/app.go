package voicetype

import (
	"errors"
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

func logf(tag, format string, args ...any) {
	log.Printf("[%s] %s: %s", time.Now().Format("15:04:05"), tag, fmt.Sprintf(format, args...))
}

// retainWAV saves audio that failed or produced a suspicious transcription, so
// the evidence and speech the user already gave are not silently discarded.
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

// Run dispatches commands and runs the daemon. version stays in package main so
// release builds can stamp it with -X main.version.
func Run(version string) {
	opts, err := parseCLI(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(cliUsage)
		fmt.Println("--model ID: unsupported override for start (process lifetime only)")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n\n%s\n", err, cliUsage)
		os.Exit(2)
	}
	if opts.Version {
		fmt.Println(version)
		return
	}

	switch opts.Command {
	case "start":
	case "version":
		fmt.Println(version)
		return

	}

	if exitCode := runDaemon(version, opts.Model); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runDaemon(version, modelOverride string) (exitCode int) {
	runLog := openRunLog(version)
	defer func() {
		outcome := "ok"
		if exitCode != 0 {
			outcome = "error"
		}
		runLog.close(outcome)
	}()

	cfg, configErr := configReadStrict()
	if configErr != nil {
		cfg = configDefaults()
	}
	if portInUse(cfg.Port) {
		logf("PREFLIGHT", "port %d already in use", cfg.Port)
		return 0
	}
	settings, err := cfg.resolveSTT(modelOverride, workspaceLoad())
	if err != nil {
		configErr = err
		settings = sttSettings{Provider: cfg.Provider, Model: providerModel(cfg.Provider)}
	}
	res := &resources{Recorder: &Recorder{}}
	defer res.Close()
	d := newDaemon(cfg, res, settings, modelOverride)
	if configErr != nil {
		d.configError = configErr.Error()
	} else if err := settings.validateCredentials(); err != nil {
		d.configError = err.Error()
	}
	d.runLog = runLog

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		d.shutdown()
	}()

	if err := d.Run(); err != nil {
		logf("DAEMON", "%v", err)
		return 1
	}
	return 0
}

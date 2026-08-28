package voicetype

import (
	"flag"
	"fmt"
	"io"
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

func printConfig(w io.Writer) error {
	cfg, err := configReadStrict()
	if err != nil {
		return err
	}

	keyStatus := "MISSING"
	switch {
	case os.Getenv("OPENROUTER_API_KEY") != "":
		keyStatus = "set (from OPENROUTER_API_KEY)"
	case cfg.APIKey != "":
		keyStatus = "set (from config file)"
	}

	spec := cfg.modelSpec()
	vocabulary := "ignores vocabulary"
	if spec.Vocabulary {
		vocabulary = "reads vocabulary"
	}
	_, err = fmt.Fprintf(w, "config:     %s\napi_key:    %s\nport:       %d\nmodel:      %s  ($%.3f/hour, %s)\nvocabulary: %d terms\n",
		configFilePath(), keyStatus, cfg.Port, spec.ID, spec.USDPerHour, vocabulary, len(cfg.Vocabulary))
	return err
}

func printModels(w io.Writer) {
	fmt.Fprintln(w, "ID                           $/hour   vocabulary")
	for _, model := range sttModels {
		vocabulary := "no"
		if model.Vocabulary {
			vocabulary = "yes"
		}
		defaultLabel := ""
		if model.ID == defaultModelID {
			defaultLabel = "  (default)"
		}
		fmt.Fprintf(w, "%-28s %7.3f   %-10s%s\n", model.ID, model.USDPerHour, vocabulary, defaultLabel)
	}
}

// Run dispatches commands and runs the daemon. version stays in package main so
// release builds can stamp it with -X main.version.
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
	case "config":
		if err := printConfig(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case "models":
		printModels(os.Stdout)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\nusage: voice-type [start|version|config|models|config-port]\n", flag.Arg(0))
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

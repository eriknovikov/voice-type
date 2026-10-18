package voicetype

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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

func printConfig(w io.Writer, modelOverride string) error {
	cfg, err := configReadStrict()
	if err != nil {
		return err
	}

	keyStatus := "MISSING"
	switch {
	case strings.TrimSpace(os.Getenv(cfg.keyEnv())) != "":
		keyStatus = "set (from " + cfg.keyEnv() + ")"
	case cfg.APIKey != "":
		keyStatus = "set (from config file)"
	}

	workspace := workspaceLoad()
	settings, err := cfg.resolveSTT(modelOverride, workspace)
	if err != nil {
		return err
	}
	modelStatus := "supported"
	if !settings.supportedModel() {
		modelStatus = "unsupported override, vocabulary disabled"
	}
	_, err = fmt.Fprintf(w, "config:     %s\nprovider:   %s\napi_key:    %s\nport:       %d\nmodel:      %s (%s)\nfallback:   none\nvocabulary: %d terms (%s)\n",
		configFilePath(), cfg.Provider, keyStatus, cfg.Port, settings.Model, modelStatus,
		len(settings.Vocabulary), vocabularySources(cfg, workspace))
	return err
}

// vocabularySources names the lists that make up what gets sent, so a surprising
// term count says where to look.
func vocabularySources(cfg Config, workspace string) string {
	sources := []string{}
	if len(cfg.terms(generalWorkspace)) > 0 {
		sources = append(sources, generalWorkspace)
	}
	if workspace != generalWorkspace && cfg.hasWorkspace(workspace) {
		sources = append(sources, workspace)
	}
	if len(sources) == 0 {
		return "no vocab active"
	}
	return strings.Join(sources, " + ")
}

// printVocab is `voice-type vocab ls`: how many terms the next dictation sends,
// which list is picked, and every list in the order the config file writes them.
func printVocab(w io.Writer, cfg Config, workspace string) {
	fmt.Fprintln(w, "Vocabulary applies to GPT Transcribe. A daemon using an unsupported --model sends none.")
	fmt.Fprintf(w, "sending: %d\ncurrent: %s\n", len(cfg.activeVocabulary(workspace)), workspaceLabel(workspace))

	if len(cfg.Vocabulary) == 0 {
		fmt.Fprintf(w, "\nno lists yet -- add \"vocabulary\": {\"%s\": [...], \"my-project\": [...]} to the config\n", generalWorkspace)
		return
	}
	for _, list := range cfg.Vocabulary {
		marker := ""
		if list.Name == workspace {
			marker = "*"
		}
		fmt.Fprintf(w, "%s%s: %d\n", list.Name, marker, len(cfg.terms(list.Name)))
	}
	if workspace != "" && !cfg.hasWorkspace(workspace) {
		fmt.Fprintf(w, "\n%q is not in the config file -- only %s is being sent\n", workspace, generalWorkspace)
	}
}

// resolveWorkspace turns what the user typed into a workspace name: one of the
// names `voice-type vocab ls` lists, or none.
func resolveWorkspace(cfg Config, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	switch arg {
	case "none", "off", "-":
		return "", nil
	case generalWorkspace:
		return "", fmt.Errorf("%s is always sent -- `voice-type vocab set none` sends it alone", generalWorkspace)
	}

	names := cfg.workspaces()
	for _, name := range names {
		if name == arg {
			return name, nil
		}
	}
	return "", fmt.Errorf("no vocabulary list %q -- available: %s",
		arg, strings.Join(append(names, "none"), ", "))
}

// runVocab dispatches `voice-type vocab [...]`.
func runVocab(w io.Writer, args []string) error {
	cfg, err := configReadStrict()
	if err != nil {
		return err
	}

	command := "ls"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "ls":
		printVocab(w, cfg, workspaceLoad())
		return nil
	case "set":
		if len(args) < 2 {
			printVocab(w, cfg, workspaceLoad())
			return errors.New("usage: voice-type vocab set <name|none>")
		}
		name, err := resolveWorkspace(cfg, args[1])
		if err != nil {
			return err
		}
		if err := workspaceSave(name); err != nil {
			return fmt.Errorf("could not record the workspace in %s: %w", workspaceFilePath(), err)
		}
		fmt.Fprintf(w, "vocabulary: %s  ->  %d terms on the next dictation (%s)\n",
			workspaceLabel(name), len(cfg.activeVocabulary(name)), vocabularySources(cfg, name))
		return nil
	default:
		return fmt.Errorf("unknown vocab command %q\n\nusage: voice-type vocab [ls|set <name|none>]", command)
	}
}

func printModels(w io.Writer) {
	fmt.Fprintln(w, "GPT Transcribe (default, only supported model)")
	fmt.Fprintln(w, "  OpenAI:     gpt-transcribe (vocabulary keywords)")
	fmt.Fprintln(w, "  OpenRouter: openai/gpt-transcribe (vocabulary prompt)")
	fmt.Fprintln(w, "No provider or model fallback. Billing comes from the API when available.")
	fmt.Fprintln(w, "Use --model ID with start or config for an unsupported override.")
	fmt.Fprintln(w, "Other IDs pass through unchanged. Vocabulary and model-specific hints are disabled.")
}

// Run dispatches commands and runs the daemon. version stays in package main so
// release builds can stamp it with -X main.version.
func Run(version string) {
	opts, err := parseCLI(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Println(cliUsage)
		fmt.Println("--model ID: unsupported override for start or config (process lifetime only)")
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
	case "config-port":
		cfg, err := configReadStrict()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(cfg.Port)
		return
	case "config":
		if err := printConfig(os.Stdout, opts.Model); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	case "models":
		printModels(os.Stdout)
		return
	case "vocab", "vocabulary":
		if err := runVocab(os.Stdout, opts.Args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
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

	cfg, err := configReadStrict()
	if err != nil {
		logf("CONFIG", "%v", err)
		return 1
	}
	settings, err := cfg.resolveSTT(modelOverride, workspaceLoad())
	if err != nil {
		logf("CONFIG", "%v", err)
		return 1
	}

	result, res, err := runPreflight(cfg, settings)
	switch result {
	case preflightAlreadyRunning:
		// Matches v4: an existing daemon is a successful no-op, which is what
		// makes the F10 `curl /exit || voice-type` toggle work.
		logf("PREFLIGHT", "%v", err)
		return 0
	case preflightFailed:
		logf("PREFLIGHT", "%v", err)
		return 1
	}
	defer res.Close()

	d := newDaemon(cfg, res, settings, modelOverride)
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

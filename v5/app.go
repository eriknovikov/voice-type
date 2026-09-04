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
	fallback := "none"
	if route := sttRouteFor(spec); route.Fallback != nil {
		fallback = route.Fallback.ID
	}
	vocabulary := "ignores vocabulary"
	if spec.TakesVocabulary() {
		vocabulary = "reads vocabulary"
	}
	workspace := workspaceLoad()
	_, err = fmt.Fprintf(w, "config:     %s\napi_key:    %s\nport:       %d\nmodel:      %s  ($%.3f/hour, %s)\nfallback:   %s\nvocabulary: %d terms (%s)\n",
		configFilePath(), keyStatus, cfg.Port, spec.ID, spec.USDPerHour, vocabulary, fallback,
		len(cfg.activeVocabulary(workspace)), vocabularySources(cfg, workspace))
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
	if spec := cfg.modelSpec(); !spec.TakesVocabulary() {
		fmt.Fprintf(w, "model:   %s (ignores vocabulary -- nothing below is sent)\n", spec.ID)
	}
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
		fmt.Fprintf(w, "%s%s: %d\n", list.Name, marker, len(list.Terms))
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
	fmt.Fprintln(w, "ID                           $/hour   vocabulary  fallback")
	for _, model := range sttModels {
		vocabulary := "no"
		if model.TakesVocabulary() {
			vocabulary = "yes"
		}
		defaultLabel := ""
		if model.ID == defaultModelID {
			defaultLabel = "  (default)"
		}
		fallback := "-"
		if model.FallbackID != "" {
			fallback = model.FallbackID
		}
		fmt.Fprintf(w, "%-28s %7.3f   %-10s  %-14s%s\n", model.ID, model.USDPerHour, vocabulary, fallback, defaultLabel)
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
	case "vocab", "vocabulary":
		if err := runVocab(os.Stdout, flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\nusage: voice-type [start|version|config|models|vocab|config-port]\n", flag.Arg(0))
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

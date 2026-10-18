package voicetype

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const cliUsage = "usage: voice-type [start|version|config|models|vocab|config-port] [--model ID]"

type cliOptions struct {
	Command string
	Model   string
	Args    []string
	Version bool
}

// Parse flags both before and after the command. Other positional arguments
// must be consumed explicitly, never silently ignored by flag.Parse.
func parseCLI(args []string) (cliOptions, error) {
	opts := cliOptions{Command: "start"}
	flags := flag.NewFlagSet("voice-type", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.Model, "model", "", "unsupported transcription model override")
	flags.BoolVar(&opts.Version, "version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	rest := flags.Args()
	if len(rest) > 0 {
		opts.Command, rest = rest[0], rest[1:]
		if opts.Command != "vocab" && opts.Command != "vocabulary" {
			if err := flags.Parse(rest); err != nil {
				return opts, err
			}
			rest = flags.Args()
		}
	}
	modelSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "model" {
			modelSet = true
		}
	})
	if modelSet {
		opts.Model = strings.TrimSpace(opts.Model)
		if opts.Model == "" || strings.HasPrefix(opts.Model, "-") || strings.ContainsFunc(opts.Model, func(r rune) bool { return r <= ' ' }) {
			return opts, errors.New("--model needs a nonempty model ID")
		}
		if opts.Version || (opts.Command != "start" && opts.Command != "config") {
			return opts, errors.New("--model is only valid with start or config")
		}
	}
	if opts.Version && (opts.Command != "start" || len(flags.Args()) > 0 || len(args) > 1) {
		return opts, errors.New("--version cannot be combined with a command or other arguments")
	}
	if err := validateCLICommand(opts.Command, rest); err != nil {
		return opts, err
	}
	opts.Args = rest
	return opts, nil
}

func validateCLICommand(command string, args []string) error {
	switch command {
	case "start", "config", "config-port", "models", "version":
		if len(args) != 0 {
			return fmt.Errorf("unexpected arguments for %s", command)
		}
	case "vocab", "vocabulary":
		if len(args) == 0 || (len(args) == 1 && args[0] == "ls") ||
			(len(args) == 2 && args[0] == "set" && !strings.HasPrefix(args[1], "--")) {
			return nil
		}
		return errors.New("usage: voice-type vocab [ls|set <name|none>]")
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	return nil
}

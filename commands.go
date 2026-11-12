package voicetype

import (
	"errors"
	"sync"
)

// Command is deliberately a small whitelist. Forwarded working directories and
// additional data are never interpreted.
type Command string

const (
	CommandShow            Command = "show-settings"
	CommandBackground      Command = "background"
	CommandToggle          Command = "toggle"
	CommandQuit            Command = "quit"
	CommandRemoveShortcuts Command = "remove-shortcuts"
	CommandHelp            Command = "help"
	CommandVersion         Command = "version"
	CommandDoctor          Command = "doctor"
	CommandUpdate          Command = "update"
)
const CommandUsage = "usage: voice-type [--background|--toggle|--quit|--version|--help] | voice-type doctor | voice-type update"

func ParseCommand(args []string) (Command, error) {
	if len(args) == 0 {
		return CommandShow, nil
	}
	if len(args) == 1 {
		switch args[0] {
		case "--version":
			return CommandVersion, nil
		case "--background":
			return CommandBackground, nil
		case "--toggle":
			return CommandToggle, nil
		case "--quit":
			return CommandQuit, nil
		case "--remove-shortcuts":
			return CommandRemoveShortcuts, nil
		case "--help", "-h":
			return CommandHelp, nil
		case "doctor":
			return CommandDoctor, nil
		case "update":
			return CommandUpdate, nil
		}
	}
	return "", errors.New("unknown or conflicting application commands")
}

// Startup is the only time commands are buffered. Dispatch must return promptly:
// recording work runs asynchronously and is never queued behind transcription.
// A bound prevents an unready instance from accumulating unlimited requests.
type commandRouter struct {
	mu            sync.Mutex
	ready, closed bool
	pending       []Command
	dispatch      func(Command)
}

func (r *commandRouter) submit(command Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || command == CommandBackground {
		return
	}
	if r.ready {
		r.dispatch(command)
		return
	}
	if len(r.pending) < 32 {
		r.pending = append(r.pending, command)
	}
}
func (r *commandRouter) start(recordingReady bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.ready = true
	for _, command := range r.pending {
		if command == CommandToggle && !recordingReady {
			command = CommandShow
		}
		r.dispatch(command)
	}
	r.pending = nil
}
func (r *commandRouter) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.pending = nil
}

func (r *commandRouter) active() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready && !r.closed
}

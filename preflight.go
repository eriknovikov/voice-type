package main

import (
	"fmt"
	"net"
	"time"
)

// preflightResult distinguishes "already running" (a clean no-op exit) from a
// real misconfiguration.
type preflightResult int

const (
	preflightOK preflightResult = iota
	preflightAlreadyRunning
	preflightFailed
)

// resources are the long-lived handles preflight proves are usable. They are
// handed to the daemon rather than recreated, so the 201 ms keyboard
// registration and the Pulse connect are paid exactly once.
type resources struct {
	Typer    *Typer
	Recorder audioRecorder
}

func (r *resources) Close() {
	if r == nil {
		return
	}
	if r.Typer != nil {
		r.Typer.Close()
	}
	if r.Recorder != nil {
		r.Recorder.Close()
	}
}

// portInUse reports whether something is already listening on the daemon port.
func portInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// runPreflight mirrors the order in src/preflight.ts.
func runPreflight(cfg Config) (preflightResult, *resources, error) {
	// 1. Port free -- in use means a daemon is already up, which is not a failure.
	if portInUse(cfg.Port) {
		return preflightAlreadyRunning, nil, fmt.Errorf("port %d is already in use -- daemon already running?", cfg.Port)
	}

	// 2. API key present.
	if configAPIKey(cfg) == "" {
		return preflightFailed, nil, fmt.Errorf(
			"no OpenRouter API key. Set OPENROUTER_API_KEY in the environment, or add\n"+
				"  \"api_key\": \"sk-or-...\"\n"+
				"to %s", configFilePath())
	}

	// 3. /dev/uinput usable. Creating the keyboard is the real test -- opening
	//    the device can succeed where the ioctl setup still fails.
	typer, err := newTyper()
	if err != nil {
		return preflightFailed, nil, fmt.Errorf(
			"cannot create the virtual keyboard: %w\n"+
				"voice-type needs write access to /dev/uinput. Add yourself to the 'input' group:\n"+
				"  sudo usermod -aG input $USER\n"+
				"then log out and back in (or run: newgrp input)", err)
	}

	// 4. PulseAudio reachable.
	rec, err := newRecorder()
	if err != nil {
		typer.Close()
		return preflightFailed, nil, fmt.Errorf(
			"cannot reach the audio server: %w\n"+
				"voice-type needs PulseAudio or pipewire-pulse running", err)
	}

	return preflightOK, &resources{Typer: typer, Recorder: rec}, nil
}

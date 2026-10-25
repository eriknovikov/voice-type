package voicetype

import (
	"context"
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

type resources struct {
	Recorder audioRecorder
}

func (r *resources) Close() {
	if r == nil {
		return
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

func runPreflight(cfg Config, settings sttSettings) (preflightResult, *resources, error) {
	// 1. Port free -- in use means a daemon is already up, which is not a failure.
	if portInUse(cfg.Port) {
		return preflightAlreadyRunning, nil, fmt.Errorf("port %d is already in use -- daemon already running?", cfg.Port)
	}

	// 2. API key present.
	if err := settings.validateCredentials(); err != nil {
		return preflightFailed, nil, err
	}

	// 3. IBus usable. Restore the active engine immediately; a temporary
	//    insertion engine must not stay selected while the daemon is idle.
	inserter, err := NewTextInserter(context.Background(), InsertBackendIBus)
	if err != nil {
		return preflightFailed, nil, fmt.Errorf("cannot prepare IBus text insertion: %w", err)
	}
	if err := inserter.Close(); err != nil {
		return preflightFailed, nil, fmt.Errorf("cannot restore IBus after preflight: %w", err)
	}

	// 4. PulseAudio reachable.
	rec, err := NewRecorder()
	if err != nil {
		return preflightFailed, nil, fmt.Errorf(
			"cannot reach the audio server: %w\n"+
				"voice-type needs PulseAudio or pipewire-pulse running", err)
	}

	return preflightOK, &resources{Recorder: rec}, nil
}

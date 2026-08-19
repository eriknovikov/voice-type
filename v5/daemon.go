package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sessionState is the dictation state machine:
//
//	idle --/start--> recording --/stop--> transcribing --> idle
//	                     |                      |
//	                     +--- duration cap ------+
type sessionState int

const (
	stateIdle sessionState = iota
	stateRecording
	stateTranscribing
)

func (s sessionState) String() string {
	switch s {
	case stateRecording:
		return "recording"
	case stateTranscribing:
		return "transcribing"
	default:
		return "idle"
	}
}

type daemon struct {
	cfg  Config
	res  *resources
	stt  *sttClient
	srv  *http.Server
	done chan struct{}

	mu    sync.Mutex
	state sessionState
	// capTimer force-stops a session at the fixed duration cap.
	capTimer     *time.Timer
	shuttingDown bool
	sessionWG    sync.WaitGroup
	sessionID    uint64

	exitOnce sync.Once
}

func newDaemon(cfg Config, res *resources) *daemon {
	return &daemon{
		cfg:  cfg,
		res:  res,
		stt:  newSTTClient(configAPIKey(cfg), sttModel),
		done: make(chan struct{}),
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// startSession moves idle -> recording. It reports a conflict if a session is
// already in flight.
func (d *daemon) startSession() (int, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shuttingDown {
		return http.StatusConflict, "shutting down"
	}
	if d.state != stateIdle {
		return http.StatusConflict, fmt.Sprintf("already %s", d.state)
	}

	if err := d.res.Recorder.Start(); err != nil {
		logf("AUDIO", "start failed: %v", err)
		return http.StatusInternalServerError, err.Error()
	}

	d.state = stateRecording
	d.sessionID++
	sessionID := d.sessionID
	d.capTimer = time.AfterFunc(maxDurationSeconds*time.Second, func() {
		d.stopSessionFor(sessionID)
	})

	logf("AUDIO", "recording")
	return http.StatusOK, "recording"
}

// stopSession moves recording -> transcribing -> idle. It runs on the caller's
// goroutine so /stop returns only once the text has landed.
func (d *daemon) stopSession() (int, string) {
	return d.stopSessionFor(0)
}

// stopSessionFor ignores an expired duration timer from an older session. A
// zero session ID is an explicit user stop and applies to the current session.
func (d *daemon) stopSessionFor(sessionID uint64) (int, string) {
	d.mu.Lock()
	if d.state != stateRecording || (sessionID != 0 && sessionID != d.sessionID) {
		state := d.state
		d.mu.Unlock()
		// A stop that arrives while transcribing, while idle, or from an expired
		// timer belonging to an older recording is a no-op.
		return http.StatusOK, fmt.Sprintf("not recording (%s)", state)
	}
	d.sessionWG.Add(1)
	d.state = stateTranscribing
	if sessionID != 0 {
		logf("AUDIO", "duration cap of %ds reached -- stopping", maxDurationSeconds)
	}
	if d.capTimer != nil {
		d.capTimer.Stop()
		d.capTimer = nil
	}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.state = stateIdle
		d.mu.Unlock()
		d.sessionWG.Done()
	}()

	samples := d.res.Recorder.Stop()
	captured := wavDurationSeconds(samples)
	if len(samples) == 0 {
		logf("AUDIO", "no audio captured")
		return http.StatusOK, "no audio captured"
	}
	logf("AUDIO", "captured %.2fs (%d samples)", captured, len(samples))

	if !worthUploading(samples) {
		logf("AUDIO", "no speech in %.2fs -- discarded", captured)
		return http.StatusOK, "no speech detected"
	}

	samples = trimSilence(samples)
	duration := wavDurationSeconds(samples)
	if duration < captured {
		logf("AUDIO", "trimmed %.2fs of silence -> %.2fs", captured-duration, duration)
	}

	wav := wavEncode(samples)

	t0 := time.Now()
	result, err := d.stt.Transcribe(wav)
	if err != nil {
		return d.handleSTTError(err, wav)
	}
	logf("STT", "%.2fs audio -> %d chars in %v (billed %.0fs, $%.6f)",
		duration, len(result.Text), time.Since(t0).Round(time.Millisecond), result.Seconds, result.Cost)

	if result.Text == "" {
		logf("STT", "empty transcript -- nothing to type")
		return http.StatusOK, ""
	}

	outputStart := time.Now()
	if err := d.res.Typer.Paste(result.Text); err != nil {
		logf("OUTPUT", "paste failed: %v", err)
		return http.StatusInternalServerError, fmt.Sprintf("paste failed: %v", err)
	}

	logf("OUTPUT", "typed in %v: %s", time.Since(outputStart).Round(time.Millisecond), truncate(result.Text, 120))
	return http.StatusOK, result.Text
}

// handleSTTError logs the failure at the right severity and keeps the audio
// around rather than silently discarding speech.
func (d *daemon) handleSTTError(err error, wav []byte) (int, string) {
	retained := ""
	if path, werr := retainWAV(wav); werr == nil {
		retained = fmt.Sprintf(" -- audio retained at %s", path)
	} else {
		retained = fmt.Sprintf(" (could not retain audio: %v)", werr)
	}

	var se *sttError
	if errors.As(err, &se) {
		switch {
		case se.Fatal():
			logf("STT", "authentication rejected (%d): %s%s", se.Status, se.Error(), retained)
			logf("STT", "check OPENROUTER_API_KEY or the api_key field in %s", configFilePath())
		case se.Status == http.StatusRequestEntityTooLarge:
			logf("STT", "audio rejected as too large (%d) -- dictate in shorter takes%s", se.Status, retained)
		case se.Retryable():
			logf("STT", "transient failure (%d): %s%s", se.Status, se.Error(), retained)
		default:
			logf("STT", "failed: %v%s", se.Error(), retained)
		}
		return http.StatusBadGateway, se.Error()
	}

	logf("STT", "failed: %v%s", err, retained)
	return http.StatusBadGateway, err.Error()
}

func (d *daemon) currentState() sessionState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func (d *daemon) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "state": d.currentState().String()})
	})

	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		status, msg := d.startSession()
		writeJSON(w, status, map[string]string{"status": statusWord(status), "message": msg})
	})

	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		status, msg := d.stopSession()
		writeJSON(w, status, map[string]string{"status": statusWord(status), "text": msg})
	})

	mux.HandleFunc("/toggle", func(w http.ResponseWriter, r *http.Request) {
		if d.currentState() == stateRecording {
			status, msg := d.stopSession()
			writeJSON(w, status, map[string]string{"status": statusWord(status), "text": msg})
			return
		}
		status, msg := d.startSession()
		writeJSON(w, status, map[string]string{"status": statusWord(status), "message": msg})
	})

	mux.HandleFunc("/exit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "shutting down"})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		d.shutdown()
	})

	return mux
}

func statusWord(status int) string {
	if status >= 200 && status < 300 {
		return "ok"
	}
	return "error"
}

func (d *daemon) shutdown() {
	d.exitOnce.Do(func() {
		logf("DAEMON", "shutting down")
		d.mu.Lock()
		d.shuttingDown = true
		state := d.state
		d.mu.Unlock()

		if state == stateRecording {
			d.stopSession()
		}
		d.sessionWG.Wait()
		close(d.done)
	})
}

// Run serves until /exit or a signal, then drains and releases the handles.
func (d *daemon) Run() error {
	d.srv = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", d.cfg.Port),
		Handler: d.routes(),
	}

	errCh := make(chan error, 1)
	go func() {
		logf("DAEMON", "listening on http://127.0.0.1:%d (model %s)", d.cfg.Port, sttModel)
		if err := d.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		d.shutdown()
		return err
	case <-d.done:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.srv.Shutdown(ctx)
}

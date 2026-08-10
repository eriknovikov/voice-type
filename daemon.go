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
//	                     +--- max_duration ------+
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
	// capTimer force-stops a session that hits max_duration.
	capTimer *time.Timer

	exitOnce sync.Once
}

func newDaemon(cfg Config, res *resources) *daemon {
	return &daemon{
		cfg:  cfg,
		res:  res,
		stt:  newSTTClient(configAPIKey(cfg), cfg.Model),
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
	if d.state != stateIdle {
		state := d.state
		d.mu.Unlock()
		return http.StatusConflict, fmt.Sprintf("already %s", state)
	}
	d.state = stateRecording
	d.mu.Unlock()

	if err := d.res.Recorder.Start(); err != nil {
		d.mu.Lock()
		d.state = stateIdle
		d.mu.Unlock()
		logf("AUDIO", "start failed: %v", err)
		return http.StatusInternalServerError, err.Error()
	}

	// Hard duration cap: without silence detection, a forgotten hotkey would
	// otherwise record (and bill) indefinitely.
	d.mu.Lock()
	d.capTimer = time.AfterFunc(time.Duration(d.cfg.MaxDuration)*time.Second, func() {
		logf("AUDIO", "max_duration of %ds reached -- stopping", d.cfg.MaxDuration)
		d.stopSession()
	})
	d.mu.Unlock()

	logf("AUDIO", "recording")
	return http.StatusOK, "recording"
}

// stopSession moves recording -> transcribing -> idle. It runs on the caller's
// goroutine so /stop returns only once the text has landed.
func (d *daemon) stopSession() (int, string) {
	d.mu.Lock()
	if d.state != stateRecording {
		state := d.state
		d.mu.Unlock()
		// A stop that arrives while transcribing, or while idle, is a no-op.
		return http.StatusOK, fmt.Sprintf("not recording (%s)", state)
	}
	d.state = stateTranscribing
	if d.capTimer != nil {
		d.capTimer.Stop()
		d.capTimer = nil
	}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.state = stateIdle
		d.mu.Unlock()
	}()

	samples := d.res.Recorder.Stop()
	captured := wavDurationSeconds(samples)
	if len(samples) == 0 {
		logf("AUDIO", "no audio captured")
		return http.StatusOK, "no audio captured"
	}
	logf("AUDIO", "captured %.2fs (%d samples)", captured, len(samples))

	if d.cfg.TrimSilence {
		samples = trimSilence(samples)
	}
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
		logf("STT", "empty transcript -- nothing to paste")
		return http.StatusOK, ""
	}

	if err := d.res.Typer.Paste(result.Text); err != nil {
		logf("OUTPUT", "paste failed: %v", err)
		return http.StatusInternalServerError, fmt.Sprintf("paste failed: %v", err)
	}

	logf("OUTPUT", "pasted: %s", truncate(result.Text, 120))
	return http.StatusOK, result.Text
}

// handleSTTError logs the failure at the right severity and, for transient
// errors, keeps the audio around rather than silently discarding speech.
func (d *daemon) handleSTTError(err error, wav []byte) (int, string) {
	var se *sttError
	if errors.As(err, &se) {
		switch {
		case se.Fatal():
			logf("STT", "authentication rejected (%d): %s", se.Status, se.Error())
			logf("STT", "check OPENROUTER_API_KEY or the api_key field in %s", configFilePath())
		case se.Status == http.StatusRequestEntityTooLarge:
			logf("STT", "audio rejected as too large (%d). Lower max_duration (currently %ds) in %s",
				se.Status, d.cfg.MaxDuration, configFilePath())
		case se.Retryable():
			if path, werr := retainWAV(wav); werr == nil {
				logf("STT", "transient failure (%d): %s -- audio retained at %s", se.Status, se.Error(), path)
			} else {
				logf("STT", "transient failure (%d): %s (could not retain audio: %v)", se.Status, se.Error(), werr)
			}
		default:
			logf("STT", "failed: %v", se.Error())
		}
		return http.StatusBadGateway, se.Error()
	}

	logf("STT", "failed: %v", err)
	if path, werr := retainWAV(wav); werr == nil {
		logf("STT", "audio retained at %s", path)
	}
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
		logf("DAEMON", "listening on http://127.0.0.1:%d (model %s)", d.cfg.Port, d.cfg.Model)
		if err := d.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-d.done:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.srv.Shutdown(ctx)
}

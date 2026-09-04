package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// sessionState is the dictation state machine:
//
//	idle --/start--> checking --> recording --/stop--> transcribing --> idle
//	                                   |                      |
//	                                   +--- duration cap ------+
type sessionState int

const (
	stateIdle sessionState = iota
	stateChecking
	stateRecording
	stateTranscribing
)

func (s sessionState) String() string {
	switch s {
	case stateChecking:
		return "checking"
	case stateRecording:
		return "recording"
	case stateTranscribing:
		return "transcribing"
	default:
		return "idle"
	}
}

type daemon struct {
	cfg Config
	// workspace is the active vocabulary workspace, re-read from the state file
	// with the config so `voice-type vocab set` lands on the next dictation.
	workspace string
	res       *resources
	stt       *sttClient
	srv       *http.Server
	done      chan struct{}

	mu    sync.Mutex
	state sessionState
	// capTimer force-stops a session at the fixed duration cap.
	capTimer      *time.Timer
	shuttingDown  bool
	sessionWG     sync.WaitGroup
	sessionID     uint64
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	checkOnline   func(context.Context) error

	exitOnce sync.Once
}

func newDaemon(cfg Config, res *resources) *daemon {
	workspace := workspaceLoad()
	return &daemon{
		cfg:         cfg,
		workspace:   workspace,
		res:         res,
		stt:         newSTTClient(configAPIKey(cfg), cfg.modelSpec(), cfg.activeVocabulary(workspace)),
		done:        make(chan struct{}),
		checkOnline: checkConnectivity,
	}
}

const connectivityURL = "https://connectivitycheck.gstatic.com/generate_204"

var connectivityClient = &http.Client{
	Timeout: time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func checkConnectivity(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, connectivityURL, nil)
	if err != nil {
		return err
	}
	resp, err := connectivityClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("connectivity check returned HTTP %d", resp.StatusCode)
	}
	return nil
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
	if d.shuttingDown {
		d.mu.Unlock()
		return http.StatusConflict, "shutting down"
	}
	if d.state != stateIdle {
		state := d.state
		d.mu.Unlock()
		return http.StatusConflict, fmt.Sprintf("already %s", state)
	}

	d.reloadConfig()
	d.state = stateChecking
	d.sessionID++
	sessionID := d.sessionID
	ctx, cancel := context.WithCancel(context.Background())
	d.sessionCtx = ctx
	d.sessionCancel = cancel
	d.sessionWG.Add(1)
	d.mu.Unlock()

	started := false
	defer func() {
		if !started {
			d.mu.Lock()
			if d.sessionID == sessionID && d.state == stateChecking {
				d.state = stateIdle
				d.sessionCtx = nil
				d.sessionCancel = nil
			}
			d.mu.Unlock()
			cancel()
		}
		d.sessionWG.Done()
	}()

	if err := d.checkOnline(ctx); err != nil {
		d.mu.Lock()
		cancelled := ctx.Err() != nil || d.shuttingDown || d.sessionID != sessionID
		d.mu.Unlock()
		if cancelled {
			return http.StatusConflict, "shutting down"
		}
		logf("NETWORK", "offline: %v", err)

		typeErr := d.res.Typer.TypeContext(ctx, "?")
		if ctx.Err() != nil {
			return http.StatusConflict, "shutting down"
		}
		if typeErr != nil {
			logf("OUTPUT", "offline marker failed: %v", typeErr)
			return http.StatusInternalServerError, fmt.Sprintf("offline; type marker: %v", typeErr)
		}
		return http.StatusServiceUnavailable, "offline"
	}

	d.mu.Lock()
	if d.shuttingDown || ctx.Err() != nil || d.sessionID != sessionID || d.state != stateChecking {
		d.mu.Unlock()
		return http.StatusConflict, "shutting down"
	}
	d.mu.Unlock()

	err := d.res.Recorder.Start()
	d.mu.Lock()
	if err != nil {
		d.mu.Unlock()
		logf("AUDIO", "start failed: %v", err)
		return http.StatusInternalServerError, err.Error()
	}
	if d.shuttingDown || ctx.Err() != nil || d.sessionID != sessionID || d.state != stateChecking {
		d.mu.Unlock()
		d.res.Recorder.Stop()
		return http.StatusConflict, "shutting down"
	}

	d.state = stateRecording
	d.capTimer = time.AfterFunc(maxDurationSeconds*time.Second, func() {
		d.stopSessionFor(sessionID)
	})
	started = true
	d.mu.Unlock()

	return http.StatusOK, "recording"
}

// reloadConfig re-reads the config file and the active workspace so a hand edit
// or a `vocab set` applies to this dictation without a restart. Callers hold
// d.mu. The bound port is the one exception: the listener is already up on the
// old one.
func (d *daemon) reloadConfig() {
	d.workspace = workspaceLoad()

	cfg, err := configReadStrict()
	if err != nil {
		configWarn("file", err.Error()+" -- keeping current configuration")
		d.stt.vocabulary = d.cfg.activeVocabulary(d.workspace)
		return
	}
	lastWarning.Delete("file")
	cfg.Port = d.cfg.Port
	d.cfg = cfg

	d.stt.apiKey = configAPIKey(cfg)
	d.stt.setRoute(cfg.modelSpec())
	d.stt.vocabulary = cfg.activeVocabulary(d.workspace)
}

// stopSession moves recording -> transcribing -> idle. It runs on the caller's
// goroutine so /stop returns only once the text has landed.
func (d *daemon) stopSession() (int, string) {
	d.mu.Lock()
	if d.state == stateChecking {
		if d.sessionCancel != nil {
			d.sessionCancel()
		}
		d.mu.Unlock()
		return http.StatusOK, "cancelled"
	}
	d.mu.Unlock()
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
	ctx := d.sessionCtx
	stt := d.stt
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
		if d.sessionCancel != nil {
			d.sessionCancel()
		}
		d.sessionCtx = nil
		d.sessionCancel = nil
		d.mu.Unlock()
		d.sessionWG.Done()
	}()

	samples, recordErr := d.res.Recorder.Stop()
	if err := ctx.Err(); err != nil {
		logf("AUDIO", "recording discarded during shutdown")
		return http.StatusOK, "cancelled"
	}
	if recordErr != nil {
		logf("AUDIO", "capture failed: %v", recordErr)
		return http.StatusInternalServerError, recordErr.Error()
	}
	captured := wavDurationSeconds(samples)
	if len(samples) == 0 {
		logf("AUDIO", "no audio captured")
		return http.StatusOK, "no audio captured"
	}
	levels := measureAudioLevels(samples)
	analysis := analyzeSpeech(samples)
	logf("AUDIO", "captured %.2fs (%d samples); peak RMS %.0f, noise floor %.0f, gate %.0f",
		captured, len(samples), levels.peakRMS, levels.noiseFloor, analysis.gate)

	noSpeech := false
	if captured < minCaptureSeconds {
		logf("AUDIO", "capture shorter than %.2fs -- discarded", minCaptureSeconds)
		noSpeech = true
	} else if !analysis.detected {
		logf("AUDIO", "no speech in %.2fs; longest active span %dms, candidate ZCR %.3f -- discarded",
			captured, analysis.maxActiveFrames*20, analysis.zeroCrossingRate)
		noSpeech = true
	}
	if noSpeech {
		if err := d.res.Typer.TypeContext(ctx, "?"); err != nil {
			if errors.Is(err, context.Canceled) {
				logf("OUTPUT", "no-speech marker cancelled")
				return http.StatusOK, "cancelled"
			}
			logf("OUTPUT", "no-speech marker failed: %v", err)
			return http.StatusInternalServerError, fmt.Sprintf("no speech detected; type marker: %v", err)
		}
		logf("OUTPUT", "typed no-speech marker")
		return http.StatusOK, "no speech detected"
	}

	samples = TrimSilence(samples)
	duration := wavDurationSeconds(samples)
	if duration < captured {
		logf("AUDIO", "trimmed %.2fs of silence -> %.2fs", captured-duration, duration)
	}

	wav := WavEncode(samples)
	if err := ctx.Err(); err != nil {
		logf("STT", "cancelled before upload")
		return http.StatusOK, "cancelled"
	}

	t0 := time.Now()
	result, err := stt.TranscribeContext(ctx, wav)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logf("STT", "cancelled during shutdown")
			return http.StatusOK, "cancelled"
		}
		return d.handleSTTError(err, wav)
	}
	logf("STT", "%s: %.2fs audio -> %d chars in %v (billed %.0fs, $%.6f)",
		result.ModelID, duration, len(result.Text), time.Since(t0).Round(time.Millisecond), result.Seconds, result.Cost)

	if result.Text == "" {
		logf("STT", "empty transcript -- nothing to type")
		return http.StatusOK, ""
	}

	producingModel, ok := sttLookup(result.ModelID)
	if !ok {
		producingModel = stt.model
	}
	prompt := sttVocabularyPrompt(producingModel, stt.vocabulary)
	if vocabularyEcho(result.Text, prompt) {
		retained := ""
		if path, retainErr := retainWAV(wav); retainErr == nil {
			retained = "audio retained at " + path
		} else {
			retained = fmt.Sprintf("could not retain audio: %v", retainErr)
		}
		logf("STT", "rejected vocabulary echo after %.2fs capture (peak RMS %.0f, noise floor %.0f) -- %s",
			captured, levels.peakRMS, levels.noiseFloor, retained)
		return http.StatusBadGateway, "transcription matched the vocabulary prompt; " + retained
	}
	if strings.HasPrefix(normalizeTranscript(result.Text), "vocabulary:") {
		logf("STT", "possible vocabulary echo contained other words; preserving transcript: %s", truncate(result.Text, 120))
	}
	if err := ctx.Err(); err != nil {
		logf("OUTPUT", "cancelled before insertion")
		return http.StatusOK, "cancelled"
	}

	outputStart := time.Now()
	if err := d.res.Typer.PasteContext(ctx, result.Text); err != nil {
		if errors.Is(err, context.Canceled) {
			logf("OUTPUT", "cancelled before insertion")
			return http.StatusOK, "cancelled"
		}
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
		d.mu.Lock()
		cfg, state, workspace := d.cfg, d.state, d.workspace
		terms := len(cfg.vocabularyFor(workspace))
		d.mu.Unlock()
		route := sttRouteFor(cfg.modelSpec())
		fallback := ""
		if route.Fallback != nil {
			fallback = route.Fallback.ID
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok", "state": state.String(),
			"model": cfg.Model, "fallback": fallback,
			"workspace": workspaceLabel(workspace), "vocabulary": terms,
		})
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
		state := d.currentState()
		if state == stateChecking || state == stateRecording {
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
		if d.sessionCancel != nil {
			d.sessionCancel()
		}
		if d.capTimer != nil {
			d.capTimer.Stop()
			d.capTimer = nil
		}
		if state == stateRecording || state == stateChecking {
			d.state = stateIdle
			d.sessionID++
		}
		d.mu.Unlock()

		if state == stateRecording {
			d.res.Recorder.Stop()
			logf("AUDIO", "recording discarded during shutdown")
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
		logf("DAEMON", "listening on http://127.0.0.1:%d (model %s, workspace %s, %d vocabulary terms)",
			d.cfg.Port, d.cfg.Model, workspaceLabel(d.workspace), len(d.cfg.vocabularyFor(d.workspace)))
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

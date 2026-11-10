package voicetype

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// sessionState is the dictation state machine:
//
//	idle --> checking --> recording --> transcribing --> idle
//	                                   |                      |
//	                                   +--- duration cap ------+
type sessionState int

const (
	stateIdle sessionState = iota
	stateChecking
	stateRecording
	stateTranscribing
	stateTesting
)

func (s sessionState) String() string {
	switch s {
	case stateTesting:
		return "testing"
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

type engine struct {
	toggleMu          sync.Mutex
	lock              *os.File
	cfg               Config
	configError       string
	notice            string
	noticeVersion     uint64
	recoveryVersion   uint64
	lastAttemptFailed bool
	recorder          audioRecorder
	stt               *sttClient
	localModels       *LocalModelManager
	runLog            *runLog

	mu    sync.Mutex
	state sessionState
	// capTimer force-stops a session at the fixed duration cap.
	capTimer      *time.Timer
	shuttingDown  bool
	sessionWG     sync.WaitGroup
	sessionID     uint64
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	activeLocal   *localModelUse
	activeStream  *localStreamSession

	testRecorder *Recorder
	testTimer    *time.Timer
	exitOnce     sync.Once
}

func newEngine(cfg Config, recorder audioRecorder, settings sttSettings) *engine {
	notice, recoveryVersion := configRecoveryNotice()
	models := newLocalModelManager()
	d := &engine{
		cfg:             cfg,
		recorder:        recorder,
		stt:             newSTTClientWithModels(settings, models),
		localModels:     models,
		recoveryVersion: recoveryVersion,
	}
	if notice != "" {
		d.showNotice(notice)
	}
	return d
}

// Call with d.mu held, or before publishing the engine to other goroutines.
func (d *engine) showNotice(message string) {
	d.notice = message
	d.noticeVersion++
}
func (d *engine) checkConfigRecovery() {
	message, version := configRecoveryNotice()
	if version != d.recoveryVersion {
		d.recoveryVersion = version
		if message != "" {
			d.showNotice(message)
			d.runLog.diagnostic("configuration.recovered", "ok", message)
		}
	}
}

// startSession moves idle -> recording. It reports a conflict if a session is
// already in flight.
func (d *engine) startSession() error {
	d.mu.Lock()
	if d.shuttingDown {
		d.mu.Unlock()
		return errors.New("shutting down")
	}
	if d.state != stateIdle {
		state := d.state
		d.mu.Unlock()
		return fmt.Errorf("already %s", state)
	}

	d.lastAttemptFailed = false
	if err := d.reloadConfig(); err != nil {
		d.lastAttemptFailed = true
		d.runLog.complete(d.cfg.Provider, providerModel(d.cfg.Provider), "", "configuration_error", 0, 0, nil, err)
		d.configError = err.Error()
		d.mu.Unlock()
		logf("CONFIG", "recording blocked: %v", err)
		return err
	}
	d.configError = ""
	d.state = stateChecking
	stt := d.stt
	d.sessionID++
	sessionID := d.sessionID
	ctx, cancel := context.WithCancel(context.Background())
	d.sessionCtx = ctx
	d.sessionCancel = cancel
	d.sessionWG.Add(1)
	d.mu.Unlock()

	var localUse *localModelUse
	var localStream *localStreamSession
	var err error
	cleanupLocal := func() {
		if setter, ok := d.recorder.(interface{ SetChunkSink(func([]int16)) }); ok {
			setter.SetChunkSink(nil)
		}
		if localStream != nil {
			localStream.Abort()
		}
		if localUse != nil {
			localUse.close()
		}
	}
	started := false
	defer func() {
		if !started {
			cleanupLocal()
			d.mu.Lock()
			if d.sessionID == sessionID && d.state == stateChecking {
				d.state = stateIdle
				d.sessionCtx = nil
				d.sessionCancel = nil
			}
			if d.activeLocal == localUse {
				d.activeLocal = nil
			}
			if d.activeStream == localStream {
				d.activeStream = nil
			}
			d.mu.Unlock()
			cancel()
		}
		d.sessionWG.Done()
	}()

	d.mu.Lock()
	if d.shuttingDown || ctx.Err() != nil || d.sessionID != sessionID || d.state != stateChecking {
		d.mu.Unlock()
		return errors.New("shutting down")
	}
	d.mu.Unlock()

	if stt != nil && stt.local != nil {
		localUse, err = stt.local.models.acquire(ctx, stt.settings.Model)
		if err != nil {
			d.mu.Lock()
			d.lastAttemptFailed = true
			d.runLog.complete(stt.settings.Provider, stt.settings.Model, "", "local_model_unavailable", 0, 0, nil, err)
			d.mu.Unlock()
			logf("LOCAL", "recording blocked: %v", err)
			return err
		}
		if mode := stt.settings.StreamingMode; mode == streamingModeProcess || mode == streamingModeLive {
			localStream, err = startLocalStream(ctx, localUse, mode, stt.settings.Vocabulary)
			if err != nil {
				d.mu.Lock()
				d.lastAttemptFailed = true
				d.runLog.complete(stt.settings.Provider, stt.settings.Model, "", "local_stream_start_error", 0, 0, nil, err)
				d.mu.Unlock()
				logf("LOCAL", "streaming could not start: %v", err)
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if setter, ok := d.recorder.(interface{ SetChunkSink(func([]int16)) }); ok && localStream != nil {
		setter.SetChunkSink(localStream.OfferSamples)
	}

	if rec, ok := d.recorder.(*Recorder); ok {
		rec.SetSource(d.cfg.Microphone)
	}
	err = d.recorder.Start()
	d.mu.Lock()
	if rec, ok := d.recorder.(*Recorder); ok && rec.sourceFellBack() {
		d.showNotice(microphoneFallbackNotice)
		d.runLog.diagnostic("microphone.changed", "ok", microphoneFallbackNotice)
	}
	if err != nil {
		d.lastAttemptFailed = true
		provider, model := d.cfg.Provider, providerModel(d.cfg.Provider)
		if stt != nil {
			provider, model = stt.settings.Provider, stt.settings.Model
		}
		d.runLog.complete(provider, model, "", "capture_error", 0, 0, nil, err)
		d.mu.Unlock()
		logf("AUDIO", "start failed: %v", err)
		return err
	}
	if d.shuttingDown || ctx.Err() != nil || d.sessionID != sessionID || d.state != stateChecking {
		d.mu.Unlock()
		d.recorder.Stop()
		cleanupLocal()
		return errors.New("shutting down")
	}

	d.state = stateRecording
	d.activeLocal = localUse
	d.activeStream = localStream
	d.capTimer = time.AfterFunc(maxDurationSeconds*time.Second, func() {
		d.stopSessionMatching(sessionID, true)
	})
	started = true
	d.mu.Unlock()

	return nil
}

// reloadConfig re-reads the config file and the active workspace so a hand edit
// or a desktop edit applies to this dictation without a restart. Callers hold
// d.mu. Each recording keeps its selected profile until completion.
func (d *engine) reloadConfig() error {
	workspace := workspaceLoad()
	cfg, err := configReadStrict()
	d.checkConfigRecovery()
	if err != nil {
		return err
	}
	settings, err := cfg.resolveSTT(workspace)
	if err != nil {
		return err
	}
	if err := settings.validateCredentials(); err != nil {
		return err
	}
	d.cfg = cfg
	d.stt = newSTTClientWithModels(settings, d.localModels)
	return nil
}

func (d *engine) stopSessionMatching(sessionID uint64, durationCap bool) (operationErr error) {
	d.mu.Lock()
	if d.state != stateRecording || (sessionID != 0 && sessionID != d.sessionID) {
		d.mu.Unlock()
		// A stop that arrives while transcribing, while idle, or from an expired
		// timer belonging to an older recording is a no-op.
		return nil
	}
	d.sessionWG.Add(1)
	d.state = stateTranscribing
	ctx := d.sessionCtx
	stt := d.stt
	localUse := d.activeLocal
	localStream := d.activeStream
	if durationCap {
		logf("AUDIO", "duration cap of %ds reached -- stopping", maxDurationSeconds)
	}
	if d.capTimer != nil {
		d.capTimer.Stop()
		d.capTimer = nil
	}
	d.mu.Unlock()

	streamFinished := false
	defer func() {
		if localStream != nil && !streamFinished {
			localStream.Abort()
		}
		if localUse != nil {
			localUse.close()
		}
		d.mu.Lock()
		if d.activeStream == localStream {
			d.activeStream = nil
		}
		if d.activeLocal == localUse {
			d.activeLocal = nil
		}
		d.state = stateIdle
		if operationErr != nil && !d.shuttingDown {
			d.lastAttemptFailed = true
		}
		if d.sessionCancel != nil {
			d.sessionCancel()
		}
		d.sessionCtx = nil
		d.sessionCancel = nil
		if !d.shuttingDown {
			if err := d.reloadConfig(); err != nil {
				d.configError = err.Error()
			} else {
				d.configError = ""
			}
		}
		d.mu.Unlock()
		d.sessionWG.Done()
	}()

	samples, recordErr := d.recorder.Stop()
	if err := ctx.Err(); err != nil {
		logf("AUDIO", "recording discarded during shutdown")
		return nil
	}
	if recordErr != nil {
		if stt.local != nil {
			wav := WavEncode(samples)
			retained := ""
			if path, retainErr := retainWAV(wav); retainErr == nil {
				retained = " -- audio retained at " + path
			}
			d.runLog.complete(stt.settings.Provider, stt.settings.Model, "", "capture_error", 0, wavDurationSeconds(samples), nil, recordErr)
			logf("AUDIO", "capture failed: %v%s", recordErr, retained)
			return fmt.Errorf("capture failed: %w%s", recordErr, retained)
		}
		d.runLog.complete(stt.settings.Provider, stt.settings.Model, "", "capture_error", 0, 0, nil, recordErr)
		logf("AUDIO", "capture failed: %v", recordErr)
		return recordErr
	}
	duration := wavDurationSeconds(samples)
	if len(samples) == 0 {
		logf("AUDIO", "no audio captured")
		d.runLog.complete(stt.settings.Provider, stt.settings.Model, "", "no_audio", 0, 0, nil, errors.New("no audio captured"))
		return errors.New("no audio captured")
	}
	logf("AUDIO", "captured %.2fs (%d samples)", duration, len(samples))

	wav := WavEncode(samples)
	if err := ctx.Err(); err != nil {
		logf("STT", "cancelled before upload")
		return nil
	}

	requestedModel := stt.settings.Model
	t0 := time.Now()
	attempts := make([]runLogAttempt, 0, sttMaxAttempts)
	outcome := ""
	observe := func(model string, attempt int, requestDuration time.Duration, result sttResult, err error) {
		if stt.local != nil {
			attempt = len(attempts) + 1
		}
		status := http.StatusOK
		if err != nil {
			status = 0
			var sttErr *sttError
			if errors.As(err, &sttErr) {
				status = sttErr.Status
			}
		}
		attempts = append(attempts, runLogAttempt{
			Provider:   stt.settings.Provider,
			Model:      model,
			StatusCode: status,
			DurationMS: requestDuration.Milliseconds(),
			Error:      safeRequestError(err),
		})
	}
	var result sttResult
	var err error
	liveInserted := false
	if stt.local != nil {
		if localStream != nil {
			streamStarted := time.Now()
			streamResult := localStream.Finish()
			streamFinished = true
			streamDuration := time.Since(streamStarted)
			streamErr := streamResult.Err
			streamResultValue := sttResult{Text: streamResult.Text, ModelID: requestedModel, Offline: true}
			observe(requestedModel, 1, streamDuration, streamResultValue, streamErr)
			if streamErr != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return nil
				}
				if stt.settings.StreamingMode == streamingModeProcess {
					logf("LOCAL", "streaming failed; retrying the full recording locally: %v", streamErr)
					result, err = stt.local.transcribeSamples(ctx, samples, observe, localUse)
				} else {
					outcome = "local_stream_error"
					err = streamErr
				}
			} else {
				result = streamResultValue
				liveInserted = stt.settings.StreamingMode == streamingModeLive
				if liveInserted && result.Text != "" && streamResult.Inserted == "" {
					err = errors.New("live streaming finished without any committed text; recording retained")
					outcome = "local_stream_error"
				}
			}
		} else {
			result, err = stt.local.transcribeSamples(ctx, samples, observe, localUse)
		}
	} else {
		result, err = stt.transcribeContext(ctx, wav, observe)
	}
	requestDuration := time.Since(t0)
	finalModel := result.ModelID
	if finalModel == "" && len(attempts) > 0 {
		finalModel = attempts[len(attempts)-1].Model
	}
	defer func() {
		if outcome == "" {
			outcome = requestOutcome(err)
		}
		logErr := err
		if logErr == nil {
			logErr = operationErr
		}
		d.runLog.complete(stt.settings.Provider, requestedModel, finalModel, outcome, time.Since(t0), duration, attempts, logErr)
	}()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logf("STT", "cancelled during shutdown")
			return nil
		}
		return d.handleSTTError(err, wav)
	}
	logf("STT", "%s/%s: %.2fs audio -> %d chars in %v (%s)",
		stt.settings.Provider, result.ModelID, duration, len(result.Text), requestDuration.Round(time.Millisecond), result.billingSummary())

	if result.Text == "" {
		logf("STT", "empty transcript -- nothing to insert")
		outcome = "empty_transcript"
		return errors.New("provider returned no text; nothing was inserted")
	}

	prompt := stt.settings.vocabularyPrompt()
	if vocabularyEcho(result.Text, prompt) {
		retained := ""
		if path, retainErr := retainWAV(wav); retainErr == nil {
			retained = "audio retained at " + path
		} else {
			retained = fmt.Sprintf("could not retain audio: %v", retainErr)
		}
		logf("STT", "rejected vocabulary echo after %.2fs capture -- %s", duration, retained)
		outcome = "vocabulary_echo"
		return errors.New("transcription matched the vocabulary prompt; " + retained)
	}
	if prompt != "" && strings.HasPrefix(normalizeTranscript(result.Text), "vocabulary:") {
		logf("STT", "possible vocabulary echo contained other words; preserving transcript")
	}
	if err := ctx.Err(); err != nil {
		logf("OUTPUT", "cancelled before insertion")
		return nil
	}
	if liveInserted {
		logf("OUTPUT", "live insertion completed with %d committed characters", len(result.Text))
		return nil
	}

	outputStart := time.Now()
	if err := insertText(ctx, result.Text); err != nil {
		if errors.Is(err, context.Canceled) {
			logf("OUTPUT", "cancelled before insertion")
			return nil
		}
		logf("OUTPUT", "IBus insertion failed: %v", err)
		outcome = "insertion_error"
		return fmt.Errorf("IBus insertion failed: %w", err)
	}

	logf("OUTPUT", "submitted via IBus in %v", time.Since(outputStart).Round(time.Millisecond))
	return nil
}

// handleSTTError logs the failure at the right severity and keeps the audio
// around rather than silently discarding speech.
func (d *engine) handleSTTError(err error, wav []byte) error {
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
			logf("STT", "check the API key in the selected profile in General")
		case se.Quota:
			logf("STT", "%s quota or credit exhausted: %s%s", se.Provider, se.Error(), retained)
		case se.Status == http.StatusRequestEntityTooLarge:
			logf("STT", "audio rejected as too large (%d) -- dictate in shorter takes%s", se.Status, retained)
		case se.Retryable():
			logf("STT", "transient failure (%d): %s%s", se.Status, se.Error(), retained)
		default:
			logf("STT", "failed: %v%s", se.Error(), retained)
		}
		return se
	}

	logf("STT", "failed: %v%s", err, retained)
	return err
}

func (d *engine) Close() {
	d.exitOnce.Do(func() {
		logf("APP", "shutting down")
		d.mu.Lock()
		d.shuttingDown = true
		state := d.state
		activeStream := d.activeStream
		activeLocal := d.activeLocal
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
			d.recorder.Stop()
			if activeStream != nil {
				activeStream.Abort()
			}
			if activeLocal != nil {
				activeLocal.close()
			}
			d.mu.Lock()
			d.activeStream = nil
			d.activeLocal = nil
			d.mu.Unlock()
			logf("AUDIO", "recording discarded during shutdown")
		}
		d.sessionWG.Wait()
		d.StopMicrophoneTest()
		if d.recorder != nil {
			d.recorder.Close()
		}
		if d.localModels != nil {
			d.localModels.Close()
		}
		d.runLog.close("ok")
		if d.lock != nil {
			d.lock.Close()
		}
	})
}

func (d *engine) StartMicrophoneTest(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != stateIdle || d.shuttingDown {
		return fmt.Errorf("cannot test microphone while %s", d.state)
	}
	rec := &Recorder{sourceID: id, meterOnly: true}
	if err := rec.Start(); err != nil {
		rec.Close()
		return err
	}
	if rec.sourceFellBack() {
		d.showNotice(microphoneFallbackNotice)
		d.runLog.diagnostic("microphone.changed", "ok", microphoneFallbackNotice)
	}
	d.testRecorder = rec
	d.state = stateTesting
	d.testTimer = time.AfterFunc(60*time.Second, d.StopMicrophoneTest)
	return nil
}
func (d *engine) StopMicrophoneTest() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.testTimer != nil {
		d.testTimer.Stop()
		d.testTimer = nil
	}
	if d.testRecorder != nil {
		d.testRecorder.Stop()
		d.testRecorder.Close()
		d.testRecorder = nil
		if d.state == stateTesting {
			d.state = stateIdle
		}
	}
}

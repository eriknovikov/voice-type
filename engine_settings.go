package voicetype

import "errors"

type Status struct {
	LastAttemptFailed bool    `json:"last_attempt_failed"`
	State             string  `json:"state"`
	Error             string  `json:"error"`
	Level             float64 `json:"level"`
	Notice            string  `json:"notice"`
	NoticeVersion     uint64  `json:"notice_version"`
}

// startEngine acquires ownership before reading settings or opening logs.
func startEngine(version string) (*engine, error) {
	lock, err := acquireEngineLock()
	if err != nil {
		return nil, err
	}
	cfg, configErr := configReadStrict()
	if configErr != nil {
		cfg = configDefaults()
	}
	// Device enumeration failure is not evidence that the saved device vanished.
	// Only a successful enumeration may replace a saved preference.
	microphoneChanged := false
	if cfg.Microphone != "" {
		if sources, e := ListAudioSources(); e == nil {
			if changed, e := reconcileMicrophone(cfg.Microphone, sources); e != nil {
				configErr = e
			} else if changed {
				cfg.Microphone = ""
				microphoneChanged = true
			}
		}
	}
	settings, err := cfg.resolveSTT(workspaceLoad())
	if err != nil {
		if configErr == nil {
			configErr = err
		}
		settings = sttSettings{Provider: cfg.Provider, Model: providerModel(cfg.Provider)}
	}
	d := newEngine(cfg, &Recorder{}, settings)
	d.lock = lock
	d.runLog = openRunLog(version)
	if microphoneChanged {
		d.showNotice(microphoneFallbackNotice)
		d.runLog.diagnostic("microphone.changed", "ok", microphoneFallbackNotice)
	}
	if notice := ConfigNotice(); notice != "" {
		d.runLog.diagnostic("configuration.recovered", "ok", notice)
	}
	if configErr != nil {
		d.configError = configErr.Error()
		d.runLog.diagnostic("configuration.failed", "error", "Configuration could not be loaded. Open settings to repair it.")
	} else if err := settings.validateCredentials(); err != nil {
		d.configError = err.Error()
	}
	return d, nil
}

func (d *engine) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.checkConfigRecovery()
	level := float64(0)
	if d.testRecorder != nil {
		level = d.testRecorder.Level()
	}
	return Status{LastAttemptFailed: d.lastAttemptFailed, State: d.state.String(), Error: d.configError, Level: level, Notice: d.notice, NoticeVersion: d.noticeVersion}
}
func (d *engine) SaveSettings(update SettingsUpdate) (Settings, error) {
	value, err := SaveSettings(update)
	if err == nil {
		d.RefreshSettings()
	} else {
		d.runLog.diagnostic("settings.failed", "error", "Settings could not be saved. Your pending changes are retained in the app.")
	}
	return value, err
}

// RefreshSettings applies idle changes only. An in-flight session keeps its
// original client until completion, including after profile deletion.
func (d *engine) RefreshSettings() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state == stateIdle && !d.shuttingDown {
		if err := d.reloadConfig(); err != nil {
			d.configError = err.Error()
		} else {
			d.configError = ""
		}
	}
}

func (d *engine) Microphones() ([]AudioSource, error) {
	sources, err := ListAudioSources()
	if err != nil {
		return nil, err
	}
	settings, err := ReadSettings()
	if err != nil {
		return nil, err
	}
	if changed, err := reconcileMicrophone(settings.Microphone, sources); err != nil {
		return nil, err
	} else if changed {
		d.mu.Lock()
		d.showNotice(microphoneFallbackNotice)
		d.mu.Unlock()
		d.runLog.diagnostic("microphone.changed", "ok", microphoneFallbackNotice)
		d.RefreshSettings()
	}
	return sources, nil
}

func (d *engine) LocalModels() ([]LocalModel, error) {
	return d.localModels.List()
}

func (d *engine) InstallLocalModel(id string) error { return d.localModels.Install(id) }
func (d *engine) ResumeLocalModel(id string) error  { return d.localModels.Resume(id) }
func (d *engine) RetryLocalModel(id string) error   { return d.localModels.Retry(id) }
func (d *engine) PauseLocalModel(id string) error   { return d.localModels.Pause(id) }
func (d *engine) CancelLocalModel(id string) error  { return d.localModels.Cancel(id) }

func (d *engine) RemoveLocalModel(id string) error {
	if !d.toggleMu.TryLock() {
		return errors.New("finish the current dictation before removing a model")
	}
	defer d.toggleMu.Unlock()
	d.mu.Lock()
	busy := d.shuttingDown || d.state != stateIdle
	d.mu.Unlock()
	if busy {
		return errors.New("finish the current dictation before removing a model")
	}
	return d.localModels.Remove(id)
}

// Reserve work before spawning a goroutine. Additional commands are rejected,
// never saved up to start another recording after transcription finishes.
func (d *engine) prepareToggle() (func() error, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.shuttingDown {
		return nil, errors.New("shutting down")
	}
	if d.state == stateChecking {
		if d.sessionCancel != nil {
			d.sessionCancel()
		}
		return nil, nil
	}
	if d.state == stateTranscribing {
		return nil, errors.New("already transcribing")
	}
	if !d.toggleMu.TryLock() {
		return nil, errors.New("a recording command is already in progress")
	}
	state, sessionID := d.state, d.sessionID
	return func() error {
		defer d.toggleMu.Unlock()
		if state == stateRecording {
			return d.stopSessionMatching(sessionID, false)
		}
		return d.startSession()
	}, nil
}
func (d *engine) Toggle() error {
	operation, err := d.prepareToggle()
	if err != nil || operation == nil {
		return err
	}
	return operation()
}

// ToggleAsync reserves the command synchronously and completes off the caller's
// event loop. Its buffered result never blocks shutdown or the command receiver.
func (d *engine) ToggleAsync() <-chan error {
	result := make(chan error, 1)
	operation, err := d.prepareToggle()
	if err != nil || operation == nil {
		result <- err
	} else {
		go func() { result <- operation() }()
	}
	return result
}
func (d *engine) ClearLogs() error { return d.runLog.clear() }

func (d *engine) RecordDiagnostic(event, outcome, message string) {
	d.runLog.diagnostic(event, outcome, message)
}

// DiagnosticRecord is the redacted, bounded diagnostic format shown by the UI.
type DiagnosticRecord = runLogRecord

func (d *engine) Logs() ([]DiagnosticRecord, error) { return d.runLog.records() }

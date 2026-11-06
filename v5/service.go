package voicetype

import (
	"errors"
	"sync"
)

// Service owns dictation in its host process. It creates no control listener.
type Service struct {
	toggleMu sync.Mutex
	d        *daemon
	finished chan struct{}
}

type ServiceStatus struct {
	LastAttemptFailed bool    `json:"last_attempt_failed"`
	State             string  `json:"state"`
	Error             string  `json:"error"`
	Level             float64 `json:"level"`
	Notice            string  `json:"notice"`
	NoticeVersion     uint64  `json:"notice_version"`
}

// StartService acquires engine ownership before reading settings or opening logs.
// Only the standalone CLI serves HTTP; desktop control stays inside Wails.
func StartService(version string) (*Service, error) {
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
	settings, err := cfg.resolveSTT("", workspaceLoad())
	if err != nil {
		if configErr == nil {
			configErr = err
		}
		settings = sttSettings{Provider: cfg.Provider, Model: providerModel(cfg.Provider)}
	}
	d := newDaemon(cfg, &resources{Recorder: &Recorder{}}, settings, "")
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
	s := &Service{d: d, finished: make(chan struct{})}
	go func() {
		defer close(s.finished)
		<-d.done
		d.stopMicrophoneTest()
		d.res.Close()
		d.runLog.close("ok")
		lock.Close()
	}()
	return s, nil
}

func (s *Service) Done() <-chan struct{} { return s.finished }
func (s *Service) Close() {
	s.d.shutdown()
	<-s.finished
}
func (s *Service) Status() ServiceStatus {
	d := s.d
	d.mu.Lock()
	defer d.mu.Unlock()
	d.checkConfigRecovery()
	level := float64(0)
	if d.testRecorder != nil {
		level = d.testRecorder.Level()
	}
	return ServiceStatus{LastAttemptFailed: d.lastAttemptFailed, State: d.state.String(), Error: d.configError, Level: level, Notice: d.notice, NoticeVersion: d.noticeVersion}
}
func (s *Service) SaveSettings(update SettingsUpdate) (Settings, error) {
	value, err := SaveSettings(update)
	if err == nil {
		s.RefreshSettings()
	} else {
		s.d.runLog.diagnostic("settings.failed", "error", "Settings could not be saved. Your pending changes are retained in the app.")
	}
	return value, err
}

// RefreshSettings applies idle changes only. An in-flight session keeps its
// original client until completion, including after profile deletion.
func (s *Service) RefreshSettings() {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	if s.d.state == stateIdle && !s.d.shuttingDown {
		if err := s.d.reloadConfig(); err != nil {
			s.d.configError = err.Error()
		} else {
			s.d.configError = ""
		}
	}
}

func (s *Service) Microphones() ([]AudioSource, error) {
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
		s.d.mu.Lock()
		s.d.showNotice(microphoneFallbackNotice)
		s.d.mu.Unlock()
		s.d.runLog.diagnostic("microphone.changed", "ok", microphoneFallbackNotice)
		s.RefreshSettings()
	}
	return sources, nil
}

// Reserve work before spawning a goroutine. Additional commands are rejected,
// never saved up to start another recording after transcription finishes.
func (s *Service) prepareToggle() (func() error, error) {
	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	if s.d.shuttingDown {
		return nil, errors.New("shutting down")
	}
	if s.d.state == stateChecking {
		if s.d.sessionCancel != nil {
			s.d.sessionCancel()
		}
		return nil, nil
	}
	if s.d.state == stateTranscribing {
		return nil, errors.New("already transcribing")
	}
	if !s.toggleMu.TryLock() {
		return nil, errors.New("a recording command is already in progress")
	}
	return func() error {
		defer s.toggleMu.Unlock()
		var code int
		var message string
		if s.d.currentState() == stateRecording {
			code, message = s.d.stopSession()
		} else {
			code, message = s.d.startSession()
		}
		if code >= 400 {
			return errors.New(message)
		}
		return nil
	}, nil
}
func (s *Service) Toggle() error {
	operation, err := s.prepareToggle()
	if err != nil || operation == nil {
		return err
	}
	return operation()
}

// ToggleAsync reserves the command synchronously and completes off the caller's
// event loop. Its buffered result never blocks shutdown or the command receiver.
func (s *Service) ToggleAsync() <-chan error {
	result := make(chan error, 1)
	operation, err := s.prepareToggle()
	if err != nil || operation == nil {
		result <- err
	} else {
		go func() { result <- operation() }()
	}
	return result
}
func (s *Service) StartMicrophoneTest(id string) error { return s.d.startMicrophoneTest(id) }
func (s *Service) StopMicrophoneTest()                 { s.d.stopMicrophoneTest() }
func (s *Service) ClearLogs() error                    { return s.d.runLog.clear() }

func (s *Service) RecordDiagnostic(event, outcome, message string) {
	s.d.runLog.diagnostic(event, outcome, message)
}

// DiagnosticRecord is the redacted, bounded diagnostic format shown by the UI.
type DiagnosticRecord = runLogRecord

func (s *Service) Logs() ([]DiagnosticRecord, error) { return s.d.runLog.records() }

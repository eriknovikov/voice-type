package voicetype

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Service owns dictation and the loopback shortcut listener in its host process.
// Desktop settings stay in process; only the existing control routes use TCP.
type Service struct {
	d        *daemon
	server   *http.Server
	finished chan struct{}
}

type ServiceStatus struct {
	LastAttemptFailed bool    `json:"last_attempt_failed"`
	State             string  `json:"state"`
	Error             string  `json:"error"`
	Port              int     `json:"port"`
	Level             float64 `json:"level"`
	Notice            string  `json:"notice"`
	NoticeVersion     uint64  `json:"notice_version"`
}

// StartService reserves the port before allocating resources. A conflicting
// standalone instance is an error, never a service the desktop silently adopts.
func StartService(version string) (*Service, error) {
	cfg, configErr := configReadStrict()
	if configErr != nil {
		cfg = configDefaults()
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		RecordDiagnostic(version, "startup.failed", "error", "The local control port is unavailable. Close another Voice Type instance and reopen the app.")
		return nil, fmt.Errorf("cannot start Voice Type on port %d; close any other Voice Type instance: %w", cfg.Port, err)
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
	s.server = &http.Server{Handler: requireLoopbackHost(d.routes()), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		defer close(s.finished)
		serveDone := make(chan struct{})
		go func() {
			defer close(serveDone)
			if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logf("SERVICE", "listener failed: %v", err)
				d.shutdown()
			}
		}()
		<-d.done
		d.stopMicrophoneTest()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			_ = s.server.Close()
		}
		<-serveDone
		d.res.Close()
		d.runLog.close("ok")
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
	return ServiceStatus{LastAttemptFailed: d.lastAttemptFailed, State: d.state.String(), Error: d.configError, Port: d.cfg.Port, Level: level, Notice: d.notice, NoticeVersion: d.noticeVersion}
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
func (s *Service) Toggle() error {
	var code int
	var message string
	state := s.d.currentState()
	if state == stateChecking || state == stateRecording {
		code, message = s.d.stopSession()
	} else {
		code, message = s.d.startSession()
	}
	if code >= 400 {
		return errors.New(message)
	}
	return nil
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

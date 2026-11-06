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
		return nil, fmt.Errorf("cannot start Voice Type on port %d; close any other Voice Type instance: %w", cfg.Port, err)
	}
	settings, err := cfg.resolveSTT("", workspaceLoad())
	if err != nil {
		configErr = err
		settings = sttSettings{Provider: cfg.Provider, Model: providerModel(cfg.Provider)}
	}
	d := newDaemon(cfg, &resources{Recorder: &Recorder{}}, settings, "")
	d.runLog = openRunLog(version)
	if configErr != nil {
		d.configError = configErr.Error()
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
	level := float64(0)
	if d.testRecorder != nil {
		level = d.testRecorder.Level()
	}
	return ServiceStatus{LastAttemptFailed: d.lastAttemptFailed, State: d.state.String(), Error: d.configError, Port: d.cfg.Port, Level: level}
}
func (s *Service) SaveSettings(update SettingsUpdate) (Settings, error) {
	value, err := SaveSettings(update)
	if err == nil {
		s.d.mu.Lock()
		if s.d.state == stateIdle {
			if e := s.d.reloadConfig(); e != nil {
				s.d.configError = e.Error()
			} else {
				s.d.configError = ""
			}
		}
		s.d.mu.Unlock()
	}
	return value, err
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

// DiagnosticRecord is the redacted, bounded diagnostic format shown by the UI.
type DiagnosticRecord = runLogRecord

func (s *Service) Logs() ([]DiagnosticRecord, error) { return s.d.runLog.records() }

package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ManagementSocketPath is shared by the daemon and its desktop companion.
func ManagementSocketPath() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = filepath.Dir(workspaceFilePath())
	} else {
		base = filepath.Join(base, "voice-type")
	}
	return filepath.Join(base, "desktop.sock")
}
func (d *daemon) serveManagement() (*http.Server, error) {
	path := ManagementSocketPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("management socket directory must be private (0700)")
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return nil, errors.New("desktop management is already running")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace a non-socket management path")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	srv := &http.Server{Handler: d.managementRoutes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logf("DESKTOP", "management server: %v", err)
		}
	}()
	return srv, nil
}
func decodeManagement(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeJSON(w, 400, map[string]string{"error": "expected one JSON value"})
		return false
	}
	return true
}
func managementResult(w http.ResponseWriter, value any, err error) {
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, value)
}
func (d *daemon) managementRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /microphone-levels", d.streamMicrophoneLevels)
	mux.HandleFunc("GET /recovery", func(w http.ResponseWriter, r *http.Request) {
		value, err := RecoveryRevision()
		managementResult(w, value, err)
	})
	mux.HandleFunc("POST /recovery", func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			Revision string `json:"revision"`
		}
		if !decodeManagement(w, r, &value) {
			return
		}
		settings, err := ResetBrokenSettings(value.Revision)
		managementResult(w, settings, err)
	})
	mux.HandleFunc("GET /logs", func(w http.ResponseWriter, r *http.Request) {
		value, err := d.runLog.records()
		managementResult(w, value, err)
	})
	mux.HandleFunc("DELETE /logs", func(w http.ResponseWriter, r *http.Request) {
		managementResult(w, map[string]bool{"ok": true}, d.runLog.clear())
	})
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		value, err := ReadSettings()
		managementResult(w, value, err)
	})
	mux.HandleFunc("PUT /settings", func(w http.ResponseWriter, r *http.Request) {
		var update SettingsUpdate
		if !decodeManagement(w, r, &update) {
			return
		}
		value, err := SaveSettings(update)
		if err == nil {
			d.mu.Lock()
			if d.state == stateIdle {
				if e := d.reloadConfig(); e != nil {
					d.configError = e.Error()
				} else {
					d.configError = ""
				}
			}
			d.mu.Unlock()
		}
		managementResult(w, value, err)
	})
	mux.HandleFunc("PUT /workspace", func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			Name string `json:"name"`
		}
		if !decodeManagement(w, r, &value) {
			return
		}
		managementResult(w, map[string]bool{"ok": true}, SelectWorkspace(value.Name))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		level := float64(0)
		if d.testRecorder != nil {
			level = d.testRecorder.Level()
		}
		writeJSON(w, 200, map[string]any{"state": d.state.String(), "error": d.configError, "port": d.cfg.Port, "level": level})
	})
	mux.HandleFunc("GET /microphones", func(w http.ResponseWriter, r *http.Request) {
		value, err := ListAudioSources()
		managementResult(w, value, err)
	})
	mux.HandleFunc("POST /microphone-test", func(w http.ResponseWriter, r *http.Request) {
		var value struct {
			ID string `json:"id"`
		}
		if !decodeManagement(w, r, &value) {
			return
		}
		managementResult(w, map[string]bool{"ok": true}, d.startMicrophoneTest(value.ID))
	})
	mux.HandleFunc("DELETE /microphone-test", func(w http.ResponseWriter, r *http.Request) {
		d.stopMicrophoneTest()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /toggle", func(w http.ResponseWriter, r *http.Request) {
		state := d.currentState()
		var code int
		var message string
		if state == stateRecording || state == stateChecking {
			code, message = d.stopSession()
		} else {
			code, message = d.startSession()
		}
		if code >= 400 {
			writeJSON(w, code, map[string]string{"error": message})
			return
		}
		writeJSON(w, code, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"ok": true})
		go d.shutdown()
	})
	return mux
}
func (d *daemon) startMicrophoneTest(id string) error {
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
	d.testRecorder = rec
	d.state = stateTesting
	d.testTimer = time.AfterFunc(60*time.Second, d.stopMicrophoneTest)
	return nil
}
func (d *daemon) stopMicrophoneTest() {
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

// NewManagementClient only connects over the private Unix socket, never TCP.
func NewManagementClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", ManagementSocketPath())
	}}, Timeout: 12 * time.Minute}
}

// One bounded stream per microphone test; no audio data leaves the daemon.
func (d *daemon) streamMicrophoneLevels(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	d.mu.Lock()
	rec := d.testRecorder
	d.mu.Unlock()
	if rec == nil {
		http.Error(w, "no microphone test is active", 409)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	for {
		d.mu.Lock()
		active := d.testRecorder == rec && !d.shuttingDown
		d.mu.Unlock()
		if !active {
			_ = encoder.Encode(map[string]any{"level": 0, "testing": false})
			flusher.Flush()
			return
		}
		if err := encoder.Encode(map[string]any{"level": rec.Level(), "testing": true}); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-d.done:
			return
		case <-ticker.C:
		}
	}
}

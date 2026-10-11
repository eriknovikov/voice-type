package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const runLogMaxBytes = 5 << 20

type runLog struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	version string
}

type runLogAttempt struct {
	Model      string `json:"model"`
	StatusCode int    `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type runLogRecord struct {
	Event           string          `json:"event"`
	Version         string          `json:"version"`
	ConfiguredModel string          `json:"configured_model"`
	FinalModel      string          `json:"final_model,omitempty"`
	Outcome         string          `json:"outcome"`
	Error           string          `json:"error,omitempty"`
	StatusCode      int             `json:"status_code,omitempty"`
	TotalMS         int64           `json:"total_ms"`
	AudioMS         int64           `json:"audio_ms"`
	AttemptCount    int             `json:"attempt_count"`
	FallbackUsed    bool            `json:"fallback_used"`
	Attempts        []runLogAttempt `json:"attempts"`
}

func runLogFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".voice-type-logs.jsonl"
	}
	return filepath.Join(home, ".voice-type-logs.jsonl")
}

func stderrIsTerminal(stderr *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, stderr.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}

func openRunLog(version string) *runLog {
	return newRunLog(version, stderrIsTerminal(os.Stderr))
}

func newRunLog(version string, terminal bool) *runLog {
	l := &runLog{version: version}
	if terminal {
		return l
	}

	l.path = runLogFilePath()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		logf("LOG", "persistent logging unavailable: %v", err)
		return l
	}
	_ = os.Chmod(l.path, 0o600)
	if info, err := os.Stat(l.path); err == nil && info.Size() >= runLogMaxBytes {
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		logf("LOG", "persistent logging unavailable: %v", err)
		return l
	}
	l.file = file
	return l
}

func (l *runLog) complete(configuredModel, finalModel, outcome string, total time.Duration, audioSeconds float64, attempts []runLogAttempt, err error) {
	if l == nil {
		return
	}
	status := http.StatusOK
	if err != nil {
		status = 0
		var sttErr *sttError
		if errors.As(err, &sttErr) {
			status = sttErr.Status
		}
	}
	fallbackUsed := false
	for _, attempt := range attempts {
		if attempt.Model != configuredModel {
			fallbackUsed = true
			break
		}
	}
	l.write(runLogRecord{
		Event:           "dictation.completed",
		Version:         l.version,
		ConfiguredModel: configuredModel,
		FinalModel:      finalModel,
		Outcome:         outcome,
		Error:           safeRequestError(err),
		StatusCode:      status,
		TotalMS:         total.Milliseconds(),
		AudioMS:         int64(audioSeconds * 1000),
		AttemptCount:    len(attempts),
		FallbackUsed:    fallbackUsed,
		Attempts:        attempts,
	})
}

func (l *runLog) close(outcome string) {
	if l == nil || l.file == nil {
		return
	}
	l.mu.Lock()
	_ = l.file.Close()
	l.file = nil
	l.mu.Unlock()
}

func (l *runLog) write(record runLogRecord) {
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return
	}
	if info, err := l.file.Stat(); err == nil && info.Size()+int64(len(line)) > runLogMaxBytes {
		_ = l.file.Close()
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
		l.file, _ = os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	if l.file != nil {
		_, _ = l.file.Write(line)
	}
}

func safeRequestError(err error) string {
	if err == nil {
		return ""
	}
	var sttErr *sttError
	if errors.As(err, &sttErr) {
		return fmt.Sprintf("openrouter %d", sttErr.Status)
	}
	return requestOutcome(err)
}

func requestOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var sttErr *sttError
	if errors.As(err, &sttErr) {
		switch {
		case sttErr.Fatal():
			return "auth_error"
		case sttErr.Status == http.StatusRequestEntityTooLarge:
			return "too_large"
		case sttErr.Retryable():
			return "transient_error"
		}
	}
	return "error"
}

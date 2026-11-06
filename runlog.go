package voicetype

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const runLogMaxBytes = 5 << 20

type runLog struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	version string
}

type runLogAttempt struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	StatusCode int    `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type runLogRecord struct {
	Timestamp       string          `json:"timestamp,omitempty"`
	Event           string          `json:"event"`
	Version         string          `json:"version"`
	Provider        string          `json:"provider"`
	ConfiguredModel string          `json:"configured_model"`
	FinalModel      string          `json:"final_model,omitempty"`
	Outcome         string          `json:"outcome"`
	Error           string          `json:"error,omitempty"`
	Message         string          `json:"message,omitempty"`
	StatusCode      int             `json:"status_code,omitempty"`
	TotalMS         int64           `json:"total_ms"`
	AudioMS         int64           `json:"audio_ms"`
	AttemptCount    int             `json:"attempt_count"`
	FallbackUsed    bool            `json:"fallback_used"`
	Attempts        []runLogAttempt `json:"attempts"`
}

// RecordDiagnostic records application diagnostics, including failures before
// the engine exists. Callers must supply fixed, sanitized messages: never raw
// configuration, provider responses or credentials.
func RecordDiagnostic(version, event, outcome, message string) {
	l := openRunLog(version)
	defer l.close(outcome)
	l.diagnostic(event, outcome, message)
}

func (l *runLog) diagnostic(event, outcome, message string) {
	if l != nil {
		l.write(runLogRecord{Event: event, Version: l.version, Outcome: outcome, Message: message})
	}
}

func runLogFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".voice-type-logs.jsonl"
	}
	return filepath.Join(home, ".voice-type-logs.jsonl")
}

func openRunLog(version string) *runLog {
	return newRunLog(version)
}

func newRunLog(version string) *runLog {
	l := &runLog{version: version}

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

func (l *runLog) complete(provider, configuredModel, finalModel, outcome string, total time.Duration, audioSeconds float64, attempts []runLogAttempt, err error) {
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
	l.write(runLogRecord{
		Event:           "dictation.completed",
		Version:         l.version,
		Provider:        provider,
		ConfiguredModel: configuredModel,
		FinalModel:      finalModel,
		Outcome:         outcome,
		Error:           safeRequestError(err),
		StatusCode:      status,
		TotalMS:         total.Milliseconds(),
		AudioMS:         int64(audioSeconds * 1000),
		AttemptCount:    len(attempts),
		FallbackUsed:    false,
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
	if record.Timestamp == "" {
		record.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
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
		return fmt.Sprintf("%s %d", sttErr.Provider, sttErr.Status)
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
		case sttErr.Quota:
			return "quota_error"
		case sttErr.Status == http.StatusRequestEntityTooLarge:
			return "too_large"
		case sttErr.Retryable():
			return "transient_error"
		}
	}
	return "error"
}

// records returns at most two bounded log segments, newest entries first.
func (l *runLog) records() ([]runLogRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []runLogRecord{}
	for _, path := range []string{runLogFilePath() + ".1", runLogFilePath()} {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(io.LimitReader(f, runLogMaxBytes+1))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var record runLogRecord
			if json.Unmarshal(scanner.Bytes(), &record) == nil {
				out = append(out, record)
			}
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
func (l *runLog) clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		if err := l.file.Truncate(0); err != nil {
			return err
		}
	} else if err := os.Truncate(runLogFilePath(), 0); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(runLogFilePath() + ".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

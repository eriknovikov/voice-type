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
	"strconv"
	"sync"
	"time"
)

const runLogMaxBytes = 5 << 20

// One UI page. Filtering happens before pagination, so a page always contains
// at most this many matching records.
const runLogPageSize = 200

type runLog struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	version string
	// lastPageToken makes an unchanged poll skip parsing the history entirely.
	lastPageToken string
}

// LogPage is a bounded slice of the retained history plus a revision token.
// Unchanged is true when the same revision, filter and offset were requested
// again; Records is then empty and the caller keeps its current page.
type LogPage struct {
	Records   []runLogRecord `json:"records"`
	Revision  string         `json:"revision"`
	Outcome   string         `json:"outcome"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
	Total     int            `json:"total"`
	Retained  int            `json:"retained"`
	Unchanged bool           `json:"unchanged"`
}

type runLogAttempt struct {
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	StatusCode int    `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// Submission is an IBus handoff, not confirmation of visible target text.
type dictationTiming struct {
	streamTiming
	CaptureRequested time.Time
	CaptureStarted   time.Time
	StopRequested    time.Time
	CaptureStopped   time.Time
}
type runLogTiming struct {
	CaptureRequested          string `json:"capture_requested,omitempty"`
	CaptureStarted            string `json:"capture_started,omitempty"`
	CaptureStartupMS          *int64 `json:"capture_startup_ms,omitempty"`
	StartupBacklogDrained     string `json:"startup_backlog_drained,omitempty"`
	ConnectionStarted         string `json:"connection_started,omitempty"`
	ConnectionReady           string `json:"connection_ready,omitempty"`
	ConnectionMS              *int64 `json:"connection_ms,omitempty"`
	StopRequested             string `json:"stop_requested,omitempty"`
	CaptureStopped            string `json:"capture_stopped,omitempty"`
	FinalAudioSent            string `json:"final_audio_sent,omitempty"`
	CommitSent                string `json:"commit_sent,omitempty"`
	TranscriptReceived        string `json:"transcript_received,omitempty"`
	InsertionSubmitted        string `json:"insertion_submitted,omitempty"`
	InsertionCleanupCompleted string `json:"insertion_cleanup_completed,omitempty"`
	StopToTranscriptMS        *int64 `json:"stop_to_transcript_ms,omitempty"`
	StopToSubmissionMS        *int64 `json:"stop_to_submission_ms,omitempty"`
}

func (t *dictationTiming) logTiming() *runLogTiming {
	if t == nil {
		return nil
	}
	stamp := func(v time.Time) string {
		if v.IsZero() {
			return ""
		}
		return v.UTC().Format(time.RFC3339Nano)
	}
	elapsed := func(v time.Time) *int64 {
		if v.IsZero() || t.StopRequested.IsZero() || v.Before(t.StopRequested) {
			return nil
		}
		ms := v.Sub(t.StopRequested).Milliseconds()
		return &ms
	}
	var connectionMS *int64
	if !t.ConnectionStarted.IsZero() && !t.ConnectionReady.IsZero() {
		ms := t.ConnectionReady.Sub(t.ConnectionStarted).Milliseconds()
		connectionMS = &ms
	}
	var captureStartupMS *int64
	if !t.CaptureRequested.IsZero() && !t.CaptureStarted.IsZero() {
		ms := t.CaptureStarted.Sub(t.CaptureRequested).Milliseconds()
		captureStartupMS = &ms
	}
	return &runLogTiming{CaptureRequested: stamp(t.CaptureRequested), CaptureStarted: stamp(t.CaptureStarted), CaptureStartupMS: captureStartupMS, StartupBacklogDrained: stamp(t.StartupBacklogDrained), ConnectionStarted: stamp(t.ConnectionStarted), ConnectionReady: stamp(t.ConnectionReady), ConnectionMS: connectionMS, StopRequested: stamp(t.StopRequested), CaptureStopped: stamp(t.CaptureStopped), FinalAudioSent: stamp(t.FinalAudioSent), CommitSent: stamp(t.CommitSent), TranscriptReceived: stamp(t.TranscriptReceived), InsertionSubmitted: stamp(t.InsertionSubmitted), InsertionCleanupCompleted: stamp(t.InsertionCleanupCompleted), StopToTranscriptMS: elapsed(t.TranscriptReceived), StopToSubmissionMS: elapsed(t.InsertionSubmitted)}
}

type runLogRecord struct {
	Timing          *runLogTiming   `json:"timing,omitempty"`
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
	l.completeTimed(provider, configuredModel, finalModel, outcome, total, audioSeconds, attempts, err, nil)
}
func (l *runLog) completeTimed(provider, configuredModel, finalModel, outcome string, total time.Duration, audioSeconds float64, attempts []runLogAttempt, err error, timings *dictationTiming) {
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
		Timing:          timings.logTiming(),
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
	l.lastPageToken = ""
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
	var cloudErr *cloudStreamError
	if errors.As(err, &cloudErr) {
		return cloudErr.Error()
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
	var cloudErr *cloudStreamError
	if errors.As(err, &cloudErr) {
		return "cloud_stream_error"
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
// It is retained for diagnostics and tests that need the complete history;
// the UI uses page instead.
func (l *runLog) records() ([]runLogRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []runLogRecord{}
	if err := scanLogRecords(func(record runLogRecord) error {
		out = append(out, record)
		return nil
	}); err != nil {
		return nil, err
	}
	reverseRecords(out)
	return out, nil
}

func validateLogOutcome(outcome string) error {
	if outcome != "all" && outcome != "errors" && outcome != "ok" {
		return errors.New("choose a valid log outcome")
	}
	return nil
}

func logRecordMatches(record runLogRecord, outcome string) bool {
	switch outcome {
	case "errors":
		return record.Outcome != "ok"
	case "ok":
		return record.Outcome == "ok"
	default:
		return true
	}
}

// page returns one filtered page, newest first, without materializing the full
// history. A repeated request with the same file revision, filter and offset
// returns Unchanged instead of re-parsing or serializing records.
func (l *runLog) page(outcome string, offset int) (LogPage, error) {
	if outcome == "" {
		outcome = "all"
	}
	if err := validateLogOutcome(outcome); err != nil {
		return LogPage{}, err
	}
	if offset < 0 {
		offset = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	revision := l.pageRevisionLocked()
	token := revision + "|" + outcome + "|" + strconv.Itoa(offset)
	if token == l.lastPageToken {
		return LogPage{Records: []runLogRecord{}, Revision: token, Outcome: outcome, Offset: offset, Limit: runLogPageSize, Unchanged: true}, nil
	}

	// First pass counts retained and matching records. Files are read oldest
	// first, so the requested newest-first window maps to a range at the end.
	retained := 0
	total := 0
	if err := scanLogRecords(func(record runLogRecord) error {
		retained++
		if logRecordMatches(record, outcome) {
			total++
		}
		return nil
	}); err != nil {
		return LogPage{}, err
	}
	start := total - offset - runLogPageSize
	if start < 0 {
		start = 0
	}
	end := total - offset
	if end < start {
		end = start
	}
	page := make([]runLogRecord, 0, end-start)
	index := 0
	if err := scanLogRecords(func(record runLogRecord) error {
		if !logRecordMatches(record, outcome) {
			return nil
		}
		if index >= start && index < end {
			page = append(page, record)
		}
		index++
		return nil
	}); err != nil {
		return LogPage{}, err
	}
	reverseRecords(page)
	l.lastPageToken = token
	return LogPage{Records: page, Revision: token, Outcome: outcome, Offset: offset, Limit: runLogPageSize, Total: total, Retained: retained}, nil
}

// writeFiltered streams every retained record matching outcome to w. It never
// assembles a complete export buffer.
func (l *runLog) writeFiltered(outcome string, w io.Writer) error {
	if outcome == "" {
		outcome = "all"
	}
	if err := validateLogOutcome(outcome); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	buf := bufio.NewWriter(w)
	err := scanLogRecords(func(record runLogRecord) error {
		if !logRecordMatches(record, outcome) {
			return nil
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := buf.Write(raw); err != nil {
			return err
		}
		return buf.WriteByte('\n')
	})
	if err != nil {
		return err
	}
	return buf.Flush()
}

// pageRevisionLocked fingerprints both bounded log files. It must be called
// with l.mu held so a concurrent rotation cannot pair a new revision with old
// records.
func (l *runLog) pageRevisionLocked() string {
	revision := ""
	for _, path := range []string{runLogFilePath() + ".1", runLogFilePath()} {
		info, err := os.Stat(path)
		if err != nil {
			revision += "missing;"
			continue
		}
		revision += strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ";"
	}
	return revision
}

func reverseRecords(records []runLogRecord) {
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
}

// scanLogRecords reads both bounded segments oldest first and calls visit for
// every successfully decoded record.
func scanLogRecords(visit func(runLogRecord) error) error {
	for _, path := range []string{runLogFilePath() + ".1", runLogFilePath()} {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(io.LimitReader(f, runLogMaxBytes+1))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var record runLogRecord
			if json.Unmarshal(scanner.Bytes(), &record) == nil {
				if err := visit(record); err != nil {
					f.Close()
					return err
				}
			}
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
func (l *runLog) clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastPageToken = ""
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

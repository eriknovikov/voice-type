package voicetype

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunLogRecordsSafeRequestMetrics(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HOME", state)

	logs := newRunLog("v5.test", false)
	logs.complete("mai-transcribe-2", "gpt-transcribe", "ok", 4200*time.Millisecond, 3, []runLogAttempt{
		{Model: "mai-transcribe-2", StatusCode: 429, DurationMS: 1800, Error: "openrouter 429"},
		{Model: "mai-transcribe-2", StatusCode: 429, DurationMS: 1700, Error: "openrouter 429"},
		{Model: "gpt-transcribe", StatusCode: 200, DurationMS: 700},
	}, nil)
	logs.close("ok")

	raw, readErr := os.ReadFile(runLogFilePath())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "private dictated text") {
		t.Fatal("persistent log contains transcript text")
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d records, want one dictation summary:\n%s", len(lines), raw)
	}
	var request runLogRecord
	if err := json.Unmarshal([]byte(lines[0]), &request); err != nil {
		t.Fatal(err)
	}
	if request.Event != "dictation.completed" || request.Version != "v5.test" || request.ConfiguredModel != "mai-transcribe-2" || request.FinalModel != "gpt-transcribe" || request.Outcome != "ok" {
		t.Errorf("unexpected request identity: %+v", request)
	}
	if request.TotalMS != 4200 || request.AudioMS != 3000 || request.AttemptCount != 3 || !request.FallbackUsed || len(request.Attempts) != 3 {
		t.Errorf("unexpected request summary: %+v", request)
	}
	if request.Attempts[0].StatusCode != 429 || request.Attempts[0].DurationMS != 1800 || request.Attempts[2].StatusCode != 200 {
		t.Errorf("unexpected attempts: %+v", request.Attempts)
	}
	info, err := os.Stat(runLogFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("log permissions = %o, want 600", got)
	}
}

func TestRunLogFilePathUsesHomeDotFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "xdg-state"))

	if got, want := runLogFilePath(), filepath.Join(home, ".voice-type-logs.jsonl"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunLogPreservesHomePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}

	logs := newRunLog("v5.test", false)
	defer logs.close("ok")
	info, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("home permissions = %o, want 755", got)
	}
}

func TestRunLogDoesNotCreateFileForTerminal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logs := newRunLog("v5.test", true)
	logs.close("ok")
	if _, err := os.Stat(runLogFilePath()); !os.IsNotExist(err) {
		t.Fatalf("terminal run created persistent log: %v", err)
	}
}

func TestRunLogRotatesAtSizeLimit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(runLogFilePath()), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(runLogFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(runLogMaxBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	logs := newRunLog("v5.test", false)
	logs.close("ok")
	info, err := os.Stat(runLogFilePath() + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != runLogMaxBytes {
		t.Fatalf("rotated log size = %d, want %d", info.Size(), runLogMaxBytes)
	}
}

func TestRunLogStoresStatusWithoutErrorBody(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logs := newRunLog("v5.test", false)
	err := &sttError{Status: 401, Body: "secret provider response"}
	logs.complete("mai-transcribe-2", "mai-transcribe-2", "auth_error", 200*time.Millisecond, 1, []runLogAttempt{{
		Model: "mai-transcribe-2", StatusCode: 401, DurationMS: 200, Error: safeRequestError(err),
	}}, err)
	logs.close("error")

	raw, readErr := os.ReadFile(runLogFilePath())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "secret provider response") {
		t.Fatal("persistent log contains provider response")
	}
	if !strings.Contains(string(raw), `"status_code":401`) {
		t.Fatalf("persistent log lacks HTTP status:\n%s", raw)
	}
}

func TestDevNullIsNotTerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if stderrIsTerminal(f) {
		t.Fatal("/dev/null reported as a terminal")
	}
}

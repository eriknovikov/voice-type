package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testWAV() []byte { return WavEncode([]int16{1, 2, 3, 4}) }

func testSpec(id string) sttSpec { m, _ := sttLookup(id); return m }

func TestSTTJSONRequestShape(t *testing.T) {
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"text": "ok"})
	}))
	defer srv.Close()

	c := newSTTClient("k", testSpec("parakeet-v3"), nil)
	c.endpoint = srv.URL

	if _, err := c.Transcribe(testWAV()); err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	audio, ok := body["input_audio"].(map[string]any)
	if !ok {
		t.Fatalf("input_audio missing, body = %v", body)
	}
	if audio["format"] != "wav" {
		t.Errorf("format = %v", audio["format"])
	}
	data, _ := audio["data"].(string)
	if strings.HasPrefix(data, "data:") {
		t.Error("base64 payload must not carry a data: prefix")
	}
	if data == "" {
		t.Error("empty base64 payload")
	}
}

// The provider block is what makes a vocabulary work: OpenRouter keys the
// options by provider slug, and ignores a prompt on an unpinned request.
func TestSTTSendsPinnedProviderAndVocabulary(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"text":"ok"}`)
	}))
	defer srv.Close()

	send := func(id string) {
		c := newSTTClient("k", testSpec(id), []string{"Numbero", "Erik Novikov"})
		c.endpoint = srv.URL
		if _, err := c.Transcribe(testWAV()); err != nil {
			t.Fatalf("transcribe: %v", err)
		}
	}

	send(defaultModelID)
	for _, want := range []string{
		`"provider":{"only":["openai"],"options":{"openai":{"prompt":"Vocabulary: Numbero, Erik Novikov."}}}`,
		`"temperature":0`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("request body lacks %s\n%s", want, raw)
		}
	}

	// parakeet-v3 discards a vocabulary, so it must not be billed to carry one.
	send("parakeet-v3")
	if strings.Contains(string(raw), "prompt") {
		t.Errorf("vocabulary sent to a model that ignores it: %s", raw)
	}
}

func TestVocabularyEchoMatchingIsWholeTranscriptOnly(t *testing.T) {
	vocabulary := []string{"Numbero", "Erik Novikov"}
	prompt := sttVocabularyPrompt(testSpec(defaultModelID), vocabulary)
	for _, text := range []string{
		prompt,
		"  VOCABULARY:   Numbero, Erik Novikov.  ",
	} {
		if !vocabularyEcho(text, prompt) {
			t.Errorf("did not flag echo %q", text)
		}
	}
	for _, text := range []string{
		"Numbero is spelled correctly.",
		"Vocabulary: Numbero.",
		"Vocabulary: Numbero is the term I dictated.",
		"Erik Novikov",
	} {
		if vocabularyEcho(text, prompt) {
			t.Errorf("flagged legitimate transcript %q", text)
		}
	}
	if vocabularyEcho(prompt, "") {
		t.Error("flagged a prompt for a model that was not sent one")
	}
}

// 401 is fatal: retrying a bad key only wastes time.
func TestSTTUnauthorizedIsFatalAndNotRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "No auth credentials found", "code": 401},
		})
	}))
	defer srv.Close()

	c := newSTTClient("bad", testSpec(defaultModelID), nil)
	c.endpoint = srv.URL

	_, err := c.Transcribe(testWAV())
	if err == nil {
		t.Fatal("expected an error")
	}
	se, ok := err.(*sttError)
	if !ok {
		t.Fatalf("want *sttError, got %T", err)
	}
	if !se.Fatal() || se.Retryable() {
		t.Errorf("401 should be fatal and not retryable")
	}
	if se.Message != "No auth credentials found" {
		t.Errorf("message = %q", se.Message)
	}
	if calls != 1 {
		t.Errorf("401 was retried %d times, want 1 attempt", calls)
	}
}

func TestSTTPayloadTooLarge(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		io.WriteString(w, "too big")
	}))
	defer srv.Close()

	c := newSTTClient("k", testSpec(defaultModelID), nil)
	c.endpoint = srv.URL

	_, err := c.Transcribe(testWAV())
	se, ok := err.(*sttError)
	if !ok {
		t.Fatalf("want *sttError, got %T", err)
	}
	if se.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d", se.Status)
	}
	if se.Retryable() {
		t.Error("413 must not be retried")
	}
	if calls != 1 {
		t.Errorf("413 was retried %d times, want 1", calls)
	}
}

// The live provider returns intermittent 503s under bursty load; a dictation
// must survive them rather than being lost.
func TestSTTRetriesTransientFailures(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "Provider returned 503")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"text": "recovered"})
	}))
	defer srv.Close()

	c := newSTTClient("k", testSpec(defaultModelID), nil)
	c.endpoint = srv.URL

	res, err := c.Transcribe(testWAV())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if res.Text != "recovered" {
		t.Errorf("text = %q", res.Text)
	}
	if calls != 3 {
		t.Errorf("attempts = %d, want 3", calls)
	}
}

func TestSTTGivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newSTTClient("k", testSpec(defaultModelID), nil)
	c.endpoint = srv.URL

	if _, err := c.Transcribe(testWAV()); err == nil {
		t.Fatal("expected an error")
	}
	if calls != sttMaxAttempts {
		t.Errorf("attempts = %d, want %d", calls, sttMaxAttempts)
	}
}

func TestSTTCancellationInterruptsRetryBackoff(t *testing.T) {
	var calls atomic.Int32
	firstAttempt := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(firstAttempt)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newSTTClient("k", testSpec(defaultModelID), nil)
	c.endpoint = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.TranscribeContext(ctx, testWAV())
		done <- err
	}()
	<-firstAttempt
	time.Sleep(20 * time.Millisecond)

	started := time.Now()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Errorf("retry cancellation took %v", elapsed)
	}
	if calls.Load() != 1 {
		t.Errorf("requests after cancellation = %d, want 1", calls.Load())
	}
}

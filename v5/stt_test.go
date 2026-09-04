package voicetype

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func decodeSTTRequest(t *testing.T, model string, vocabulary []string) map[string]any {
	t.Helper()
	raw, err := sttBuildJSON(testSpec(model), vocabulary, testWAV())
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func providerOptions(t *testing.T, body map[string]any, provider string) map[string]any {
	t.Helper()
	p, ok := body["provider"].(map[string]any)
	if !ok {
		t.Fatalf("provider missing: %#v", body)
	}
	if _, exists := p["only"]; exists {
		t.Fatalf("provider.only must not be sent for STT: %#v", p)
	}
	all, ok := p["options"].(map[string]any)
	if !ok {
		t.Fatalf("provider.options missing: %#v", p)
	}
	opts, ok := all[provider].(map[string]any)
	if !ok {
		t.Fatalf("provider.options.%s missing: %#v", provider, all)
	}
	return opts
}

func TestSTTProviderOptions(t *testing.T) {
	t.Run("MAI vocabulary and clean style", func(t *testing.T) {
		body := decodeSTTRequest(t, "mai-transcribe-2", []string{" ProjectTerm ", "Maintainer Name", "projectterm", "C++/CLI"})
		opts := providerOptions(t, body, "azure")
		phraseList := opts["phraseList"].(map[string]any)
		if got := phraseList["phrases"]; !reflect.DeepEqual(got, []any{"ProjectTerm", "Maintainer Name", "C++/CLI"}) {
			t.Errorf("phrases = %#v", got)
		}
		enhanced := opts["enhancedMode"].(map[string]any)
		modelOptions := enhanced["modelOptions"].(map[string]any)
		if modelOptions["transcribeStyle"] != "clean" {
			t.Errorf("transcribeStyle = %#v", modelOptions["transcribeStyle"])
		}
	})

	t.Run("MAI clean style without vocabulary", func(t *testing.T) {
		opts := providerOptions(t, decodeSTTRequest(t, "mai-transcribe-2", nil), "azure")
		if _, exists := opts["phraseList"]; exists {
			t.Errorf("empty phraseList sent: %#v", opts)
		}
		if _, exists := opts["enhancedMode"]; !exists {
			t.Errorf("clean style missing: %#v", opts)
		}
	})

	t.Run("GPT prompt", func(t *testing.T) {
		opts := providerOptions(t, decodeSTTRequest(t, "gpt-transcribe", []string{"ProjectTerm", "Maintainer Name"}), "openai")
		if opts["prompt"] != "Vocabulary: ProjectTerm, Maintainer Name." {
			t.Errorf("prompt = %#v", opts["prompt"])
		}
		if _, exists := opts["phraseList"]; exists {
			t.Errorf("GPT received phraseList: %#v", opts)
		}
	})

	t.Run("unsupported model omits provider block", func(t *testing.T) {
		body := decodeSTTRequest(t, "parakeet-v3", []string{"ProjectTerm"})
		if _, exists := body["provider"]; exists {
			t.Errorf("provider options sent to unsupported model: %#v", body["provider"])
		}
	})
}

func TestVocabularyEchoMatchingIsWholeTranscriptOnly(t *testing.T) {
	vocabulary := []string{"Numbero", "Erik Novikov"}
	prompt := sttVocabularyPrompt(testSpec("gpt-4o-transcribe"), vocabulary)
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
	// A phrase-list model is conditioned on terms, not prose, so there is no
	// prompt for it to echo and nothing for the guard to match.
	if p := sttVocabularyPrompt(testSpec(defaultModelID), vocabulary); p != "" {
		t.Errorf("phrase-list model was handed a prompt %q", p)
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

func TestSTTMAIFallsBackToGPTTranscribe(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var requests []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				requests = append(requests, body)
				if len(requests) == 1 {
					w.WriteHeader(status)
					io.WriteString(w, `{"error":{"message":"try another provider"}}`)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"text": "recovered", "usage": map[string]any{"seconds": 2, "cost": 0.01}})
			}))
			defer srv.Close()

			wav := testWAV()
			c := newSTTClient("k", testSpec("mai-transcribe-2"), []string{"ProjectTerm", "Maintainer Name"})
			c.endpoint = srv.URL
			res, err := c.Transcribe(wav)
			if err != nil {
				t.Fatalf("transcribe: %v", err)
			}
			if res.Text != "recovered" || res.ModelID != "gpt-transcribe" {
				t.Errorf("result = %+v", res)
			}
			if len(requests) != 2 {
				t.Fatalf("requests = %d, want 2", len(requests))
			}
			if requests[0]["model"] != "microsoft/mai-transcribe-2" || requests[1]["model"] != "openai/gpt-transcribe" {
				t.Errorf("models = %q, %q", requests[0]["model"], requests[1]["model"])
			}
			firstAudio := requests[0]["input_audio"].(map[string]any)["data"]
			secondAudio := requests[1]["input_audio"].(map[string]any)["data"]
			if firstAudio != secondAudio {
				t.Error("fallback did not reuse the exact WAV")
			}
			providerOptions(t, requests[0], "azure")
			gpt := providerOptions(t, requests[1], "openai")
			if gpt["prompt"] != "Vocabulary: ProjectTerm, Maintainer Name." {
				t.Errorf("fallback prompt = %#v", gpt["prompt"])
			}
		})
	}
}

func TestSTTMAIDoesNotFallbackOnDeterministicOrSuccessfulResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		wantOK bool
	}{
		{"bad request", http.StatusBadRequest, `{"error":{"message":"bad request"}}`, false},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"message":"bad key"}}`, false},
		{"forbidden", http.StatusForbidden, `{"error":{"message":"forbidden"}}`, false},
		{"too large", http.StatusRequestEntityTooLarge, `{"error":{"message":"too large"}}`, false},
		{"malformed success", http.StatusOK, `not-json`, false},
		{"empty success", http.StatusOK, `{"text":""}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			c := newSTTClient("k", testSpec("mai-transcribe-2"), nil)
			c.endpoint = srv.URL
			res, err := c.Transcribe(testWAV())
			if tt.wantOK {
				if err != nil || res.Text != "" || res.ModelID != "mai-transcribe-2" {
					t.Fatalf("result/error = %+v/%v", res, err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if calls != 1 {
				t.Errorf("requests = %d, want 1", calls)
			}
		})
	}
}

func TestSTTCancelledMAIRequestDoesNotFallback(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	c := newSTTClient("k", testSpec("mai-transcribe-2"), nil)
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.TranscribeContext(ctx, testWAV())
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if calls.Load() != 1 {
		t.Errorf("requests = %d, want 1", calls.Load())
	}
}

func TestSTTFallbackStaysWithinTotalAttemptBudgetAndIsNotSticky(t *testing.T) {
	var models []string
	dictation := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		models = append(models, body["model"].(string))
		if dictation == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"text": "primary again"})
	}))
	defer srv.Close()

	c := newSTTClient("k", testSpec("mai-transcribe-2"), nil)
	c.endpoint = srv.URL
	if _, err := c.Transcribe(testWAV()); err == nil {
		t.Fatal("expected exhausted fallback error")
	}
	if len(models) != sttMaxAttempts {
		t.Fatalf("requests = %d, want %d", len(models), sttMaxAttempts)
	}
	want := []string{"microsoft/mai-transcribe-2", "openai/gpt-transcribe", "openai/gpt-transcribe", "openai/gpt-transcribe"}
	if !reflect.DeepEqual(models, want) {
		t.Errorf("models = %#v, want %#v", models, want)
	}

	dictation = 2
	res, err := c.Transcribe(testWAV())
	if err != nil {
		t.Fatalf("second dictation: %v", err)
	}
	if res.ModelID != "mai-transcribe-2" || models[len(models)-1] != "microsoft/mai-transcribe-2" {
		t.Errorf("second dictation did not restart on MAI: result=%+v models=%#v", res, models)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSTTMAIFallsBackOnTransportFailure(t *testing.T) {
	var models []string
	c := newSTTClient("k", testSpec("mai-transcribe-2"), nil)
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		models = append(models, body["model"].(string))
		if len(models) == 1 {
			return nil, errors.New("connection reset")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"text":"recovered"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	res, err := c.Transcribe(testWAV())
	if err != nil || res.ModelID != "gpt-transcribe" {
		t.Fatalf("result/error = %+v/%v", res, err)
	}
	if !reflect.DeepEqual(models, []string{"microsoft/mai-transcribe-2", "openai/gpt-transcribe"}) {
		t.Errorf("models = %#v", models)
	}
}

func TestSTTFallbackLogsDoNotExposeRequestData(t *testing.T) {
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	wav := []byte("private spoken audio sentinel")
	c := newSTTClient("test-api-key", testSpec("mai-transcribe-2"), []string{"PrivateVocabularySentinel"})
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(logs.String(), "switching") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"text":"done"}`)),
				Header:     make(http.Header),
			}, nil
		}
		return nil, errors.New("connection reset")
	})}
	if _, err := c.Transcribe(wav); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"test-api-key",
		"PrivateVocabularySentinel",
		base64.StdEncoding.EncodeToString(wav),
	} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("logs exposed request data %q: %s", secret, logs.String())
		}
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

	// Exercise cancellation during a same-model backoff. MAI would switch to
	// its fallback immediately after this first failure.
	c := newSTTClient("k", testSpec("gpt-transcribe"), nil)
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

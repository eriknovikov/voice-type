package voicetype

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const sttEndpoint = "https://openrouter.ai/api/v1/audio/transcriptions"

// sttResult is the useful part of OpenRouter's transcription response.
type sttResult struct {
	Text    string
	Seconds float64
	Cost    float64
}

type sttResponse struct {
	Text  string `json:"text"`
	Usage struct {
		Seconds float64 `json:"seconds"`
		Cost    float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// sttError carries the HTTP status so callers can distinguish a bad key from a
// transient failure, and holds onto whether the audio is worth retrying.
type sttError struct {
	Status  int
	Body    string
	Message string
}

func (e *sttError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("openrouter %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("openrouter %d: %s", e.Status, e.Body)
}

// Fatal reports whether retrying could ever help.
func (e *sttError) Fatal() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// Retryable reports whether the audio should be kept for a retry.
func (e *sttError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

type sttClient struct {
	apiKey     string
	model      sttSpec
	vocabulary []string
	endpoint   string
	http       *http.Client
}

func newSTTClient(apiKey string, model sttSpec, vocabulary []string) *sttClient {
	return &sttClient{
		apiKey:     apiKey,
		model:      model,
		vocabulary: vocabulary,
		endpoint:   sttEndpoint,
		http:       &http.Client{Timeout: 120 * time.Second},
	}
}

func sttVocabularyPrompt(model sttSpec, vocabulary []string) string {
	if !model.Vocabulary || len(vocabulary) == 0 {
		return ""
	}
	return "Vocabulary: " + strings.Join(vocabulary, ", ") + "."
}

func normalizeTranscript(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// vocabularyEcho reports whole responses that contain only the vocabulary
// conditioning text. A vocabulary word inside real prose is never enough.
func vocabularyEcho(text, prompt string) bool {
	return prompt != "" && normalizeTranscript(text) == normalizeTranscript(prompt)
}

// OpenRouter reads provider options only from its base64 JSON encoding.
func sttBuildJSON(model sttSpec, vocabulary []string, wav []byte) ([]byte, error) {
	provider := map[string]any{"only": []string{model.Provider}}
	if prompt := sttVocabularyPrompt(model, vocabulary); prompt != "" {
		provider["options"] = map[string]any{model.Provider: map[string]any{
			"prompt": prompt,
		}}
	}

	payload := map[string]any{
		"model": model.Slug,
		"input_audio": map[string]string{
			"data":   base64.StdEncoding.EncodeToString(wav),
			"format": "wav",
		},
		"language": "en",
		// LLM transcribers sample, so the same audio would otherwise come back
		// worded differently from one dictation to the next.
		"temperature": 0,
		"provider":    provider,
	}
	return json.Marshal(payload)
}

// Retry transient failures rather than dropping an already-spoken dictation.
const sttMaxAttempts = 4

// Transcribe uploads a WAV and returns the transcript, retrying transient
// failures with backoff.
func (c *sttClient) Transcribe(wav []byte) (sttResult, error) {
	return c.TranscribeContext(context.Background(), wav)
}

// TranscribeContext is Transcribe with cancellation for daemon shutdown.
func (c *sttClient) TranscribeContext(ctx context.Context, wav []byte) (sttResult, error) {
	var lastErr error

	for attempt := 1; attempt <= sttMaxAttempts; attempt++ {
		res, err := c.transcribeOnce(ctx, wav)
		if err == nil {
			if attempt > 1 {
				logf("STT", "succeeded on attempt %d", attempt)
			}
			return res, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return sttResult{}, ctx.Err()
		}

		if !sttShouldRetry(err) || attempt == sttMaxAttempts {
			break
		}

		// 400ms, 800ms, 1600ms
		backoff := time.Duration(400*(1<<(attempt-1))) * time.Millisecond
		logf("STT", "attempt %d/%d failed (%v) -- retrying in %v", attempt, sttMaxAttempts, err, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return sttResult{}, ctx.Err()
		}
	}

	return sttResult{}, lastErr
}

// sttShouldRetry covers transient HTTP statuses and bare network failures.
func sttShouldRetry(err error) bool {
	var se *sttError
	if errors.As(err, &se) {
		return se.Retryable()
	}
	// A transport-level failure (timeout, connection reset) is worth another go;
	// a request we could not even build is not.
	var be *sttBuildError
	return !errors.As(err, &be)
}

// sttBuildError marks failures that happen before anything is sent, which are
// never worth retrying.
type sttBuildError struct{ err error }

func (e *sttBuildError) Error() string { return e.err.Error() }
func (e *sttBuildError) Unwrap() error { return e.err }

// transcribeOnce performs a single upload attempt.
func (c *sttClient) transcribeOnce(ctx context.Context, wav []byte) (sttResult, error) {
	payload, err := sttBuildJSON(c.model, c.vocabulary, wav)
	if err != nil {
		return sttResult{}, &sttBuildError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return sttResult{}, &sttBuildError{err}
	}
	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return sttResult{}, fmt.Errorf("transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return sttResult{}, fmt.Errorf("read transcription response: %w", err)
	}

	var parsed sttResponse
	parseErr := json.Unmarshal(raw, &parsed)

	if resp.StatusCode != http.StatusOK {
		e := &sttError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
		if parseErr == nil && parsed.Error != nil {
			e.Message = parsed.Error.Message
		}
		return sttResult{}, e
	}

	if parseErr != nil {
		return sttResult{}, fmt.Errorf("decode transcription response: %w (body: %s)", parseErr, truncate(string(raw), 300))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return sttResult{}, &sttError{Status: resp.StatusCode, Message: parsed.Error.Message}
	}

	return sttResult{
		Text:    strings.TrimSpace(parsed.Text),
		Seconds: parsed.Usage.Seconds,
		Cost:    parsed.Usage.Cost,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

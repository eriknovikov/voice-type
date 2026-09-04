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
	ModelID string
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
	fallback   *sttSpec
	vocabulary []string
	endpoint   string
	http       *http.Client
}

func newSTTClient(apiKey string, model sttSpec, vocabulary []string) *sttClient {
	c := &sttClient{
		apiKey:     apiKey,
		vocabulary: vocabulary,
		endpoint:   sttEndpoint,
		http:       &http.Client{Timeout: 120 * time.Second},
	}
	c.setRoute(model)
	return c
}

func (c *sttClient) setRoute(model sttSpec) {
	route := sttRouteFor(model)
	c.model = route.Primary
	c.fallback = route.Fallback
}

func (c *sttClient) fallbackID() string {
	if c.fallback == nil {
		return ""
	}
	return c.fallback.ID
}

// sttVocabularyPrompt is the conditioning sentence for prompt-biased models,
// and is empty for every other model -- including phrase-list ones, which have
// no prompt to echo and so need no echo guard.
func sttVocabularyPrompt(model sttSpec, vocabulary []string) string {
	if model.Bias != biasPrompt || len(vocabulary) == 0 {
		return ""
	}
	return "Vocabulary: " + strings.Join(vocabulary, ", ") + "."
}

// sttProviderOptions is the model-specific option block in the provider's own
// spelling. The provider name is an options namespace, not a routing pin.
func sttProviderOptions(model sttSpec, vocabulary []string) map[string]any {
	switch model.Bias {
	case biasPhraseList:
		options := map[string]any{
			"enhancedMode": map[string]any{
				"modelOptions": map[string]any{"transcribeStyle": "clean"},
			},
		}
		if len(vocabulary) > 0 {
			options["phraseList"] = map[string]any{"phrases": vocabulary}
		}
		return options
	case biasPrompt:
		if len(vocabulary) == 0 {
			return nil
		}
		return map[string]any{"prompt": sttVocabularyPrompt(model, vocabulary)}
	default:
		return nil
	}
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
	vocabulary = cleanTerms(vocabulary)
	var provider map[string]any
	if options := sttProviderOptions(model, vocabulary); options != nil {
		provider = map[string]any{"options": map[string]any{model.Provider: options}}
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
	}
	if provider != nil {
		payload["provider"] = provider
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
	model := c.model
	retriesOnModel := 0

	for attempt := 1; attempt <= sttMaxAttempts; attempt++ {
		res, err := c.transcribeOnce(ctx, model, wav)
		if err == nil {
			if attempt > 1 {
				logf("STT", "%s succeeded on attempt %d/%d", model.ID, attempt, sttMaxAttempts)
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
		if model.ID == c.model.ID && c.fallback != nil {
			logf("STT", "%s failed (%v) -- switching this dictation to %s", model.ID, err, c.fallback.ID)
			model = *c.fallback
			retriesOnModel = 0
			continue
		}

		// 400ms, 800ms, 1600ms
		backoff := time.Duration(400*(1<<retriesOnModel)) * time.Millisecond
		retriesOnModel++
		logf("STT", "%s attempt %d/%d failed (%v) -- retrying in %v", model.ID, attempt, sttMaxAttempts, err, backoff)
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
	var te *sttTransportError
	return errors.As(err, &te)
}

// sttBuildError marks failures that happen before anything is sent, which are
// never worth retrying.
type sttBuildError struct{ err error }

func (e *sttBuildError) Error() string { return e.err.Error() }
func (e *sttBuildError) Unwrap() error { return e.err }

// sttTransportError is a failure while exchanging a request or response. It
// distinguishes retryable network failures from deterministic decode errors.
type sttTransportError struct{ err error }

func (e *sttTransportError) Error() string { return e.err.Error() }
func (e *sttTransportError) Unwrap() error { return e.err }

// transcribeOnce performs a single upload attempt.
func (c *sttClient) transcribeOnce(ctx context.Context, model sttSpec, wav []byte) (sttResult, error) {
	payload, err := sttBuildJSON(model, c.vocabulary, wav)
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
		return sttResult{}, &sttTransportError{fmt.Errorf("transcription request failed: %w", err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return sttResult{}, &sttTransportError{fmt.Errorf("read transcription response: %w", err)}
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
		ModelID: model.ID,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

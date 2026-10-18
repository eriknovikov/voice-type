package voicetype

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

const (
	openAIEndpoint     = "https://api.openai.com/v1/audio/transcriptions"
	openRouterEndpoint = "https://openrouter.ai/api/v1/audio/transcriptions"
)

// Missing usage data is unknown, not free transcription.
type sttResult struct {
	Text    string
	Seconds *float64
	Cost    *float64
	ModelID string
}

func (r sttResult) billingSummary() string {
	seconds, cost := "unavailable", "unavailable"
	if r.Seconds != nil {
		seconds = fmt.Sprintf("%.0fs", *r.Seconds)
	}
	if r.Cost != nil {
		cost = fmt.Sprintf("$%.6f", *r.Cost)
	}
	return "billed duration " + seconds + ", cost " + cost
}

type sttAttemptObserver func(model string, attempt int, duration time.Duration, result sttResult, err error)

type sttResponse struct {
	Text  *string `json:"text"`
	Usage struct {
		Seconds *float64 `json:"seconds"`
		Cost    *float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
}

// sttError carries the HTTP status so callers can distinguish a bad key from a
// transient failure, and holds onto whether the audio is worth retrying.
type sttError struct {
	Provider string
	Status   int
	Message  string
	Quota    bool
}

func (e *sttError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %d: %s", e.Provider, e.Status, e.Message)
	}
	return fmt.Sprintf("%s %d: %s", e.Provider, e.Status, http.StatusText(e.Status))
}

// Fatal reports whether retrying could ever help.
func (e *sttError) Fatal() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// Retryable reports whether the audio should be kept for a retry.
func (e *sttError) Retryable() bool {
	return !e.Quota && (e.Status == http.StatusTooManyRequests || (e.Status >= 500 && e.Status < 600))
}

type sttClient struct {
	settings   sttSettings
	endpoint   string
	http       *http.Client
	retryDelay time.Duration
}

func newSTTClient(settings sttSettings) *sttClient {
	endpoint := openRouterEndpoint
	if settings.Provider == providerOpenAI {
		endpoint = openAIEndpoint
	}
	return &sttClient{
		settings:   settings,
		endpoint:   endpoint,
		http:       &http.Client{Timeout: 120 * time.Second},
		retryDelay: 400 * time.Millisecond,
	}
}

// Only OpenRouter sends a prompt. OpenAI keywords need no prompt-echo guard.
func (s sttSettings) vocabularyPrompt() string {
	if s.Provider != providerOpenRouter || !s.supportedModel() || len(s.Vocabulary) == 0 {
		return ""
	}
	return "Vocabulary: " + strings.Join(s.Vocabulary, ", ") + "."
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
func sttBuildJSON(settings sttSettings, wav []byte) ([]byte, error) {
	payload := map[string]any{
		"model": settings.Model,
		"input_audio": map[string]string{
			"data":   base64.StdEncoding.EncodeToString(wav),
			"format": "wav",
		},
	}
	if settings.supportedModel() {
		payload["language"] = "en"
		payload["temperature"] = 0
	}
	if prompt := settings.vocabularyPrompt(); prompt != "" {
		payload["provider"] = map[string]any{
			"options": map[string]any{"openai": map[string]any{"prompt": prompt}},
		}
	}
	return json.Marshal(payload)
}

func sttBuildMultipart(settings sttSettings, wav []byte) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", settings.Model); err != nil {
		return nil, "", err
	}
	if settings.supportedModel() {
		if err := writer.WriteField("languages[]", "en"); err != nil {
			return nil, "", err
		}
		for _, term := range settings.Vocabulary {
			if err := writer.WriteField("keywords[]", term); err != nil {
				return nil, "", err
			}
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="dictation.wav"`)
	header.Set("Content-Type", "audio/wav")
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(wav); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func (c *sttClient) buildRequest(ctx context.Context, wav []byte) (*http.Request, error) {
	var payload []byte
	var err error
	contentType := "application/json"
	if c.settings.Provider == providerOpenAI {
		payload, contentType, err = sttBuildMultipart(c.settings, wav)
	} else {
		payload, err = sttBuildJSON(c.settings, wav)
	}
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+c.settings.APIKey)
	return req, nil
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
	return c.transcribeContext(ctx, wav, nil)
}

func (c *sttClient) transcribeContext(ctx context.Context, wav []byte, observe sttAttemptObserver) (sttResult, error) {
	var lastErr error
	for attempt := 1; attempt <= sttMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return sttResult{}, err
		}
		started := time.Now()
		res, err := c.transcribeOnce(ctx, wav)
		if observe != nil {
			observe(c.settings.Model, attempt, time.Since(started), res, err)
		}
		if err == nil {
			if attempt > 1 {
				logf("STT", "%s/%s succeeded on attempt %d/%d", c.settings.Provider, c.settings.Model, attempt, sttMaxAttempts)
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
		backoff := c.retryDelay * time.Duration(1<<(attempt-1))
		logf("STT", "%s/%s attempt %d/%d failed (%v) -- retrying in %v", c.settings.Provider, c.settings.Model, attempt, sttMaxAttempts, err, backoff)
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
func (c *sttClient) transcribeOnce(ctx context.Context, wav []byte) (sttResult, error) {
	req, err := c.buildRequest(ctx, wav)
	if err != nil {
		return sttResult{}, &sttBuildError{fmt.Errorf("%s: build transcription request: %w", c.settings.Provider, err)}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return sttResult{}, &sttTransportError{fmt.Errorf("%s: transcription request failed: %w", c.settings.Provider, err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return sttResult{}, &sttTransportError{fmt.Errorf("%s: read transcription response: %w", c.settings.Provider, err)}
	}

	var parsed sttResponse
	parseErr := json.Unmarshal(raw, &parsed)

	if resp.StatusCode != http.StatusOK || (parseErr == nil && parsed.Error != nil) {
		return sttResult{}, c.responseError(resp.StatusCode, parsed)
	}

	if parseErr != nil {
		return sttResult{}, fmt.Errorf("%s: decode transcription response: %w", c.settings.Provider, parseErr)
	}
	if parsed.Text == nil {
		return sttResult{}, fmt.Errorf("%s: transcription response is missing text", c.settings.Provider)
	}

	return sttResult{
		Text:    strings.TrimSpace(*parsed.Text),
		Seconds: parsed.Usage.Seconds,
		Cost:    parsed.Usage.Cost,
		ModelID: c.settings.Model,
	}, nil
}

func (c *sttClient) responseError(status int, parsed sttResponse) *sttError {
	e := &sttError{Provider: c.settings.Provider, Status: status, Quota: status == http.StatusPaymentRequired}
	if parsed.Error != nil {
		e.Message = parsed.Error.Message
		if c.settings.APIKey != "" {
			e.Message = strings.ReplaceAll(e.Message, c.settings.APIKey, "[redacted]")
		}
		classification := strings.ToLower(fmt.Sprint(parsed.Error.Code) + " " + parsed.Error.Type + " " + parsed.Error.Message)
		for _, marker := range []string{"insufficient_quota", "quota_exceeded", "billing_hard_limit", "billing_not_active", "credit_balance_too_low", "exceeded your current quota", "insufficient credits"} {
			if strings.Contains(classification, marker) {
				e.Quota = true
			}
		}
	}
	return e
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

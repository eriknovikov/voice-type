package voicetype

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	// useJSON selects the base64 JSON encoding instead of multipart. It costs
	// 33% payload, but OpenRouter reads the provider block only from JSON.
	useJSON bool
}

func newSTTClient(apiKey string, model sttSpec, vocabulary []string) *sttClient {
	return &sttClient{
		apiKey:     apiKey,
		model:      model,
		vocabulary: vocabulary,
		endpoint:   sttEndpoint,
		http:       &http.Client{Timeout: 120 * time.Second},
		useJSON:    true,
	}
}

// sttBuildMultipart produces an OpenAI-style file upload. It cannot carry the
// provider block, so it is a diagnostic path, not the one dictation takes.
func sttBuildMultipart(model sttSpec, wav []byte) (body *bytes.Buffer, contentType string, err error) {
	body = &bytes.Buffer{}
	w := multipart.NewWriter(body)

	fw, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, "", err
	}
	if _, err := fw.Write(wav); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("model", model.Slug); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("language", "en"); err != nil {
		return nil, "", err
	}
	if err := w.WriteField("response_format", "json"); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return body, w.FormDataContentType(), nil
}

// sttBuildJSON produces the input_audio encoding: raw base64, no data: prefix.
// The vocabulary goes in the provider's own prompt option, framed as a labelled
// list because a bare one measurably regressed accuracy.
func sttBuildJSON(model sttSpec, vocabulary []string, wav []byte) ([]byte, error) {
	provider := map[string]any{"only": []string{model.Provider}}
	if model.Vocabulary && len(vocabulary) > 0 {
		provider["options"] = map[string]any{model.Provider: map[string]any{
			"prompt": "Vocabulary: " + strings.Join(vocabulary, ", ") + ".",
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

// sttMaxAttempts bounds retries. The upstream provider returns intermittent
// 503s -- measured roughly 1 in 3 requests during Phase 0 -- so a single
// attempt would drop dictations the user already spoke.
const sttMaxAttempts = 4

// Transcribe uploads a WAV and returns the transcript, retrying transient
// failures with backoff.
func (c *sttClient) Transcribe(wav []byte) (sttResult, error) {
	var lastErr error

	for attempt := 1; attempt <= sttMaxAttempts; attempt++ {
		res, err := c.transcribeOnce(wav)
		if err == nil {
			if attempt > 1 {
				logf("STT", "succeeded on attempt %d", attempt)
			}
			return res, nil
		}
		lastErr = err

		if !sttShouldRetry(err) || attempt == sttMaxAttempts {
			break
		}

		// 400ms, 800ms, 1600ms
		backoff := time.Duration(400*(1<<(attempt-1))) * time.Millisecond
		logf("STT", "attempt %d/%d failed (%v) -- retrying in %v", attempt, sttMaxAttempts, err, backoff)
		time.Sleep(backoff)
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
func (c *sttClient) transcribeOnce(wav []byte) (sttResult, error) {
	var req *http.Request
	var err error

	if c.useJSON {
		var payload []byte
		payload, err = sttBuildJSON(c.model, c.vocabulary, wav)
		if err != nil {
			return sttResult{}, &sttBuildError{err}
		}
		req, err = http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			return sttResult{}, &sttBuildError{err}
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		var body *bytes.Buffer
		var ct string
		body, ct, err = sttBuildMultipart(c.model, wav)
		if err != nil {
			return sttResult{}, &sttBuildError{err}
		}
		req, err = http.NewRequest(http.MethodPost, c.endpoint, body)
		if err != nil {
			return sttResult{}, &sttBuildError{err}
		}
		req.Header.Set("Content-Type", ct)
	}

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

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
	Offline bool
}

func (r sttResult) billingSummary() string {
	if r.Offline {
		return "local inference"
	}
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
	local      *localTranscriber
}

func newSTTClient(settings sttSettings) *sttClient {
	return newSTTClientWithModels(settings, nil)
}

func newSTTClientWithModels(settings sttSettings, models *LocalModelManager) *sttClient {
	if settings.Provider == providerLocal {
		if models == nil {
			models = newLocalModelManager()
		}
		return &sttClient{
			settings: settings,
			local:    &localTranscriber{settings: settings, models: models},
		}
	}
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
	if s.Provider != providerOpenRouter || len(s.Vocabulary) == 0 {
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

// appendBuffer is the destination for the streaming base64 encoder.
type appendBuffer struct{ data []byte }

func (b *appendBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// sttBuildJSONParts builds the OpenRouter request from one or more adjacent
// byte slices. OpenRouter reads provider options only from its base64 JSON
// encoding, so the payload is assembled directly into one pre-sized buffer:
// small metadata is marshalled normally, then the audio is base64-encoded into
// the final payload. Streaming the encoder lets a WAV header and the raw PCM
// backing a sample slice be encoded without first joining or copying them.
func sttBuildJSONParts(settings sttSettings, audio ...[]byte) ([]byte, error) {
	metadata := map[string]any{
		"model":       settings.Model,
		"language":    "en",
		"temperature": 0,
	}
	if prompt := settings.vocabularyPrompt(); prompt != "" {
		metadata["provider"] = map[string]any{
			"options": map[string]any{"openai": map[string]any{"prompt": prompt}},
		}
	}
	head, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	const audioKey = `,"input_audio":{"data":"`
	const audioTail = `","format":"wav"}`
	encodedLen := 0
	for _, part := range audio {
		encodedLen += base64.StdEncoding.EncodedLen(len(part))
	}
	out := &appendBuffer{data: make([]byte, 0, len(head)+len(audioKey)+encodedLen+len(audioTail)+1)}
	out.data = append(out.data, head[:len(head)-1]...) // drop the metadata object's closing brace
	out.data = append(out.data, audioKey...)
	encoder := base64.NewEncoder(base64.StdEncoding, out)
	for _, part := range audio {
		if _, err := encoder.Write(part); err != nil {
			return nil, err
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	out.data = append(out.data, audioTail...)
	out.data = append(out.data, '}')
	return out.data, nil
}

// sttBuildMultipartParts builds the small multipart prefix and suffix around
// the WAV so the upload can stream prefix -> WAV -> suffix without copying the
// recording into another complete buffer.
func sttBuildMultipartParts(settings sttSettings) (prefix, suffix []byte, contentType string, err error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", settings.Model); err != nil {
		return nil, nil, "", err
	}
	for _, code := range settings.Languages {
		if err := writer.WriteField("languages[]", code); err != nil {
			return nil, nil, "", err
		}
	}
	for _, term := range settings.Vocabulary {
		if err := writer.WriteField("keywords[]", term); err != nil {
			return nil, nil, "", err
		}
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="dictation.wav"`)
	header.Set("Content-Type", "audio/wav")
	if _, err := writer.CreatePart(header); err != nil {
		return nil, nil, "", err
	}
	// Close writes the final boundary and flushes buffered part headers. With
	// no file bytes written, the result is exactly prefix+suffix.
	if err := writer.Close(); err != nil {
		return nil, nil, "", err
	}
	suffix = []byte("\r\n--" + writer.Boundary() + "--\r\n")
	full := body.Bytes()
	if len(full) < len(suffix) || !bytes.HasSuffix(full, suffix) {
		return nil, nil, "", errors.New("multipart prefix did not end with the generated boundary")
	}
	prefix = full[:len(full)-len(suffix)]
	return prefix, suffix, writer.FormDataContentType(), nil
}

// sttPreparedRequest holds immutable payload data for one retry sequence.
// Every attempt gets a fresh reader over the same bytes.
type sttPreparedRequest struct {
	contentType   string
	contentLength int64
	prefix        []byte
	// lead carries a small fixed prefix before the audio, such as a WAV header.
	lead      []byte
	suffix    []byte
	wav       []byte
	payload   []byte
	multipart bool
}

func (p *sttPreparedRequest) reader() io.Reader {
	if p.multipart {
		parts := []io.Reader{bytes.NewReader(p.prefix)}
		if len(p.lead) > 0 {
			parts = append(parts, bytes.NewReader(p.lead))
		}
		parts = append(parts, bytes.NewReader(p.wav), bytes.NewReader(p.suffix))
		return io.MultiReader(parts...)
	}
	return bytes.NewReader(p.payload)
}

func (c *sttClient) prepareRequest(wav []byte) (*sttPreparedRequest, error) {
	if c.settings.Provider == providerLocal {
		return nil, errors.New("local transcription never creates cloud requests")
	}
	if c.settings.Provider == providerOpenAI {
		prefix, suffix, contentType, err := sttBuildMultipartParts(c.settings)
		if err != nil {
			return nil, err
		}
		return &sttPreparedRequest{
			contentType:   contentType,
			contentLength: int64(len(prefix) + len(wav) + len(suffix)),
			prefix:        prefix,
			suffix:        suffix,
			wav:           wav,
			multipart:     true,
		}, nil
	}
	payload, err := sttBuildJSONParts(c.settings, wav)
	if err != nil {
		return nil, err
	}
	return &sttPreparedRequest{contentType: "application/json", contentLength: int64(len(payload)), payload: payload}, nil
}

// prepareSamplesRequest builds the upload around captured PCM instead of a
// materialized WAV. OpenAI receives the header and the little-endian sample
// bytes as adjacent multipart parts; OpenRouter encodes both directly into its
// JSON payload. No full-recording copy is made.
func (c *sttClient) prepareSamplesRequest(samples []int16, pcm []byte) (*sttPreparedRequest, error) {
	if c.settings.Provider == providerLocal {
		return nil, errors.New("local transcription never creates cloud requests")
	}
	header := wavHeader(len(samples))
	if c.settings.Provider == providerOpenAI {
		prefix, suffix, contentType, err := sttBuildMultipartParts(c.settings)
		if err != nil {
			return nil, err
		}
		return &sttPreparedRequest{
			contentType:   contentType,
			contentLength: int64(len(prefix) + len(header) + len(pcm) + len(suffix)),
			prefix:        prefix,
			lead:          header[:],
			suffix:        suffix,
			wav:           pcm,
			multipart:     true,
		}, nil
	}
	payload, err := sttBuildJSONParts(c.settings, header[:], pcm)
	if err != nil {
		return nil, err
	}
	return &sttPreparedRequest{contentType: "application/json", contentLength: int64(len(payload)), payload: payload}, nil
}

func (c *sttClient) requestFrom(ctx context.Context, prepared *sttPreparedRequest) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, prepared.reader())
	if err != nil {
		return nil, err
	}
	req.ContentLength = prepared.contentLength
	req.Header.Set("Content-Type", prepared.contentType)
	req.Header.Set("Authorization", "Bearer "+c.settings.APIKey)
	return req, nil
}

func (c *sttClient) buildRequest(ctx context.Context, wav []byte) (*http.Request, error) {
	prepared, err := c.prepareRequest(wav)
	if err != nil {
		return nil, err
	}
	return c.requestFrom(ctx, prepared)
}

// Retry transient failures rather than dropping an already-spoken dictation.
const sttMaxAttempts = 4

// Transcribe uploads a WAV and returns the transcript, retrying transient
// failures with backoff.
func (c *sttClient) Transcribe(wav []byte) (sttResult, error) {
	return c.TranscribeContext(context.Background(), wav)
}

// TranscribeContext is Transcribe with cancellation for application shutdown.
func (c *sttClient) TranscribeContext(ctx context.Context, wav []byte) (sttResult, error) {
	return c.transcribeContext(ctx, wav, nil)
}

func (c *sttClient) transcribeContext(ctx context.Context, wav []byte, observe sttAttemptObserver) (sttResult, error) {
	if c.local != nil {
		return c.local.transcribeWAV(ctx, wav, observe)
	}
	prepared, err := c.prepareRequest(wav)
	if err != nil {
		return sttResult{}, &sttBuildError{fmt.Errorf("%s: build transcription request: %w", c.settings.Provider, err)}
	}
	return c.transcribePrepared(ctx, prepared, observe)
}

// transcribeSamplesContext uploads captured samples directly. On little-endian
// hosts the WAV copy is avoided entirely; the header and PCM view are read
// straight into the request body for every retry attempt.
func (c *sttClient) transcribeSamplesContext(ctx context.Context, samples []int16, observe sttAttemptObserver) (sttResult, error) {
	if c.local != nil {
		return c.local.transcribeSamples(ctx, samples, observe, nil)
	}
	pcm, ok := pcm16Bytes(samples)
	if !ok {
		return c.transcribeContext(ctx, WavEncode(samples), observe)
	}
	prepared, err := c.prepareSamplesRequest(samples, pcm)
	if err != nil {
		return sttResult{}, &sttBuildError{fmt.Errorf("%s: build transcription request: %w", c.settings.Provider, err)}
	}
	return c.transcribePrepared(ctx, prepared, observe)
}

func (c *sttClient) transcribePrepared(ctx context.Context, prepared *sttPreparedRequest, observe sttAttemptObserver) (sttResult, error) {
	var lastErr error
	for attempt := 1; attempt <= sttMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return sttResult{}, err
		}
		started := time.Now()
		res, err := c.transcribePreparedOnce(ctx, prepared)
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

// Bound batch responses so a hostile or broken provider cannot exhaust memory.
const sttResponseLimit = 1 << 20

// transcribePreparedOnce performs a single upload attempt with a fresh reader
// over the immutable payload.
func (c *sttClient) transcribePreparedOnce(ctx context.Context, prepared *sttPreparedRequest) (sttResult, error) {
	req, err := c.requestFrom(ctx, prepared)
	if err != nil {
		return sttResult{}, &sttBuildError{fmt.Errorf("%s: build transcription request: %w", c.settings.Provider, err)}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return sttResult{}, &sttTransportError{fmt.Errorf("%s: transcription request failed: %w", c.settings.Provider, err)}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, sttResponseLimit+1))
	if err != nil {
		return sttResult{}, &sttTransportError{fmt.Errorf("%s: read transcription response: %w", c.settings.Provider, err)}
	}
	if len(raw) > sttResponseLimit {
		// Preserve status-based classification for an oversized error body.
		if resp.StatusCode != http.StatusOK {
			return sttResult{}, c.responseError(resp.StatusCode, sttResponse{})
		}
		return sttResult{}, fmt.Errorf("%s: transcription response exceeded %d bytes", c.settings.Provider, sttResponseLimit)
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

package voicetype

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testWAV() []byte { return WavEncode([]int16{1, 2, 3, 4}) }

// The multipart field names are the ones Phase 0 validated against the live
// endpoint: an OpenAI-style "file" part plus a "model" field.
func TestSTTMultipartRequestShape(t *testing.T) {
	var gotModel, gotFilename, gotAuth string
	var gotFileBytes []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Errorf("parse content type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "model":
				gotModel = string(data)
			case "file":
				gotFilename = part.FileName()
				gotFileBytes = data
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"text":  " hello world ",
			"usage": map[string]any{"seconds": 7, "cost": 0.000176},
		})
	}))
	defer srv.Close()

	c := newSTTClient("test-key", "nvidia/parakeet-tdt-0.6b-v3")
	c.endpoint = srv.URL

	res, err := c.Transcribe(testWAV())
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotModel != "nvidia/parakeet-tdt-0.6b-v3" {
		t.Errorf("model = %q", gotModel)
	}
	if !strings.HasSuffix(gotFilename, ".wav") {
		t.Errorf("filename = %q, want a .wav", gotFilename)
	}
	if !strings.HasPrefix(string(gotFileBytes), "RIFF") {
		t.Error("uploaded file is not a RIFF payload")
	}
	if res.Text != "hello world" {
		t.Errorf("text = %q, want trimmed %q", res.Text, "hello world")
	}
	if res.Seconds != 7 || res.Cost != 0.000176 {
		t.Errorf("usage = %v/%v", res.Seconds, res.Cost)
	}
}

func TestSTTJSONRequestShape(t *testing.T) {
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"text": "ok"})
	}))
	defer srv.Close()

	c := newSTTClient("k", "m")
	c.endpoint = srv.URL
	c.useJSON = true

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

	c := newSTTClient("bad", "m")
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

	c := newSTTClient("k", "m")
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

	c := newSTTClient("k", "m")
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

	c := newSTTClient("k", "m")
	c.endpoint = srv.URL

	if _, err := c.Transcribe(testWAV()); err == nil {
		t.Fatal("expected an error")
	}
	if calls != sttMaxAttempts {
		t.Errorf("attempts = %d, want %d", calls, sttMaxAttempts)
	}
}

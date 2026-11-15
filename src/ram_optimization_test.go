package voicetype

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWavEncodeIsByteIdenticalAndSingleAllocation(t *testing.T) {
	samples := []int16{1, -2, 0x7fff, -0x8000}
	want, err := hex.DecodeString(
		"524946462c00000057415645666d7420100000000100" +
			"0100803e0000007d0000020010006461746108000000" +
			"0100feffff7f0080",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := WavEncode(samples); !bytes.Equal(got, want) {
		t.Fatalf("WAV bytes changed:\n got %x\nwant %x", got, want)
	}
	larger := make([]int16, 16000)
	if allocs := testing.AllocsPerRun(10, func() { _ = WavEncode(larger) }); allocs > 1 {
		t.Fatalf("WavEncode allocated %v times per call; expected one", allocs)
	}
}

func TestRecorderStopTransfersAndReleasesSamples(t *testing.T) {
	recorder := &Recorder{}
	if _, err := recorder.acceptSamples([]int16{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	first, err := recorder.Stop()
	if err != nil {
		t.Fatal(err)
	}
	recorder.bufMu.Lock()
	retained := recorder.samples
	recorder.bufMu.Unlock()
	if retained != nil {
		t.Fatal("Stop retained the transferred recording")
	}
	// A later capture must not reuse the returned backing array.
	if _, err := recorder.acceptSamples([]int16{9, 8, 7}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, []int16{1, 2, 3}) {
		t.Fatalf("next recording altered the previous result: %v", first)
	}
}

func TestRecorderCloseReleasesSamples(t *testing.T) {
	recorder := &Recorder{}
	if _, err := recorder.acceptSamples([]int16{4, 5, 6}); err != nil {
		t.Fatal(err)
	}
	recorder.Close()
	recorder.bufMu.Lock()
	defer recorder.bufMu.Unlock()
	if recorder.samples != nil || recorder.chunkSink != nil || recorder.sessionBuffer != nil {
		t.Fatal("Close retained recorder-owned audio references")
	}
}

func TestRecorderChunkSinkOwnsACopy(t *testing.T) {
	recorder := &Recorder{}
	var received []int16
	recorder.SetChunkSink(func(samples []int16) { received = samples })
	shared := []int16{11, 12, 13}
	if _, err := recorder.acceptSamples(shared); err != nil {
		t.Fatal(err)
	}
	shared[0] = 99
	if !reflect.DeepEqual(received, []int16{11, 12, 13}) {
		t.Fatalf("sink observed caller mutation: %v", received)
	}
	if _, err := recorder.Stop(); err != nil {
		t.Fatal(err)
	}
}

type recordingStreamEngine struct {
	received chan []int16
}

func (e *recordingStreamEngine) feed(_ context.Context, samples []int16) (string, string, error) {
	e.received <- samples
	return "", "", nil
}
func (e *recordingStreamEngine) finalize(context.Context) (string, string, error) {
	return "", "", nil
}
func (e *recordingStreamEngine) close() {}

func TestLocalStreamTakesOwnershipOfRecorderChunk(t *testing.T) {
	engine := &recordingStreamEngine{received: make(chan []int16, 1)}
	stream := newLocalStreamSession(context.Background(), streamingModeProcess, engine, nil)
	defer stream.Abort()
	owned := []int16{21, 22, 23}
	stream.OfferSamples(owned)
	select {
	case got := <-engine.received:
		if &got[0] != &owned[0] {
			t.Fatal("local queue copied an already-owned recorder chunk")
		}
	case <-time.After(time.Second):
		t.Fatal("local stream did not receive the chunk")
	}
}

func TestNativeStreamReusesUnchangedSnapshot(t *testing.T) {
	stream := &nativeStream{full: "stable text", committed: "stable", haveText: true}
	full, committed, err := stream.snapshot(false)
	if err != nil {
		t.Fatal(err)
	}
	if full != "stable text" || committed != "stable" {
		t.Fatalf("unchanged snapshot was rebuilt: %q %q", full, committed)
	}
}

func TestCloudAppendMessageReusesBuffersAndPreservesSamples(t *testing.T) {
	samples := make([]int16, 3200)
	for i := range samples {
		samples[i] = int16(i*31 - 9000)
	}
	pcm := make([]byte, 0, 2*len(samples))
	message := make([]byte, 0, 12+base64.StdEncoding.EncodedLen(2*len(samples)))
	for run := 0; run < 3; run++ {
		pcm = pcm16ToBytes(pcm[:0], samples)
		message = appendRealtimeAudio(message[:0], pcm)
		var event struct {
			Type  string `json:"type"`
			Audio string `json:"audio"`
		}
		if err := json.Unmarshal(message, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "input_audio_buffer.append" {
			t.Fatalf("wrong event type %q", event.Type)
		}
		decoded, err := base64.StdEncoding.DecodeString(event.Audio)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(decoded, pcm) {
			t.Fatal("decoded append audio changed")
		}
	}
}

func TestPreparedSamplesRequestMatchesWav(t *testing.T) {
	samples := []int16{1, -2, 3, -4, 0x7fff, -0x8000}
	pcm, ok := pcm16Bytes(samples)
	if !ok {
		t.Skip("zero-copy PCM view requires a little-endian host")
	}
	wav := WavEncode(samples)
	settings := sttSettings{Provider: providerOpenAI, Model: defaultModelID, APIKey: "secret"}
	client := newSTTClient(settings)
	prepared, err := client.prepareSamplesRequest(samples, pcm)
	if err != nil {
		t.Fatal(err)
	}
	request, err := client.requestFrom(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	request.Body.Close()
	if int64(len(body)) != prepared.contentLength {
		t.Fatalf("body length %d != prepared %d", len(body), prepared.contentLength)
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	if err := request.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	files := request.MultipartForm.File["file"]
	if len(files) != 1 {
		t.Fatalf("missing audio part: %v", request.MultipartForm.File)
	}
	file, err := files[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	audio, readErr := io.ReadAll(file)
	file.Close()
	if readErr != nil || !bytes.Equal(audio, wav) {
		t.Fatal("zero-copy multipart audio changed")
	}

	jsonSettings := settings
	jsonSettings.Provider = providerOpenRouter
	jsonClient := newSTTClient(jsonSettings)
	jsonPrepared, err := jsonClient.prepareSamplesRequest(samples, pcm)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		InputAudio struct {
			Data string `json:"data"`
		} `json:"input_audio"`
	}
	if err := json.Unmarshal(jsonPrepared.payload, &payload); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.InputAudio.Data)
	if err != nil || !bytes.Equal(decoded, wav) {
		t.Fatal("zero-copy JSON audio changed")
	}
}

func TestEvictUnusedModelsKeepsSelectedAndBusy(t *testing.T) {
	manager := newLocalModelManager()
	manager.native["keep"] = &nativeModel{}
	manager.native["drop"] = &nativeModel{}
	if evicted := manager.evictUnused("keep"); evicted != 1 {
		t.Fatalf("evicted %d models, want 1", evicted)
	}
	if manager.native["keep"] == nil || manager.native["drop"] != nil {
		t.Fatalf("unexpected loaded models: %v", manager.native)
	}

	manager.native["busy"] = &nativeModel{}
	manager.active["busy"] = 1
	manager.evictUnused("")
	if manager.native["busy"] == nil {
		t.Fatal("evicted a model with an active use")
	}
}

func TestBatchRequestsAreReplayableAndEquivalent(t *testing.T) {
	wav := WavEncode([]int16{1, -2, 3, -4, 0x7fff})
	settings := sttSettings{
		Provider:   providerOpenAI,
		Model:      defaultModelID,
		APIKey:     "secret",
		Languages:  []string{"es", "de"},
		Vocabulary: []string{`Voice "Type"`, `back\slash`, "naïve"},
	}
	client := newSTTClient(settings)
	prepared, err := client.prepareRequest(wav)
	if err != nil {
		t.Fatal(err)
	}
	var bodies [][]byte
	for attempt := 0; attempt < 2; attempt++ {
		request, err := client.requestFrom(context.Background(), prepared)
		if err != nil {
			t.Fatal(err)
		}
		if request.ContentLength != prepared.contentLength {
			t.Fatalf("content length %d != prepared %d", request.ContentLength, prepared.contentLength)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		request.Body.Close()
		if int64(len(body)) != prepared.contentLength {
			t.Fatalf("replay %d length %d != %d", attempt, len(body), prepared.contentLength)
		}
		bodies = append(bodies, body)
		request.Body = io.NopCloser(bytes.NewReader(body))
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(request.MultipartForm.Value["languages[]"], settings.Languages) {
			t.Fatalf("languages changed: %v", request.MultipartForm.Value)
		}
		if !reflect.DeepEqual(request.MultipartForm.Value["keywords[]"], settings.Vocabulary) {
			t.Fatalf("vocabulary changed: %v", request.MultipartForm.Value)
		}
		files := request.MultipartForm.File["file"]
		if len(files) != 1 {
			t.Fatalf("missing audio part: %v", request.MultipartForm.File)
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		audio, err := io.ReadAll(file)
		file.Close()
		if err != nil || !bytes.Equal(audio, wav) {
			t.Fatal("multipart audio changed")
		}
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Fatal("retry payload changed")
	}

	jsonSettings := settings
	jsonSettings.Provider = providerOpenRouter
	jsonClient := newSTTClient(jsonSettings)
	jsonPrepared, err := jsonClient.prepareRequest(wav)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Model       string `json:"model"`
		Language    string `json:"language"`
		Temperature int    `json:"temperature"`
		InputAudio  struct {
			Data   string `json:"data"`
			Format string `json:"format"`
		} `json:"input_audio"`
		Provider struct {
			Options struct {
				OpenAI struct {
					Prompt string `json:"prompt"`
				} `json:"openai"`
			} `json:"options"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(jsonPrepared.payload, &payload); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.InputAudio.Data)
	if err != nil || !bytes.Equal(decoded, wav) {
		t.Fatal("JSON audio changed")
	}
	if payload.Model != settings.Model || payload.Language != "en" || payload.Temperature != 0 || payload.InputAudio.Format != "wav" {
		t.Fatalf("JSON metadata changed: %+v", payload)
	}
	if !strings.Contains(payload.Provider.Options.OpenAI.Prompt, `Voice "Type"`) {
		t.Fatalf("vocabulary prompt was not escaped: %q", payload.Provider.Options.OpenAI.Prompt)
	}
}

func TestBatchResponseLimitAndEarlyRejection(t *testing.T) {
	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), sttResponseLimit+10))
	}))
	defer oversized.Close()
	client := newSTTClient(sttSettings{Provider: providerOpenAI, Model: defaultModelID, APIKey: "key"})
	client.endpoint = oversized.URL
	client.http = oversized.Client()
	client.retryDelay = time.Millisecond
	_, err := client.transcribeContext(context.Background(), WavEncode([]int16{1, 2}), nil)
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversized response was not rejected: %v", err)
	}

	attempts := 0
	rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key","type":"invalid_request_error"}}`))
	}))
	defer rejected.Close()
	client.endpoint = rejected.URL
	client.http = rejected.Client()
	_, err = client.transcribeContext(context.Background(), WavEncode([]int16{1, 2}), nil)
	var sttErr *sttError
	if !errors.As(err, &sttErr) || sttErr.Status != http.StatusUnauthorized {
		t.Fatalf("authorization failure was not classified: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("early rejection retried %d times", attempts)
	}
}

func TestBatchRetryReplaysPayloadAfterPartialRead(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			return
		}
		mu.Lock()
		bodies = append(bodies, body)
		attempts++
		attempt := attempts
		mu.Unlock()
		if attempt == 1 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("test server cannot hijack")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n{\"text\":\"partial"))
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"replayed"}`))
	}))
	defer server.Close()
	client := newSTTClient(sttSettings{Provider: providerOpenAI, Model: defaultModelID, APIKey: "key"})
	client.endpoint = server.URL
	client.http = server.Client()
	client.retryDelay = time.Millisecond
	result, err := client.transcribeContext(context.Background(), WavEncode([]int16{1, 2, 3}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "replayed" {
		t.Fatalf("unexpected result %q", result.Text)
	}
	mu.Lock()
	gotAttempts := attempts
	gotBodies := append([][]byte(nil), bodies...)
	mu.Unlock()
	if gotAttempts != 2 {
		t.Fatalf("partial response did not trigger exactly one retry: %d attempts", gotAttempts)
	}
	if len(gotBodies) != 2 || !bytes.Equal(gotBodies[0], gotBodies[1]) {
		t.Fatal("retry did not replay the identical payload")
	}

	blocked := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer blocked.Close()
	client.endpoint = blocked.URL
	client.http = blocked.Client()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.transcribeContext(ctx, WavEncode([]int16{1}), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
}

func TestLogPageFiltersBeforePaginationAndRejectsUnchangedPolls(t *testing.T) {
	isolatedPreferences(t)
	log := newRunLog("test")
	defer log.close("ok")
	for i := 0; i < 450; i++ {
		outcome := "ok"
		if i%2 == 1 {
			outcome = "error"
		}
		log.write(runLogRecord{Event: "dictation.completed", Outcome: outcome, Message: fmt.Sprintf("n=%d", i)})
	}
	first, err := log.page("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 450 || first.Retained != 450 || len(first.Records) != runLogPageSize || first.Unchanged {
		t.Fatalf("first page wrong: total=%d retained=%d len=%d unchanged=%v", first.Total, first.Retained, len(first.Records), first.Unchanged)
	}
	if first.Records[0].Message != "n=449" || first.Records[len(first.Records)-1].Message != "n=250" {
		t.Fatalf("page was not newest first: %q..%q", first.Records[0].Message, first.Records[len(first.Records)-1].Message)
	}
	second, err := log.page("all", runLogPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Records) != runLogPageSize || second.Records[0].Message != "n=249" || second.Records[len(second.Records)-1].Message != "n=50" {
		t.Fatalf("second page wrong: %q..%q", second.Records[0].Message, second.Records[len(second.Records)-1].Message)
	}
	last, err := log.page("all", 2*runLogPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Records) != 50 || last.Records[0].Message != "n=49" || last.Records[len(last.Records)-1].Message != "n=0" {
		t.Fatalf("last page wrong: %q..%q", last.Records[0].Message, last.Records[len(last.Records)-1].Message)
	}
	errorsPage, err := log.page("errors", 0)
	if err != nil {
		t.Fatal(err)
	}
	if errorsPage.Total != 225 || errorsPage.Retained != 450 || len(errorsPage.Records) != runLogPageSize || errorsPage.Records[0].Message != "n=449" {
		t.Fatalf("filter applied after pagination: total=%d retained=%d len=%d first=%q", errorsPage.Total, errorsPage.Retained, len(errorsPage.Records), errorsPage.Records[0].Message)
	}
	unchanged, err := log.page("errors", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.Unchanged || len(unchanged.Records) != 0 || unchanged.Revision != errorsPage.Revision {
		t.Fatalf("unchanged poll parsed records: %+v", unchanged)
	}
	log.write(runLogRecord{Event: "dictation.completed", Outcome: "ok", Message: "n=450"})
	afterWrite, err := log.page("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if afterWrite.Unchanged || afterWrite.Total != 451 || afterWrite.Records[0].Message != "n=450" {
		t.Fatalf("new record did not change the revision: %+v", afterWrite)
	}
	if err := log.clear(); err != nil {
		t.Fatal(err)
	}
	cleared, err := log.page("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Total != 0 || len(cleared.Records) != 0 {
		t.Fatalf("clear left records: %+v", cleared)
	}
}

func TestLogPageIncludesRotatedSegmentAndExportStreamsAll(t *testing.T) {
	isolatedPreferences(t)
	current := runLogFilePath()
	rotated := current + ".1"
	writeLine := func(path, message string) {
		record := runLogRecord{Event: "dictation.completed", Outcome: "ok", Message: message}
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeLine(rotated, "oldest")
	writeLine(current, "newest")
	log := newRunLog("test")
	defer log.close("ok")
	page, err := log.page("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || page.Records[0].Message != "newest" || page.Records[1].Message != "oldest" {
		t.Fatalf("rotated segment missing or misordered: %+v", page.Records)
	}
	var out bytes.Buffer
	if err := log.writeFiltered("all", &out); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 2 {
		t.Fatalf("export did not stream all records: %q", out.String())
	}
	if !strings.Contains(out.String(), "oldest") || !strings.Contains(out.String(), "newest") {
		t.Fatal("export skipped a retained segment")
	}
}

func TestDiagnosticExportUsesPrivatePermissionsAndCompleteHistory(t *testing.T) {
	isolatedPreferences(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for i := 0; i < 3; i++ {
		service.runLog.diagnostic("dictation.completed", "ok", fmt.Sprintf("record-%d", i))
	}
	bridge := &Bridge{engine: service}
	path := t.TempDir() + "/logs.jsonl"
	if err := bridge.writeExportFile(path, "all"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("export permissions are %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := service.runLog.records()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != len(records) {
		t.Fatalf("export wrote %d records, want %d", got, len(records))
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(string(data), fmt.Sprintf("record-%d", i)) {
			t.Fatalf("export skipped record-%d", i)
		}
	}
}

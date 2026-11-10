package voicetype

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func cloudTestClient() *sttClient {
	return newSTTClient(sttSettings{Provider: providerOpenAI, Model: defaultModelID, APIKey: "test-key", Vocabulary: []string{"Voice Type"}, Languages: []string{"en", "es"}, StreamingMode: streamingModeProcess})
}

func startCloudStreamAt(c *sttClient, ctx context.Context, endpoint string, setupTimeout, finalTimeout time.Duration) (*cloudStreamSession, error) {
	s := c.startCloudStreamAsyncAt(ctx, endpoint, setupTimeout, finalTimeout)
	select {
	case <-s.ready:
		return s, nil
	case <-s.ctx.Done():
		s.Abort()
		s.mu.Lock()
		err := s.result.Err
		s.mu.Unlock()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, err
	}
}
func websocketTestServer(t *testing.T, handler func(context.Context, *websocket.Conn)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing selected credential snapshot")
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		handler(ctx, conn)
	}))
	t.Cleanup(server.Close)
	return strings.Replace(server.URL, "http://", "ws://", 1)
}
func readTestJSON(ctx context.Context, conn *websocket.Conn) (map[string]any, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	var event map[string]any
	err = json.Unmarshal(data, &event)
	return event, err
}

func TestCloudStreamUploadsBeforeStopAndCommitsExactlyOnce(t *testing.T) {
	audioReceived := make(chan struct{})
	events := make(chan string, 32)
	serverDone := make(chan struct{})
	var audio []int16
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		defer close(serverDone)
		update, err := readTestJSON(ctx, conn)
		if err != nil {
			t.Error(err)
			return
		}
		input := update["session"].(map[string]any)["audio"].(map[string]any)["input"].(map[string]any)
		transcription := input["transcription"].(map[string]any)
		if update["type"] != "session.update" || input["turn_detection"] != nil || transcription["model"] != defaultModelID || !reflect.DeepEqual(transcription["languages"], []any{"en", "es"}) || !reflect.DeepEqual(transcription["keywords"], []any{"Voice Type"}) {
			t.Error("incorrect manual transcription configuration")
		}
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.created"})
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		first := true
		for {
			event, err := readTestJSON(ctx, conn)
			if err != nil {
				return
			}
			kind, _ := event["type"].(string)
			events <- kind
			if kind == "input_audio_buffer.append" {
				data, err := base64.StdEncoding.DecodeString(event["audio"].(string))
				if err != nil {
					t.Error(err)
					return
				}
				for i := 0; i < len(data); i += 2 {
					audio = append(audio, int16(binary.LittleEndian.Uint16(data[i:])))
				}
				if first {
					close(audioReceived)
					first = false
				}
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.delta", "item_id": "unrelated", "delta": "never insert"})
			}
			if kind == "input_audio_buffer.commit" {
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "wrong", "transcript": "wrong text"})
				// Completion and acknowledgement may be delivered in either order.
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "right", "transcript": "final text"})
				_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "right"})
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "right", "transcript": "duplicate"})
			}
		}
	})
	stream, err := startCloudStreamAt(cloudTestClient(), context.Background(), endpoint, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Abort()
	samples := make([]int16, 3201)
	for i := range samples {
		samples[i] = int16(i - 1600)
	}
	stream.OfferSamples(samples[:1500])
	select {
	case <-audioReceived:
	case <-time.After(time.Second):
		t.Fatal("audio not uploaded during recording")
	}
	select {
	case kind := <-events:
		if kind != "input_audio_buffer.append" {
			t.Fatalf("committed before Stop: %s", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("no append")
	}
	select {
	case kind := <-events:
		t.Fatalf("unexpected event before Stop: %s", kind)
	case <-time.After(20 * time.Millisecond):
	}
	stream.mu.Lock()
	textBeforeStop := stream.result.Text
	stream.mu.Unlock()
	if textBeforeStop != "" {
		t.Fatal("delta became a final transcript")
	}
	stream.OfferSamples(samples[1500:])
	result := stream.Finish()
	if result.Err != nil || result.Text != "final text" {
		t.Fatalf("wrong final result: %+v", result)
	}
	if result.Timing.FinalAudioSent.IsZero() || result.Timing.CommitSent.Before(result.Timing.FinalAudioSent) || result.Timing.TranscriptReceived.IsZero() {
		t.Fatal("missing or invalid stream timings")
	}
	<-serverDone
	close(events)
	commits := 0
	committed := false
	for event := range events {
		if event == "input_audio_buffer.commit" {
			commits++
			committed = true
		} else if committed {
			t.Fatal("audio sent after commit")
		}
	}
	if commits != 1 {
		t.Fatalf("commits=%d", commits)
	}
	var resampler pcmResampler
	want := append(resampler.Process(samples), resampler.Flush()...)
	if !reflect.DeepEqual(audio, want) {
		t.Fatalf("resampled audio differs: got %d samples want %d", len(audio), len(want))
	}
}

func TestCloudStreamFailuresAndCancellation(t *testing.T) {
	for _, scenario := range []string{"malformed", "disconnect", "server_error", "transcription_failed", "failed_before_ack", "unexpected_commit", "missing_transcript", "timeout", "cancel", "wrong_item"} {
		t.Run(scenario, func(t *testing.T) {
			endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
				if _, err := readTestJSON(ctx, conn); err != nil {
					return
				}
				_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
				for {
					event, err := readTestJSON(ctx, conn)
					if err != nil {
						return
					}
					if event["type"] != "input_audio_buffer.commit" {
						continue
					}
					switch scenario {
					case "malformed":
						_ = conn.Write(ctx, websocket.MessageText, []byte("not json"))
					case "disconnect":
						return
					case "server_error":
						_ = writeRealtime(ctx, conn, map[string]any{"type": "error", "error": map[string]any{"message": "SECRET should not enter logs"}})
					case "transcription_failed":
						_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "right"})
						_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.failed", "item_id": "right"})
					case "failed_before_ack":
						_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.failed", "item_id": "right"})
						_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "right"})
					case "unexpected_commit":
						_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed"})
					case "missing_transcript":
						_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "right"})
					case "wrong_item":
						_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "right"})
						_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "wrong", "transcript": "never insert"})
					}
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := startCloudStreamAt(cloudTestClient(), ctx, endpoint, time.Second, 50*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			stream.OfferSamples(make([]int16, 1600))
			if scenario == "cancel" {
				cancel()
			}
			result := stream.Finish()
			if result.Err == nil || result.Text != "" || strings.Contains(result.Err.Error(), "SECRET") {
				t.Fatalf("failure not sanitized: %+v", result)
			}
		})
	}
}

func TestCloudStreamWaitsForConfigurationAcknowledgement(t *testing.T) {
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		_, _ = readTestJSON(ctx, conn)
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.created"})
		_, _, _ = conn.Read(ctx)
	})
	if stream, err := startCloudStreamAt(cloudTestClient(), context.Background(), endpoint, 30*time.Millisecond, time.Second); err == nil {
		stream.Abort()
		t.Fatal("recording accepted without configuration acknowledgement")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := startCloudStreamAt(cloudTestClient(), ctx, endpoint, time.Second, time.Second); err != context.Canceled {
		t.Fatalf("setup cancellation: %v", err)
	}
}
func TestCloudStreamQueueOverflowIsNonblockingAndExplicit(t *testing.T) {
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		_, _ = readTestJSON(ctx, conn)
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		_, _, _ = conn.Read(ctx)
	})
	stream, err := startCloudStreamAt(cloudTestClient(), context.Background(), endpoint, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	stream.OfferSamples(make([]int16, cloudQueueSamples+1))
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("capture callback blocked")
	}
	result := stream.Finish()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "queue exceeded") {
		t.Fatalf("overflow not reported: %+v", result)
	}
}
func TestPCMResamplerContinuity(t *testing.T) {
	for _, size := range []int{0, 1, 2, 3, 7, 16000, 16001} {
		input := make([]int16, size)
		for i := range input {
			input[i] = int16(i * 47)
		}
		var whole pcmResampler
		expected := append(whole.Process(input), whole.Flush()...)
		if len(expected) != (size*3+1)/2 {
			t.Fatalf("length %d -> %d", size, len(expected))
		}
		for _, step := range []int{1, 2, 17, 319, 3200} {
			var chunked pcmResampler
			var actual []int16
			for i := 0; i < size; i += step {
				actual = append(actual, chunked.Process(input[i:min(i+step, size)])...)
			}
			actual = append(actual, chunked.Flush()...)
			if len(expected) != len(actual) || (len(expected) > 0 && !reflect.DeepEqual(expected, actual)) {
				t.Fatalf("chunk boundary changed samples: size %d step %d", size, step)
			}
			if len(chunked.Flush()) != 0 {
				t.Fatal("flush replayed endpoint")
			}
		}
	}
}

func TestOpenAIBatchLanguageHintsAndVocabulary(t *testing.T) {
	for _, languages := range [][]string{nil, {"es", "de"}} {
		settings := sttSettings{Provider: providerOpenAI, Model: defaultModelID, Languages: languages, Vocabulary: []string{"Voice Type", "Parakeet"}}
		client := newSTTClient(settings)
		request, err := client.buildRequest(context.Background(), WavEncode([]int16{1, 2, 3}))
		if err != nil {
			t.Fatal(err)
		}
		if err = request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(request.MultipartForm.Value["languages[]"], languages) {
			t.Fatalf("language hints changed: %v", request.MultipartForm.Value)
		}
		if request.MultipartForm.Value["language"] != nil || !reflect.DeepEqual(request.MultipartForm.Value["keywords[]"], settings.Vocabulary) {
			t.Fatal("wrong batch hints")
		}
	}
}

func TestCloudStreamEngineInsertsFinalOnceAndRecordsTiming(t *testing.T) {
	setupEngineTest(t)
	view := readSettingsTest(t)
	if err := SaveAPIKey(providerOpenAI, "test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: providerOpenAI, Name: "Streaming"}); err != nil {
		t.Fatal(err)
	}
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		_, _ = readTestJSON(ctx, conn)
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		for {
			event, err := readTestJSON(ctx, conn)
			if err != nil {
				return
			}
			if event["type"] == "input_audio_buffer.commit" {
				_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "right"})
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.delta", "item_id": "right", "delta": "never insert"})
				for range 2 {
					_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "right", "transcript": "final transcript"})
				}
			}
		}
	})
	inserter := &recordingInserter{}
	oldFactory := textInserterBackends[InsertBackendIBus]
	textInserterBackends[InsertBackendIBus] = func(context.Context) (TextInserter, error) { return inserter, nil }
	defer func() { textInserterBackends[InsertBackendIBus] = oldFactory }()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &fixtureRecorder{}
	for range 3 {
		stream, err := startCloudStreamAt(service.stt, context.Background(), endpoint, time.Second, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		stream.OfferSamples(make([]int16, 3200))
		service.mu.Lock()
		service.state = stateRecording
		service.sessionID++
		id := service.sessionID
		service.sessionCtx = context.Background()
		service.activeStream = stream
		service.mu.Unlock()
		if err := service.stopSessionMatching(id, false); err != nil {
			t.Fatal(err)
		}
		if err := service.stopSessionMatching(id, true); err != nil {
			t.Fatal(err)
		} // expired cap must not replay
		if service.Status().State != "idle" {
			t.Fatal("recording did not return idle")
		}
	}
	if got := inserter.inserted(); got != "final transcriptfinal transcriptfinal transcript" {
		t.Fatalf("insertion was missing or duplicated: %q", got)
	}
	records, err := service.Logs("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 3 {
		t.Fatalf("completed dictations=%d", len(records.Records))
	}
	for _, record := range records.Records {
		if record.Outcome != "ok" || record.Timing == nil || record.Timing.StopToTranscriptMS == nil || record.Timing.StopToSubmissionMS == nil || record.Timing.InsertionCleanupCompleted == "" {
			t.Fatalf("completion timing missing: %+v", record)
		}
		encoded, _ := json.Marshal(record)
		if strings.Contains(string(encoded), "final transcript") || strings.Contains(string(encoded), "test-key") {
			t.Fatal("private data in timing diagnostics")
		}
	}
}

// Explicit opt-in; never uses environment credentials or logs private content.
// This proves live API compatibility with synthetic speech, not microphone or
// visible desktop insertion acceptance.
func TestLiveOpenAIWebSocket(t *testing.T) {
	path := os.Getenv("VOICE_TYPE_TEST_LIVE_WS_WAV")
	if path == "" {
		t.Skip("set VOICE_TYPE_TEST_LIVE_WS_WAV for real OpenAI WebSocket verification")
	}
	wav, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	samples, err := decodeWAVPCM16(wav)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := configReadStrict()
	if err != nil {
		t.Fatal("cannot read configured credentials")
	}
	profile, ok := cfg.activeProfile()
	if !ok || profile.Provider != providerOpenAI {
		t.Skip("select an OpenAI profile for live WebSocket verification")
	}
	key, saved, err := providerAPIKey(providerOpenAI)
	if err != nil || !saved {
		t.Fatal("cannot read configured keyring credential", err)
	}
	settings := sttSettings{Provider: providerOpenAI, Model: defaultModelID, APIKey: key, Vocabulary: []string{"Voice Type"}}
	if os.Getenv("VOICE_TYPE_TEST_LIVE_WS_PROFILE_SETTINGS") == "1" {
		settings, err = cfg.resolveSTT(workspaceLoad())
		if err != nil {
			t.Fatal("cannot resolve configured profile settings")
		}
	}

	if hint := os.Getenv("VOICE_TYPE_TEST_LIVE_WS_LANGUAGE"); hint != "" {
		settings.Languages = []string{hint}
	}
	stream := newSTTClient(settings).startCloudStreamAsyncAt(context.Background(), openAIRealtimeURL, 10*time.Second, 120*time.Second)
	defer stream.Abort()
	earlyStop := os.Getenv("VOICE_TYPE_TEST_LIVE_WS_STOP_EARLY") == "1"
	if earlyStop {
		stream.OfferSamples(samples[:min(2*wavSampleRate, len(samples))])
	} else {
		for start := 0; start < len(samples); start += 3200 {
			stream.OfferSamples(samples[start:min(start+3200, len(samples))])
			time.Sleep(200 * time.Millisecond)
		}
	}
	stop := time.Now()
	result := stream.Finish()
	if result.Err != nil {
		t.Fatalf("live WebSocket finalization failed: %s", safeRequestError(result.Err))
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatal("live WebSocket returned no text")
	}
	if expected := os.Getenv("VOICE_TYPE_TEST_LIVE_WS_EXPECT_TEXT"); expected != "" && strings.Trim(normalizeTranscript(result.Text), " .,!?:;\"’'") != strings.Trim(normalizeTranscript(expected), " .,!?:;\"’'") {
		t.Fatal("real API did not recognize the expected nonsensitive test phrase")
	}

	if earlyStop && !stop.Before(result.Timing.ConnectionReady) {
		t.Fatal("early-stop live check did not precede session acknowledgement")
	}
	if earlyStop {
		t.Log("real API accepted Stop before connection readiness; nonsensitive short recording completed")
	}

	t.Logf("real API synthetic speech: cold connect/configure %dms; Stop-to-transcript %dms; %d transcript characters; no desktop insertion attempted", result.Timing.ConnectionReady.Sub(result.Timing.ConnectionStarted).Milliseconds(), result.Timing.TranscriptReceived.Sub(stop).Milliseconds(), len(result.Text))
}

func TestCloudStreamAuthenticationRejectedWithoutResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "SECRET provider payload", http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := startCloudStreamAt(cloudTestClient(), context.Background(), strings.Replace(server.URL, "http://", "ws://", 1), time.Second, time.Second)
	if err == nil || safeRequestError(err) != "openai 401" || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe authentication failure: %v", err)
	}
}

type redirectedRealtimeTransport struct {
	target   string
	original http.RoundTripper
}

func (r redirectedRealtimeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copy := request.Clone(request.Context())
	target, err := url.Parse(r.target)
	if err != nil {
		return nil, err
	}
	copy.URL.Scheme = "http"
	copy.URL.Host = target.Host
	copy.Host = target.Host
	return r.original.RoundTrip(copy)
}

type streamingFixtureRecorder struct {
	fixtureRecorder
	sink    func([]int16)
	starts  int
	buffer  *sessionAudioBuffer
	failure func(error)
	initial []int16
}

func (r *streamingFixtureRecorder) SetChunkSink(sink func([]int16)) { r.sink = sink }
func (r *streamingFixtureRecorder) SetSessionBuffer(buffer *sessionAudioBuffer, failure func(error)) {
	r.buffer, r.failure = buffer, failure
}
func (r *streamingFixtureRecorder) Start() error {
	r.starts++
	samples := r.initial
	if samples == nil {
		samples = make([]int16, 3200)
	}
	if r.buffer != nil {
		if err := r.buffer.append(samples); err != nil && r.failure != nil {
			r.failure(err)
		}
	} else if r.sink != nil {
		r.sink(samples)
	}
	return nil
}
func (r *streamingFixtureRecorder) Stop() ([]int16, error) {
	if r.buffer != nil {
		buffer := r.buffer
		r.buffer = nil
		return buffer.freeze(), nil
	}
	return r.fixtureRecorder.Stop()
}

func TestCloudCaptureStartsImmediatelyAndStopBeforeAckFinishes(t *testing.T) {
	setupEngineTest(t)
	view := readSettingsTest(t)
	if err := SaveAPIKey(providerOpenAI, "test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: providerOpenAI, Name: "Streaming"}); err != nil {
		t.Fatal(err)
	}
	entered, acknowledge := make(chan struct{}), make(chan struct{})
	var sessions atomic.Int32
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		if _, err := readTestJSON(ctx, conn); err != nil {
			return
		}
		if sessions.Add(1) == 1 {
			close(entered)
			select {
			case <-acknowledge:
			case <-ctx.Done():
				return
			}
		}
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		for {
			event, err := readTestJSON(ctx, conn)
			if err != nil {
				return
			}
			if event["type"] == "input_audio_buffer.commit" {
				_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "next"})
				_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "next", "transcript": "next recording works"})
			}
		}
	})
	originalTransport := http.DefaultTransport
	http.DefaultTransport = redirectedRealtimeTransport{target: endpoint, original: originalTransport}
	defer func() { http.DefaultTransport = originalTransport }()
	inserter := &recordingInserter{}
	oldFactory := textInserterBackends[InsertBackendIBus]
	textInserterBackends[InsertBackendIBus] = func(context.Context) (TextInserter, error) { return inserter, nil }
	defer func() { textInserterBackends[InsertBackendIBus] = oldFactory }()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	recorder := &streamingFixtureRecorder{}
	service.recorder = recorder
	started := service.ToggleAsync()
	select {
	case err := <-started:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("capture waited on OpenAI acknowledgement")
	}
	if service.Status().State != "recording" || recorder.starts != 1 {
		t.Fatal("capture not immediately active")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("setup did not reach mock server")
	}
	stopped := service.ToggleAsync()
	deadline := time.Now().Add(time.Second)
	for service.Status().State != "transcribing" {
		if time.Now().After(deadline) {
			t.Fatal("Stop did not finish capture during setup")
		}
		time.Sleep(time.Millisecond)
	}
	if inserter.inserted() != "" {
		t.Fatal("inserted before final transcript")
	}
	if err := <-service.ToggleAsync(); err == nil {
		t.Fatal("accepted another recording during finalization")
	}
	close(acknowledge)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop cancelled pending dictation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending dictation did not complete")
	}
	if service.Status().State != "idle" || service.Status().LastAttemptFailed {
		t.Fatal("completed take showed failure")
	}
	if err := service.Toggle(); err != nil {
		t.Fatalf("next start failed: %v", err)
	}
	if err := service.Toggle(); err != nil {
		t.Fatalf("next finish failed: %v", err)
	}
	if inserter.inserted() != "next recording worksnext recording works" {
		t.Fatal("recordings missing or duplicated")
	}
	records, err := service.Logs("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 2 {
		t.Fatal("unexpected recording count")
	}
	timing := records.Records[1].Timing
	if timing == nil || timing.CaptureStartupMS == nil || timing.StartupBacklogDrained == "" || timing.CaptureStarted >= timing.ConnectionReady {
		t.Fatalf("capture/connection timing not separated: %+v", timing)
	}
}

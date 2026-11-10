package voicetype

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestCloudDelayedSetupPreservesStartupAudio(t *testing.T) {
	for _, delay := range []time.Duration{2 * time.Second, 8 * time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			var received []int16
			serverDone := make(chan struct{})
			endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
				defer close(serverDone)
				if _, err := readTestJSON(ctx, conn); err != nil {
					return
				}
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return
				}
				_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
				commits := 0
				for {
					event, err := readTestJSON(ctx, conn)
					if err != nil {
						return
					}
					switch event["type"] {
					case "input_audio_buffer.append":
						pcm, err := base64.StdEncoding.DecodeString(event["audio"].(string))
						if err != nil {
							t.Error(err)
							return
						}
						for i := 0; i < len(pcm); i += 2 {
							received = append(received, int16(binary.LittleEndian.Uint16(pcm[i:])))
						}
					case "input_audio_buffer.commit":
						commits++
						if commits != 1 {
							t.Error("duplicate commit")
						}
						_ = writeRealtime(ctx, conn, map[string]any{"type": "input_audio_buffer.committed", "item_id": "buffered"})
						_ = writeRealtime(ctx, conn, map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "buffered", "transcript": "complete recording"})
					}
				}
			})
			stream := cloudTestClient().startCloudStreamAsyncAt(context.Background(), endpoint, 10*time.Second, 15*time.Second)
			defer stream.Abort()
			// More than the old five-second queue, captured while acknowledgement is
			// withheld. Feed varying chunk boundaries including the very first sample.
			samples := make([]int16, 8*wavSampleRate+17)
			for i := range samples {
				samples[i] = int16(i*47 - 12000)
			}
			for i := 0; i < len(samples); i += 319 {
				stream.OfferSamples(samples[i:min(i+319, len(samples))])
			}
			result := stream.Finish() // Stop before acknowledgement; must still finish
			if result.Err != nil || result.Text != "complete recording" {
				t.Fatalf("startup audio rejected or lost: %+v", result)
			}
			<-serverDone
			var resampler pcmResampler
			want := append(resampler.Process(samples), resampler.Flush()...)
			if !reflect.DeepEqual(received, want) {
				t.Fatalf("initial/final audio changed: received=%d expected=%d", len(received), len(want))
			}
		})
	}
}

func TestSessionAudioPostAckBudgetAndImmutableStop(t *testing.T) {
	buffer := newSessionAudioBuffer()
	if err := buffer.append(make([]int16, 8*wavSampleRate)); err != nil {
		t.Fatal(err)
	}
	buffer.acknowledge()
	if err := buffer.append(make([]int16, cloudQueueSamples)); err != nil {
		t.Fatal("startup backlog incorrectly counted against new capture", err)
	}
	if err := buffer.append([]int16{1}); err == nil {
		t.Fatal("post-ack queue exceeded five seconds silently")
	}
	buffer.advance(8*wavSampleRate - 1)
	if err := buffer.append([]int16{1}); err == nil {
		t.Fatal("consuming startup backlog admitted unsent new capture")
	}
	buffer.advance(8*wavSampleRate + 3200)
	if err := buffer.append([]int16{1}); err != nil {
		t.Fatal("successful upload did not free post-ack budget", err)
	}
	frozen := buffer.freeze()
	oldCount := len(frozen)
	if err := buffer.append([]int16{99}); err != nil {
		t.Fatal(err)
	}
	if len(buffer.freeze()) != oldCount {
		t.Fatal("audio appended after Stop")
	}
}

func TestRecorderSessionBufferSurvivesStopAndNextRecording(t *testing.T) {
	recorder := &Recorder{}
	buffer := newSessionAudioBuffer()
	recorder.SetSessionBuffer(buffer, func(err error) { t.Error(err) })
	_, _ = recorder.acceptSamples([]int16{17, -29, 300})
	frozen, err := recorder.Stop()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = recorder.acceptSamples([]int16{99}) // independent next recording
	if !reflect.DeepEqual(frozen, []int16{17, -29, 300}) {
		t.Fatal("next capture overwrote old upload buffer")
	}
	old, closed := buffer.read(0)
	if !closed || !reflect.DeepEqual(old, frozen) {
		t.Fatal("Stop cleared pending upload")
	}
}

func TestCloudFailureStopsActiveCaptureWithoutInsertion(t *testing.T) {
	setupEngineTest(t)
	view := readSettingsTest(t)
	if err := SaveAPIKey(providerOpenAI, "test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: providerOpenAI, Name: "Streaming"}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	serverURL := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		_ = writeRealtime(ctx, conn, map[string]any{"type": "error"})
	})
	original := http.DefaultTransport
	http.DefaultTransport = redirectedRealtimeTransport{target: serverURL, original: original}
	defer func() { http.DefaultTransport = original }()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &streamingFixtureRecorder{}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	if service.Status().State != "recording" {
		t.Fatal("capture did not start")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		status := service.Status()
		if status.State == "idle" && status.LastAttemptFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup failure left microphone running: %+v", status)
		}
		time.Sleep(time.Millisecond)
	}
	records, err := service.Logs("all", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) != 1 || records.Records[0].Outcome == "cancelled" || !strings.Contains(records.Records[0].Error, "configuration") {
		t.Fatalf("provider failure was not explicit: %+v", records)
	}
}

func TestCloudShutdownDuringSetupDrainsWorkers(t *testing.T) {
	setupEngineTest(t)
	view := readSettingsTest(t)
	if err := SaveAPIKey(providerOpenAI, "test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Provider: providerOpenAI, Name: "Streaming"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		if _, err := readTestJSON(ctx, conn); err != nil {
			return
		}
		close(entered)
		_, _, _ = conn.Read(ctx)
	})
	original := http.DefaultTransport
	http.DefaultTransport = redirectedRealtimeTransport{target: endpoint, original: original}
	defer func() { http.DefaultTransport = original }()
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	service.recorder = &streamingFixtureRecorder{}
	if err := service.Toggle(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("connection never started")
	}
	service.mu.Lock()
	stream := service.activeStream.(*cloudStreamSession)
	service.mu.Unlock()
	done := make(chan struct{})
	go func() { service.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown leaked pending setup/capture workers")
	}
	select {
	case <-stream.done:
	default:
		t.Fatal("cloud worker survived shutdown")
	}
	if service.Status().State != "idle" {
		t.Fatal("shutdown left capture active")
	}
}

func TestCloudFinishingTimeoutIncludesPendingSetupAndNeverCommitsEmpty(t *testing.T) {
	endpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		if _, err := readTestJSON(ctx, conn); err != nil {
			return
		}
		// Withhold acknowledgement until the client cancels its finalization.
		_, _, _ = conn.Read(ctx)
	})
	stream := cloudTestClient().startCloudStreamAsyncAt(context.Background(), endpoint, time.Second, 30*time.Millisecond)
	stream.OfferSamples(make([]int16, 1600))
	result := stream.Finish()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "finalization timed out") {
		t.Fatalf("Stop deadline excluded pending setup: %v", result.Err)
	}
	emptyEndpoint := websocketTestServer(t, func(ctx context.Context, conn *websocket.Conn) {
		_, _ = readTestJSON(ctx, conn)
		_ = writeRealtime(ctx, conn, map[string]any{"type": "session.updated"})
		if event, err := readTestJSON(ctx, conn); err == nil {
			t.Errorf("empty capture sent an event: %v", event["type"])
		}
	})
	empty := cloudTestClient().startCloudStreamAsyncAt(context.Background(), emptyEndpoint, time.Second, time.Second)
	if result := empty.Finish(); result.Err == nil {
		t.Fatal("empty capture committed successfully")
	}
}

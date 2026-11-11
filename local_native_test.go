package voicetype

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLocalNativeModelTranscribesOffline(t *testing.T) {
	if os.Getenv("VOICE_TYPE_TEST_LOCAL_INFERENCE") != "1" {
		t.Skip("set VOICE_TYPE_TEST_LOCAL_INFERENCE=1 to download a model and run real CPU inference")
	}
	audioPath := os.Getenv("VOICE_TYPE_TEST_LOCAL_AUDIO")
	if audioPath == "" {
		t.Fatal("VOICE_TYPE_TEST_LOCAL_AUDIO must name a 16 kHz mono PCM16 WAV")
	}
	modelID := os.Getenv("VOICE_TYPE_TEST_LOCAL_MODEL")
	if modelID == "" {
		modelID = localModelUnifiedEnglish
	}
	if _, ok := localModelByID(modelID); !ok {
		t.Fatalf("VOICE_TYPE_TEST_LOCAL_MODEL names unsupported model %q", modelID)
	}
	wav, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	samples, err := decodeWAVPCM16(wav)
	if err != nil {
		t.Fatal(err)
	}

	modelDirectory := os.Getenv("VOICE_TYPE_TEST_LOCAL_MODEL_DIR")
	if modelDirectory == "" {
		modelDirectory = t.TempDir()
	}
	manager := newLocalModelManagerAt(modelDirectory)
	defer manager.Close()
	definition, _ := localModelByID(modelID)
	if manager.inspect(definition).State != localModelReady {
		if err := manager.Install(modelID); err != nil {
			t.Fatal(err)
		}
		waitForModelDownloadTimeout(t, manager, modelID, 20*time.Minute)
	}
	if status := manager.statusForTest(modelID); status.State != localModelReady {
		t.Fatalf("verified model did not install: %+v", status)
	}
	use, err := manager.acquire(context.Background(), modelID)
	if err != nil {
		t.Fatal(err)
	}
	defer use.close()
	if use.native.backend == "" {
		t.Fatal("native engine did not report the selected execution backend")
	}
	text, err := use.native.transcribe(context.Background(), samples, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		t.Fatal("offline model returned an empty transcript")
	}
	t.Logf("offline %s inference succeeded using %s (%d transcript characters)", modelID, use.native.backend, len(text))
	if !localModelSupportsStreaming(modelID) {
		return
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := use.native.beginStream(streamCtx, nil)
	if err != nil {
		t.Fatalf("start verified Unified English stream: %v", err)
	}
	defer stream.close()
	committed := ""
	for start := 0; start < len(samples); start += 3200 {
		end := min(start+3200, len(samples))
		_, nextCommitted, err := stream.feed(streamCtx, samples[start:end])
		if err != nil {
			t.Fatalf("feed local stream at sample %d: %v", start, err)
		}
		if !strings.HasPrefix(nextCommitted, committed) {
			t.Fatalf("native committed prefix was rewritten: old=%q new=%q", committed, nextCommitted)
		}
		committed = nextCommitted
	}
	finalText, finalCommitted, err := stream.finalize(streamCtx)
	if err != nil {
		t.Fatalf("finalize local stream: %v", err)
	}
	if !strings.HasPrefix(finalCommitted, committed) || finalText == "" {
		t.Fatalf("invalid final streaming output: text=%q committed=%q previous=%q", finalText, finalCommitted, committed)
	}
	if finalText != finalCommitted {
		t.Fatal("native final transcript disagreed with committed text; application would reject this stream")
	}
	t.Logf("offline stream inference succeeded (%d transcript characters; %d committed)", len(finalText), len(finalCommitted))
}

func TestLocalNativePacedStreamingQueue(t *testing.T) {
	if os.Getenv("VOICE_TYPE_TEST_LOCAL_PACED") != "1" {
		t.Skip("set VOICE_TYPE_TEST_LOCAL_PACED=1 with cached models and synthetic WAV")
	}
	wav, err := os.ReadFile(os.Getenv("VOICE_TYPE_TEST_LOCAL_AUDIO"))
	if err != nil {
		t.Fatal(err)
	}
	samples, err := decodeWAVPCM16(wav)
	if err != nil {
		t.Fatal(err)
	}
	manager := newLocalModelManagerAt(os.Getenv("VOICE_TYPE_TEST_LOCAL_MODEL_DIR"))
	defer manager.Close()
	use, err := manager.acquire(context.Background(), localModelUnifiedEnglish)
	if err != nil {
		t.Fatal(err)
	}
	defer use.close()
	inserter := &recordingInserter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	native, err := use.native.beginStream(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream := newLocalStreamSession(ctx, streamingModeLive, native, func(context.Context) (TextInserter, error) { return inserter, nil })
	defer stream.Abort()
	start := time.Now()
	for position := 0; position < len(samples); position += 320 {
		stream.OfferSamples(samples[position:min(position+320, len(samples))])
		// Match the production microphone's 20 ms cadence without accumulating
		// scheduler drift or making a faster machine appear necessary.
		target := start.Add(time.Duration(min(position+320, len(samples))) * time.Second / wavSampleRate)
		if remaining := time.Until(target); remaining > 0 {
			time.Sleep(remaining)
		}
	}
	beforeStop := len(inserter.inserted())
	stop := time.Now()
	result := stream.Finish()
	if result.Err != nil {
		t.Fatalf("production streaming queue failed at microphone cadence: %v", result.Err)
	}
	if result.Text == "" || result.Text != result.Committed || result.Text != result.Inserted {
		t.Fatal("paced stream failed application transcript/commit/insertion conditions")
	}
	if beforeStop == 0 {
		t.Fatal("no committed live text was submitted before Stop")
	}
	t.Logf("paced CPU streaming: %d characters submitted before Stop; Stop-to-finish %dms", beforeStop, time.Since(stop).Milliseconds())
}

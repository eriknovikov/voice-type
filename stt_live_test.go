package main

import (
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// TestLiveTranscribe exercises the real OpenRouter endpoint. It is skipped
// unless VOICE_TYPE_LIVE=1 and an API key are set, so `go test ./...` stays
// hermetic. Point VOICE_TYPE_PCM at a headerless 16 kHz mono s16le file.
func TestLiveTranscribe(t *testing.T) {
	if os.Getenv("VOICE_TYPE_LIVE") != "1" {
		t.Skip("set VOICE_TYPE_LIVE=1 to run the live OpenRouter test")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	pcmPath := os.Getenv("VOICE_TYPE_PCM")
	if pcmPath == "" {
		t.Skip("set VOICE_TYPE_PCM to a 16 kHz mono s16le file")
	}

	raw, err := os.ReadFile(pcmPath)
	if err != nil {
		t.Fatalf("read pcm: %v", err)
	}
	samples := make([]int16, len(raw)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
	}

	wav := wavEncode(samples)
	t.Logf("audio: %.2fs, wav %d bytes", wavDurationSeconds(samples), len(wav))

	model := os.Getenv("VOICE_TYPE_MODEL")
	if model == "" {
		model = defaultModel
	}

	for _, mode := range []struct {
		name    string
		useJSON bool
	}{
		{"multipart", false},
		{"json-base64", true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			c := newSTTClient(key, model)
			c.useJSON = mode.useJSON

			t0 := time.Now()
			res, err := c.Transcribe(wav)
			elapsed := time.Since(t0)
			if err != nil {
				t.Fatalf("transcribe: %v", err)
			}

			t.Logf("latency: %v", elapsed.Round(time.Millisecond))
			t.Logf("billed:  %.0fs  $%.6f", res.Seconds, res.Cost)
			t.Logf("text:    %q", res.Text)

			if res.Text == "" {
				t.Error("empty transcript")
			}
		})
	}
}

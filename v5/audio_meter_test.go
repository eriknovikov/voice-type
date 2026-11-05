package voicetype

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func meterTone(amplitude float64, count int, offset float64) []int16 {
	samples := make([]int16, count)
	for i := range samples {
		samples[i] = int16(offset + amplitude*math.Sin(2*math.Pi*200*float64(i)/wavSampleRate))
	}
	return samples
}
func TestMeterRMSAndPacketGaps(t *testing.T) {
	recorder := &Recorder{meterOnly: true}
	recorder.acceptSamples(meterTone(16384, wavSampleRate/10, 0))
	want := .5 / math.Sqrt2
	if got := recorder.Level(); math.Abs(got-want) > .0001 {
		t.Fatalf("RMS %v, want %v", got, want)
	}
	if recorder.TakeLevel() != recorder.Level() {
		t.Fatal("reading consumed the signal")
	}
	first := recorder.Level()
	recorder.acceptSamples(nil)
	if recorder.Level() != first {
		t.Fatal("a gap between packets was rendered as silence")
	}
	recorder.acceptSamples(make([]int16, wavSampleRate/10))
	if recorder.Level() != 0 {
		t.Fatal("recorded silence did not clear meter")
	}
	recorder.acceptSamples(meterTone(1000, wavSampleRate/10, 5000))
	if math.Abs(recorder.Level()-1000/math.Sqrt2/32768) > .0001 {
		t.Fatal("DC offset inflated meter")
	}
	recorder.meterAt = time.Now().Add(-time.Second)
	if recorder.Level() != 0 {
		t.Fatal("stalled source did not reset")
	}
	if len(recorder.samples) != 0 || len(recorder.meterWindow) > wavSampleRate/10 {
		t.Fatal("meter memory is unbounded")
	}
	ordinary := &Recorder{}
	ordinary.acceptSamples([]int16{1, -2, 3})
	if len(ordinary.samples) != 3 {
		t.Fatal("dictation samples lost")
	}
}
func TestMeterFollowsTwoSecondVolumeRamp(t *testing.T) {
	recorder := &Recorder{meterOnly: true}
	previous := float64(-1)
	// 100 packets of 20 ms: the same increasing input, regardless of UI polling.
	for frame := 1; frame <= 100; frame++ {
		recorder.acceptSamples(meterTone(float64(frame)*160, wavSampleRate/50, 0))
		value := recorder.Level()
		if value <= previous {
			t.Fatalf("increasing input fell at frame %d: %f -> %f", frame, previous, value)
		}
		if recorder.Level() != value {
			t.Fatal("second subscriber drained the meter")
		}
		previous = value
	}
	if previous < .3 {
		t.Fatal("loud end of ramp was suppressed")
	}
}
func TestMicrophoneLevelStreamLifecycle(t *testing.T) {
	d := newDaemon(configDefaults(), &resources{}, sttSettings{}, "")
	server := httptest.NewServer(d.managementRoutes())
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/microphone-levels")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 409 {
		t.Fatal("stream opened without test")
	}
	recorder := &Recorder{meterOnly: true}
	recorder.acceptSamples(meterTone(16384, wavSampleRate/10, 0))
	d.mu.Lock()
	d.testRecorder = recorder
	d.state = stateTesting
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request := httptest.NewRequest("GET", server.URL+"/microphone-levels", nil).WithContext(ctx)
	// A client request must not carry httptest's server-only RequestURI.
	request.RequestURI = ""
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	var frame struct {
		Level   float64 `json:"level"`
		Testing bool    `json:"testing"`
	}
	if err := decoder.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	if math.Abs(frame.Level-.5/math.Sqrt2) > .0001 || !frame.Testing {
		t.Fatalf("initial level: %+v", frame)
	}
	if err := decoder.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	if math.Abs(frame.Level-.5/math.Sqrt2) > .0001 || !frame.Testing {
		t.Fatalf("packet gap lost level: %+v", frame)
	}
	recorder.acceptSamples(make([]int16, wavSampleRate/10))
	for frame.Level != 0 {
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
	}
	d.stopMicrophoneTest()
	for frame.Testing {
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
	}
	if frame.Level != 0 {
		t.Fatal("meter not reset at end")
	}
	if err := decoder.Decode(&frame); err != io.EOF {
		t.Fatalf("stream did not close: %v", err)
	}
}

// Opt-in: captures levels locally for less than a second; no audio is stored.
func TestRealMicrophoneLevelStream(t *testing.T) {
	if os.Getenv("VOICE_TYPE_TEST_REAL_AUDIO") != "1" {
		t.Skip("set VOICE_TYPE_TEST_REAL_AUDIO=1 to verify real audio levels")
	}
	d := newDaemon(configDefaults(), &resources{}, sttSettings{}, "")
	if err := d.startMicrophoneTest(""); err != nil {
		t.Fatal(err)
	}
	defer d.stopMicrophoneTest()
	server := httptest.NewServer(d.managementRoutes())
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	response, err := client.Get(server.URL + "/microphone-levels")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(response.Body)
	started := time.Now()
	for i := 0; i < 20; i++ {
		var frame struct {
			Level   float64 `json:"level"`
			Testing bool    `json:"testing"`
		}
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if !frame.Testing || frame.Level < 0 || frame.Level > 1 {
			t.Fatalf("invalid meter frame: %+v", frame)
		}
	}
	elapsed := time.Since(started)
	if elapsed > time.Second {
		t.Fatalf("level stream too slow: %v for 20 frames", elapsed)
	}
	t.Logf("received 20 real audio meter frames in %v", elapsed.Round(time.Millisecond))
}

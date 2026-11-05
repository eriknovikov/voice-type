package voicetype

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestMeterKeepsTransientPeaksWithoutStoringAudio(t *testing.T) {
	recorder := &Recorder{meterOnly: true}
	recorder.acceptSamples([]int16{-16384, 0, 3277})
	recorder.acceptSamples([]int16{0, 0})
	if level := recorder.TakeLevel(); level != .5 {
		t.Fatalf("transient peak lost: %v", level)
	}
	if level := recorder.TakeLevel(); level != 0 {
		t.Fatalf("consumed peak persisted: %v", level)
	}
	recorder.acceptSamples([]int16{-32768})
	if level := recorder.TakeLevel(); level != 1 {
		t.Fatalf("negative full scale: %v", level)
	}
	if len(recorder.samples) != 0 {
		t.Fatal("meter retained audio")
	}
	ordinary := &Recorder{}
	ordinary.acceptSamples([]int16{1, -2, 3})
	if len(ordinary.samples) != 3 {
		t.Fatal("dictation samples lost")
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
	recorder.acceptSamples([]int16{16384})
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
	if frame.Level != .5 || !frame.Testing {
		t.Fatalf("initial level: %+v", frame)
	}
	if err := decoder.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	if frame.Level != 0 || !frame.Testing {
		t.Fatalf("silence level: %+v", frame)
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

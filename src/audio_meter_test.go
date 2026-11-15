package voicetype

import (
	"math"
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

// The Wails event stream reads this same non-consuming engine snapshot.
func TestMicrophoneLevelsFollowEngineLifecycle(t *testing.T) {
	d := newEngine(configDefaults(), nil, sttSettings{})
	if status := d.Status(); status.State != "idle" || status.Level != 0 {
		t.Fatal(status)
	}
	recorder := &Recorder{meterOnly: true}
	recorder.acceptSamples(meterTone(16384, wavSampleRate/10, 0))
	d.testRecorder, d.state = recorder, stateTesting
	for range 2 {
		status := d.Status()
		if status.State != "testing" || math.Abs(status.Level-.5/math.Sqrt2) > .0001 {
			t.Fatal(status)
		}
	}
	recorder.acceptSamples(make([]int16, wavSampleRate/10))
	if d.Status().Level != 0 {
		t.Fatal("silence did not reset meter")
	}
	d.StopMicrophoneTest()
	if status := d.Status(); status.State != "idle" || status.Level != 0 {
		t.Fatal("test leaked", status)
	}
}

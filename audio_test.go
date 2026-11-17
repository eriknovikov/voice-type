package voicetype

import (
	"errors"
	"reflect"
	"testing"
)

type testCaptureDevice struct {
	startFn func(string, func([]int16) (int, error)) (bool, error)
	stopFn  func() error
	closed  int
}

func (d *testCaptureDevice) start(id string, samples func([]int16) (int, error)) (bool, error) {
	return d.startFn(id, samples)
}
func (d *testCaptureDevice) stop() error {
	if d.stopFn != nil {
		return d.stopFn()
	}
	return nil
}
func (d *testCaptureDevice) close() { d.closed++ }

func TestRecorderCaptureLifecycle(t *testing.T) {
	var callback func([]int16) (int, error)
	device := &testCaptureDevice{startFn: func(id string, samples func([]int16) (int, error)) (bool, error) {
		if id != "microphone-uid" {
			t.Fatal("source not forwarded", id)
		}
		callback = samples
		_, err := samples([]int16{1, 2}) // A synchronous callback must not deadlock Start.
		return false, err
	}}
	recorder := &Recorder{device: device}
	recorder.SetSource("microphone-uid")
	for range 2 {
		if err := recorder.Start(); err != nil {
			t.Fatal(err)
		}
		if err := recorder.Start(); err == nil {
			t.Fatal("concurrent capture accepted")
		}
		device.stopFn = func() error {
			_, err := callback([]int16{3}) // Stop must drain callbacks before detaching samples.
			return err
		}
		samples, err := recorder.Stop()
		if err != nil || !reflect.DeepEqual(samples, []int16{1, 2, 3}) {
			t.Fatalf("capture = %v, %v", samples, err)
		}
		device.stopFn = nil
	}
	recorder.Close()
	recorder.Close()
	if device.closed != 1 || recorder.device != nil || recorder.recording {
		t.Fatal("device not closed exactly once")
	}
}

func TestRecorderCaptureErrorsAndFallback(t *testing.T) {
	want := errors.New("capture failed")
	device := &testCaptureDevice{startFn: func(string, func([]int16) (int, error)) (bool, error) {
		return true, want
	}}
	recorder := &Recorder{device: device, sourceID: "missing"}
	if err := recorder.Start(); !errors.Is(err, want) || recorder.recording {
		t.Fatalf("start failure lost: %v", err)
	}
	if !recorder.sourceFellBack() || recorder.sourceID != "" {
		t.Fatal("fallback not reflected in recorder state")
	}
	device.stopFn = func() error { return want }
	_, _ = recorder.acceptSamples([]int16{7})
	samples, err := recorder.Stop()
	if !errors.Is(err, want) || !reflect.DeepEqual(samples, []int16{7}) {
		t.Fatalf("stop discarded error or recovery samples: %v, %v", samples, err)
	}
	recorder.Close()
}

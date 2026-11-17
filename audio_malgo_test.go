//go:build darwin || malgo_test

package voicetype

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gen2brain/malgo"
)

func TestMalgoNullCaptureContract(t *testing.T) {
	capture, err := newMalgoCapture([]malgo.Backend{malgo.BackendNull})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &Recorder{device: capture}
	t.Cleanup(recorder.Close)
	devices, err := capture.context.Devices(malgo.Capture)
	if err != nil || len(devices) == 0 {
		t.Fatalf("null capture enumeration: %v, %v", devices, err)
	}
	id := devices[0].ID.String()
	parsed, err := malgo.ParseDeviceID(id)
	if err != nil || parsed != devices[0].ID {
		t.Fatal("capture ID did not round-trip", err)
	}
	recorder.SetSource(id)
	for range 2 {
		chunks := make(chan []int16, 4)
		recorder.SetChunkSink(func(samples []int16) {
			select {
			case chunks <- samples:
			default:
			}
		})
		if err := recorder.Start(); err != nil {
			t.Fatal(err)
		}
		select {
		case chunk := <-chunks:
			if len(chunk) != 320 {
				t.Fatalf("20ms @16kHz mono = %d samples, want 320", len(chunk))
			}
			chunk[0] = 1234
		case <-time.After(3 * time.Second):
			t.Fatal("null capture delivered no audio")
		}
		samples, err := recorder.Stop()
		if err != nil || len(samples) < 320 || samples[0] != 0 {
			t.Fatalf("capture ownership/stop: %d samples, %v", len(samples), err)
		}
		if capture.device != nil {
			t.Fatal("capture device leaked after Stop")
		}
	}
	recorder.Close()
	if capture.context != nil {
		t.Fatal("native context leaked after Close")
	}
	capture.close()
}

func TestMalgoSourceSelectionRequiresSuccessfulEnumeration(t *testing.T) {
	id := malgo.DeviceID{1, 2, 3}
	listed, persisted := 0, 0
	list := func() ([]malgo.DeviceInfo, error) {
		listed++
		return []malgo.DeviceInfo{{ID: id}}, nil
	}
	missing := func(string) (bool, error) { persisted++; return true, nil }
	if selected, fallback, err := resolveMalgoSource("", list, missing); err != nil || selected != nil || fallback || listed != 0 {
		t.Fatal("default did not use nil selection", err)
	}
	if selected, fallback, err := resolveMalgoSource(id.String(), list, missing); err != nil || selected == nil || *selected != id || fallback || persisted != 0 {
		t.Fatal("saved stable ID not selected", err)
	}
	want := errors.New("enumeration failed")
	if _, fallback, err := resolveMalgoSource("absent", func() ([]malgo.DeviceInfo, error) { return nil, want }, missing); !errors.Is(err, want) || fallback || persisted != 0 {
		t.Fatal("failed enumeration cleared saved microphone", err)
	}
	if selected, fallback, err := resolveMalgoSource("absent", list, missing); err != nil || selected != nil || !fallback || persisted != 1 {
		t.Fatal("missing device did not fall back", err)
	}
	if _, fallback, err := resolveMalgoSource("absent", list, func(string) (bool, error) { return false, want }); !errors.Is(err, want) || fallback {
		t.Fatal("failed settings save silently switched devices", err)
	}
}

func TestMalgoCallbackOwnershipAndErrors(t *testing.T) {
	capture := &malgoCapture{}
	input := []byte{0xff, 0x7f, 0, 0x80}
	var owned []int16
	capture.recordFrames(func(samples []int16) (int, error) { owned = samples; return len(samples), nil }, input, 2)
	input[0] = 0
	if len(owned) != 2 || owned[0] != 32767 || owned[1] != -32768 {
		t.Fatal("PCM16 decoding retained borrowed bytes", owned)
	}
	capture.recordFrames(func([]int16) (int, error) { t.Fatal("truncated callback reached sink"); return 0, nil }, input, 3)
	if err := capture.captureError(); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatal("malformed capture was silently accepted", err)
	}
	capture.onStopped()
	if err := capture.captureError(); !strings.Contains(err.Error(), "unexpectedly") {
		t.Fatal("device loss was not recorded", err)
	}
	capture.intentionalStop.Store(true)
	capture.unexpectedStop.Store(false)
	capture.onStopped()
	if capture.unexpectedStop.Load() {
		t.Fatal("intentional Stop reported device loss")
	}
	failure := &malgoCapture{}
	want := errors.New("session buffer failed")
	failure.recordFrames(func([]int16) (int, error) { return 0, want }, input, 2)
	if !errors.Is(failure.captureError(), want) {
		t.Fatal("callback sink failure was lost")
	}
}

func TestMalgoPermissionFailureDoesNotOpenDevice(t *testing.T) {
	want := errors.New("microphone permission revoked")
	capture := &malgoCapture{permission: func() error { return want }}
	if _, err := capture.start("", func([]int16) (int, error) { t.Fatal("captured without permission"); return 0, nil }); !errors.Is(err, want) {
		t.Fatal("permission failure bypassed", err)
	}
	if capture.context != nil || capture.device != nil {
		t.Fatal("permission failure opened native resources")
	}
}

func TestMalgoStopRetainsAudioWhenPermissionRevoked(t *testing.T) {
	capture, err := newMalgoCapture([]malgo.Backend{malgo.BackendNull})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &Recorder{device: capture}
	defer recorder.Close()
	chunks := make(chan struct{}, 1)
	recorder.SetChunkSink(func([]int16) {
		select {
		case chunks <- struct{}{}:
		default:
		}
	})
	if err := recorder.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-chunks:
	case <-time.After(3 * time.Second):
		t.Fatal("null capture delivered no audio")
	}
	want := errors.New("microphone permission revoked")
	capture.permission = func() error { return want }
	samples, err := recorder.Stop()
	if !errors.Is(err, want) || len(samples) == 0 || capture.device != nil {
		t.Fatalf("revoked permission lost recovery audio or leaked capture: %d, %v", len(samples), err)
	}
}

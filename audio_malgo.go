//go:build darwin || malgo_test

package voicetype

// #include <stdlib.h>
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/gen2brain/malgo"
)

type captureFailure struct{ err error }

type malgoCapture struct {
	context         *malgo.AllocatedContext
	device          *malgo.Device
	permission      func() error
	intentionalStop atomic.Bool
	unexpectedStop  atomic.Bool
	failure         atomic.Pointer[captureFailure]
}

func newMalgoCapture(backends []malgo.Backend) (*malgoCapture, error) {
	context, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("initialize audio capture: %w", err)
	}
	return &malgoCapture{context: context}, nil
}

func (c *malgoCapture) start(sourceID string, samples func([]int16) (int, error)) (bool, error) {
	if c.permission != nil {
		if err := c.permission(); err != nil {
			return false, err
		}
	}
	if c.context == nil {
		return false, errors.New("audio capture is closed")
	}
	if c.device != nil {
		return false, errors.New("already recording")
	}
	id, fellBack, err := resolveMalgoSource(sourceID, func() ([]malgo.DeviceInfo, error) {
		return c.context.Devices(malgo.Capture)
	}, persistMissingMicrophone)
	if err != nil {
		return fellBack, err
	}
	config := malgo.DefaultDeviceConfig(malgo.Capture)
	config.Capture.Format = malgo.FormatS16
	config.Capture.Channels = 1
	config.SampleRate = wavSampleRate
	config.PeriodSizeInMilliseconds = 20
	if id != nil {
		config.Capture.DeviceID = id.Pointer()
		defer C.free(config.Capture.DeviceID)
	}
	c.intentionalStop.Store(false)
	c.unexpectedStop.Store(false)
	c.failure.Store(nil)
	device, err := malgo.InitDevice(c.context.Context, config, malgo.DeviceCallbacks{
		Data: func(_ []byte, input []byte, frames uint32) { c.recordFrames(samples, input, frames) },
		Stop: c.onStopped,
	})
	if err != nil {
		return fellBack, fmt.Errorf("open capture device: %w", err)
	}
	if device.CaptureFormat() != malgo.FormatS16 || device.CaptureChannels() != 1 || device.SampleRate() != wavSampleRate {
		c.intentionalStop.Store(true)
		device.Uninit()
		return fellBack, errors.New("capture device did not provide 16 kHz mono PCM16")
	}
	if err := device.Start(); err != nil {
		c.intentionalStop.Store(true)
		device.Uninit()
		return fellBack, fmt.Errorf("start capture device: %w", err)
	}
	c.device = device
	return fellBack, nil
}

func (c *malgoCapture) recordFrames(sink func([]int16) (int, error), input []byte, frames uint32) {
	if uint64(frames)*2 > uint64(len(input)) {
		c.failure.CompareAndSwap(nil, &captureFailure{errors.New("audio capture returned a truncated PCM16 packet")})
		return
	}
	// Callback bytes are borrowed C memory. Decode before returning; neither
	// recorder buffering nor streaming consumers retain that native memory.
	samples := make([]int16, int(frames))
	for index := range samples {
		samples[index] = int16(binary.LittleEndian.Uint16(input[index*2:]))
	}
	if _, err := sink(samples); err != nil {
		c.failure.CompareAndSwap(nil, &captureFailure{err})
	}
}

func (c *malgoCapture) onStopped() {
	if !c.intentionalStop.Load() {
		c.unexpectedStop.Store(true)
	}
}

func (c *malgoCapture) captureError() error {
	var err error
	if failure := c.failure.Load(); failure != nil {
		err = failure.err
	}
	if c.unexpectedStop.Load() {
		err = errors.Join(err, errors.New("audio capture stopped unexpectedly; check the microphone and its permission"))
	}
	return err
}

func (c *malgoCapture) stop() error {
	if c.device == nil {
		return nil
	}
	c.intentionalStop.Store(true)
	err := c.device.Stop()
	c.device.Uninit() // Drains callbacks even if stopping failed.
	c.device = nil
	if c.permission != nil {
		err = errors.Join(err, c.permission())
	}
	return errors.Join(err, c.captureError())
}

func (c *malgoCapture) close() {
	_ = c.stop()
	if c.context != nil {
		_ = c.context.Uninit()
		c.context.Free()
		c.context = nil
	}
}

func resolveMalgoSource(sourceID string, list func() ([]malgo.DeviceInfo, error), missing func(string) (bool, error)) (*malgo.DeviceID, bool, error) {
	if sourceID == "" {
		return nil, false, nil // nil selects the OS default, not the first device.
	}
	devices, err := list()
	if err != nil {
		return nil, false, fmt.Errorf("enumerate capture devices: %w", err)
	}
	for _, device := range devices {
		if device.ID.String() == sourceID {
			id := device.ID
			return &id, false, nil
		}
	}
	if _, err := missing(sourceID); err != nil {
		return nil, false, fmt.Errorf("save system default microphone: %w", err)
	}
	return nil, true, nil
}

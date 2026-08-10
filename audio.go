package main

import (
	"fmt"
	"sync"

	"github.com/jfreymuth/pulse"
)

// Recorder owns the PulseAudio connection. The client is created once at daemon
// start (measured ~3 ms) and reused; only the record stream is per-session
// (~13 ms setup, ~87 ms to the first sample on a cold device).
//
// Two locks on purpose: streamMu guards the stream lifecycle, bufMu guards the
// sample buffer that the pulse library's own goroutine writes into. Sharing one
// lock would make Start() block its own write callback.
type Recorder struct {
	client *pulse.Client

	streamMu sync.Mutex
	stream   *pulse.RecordStream

	bufMu   sync.Mutex
	samples []int16
}

func newRecorder() (*Recorder, error) {
	client, err := pulse.NewClient(pulse.ClientApplicationName("voice-type"))
	if err != nil {
		return nil, fmt.Errorf("connect to PulseAudio: %w", err)
	}
	return &Recorder{client: client}, nil
}

func (r *Recorder) Close() {
	r.streamMu.Lock()
	if r.stream != nil {
		r.stream.Stop()
		r.stream.Close()
		r.stream = nil
	}
	r.streamMu.Unlock()

	if r.client != nil {
		r.client.Close()
		r.client = nil
	}
}

// Start opens a fresh 16 kHz mono stream and begins appending samples.
func (r *Recorder) Start() error {
	r.streamMu.Lock()
	defer r.streamMu.Unlock()

	if r.stream != nil {
		return fmt.Errorf("already recording")
	}

	r.bufMu.Lock()
	r.samples = r.samples[:0]
	r.bufMu.Unlock()

	stream, err := r.client.NewRecord(
		pulse.Int16Writer(func(b []int16) (int, error) {
			r.bufMu.Lock()
			r.samples = append(r.samples, b...)
			r.bufMu.Unlock()
			return len(b), nil
		}),
		pulse.RecordMono,
		pulse.RecordSampleRate(wavSampleRate),
		pulse.RecordLatency(0.02),
		pulse.RecordMediaName("voice-type dictation"),
	)
	if err != nil {
		return fmt.Errorf("open record stream: %w", err)
	}

	r.stream = stream
	stream.Start()
	return nil
}

// Stop ends capture and returns everything recorded during the session.
func (r *Recorder) Stop() []int16 {
	r.streamMu.Lock()
	stream := r.stream
	r.stream = nil
	r.streamMu.Unlock()

	if stream != nil {
		stream.Stop()
		stream.Close()
	}

	r.bufMu.Lock()
	defer r.bufMu.Unlock()
	out := make([]int16, len(r.samples))
	copy(out, r.samples)
	r.samples = r.samples[:0]
	return out
}

// SampleCount is the live length of the capture buffer, used for the duration cap.
func (r *Recorder) SampleCount() int {
	r.bufMu.Lock()
	defer r.bufMu.Unlock()
	return len(r.samples)
}

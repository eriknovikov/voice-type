package voicetype

import (
	"fmt"
	"math"
	"sync"
	"time"

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

	bufMu     sync.Mutex
	samples   []int16
	sourceID  string
	level     float64
	meterOnly bool
}

type audioRecorder interface {
	Start() error
	Stop() ([]int16, error)
	Close()
}

func NewRecorder() (*Recorder, error) {
	client, err := pulse.NewClient(
		pulse.ClientApplicationName("voice-type"),
		pulse.ClientTimeout(time.Second),
	)
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

	if r.client == nil {
		client, err := pulse.NewClient(pulse.ClientApplicationName("voice-type"), pulse.ClientTimeout(time.Second))
		if err != nil {
			return fmt.Errorf("connect to audio server: %w", err)
		}
		r.client = client
	}
	source, err := r.client.DefaultSource()
	if r.sourceID != "" {
		source, err = r.client.SourceByID(r.sourceID)
	}
	if err != nil {
		return fmt.Errorf("resolve default source: %w", err)
	}

	stream, err := r.client.NewRecord(
		pulse.Int16Writer(func(b []int16) (int, error) {
			r.bufMu.Lock()
			var peak float64
			for _, sample := range b {
				value := math.Abs(float64(sample)) / 32768
				if value > peak {
					peak = value
				}
			}
			r.level = peak
			if !r.meterOnly {
				r.samples = append(r.samples, b...)
			}
			r.bufMu.Unlock()
			return len(b), nil
		}),
		pulse.RecordMono,
		pulse.RecordSampleRate(wavSampleRate),
		pulse.RecordLatency(0.02),
		pulse.RecordMediaName("voice-type dictation"),
		pulse.RecordSource(source),
	)
	if err != nil {
		return fmt.Errorf("open record stream: %w", err)
	}

	r.stream = stream
	stream.Start()
	logf("AUDIO", "recording from %s (%s)", source.Name(), source.ID())
	return nil
}

// Stop ends capture and returns everything recorded during the session.
func (r *Recorder) Stop() ([]int16, error) {
	r.streamMu.Lock()
	stream := r.stream
	r.stream = nil
	r.streamMu.Unlock()

	var streamErr error
	if stream != nil {
		stream.Stop()
		streamErr = stream.Error()
		stream.Close()
	}

	r.bufMu.Lock()
	defer r.bufMu.Unlock()
	out := make([]int16, len(r.samples))
	copy(out, r.samples)
	r.samples = r.samples[:0]
	if streamErr != nil {
		streamErr = fmt.Errorf("record stream: %w", streamErr)
	}
	return out, streamErr
}

type AudioSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func ListAudioSources() ([]AudioSource, error) {
	r, err := NewRecorder()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	sources, err := r.client.ListSources()
	if err != nil {
		return nil, err
	}
	out := []AudioSource{}
	for _, source := range sources {
		out = append(out, AudioSource{source.ID(), source.Name()})
	}
	return out, nil
}
func (r *Recorder) SetSource(id string) {
	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	r.sourceID = id
}
func (r *Recorder) Level() float64 { r.bufMu.Lock(); defer r.bufMu.Unlock(); return r.level }

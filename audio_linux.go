package voicetype

import (
	"errors"
	"fmt"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

type pulseCapture struct {
	client *pulse.Client
	stream *pulse.RecordStream
}

func newCaptureDevice() (*pulseCapture, error) {
	client, err := pulse.NewClient(pulse.ClientApplicationName("voice-type"), pulse.ClientTimeout(time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect to PulseAudio: %w", err)
	}
	return &pulseCapture{client: client}, nil
}

func (p *pulseCapture) start(sourceID string, samples func([]int16) (int, error)) (bool, error) {
	var source *pulse.Source
	var err error
	fellBack := false
	if sourceID != "" {
		source, err = p.client.SourceByID(sourceID)
		if errors.Is(err, proto.ErrNoSuchEntity) {
			if _, saveErr := persistMissingMicrophone(sourceID); saveErr != nil {
				return false, fmt.Errorf("save system default microphone: %w", saveErr)
			}
			fellBack = true
			source, err = p.client.DefaultSource()
		}
	} else {
		source, err = p.client.DefaultSource()
	}
	if err != nil {
		return fellBack, fmt.Errorf("resolve default source: %w", err)
	}
	stream, err := p.client.NewRecord(
		pulse.Int16Writer(samples),
		pulse.RecordMono,
		pulse.RecordSampleRate(wavSampleRate),
		pulse.RecordLatency(0.02),
		pulse.RecordMediaName("voice-type dictation"),
		pulse.RecordSource(source),
	)
	if err != nil {
		return fellBack, fmt.Errorf("open record stream: %w", err)
	}
	p.stream = stream
	stream.Start()
	logf("AUDIO", "recording from %s (%s)", source.Name(), source.ID())
	return fellBack, nil
}

func (p *pulseCapture) stop() error {
	if p.stream == nil {
		return nil
	}
	p.stream.Stop()
	err := p.stream.Error()
	p.stream.Close()
	p.stream = nil
	return err
}

func (p *pulseCapture) close() { p.client.Close() }

func ListAudioSources() ([]AudioSource, error) {
	device, err := newCaptureDevice()
	if err != nil {
		return nil, err
	}
	defer device.close()
	sources, err := device.client.ListSources()
	if err != nil {
		return nil, err
	}
	out := []AudioSource{}
	for _, source := range sources {
		out = append(out, AudioSource{source.ID(), source.Name()})
	}
	return out, nil
}

package voicetype

import "sync"

// One capture-owned recording, retained until its uploader finishes. Reads copy
// only a bounded chunk; Freeze returns immutable storage without a second full
// recording. A later dictation always gets a fresh instance.
type sessionAudioBuffer struct {
	mu             sync.Mutex
	samples        []int16
	closed         bool
	acknowledged   bool
	startupSamples int
	sent           int
	wake           chan struct{}
}

func newSessionAudioBuffer() *sessionAudioBuffer {
	return &sessionAudioBuffer{wake: make(chan struct{}, 1)}
}
func (b *sessionAudioBuffer) append(samples []int16) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	// Allow one capture-shutdown fragment beyond the ten-minute timer, while
	// bounding memory if a recorder or duration timer misbehaves.
	if len(samples) > (maxDurationSeconds+1)*wavSampleRate-len(b.samples) {
		return cloudError("OpenAI recording exceeded its capture limit")
	}
	if b.acknowledged && len(b.samples)+len(samples)-max(b.sent, b.startupSamples) > cloudQueueSamples {
		return cloudError("OpenAI upload could not keep up: five-second audio queue exceeded")
	}
	b.samples = append(b.samples, samples...)
	b.notify()
	return nil
}
func (b *sessionAudioBuffer) notify() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *sessionAudioBuffer) acknowledge() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.acknowledged = true
	b.startupSamples = len(b.samples)
	return b.startupSamples
}
func (b *sessionAudioBuffer) read(offset int) ([]int16, bool) {
	samples, closed := b.readInto(nil, offset)
	return samples, closed
}

// readInto copies the next bounded chunk into dst so an uploader can reuse one
// sample buffer instead of allocating a copy per chunk.
func (b *sessionAudioBuffer) readInto(dst []int16, offset int) ([]int16, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	end := min(offset+3200, len(b.samples)) // 200 ms, independent of callback size
	chunk := b.samples[offset:end]
	if cap(dst) < len(chunk) {
		dst = make([]int16, len(chunk))
	} else {
		dst = dst[:len(chunk)]
	}
	copy(dst, chunk)
	return dst, b.closed
}
func (b *sessionAudioBuffer) advance(sent int) {
	b.mu.Lock()
	b.sent = sent
	b.mu.Unlock()
}
func (b *sessionAudioBuffer) freeze() []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.notify()
	return b.samples
}

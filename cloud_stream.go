package voicetype

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const openAIRealtimeURL = "wss://api.openai.com/v1/realtime?intent=transcription"
const cloudQueueSamples = 5 * wavSampleRate

// Capture owns the complete recording. Streams receive copies without waiting
// for inference, insertion, or the network on the microphone callback.
// OfferSamples takes ownership of the slice it receives; the recorder
// transfers the copy it made for its chunk sink.
type transcriptionStream interface {
	OfferSamples([]int16)
	Finish() localStreamResult
	Abort()
}

type streamTiming struct {
	ConnectionStarted         time.Time
	ConnectionReady           time.Time
	StartupBacklogDrained     time.Time
	FinalAudioSent            time.Time
	CommitSent                time.Time
	TranscriptReceived        time.Time
	InsertionSubmitted        time.Time
	InsertionCleanupCompleted time.Time
}

// Only fixed application messages enter this type, never provider payloads.
// It preserves a useful failure reason in routine logs without raw server text.
type cloudStreamError struct{ message string }

func (e *cloudStreamError) Error() string { return e.message }
func cloudError(message string) error     { return &cloudStreamError{message: message} }

type cloudStreamSession struct {
	parent          context.Context
	ctx             context.Context
	cancel          context.CancelFunc
	conn            *websocket.Conn // owned by run, then read/send until both workers finish
	audio           *sessionAudioBuffer
	ready           chan struct{}
	done            chan struct{}
	failed          chan struct{}
	failureOnce     sync.Once
	readerDone      chan struct{}
	writerDone      chan struct{}
	finalDone       chan struct{}
	finalOnce       sync.Once
	mu              sync.Mutex
	result          localStreamResult
	commitRequested bool
	itemID          string
	completed       map[string]string
	failedItems     map[string]bool
	finalTimeout    time.Duration
}

type realtimeEvent struct {
	Type       string  `json:"type"`
	ItemID     string  `json:"item_id"`
	Transcript *string `json:"transcript"`
}

func (c *sttClient) startCloudStreamAsyncAt(ctx context.Context, endpoint string, setupTimeout, finalTimeout time.Duration) *cloudStreamSession {
	streamCtx, cancel := context.WithCancel(ctx)
	s := &cloudStreamSession{parent: ctx, ctx: streamCtx, cancel: cancel, audio: newSessionAudioBuffer(),
		ready: make(chan struct{}), done: make(chan struct{}), failed: make(chan struct{}), readerDone: make(chan struct{}), writerDone: make(chan struct{}), finalDone: make(chan struct{}),
		completed: make(map[string]string), failedItems: make(map[string]bool), finalTimeout: finalTimeout}
	s.result.Timing.ConnectionStarted = time.Now()
	go s.run(c, endpoint, setupTimeout)
	return s
}
func (c *sttClient) configureCloudStream(ctx context.Context, endpoint string, setupDeadline time.Time) (*websocket.Conn, error) {
	setupCtx, setupCancel := context.WithDeadline(ctx, setupDeadline)
	defer setupCancel()
	header := http.Header{"Authorization": {"Bearer " + c.settings.APIKey}}
	conn, response, err := websocket.Dial(setupCtx, endpoint, &websocket.DialOptions{HTTPHeader: header, HTTPClient: c.http})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if response != nil {
			return nil, &sttError{Provider: providerOpenAI, Status: response.StatusCode}
		}
		return nil, cloudError("OpenAI WebSocket connection failed or timed out")
	}
	conn.SetReadLimit(1 << 20)
	fail := func() { _ = conn.CloseNow() }
	transcription := map[string]any{"model": defaultModelID}
	if len(c.settings.Vocabulary) > 0 {
		transcription["keywords"] = c.settings.Vocabulary
	}
	if len(c.settings.Languages) > 0 {
		transcription["languages"] = c.settings.Languages
	}
	update := map[string]any{"type": "session.update", "session": map[string]any{
		"type": "transcription", "audio": map[string]any{"input": map[string]any{
			"format":        map[string]any{"type": "audio/pcm", "rate": 24000},
			"transcription": transcription, "turn_detection": nil,
		}},
	}}
	if err = writeRealtime(setupCtx, conn, update); err != nil {
		fail()
		return nil, cloudError("OpenAI WebSocket configuration failed or timed out")
	}
	for {
		event, readErr := readRealtime(setupCtx, conn)
		if readErr != nil {
			fail()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, cloudError("OpenAI WebSocket configuration failed or timed out")
		}
		if event.Type == "error" {
			fail()
			return nil, cloudError("OpenAI rejected the transcription session configuration")
		}
		if event.Type == "session.updated" {
			break
		}
	}
	return conn, nil
}
func (s *cloudStreamSession) run(c *sttClient, endpoint string, setupTimeout time.Duration) {
	defer close(s.done)
	conn, err := c.configureCloudStream(s.ctx, endpoint, s.result.Timing.ConnectionStarted.Add(setupTimeout))
	if err != nil {
		s.failWithError(err)
		return
	}
	s.conn = conn
	defer conn.CloseNow()
	startupSamples := s.audio.acknowledge()
	s.mu.Lock()
	s.result.Timing.ConnectionReady = time.Now()
	s.mu.Unlock()
	close(s.ready)
	go s.receive()
	s.send(startupSamples)
	select {
	case <-s.finalDone:
	case <-s.ctx.Done():
	}
	s.cancel()
	_ = conn.CloseNow()
	<-s.readerDone
}

func writeRealtime(ctx context.Context, conn *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
func readRealtime(ctx context.Context, conn *websocket.Conn) (realtimeEvent, error) {
	kind, data, err := conn.Read(ctx)
	if err != nil {
		return realtimeEvent{}, err
	}
	var event realtimeEvent
	if kind != websocket.MessageText || json.Unmarshal(data, &event) != nil || event.Type == "" {
		return event, cloudError("malformed OpenAI WebSocket response")
	}
	return event, nil
}

func (s *cloudStreamSession) fail(message string) { s.failWithError(cloudError(message)) }
func (s *cloudStreamSession) failWithError(err error) {
	s.mu.Lock()
	if s.result.Err == nil {
		s.result.Err = err
	}
	s.mu.Unlock()
	s.failureOnce.Do(func() { close(s.failed) })
	s.cancel()
}
func (s *cloudStreamSession) OfferSamples(samples []int16) {
	if len(samples) == 0 || s.ctx.Err() != nil {
		return
	}
	if err := s.audio.append(samples); err != nil {
		s.failWithError(err)
	}
}
func (s *cloudStreamSession) closeInput() { s.audio.freeze() }

func (s *cloudStreamSession) send(startupSamples int) {
	defer close(s.writerDone)
	var resampler pcmResampler
	nonempty := false
	// Reused for the whole stream. Buffers are only recycled after the
	// synchronous WebSocket write returns.
	readBuf := make([]int16, 0, 3200)
	resampleBuf := make([]int16, 0, 4801)
	var pcmBuf []byte
	messageBuf := make([]byte, 0, len(realtimeAppendPrefix)+base64.StdEncoding.EncodedLen(2*4801)+len(realtimeAppendSuffix))
	appendAudio := func(samples []int16) bool {
		if len(samples) == 0 {
			return true
		}
		pcm, ok := pcm16Bytes(samples)
		if !ok {
			pcmBuf = pcm16ToBytes(pcmBuf[:0], samples)
			pcm = pcmBuf
		}
		messageBuf = appendRealtimeAudio(messageBuf[:0], pcm)
		if s.conn.Write(s.ctx, websocket.MessageText, messageBuf) != nil {
			s.fail("OpenAI WebSocket audio upload failed")
			return false
		}
		s.mu.Lock()
		s.result.Timing.FinalAudioSent = time.Now()
		s.mu.Unlock()
		return true
	}
	offset := 0
	backlogDrained := false
	for {
		if s.ctx.Err() != nil {
			return
		}
		if !backlogDrained && offset >= startupSamples {
			s.mu.Lock()
			s.result.Timing.StartupBacklogDrained = time.Now()
			s.mu.Unlock()
			backlogDrained = true
		}
		samples, closed := s.audio.readInto(readBuf, offset)
		if len(samples) > 0 {
			nonempty = true
			resampleBuf = resampler.ProcessInto(resampleBuf[:0], samples)
			if !appendAudio(resampleBuf) {
				return
			}
			offset += len(samples)
			s.audio.advance(offset)
			continue
		}
		if closed {
			if !nonempty {
				s.fail("no audio captured")
				return
			}
			resampleBuf = resampler.FlushInto(resampleBuf[:0])
			if !appendAudio(resampleBuf) {
				return
			}
			s.mu.Lock()
			s.commitRequested = true
			s.mu.Unlock()
			if s.conn.Write(s.ctx, websocket.MessageText, []byte(realtimeCommitMessage)) != nil {
				s.fail("OpenAI WebSocket commit failed")
				return
			}
			s.mu.Lock()
			s.result.Timing.CommitSent = time.Now()
			s.mu.Unlock()
			return
		}
		select {
		case <-s.ctx.Done():
			return
		case <-s.audio.wake:
		}
	}
}

const (
	realtimeAppendPrefix  = `{"type":"input_audio_buffer.append","audio":"`
	realtimeAppendSuffix  = `"}`
	realtimeCommitMessage = `{"type":"input_audio_buffer.commit"}`
)

// pcm16ToBytes appends little-endian PCM16 into dst so the uploader can reuse
// one byte buffer.
func pcm16ToBytes(dst []byte, samples []int16) []byte {
	need := 2 * len(samples)
	if cap(dst) < need {
		dst = make([]byte, need)
	} else {
		dst = dst[:need]
	}
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(dst[2*i:], uint16(sample))
	}
	return dst
}

// appendRealtimeAudio builds the fixed append message without JSON escaping or
// an intermediate base64 string. The base64 alphabet needs no JSON escaping.
func appendRealtimeAudio(dst, pcm []byte) []byte {
	dst = append(dst, realtimeAppendPrefix...)
	dst = base64.StdEncoding.AppendEncode(dst, pcm)
	return append(dst, realtimeAppendSuffix...)
}

func (s *cloudStreamSession) receive() {
	defer close(s.readerDone)
	for {
		event, err := readRealtime(s.ctx, s.conn)
		if err != nil {
			s.fail("OpenAI WebSocket disconnected or returned a malformed response")
			return
		}
		s.mu.Lock()
		switch event.Type {
		case "error":
			s.mu.Unlock()
			s.fail("OpenAI WebSocket server rejected the request")
			return
		case "input_audio_buffer.committed":
			if !s.commitRequested || event.ItemID == "" || (s.itemID != "" && s.itemID != event.ItemID) {
				s.mu.Unlock()
				s.fail("OpenAI WebSocket returned an unexpected commit acknowledgement")
				return
			}
			s.itemID = event.ItemID
		case "conversation.item.input_audio_transcription.completed":
			if event.ItemID == "" || event.Transcript == nil {
				s.mu.Unlock()
				s.fail("OpenAI WebSocket returned an invalid completed transcript")
				return
			}
			if _, exists := s.completed[event.ItemID]; !exists {
				if len(s.completed) >= 8 {
					s.mu.Unlock()
					s.fail("OpenAI WebSocket returned too many unrelated transcripts")
					return
				}
				s.completed[event.ItemID] = *event.Transcript
			}
		case "conversation.item.input_audio_transcription.failed":
			if event.ItemID == "" || len(s.failedItems) >= 8 {
				s.mu.Unlock()
				s.fail("OpenAI WebSocket returned an invalid failed transcript")
				return
			}
			s.failedItems[event.ItemID] = true
		}
		if s.itemID != "" && s.failedItems[s.itemID] {
			s.mu.Unlock()
			s.fail("OpenAI WebSocket transcription failed")
			return
		}
		if text, ok := s.completed[s.itemID]; ok && s.itemID != "" {
			s.result.Text = strings.TrimSpace(text)
			s.result.Timing.TranscriptReceived = time.Now()
			s.mu.Unlock()
			s.finalOnce.Do(func() { close(s.finalDone) })
			return
		}
		s.mu.Unlock()
	}
}
func (s *cloudStreamSession) Finish() localStreamResult {
	timer := time.AfterFunc(s.finalTimeout, func() {
		s.mu.Lock()
		received := !s.result.Timing.TranscriptReceived.IsZero()
		s.mu.Unlock()
		if !received {
			s.fail("OpenAI WebSocket finalization timed out")
		}
	})
	defer timer.Stop()
	s.closeInput()
	<-s.done
	s.mu.Lock()
	result := s.result
	s.mu.Unlock()
	if s.parent.Err() != nil {
		result.Err = s.parent.Err()
	}
	return result
}
func (s *cloudStreamSession) Abort() {
	if s == nil {
		return
	}
	s.cancel()
	s.closeInput()
	<-s.done
}

// A continuous 3:2 linear interpolator. Keep the fractional output position
// across capture chunks; duplicate the endpoint only once, when capture ends.
// Upsampling adds no new information; linear interpolation suppresses images
// while preserving the original 16 kHz capture and the API's 24 kHz format.
type pcmResampler struct {
	count, next int64
	previous    int16
}

func (r *pcmResampler) Process(samples []int16) []int16 {
	return r.ProcessInto(nil, samples)
}

// ProcessInto appends resampled output to dst. Callers that stream reuse dst
// across chunks to avoid per-chunk allocation.
func (r *pcmResampler) ProcessInto(dst []int16, samples []int16) []int16 {
	if cap(dst) < (len(samples)*3+1)/2 {
		dst = make([]int16, 0, (len(samples)*3+1)/2)
	}
	for _, sample := range samples {
		index := r.count
		if index == 0 {
			dst = append(dst, sample)
			r.next = 1
		} else {
			for r.next*2 <= index*3 {
				fraction := r.next*2 - (index-1)*3
				value := (int64(r.previous)*(3-fraction) + int64(sample)*fraction) / 3
				dst = append(dst, int16(value))
				r.next++
			}
		}
		r.previous = sample
		r.count++
	}
	return dst
}
func (r *pcmResampler) Flush() []int16 {
	return r.FlushInto(nil)
}

func (r *pcmResampler) FlushInto(dst []int16) []int16 {
	for r.next*2 < r.count*3 {
		dst = append(dst, r.previous)
		r.next++
	}
	return dst
}

package voicetype

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const localStreamQueueSize = 96

type localStreamEngine interface {
	feed(context.Context, []int16) (string, string, error)
	finalize(context.Context) (string, string, error)
	close()
}

type localInserterFactory func(context.Context) (TextInserter, error)

type localTranscriber struct {
	settings sttSettings
	models   *LocalModelManager
}

func (t *localTranscriber) transcribeWAV(ctx context.Context, wav []byte, observe sttAttemptObserver) (sttResult, error) {
	started := time.Now()
	samples, err := decodeWAVPCM16(wav)
	if err != nil {
		if observe != nil {
			observe(t.settings.Model, 1, time.Since(started), sttResult{}, err)
		}
		return sttResult{}, err
	}
	return t.transcribeSamples(ctx, samples, observe, nil)
}

func (t *localTranscriber) transcribeSamples(ctx context.Context, samples []int16, observe sttAttemptObserver, use *localModelUse) (sttResult, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		if observe != nil {
			observe(t.settings.Model, 1, time.Since(started), sttResult{}, err)
		}
		return sttResult{}, err
	}
	if len(samples) == 0 {
		err := errors.New("no audio samples to transcribe")
		if observe != nil {
			observe(t.settings.Model, 1, time.Since(started), sttResult{}, err)
		}
		return sttResult{}, err
	}
	ownedUse := false
	if use == nil {
		var err error
		use, err = t.models.acquire(ctx, t.settings.Model)
		if err != nil {
			if observe != nil {
				observe(t.settings.Model, 1, time.Since(started), sttResult{}, err)
			}
			return sttResult{}, err
		}
		ownedUse = true
	}
	if ownedUse {
		defer use.close()
	}
	text, err := use.native.transcribe(ctx, samples, t.settings.Vocabulary)
	result := sttResult{Text: strings.TrimSpace(text), ModelID: t.settings.Model, Offline: true}
	if observe != nil {
		observe(t.settings.Model, 1, time.Since(started), result, err)
	}
	if err != nil {
		return sttResult{}, err
	}
	return result, nil
}

func decodeWAVPCM16(wav []byte) ([]int16, error) {
	if len(wav) < wavHeaderSize || !bytes.Equal(wav[:4], []byte("RIFF")) || !bytes.Equal(wav[8:12], []byte("WAVE")) || !bytes.Equal(wav[12:16], []byte("fmt ")) || !bytes.Equal(wav[36:40], []byte("data")) {
		return nil, errors.New("local transcription requires a canonical 16 kHz mono PCM WAV")
	}
	if binary.LittleEndian.Uint16(wav[20:22]) != 1 ||
		binary.LittleEndian.Uint16(wav[22:24]) != wavChannels ||
		binary.LittleEndian.Uint32(wav[24:28]) != wavSampleRate ||
		binary.LittleEndian.Uint16(wav[34:36]) != wavBitsPerSample {
		return nil, errors.New("local transcription requires 16 kHz mono PCM16 audio")
	}
	dataBytes := int(binary.LittleEndian.Uint32(wav[40:44]))
	if dataBytes < 0 || dataBytes%2 != 0 || dataBytes > len(wav)-wavHeaderSize {
		return nil, errors.New("local transcription WAV data is truncated")
	}
	count := dataBytes / 2
	samples := make([]int16, count)
	if err := binary.Read(bytes.NewReader(wav[wavHeaderSize:wavHeaderSize+dataBytes]), binary.LittleEndian, samples); err != nil {
		return nil, err
	}
	return samples, nil
}

type localStreamResult struct {
	Text      string
	Committed string
	Inserted  string
	Err       error
	InsertErr error
}

type localStreamSession struct {
	streamCtx  context.Context
	cancel     context.CancelFunc
	stream     localStreamEngine
	mode       streamingMode
	newInserter localInserterFactory
	chunks     chan []int16
	done       chan struct{}
	inputMu    sync.Mutex
	inputClosed bool

	mu             sync.Mutex
	overflow       atomic.Bool
	previousCommit string
	pendingInsert  string
	result         localStreamResult
	inserter       TextInserter
	insertDisabled bool
}

func startLocalStream(ctx context.Context, use *localModelUse, mode streamingMode, vocabulary []string) (*localStreamSession, error) {
	if use == nil || use.native == nil || !use.native.supportsStreaming {
		return nil, errors.New("selected local model is not streaming-capable")
	}
	if mode != streamingModeProcess && mode != streamingModeLive {
		return nil, errors.New("streaming is disabled for this profile")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := use.native.beginStream(streamCtx, vocabulary)
	if err != nil {
		cancel()
		return nil, err
	}
	return newLocalStreamSessionWithContext(streamCtx, cancel, mode, stream, func(ctx context.Context) (TextInserter, error) {
		return NewTextInserter(ctx, InsertBackendIBus)
	}), nil
}

func newLocalStreamSession(ctx context.Context, mode streamingMode, stream localStreamEngine, newInserter localInserterFactory) *localStreamSession {
	streamCtx, cancel := context.WithCancel(ctx)
	return newLocalStreamSessionWithContext(streamCtx, cancel, mode, stream, newInserter)
}

func newLocalStreamSessionWithContext(streamCtx context.Context, cancel context.CancelFunc, mode streamingMode, stream localStreamEngine, newInserter localInserterFactory) *localStreamSession {
	if newInserter == nil {
		newInserter = func(ctx context.Context) (TextInserter, error) {
			return NewTextInserter(ctx, InsertBackendIBus)
		}
	}
	session := &localStreamSession{
		streamCtx: streamCtx,
		cancel:    cancel,
		stream:    stream,
		mode:      mode,
		newInserter: newInserter,
		chunks:    make(chan []int16, localStreamQueueSize),
		done:      make(chan struct{}),
	}
	go session.run()
	return session
}

// OfferSamples never waits on inference. A full queue disables streaming and
// leaves the recorder's complete sample buffer intact for safe recovery.
func (s *localStreamSession) OfferSamples(samples []int16) {
	if s == nil || len(samples) == 0 {
		return
	}
	chunk := append([]int16(nil), samples...)
	s.inputMu.Lock()
	defer s.inputMu.Unlock()
	if s.inputClosed || s.streamCtx.Err() != nil {
		return
	}
	select {
	case s.chunks <- chunk:
	default:
		s.overflow.Store(true)
		s.cancel()
	}
}

func (s *localStreamSession) run() {
	defer close(s.done)
	for {
		select {
		case <-s.streamCtx.Done():
			s.mu.Lock()
			if s.result.Err == nil {
				if s.streamCtx.Err() != nil {
					s.result.Err = s.streamCtx.Err()
				} else if s.overflow.Load() {
					s.result.Err = errors.New("local streaming could not keep up; recording retained")
				} else {
					s.result.Err = errors.New("local streaming was cancelled")
				}
			}
			s.mu.Unlock()
			return
		case chunk, ok := <-s.chunks:
			if !ok {
				full, committed, err := s.stream.finalize(s.streamCtx)
				if err != nil {
					s.setStreamError(err)
					return
				}
				s.commitSnapshot(full, committed, true)
				s.mu.Lock()
				s.result.Text, s.result.Committed = full, committed
				s.mu.Unlock()
				return
			}
			full, committed, err := s.stream.feed(s.streamCtx, chunk)
			if err != nil {
				s.setStreamError(err)
				return
			}
			s.commitSnapshot(full, committed, false)
		}
	}
}

func (s *localStreamSession) setStreamError(err error) {
	s.mu.Lock()
	if s.result.Err == nil {
		s.result.Err = err
	}
	s.result.Text = ""
	s.mu.Unlock()
}

func (s *localStreamSession) commitSnapshot(full, committed string, final bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasPrefix(committed, s.previousCommit) {
		s.result.Err = errors.New("local streaming changed text it had already committed; recording retained")
		s.cancel()
		return
	}
	if final && !strings.HasPrefix(full, committed) {
		s.result.Err = errors.New("final transcript disagreed with committed live text; recording retained")
		s.result.Text = full
		s.result.Committed = committed
		return
	}
	delta := committed[len(s.previousCommit):]
	s.previousCommit = committed
	s.result.Committed = committed
	s.result.Text = full
	if s.mode != streamingModeLive || s.insertDisabled {
		return
	}
	s.pendingInsert += delta
	insertThrough := 0
	if final {
		insertThrough = len(s.pendingInsert)
	} else {
		for index, char := range s.pendingInsert {
			if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
				insertThrough = index + 1
			}
		}
	}
	if insertThrough == 0 {
		return
	}
	text := s.pendingInsert[:insertThrough]
	s.pendingInsert = s.pendingInsert[insertThrough:]
	if s.inserter == nil {
		inserter, err := s.newInserter(s.streamCtx)
		if err != nil {
			s.result.InsertErr = err
			s.insertDisabled = true
			return
		}
		s.inserter = inserter
	}
	if err := s.inserter.Insert(s.streamCtx, text); err != nil {
		s.result.InsertErr = err
		s.insertDisabled = true
		return
	}
	s.result.Inserted += text
}

func (s *localStreamSession) Finish() localStreamResult {
	if s == nil {
		return localStreamResult{Err: errors.New("local streaming session is not open")}
	}
	s.closeInput()
	<-s.done
	s.stream.close()
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inserter != nil {
		if err := s.inserter.Close(); err != nil && s.result.InsertErr == nil {
			s.result.InsertErr = err
		}
		s.inserter = nil
	}
	if s.result.InsertErr != nil {
		s.result.Err = errors.Join(s.result.Err, fmt.Errorf("live insertion failed: %w", s.result.InsertErr))
	}
	return s.result
}

func (s *localStreamSession) Abort() {
	if s == nil {
		return
	}
	s.cancel()
	s.closeInput()
	<-s.done
	s.stream.close()
	s.mu.Lock()
	if s.inserter != nil {
		_ = s.inserter.Close()
		s.inserter = nil
	}
	s.mu.Unlock()
}

func (s *localStreamSession) closeInput() {
	s.inputMu.Lock()
	if !s.inputClosed {
		s.inputClosed = true
		close(s.chunks)
	}
	s.inputMu.Unlock()
}

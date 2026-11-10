package voicetype

/*
#cgo pkg-config: voicetype-transcribe
#cgo CXXFLAGS: -std=c++17
#include <stdlib.h>
#include "local_native.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

const nativeErrorBufferSize = 512

type nativeModel struct {
	ptr                *C.struct_vt_asr_model
	id                 string
	backend            string
	supportsStreaming  bool
	supportsVocabulary bool
	closeOnce          sync.Once
}

type nativeSession struct {
	mu        sync.RWMutex
	ptr       *C.struct_vt_asr_session
	closeOnce sync.Once
}

type nativeStream struct {
	session   *nativeSession
	stopWatch func()
	closeOnce sync.Once
	// floatScratch converts each chunk in place; the native call consumes it
	// synchronously before the next feed.
	floatScratch []float32
	full         string
	committed    string
	haveText     bool
}

func loadNativeModel(path, id string) (*nativeModel, error) {
	definition, ok := localModelByID(id)
	if !ok {
		return nil, errors.New("unsupported local model")
	}
	cPath := C.CString(path)
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cID))
	var model *C.struct_vt_asr_model
	backend := make([]C.char, 128)
	var supportsStreaming, supportsVocabulary C.int
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	status := C.vt_asr_model_open(
		cPath,
		cID,
		&model,
		&backend[0],
		C.size_t(len(backend)),
		&supportsStreaming,
		&supportsVocabulary,
		&errorBuffer[0],
		C.size_t(len(errorBuffer)),
	)
	if status != 0 {
		return nil, nativeError("load local model", status, &errorBuffer[0])
	}
	loaded := &nativeModel{
		ptr:                model,
		id:                 id,
		backend:            C.GoString(&backend[0]),
		supportsStreaming:  supportsStreaming != 0,
		supportsVocabulary: supportsVocabulary != 0,
	}
	if definition.SupportsStreaming && !loaded.supportsStreaming {
		loaded.close()
		return nil, fmt.Errorf("%s is not streaming-capable in the bundled inference engine", definition.Name)
	}
	if definition.VocabularySupport != "" && !loaded.supportsVocabulary {
		loaded.close()
		return nil, fmt.Errorf("%s does not expose the cataloged vocabulary support in the bundled inference engine", definition.Name)
	}
	return loaded, nil
}

func (m *nativeModel) close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		if m.ptr != nil {
			C.vt_asr_model_close(m.ptr)
			m.ptr = nil
		}
	})
}

func (m *nativeModel) newSession() (*nativeSession, error) {
	if m == nil || m.ptr == nil {
		return nil, errors.New("local model is not loaded")
	}
	var session *C.struct_vt_asr_session
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	status := C.vt_asr_session_open(m.ptr, &session, &errorBuffer[0], C.size_t(len(errorBuffer)))
	if status != 0 {
		return nil, nativeError("create local transcription session", status, &errorBuffer[0])
	}
	return &nativeSession{ptr: session}, nil
}

func (s *nativeSession) abort() {
	s.mu.RLock()
	if s.ptr != nil {
		C.vt_asr_session_abort(s.ptr)
	}
	s.mu.RUnlock()
}

func (s *nativeSession) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		if s.ptr != nil {
			C.vt_asr_session_close(s.ptr)
			s.ptr = nil
		}
		s.mu.Unlock()
	})
}

func (s *nativeSession) watch(ctx context.Context) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			s.abort()
		case <-stop:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}

func (m *nativeModel) transcribe(ctx context.Context, samples []int16, vocabulary []string) (string, error) {
	if len(samples) == 0 {
		return "", errors.New("no audio samples to transcribe")
	}
	session, err := m.newSession()
	if err != nil {
		return "", err
	}
	defer session.close()
	stopWatch := session.watch(ctx)
	defer stopWatch()

	floatSamples := pcm16ToFloat32(samples)
	cVocabulary := nativeVocabulary(vocabulary)
	if cVocabulary != nil {
		defer C.free(unsafe.Pointer(cVocabulary))
	}
	var text *C.char
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	status := C.vt_asr_run(
		session.ptr,
		(*C.float)(unsafe.Pointer(&floatSamples[0])),
		C.int(len(floatSamples)),
		cVocabulary,
		&text,
		&errorBuffer[0],
		C.size_t(len(errorBuffer)),
	)
	runtime.KeepAlive(floatSamples)
	if status != 0 {
		return "", nativeError("transcribe locally", status, &errorBuffer[0])
	}
	defer C.vt_asr_string_free(text)
	return C.GoString(text), nil
}

func (m *nativeModel) beginStream(ctx context.Context, vocabulary []string) (*nativeStream, error) {
	if !m.supportsStreaming {
		return nil, errors.New("this local model does not support streaming")
	}
	session, err := m.newSession()
	if err != nil {
		return nil, err
	}
	stream := &nativeStream{session: session, stopWatch: session.watch(ctx)}
	cVocabulary := nativeVocabulary(vocabulary)
	if cVocabulary != nil {
		defer C.free(unsafe.Pointer(cVocabulary))
	}
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	status := C.vt_asr_stream_begin(session.ptr, cVocabulary, &errorBuffer[0], C.size_t(len(errorBuffer)))
	if status != 0 {
		stream.close()
		return nil, nativeError("start local streaming transcription", status, &errorBuffer[0])
	}
	return stream, nil
}

func (s *nativeStream) feed(ctx context.Context, samples []int16) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if len(samples) == 0 {
		return "", "", errors.New("streaming audio chunk is empty")
	}
	s.floatScratch = pcm16ToFloat32Into(s.floatScratch[:0], samples)
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	var changed C.int
	status := C.vt_asr_stream_feed(
		s.session.ptr,
		(*C.float)(unsafe.Pointer(&s.floatScratch[0])),
		C.int(len(s.floatScratch)),
		&changed,
		&errorBuffer[0],
		C.size_t(len(errorBuffer)),
	)
	runtime.KeepAlive(s.floatScratch)
	if status != 0 {
		return "", "", nativeError("process local streaming audio", status, &errorBuffer[0])
	}
	return s.snapshot(changed != 0)
}

func (s *nativeStream) finalize(ctx context.Context) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	var changed C.int
	status := C.vt_asr_stream_finalize(s.session.ptr, &changed, &errorBuffer[0], C.size_t(len(errorBuffer)))
	if status != 0 {
		return "", "", nativeError("finish local streaming transcription", status, &errorBuffer[0])
	}
	return s.snapshot(changed != 0)
}

// snapshot copies transcript text from C only when the engine reports a change.
// An unchanged feed reuses the previous Go strings.
func (s *nativeStream) snapshot(changed bool) (string, string, error) {
	if !changed && s.haveText {
		return s.full, s.committed, nil
	}
	full, committed, err := s.text()
	if err != nil {
		return "", "", err
	}
	s.full, s.committed, s.haveText = full, committed, true
	return full, committed, nil
}

func (s *nativeStream) text() (string, string, error) {
	var full, committed *C.char
	errorBuffer := make([]C.char, nativeErrorBufferSize)
	status := C.vt_asr_stream_text(s.session.ptr, &full, &committed, &errorBuffer[0], C.size_t(len(errorBuffer)))
	if status != 0 {
		return "", "", nativeError("read local streaming transcript", status, &errorBuffer[0])
	}
	defer C.vt_asr_string_free(full)
	defer C.vt_asr_string_free(committed)
	return C.GoString(full), C.GoString(committed), nil
}

func (s *nativeStream) close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.stopWatch != nil {
			s.stopWatch()
		}
		s.session.close()
	})
}

func nativeVocabulary(terms []string) *C.char {
	if len(terms) == 0 {
		return nil
	}
	return C.CString(strings.Join(terms, "\n"))
}

func pcm16ToFloat32(samples []int16) []float32 {
	return pcm16ToFloat32Into(nil, samples)
}

// pcm16ToFloat32Into appends the converted samples to dst, reusing its storage
// so a long stream does not allocate a fresh conversion buffer per chunk.
func pcm16ToFloat32Into(dst []float32, samples []int16) []float32 {
	if need := len(samples); cap(dst) < need {
		dst = make([]float32, need)
	} else {
		dst = dst[:need]
	}
	for i, sample := range samples {
		dst[i] = float32(sample) / 32768
	}
	return dst
}

func nativeError(action string, status C.int, message *C.char) error {
	if message != nil && C.GoString(message) != "" {
		return fmt.Errorf("%s: %s", action, C.GoString(message))
	}
	return fmt.Errorf("%s: native engine returned status %d", action, int(status))
}

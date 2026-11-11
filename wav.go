package voicetype

import (
	"encoding/binary"
	"unsafe"
)

const (
	wavSampleRate    = 16000
	wavChannels      = 1
	wavBitsPerSample = 16
	wavHeaderSize    = 44
)

// hostLittleEndian matches the WAV byte order on every supported Linux target.
// The zero-copy PCM view below is only valid when the in-memory int16 layout is
// already little-endian, so big-endian hosts keep the copying fallback.
var hostLittleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

// wavHeader returns the canonical 44-byte RIFF/WAVE header for count samples.
func wavHeader(count int) [wavHeaderSize]byte {
	dataSize := count * 2
	byteRate := wavSampleRate * wavChannels * wavBitsPerSample / 8
	blockAlign := wavChannels * wavBitsPerSample / 8

	var header [wavHeaderSize]byte
	le := binary.LittleEndian
	copy(header[0:4], "RIFF")
	le.PutUint32(header[4:8], uint32(36+dataSize)) // size of everything after this field
	copy(header[8:12], "WAVE")

	copy(header[12:16], "fmt ")
	le.PutUint32(header[16:20], 16)                 // fmt chunk size
	le.PutUint16(header[20:22], 1)                  // PCM
	le.PutUint16(header[22:24], wavChannels)        //
	le.PutUint32(header[24:28], wavSampleRate)      //
	le.PutUint32(header[28:32], uint32(byteRate))   // 32000
	le.PutUint16(header[32:34], uint16(blockAlign)) // 2
	le.PutUint16(header[34:36], wavBitsPerSample)   // 16

	copy(header[36:40], "data")
	le.PutUint32(header[40:44], uint32(dataSize))
	return header
}

// WavEncode wraps PCM16 samples in a canonical 44-byte RIFF/WAVE header.
// Capture and upload both use 16 kHz mono PCM16, with no resampling.
//
// The result is written into one allocation sized for header plus audio; no
// intermediate buffer is retained.
func WavEncode(samples []int16) []byte {
	out := make([]byte, wavHeaderSize+len(samples)*2)
	header := wavHeader(len(samples))
	copy(out, header[:])
	if pcm, ok := pcm16Bytes(samples); ok {
		copy(out[wavHeaderSize:], pcm)
		return out
	}
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(out[wavHeaderSize+2*i:], uint16(sample))
	}
	return out
}

// pcm16Bytes exposes the little-endian PCM bytes backing samples without
// copying. The result aliases samples, so callers must not retain it beyond the
// sample slice's lifetime or mutate it.
func pcm16Bytes(samples []int16) ([]byte, bool) {
	if !hostLittleEndian || len(samples) == 0 {
		return nil, false
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), len(samples)*2), true
}

// wavDurationSeconds reports how long a sample slice plays for.
func wavDurationSeconds(samples []int16) float64 {
	return float64(len(samples)) / float64(wavSampleRate)
}

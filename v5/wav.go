package main

import (
	"bytes"
	"encoding/binary"
)

const (
	wavSampleRate    = 16000
	wavChannels      = 1
	wavBitsPerSample = 16
	wavHeaderSize    = 44
)

// wavEncode wraps PCM16 samples in a canonical 44-byte RIFF/WAVE header.
// Parakeet's native input spec is 16 kHz mono PCM16, so no resampling happens
// anywhere in the pipeline.
func wavEncode(samples []int16) []byte {
	dataSize := len(samples) * 2
	byteRate := wavSampleRate * wavChannels * wavBitsPerSample / 8
	blockAlign := wavChannels * wavBitsPerSample / 8

	buf := bytes.NewBuffer(make([]byte, 0, wavHeaderSize+dataSize))
	le := binary.LittleEndian

	buf.WriteString("RIFF")
	binary.Write(buf, le, uint32(36+dataSize)) // size of everything after this field
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	binary.Write(buf, le, uint32(16))               // fmt chunk size
	binary.Write(buf, le, uint16(1))                // PCM
	binary.Write(buf, le, uint16(wavChannels))      //
	binary.Write(buf, le, uint32(wavSampleRate))    //
	binary.Write(buf, le, uint32(byteRate))         // 32000
	binary.Write(buf, le, uint16(blockAlign))       // 2
	binary.Write(buf, le, uint16(wavBitsPerSample)) // 16

	buf.WriteString("data")
	binary.Write(buf, le, uint32(dataSize))
	binary.Write(buf, le, samples)

	return buf.Bytes()
}

// wavDurationSeconds reports how long a sample slice plays for.
func wavDurationSeconds(samples []int16) float64 {
	return float64(len(samples)) / float64(wavSampleRate)
}

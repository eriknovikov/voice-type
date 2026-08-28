package voicetype

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWavHeaderGolden(t *testing.T) {
	// 4 samples -> 8 bytes of data
	got := WavEncode([]int16{0, 1, -1, 32767})

	if len(got) != wavHeaderSize+8 {
		t.Fatalf("length = %d, want %d", len(got), wavHeaderSize+8)
	}

	want := []byte{
		'R', 'I', 'F', 'F',
		44, 0, 0, 0, // 36 + 8
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		16, 0, 0, 0, // fmt chunk size
		1, 0, // PCM
		1, 0, // mono
		0x80, 0x3e, 0, 0, // 16000
		0x00, 0x7d, 0, 0, // 32000 byte rate
		2, 0, // block align
		16, 0, // bits per sample
		'd', 'a', 't', 'a',
		8, 0, 0, 0,
	}

	if !bytes.Equal(got[:wavHeaderSize], want) {
		t.Errorf("header mismatch\ngot:  % x\nwant: % x", got[:wavHeaderSize], want)
	}

	// samples land little-endian right after the header
	for i, w := range []int16{0, 1, -1, 32767} {
		g := int16(binary.LittleEndian.Uint16(got[wavHeaderSize+i*2:]))
		if g != w {
			t.Errorf("sample %d = %d, want %d", i, g, w)
		}
	}
}

func TestWavEmptyInput(t *testing.T) {
	got := WavEncode(nil)
	if len(got) != wavHeaderSize {
		t.Fatalf("length = %d, want bare header %d", len(got), wavHeaderSize)
	}
	if riffSize := binary.LittleEndian.Uint32(got[4:]); riffSize != 36 {
		t.Errorf("RIFF size = %d, want 36", riffSize)
	}
	if dataSize := binary.LittleEndian.Uint32(got[40:]); dataSize != 0 {
		t.Errorf("data size = %d, want 0", dataSize)
	}
}

func TestWavSizesTrackSampleCount(t *testing.T) {
	samples := make([]int16, 16000) // exactly one second
	got := WavEncode(samples)

	if dataSize := binary.LittleEndian.Uint32(got[40:]); dataSize != 32000 {
		t.Errorf("data size = %d, want 32000", dataSize)
	}
	if riffSize := binary.LittleEndian.Uint32(got[4:]); riffSize != 36+32000 {
		t.Errorf("RIFF size = %d, want %d", riffSize, 36+32000)
	}
	if d := wavDurationSeconds(samples); d != 1.0 {
		t.Errorf("duration = %v, want 1.0", d)
	}
}

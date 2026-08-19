package voicetype

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
)

// Instrumentation for the endpoint trimmer. TrimSilence answers "what do we
// upload"; this answers "why", and flags cuts that look wrong.
//
// The failure it hunts: silenceGate estimates a floor from the quietest frames
// of a one-second window, so an utterance with no quiet lead-in makes its own
// speech the floor, the gate lands above part of the utterance, and the trim
// eats into it. worthUploading guards its version of this with
// hasVoicedCadence; TrimSilence does not.

// EndReport describes one end of a capture: what floor was estimated there,
// what gate that produced, and whether the window was quiet enough to trust.
type EndReport struct {
	Floor       float64 // RMS taken as the noise floor (trimFloorRank-th quietest)
	Gate        float64 // floor*trimGateRatio, or trimMinGate
	WindowMin   float64 // quietest frame in the estimation window
	WindowMax   float64 // loudest frame in the estimation window
	WindowMedn  float64 // median frame in the estimation window
	CutSeconds  float64 // audio removed at this end
	Contaminate bool    // window has no genuinely quiet frames -- floor is suspect
}

// TrimReport is the full picture for one capture.
type TrimReport struct {
	Samples     int
	Seconds     float64
	PeakRMS     float64
	GlobalFloor float64 // quietest frame anywhere, the most optimistic floor available
	Frames      []float64

	Head EndReport
	Tail EndReport

	Start, End     int // sample offsets TrimSilence chose
	TrimmedSeconds float64

	WorthUploading bool
	HasSpeech      bool
	VoicedCadence  bool

	// Frames above the global gate that fall outside the kept span. Non-zero
	// means the trim discarded audio that some other part of the clip would
	// have called speech -- the direct signature of the bug above.
	CutFramesHead int
	CutFramesTail int

	Warnings []string
}

const (
	// contaminationRatio: a window whose "floor" sits this far above the
	// quietest frame in the whole clip contains no true silence, so what
	// silenceGate called a floor is really speech.
	contaminationRatio = 4.0
	// harmRatio: a frame within this factor of the clip's peak is speech by any
	// reasonable reading. Judging cuts against the peak rather than against an
	// estimated floor is the point -- a contaminated floor is exactly the
	// failure being hunted, so it cannot also be the yardstick.
	harmRatio = 8.0
)

// AnalyzeTrim runs the real trimmer and reports how it reached its decision.
func AnalyzeTrim(samples []int16) TrimReport {
	r := TrimReport{
		Samples:        len(samples),
		Seconds:        wavDurationSeconds(samples),
		WorthUploading: worthUploading(samples),
		HasSpeech:      hasSpeech(samples),
		VoicedCadence:  hasVoicedCadence(samples),
	}

	frames := len(samples) / trimFrameSamples
	if frames == 0 {
		r.Warnings = append(r.Warnings, "capture shorter than one 20 ms frame; nothing to analyse")
		return r
	}

	r.Frames = make([]float64, frames)
	for i := range r.Frames {
		r.Frames[i] = frameRMS(samples[i*trimFrameSamples : (i+1)*trimFrameSamples])
	}
	r.PeakRMS = slices.Max(r.Frames)
	r.GlobalFloor = slices.Min(r.Frames)

	headWindow := r.Frames[:min(trimWindowFrames, frames)]
	tailWindow := r.Frames[max(frames-trimWindowFrames, 0):]
	r.Head = describeEnd(headWindow, r.GlobalFloor)
	r.Tail = describeEnd(tailWindow, r.GlobalFloor)

	trimmed := TrimSilence(samples)
	r.TrimmedSeconds = wavDurationSeconds(trimmed)
	r.Start, r.End = spanOf(samples, trimmed)
	r.Head.CutSeconds = float64(r.Start) / float64(wavSampleRate)
	r.Tail.CutSeconds = float64(len(samples)-r.End) / float64(wavSampleRate)

	// Judge the discarded audio against the clip's own peak. Any floor estimate
	// is suspect here by construction, so the harm test avoids floors entirely:
	// a cut frame within harmRatio of the peak is speech that got thrown away.
	harmGate := max(r.PeakRMS/harmRatio, trimMinGate)
	for i, v := range r.Frames {
		if v < harmGate {
			continue
		}
		switch {
		case (i+1)*trimFrameSamples <= r.Start:
			r.CutFramesHead++
		case i*trimFrameSamples >= r.End:
			r.CutFramesTail++
		}
	}

	r.Warnings = warningsFor(&r)
	return r
}

func describeEnd(window []float64, globalFloor float64) EndReport {
	sorted := slices.Clone(window)
	slices.Sort(sorted)

	floor := sorted[min(trimFloorRank, len(sorted)-1)]
	return EndReport{
		Floor:      floor,
		Gate:       max(floor*trimGateRatio, trimMinGate),
		WindowMin:  sorted[0],
		WindowMax:  sorted[len(sorted)-1],
		WindowMedn: sorted[len(sorted)/2],
		// No frame in this window comes close to the quietest frame elsewhere
		// in the clip, so this end never went silent and its "floor" is speech.
		Contaminate: floor >= globalFloor*contaminationRatio,
	}
}

func warningsFor(r *TrimReport) []string {
	var w []string

	if r.CutFramesHead > 0 {
		w = append(w, fmt.Sprintf(
			"HEAD CUT %d frame(s) (%.0f ms) close to the clip peak -- clipped speech",
			r.CutFramesHead, float64(r.CutFramesHead)*frameMillis))
	}
	if r.CutFramesTail > 0 {
		w = append(w, fmt.Sprintf(
			"TAIL CUT %d frame(s) (%.0f ms) close to the clip peak -- clipped speech",
			r.CutFramesTail, float64(r.CutFramesTail)*frameMillis))
	}
	if r.Head.Contaminate {
		w = append(w, "head window has no quiet frames: its floor is speech, so the head gate is too high")
	}
	if r.Tail.Contaminate {
		w = append(w, "tail window has no quiet frames: its floor is speech, so the tail gate is too high")
	}
	if r.Head.Gate <= trimMinGate && r.Head.WindowMedn > trimMinGate {
		w = append(w, "head gate pinned to trimMinGate while the room is louder than it -- head trim may fire on room noise")
	}
	if !r.WorthUploading {
		w = append(w, "worthUploading says DISCARD: this capture would never be sent")
	}
	return w
}

const frameMillis = float64(trimFrameSamples) / float64(wavSampleRate) * 1000

// spanOf recovers the offsets of a sub-slice within its parent. TrimSilence
// aliases its input, so the trimmed slice's capacity reveals where it started.
func spanOf(parent []int16, sub []int16) (start, end int) {
	start = cap(parent) - cap(sub)
	if start < 0 || start > len(parent) {
		return 0, len(parent)
	}
	return start, start + len(sub)
}

// sttCostPerSecond is the tuned model's rate, measured against the live
// endpoint: a 0.5 s clip bills $0.0000125. Quoted here only so the trimmer's
// savings can be stated in dollars -- which is the point, because they are
// vanishingly small and trimming has to justify itself some other way.
const sttCostPerSecond = 0.000025

// EstimatedCost returns what a capture of this length costs to transcribe.
func EstimatedCost(seconds float64) float64 { return seconds * sttCostPerSecond }

// WavDecode reads a canonical 16 kHz mono PCM16 RIFF file -- the format
// WavEncode writes, which is all this tool needs to round-trip its own samples.
func WavDecode(raw []byte) ([]int16, error) {
	if len(raw) < wavHeaderSize {
		return nil, fmt.Errorf("not a WAV file: %d bytes is shorter than a header", len(raw))
	}
	if string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a RIFF/WAVE file")
	}

	// Walk the chunk list rather than assuming a 44-byte header: recorders
	// routinely insert LIST/fact chunks before the data.
	var channels, bits int
	var rate int
	offset := 12
	for offset+8 <= len(raw) {
		id := string(raw[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		body := offset + 8
		if body+size > len(raw) {
			size = len(raw) - body
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, fmt.Errorf("fmt chunk too small")
			}
			channels = int(binary.LittleEndian.Uint16(raw[body+2 : body+4]))
			rate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(raw[body+14 : body+16]))
		case "data":
			if channels != wavChannels || bits != wavBitsPerSample {
				return nil, fmt.Errorf("need %d-channel %d-bit audio, got %d-channel %d-bit",
					wavChannels, wavBitsPerSample, channels, bits)
			}
			if rate != wavSampleRate {
				return nil, fmt.Errorf("need %d Hz, got %d Hz -- resample first, the pipeline never does", wavSampleRate, rate)
			}
			samples := make([]int16, size/2)
			for i := range samples {
				samples[i] = int16(binary.LittleEndian.Uint16(raw[body+i*2 : body+i*2+2]))
			}
			return samples, nil
		}
		offset = body + size
		if size%2 == 1 {
			offset++ // chunks are word-aligned
		}
	}
	return nil, fmt.Errorf("no data chunk found")
}

// DBFS converts an RMS level to dB relative to int16 full scale, which is the
// unit a human can reason about when staring at these numbers.
func DBFS(rms float64) float64 {
	if rms <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(rms/32768)
}

package main

import (
	"math"
	"slices"
)

// Endpoint trimming: drop the leading and trailing silence from a capture
// before upload. Two reasons, in the order they actually matter:
//
//  1. Bytes on the wire. WAV is 32 KB/s, so a couple of seconds of room tone
//     adds ~64 KB to every multipart POST.
//  2. Billing rounds to whole seconds, so cutting the dead air at each end
//     usually knocks a second or two off the bill. In absolute terms this is
//     pennies per week -- it is not the reason this file exists.
//
// This is *not* the VAD auto-stop that PLAN_V5.md rules out. Session boundaries
// stay exactly where the hotkey put them, and nothing inside the utterance is
// touched -- pauses between words survive, because Parakeet infers punctuation
// from prosody. That same reason is why trimPadSamples leaves a margin at each
// end instead of cutting flush to the first and last loud frame: clipping the
// trailing pause costs the terminal period.
const (
	trimFrameSamples = 320  // 20 ms at 16 kHz
	trimPadSamples   = 4000 // 250 ms kept outside the detected speech
	trimWindowFrames = 50   // 1 s at each end, used to estimate the noise floor
	trimGateRatio    = 4.0  // gate sits this far above the estimated noise floor
	trimMinGate      = 80.0 // absolute RMS gate, for a digitally silent capture
	trimFloorRank    = 2    // rank within a window taken as that end's floor
)

// trimSilence returns the span of samples between the first and last frame that
// clears the noise gate, padded by trimPadSamples on each side. The result
// aliases the input slice; callers must not mutate it afterwards.
//
// Each end gets its own gate, estimated from the second of audio at that end.
// A single whole-clip floor does not survive contact with real recordings: a
// capture that contains any digitally silent frame -- a noise-gating mic, a
// Bluetooth headset using DTX, a synthesised test fixture -- pins the estimate
// at zero, and a noisy room then reads as speech from the first frame on.
func trimSilence(samples []int16) []int16 {
	// Too short to estimate a noise floor from. A capture this brief is all
	// onset anyway.
	if len(samples) < trimFrameSamples*2 {
		return samples
	}

	frames := len(samples) / trimFrameSamples
	rms := make([]float64, frames)
	for i := range rms {
		rms[i] = frameRMS(samples[i*trimFrameSamples : (i+1)*trimFrameSamples])
	}

	// Nothing clearing a gate means that end is either silent throughout or the
	// estimate is off. Both are safer left alone: keeping too much audio costs
	// a fraction of a cent, cutting too much costs the user their words.
	start, end := 0, len(samples)

	headGate := silenceGate(rms[:min(trimWindowFrames, frames)])
	for i, v := range rms {
		if v >= headGate {
			start = max(i*trimFrameSamples-trimPadSamples, 0)
			break
		}
	}

	// The scan deliberately runs past the estimation window. If speech starts
	// inside that first second, a frame clears the gate there and the cut is
	// small; only when the whole window is below its own gate -- genuinely
	// quiet -- does the search continue, which is exactly when it should.
	tailGate := silenceGate(rms[max(frames-trimWindowFrames, 0):])
	for i := frames - 1; i >= 0; i-- {
		if rms[i] >= tailGate {
			// The final partial frame is not analysed; the pad covers it.
			end = min((i+1)*trimFrameSamples+trimPadSamples, len(samples))
			break
		}
	}

	if start >= end {
		return samples
	}
	return samples[start:end]
}

// silenceGate estimates the noise floor of one window as the level of its
// quietest few frames, and puts the gate a fixed ratio above it.
//
// A rank, not a percentile: a percentile assumes silence makes up some known
// share of the window, which breaks on exactly the clips worth trimming. Taking
// the 3rd-quietest frame is robust to one anomalous frame while still tracking
// the floor when most of the window is speech.
func silenceGate(window []float64) float64 {
	sorted := slices.Clone(window)
	slices.Sort(sorted)

	floor := sorted[min(trimFloorRank, len(sorted)-1)]
	return max(floor*trimGateRatio, trimMinGate)
}

func frameRMS(frame []int16) float64 {
	var sum float64
	for _, s := range frame {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(frame)))
}

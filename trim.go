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
// This is *not* VAD auto-stop. Session boundaries stay exactly where the hotkey
// put them, and nothing inside the utterance is touched -- pauses between words
// survive, because Parakeet infers punctuation from prosody. That same reason
// is why trimPadSamples leaves a margin at each end instead of cutting flush to
// the first and last loud frame: clipping the trailing pause costs the terminal
// period.
const (
	trimFrameSamples = 320  // 20 ms at 16 kHz
	trimPadSamples   = 4000 // 250 ms kept outside the detected speech
	trimWindowFrames = 50   // 1 s at each end, used to estimate the noise floor
	trimGateRatio    = 4.0  // gate sits this far above the estimated noise floor
	trimMinGate      = 80.0 // absolute RMS gate, for a digitally silent capture
	trimFloorRank    = 2    // rank within a window taken as that end's floor
)

// Discard thresholds for a capture that never contained an utterance -- see
// worthUploading.
const (
	minCaptureSeconds = 0.35 // shorter than any real word: a fumbled hotkey
	speechGateSeconds = 2.0  // above this a capture is intentional, always upload
	// Voiced speech crosses zero much less often than broadband room noise. This
	// rescues an utterance that starts before the capture has a quiet floor.
	speechMaxZeroCrossingRate = 0.20
)

// worthUploading reports whether a capture is worth a round trip. It exists for
// the fumbled hotkey: opened and closed in the same gesture, or opened and left
// pointing at a quiet room. Such a capture costs a blocked /stop, and risks
// typing whatever the model makes of room tone into the focused window.
//
// Two rules keep it from eating real dictation. Only short captures are judged
// at all -- anything past speechGateSeconds was deliberate, and a long recording
// wrongly dropped costs the user far more than an unnecessary upload. And the
// gate errs toward uploading: a digitally silent frame pins the estimated floor
// to zero and the gate falls back to trimMinGate, which speech clears by a wide
// margin. Same asymmetry as trimSilence -- over-sending costs a fraction of a
// cent, over-dropping costs the user their words.
func worthUploading(samples []int16) bool {
	if wavDurationSeconds(samples) < minCaptureSeconds {
		return false
	}
	if wavDurationSeconds(samples) >= speechGateSeconds {
		return true
	}
	return hasSpeech(samples)
}

// hasSpeech reports whether any frame clears the noise gate estimated over the
// whole capture. Unlike trimSilence this needs no per-end estimate: it asks
// whether the clip contains an utterance at all, not where one begins.
func hasSpeech(samples []int16) bool {
	frames := len(samples) / trimFrameSamples
	if frames == 0 {
		return false
	}

	rms := make([]float64, frames)
	for i := range rms {
		rms[i] = frameRMS(samples[i*trimFrameSamples : (i+1)*trimFrameSamples])
	}

	peak := slices.Max(rms)
	if peak >= silenceGate(rms) {
		return true
	}

	// A clip that begins immediately with sustained speech has no quiet frames
	// from which to estimate a floor. Voiced cadence provides a conservative
	// fallback without treating all steady room noise as speech.
	return peak >= trimMinGate && hasVoicedCadence(samples)
}

func hasVoicedCadence(samples []int16) bool {
	var previous int16
	crossings, transitions := 0, 0
	for _, sample := range samples {
		if sample == 0 {
			continue
		}
		if previous != 0 {
			transitions++
			if (previous < 0) != (sample < 0) {
				crossings++
			}
		}
		previous = sample
	}
	return transitions > 0 && float64(crossings)/float64(transitions) <= speechMaxZeroCrossingRate
}

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

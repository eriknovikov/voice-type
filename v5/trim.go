package voicetype

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
//
// The pad is 400 ms rather than a tighter figure because the gate is estimated
// from the quietest frames of the first and last second, and a soft unvoiced
// onset -- the "s" of "send", a whispered first syllable -- is itself one of
// those quiet frames. It raises the floor, fails to clear the gate it just
// raised, and the scan runs on to the first voiced frame. The pad is what walks
// that back, so it has to be wider than a leading consonant. Widening it costs
// $0.000004 per dictation; getting it wrong costs the user the word.
const (
	trimFrameSamples = 320  // 20 ms at 16 kHz
	trimPadSamples   = 6400 // 400 ms kept outside the detected speech
	trimWindowFrames = 50   // 1 s at each end, used to estimate the noise floor
	trimGateRatio    = 4.0  // gate sits this far above the estimated noise floor
	trimMinGate      = 80.0 // absolute RMS gate, for a digitally silent capture
	trimFloorRank    = 2    // rank within a window taken as that end's floor
)

// Discard thresholds for a capture that never contained an utterance -- see
// worthUploading.
const (
	minCaptureSeconds = 0.35 // shorter than any real word: a fumbled hotkey
	// A single loud frame can be a click or a bumped microphone. Requiring 80 ms
	// still admits short words while rejecting isolated transients.
	speechMinActiveFrames = 4
	// Voiced speech crosses zero much less often than broadband room noise. This
	// rescues an utterance that starts before the capture has a quiet floor.
	speechMinZeroCrossingRate = 0.004
	speechMaxZeroCrossingRate = 0.20
)

// worthUploading reports whether a capture is worth a round trip. It exists for
// the fumbled hotkey: opened and closed in the same gesture, or opened and left
// pointing at a quiet room. Such a capture costs a blocked /stop, and risks
// typing whatever the model makes of room tone into the focused window.
//
// The speech check runs on every capture. Duration alone never makes room tone
// worth uploading: a forgotten open microphone must not reach OpenRouter.
func worthUploading(samples []int16) bool {
	if wavDurationSeconds(samples) < minCaptureSeconds {
		return false
	}
	return hasSpeech(samples)
}

type audioLevels struct {
	peakRMS    float64
	noiseFloor float64
}

type speechAnalysis struct {
	detected         bool
	gate             float64
	maxActiveFrames  int
	zeroCrossingRate float64
}

// measureAudioLevels uses the same 20 ms frames and quiet-frame rank as the
// speech gate, making its log output directly comparable to that decision.
func measureAudioLevels(samples []int16) audioLevels {
	if len(samples) == 0 {
		return audioLevels{}
	}
	frames := (len(samples) + trimFrameSamples - 1) / trimFrameSamples
	rms := make([]float64, 0, frames)
	for start := 0; start < len(samples); start += trimFrameSamples {
		end := min(start+trimFrameSamples, len(samples))
		rms = append(rms, frameRMS(samples[start:end]))
	}
	peak := slices.Max(rms)
	slices.Sort(rms)
	floor := rms[min(trimFloorRank, len(rms)-1)]
	return audioLevels{peakRMS: peak, noiseFloor: floor}
}

// hasSpeech reports whether sustained frames clear the noise gate estimated
// over the whole capture. Unlike TrimSilence this needs no per-end estimate: it
// asks whether the clip contains an utterance at all, not where one begins.
func hasSpeech(samples []int16) bool {
	return analyzeSpeech(samples).detected
}

func analyzeSpeech(samples []int16) speechAnalysis {
	frames := len(samples) / trimFrameSamples
	if frames == 0 {
		return speechAnalysis{}
	}

	rms := make([]float64, frames)
	for i := range rms {
		rms[i] = frameRMS(samples[i*trimFrameSamples : (i+1)*trimFrameSamples])
	}

	peak := slices.Max(rms)
	gate := silenceGate(rms)
	analysis := speechAnalysis{gate: gate}
	activeFrames := 0
	for i, level := range rms {
		if level < gate {
			activeFrames = 0
			continue
		}
		activeFrames++
		analysis.maxActiveFrames = max(analysis.maxActiveFrames, activeFrames)
		if activeFrames < speechMinActiveFrames {
			continue
		}

		// Keep scanning after a noise-like onset. The first 80 ms can be an
		// unvoiced consonant; a later window in the same run may be voiced speech.
		start := (i - speechMinActiveFrames + 1) * trimFrameSamples
		end := (i + 1) * trimFrameSamples
		rate, ok := voicedCadence(samples[start:end])
		analysis.zeroCrossingRate = rate
		if ok {
			analysis.detected = true
			return analysis
		}
	}
	// A clip that begins immediately with sustained speech has no quiet frames
	// from which to estimate a floor. Voiced cadence provides a conservative
	// fallback without treating all steady room noise as speech.
	if gate > peak && peak >= trimMinGate {
		analysis.zeroCrossingRate, analysis.detected = voicedCadence(samples)
	}
	return analysis
}

func voicedCadence(samples []int16) (float64, bool) {
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
	if transitions == 0 {
		return 0, false
	}
	rate := float64(crossings) / float64(transitions)
	return rate, rate >= speechMinZeroCrossingRate && rate <= speechMaxZeroCrossingRate
}

// TrimSilence returns the span of samples between the first and last frame that
// clears the noise gate, padded by trimPadSamples on each side. The result
// aliases the input slice; callers must not mutate it afterwards.
//
// Each end gets its own gate, estimated from the second of audio at that end.
// A single whole-clip floor does not survive contact with real recordings: a
// capture that contains any digitally silent frame -- a noise-gating mic, a
// Bluetooth headset using DTX, a synthesised test fixture -- pins the estimate
// at zero, and a noisy room then reads as speech from the first frame on.
func TrimSilence(samples []int16) []int16 {
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

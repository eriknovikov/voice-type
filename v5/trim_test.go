package main

import "testing"

// silence returns d seconds of room tone at the given peak amplitude. The
// pattern is deterministic but not a constant, so every frame is not identical.
func silence(seconds float64, amplitude int16) []int16 {
	n := int(seconds * wavSampleRate)
	out := make([]int16, n)
	for i := range out {
		v := int16(i % 7)
		if amplitude == 0 {
			continue
		}
		out[i] = (v - 3) * (amplitude / 3)
	}
	return out
}

// speech returns d seconds of a loud square wave -- a stand-in for voiced audio.
func speech(seconds float64, amplitude int16) []int16 {
	n := int(seconds * wavSampleRate)
	out := make([]int16, n)
	for i := range out {
		if (i/40)%2 == 0 {
			out[i] = amplitude
		} else {
			out[i] = -amplitude
		}
	}
	return out
}

func concat(parts ...[]int16) []int16 {
	var out []int16
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestTrimSilenceCutsBothEnds(t *testing.T) {
	in := concat(silence(2, 0), speech(1, 5000), silence(2, 0))

	got := trimSilence(in)

	// 1 s of speech plus 250 ms of pad at each end.
	want := 1.5
	if d := wavDurationSeconds(got); d != want {
		t.Errorf("duration = %.3fs, want %.3fs", d, want)
	}
}

func TestTrimSilenceKeepsPadBeforeOnset(t *testing.T) {
	in := concat(silence(2, 0), speech(1, 5000))

	got := trimSilence(in)

	// The onset must still have room in front of it: cutting flush loses the
	// prosodic lead-in the model uses.
	lead := len(got) - wavSampleRate
	if lead < trimPadSamples-trimFrameSamples || lead > trimPadSamples+trimFrameSamples {
		t.Errorf("lead-in = %d samples, want ~%d", lead, trimPadSamples)
	}
}

func TestTrimSilenceWithRoomTone(t *testing.T) {
	// A noisy mic: the floor is well above zero, so an absolute gate would
	// keep everything.
	in := concat(silence(2, 300), speech(1, 5000), silence(2, 300))

	got := trimSilence(in)

	if d := wavDurationSeconds(got); d != 1.5 {
		t.Errorf("duration = %.3fs, want 1.500s", d)
	}
}

func TestTrimSilencePreservesInternalPause(t *testing.T) {
	// A pause between two phrases is prosody, not dead air -- Parakeet reads
	// punctuation off it. Only the endpoints may be cut.
	in := concat(silence(1, 0), speech(1, 5000), silence(1, 0), speech(1, 5000), silence(1, 0))

	got := trimSilence(in)

	want := 3.5 // 3 s span between onsets plus 250 ms of pad at each end
	if d := wavDurationSeconds(got); d != want {
		t.Errorf("duration = %.3fs, want %.3fs -- internal pause was cut", d, want)
	}
}

func TestTrimSilenceShortLeadLongSpeech(t *testing.T) {
	// The case a percentile-based noise floor gets wrong: silence is a tiny
	// share of the capture, but it is still the part worth removing.
	in := concat(silence(0.5, 200), speech(10, 5000))

	got := trimSilence(in)

	want := 10.25
	if d := wavDurationSeconds(got); d != want {
		t.Errorf("duration = %.3fs, want %.3fs", d, want)
	}
}

// Regression: a capture with any digitally silent frame inside it -- a
// noise-gating mic, or espeak output in a fixture -- used to pin a whole-clip
// floor estimate at zero, so a noisy room read as speech and nothing was cut.
// Each end is estimated independently to keep that from happening.
func TestTrimSilenceNoisyRoomWithInteriorDigitalSilence(t *testing.T) {
	in := concat(
		silence(1.5, 400), // noisy room tone
		speech(1, 5000),   //
		silence(0.3, 0),   // a hard digital gap mid-utterance
		speech(1, 5000),   //
		silence(1.5, 400), //
	)

	got := trimSilence(in)

	want := 2.8 // 2.3 s from first onset to last offset, plus 250 ms each side
	if d := wavDurationSeconds(got); d != want {
		t.Errorf("duration = %.3fs, want %.3fs", d, want)
	}
}

// Speech from the first frame: there is nothing to cut at the head, and the
// head gate must not be so high that the scan runs on and eats the utterance.
func TestTrimSilenceSpeechFromFirstFrame(t *testing.T) {
	in := concat(speech(3, 5000), silence(2, 300))

	got := trimSilence(in)

	// Every sample of speech survives: 3 s of it, plus the trailing pad.
	want := 3.25
	if d := wavDurationSeconds(got); d != want {
		t.Errorf("duration = %.3fs, want %.3fs -- speech was cut from the head", d, want)
	}
}

func TestTrimSilenceAllSilentIsUnchanged(t *testing.T) {
	in := silence(3, 0)

	got := trimSilence(in)

	// Nothing clears the gate. Upload it whole rather than risk discarding
	// speech on a bad floor estimate.
	if len(got) != len(in) {
		t.Errorf("length = %d, want unchanged %d", len(got), len(in))
	}
}

func TestTrimSilenceShortInputIsUnchanged(t *testing.T) {
	in := make([]int16, trimFrameSamples) // one frame, too short to estimate from

	got := trimSilence(in)

	if len(got) != len(in) {
		t.Errorf("length = %d, want unchanged %d", len(got), len(in))
	}
}

func TestTrimSilenceEmptyInput(t *testing.T) {
	if got := trimSilence(nil); len(got) != 0 {
		t.Errorf("length = %d, want 0", len(got))
	}
}

func TestTrimSilenceNeverGrows(t *testing.T) {
	cases := map[string][]int16{
		"pure speech":   speech(2, 5000),
		"quiet speech":  speech(2, 200),
		"loud room":     silence(2, 4000),
		"speech at end": concat(silence(3, 100), speech(0.4, 8000)),
	}

	for name, in := range cases {
		if got := trimSilence(in); len(got) > len(in) {
			t.Errorf("%s: length = %d, grew past input %d", name, len(got), len(in))
		}
	}
}

func TestSilenceGateClearsRoomTone(t *testing.T) {
	rms := []float64{100, 110, 105, 120, 4000, 4200, 98}

	gate := silenceGate(rms)

	if gate <= 120 {
		t.Errorf("gate = %.1f, does not clear room tone at 120", gate)
	}
	if gate >= 4000 {
		t.Errorf("gate = %.1f, would reject speech at 4000", gate)
	}
}

func TestSilenceGateHasAbsoluteMinimum(t *testing.T) {
	// Digital silence: a purely relative gate would be zero and pass anything.
	if gate := silenceGate([]float64{0, 0, 0, 0}); gate != trimMinGate {
		t.Errorf("gate = %.1f, want %.1f", gate, trimMinGate)
	}
}

func TestFrameRMS(t *testing.T) {
	if got := frameRMS([]int16{0, 0, 0, 0}); got != 0 {
		t.Errorf("silence rms = %v, want 0", got)
	}
	if got := frameRMS([]int16{100, -100, 100, -100}); got != 100 {
		t.Errorf("square rms = %v, want 100", got)
	}
}

func TestWorthUploadingRejectsFumbledHotkey(t *testing.T) {
	cases := map[string][]int16{
		"instant toggle":  silence(0.2, 300),
		"brief room tone": silence(1, 300),
		"digital silence": silence(1.5, 0),
		"loud room tone":  silence(1.5, 4000),
	}

	for name, in := range cases {
		if worthUploading(in) {
			t.Errorf("%s: uploaded, want discarded", name)
		}
	}
}

func TestWorthUploadingKeepsSpeech(t *testing.T) {
	cases := map[string][]int16{
		// The short ones are the reason there is no hard multi-second floor:
		// "yes" and "no" are real dictation.
		"one short word":   concat(silence(0.2, 200), speech(0.4, 6000)),
		"quiet speaker":    concat(silence(0.3, 50), speech(0.6, 800)),
		"immediate word":   speech(0.6, 6000),
		"no quiet lead-in": speech(0.6, 800),
		"very quiet onset": speech(0.6, 200),
		"normal sentence":  concat(silence(0.5, 200), speech(1.2, 6000), silence(0.3, 200)),
	}

	for name, in := range cases {
		if !worthUploading(in) {
			t.Errorf("%s: discarded, want uploaded", name)
		}
	}
}

func TestWorthUploadingNeverJudgesLongCaptures(t *testing.T) {
	// Past the ceiling the capture was deliberate. Even if the gate reads it as
	// silence, dropping it would cost the user their words.
	if !worthUploading(silence(speechGateSeconds+0.5, 0)) {
		t.Error("long silent capture discarded, want uploaded unjudged")
	}
}

func TestHasSpeechEmptyInput(t *testing.T) {
	if hasSpeech(nil) {
		t.Error("nil reported as speech")
	}
	if hasSpeech(make([]int16, trimFrameSamples-1)) {
		t.Error("sub-frame capture reported as speech")
	}
}

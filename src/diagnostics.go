package voicetype

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"
)

func init() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
}

func logf(tag, format string, args ...any) {
	log.Printf("[%s] %s: %s", time.Now().Format("15:04:05"), tag, fmt.Sprintf(format, args...))
}

// retainWAV saves audio that failed or produced a suspicious transcription, so
// the evidence and speech the user already gave are not silently discarded.
func retainWAV(wav []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "voice-type")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "dictation-*.wav")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(wav); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// Recovery artifacts are private, explicit, and never automatically replayed.
func retainLocalResult(wav []byte, text string) string {
	path, err := retainWAV(wav)
	if err != nil {
		return "could not retain recording: " + err.Error()
	}
	if text != "" {
		if err := os.WriteFile(path+".txt", []byte(text), 0600); err != nil {
			return "audio retained at " + path + "; could not retain transcript: " + err.Error()
		}
		return "audio retained at " + path + "; transcript retained at " + path + ".txt"
	}
	return "audio retained at " + path
}

// Only aggregate amplitude is logged for empty cloud results; speech content
// and raw samples stay out of routine diagnostics. Never gate quiet speech.
func recordingSignal(samples []int16) (rms, peak float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, sample := range samples {
		value := float64(sample) / 32768
		sum += value * value
		peak = math.Max(peak, math.Abs(value))
	}
	return math.Sqrt(sum / float64(len(samples))), peak
}

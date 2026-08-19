// Command audio-audit runs a capture through the real endpoint trimmer and
// reports what it decided and why, without contacting the provider. A clipped
// syllable comes back as a slightly wrong transcript, never an error, so the
// trimmer is the one stage whose failures are otherwise invisible.
//
//	audio-audit -record 6s -o take.wav   # capture through the real Recorder
//	audio-audit take.wav                 # analyse; writes take.trimmed.wav
//	audio-audit -csv take.wav            # frame RMS series
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	voicetype "github.com/eriknovikov/voice-type"
)

func main() {
	record := flag.Duration("record", 0, "record for this long through the real Recorder instead of reading a file")
	out := flag.String("o", "", "where to write the recorded WAV (with -record)")
	csv := flag.Bool("csv", false, "print the per-frame RMS series as CSV and exit")
	flag.Parse()

	if *record > 0 {
		if err := doRecord(*record, *out); err != nil {
			fail(err)
		}
		return
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: audio-audit [-csv] <capture.wav>")
		fmt.Fprintln(os.Stderr, "       audio-audit -record 6s -o take.wav")
		os.Exit(2)
	}

	path := flag.Arg(0)
	samples, err := load(path)
	if err != nil {
		fail(err)
	}

	report := voicetype.AnalyzeTrim(samples)

	if *csv {
		fmt.Println("frame,ms,rms,dbfs")
		for i, v := range report.Frames {
			fmt.Printf("%d,%.0f,%.1f,%.1f\n", i, float64(i)*20, v, voicetype.DBFS(v))
		}
		return
	}

	printReport(path, report)

	trimmedPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".trimmed.wav"
	if err := os.WriteFile(trimmedPath, voicetype.WavEncode(voicetype.TrimSilence(samples)), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("\nwrote %s -- listen for a missing first or last syllable\n", trimmedPath)

	if len(report.Warnings) > 0 {
		os.Exit(1)
	}
}

// doRecord captures through the same Recorder the daemon uses, so the fixture
// has the same device, rate and buffering as a real dictation.
func doRecord(d time.Duration, out string) error {
	if out == "" {
		out = fmt.Sprintf("capture-%s.wav", time.Now().Format("150405"))
	}
	rec, err := voicetype.NewRecorder()
	if err != nil {
		return fmt.Errorf("open recorder: %w", err)
	}
	defer rec.Close()

	if err := rec.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	fmt.Fprintf(os.Stderr, "recording %s -- speak now\n", d)
	time.Sleep(d)
	samples := rec.Stop()

	if err := os.WriteFile(out, voicetype.WavEncode(samples), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d samples, %.2fs)\n", out, len(samples), float64(len(samples))/16000)
	return nil
}

func load(path string) ([]int16, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".pcm") {
		samples := make([]int16, len(raw)/2)
		for i := range samples {
			samples[i] = int16(uint16(raw[i*2]) | uint16(raw[i*2+1])<<8)
		}
		return samples, nil
	}
	return voicetype.WavDecode(raw)
}

func printReport(path string, r voicetype.TrimReport) {
	fmt.Printf("%s\n", path)
	fmt.Printf("  duration       %.2fs (%d samples, %d frames)\n", r.Seconds, r.Samples, len(r.Frames))
	fmt.Printf("  peak           %.0f rms (%.1f dBFS)\n", r.PeakRMS, voicetype.DBFS(r.PeakRMS))
	fmt.Printf("  quietest frame %.0f rms (%.1f dBFS)\n", r.GlobalFloor, voicetype.DBFS(r.GlobalFloor))
	fmt.Println()
	printEnd("head", r.Head)
	printEnd("tail", r.Tail)
	fmt.Println()
	fmt.Printf("  kept           %.2fs of %.2fs  (head -%.0f ms, tail -%.0f ms)\n",
		r.TrimmedSeconds, r.Seconds, r.Head.CutSeconds*1000, r.Tail.CutSeconds*1000)
	fmt.Printf("  cost           $%.6f -> $%.6f  (saves $%.6f)\n",
		voicetype.EstimatedCost(r.Seconds), voicetype.EstimatedCost(r.TrimmedSeconds),
		voicetype.EstimatedCost(r.Seconds-r.TrimmedSeconds))
	fmt.Printf("  gates          worthUploading=%v hasSpeech=%v voicedCadence=%v\n",
		r.WorthUploading, r.HasSpeech, r.VoicedCadence)

	fmt.Println()
	if len(r.Warnings) == 0 {
		fmt.Println("  OK -- no suspicious cuts")
		return
	}
	for _, w := range r.Warnings {
		fmt.Printf("  !! %s\n", w)
	}
}

func printEnd(name string, e voicetype.EndReport) {
	flag := ""
	if e.Contaminate {
		flag = "   <-- no quiet frames, floor is speech"
	}
	fmt.Printf("  %s window   floor %.0f  gate %.0f  (min %.0f / med %.0f / max %.0f)%s\n",
		name, e.Floor, e.Gate, e.WindowMin, e.WindowMedn, e.WindowMax, flag)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "audio-audit: %v\n", err)
	os.Exit(1)
}

package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
)

type fakeMacDirectTarget struct {
	snapshot        macTargetState
	captures        int
	inspections     int
	axCalls         []string
	unicodeCalls    [][]uint16
	axDelivery      macSubmission
	axErr           error
	unicodeDelivery macSubmission
	unicodeErr      error
	inspectErr      error
	captureErr      error
	beforeInspect   func(int)
	onAX            func()
	onUnicode       func()
	onClose         func()
	closes          int
	closeErr        error
}

func (f *fakeMacDirectTarget) capture(context.Context) (macDirectTarget, error) {
	f.captures++
	return f, f.captureErr
}

func (f *fakeMacDirectTarget) state(context.Context) (macTargetState, error) {
	f.inspections++
	if f.beforeInspect != nil {
		f.beforeInspect(f.inspections)
	}
	return f.snapshot, f.inspectErr
}

func (f *fakeMacDirectTarget) replaceSelectedText(_ context.Context, stringText string) (macSubmission, error) {
	f.axCalls = append(f.axCalls, stringText)
	if f.onAX != nil {
		f.onAX()
	}
	return f.axDelivery, f.axErr
}

func (f *fakeMacDirectTarget) postUnicode(_ context.Context, units []uint16) (macSubmission, error) {
	f.unicodeCalls = append(f.unicodeCalls, append([]uint16(nil), units...))
	if f.onUnicode != nil {
		f.onUnicode()
	}
	return f.unicodeDelivery, f.unicodeErr
}

func (f *fakeMacDirectTarget) Close() error {
	f.closes++
	if f.onClose != nil {
		f.onClose()
	}
	return f.closeErr
}

func newTestMacDirect(t *testing.T) (*macDirectInserter, *fakeMacDirectTarget) {
	t.Helper()
	target := &fakeMacDirectTarget{
		snapshot:   macTargetState{FocusMatches: true, Safety: macTargetEditable, SelectedText: macSelectedTextWritable},
		axDelivery: macSubmitted, unicodeDelivery: macSubmitted,
	}
	inserter := newMacDirectInserter(target.capture)
	t.Cleanup(func() { _ = inserter.Close() })
	return inserter, target
}

func TestMacDirectPinsAtFirstInsertionAndStopsOnFocusLoss(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	if target.captures != 0 {
		t.Fatal("constructor captured focus before first insertion")
	}
	for _, text := range []string{"Hello ", "world.\n🙂"} {
		if err := inserter.Insert(context.Background(), text); err != nil {
			t.Fatal(err)
		}
	}
	if target.captures != 1 || target.inspections < 4 || !reflect.DeepEqual(target.axCalls, []string{"Hello ", "world.\n🙂"}) || len(target.unicodeCalls) != 0 {
		t.Fatalf("did not reuse and recheck the pinned target: %+v", target)
	}
	target.snapshot.FocusMatches = false
	if err := inserter.Insert(context.Background(), "must not move to another field"); !errors.Is(err, errMacFocusChanged) {
		t.Fatalf("focus loss accepted: %v", err)
	}
	target.snapshot.FocusMatches = true
	if err := inserter.Insert(context.Background(), "must not resume stale live output"); !errors.Is(err, errMacInsertionStopped) {
		t.Fatalf("insertion resumed after focus loss: %v", err)
	}
	if target.captures != 1 || len(target.axCalls) != 2 {
		t.Fatal("recaptured focus or resubmitted after focus loss")
	}
}

func TestMacDirectRejectsUnsafeOrUnknownTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		safety macTargetSafety
	}{
		{"unknown", macTargetUnknown}, {"secure", macTargetSecure},
		{"read-only", macTargetReadOnly}, {"non-text", macTargetNonText}, {"unrecognized", 99},
	} {
		t.Run(test.name, func(t *testing.T) {
			inserter, target := newTestMacDirect(t)
			target.snapshot.Safety = test.safety
			if err := inserter.Insert(context.Background(), "do not submit"); !errors.Is(err, errMacUnsafeTarget) {
				t.Fatalf("unsafe target accepted: %v", err)
			}
			if len(target.axCalls) != 0 || len(target.unicodeCalls) != 0 {
				t.Fatal("unsafe target was mutated")
			}
		})
	}
	inserter, target := newTestMacDirect(t)
	target.snapshot.SelectedText = macSelectedTextUnknown
	if err := inserter.Insert(context.Background(), "unknown capability"); err == nil || len(target.unicodeCalls) != 0 || len(target.axCalls) != 0 {
		t.Fatal("unknown AX capability silently downgraded to Unicode", err)
	}
}

func TestMacDirectRejectsInvalidOrCanceledInputBeforeCapturing(t *testing.T) {
	for _, text := range []string{"", "bad\x00text", string([]byte{0xff})} {
		inserter, target := newTestMacDirect(t)
		if err := inserter.Insert(context.Background(), text); err == nil || target.captures != 0 {
			t.Fatal("invalid text captured a target", err)
		}
	}
	inserter, target := newTestMacDirect(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := inserter.Insert(ctx, "cancelled"); !errors.Is(err, context.Canceled) || target.captures != 0 {
		t.Fatal("cancelled insertion captured a target", err)
	}
}

func TestMacDirectUnicodePreservesScalarsAndMultilineText(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	target.snapshot.SelectedText = macSelectedTextUnsupported
	text := strings.Repeat("a", macUnicodeChunkUnits-1) + "🙂\nnaïve\t日本語e\u0301" + strings.Repeat("🚀", 20)
	submissions := 0
	if err := inserter.InsertWithSubmission(context.Background(), text, func() { submissions++ }); err != nil {
		t.Fatal(err)
	}
	var all []uint16
	for _, chunk := range target.unicodeCalls {
		if len(chunk) == 0 || len(chunk) > macUnicodeChunkUnits || (chunk[len(chunk)-1] >= 0xd800 && chunk[len(chunk)-1] <= 0xdbff) || (chunk[0] >= 0xdc00 && chunk[0] <= 0xdfff) {
			t.Fatalf("invalid UTF-16 chunk boundary: %x", chunk)
		}
		all = append(all, chunk...)
	}
	if string(utf16.Decode(all)) != text || len(target.axCalls) != 0 || submissions != 1 {
		t.Fatal("Unicode content, backend choice or submission count changed")
	}
	if target.inspections < len(target.unicodeCalls)+1 {
		t.Fatal("Unicode chunks did not recheck focus/safety")
	}
}

func TestMacDirectOnlyFallsBackBeforeAnyAXSubmission(t *testing.T) {
	backendErr := errors.New("native submission failed")
	for _, test := range []struct {
		name     string
		delivery macSubmission
		err      error
		wantErr  error
		unicode  bool
		notified int
	}{
		{"success", macSubmitted, nil, nil, false, 1},
		{"unsupported-before-mutation", macNotSubmitted, errMacSelectedTextUnsupported, nil, true, 1},
		{"unsupported-after-submission", macSubmitted, errMacSelectedTextUnsupported, errMacSelectedTextUnsupported, false, 1},
		{"unsupported-with-unknown-delivery", macSubmissionUnknown, errMacSelectedTextUnsupported, errMacAmbiguousDelivery, false, 0},
		{"partial-submission", macSubmitted, backendErr, backendErr, false, 1},
		{"unknown-failure", macSubmissionUnknown, backendErr, errMacAmbiguousDelivery, false, 0},
		{"unknown-success", macSubmissionUnknown, nil, errMacAmbiguousDelivery, false, 0},
		{"unrecognized-result", 99, nil, errMacAmbiguousDelivery, false, 0},
		{"no-submission", macNotSubmitted, nil, errMacNoDelivery, false, 0},
		{"failed-before-submission", macNotSubmitted, backendErr, backendErr, false, 0},
		{"focus-raced-with-submission", macNotSubmitted, errMacFocusChanged, errMacFocusChanged, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			inserter, target := newTestMacDirect(t)
			target.axDelivery, target.axErr = test.delivery, test.err
			if test.unicode {
				target.onAX = func() { target.snapshot.SelectedText = macSelectedTextUnsupported }
			}
			notified := 0
			err := inserter.InsertWithSubmission(context.Background(), "selection replacement", func() { notified++ })
			if !errors.Is(err, test.wantErr) || notified != test.notified {
				t.Fatalf("err=%v, notifications=%d; want err=%v, notifications=%d", err, notified, test.wantErr, test.notified)
			}
			if len(target.axCalls) != 1 || (len(target.unicodeCalls) != 0) != test.unicode {
				t.Fatalf("unsafe retry/fallback: AX=%d Unicode=%d", len(target.axCalls), len(target.unicodeCalls))
			}
			if err != nil {
				before := len(target.axCalls) + len(target.unicodeCalls)
				if retryErr := inserter.Insert(context.Background(), "never replay"); !errors.Is(retryErr, errMacInsertionStopped) {
					t.Fatalf("failed operation remained reusable: %v", retryErr)
				}
				if target.captures != 1 || before != len(target.axCalls)+len(target.unicodeCalls) {
					t.Fatal("failed operation was replayed or retargeted")
				}
			}
		})
	}
}

func TestMacDirectRechecksBeforeEveryMutation(t *testing.T) {
	for _, unicode := range []bool{false, true} {
		t.Run(map[bool]string{false: "AX", true: "Unicode"}[unicode], func(t *testing.T) {
			inserter, target := newTestMacDirect(t)
			if unicode {
				target.snapshot.SelectedText = macSelectedTextUnsupported
			}
			target.beforeInspect = func(call int) {
				if call == 2 {
					target.snapshot.FocusMatches = false
				}
			}
			if err := inserter.Insert(context.Background(), "do not submit after focus races"); !errors.Is(err, errMacFocusChanged) {
				t.Fatalf("focus race was not rejected: %v", err)
			}
			if len(target.axCalls) != 0 || len(target.unicodeCalls) != 0 {
				t.Fatal("submitted after focus changed during preparation")
			}
		})
	}
	for _, change := range []string{"focus", "secure", "read-only", "permission", "capability"} {
		t.Run("between-chunks-"+change, func(t *testing.T) {
			inserter, target := newTestMacDirect(t)
			target.snapshot.SelectedText = macSelectedTextUnsupported
			target.onUnicode = func() {
				switch change {
				case "focus":
					target.snapshot.FocusMatches = false
				case "secure":
					target.snapshot.Safety = macTargetSecure
				case "read-only":
					target.snapshot.Safety = macTargetReadOnly
				case "permission":
					target.inspectErr = errors.New("Accessibility permission revoked")
				case "capability":
					target.snapshot.SelectedText = macSelectedTextUnknown
				}
			}
			notified := 0
			err := inserter.InsertWithSubmission(context.Background(), strings.Repeat("x", macUnicodeChunkUnits*3), func() { notified++ })
			if err == nil || len(target.unicodeCalls) != 1 || notified != 1 {
				t.Fatalf("unsafe continuation: err=%v, chunks=%d, notifications=%d", err, len(target.unicodeCalls), notified)
			}
		})
	}
}

func TestMacDirectCapabilityChangesFailClosed(t *testing.T) {
	for _, access := range []macSelectedTextAccess{macSelectedTextWritable, macSelectedTextUnsupported} {
		inserter, target := newTestMacDirect(t)
		target.snapshot.SelectedText = access
		target.beforeInspect = func(call int) {
			if call == 2 {
				if access == macSelectedTextWritable {
					target.snapshot.SelectedText = macSelectedTextUnsupported
				} else {
					target.snapshot.SelectedText = macSelectedTextWritable
				}
			}
		}
		if err := inserter.Insert(context.Background(), "changed capabilities"); !errors.Is(err, errMacEditingChanged) {
			t.Fatal("used stale editing capabilities", err)
		}
		if len(target.axCalls) != 0 || len(target.unicodeCalls) != 0 {
			t.Fatal("mutated after capability drift")
		}
	}
}

func TestMacDirectCancellationStopsRemainingChunks(t *testing.T) {
	for _, duringInspection := range []bool{true, false} {
		inserter, target := newTestMacDirect(t)
		target.snapshot.SelectedText = macSelectedTextUnsupported
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if duringInspection {
			target.beforeInspect = func(call int) {
				if call == 2 {
					cancel()
				}
			}
		} else {
			target.onUnicode = cancel
		}
		notified := 0
		err := inserter.InsertWithSubmission(ctx, strings.Repeat("x", macUnicodeChunkUnits*2), func() { notified++ })
		wantChunks := 1
		if duringInspection {
			wantChunks = 0
		}
		if !errors.Is(err, context.Canceled) || len(target.unicodeCalls) != wantChunks || notified != wantChunks {
			t.Fatalf("cancelled insertion continued: %v, chunks=%d, notifications=%d", err, len(target.unicodeCalls), notified)
		}
		if err := inserter.Insert(context.Background(), "no replay after cancellation"); !errors.Is(err, errMacInsertionStopped) {
			t.Fatal("cancelled session resumed", err)
		}
	}
}

func TestMacDirectPartialUnicodeFailureIsNotRetried(t *testing.T) {
	for _, delivery := range []macSubmission{macSubmitted, macNotSubmitted, macSubmissionUnknown} {
		inserter, target := newTestMacDirect(t)
		target.snapshot.SelectedText = macSelectedTextUnsupported
		target.onUnicode = func() {
			if len(target.unicodeCalls) == 2 {
				target.unicodeDelivery = delivery
				target.unicodeErr = errors.New("native event failed")
			}
		}
		notified := 0
		err := inserter.InsertWithSubmission(context.Background(), strings.Repeat("x", macUnicodeChunkUnits*4), func() { notified++ })
		if err == nil || len(target.unicodeCalls) != 2 || notified != 1 || len(target.axCalls) != 0 {
			t.Fatalf("partial event sequence retried: %v, chunks=%d, notifications=%d", err, len(target.unicodeCalls), notified)
		}
		if err := inserter.Insert(context.Background(), "must not replay"); !errors.Is(err, errMacInsertionStopped) || len(target.unicodeCalls) != 2 {
			t.Fatal("partial sequence could be replayed", err)
		}
	}
}

func TestMacDirectAllowsRepeatedSuccessfulLiveText(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	for range 2 {
		if err := inserter.Insert(context.Background(), "very "); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(target.axCalls, []string{"very ", "very "}) || target.captures != 1 {
		t.Fatal("legitimate repeated words were deduplicated or retargeted")
	}
}

func TestMacDirectCloseIsIdempotentEvenOnFailure(t *testing.T) {
	unused, unprepared := newTestMacDirect(t)
	if err := unused.Close(); err != nil || unprepared.captures != 0 || unprepared.closes != 0 {
		t.Fatal("Close prepared an unused target", err)
	}
	if err := unused.Insert(context.Background(), "closed"); !errors.Is(err, ErrTextInserterClosed) {
		t.Fatal("unused closed inserter was reused", err)
	}
	inserter, target := newTestMacDirect(t)
	if err := inserter.Insert(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	want := errors.New("native cleanup failed")
	target.closeErr = want
	if err := inserter.Close(); !errors.Is(err, want) {
		t.Fatal("cleanup error lost", err)
	}
	if err := inserter.Close(); err != nil || target.closes != 1 {
		t.Fatal("Close retried native cleanup", err)
	}
	if err := inserter.Insert(context.Background(), "no reuse"); !errors.Is(err, ErrTextInserterClosed) || len(target.axCalls) != 1 {
		t.Fatal("closed target was reused", err)
	}
}

func TestMacDirectClosesPartiallyPreparedTargets(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	want := errors.New("native target preparation failed")
	target.captureErr = want
	if err := inserter.Insert(context.Background(), "hello"); !errors.Is(err, want) || target.inspections != 0 {
		t.Fatal("partial target preparation was used", err)
	}
	if err := inserter.Close(); err != nil || target.closes != 1 {
		t.Fatal("partial native target leaked", err)
	}
}

func TestMacDirectCloseWaitsForInFlightSubmission(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var active atomic.Bool
	target.onAX = func() {
		active.Store(true)
		close(entered)
		<-release
		active.Store(false)
	}
	target.onClose = func() {
		if active.Load() {
			t.Error("closed native target during a submission")
		}
	}
	inserted, closed := make(chan error, 1), make(chan error, 1)
	go func() { inserted <- inserter.Insert(context.Background(), "in flight") }()
	<-entered
	go func() { closed <- inserter.Close() }()
	close(release)
	if err := <-inserted; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil || target.closes != 1 {
		t.Fatal("in-flight target cleanup failed", err)
	}
}

func TestMacDirectFinalInsertionUsesSharedTimingAndCleanup(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	backend := DefaultInsertBackend()
	previous := textInserterBackends[backend]
	textInserterBackends[backend] = func(context.Context) (TextInserter, error) { return inserter, nil }
	t.Cleanup(func() { textInserterBackends[backend] = previous })
	var nativeHandoff time.Time
	var timings dictationTiming
	target.onAX = func() { nativeHandoff = time.Now() }
	target.onClose = func() {
		if timings.InsertionSubmitted.IsZero() || !timings.InsertionCleanupCompleted.IsZero() {
			t.Error("shared submission/cleanup timing is out of order")
		}
	}
	if err := insertTextTimed(context.Background(), "selected text\n🙂", &timings); err != nil {
		t.Fatal(err)
	}
	if timings.InsertionSubmitted.Before(nativeHandoff) || timings.InsertionCleanupCompleted.Before(timings.InsertionSubmitted) || target.closes != 1 {
		t.Fatal("final insertion timing or target cleanup lost")
	}
}

func TestMacDirectLiveInsertionDoesNotReplayAfterAmbiguousDelivery(t *testing.T) {
	inserter, target := newTestMacDirect(t)
	target.onAX = func() {
		if len(target.axCalls) == 2 {
			target.axDelivery, target.axErr = macSubmissionUnknown, errors.New("AX request failed after sending")
		}
	}
	stream := &fakeLocalStream{
		feedSnapshots: []fakeStreamSnapshot{
			{full: "Hello tentative", committed: "Hello "},
			{full: "Hello world tentative", committed: "Hello world "},
			{full: "Hello world again tentative", committed: "Hello world again "},
		},
		finalSnapshot: fakeStreamSnapshot{full: "Hello world again!", committed: "Hello world again!"},
	}
	session := newLocalStreamSession(context.Background(), streamingModeLive, stream, func(context.Context) (TextInserter, error) { return inserter, nil })
	for range 3 {
		session.OfferSamples([]int16{1})
	}
	result := session.Finish()
	if !errors.Is(result.Err, errMacAmbiguousDelivery) || result.Inserted != "Hello " || result.Text != "Hello world again!" {
		t.Fatalf("lost live recovery state: %+v", result)
	}
	if !reflect.DeepEqual(target.axCalls, []string{"Hello ", "world "}) || len(target.unicodeCalls) != 0 || target.captures != 1 || target.closes != 1 {
		t.Fatal("ambiguous live text was replayed, downgraded or retargeted")
	}
	if result.Timing.InsertionSubmitted.IsZero() || result.Timing.InsertionCleanupCompleted.IsZero() {
		t.Fatal("live timing lost after a later failure")
	}
}

func TestMacDirectLiveFailureRetainsWithoutFinalReplay(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("TMPDIR", t.TempDir())
	view := readSettingsTest(t)
	if _, err := UpsertProfile(ProfileUpdate{Revision: view.Revision, Name: "Local", Provider: providerLocal, ModelID: localModelUnifiedEnglish, StreamingMode: streamingModeLive}); err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	inserter, target := newTestMacDirect(t)
	target.onAX = func() {
		if len(target.axCalls) == 2 {
			target.axDelivery, target.axErr = macSubmissionUnknown, errors.New("AX request failed after sending")
		}
	}
	const transcript = "Hello recovered world."
	stream := newLocalStreamSession(context.Background(), streamingModeLive, &fakeLocalStream{
		feedSnapshots: []fakeStreamSnapshot{{full: "Hello tentative", committed: "Hello "}},
		finalSnapshot: fakeStreamSnapshot{full: transcript, committed: transcript},
	}, func(context.Context) (TextInserter, error) { return inserter, nil })
	stream.OfferSamples([]int16{1})
	service.recorder = &fixtureRecorder{}
	service.mu.Lock()
	service.state = stateRecording
	service.sessionCtx = context.Background()
	service.activeStream = stream
	service.mu.Unlock()
	operationErr := service.stopSessionMatching(0, false)
	if !errors.Is(operationErr, errMacAmbiguousDelivery) {
		t.Fatalf("ambiguous live failure was lost: %v", operationErr)
	}
	if !reflect.DeepEqual(target.axCalls, []string{"Hello ", "recovered world."}) || len(target.unicodeCalls) != 0 || target.captures != 1 || target.closes != 1 {
		t.Fatal("engine replayed, downgraded or retargeted ambiguous live output")
	}
	paths, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "voice-type", "dictation-*.wav.txt"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("missing private transcript recovery artifact: %v, %v", paths, err)
	}
	if text, err := os.ReadFile(paths[0]); err != nil || string(text) != transcript {
		t.Fatal("retained transcript was changed or lost", err)
	}
	if info, err := os.Stat(paths[0]); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("retained transcript is not private", err)
	}
	if status := service.Status(); status.State != "idle" || !status.LastAttemptFailed {
		t.Fatalf("insertion failure not visible: %+v", status)
	}
	records, err := service.runLog.records()
	if err != nil || len(records) == 0 || records[0].Outcome != "local_stream_error" {
		t.Fatalf("failure absent from diagnostics: %+v, %v", records, err)
	}
	raw, _ := json.Marshal(records)
	if strings.Contains(string(raw), transcript) {
		t.Fatal("transcript leaked into routine diagnostics")
	}
}

func TestMacDirectRejectsMissingAdapter(t *testing.T) {
	for _, capture := range []func(context.Context) (macDirectTarget, error){
		nil,
		func(context.Context) (macDirectTarget, error) { return nil, nil },
	} {
		inserter := newMacDirectInserter(capture)
		if err := inserter.Insert(context.Background(), "no adapter"); !errors.Is(err, ErrInsertBackendUnavailable) {
			t.Fatal("missing adapter accepted", err)
		}
		if err := inserter.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzMacDirectUnicodeRoundTrip(f *testing.F) {
	for _, text := range []string{"hello", "", "\x00", "\xff", strings.Repeat("a", macUnicodeChunkUnits-1) + "🙂", "\n\t日本語e\u0301🚀"} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 || validateInsertionText(text) != nil {
			t.Skip()
		}
		inserter, target := newTestMacDirect(t)
		target.snapshot.SelectedText = macSelectedTextUnsupported
		if err := inserter.Insert(context.Background(), text); err != nil {
			t.Fatal(err)
		}
		var units []uint16
		for _, chunk := range target.unicodeCalls {
			if len(chunk) == 0 || len(chunk) > macUnicodeChunkUnits {
				t.Fatal("invalid Unicode event chunk")
			}
			if chunk[0] >= 0xdc00 && chunk[0] <= 0xdfff || chunk[len(chunk)-1] >= 0xd800 && chunk[len(chunk)-1] <= 0xdbff {
				t.Fatal("surrogate pair split across event chunks")
			}
			units = append(units, chunk...)
		}
		if string(utf16.Decode(units)) != text {
			t.Fatal("Unicode event sequence changed the transcript")
		}
	})
}

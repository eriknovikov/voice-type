package voicetype

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeStreamSnapshot struct {
	full      string
	committed string
	err       error
}

type fakeLocalStream struct {
	mu            sync.Mutex
	feedSnapshots []fakeStreamSnapshot
	finalSnapshot fakeStreamSnapshot
	started       chan struct{}
	closed        chan struct{}
	once          sync.Once
	closeOnce     sync.Once
	block         bool
}

func (s *fakeLocalStream) feed(ctx context.Context, _ []int16) (string, string, error) {
	if s.started != nil {
		s.once.Do(func() { close(s.started) })
	}
	if s.block {
		<-ctx.Done()
		return "", "", ctx.Err()
	}
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.feedSnapshots) == 0 {
		return "", "", errors.New("unexpected feed")
	}
	snapshot := s.feedSnapshots[0]
	s.feedSnapshots = s.feedSnapshots[1:]
	return snapshot.full, snapshot.committed, snapshot.err
}

func (s *fakeLocalStream) finalize(ctx context.Context) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	return s.finalSnapshot.full, s.finalSnapshot.committed, s.finalSnapshot.err
}

func (s *fakeLocalStream) close() {
	if s.closed != nil {
		s.closeOnce.Do(func() { close(s.closed) })
	}
}

type recordingInserter struct {
	mu    sync.Mutex
	text  []string
	err   error
	close error
}

func (i *recordingInserter) Insert(_ context.Context, text string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.err != nil {
		return i.err
	}
	i.text = append(i.text, text)
	return nil
}

func (i *recordingInserter) Close() error { return i.close }

func (i *recordingInserter) inserted() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	var text string
	for _, part := range i.text {
		text += part
	}
	return text
}

func TestLiveStreamingInsertsCommittedTextOnlyAndNeverDuplicatesFinal(t *testing.T) {
	inserter := &recordingInserter{}
	stream := &fakeLocalStream{
		feedSnapshots: []fakeStreamSnapshot{{full: "Hello tentative", committed: "Hello "}},
		finalSnapshot: fakeStreamSnapshot{full: "Hello world.", committed: "Hello world."},
	}
	session := newLocalStreamSession(context.Background(), streamingModeLive, stream, func(context.Context) (TextInserter, error) {
		return inserter, nil
	})
	session.OfferSamples([]int16{1, 2, 3})
	result := session.Finish()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if got, want := inserter.inserted(), "Hello world."; got != want {
		t.Fatalf("inserted %q, want %q", got, want)
	}
	if result.Text != "Hello world." || result.Inserted != result.Text {
		t.Fatalf("stream result: %+v", result)
	}
}

func TestLiveStreamingRetainsOnFinalDisagreementWithoutCorrectingInsertedText(t *testing.T) {
	inserter := &recordingInserter{}
	stream := &fakeLocalStream{
		feedSnapshots: []fakeStreamSnapshot{{full: "The committed phrase", committed: "The "}},
		finalSnapshot: fakeStreamSnapshot{full: "A different phrase", committed: "The "},
	}
	session := newLocalStreamSession(context.Background(), streamingModeLive, stream, func(context.Context) (TextInserter, error) {
		return inserter, nil
	})
	session.OfferSamples([]int16{1, 2, 3})
	result := session.Finish()
	if result.Err == nil {
		t.Fatal("final disagreement was not reported")
	}
	if got, want := inserter.inserted(), "The "; got != want {
		t.Fatalf("already inserted text changed: %q, want %q", got, want)
	}
	if result.Inserted != "The " {
		t.Fatalf("reported inserted text: %q", result.Inserted)
	}
}

func TestProcessWhileRecordingUsesStreamButInsertsOnlyAfterStop(t *testing.T) {
	inserter := &recordingInserter{}
	stream := &fakeLocalStream{
		feedSnapshots: []fakeStreamSnapshot{{full: "tentative words", committed: "Stable "}},
		finalSnapshot: fakeStreamSnapshot{full: "Stable words.", committed: "Stable words."},
	}
	session := newLocalStreamSession(context.Background(), streamingModeProcess, stream, func(context.Context) (TextInserter, error) {
		return inserter, nil
	})
	session.OfferSamples([]int16{1, 2, 3})
	result := session.Finish()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Text != "Stable words." || inserter.inserted() != "" {
		t.Fatalf("process-while-recording result=%+v inserted=%q", result, inserter.inserted())
	}
}

func TestRecorderStreamsCopiedChunksAndRetainsFullRecording(t *testing.T) {
	recorder := &Recorder{}
	chunks := make(chan []int16, 1)
	recorder.SetChunkSink(func(samples []int16) { chunks <- samples })
	input := []int16{17, -29, 300}
	if _, err := recorder.acceptSamples(input); err != nil {
		t.Fatal(err)
	}
	input[0] = 0
	chunk := <-chunks
	if chunk[0] != 17 {
		t.Fatal("stream sink received a view into mutable recorder input")
	}
	full, err := recorder.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 3 || full[0] != 17 || full[1] != -29 || full[2] != 300 {
		t.Fatalf("streaming consumed or changed the retained recording: %v", full)
	}
}

func TestLocalStreamCancellationStopsWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeLocalStream{started: make(chan struct{}), closed: make(chan struct{}), block: true}
	session := newLocalStreamSession(ctx, streamingModeProcess, stream, nil)
	session.OfferSamples([]int16{1, 2, 3})
	select {
	case <-stream.started:
	case <-time.After(time.Second):
		t.Fatal("stream worker did not begin processing audio")
	}
	cancel()
	done := make(chan struct{})
	go func() {
		session.Abort()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled stream worker did not stop")
	}
	select {
	case <-stream.closed:
	default:
		t.Fatal("native stream was not closed after cancellation")
	}
}

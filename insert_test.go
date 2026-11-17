package voicetype

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func TestDefaultInsertionBackend(t *testing.T) {
	for _, test := range []struct {
		platform string
		want     InsertBackend
	}{
		{"linux", InsertBackendIBus},
		{"darwin", InsertBackendMacDirect},
		{"windows", ""},
	} {
		if got := defaultInsertionBackend(test.platform); got != test.want {
			t.Errorf("%s: got %q, want %q", test.platform, got, test.want)
		}
	}
	if DefaultInsertBackend() != defaultInsertionBackend(runtime.GOOS) {
		t.Fatal("default does not match the current platform")
	}
}

func TestDefaultInsertionUsesRegisteredFactory(t *testing.T) {
	backend := DefaultInsertBackend()
	old := textInserterBackends[backend]
	t.Cleanup(func() { textInserterBackends[backend] = old })
	want := errors.New("test factory called")
	textInserterBackends[backend] = func(context.Context) (TextInserter, error) { return nil, want }
	if err := insertText(context.Background(), "hello"); !errors.Is(err, want) {
		t.Fatalf("final insertion did not use platform factory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := newLocalStreamSessionWithContext(ctx, cancel, streamingModeLive, nil, nil)
	if _, err := session.newInserter(ctx); !errors.Is(err, want) {
		t.Fatalf("live insertion did not use platform factory: %v", err)
	}
	cancel()
	<-session.done
}

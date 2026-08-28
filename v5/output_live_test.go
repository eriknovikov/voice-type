package voicetype

import (
	"os"
	"testing"
	"time"
)

// TestLiveType sends a fixed sentinel to the focused disposable text field.
// It is intentionally opt-in because it creates a real virtual keyboard and
// types into whichever application currently has focus.
//
//	VOICE_TYPE_LIVE=1 go test -run TestLiveType -v
func TestLiveType(t *testing.T) {
	if os.Getenv("VOICE_TYPE_LIVE") != "1" {
		t.Skip("set VOICE_TYPE_LIVE=1 to type into the focused text field")
	}

	typer, err := newTyper()
	if err != nil {
		t.Fatalf("create virtual keyboard: %v", err)
	}
	defer typer.Close()

	for seconds := 3; seconds > 0; seconds-- {
		t.Logf("focus a disposable text field; typing in %d...", seconds)
		time.Sleep(time.Second)
	}

	const sentinel = `Voice Type 123: !@#$%^&*() []{} \/|;:'",.<>? cafe: café — emoji: 🙂`
	t.Logf("typing sentinel: %s", sentinel)
	if err := typer.Type(sentinel); err != nil {
		t.Fatalf("Type: %v", err)
	}
}

package voicetype

import (
	"errors"
	"time"
)

const noSpeechWarning = "No speech detected. Try speaking louder or check your microphone."
const dictationWarningDuration = 4 * time.Second

// This sentinel is an internal result, not a failed dictation. The engine
// consumes it before a toggle result reaches the desktop error handler.
var errNoSpeech = errors.New(noSpeechWarning)

// Call with d.mu held. Expiry is read by the existing status/tray polls, so no
// delayed callback can overwrite a newer recording or outlive the engine.
func (d *engine) currentWarning(now time.Time) string {
	if d.state != stateIdle || d.lastAttemptFailed || d.configError != "" || !now.Before(d.warningUntil) {
		return ""
	}
	return noSpeechWarning
}

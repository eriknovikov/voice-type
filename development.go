package voicetype

import "os"

const developmentEnvironment = "VOICE_TYPE_DEV"

// Stamped by development builds so Finder launches remain isolated too.
var developmentBuild = "false"

// developmentMode is enabled by the Wails development watcher. It isolates the
// running binary from the installed application without changing normal builds.
func developmentMode() bool {
	return developmentBuild == "true" || os.Getenv(developmentEnvironment) == "1"
}

func applicationIdentity() (id, name string) {
	if developmentMode() {
		return "io.github.eriknovikov.voice-type.dev", "Voice Type Dev"
	}
	return "io.github.eriknovikov.voice-type", "Voice Type"
}

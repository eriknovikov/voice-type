// Command voice-type is the dictation daemon. Everything it does lives in the
// library package; this file exists only to hold the linker-stamped version and
// hand it over.
package main

import voicetype "github.com/eriknovikov/voice-type"

// version is stamped at build time from the VERSION file, which is the single
// source of truth the Makefile and release CI both read:
// -ldflags "-X main.version=$(cat VERSION)". An unstamped `go build` reports
// "dev" rather than claiming a release number it may not be.
var version = "dev"

func main() {
	voicetype.Run(version)
}

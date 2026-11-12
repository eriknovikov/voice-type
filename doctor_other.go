//go:build !linux

package voicetype

import (
	"fmt"
	"io"
	"runtime"
)

// RunDoctor prints the environment diagnostics available on this platform.
func RunDoctor(version string, out io.Writer) int {
	fmt.Fprintf(out, "version: %s\nplatform: %s/%s\nkeyring: system\n", version, runtime.GOOS, runtime.GOARCH)
	return 0
}

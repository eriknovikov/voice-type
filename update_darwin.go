package voicetype

import (
	"context"
	"io"
	"os/exec"
	"runtime"
	"time"
)

func platformRunUpdate(version string, out, errOut io.Writer) int {
	return runMacUpdate(version, runtime.GOARCH, out, errOut, lookupMacUpdate, func(url string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "/usr/bin/open", url).Run()
	})
}

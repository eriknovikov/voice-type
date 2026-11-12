//go:build linux

package voicetype

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/godbus/dbus/v5"
)

// RunDoctor prints read-only environment diagnostics. It never reads secrets.
func RunDoctor(version string, out io.Writer) int {
	path, _ := credentialPath(providerOpenAI)
	fmt.Fprintf(out, "version: %s\nplatform: %s/%s\nsession: %s\nconfig: %s (%s)\ncredentials: %s (file: %s, %s)\nsecret service: %s\n",
		version, runtime.GOOS, runtime.GOARCH, os.Getenv("XDG_SESSION_TYPE"),
		configFilePath(), fileState(configFilePath()), currentCredentialStorage(), path, fileState(path),
		secretService())
	return 0
}

func fileState(path string) string {
	if _, err := os.Stat(path); err == nil {
		return "exists"
	}
	return "missing"
}

func secretService() string {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	defer conn.Close()
	var owner string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, "org.freedesktop.secrets").Store(&owner); err != nil {
		return "absent"
	}
	var pid uint32
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, owner).Store(&pid); err != nil {
		return owner
	}
	comm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	return owner + " (" + strings.TrimSpace(string(comm)) + ")"
}

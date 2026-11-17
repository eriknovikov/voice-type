package voicetype

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestMain(m *testing.M) { os.Exit(runIsolatedTests(m)) }

func runIsolatedTests(m *testing.M) int {
	// Child test processes keep their parent's sandbox and contention fixtures.
	// An explicitly opted-in provider test may read the caller's configuration.
	if os.Getenv("VOICE_TYPE_TEST_SANDBOX") == "" && os.Getenv("VOICE_TYPE_TEST_LIVE_WAV") == "" {
		home, err := os.MkdirTemp("", "voice-type-tests-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(home)
		for key, value := range map[string]string{
			"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "state"),
			"XDG_DATA_HOME": filepath.Join(home, "data"), "XDG_CONFIG_HOME": filepath.Join(home, "config"),
			"XDG_RUNTIME_DIR": filepath.Join(home, "runtime"), "VOICE_TYPE_TEST_SANDBOX": "1",
		} {
			if err := os.Setenv(key, value); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}
	if os.Getenv("VOICE_TYPE_TEST_SYSTEM_KEYRING") != "1" && os.Getenv("VOICE_TYPE_TEST_LIVE_WAV") == "" {
		keyring.MockInit()
	}
	// Unit tests use synthetic recording and permissions. Real platform probes
	// must opt in and instantiate systemPermissionAccess directly.
	newPermissionAccess = func() permissionAccess {
		return &testPermissionAccess{current: PermissionStatus{Platform: runtime.GOOS, Microphone: PermissionGranted, Accessibility: PermissionGranted}}
	}
	return m.Run()
}

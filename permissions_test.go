package voicetype

import (
	"os"
	"strings"
	"testing"
)

type testPermissionAccess struct {
	current  PermissionStatus
	requests []string
	settings []string
	check    func()
}

func (p *testPermissionAccess) status() PermissionStatus { return p.current }
func (p *testPermissionAccess) request(permission string) error {
	if p.check != nil {
		p.check()
	}
	p.requests = append(p.requests, permission)
	return nil
}
func (p *testPermissionAccess) openSettings(permission string) error {
	p.settings = append(p.settings, permission)
	return nil
}

func TestPermissionPreflightStates(t *testing.T) {
	for _, state := range []PermissionState{PermissionUnknown, PermissionGranted, PermissionDenied, PermissionRestricted, PermissionNotRequired, "unexpected"} {
		status := PermissionStatus{Platform: "darwin", Microphone: state, Accessibility: PermissionGranted}
		if err := status.recordingError(true); (err == nil) != (state == PermissionGranted) {
			t.Errorf("microphone %s: %v", state, err)
		}
		status.Microphone, status.Accessibility = PermissionGranted, state
		if err := status.recordingError(true); (err == nil) != (state == PermissionGranted) {
			t.Errorf("accessibility %s: %v", state, err)
		}
		if err := status.recordingError(false); err != nil {
			t.Errorf("microphone meter unnecessarily requires Accessibility: %v", err)
		}
	}
	status := PermissionStatus{Platform: "linux", Microphone: PermissionNotRequired, Accessibility: PermissionNotRequired}
	if err := status.recordingError(true); err != nil {
		t.Fatal("Linux gained macOS permission requirements", err)
	}
}

func TestPermissionPreflightBeforeConfigurationAndCapture(t *testing.T) {
	setupEngineTest(t)
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	access := &testPermissionAccess{current: PermissionStatus{Platform: "darwin", Microphone: PermissionDenied, Accessibility: PermissionGranted}}
	service.permissions = access
	if err := os.WriteFile(configFilePath(), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.Toggle(); err == nil || !strings.Contains(err.Error(), "microphone permission") {
		t.Fatalf("permission gate did not precede configuration/provider setup: %v", err)
	}
	if service.state != stateIdle || !service.lastAttemptFailed || len(access.requests) != 0 {
		t.Fatal("failed preflight changed session state or prompted")
	}
	if err := service.StartMicrophoneTest(""); err == nil || !strings.Contains(err.Error(), "microphone permission") {
		t.Fatalf("meter bypassed preflight: %v", err)
	}
	if service.testRecorder != nil {
		t.Fatal("preflight opened the microphone")
	}
	// A permission revoked after an earlier successful grant is read fresh.
	access.current.Microphone, access.current.Accessibility = PermissionGranted, PermissionDenied
	if err := service.Toggle(); err == nil || !strings.Contains(err.Error(), "Accessibility") {
		t.Fatalf("revoked Accessibility was ignored: %v", err)
	}
}

func TestPermissionRequestsAreExplicitAndUnlocked(t *testing.T) {
	access := &testPermissionAccess{current: PermissionStatus{Platform: "darwin", Microphone: PermissionUnknown, Accessibility: PermissionDenied}}
	service := &engine{permissions: access}
	bridge := &Bridge{engine: service, trayQuery: func() bool { return false }}
	access.check = func() {
		if !service.mu.TryLock() {
			t.Fatal("permission request held the engine mutex")
		}
		service.mu.Unlock()
	}
	_ = bridge.Permissions()
	_ = bridge.Info()
	if len(access.requests) != 0 {
		t.Fatal("read-only status prompted")
	}
	if _, err := bridge.RequestPermission("screen-recording"); err == nil {
		t.Fatal("unexpected permission accepted")
	}
	if _, err := bridge.RequestPermission("microphone"); err != nil {
		t.Fatal(err)
	}
	if err := bridge.OpenPermissionSettings("accessibility"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(access.requests, ",") != "microphone" || strings.Join(access.settings, ",") != "accessibility" {
		t.Fatal("wrong permission action", access)
	}
}

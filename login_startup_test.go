package voicetype

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

type fakeLoginStartup struct {
	current LoginStartupStatus
	writes  []bool
	err     error
}

func (s *fakeLoginStartup) status() LoginStartupStatus { return s.current }
func (s *fakeLoginStartup) setEnabled(enabled bool) error {
	s.writes = append(s.writes, enabled)
	if s.err == nil {
		s.current.Enabled = enabled
		s.current.State = "disabled"
		if enabled {
			s.current.State = "enabled"
		}
	}
	return s.err
}

func TestMacLoginInitializationNeverOverridesOSChoice(t *testing.T) {
	isolatedPreferences(t)
	login := &fakeLoginStartup{current: LoginStartupStatus{State: "requires_approval"}}
	bridge := &Bridge{prefs: defaultPreferences("darwin"), loginControl: login}
	bridge.initializePreferencesForPlatform("darwin")
	if bridge.preferencesError != "" || len(login.writes) != 0 {
		t.Fatal("cold start changed login registration", bridge.preferencesError, login.writes)
	}
	bridge.prefs.OutputMode = "paste"
	if err := bridge.savePreferences(bridge.prefs, false); err != nil || len(login.writes) != 0 {
		t.Fatal("unrelated preference save overrode login denial", err, login.writes)
	}
	if err := bridge.SetLoginStartup(true); err != nil || !reflect.DeepEqual(login.writes, []bool{true}) || !login.current.Enabled {
		t.Fatal("explicit enable could not retry the same stored intent", err, login.writes)
	}
}

func TestMacLoginRollbackAndEffectiveStatus(t *testing.T) {
	isolatedPreferences(t)
	login := &fakeLoginStartup{current: LoginStartupStatus{State: "enabled", Enabled: true}}
	permissions := &testPermissionAccess{current: PermissionStatus{Platform: "darwin"}}
	bridge := &Bridge{prefs: defaultPreferences("darwin"), loginControl: login, engine: &engine{permissions: permissions}, trayQuery: func() bool { return false }}
	if !bridge.Info().LoginStartup.Enabled {
		t.Fatal("effective login status not reported")
	}
	login.current = LoginStartupStatus{State: "requires_approval"}
	if info := bridge.Info(); info.LoginStartup.Enabled || info.LoginStartup.State != "requires_approval" || !info.Preferences.Autostart {
		t.Fatal("stored intent masked an external login disable", info)
	}
	bridge.engine = nil
	if err := writePreferences(bridge.prefs); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(preferencesPath(), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(preferencesPath(), 0600) })
	if err := bridge.SetLoginStartup(true); err == nil {
		t.Fatal("failed preference write was accepted")
	}
	if !reflect.DeepEqual(login.writes, []bool{true, false}) || login.current.Enabled {
		t.Fatal("failed save did not restore the effective OS state", login.writes)
	}
	login.writes = nil
	if err := bridge.savePreferences(bridge.prefs, false); err == nil || len(login.writes) != 0 {
		t.Fatal("failed unrelated save touched login registration", err, login.writes)
	}
}

func TestLoginRegistrationFailureStaysVisible(t *testing.T) {
	isolatedPreferences(t)
	want := errors.New("registration needs OS approval")
	login := &fakeLoginStartup{current: LoginStartupStatus{State: "requires_approval"}, err: want}
	bridge := &Bridge{prefs: defaultPreferences("darwin"), loginControl: login}
	if err := bridge.SetLoginStartup(true); !errors.Is(err, want) || bridge.preferencesError == "" || login.current.Enabled {
		t.Fatal("registration failure was hidden", err)
	}
}

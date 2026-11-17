package voicetype

import "errors"

func (systemPermissionAccess) status() PermissionStatus {
	return PermissionStatus{Platform: "linux", Microphone: PermissionNotRequired, Accessibility: PermissionNotRequired}
}

func (systemPermissionAccess) request(string) error {
	return errors.New("macOS permission requests are not available on Linux")
}

func (systemPermissionAccess) openSettings(string) error {
	return errors.New("macOS permission settings are not available on Linux")
}

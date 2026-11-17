package voicetype

/*
#cgo darwin CFLAGS: -fblocks
#cgo darwin LDFLAGS: -framework AVFoundation -framework ApplicationServices -framework AppKit
#include "permissions_darwin.h"
*/
import "C"

import "errors"

func (systemPermissionAccess) status() PermissionStatus {
	microphone := PermissionUnknown
	switch C.vt_microphone_status() {
	case 1:
		microphone = PermissionGranted
	case 2:
		microphone = PermissionDenied
	case 3:
		microphone = PermissionRestricted
	}
	accessibility := PermissionDenied
	if C.vt_accessibility_trusted() != 0 {
		accessibility = PermissionGranted
	}
	return PermissionStatus{Platform: "darwin", Microphone: microphone, Accessibility: accessibility}
}

func (p systemPermissionAccess) request(permission string) error {
	switch permission {
	case "microphone":
		if p.status().Microphone == PermissionRestricted {
			return errors.New("microphone access is restricted by system policy; contact your administrator")
		}
		if C.vt_request_microphone() == 0 {
			return errors.New("microphone permission must be requested from the Voice Type .app bundle")
		}
	case "accessibility":
		C.vt_request_accessibility()
	default:
		return validatePermission(permission)
	}
	return nil
}

func (systemPermissionAccess) openSettings(permission string) error {
	if err := validatePermission(permission); err != nil {
		return err
	}
	if permission == "microphone" {
		C.vt_open_permission_settings(1)
	} else {
		C.vt_open_permission_settings(0)
	}
	return nil
}

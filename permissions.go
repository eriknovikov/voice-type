package voicetype

import (
	"errors"
	"fmt"
)

type PermissionState string

const (
	PermissionUnknown     PermissionState = "unknown"
	PermissionGranted     PermissionState = "granted"
	PermissionDenied      PermissionState = "denied"
	PermissionRestricted  PermissionState = "restricted"
	PermissionNotRequired PermissionState = "not_required"
)

type PermissionStatus struct {
	Platform      string          `json:"platform"`
	Microphone    PermissionState `json:"microphone"`
	Accessibility PermissionState `json:"accessibility"`
}

type permissionAccess interface {
	status() PermissionStatus
	request(permission string) error
	openSettings(permission string) error
}

type systemPermissionAccess struct{}

var newPermissionAccess = func() permissionAccess { return systemPermissionAccess{} }

func (d *engine) permissionStatus() PermissionStatus {
	if d.permissions != nil {
		return d.permissions.status()
	}
	return newPermissionAccess().status()
}

func (p PermissionStatus) recordingError(inserting bool) error {
	if p.Platform != "darwin" {
		return nil
	}
	if p.Microphone != PermissionGranted {
		return fmt.Errorf("microphone permission is %s; open General and allow Voice Type in System Settings > Privacy & Security > Microphone", p.Microphone)
	}
	if inserting && p.Accessibility != PermissionGranted {
		return fmt.Errorf("Accessibility permission is %s; open General and allow Voice Type in System Settings > Privacy & Security > Accessibility", p.Accessibility)
	}
	return nil
}

func validatePermission(permission string) error {
	if permission != "microphone" && permission != "accessibility" {
		return errors.New("choose microphone or accessibility permission")
	}
	return nil
}

func (b *Bridge) permissionAccess() permissionAccess {
	if b.engine != nil && b.engine.permissions != nil {
		return b.engine.permissions
	}
	return newPermissionAccess()
}

// Requests are explicit frontend actions. Status and recording preflight never prompt.
func (b *Bridge) Permissions() PermissionStatus { return b.permissionAccess().status() }

func (b *Bridge) RequestPermission(permission string) (PermissionStatus, error) {
	if err := validatePermission(permission); err != nil {
		return PermissionStatus{}, err
	}
	access := b.permissionAccess()
	if err := access.request(permission); err != nil {
		return access.status(), err
	}
	return access.status(), nil
}

func (b *Bridge) OpenPermissionSettings(permission string) error {
	if err := validatePermission(permission); err != nil {
		return err
	}
	return b.permissionAccess().openSettings(permission)
}

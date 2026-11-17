package voicetype

import "github.com/gen2brain/malgo"

func newCaptureDevice() (*malgoCapture, error) {
	capture, err := newMalgoCapture([]malgo.Backend{malgo.BackendCoreaudio})
	if err != nil {
		return nil, err
	}
	capture.permission = func() error { return (systemPermissionAccess{}).status().recordingError(false) }
	return capture, nil
}

// Context enumeration never creates or starts a capture device or requests TCC access.
func ListAudioSources() ([]AudioSource, error) {
	capture, err := newCaptureDevice()
	if err != nil {
		return nil, err
	}
	defer capture.close()
	devices, err := capture.context.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}
	sources := []AudioSource{}
	for _, device := range devices {
		sources = append(sources, AudioSource{ID: device.ID.String(), Name: device.Name()})
	}
	return sources, nil
}

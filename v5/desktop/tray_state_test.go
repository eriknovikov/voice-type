package desktop

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

func TestTrayFailureColorAndActiveStates(t *testing.T) {
	for _, test := range []struct {
		status Status
		state  string
	}{
		{Status{State: "idle"}, "idle"},
		{Status{State: "idle", LastAttemptFailed: true}, "error"},
		{Status{State: "testing", LastAttemptFailed: true}, "error"},
		{Status{State: "recording", LastAttemptFailed: true}, "recording"},
		{Status{State: "transcribing", LastAttemptFailed: true}, "transcribing"},
	} {
		if state := trayState(test.status); state != test.state {
			t.Fatalf("%+v -> %s", test.status, state)
		}
	}
	colors := map[string]color.RGBA{"idle": {145, 150, 158, 255}, "recording": {116, 195, 170, 255}, "transcribing": {237, 188, 105, 255}, "error": {245, 110, 106, 255}}
	for state, want := range colors {
		icon, err := png.Decode(bytes.NewReader(trayIcon(state)))
		if err != nil {
			t.Fatal(err)
		}
		got := color.RGBAModel.Convert(icon.At(4, 16)).(color.RGBA)
		if got != want {
			t.Fatalf("%s color = %v, want %v", state, got, want)
		}
	}
}

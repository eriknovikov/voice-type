package voicetype

import "testing"

func TestAudioServerFailurePreservesMicrophone(t *testing.T) {
	setupEngineTest(t)
	t.Setenv("PULSE_SERVER", "unix:"+t.TempDir()+"/unavailable-pulse")
	settings, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	id := "bluetooth-saved"
	_, err = SaveSettings(SettingsUpdate{Revision: settings.Revision, Microphone: &id})
	if err != nil {
		t.Fatal(err)
	}
	service, err := startEngine("test")
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if _, err := service.Microphones(); err == nil {
		t.Fatal("unavailable server did not report failure")
	}
	saved, err := ReadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Microphone != id {
		t.Fatal("server failure reset source preference")
	}
	if err := service.StartMicrophoneTest(id); err == nil {
		t.Fatal("test started without audio server")
	}
	saved, err = ReadSettings()
	if err != nil || saved.Microphone != id {
		t.Fatal("capture failure reset source preference", err)
	}
}

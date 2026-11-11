//go:build !linux

package voicetype

type nativeShortcutSettings struct{}

func (nativeShortcutSettings) paths() ([]string, error) {
	return nil, errGNOMESettingsUnavailable
}
func (nativeShortcutSettings) setPaths([]string) error {
	return errGNOMESettingsUnavailable
}
func (nativeShortcutSettings) binding(string) (string, error) {
	return "", errGNOMESettingsUnavailable
}
func (nativeShortcutSettings) set(string, string, string) error {
	return errGNOMESettingsUnavailable
}
func (nativeShortcutSettings) reset(string) error { return errGNOMESettingsUnavailable }

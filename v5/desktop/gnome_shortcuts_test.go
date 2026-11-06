package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGNOMEShortcutsWithoutPortal(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	original := "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/custom0/"
	legacy := shortcutBase + "daemon/"
	initial := map[string]any{mediaKeysSchema + "|custom-keybindings": []string{original, legacy}, customKeySchema + ":" + legacy + "|binding": "<Control><Alt>space", customKeySchema + ":" + original + "|binding": "F9"}
	raw, _ := json.Marshal(initial)
	if err := os.WriteFile(statePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/python3
import os,sys,json,ast
p=os.environ['VOICE_TYPE_GSETTINGS_TEST_STATE']
s=json.load(open(p));a=sys.argv[1:];k=a[1]+'|'+(a[2] if len(a)>2 else '')
if a[0]=='get': print(repr(s.get(k,[] if a[2]=='custom-keybindings' else '')))
elif a[0]=='set':
 s[k]=ast.literal_eval(a[3]);json.dump(s,open(p,'w'))
elif a[0]=='reset-recursively':
 s={k:v for k,v in s.items() if not k.startswith(a[1]+'|')};json.dump(s,open(p,'w'))
else: sys.exit(1)
`
	if err := os.WriteFile(filepath.Join(dir, "gsettings"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VOICE_TYPE_GSETTINGS_TEST_STATE", statePath)
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	prefs := Preferences{Shortcut: "Ctrl+Space"}
	if err := applyGNOMEShortcuts(prefs, false); err != nil {
		t.Fatal(err)
	}
	if err := applyGNOMEShortcuts(prefs, false); err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		raw, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]any
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state := read()
	paths := state[mediaKeysSchema+"|custom-keybindings"].([]any)
	if len(paths) != 2 || paths[0] != original {
		t.Fatal("other shortcuts lost or duplicate paths added")
	}
	if _, exists := state[customKeySchema+":"+legacy+"|binding"]; exists {
		t.Fatal("legacy daemon shortcut retained")
	}
	transcriptionKey := customKeySchema + ":" + shortcutBase + "transcription/|binding"
	if state[transcriptionKey] != "<control>space" {
		t.Fatalf("binding: %v", state[transcriptionKey])
	}
	b := &Bridge{prefs: prefs} // No Wails app: any portal registration would panic.
	if err := b.BeginShortcutCapture(); err != nil {
		t.Fatal(err)
	}
	if read()[transcriptionKey] != "" {
		t.Fatal("shortcut active during capture")
	}
	if err := b.EndShortcutCapture(); err != nil {
		t.Fatal(err)
	}
	if read()[transcriptionKey] != "<control>space" {
		t.Fatal("shortcut not restored")
	}
	disabled := prefs
	disabled.Shortcut = ""
	if err := applyGNOMEShortcuts(disabled, false); err != nil {
		t.Fatal(err)
	}
	if read()[transcriptionKey] != "" {
		t.Fatal("cleared shortcut still bound")
	}
	conflict := prefs
	conflict.Shortcut = "F9"
	if err := applyGNOMEShortcuts(conflict, false); err == nil {
		t.Fatal("conflicting user shortcut accepted")
	}
	if err := RemoveGNOMEShortcuts(); err != nil {
		t.Fatal(err)
	}
	state = read()
	paths = state[mediaKeysSchema+"|custom-keybindings"].([]any)
	if len(paths) != 1 || paths[0] != original {
		t.Fatal("uninstall changed another shortcut")
	}
}

//go:build linux

package voicetype

/*
#cgo pkg-config: gio-2.0
#include <gio/gio.h>
#include <stdlib.h>

static GSettings *voice_type_settings(const char *id, const char *path) {
 GSettingsSchemaSource *source = g_settings_schema_source_get_default();
 if (!source) return NULL;
 GSettingsSchema *schema = g_settings_schema_source_lookup(source, id, TRUE);
 if (!schema) return NULL;
 GSettings *settings = g_settings_new_full(schema, NULL, path);
 g_settings_schema_unref(schema);
 return settings;
}
static gboolean voice_type_set_paths(GSettings *settings, char **paths) {
 return g_settings_set_strv(settings, "custom-keybindings", (const gchar * const *)paths);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unsafe"
)

// Use GSettings' typed API rather than trying to extract paths from printed
// GVariant text. This preserves every entry, including unfamiliar path names.
type nativeShortcutSettings struct{}

func openShortcutSettings(schema, path string) (*C.GSettings, error) {
	if path != "" && (!strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") || strings.Contains(path, "//") || strings.ContainsRune(path, 0)) {
		return nil, errors.New("a desktop shortcut has an invalid settings path; no registration was changed")
	}
	id := C.CString(schema)
	defer C.free(unsafe.Pointer(id))
	var location *C.char
	if path != "" {
		location = C.CString(path)
		defer C.free(unsafe.Pointer(location))
	}
	settings := C.voice_type_settings(id, location)
	if settings == nil {
		return nil, errGNOMESettingsUnavailable
	}
	return settings, nil
}

func (nativeShortcutSettings) paths() ([]string, error) {
	settings, err := openShortcutSettings(mediaKeysSchema, "")
	if err != nil {
		return nil, err
	}
	defer C.g_object_unref(C.gpointer(settings))
	key := C.CString("custom-keybindings")
	defer C.free(unsafe.Pointer(key))
	vector := C.g_settings_get_strv(settings, key)
	defer C.g_strfreev(vector)
	out := make([]string, 0, int(C.g_strv_length(vector)))
	for _, value := range unsafe.Slice(vector, int(C.g_strv_length(vector))) {
		out = append(out, C.GoString(value))
	}
	return out, nil
}

func (nativeShortcutSettings) setPaths(paths []string) error {
	settings, err := openShortcutSettings(mediaKeysSchema, "")
	if err != nil {
		return err
	}
	defer C.g_object_unref(C.gpointer(settings))
	vector := C.calloc(C.size_t(len(paths)+1), C.size_t(unsafe.Sizeof(uintptr(0))))
	if vector == nil {
		return errors.New("could not allocate desktop shortcut registration")
	}
	defer C.free(vector)
	entries := unsafe.Slice((**C.char)(vector), len(paths)+1)
	for i, value := range paths {
		if strings.ContainsRune(value, 0) {
			return errors.New("a desktop shortcut path contains a null character")
		}
		entries[i] = C.CString(value)
		defer C.free(unsafe.Pointer(entries[i]))
	}
	if C.voice_type_set_paths(settings, (**C.char)(vector)) == 0 {
		return errors.New("desktop shortcut registrations are read-only")
	}
	C.g_settings_sync()
	return nil
}

func (nativeShortcutSettings) binding(path string) (string, error) {
	settings, err := openShortcutSettings(customKeySchema, path)
	if err != nil {
		return "", err
	}
	defer C.g_object_unref(C.gpointer(settings))
	key := C.CString("binding")
	defer C.free(unsafe.Pointer(key))
	value := C.g_settings_get_string(settings, key)
	defer C.g_free(C.gpointer(value))
	return C.GoString(value), nil
}

func (nativeShortcutSettings) set(path, field, value string) error {
	if !ownedShortcutPath(path) || !slices.Contains([]string{"name", "command", "binding"}, field) || strings.ContainsRune(value, 0) {
		return errors.New("refusing to change settings outside Voice Type's shortcuts")
	}
	settings, err := openShortcutSettings(customKeySchema, path)
	if err != nil {
		return err
	}
	defer C.g_object_unref(C.gpointer(settings))
	key := C.CString(field)
	defer C.free(unsafe.Pointer(key))
	previous := C.g_settings_get_string(settings, key)
	defer C.g_free(C.gpointer(previous))
	if C.GoString(previous) == value {
		return nil
	}
	next := C.CString(value)
	defer C.free(unsafe.Pointer(next))
	if C.g_settings_set_string(settings, key, next) == 0 {
		return fmt.Errorf("Voice Type's desktop shortcut %s is read-only", field)
	}
	C.g_settings_sync()
	return nil
}

func (nativeShortcutSettings) reset(path string) error {
	if !ownedShortcutPath(path) {
		return errors.New("refusing to reset another application's shortcut")
	}
	settings, err := openShortcutSettings(customKeySchema, path)
	if err != nil {
		return err
	}
	defer C.g_object_unref(C.gpointer(settings))
	for _, field := range []string{"name", "command", "binding"} {
		key := C.CString(field)
		current := C.g_settings_get_user_value(settings, key)
		if current != nil {
			C.g_variant_unref(current)
			if C.g_settings_is_writable(settings, key) == 0 {
				C.free(unsafe.Pointer(key))
				return errors.New("Voice Type's desktop shortcut is read-only")
			}
			C.g_settings_reset(settings, key)
		}
		C.free(unsafe.Pointer(key))
	}
	C.g_settings_sync()
	return nil
}

package main

import "testing"

func TestClipboardKeep(t *testing.T) {
	cases := []struct {
		name  string
		types []string
		want  string
	}{
		// The owner picks the advertised order, and leading with an X11 alias
		// is routine -- first-wins would drop the image here.
		{"image behind text aliases", []string{"UTF8_STRING", "STRING", "text/plain", "image/png"}, "image/png"},
		{"png beats other image encodings", []string{"image/bmp", "image/tiff", "image/png"}, "image/png"},
		{"image beats text", []string{"text/uri-list", "image/tiff"}, "image/tiff"},
		{"utf-8 beats bare text/plain", []string{"text/plain", "text/plain;charset=utf-8"}, "text/plain;charset=utf-8"},
		{"aliases alone are not restorable", []string{"UTF8_STRING", "STRING", "TEXT"}, ""},
		{"unknown binary is not restorable", []string{"application/x-krita"}, ""},
		{"empty offer", nil, ""},
	}
	for _, tc := range cases {
		if got := clipboardKeep(tc.types); got != tc.want {
			t.Errorf("%s: clipboardKeep(%q) = %q, want %q", tc.name, tc.types, got, tc.want)
		}
	}
}

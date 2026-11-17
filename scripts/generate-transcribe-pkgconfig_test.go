package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePlatformLinkManifest(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			manifest := linkManifest{Version: "0.4.1", Libraries: []string{"transcribe", "ggml"}, SystemLibs: []string{"m"}}
			if platform == "darwin" {
				manifest.SystemLibs = append(manifest.SystemLibs, "c++")
				manifest.Frameworks = []string{"Accelerate", "Foundation", "Metal", "MetalKit"}
			}
			args := writeTestManifest(t, manifest)
			if err := generateForPlatform(args, platform); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(args[2], "voicetype-transcribe.pc"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"-ltranscribe -lggml -lm", "includedir=${prefix}/include"} {
				if !strings.Contains(string(data), want) {
					t.Fatalf("missing %q: %s", want, data)
				}
			}
			if platform == "darwin" && !strings.Contains(string(data), "-lc++ -framework Accelerate -framework Foundation -framework Metal -framework MetalKit") {
				t.Fatalf("Apple link dependencies lost: %s", data)
			}
		})
	}
}

func TestRejectUnsafeLinkManifests(t *testing.T) {
	for _, test := range []struct {
		name, platform string
		manifest       linkManifest
	}{
		{"linux-frameworks", "linux", linkManifest{Libraries: []string{"transcribe"}, Frameworks: []string{"Metal"}}},
		{"shared", "darwin", linkManifest{Shared: true}},
		{"empty", "darwin", linkManifest{}},
		{"framework-flags", "darwin", linkManifest{Libraries: []string{"transcribe"}, Frameworks: []string{"Metal -bad"}}},
		{"framework-newline", "darwin", linkManifest{Libraries: []string{"transcribe"}, Frameworks: []string{"Metal\nLibs: -bad"}}},
		{"library-flags", "darwin", linkManifest{Libraries: []string{"transcribe -bad"}}},
		{"system-library-flags", "darwin", linkManifest{Libraries: []string{"transcribe"}, SystemLibs: []string{"m -bad"}}},
		{"parent-path", "darwin", linkManifest{LibDir: "../lib", Libraries: []string{"transcribe"}}},
		{"absolute-path", "darwin", linkManifest{IncludeDir: "/usr/include", Libraries: []string{"transcribe"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := writeTestManifest(t, test.manifest)
			if err := generateForPlatform(args, test.platform); err == nil {
				t.Fatal("unsafe manifest accepted")
			}
			if _, err := os.Stat(args[2]); !os.IsNotExist(err) {
				t.Fatal("invalid manifest created output")
			}
		})
	}
}

func writeTestManifest(t *testing.T, manifest linkManifest) []string {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return []string{filepath.Join(dir, "install"), path, filepath.Join(dir, "pkgconfig")}
}

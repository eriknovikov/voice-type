package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type linkManifest struct {
	Version      string   `json:"version"`
	Shared       bool     `json:"shared"`
	IncludeDir   string   `json:"include_dir"`
	LibDir       string   `json:"lib_dir"`
	Libraries    []string `json:"libraries"`
	LibraryPaths []string `json:"library_paths"`
	SystemLibs   []string `json:"system_libs"`
	Frameworks   []string `json:"frameworks"`
	LinkFlags    []string `json:"link_flags"`
}

func main() {
	if err := generate(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: generate-transcribe-pkgconfig PREFIX MANIFEST OUTDIR")
	}
	prefix, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	var manifest linkManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("read transcribe.cpp link manifest: %w", err)
	}
	if manifest.Shared {
		return errors.New("Voice Type requires the static transcribe.cpp link posture")
	}
	if len(manifest.Frameworks) != 0 {
		return errors.New("unexpected platform frameworks in the Linux link manifest")
	}
	libDir, err := relativeManifestPath(manifest.LibDir, "lib")
	if err != nil {
		return err
	}
	includeDir, err := relativeManifestPath(manifest.IncludeDir, "include")
	if err != nil {
		return err
	}
	libs := make([]string, 0, len(manifest.Libraries)+len(manifest.LibraryPaths)+len(manifest.SystemLibs)+len(manifest.LinkFlags))
	for _, name := range manifest.Libraries {
		if name == "" || strings.ContainsAny(name, " \t\r\n") {
			return fmt.Errorf("invalid library name in transcribe.cpp manifest: %q", name)
		}
		libs = append(libs, "-l"+name)
	}
	for _, rawPath := range manifest.LibraryPaths {
		path := filepath.Clean(rawPath)
		name := filepath.Base(path)
		for _, suffix := range []string{".a", ".dylib", ".lib"} {
			if strings.HasSuffix(name, suffix) {
				name = strings.TrimSuffix(name, suffix)
				name = strings.TrimPrefix(name, "lib")
				libs = append(libs, "-L"+filepath.Dir(path), "-l"+name)
				break
			}
		}
	}
	for _, name := range manifest.SystemLibs {
		libs = append(libs, "-l"+name)
	}
	libs = append(libs, manifest.LinkFlags...)
	if len(libs) == 0 {
		return errors.New("transcribe.cpp install manifest has no libraries")
	}

	outputDir := args[2]
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	contents := strings.Join([]string{
		"prefix=" + prefix,
		"exec_prefix=${prefix}",
		"libdir=${prefix}/" + filepath.ToSlash(libDir),
		"includedir=${prefix}/" + filepath.ToSlash(includeDir),
		"",
		"Name: voicetype-transcribe",
		"Description: Pinned static transcribe.cpp engine for Voice Type",
		"Version: " + manifest.Version,
		"Libs: -L${libdir} " + strings.Join(libs, " "),
		"Cflags: -I${includedir}",
		"",
	}, "\n")
	return os.WriteFile(filepath.Join(outputDir, "voicetype-transcribe.pc"), []byte(contents), 0644)
}

func relativeManifestPath(value, fallback string) (string, error) {
	if value == "" {
		value = fallback
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path in transcribe.cpp manifest: %q", value)
	}
	return clean, nil
}

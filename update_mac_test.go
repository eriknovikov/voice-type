package voicetype

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func macReleaseFixture() macRelease {
	var release macRelease
	_ = json.Unmarshal([]byte(`{"tag_name":"v6.5.0","assets":[{"name":"voice-type_6.5.0_darwin_arm64.dmg","browser_download_url":"https://github.com/eriknovikov/voice-type/releases/download/v6.5.0/voice-type_6.5.0_darwin_arm64.dmg"},{"name":"voice-type_6.5.0_darwin_amd64.dmg","browser_download_url":"https://github.com/eriknovikov/voice-type/releases/download/v6.5.0/voice-type_6.5.0_darwin_amd64.dmg"}]}`), &release)
	return release
}

func TestMacUpdateSelectsMatchingDMGAndIgnoresLinuxApplyEnvironment(t *testing.T) {
	t.Setenv(updateApplyEnv, "v6.99.0")
	for _, arch := range []string{"arm64", "amd64"} {
		var out, errOut bytes.Buffer
		opened := []string{}
		code := runMacUpdate("6.4.0", arch, &out, &errOut, func() (macRelease, error) { return macReleaseFixture(), nil }, func(url string) error { opened = append(opened, url); return nil })
		if code != 0 || len(opened) != 1 || !strings.HasSuffix(opened[0], "_darwin_"+arch+".dmg") || !strings.Contains(out.String(), "quit Voice Type") || !strings.Contains(out.String(), "Your data is kept") || errOut.Len() != 0 {
			t.Fatal(code, opened, out.String(), errOut.String())
		}
	}
}

func TestMacUpdateFailureAndNoOpPathsNeverOpenDownloads(t *testing.T) {
	for _, scenario := range []string{"current", "older", "dev", "version-dev", "network", "missing", "wrong-url", "duplicate", "draft", "prerelease", "tag", "arch"} {
		t.Run(scenario, func(t *testing.T) {
			release, version, arch := macReleaseFixture(), "6.4.0", "arm64"
			want, lookups := 1, 0
			var lookupErr error
			switch scenario {
			case "current":
				version, want = "6.5.0", 0
			case "older":
				version, want = "6.6.0", 0
			case "dev":
				version = "dev"
			case "version-dev":
				version = "6.4.0-dev"
			case "network":
				lookupErr = errors.New("network unavailable")
			case "missing":
				release.Assets = nil
			case "wrong-url":
				release.Assets[0].URL = "https://example.test/untrusted.dmg"
			case "duplicate":
				release.Assets = append(release.Assets, release.Assets[0])
			case "draft":
				release.Draft = true
			case "prerelease":
				release.Prerelease = true
			case "tag":
				release.Tag = "v6.5.0/../../bad"
			case "arch":
				arch = "386"
			}
			var out, errOut bytes.Buffer
			code := runMacUpdate(version, arch, &out, &errOut, func() (macRelease, error) { lookups++; return release, lookupErr }, func(string) error { t.Fatal("unsafe/no-op update opened a download"); return nil })
			if code != want {
				t.Fatal(code, errOut.String())
			}
			if (scenario == "dev" || scenario == "version-dev") && lookups != 0 {
				t.Fatal("development update performed a network lookup")
			}
		})
	}
}

func TestMacUpdateReportsOpenFailureWithManualDownload(t *testing.T) {
	var out, errOut bytes.Buffer
	code := runMacUpdate("6.4.0", "arm64", &out, &errOut, func() (macRelease, error) { return macReleaseFixture(), nil }, func(string) error { return errors.New("browser unavailable") })
	if code != 1 || !strings.Contains(errOut.String(), "Download it manually: https://github.com/") || out.Len() != 0 {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestMacReleaseLookupHandlesHTTPMetadataAndCancellation(t *testing.T) {
	for _, scenario := range []string{"ok", "rate-limit", "invalid", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") != "voice-type" {
					t.Error("missing API headers")
				}
				switch scenario {
				case "rate-limit":
					w.WriteHeader(http.StatusForbidden)
				case "invalid":
					_, _ = w.Write([]byte("private-error-marker{"))
				default:
					_ = json.NewEncoder(w).Encode(macReleaseFixture())
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			release, err := latestMacRelease(ctx, server.Client(), server.URL)
			if scenario == "ok" {
				if err != nil || release.Tag != "v6.5.0" {
					t.Fatal(release, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "private-error-marker") {
				t.Fatal("failed or sensitive response accepted", err)
			}
		})
	}
}

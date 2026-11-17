package voicetype

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const macLatestReleaseURL = "https://api.github.com/repos/eriknovikov/voice-type/releases/latest"

type macRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func latestMacRelease(ctx context.Context, client *http.Client, endpoint string) (macRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return macRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "voice-type")
	response, err := client.Do(request)
	if err != nil {
		return macRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return macRelease{}, fmt.Errorf("release lookup returned %s", response.Status)
	}
	var release macRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return macRelease{}, errors.New("release lookup returned invalid metadata")
	}
	return release, nil
}

func macReleaseDownload(release macRelease, arch string) (string, error) {
	if arch != "arm64" && arch != "amd64" {
		return "", errors.New("Mac downloads support arm64 and amd64 only")
	}
	if !releaseTagPattern.MatchString(release.Tag) || release.Draft || release.Prerelease {
		return "", errors.New("release metadata is not a published stable application release")
	}
	name := "voice-type_" + strings.TrimPrefix(release.Tag, "v") + "_darwin_" + arch + ".dmg"
	expected := "https://github.com/eriknovikov/voice-type/releases/download/" + release.Tag + "/" + name
	found := false
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if found || asset.URL != expected {
			return "", errors.New("release download metadata is ambiguous or points outside the expected release")
		}
		found = true
	}
	if !found {
		return "", fmt.Errorf("%s has no published %s Mac DMG; no update was applied", release.Tag, arch)
	}
	return expected, nil
}

func runMacUpdate(version, arch string, out, errOut io.Writer, lookup func() (macRelease, error), open func(string) error) int {
	current := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if developmentMode() || !releaseTagPattern.MatchString("v"+current) {
		fmt.Fprintln(errOut, "Development builds do not self-update; install a release first.")
		return 1
	}
	release, err := lookup()
	if err == nil && (!releaseTagPattern.MatchString(release.Tag) || release.Draft || release.Prerelease) {
		err = errors.New("latest release is not a published stable application release")
	}
	if err != nil {
		fmt.Fprintf(errOut, "Could not check for updates: %v\n", err)
		return 1
	}
	if !newerVersion(release.Tag, current) {
		fmt.Fprintf(out, "Voice Type is up to date (v%s).\n", current)
		return 0
	}
	url, err := macReleaseDownload(release, arch)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if err := open(url); err != nil {
		fmt.Fprintf(errOut, "Could not open the Mac download: %v\nDownload it manually: %s\n", err, url)
		return 1
	}
	fmt.Fprintf(out, "Opened %s\nAfter downloading, quit Voice Type and replace Voice Type.app in Applications. Your data is kept.\n", url)
	return 0
}

func lookupMacUpdate() (macRelease, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return latestMacRelease(ctx, &http.Client{Timeout: 30 * time.Second}, macLatestReleaseURL)
}

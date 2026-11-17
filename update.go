package voicetype

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://github.com/eriknovikov/voice-type/releases/latest"
	updateApplyEnv   = "VOICE_TYPE_UPDATE_APPLY"
)

var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// RunUpdate uses the native update path. Only Linux embeds or executes install.sh.
func RunUpdate(version string, out, errOut io.Writer) int {
	return platformRunUpdate(version, out, errOut)
}

func latestReleaseTag() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release page returned %s", response.Status)
	}
	tag := path.Base(response.Request.URL.Path)
	if !releaseTagPattern.MatchString(tag) {
		return "", fmt.Errorf("latest release tag %q is not an application release", tag)
	}
	return tag, nil
}

func newerVersion(latest, current string) bool {
	next, ok := numericVersion(latest)
	if !ok {
		return false
	}
	installed, ok := numericVersion(current)
	if !ok {
		return false
	}
	for i := range next {
		if next[i] != installed[i] {
			return next[i] > installed[i]
		}
	}
	return false
}

func numericVersion(version string) ([3]int, bool) {
	var parsed [3]int
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if index := strings.IndexAny(version, "-+"); index >= 0 {
		version = version[:index]
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return parsed, false
	}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return parsed, false
		}
		parsed[i] = value
	}
	return parsed, true
}

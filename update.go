package voicetype

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed install.sh
var installerScript []byte

const (
	latestReleaseURL = "https://github.com/eriknovikov/voice-type/releases/latest"
	updateApplyEnv   = "VOICE_TYPE_UPDATE_APPLY"
)

var releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// RunUpdate checks the latest release and, when newer, re-executes a private
// copy of this binary so the installed executable can be replaced safely.
func RunUpdate(version string, out, errOut io.Writer) int {
	if tag := os.Getenv(updateApplyEnv); tag != "" {
		return runUpdateApply(tag, out, errOut)
	}
	current := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if current == "" || current == "dev" {
		fmt.Fprintln(errOut, "Development builds do not self-update; install a release first.")
		return 1
	}
	tag, err := latestReleaseTag()
	if err != nil {
		fmt.Fprintf(errOut, "Could not check for updates: %v\n", err)
		return 1
	}
	if !newerVersion(tag, current) {
		fmt.Fprintf(out, "Voice Type is up to date (v%s).\n", current)
		return 0
	}
	fmt.Fprintf(out, "Updating Voice Type v%s to %s...\n", current, tag)
	return reexecUpdate(tag, errOut)
}

// reexecUpdate runs a temporary copy so the installed binary is not executing
// while the installer replaces it.
func reexecUpdate(tag string, errOut io.Writer) int {
	source, err := os.Executable()
	if err != nil {
		fmt.Fprintf(errOut, "Could not locate the running binary: %v\n", err)
		return 1
	}
	temporary, err := os.CreateTemp("", "voice-type-update-*")
	if err != nil {
		fmt.Fprintf(errOut, "Could not prepare the update: %v\n", err)
		return 1
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	input, err := os.Open(source)
	if err == nil {
		_, err = io.Copy(temporary, input)
		input.Close()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temporaryPath, 0o700)
	}
	if err != nil {
		fmt.Fprintf(errOut, "Could not prepare the update: %v\n", err)
		return 1
	}
	command := exec.Command(temporaryPath, "update")
	command.Env = append(os.Environ(), updateApplyEnv+"="+tag)
	if err := syscall.Exec(temporaryPath, []string{temporaryPath, "update"}, command.Env); err != nil {
		os.Remove(temporaryPath)
		fmt.Fprintf(errOut, "Update failed: %v\n", err)
		return 1
	}
	return 0
}

// removeUpdateCopy unlinks the private copy now that it is the running image.
// Linux permits unlinking a running executable, and the installer no longer
// sees the installed path in /proc while it replaces it.
func removeUpdateCopy() {
	executable, err := os.Executable()
	if err != nil || !strings.HasPrefix(filepath.Base(executable), "voice-type-update-") {
		return
	}
	_ = os.Remove(executable)
}

func runUpdateApply(tag string, out, errOut io.Writer) int {
	removeUpdateCopy()
	if !releaseTagPattern.MatchString(tag) {
		fmt.Fprintln(errOut, "Refusing an invalid release tag.")
		return 1
	}
	quitRunningVoiceType()
	script, err := writeInstaller()
	if err != nil {
		fmt.Fprintf(errOut, "Could not prepare the installer: %v\n", err)
		return 1
	}
	defer os.Remove(script)
	command := exec.Command("sh", script, "--version", tag)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(errOut, "Update failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "Voice Type %s is installed.\n", tag)
	return 0
}

func writeInstaller() (string, error) {
	file, err := os.CreateTemp("", "voice-type-install-*.sh")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(installerScript); err == nil {
		err = file.Chmod(0o700)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
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

// quitRunningVoiceType asks an installed instance to shut down before the
// package is replaced. A missing instance is not an error.
func quitRunningVoiceType() {
	candidates := []string{"/usr/bin/voice-type"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "voice-type"))
	}
	if found, err := exec.LookPath("voice-type"); err == nil {
		candidates = append([]string{found}, candidates...)
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		command := exec.CommandContext(ctx, candidate, "--quit")
		_ = command.Run()
		cancel()
		return
	}
}

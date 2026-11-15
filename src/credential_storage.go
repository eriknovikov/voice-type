package voicetype

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	storageKeyring = "keyring"
	storageFile    = "file"
)

func credentialStoragePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "voice-type", "credentials-storage"), nil
}

// currentCredentialStorage defaults to the system keyring. A missing or
// unreadable preference file is not an error.
func currentCredentialStorage() string {
	path, err := credentialStoragePath()
	if err != nil {
		return storageKeyring
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storageKeyring
	}
	if strings.TrimSpace(string(data)) == storageFile {
		return storageFile
	}
	return storageKeyring
}

func setCredentialStorage(mode string) error {
	if mode != storageKeyring && mode != storageFile {
		return errors.New("credential storage must be keyring or file")
	}
	path, err := credentialStoragePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(mode+"\n"), 0600); err != nil {
		return err
	}
	copyCredentialStorage(mode)
	return nil
}

// copyCredentialStorage copies existing keys to the newly selected store.
// It is best-effort: an unavailable or locked keyring must not block the
// switch, so the copy is abandoned after a short timeout.
func copyCredentialStorage(to string) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, provider := range cloudProviders {
			if to == storageFile {
				if key, err := keyring.Get(credentialService, provider); err == nil {
					_ = (fileCredentialStore{}).Set(provider, key)
				}
				continue
			}
			if key, err := (fileCredentialStore{}).Get(provider); err == nil {
				_ = keyring.Set(credentialService, provider, key)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

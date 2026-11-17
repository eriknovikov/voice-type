package voicetype

import (
	"errors"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	storageKeyring = "keyring"
	storageFile    = "file"
)

// currentCredentialStorage defaults to the system keyring. A missing or
// unreadable preference file is not an error.
func currentCredentialStorage() string {
	path, err := credentialStoragePath()
	if err != nil {
		return storageKeyring
	}
	data, err := readPrivateFile(path)
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
	if err := atomicPrivateWrite(path, []byte(mode+"\n")); err != nil {
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
				if key, err := keyring.Get(credentialServiceName(), provider); err == nil {
					_ = (fileCredentialStore{}).Set(provider, key)
				}
				continue
			}
			if key, err := (fileCredentialStore{}).Get(provider); err == nil {
				_ = keyring.Set(credentialServiceName(), provider, key)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

package voicetype

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// fileCredentialStore is the plaintext fallback. Values are base64-encoded,
// which is encoding, not encryption: the file is readable by the user account.
type fileCredentialStore struct{}

func credentialPath(provider string) (string, error) {
	dir, err := credentialDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, provider+".key"), nil
}

func (fileCredentialStore) Set(provider, key string) error {
	path, err := credentialPath(provider)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(key)) + "\n"
	return atomicPrivateWrite(path, []byte(encoded))
}

func (fileCredentialStore) Get(provider string) (string, error) {
	path, err := credentialPath(provider)
	if err != nil {
		return "", err
	}
	data, err := readPrivateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", keyring.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return "", keyring.ErrNotFound
	}
	return string(decoded), nil
}

func (fileCredentialStore) Delete(provider string) error {
	path, err := credentialPath(provider)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return keyring.ErrNotFound
	}
	return err
}

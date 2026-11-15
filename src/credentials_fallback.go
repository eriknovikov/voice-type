package voicetype

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// preferredCredentialStore writes to the system keyring by default. When the
// user selects the file store, or the keyring rejects the write, the private
// base64 file is used instead.
type preferredCredentialStore struct{}

func (preferredCredentialStore) Set(provider, key string) error {
	if currentCredentialStorage() == storageFile {
		return fileCredentialStore{}.Set(provider, key)
	}
	if err := keyring.Set(credentialService, provider, key); err == nil {
		return nil
	}
	return fileCredentialStore{}.Set(provider, key)
}

func (preferredCredentialStore) Get(provider string) (string, error) {
	if currentCredentialStorage() == storageFile {
		if key, err := (fileCredentialStore{}).Get(provider); err == nil {
			return key, nil
		}
		if key, err := keyring.Get(credentialService, provider); err == nil {
			return key, nil
		}
		return "", keyring.ErrNotFound
	}
	if key, err := keyring.Get(credentialService, provider); err == nil {
		return key, nil
	}
	return fileCredentialStore{}.Get(provider)
}

func (preferredCredentialStore) Delete(provider string) error {
	keyringErr := keyring.Delete(credentialService, provider)
	fileErr := fileCredentialStore{}.Delete(provider)
	if keyringErr == nil || fileErr == nil {
		return nil
	}
	if errors.Is(fileErr, keyring.ErrNotFound) {
		return keyringErr
	}
	return fileErr
}

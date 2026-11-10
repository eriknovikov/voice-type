package voicetype

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

var credentialService = "com.eriknovikov.voice-type"

type credentialStore interface {
	Set(provider, key string) error
	Get(provider string) (string, error)
	Delete(provider string) error
}

type systemCredentialStore struct{}

func (systemCredentialStore) Set(provider, key string) error {
	return keyring.Set(credentialService, provider, key)
}

func (systemCredentialStore) Get(provider string) (string, error) {
	return keyring.Get(credentialService, provider)
}

func (systemCredentialStore) Delete(provider string) error {
	return keyring.Delete(credentialService, provider)
}

var (
	credentialsMu  sync.Mutex
	providerKeys   credentialStore = systemCredentialStore{}
	cloudProviders                 = []string{providerOpenAI, providerOpenRouter}
)

// APIKeySummary deliberately contains only presentation metadata. The secret
// remains in the operating system's credential store.
type APIKeySummary struct {
	MaskedKey string `json:"masked_key,omitempty"`
	Saved     bool   `json:"saved"`
	Error     string `json:"error,omitempty"`
}

func validateCloudProvider(provider string) error {
	if provider != providerOpenAI && provider != providerOpenRouter {
		return errors.New("choose OpenAI or OpenRouter")
	}
	return nil
}

func saveProviderAPIKey(provider, key string) error {
	if err := validateCloudProvider(provider); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("enter an API key")
	}
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	return setProviderAPIKeyLocked(provider, key)
}

func setProviderAPIKeyLocked(provider, key string) error {
	if err := providerKeys.Set(provider, key); err != nil {
		return fmt.Errorf("save %s API key in the system keyring: %s", providerLabel(provider), strings.ReplaceAll(err.Error(), key, "[redacted]"))
	}
	// Secret Service can report success when a user dismisses a prompt. Never
	// remove an inline key during migration unless the stored value matches.
	stored, saved, err := getProviderAPIKeyLocked(provider)
	if err != nil {
		return fmt.Errorf("could not verify the saved %s API key: %s", providerLabel(provider), strings.ReplaceAll(err.Error(), key, "[redacted]"))
	}
	if !saved || stored != key {
		return fmt.Errorf("could not verify the saved %s API key in the system keyring", providerLabel(provider))
	}
	return nil
}

func providerAPIKey(provider string) (string, bool, error) {
	if err := validateCloudProvider(provider); err != nil {
		return "", false, err
	}
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	return getProviderAPIKeyLocked(provider)
}

func getProviderAPIKeyLocked(provider string) (string, bool, error) {
	key, err := providerKeys.Get(provider)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s API key from the system keyring: %w", providerLabel(provider), err)
	}
	key = strings.TrimSpace(key)
	return key, key != "", nil
}

func deleteProviderAPIKey(provider string) error {
	if err := validateCloudProvider(provider); err != nil {
		return err
	}
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	return deleteProviderAPIKeyLocked(provider)
}

func deleteProviderAPIKeyLocked(provider string) error {
	if err := providerKeys.Delete(provider); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("delete %s API key from the system keyring: %w", providerLabel(provider), err)
	}
	if _, err := providerKeys.Get(provider); !errors.Is(err, keyring.ErrNotFound) {
		if err != nil {
			return fmt.Errorf("could not verify deletion of the %s API key: %w", providerLabel(provider), err)
		}
		return fmt.Errorf("the %s API key is still in the system keyring; retry deletion", providerLabel(provider))
	}
	return nil
}

func providerKeySummary(provider string) APIKeySummary {
	summary := APIKeySummary{}
	key, saved, err := providerAPIKey(provider)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	summary.Saved = saved
	if saved {
		summary.MaskedKey = maskedKey(key)
	}
	return summary
}

func providerKeySummaries() map[string]APIKeySummary {
	out := make(map[string]APIKeySummary, len(cloudProviders))
	for _, provider := range cloudProviders {
		out[provider] = providerKeySummary(provider)
	}
	return out
}

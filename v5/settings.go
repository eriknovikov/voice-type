package voicetype

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Settings is the desktop-facing view. Secrets are write-only and never returned.
type Settings struct {
	Revision    string                      `json:"revision"`
	Provider    string                      `json:"provider"`
	Port        int                         `json:"port"`
	Microphone  string                      `json:"microphone"`
	Vocabulary  []VocabularyList            `json:"vocabulary"`
	Workspace   string                      `json:"workspace"`
	Credentials map[string]CredentialStatus `json:"credentials"`
}
type VocabularyList struct {
	Name  string   `json:"name"`
	Terms []string `json:"terms"`
}
type CredentialStatus struct {
	Saved       bool `json:"saved"`
	Environment bool `json:"environment"`
}
type SettingsUpdate struct {
	Revision   string           `json:"revision"`
	Provider   string           `json:"provider"`
	Port       int              `json:"port"`
	Microphone string           `json:"microphone"`
	Vocabulary []VocabularyList `json:"vocabulary"`
	// A missing entry preserves the existing key; an empty string deletes it.
	APIKeys map[string]string `json:"api_keys,omitempty"`
}

var settingsMu sync.Mutex

func configRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func readConfigDocument() ([]byte, map[string]json.RawMessage, error) {
	data, err := os.ReadFile(configFilePath())
	if os.IsNotExist(err) {
		return nil, map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var raw map[string]json.RawMessage
	err = json.Unmarshal([]byte(stripTrailingCommas(stripJsoncComments(string(data)))), &raw)
	if err != nil || raw == nil {
		return nil, nil, errors.New("configuration is invalid; repair the existing file before saving")
	}
	return data, raw, nil
}
func ReadSettings() (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return readSettings()
}
func readSettings() (Settings, error) {
	data, raw, err := readConfigDocument()
	if err != nil {
		return Settings{}, err
	}
	cfg, err := configValidate(raw)
	if err != nil {
		return Settings{}, err
	}
	out := Settings{Revision: configRevision(data), Provider: cfg.Provider, Port: cfg.Port, Microphone: cfg.Microphone, Workspace: workspaceLoad(), Vocabulary: []VocabularyList{}, Credentials: map[string]CredentialStatus{}}
	for _, list := range cfg.Vocabulary {
		out.Vocabulary = append(out.Vocabulary, VocabularyList{list.Name, cleanTerms(list.Terms)})
	}
	for _, provider := range []string{providerOpenAI, providerOpenRouter} {
		key, exists := cfg.APIKeys[provider]
		if !exists && provider == cfg.Provider {
			key = cfg.APIKey
		}
		out.Credentials[provider] = CredentialStatus{Saved: strings.TrimSpace(key) != "", Environment: strings.TrimSpace(os.Getenv((Config{Provider: provider}).keyEnv())) != ""}
	}
	return out, nil
}

// SaveSettings preserves unknown JSON fields. The first UI save keeps an exact
// private backup of the original JSONC, including comments.
func SaveSettings(update SettingsUpdate) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	data, raw, err := readConfigDocument()
	if err != nil {
		return Settings{}, err
	}
	if update.Revision != configRevision(data) {
		return Settings{}, errors.New("settings changed elsewhere; reload before saving")
	}
	old, err := configValidate(raw)
	if err != nil {
		return Settings{}, err
	}
	if update.Provider != providerOpenAI && update.Provider != providerOpenRouter {
		return Settings{}, errors.New("provider must be openai or openrouter")
	}
	if update.Port < 1024 || update.Port > 65535 {
		return Settings{}, errors.New("port must be between 1024 and 65535")
	}
	vocab := vocabLists{}
	seen := map[string]bool{}
	for _, list := range update.Vocabulary {
		name := strings.TrimSpace(list.Name)
		if name == "" || strings.ContainsAny(name, "\r\n") || seen[name] {
			return Settings{}, errors.New("vocabulary names must be unique and nonempty")
		}
		seen[name] = true
		// Validate all workspaces so a later switch cannot introduce invalid terms.
		for _, term := range list.Terms {
			if strings.ContainsAny(term, "<>\r\n") {
				return Settings{}, fmt.Errorf("%s: terms cannot contain angle brackets or line breaks", name)
			}
		}
		vocab = append(vocab, vocabList{name, cleanTerms(list.Terms)})
	}
	keys := old.APIKeys
	if keys == nil {
		keys = map[string]string{}
	}
	if _, exists := keys[old.Provider]; !exists && old.APIKey != "" {
		keys[old.Provider] = old.APIKey
	}
	for provider, key := range update.APIKeys {
		if provider != providerOpenAI && provider != providerOpenRouter {
			return Settings{}, errors.New("unknown credential provider")
		}
		keys[provider] = strings.TrimSpace(key)
	}
	put := func(name string, value any) { raw[name], _ = json.Marshal(value) }
	put("provider", update.Provider)
	put("port", update.Port)
	put("microphone", update.Microphone)
	put("api_keys", keys)
	delete(raw, "api_key")
	// Preserve user order inside the vocabulary object.
	var b strings.Builder
	b.WriteByte('{')
	for i, list := range vocab {
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(list.Name)
		terms, _ := json.Marshal(list.Terms)
		b.Write(name)
		b.WriteByte(':')
		b.Write(terms)
	}
	b.WriteByte('}')
	raw["vocabulary"] = json.RawMessage(b.String())
	encoded, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return Settings{}, err
	}
	path := configFilePath()
	if len(data) > 0 {
		f, e := os.OpenFile(path+".before-desktop", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e == nil {
			_, e = f.Write(data)
			closeErr := f.Close()
			if e == nil {
				e = closeErr
			}
		}
		if e != nil && !os.IsExist(e) {
			return Settings{}, e
		}
	}
	// Catch hand edits made while validating the form.
	latest, _, err := readConfigDocument()
	if err != nil {
		return Settings{}, err
	}
	if configRevision(latest) != update.Revision {
		return Settings{}, errors.New("settings changed elsewhere; reload before saving")
	}
	if err := atomicPrivateWrite(path, append(encoded, '\n')); err != nil {
		return Settings{}, err
	}
	return readSettings()
}
func atomicPrivateWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".voice-type-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func SelectWorkspace(name string) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	cfg, err := configReadStrict()
	if err != nil {
		return err
	}
	if name != "" && (name == generalWorkspace || !cfg.hasWorkspace(name)) {
		return errors.New("choose an existing workspace or general only")
	}
	return workspaceSave(name)
}

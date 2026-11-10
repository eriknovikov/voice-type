package voicetype

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// Settings contains masked profile and provider-key metadata. Full credentials
// require an explicit copy action and never travel with routine settings reads.
type Settings struct {
	Revision      string                   `json:"revision"`
	Microphone    string                   `json:"microphone"`
	Vocabulary    []VocabularyList         `json:"vocabulary"`
	Workspace     string                   `json:"workspace"`
	Profiles      []ProfileSummary         `json:"profiles"`
	ActiveProfile string                   `json:"active_profile"`
	APIKeys       map[string]APIKeySummary `json:"api_keys"`
	Notice        string                   `json:"notice"`
}
type ProfileSummary struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Provider      string        `json:"provider"`
	ModelID       string        `json:"model_id,omitempty"`
	StreamingMode streamingMode `json:"streaming_mode,omitempty"`
	Languages     []string      `json:"languages,omitempty"`
}
type VocabularyList struct {
	OriginalName string   `json:"original_name,omitempty"`
	Name         string   `json:"name"`
	Terms        []string `json:"terms"`
}

// Absent fields are preserved, making independent autosaves safe to serialize.
type SettingsUpdate struct {
	Revision   string            `json:"revision"`
	Microphone *string           `json:"microphone,omitempty"`
	Vocabulary *[]VocabularyList `json:"vocabulary,omitempty"`
}
type ProfileUpdate struct {
	Revision      string    `json:"revision"`
	ID            string    `json:"id,omitempty"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	ModelID       string    `json:"model_id,omitempty"`
	StreamingMode string    `json:"streaming_mode,omitempty"`
	Languages     *[]string `json:"languages,omitempty"`
}

var settingsMu sync.Mutex
var configNotices sync.Map
var configNoticeSequence atomic.Uint64

type configNotice struct {
	message string
	version uint64
}

func ConfigNotice() string {
	message, _ := configRecoveryNotice()
	return message
}
func configRecoveryNotice() (string, uint64) {
	value, _ := configNotices.Load(configFilePath())
	notice, _ := value.(configNotice)
	return notice.message, notice.version
}
func configRevision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func decodeConfigDocument(data []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripTrailingCommas(stripJsoncComments(string(data)))), &raw); err != nil || raw == nil {
		return nil, errors.New("configuration is not a JSON object")
	}
	return raw, nil
}
func putConfig(raw map[string]json.RawMessage, name string, value any) {
	raw[name], _ = json.Marshal(value)
}
func emptyConfigDocument() []byte {
	return []byte("{\"profiles\":[],\"active_profile\":\"\",\"vocabulary\":{\"general\":[]}}\n")
}

// Called with settingsMu held for validation, migration and recovery.
func readConfigDocument() ([]byte, map[string]json.RawMessage, error) {
	if configFilePath() == "" {
		return nil, nil, errors.New("cannot locate your home directory; restore HOME and reopen Voice Type")
	}
	data, notice, err := ReadPrivateDocument(configFilePath(), "config.jsonc", func(data []byte) error { _, err := configParse(data); return err })
	if err != nil {
		return nil, nil, err
	}
	if notice != "" {
		configNotices.Store(configFilePath(), configNotice{notice, configNoticeSequence.Add(1)})
	}
	if data == nil {
		data = emptyConfigDocument()
		if err := WritePrivateDocument(configFilePath(), "config.jsonc", data); err != nil {
			return nil, nil, err
		}
	}
	raw, err := decodeConfigDocument(data)
	if err != nil {
		return nil, nil, err
	}
	if _, modern := raw["profiles"]; !modern {
		expectedMigrationRevision := configRevision(data)
		cfg, err := configValidate(raw)
		if err != nil {
			return nil, nil, err
		}
		// Preserve the pre-profile shape without storing its old secrets.
		legacyBackup := make(map[string]json.RawMessage, len(raw))
		for name, value := range raw {
			if name != "api_key" && name != "api_keys" {
				legacyBackup[name] = value
			}
		}
		putConfig(raw, "profiles", cfg.Profiles)
		putConfig(raw, "active_profile", cfg.ActiveProfile)
		delete(raw, "provider")
		delete(raw, "api_key")
		delete(raw, "api_keys")
		// Move credentials before rewriting legacy settings. If the keyring is
		// unavailable or conflicts, leave the original document untouched.
		if _, err := migrateInlineProfileKeys(raw); err != nil {
			return nil, nil, err
		}
		data, err = encodeConfigDocument(raw)
		if err != nil {
			return nil, nil, err
		}
		latest, err := os.ReadFile(configFilePath())
		if err != nil {
			return nil, nil, err
		}
		if configRevision(latest) != expectedMigrationRevision {
			return nil, nil, errors.New("settings changed elsewhere during migration; reload before saving")
		}
		backup, err := backupPath("config-before-profiles.jsonc")
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			legacyData, err := encodeConfigDocument(legacyBackup)
			if err != nil {
				return nil, nil, err
			}
			if err := atomicPrivateWrite(backup, legacyData); err != nil {
				return nil, nil, err
			}
		} else if err != nil {
			return nil, nil, err
		}
		if err := WritePrivateDocument(configFilePath(), "config.jsonc", data); err != nil {
			return nil, nil, err
		}
	}
	originalData := data
	changed, err := migrateInlineProfileKeys(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := sanitizeLegacyCredentialBackup("config-before-profiles.jsonc"); err != nil {
		return nil, nil, err
	}
	if changed {
		data, err = encodeConfigDocument(raw)
		if err != nil {
			return nil, nil, err
		}
		latest, err := os.ReadFile(configFilePath())
		if err != nil {
			return nil, nil, err
		}
		// The keyring may have prompted while another editor changed the file.
		// Never replace newer edits with the migrated snapshot.
		if configRevision(latest) != configRevision(originalData) {
			return nil, nil, errors.New("settings changed elsewhere during migration; reload before saving")
		}
		if err := WritePrivateDocument(configFilePath(), "config.jsonc", data); err != nil {
			return nil, nil, err
		}
	}
	return data, raw, nil
}

// Existing releases stored credentials inside profiles. Move one key per
// provider into the keyring before removing it from the primary and backup.
func migrateInlineProfileKeys(raw map[string]json.RawMessage) (bool, error) {
	var profiles []Profile
	if v, ok := raw["profiles"]; ok {
		if err := json.Unmarshal(v, &profiles); err != nil {
			return false, errors.New("profiles must be a list")
		}
	}
	legacy := map[string]string{}
	changed := false
	if _, exists := raw["profiles"]; !exists {
		return false, nil
	}
	addKey := func(provider, key string) error {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil
		}
		if err := validateCloudProvider(provider); err != nil {
			return fmt.Errorf("configuration contains an API key for unsupported provider %q", provider)
		}
		if previous, exists := legacy[provider]; exists && previous != key {
			return fmt.Errorf("profiles for %s contain different API keys; keep one %s API key before upgrading", providerLabel(provider), providerLabel(provider))
		}
		legacy[provider] = key
		return nil
	}
	for i := range profiles {
		key := profiles[i].APIKey()
		if key == "" {
			if _, exists := profiles[i].Config["api_key"]; exists {
				delete(profiles[i].Config, "api_key")
				changed = true
			}
			continue
		}
		provider := profiles[i].Provider
		if err := addKey(provider, key); err != nil {
			return false, err
		}
		delete(profiles[i].Config, "api_key")
		changed = true
	}
	if v, ok := raw["api_keys"]; ok {
		var keys map[string]string
		if err := json.Unmarshal(v, &keys); err != nil {
			return false, errors.New("api_keys must map provider names to keys")
		}
		for provider, key := range keys {
			if err := addKey(provider, key); err != nil {
				return false, err
			}
		}
		delete(raw, "api_keys")
		changed = true
	}
	if v, ok := raw["api_key"]; ok {
		var key string
		if err := json.Unmarshal(v, &key); err != nil {
			return false, errors.New("api_key must be a string")
		}
		if strings.TrimSpace(key) != "" {
			var provider string
			if err := json.Unmarshal(raw["provider"], &provider); err != nil || provider == "" {
				return false, errors.New("legacy API key has no provider; choose its provider before upgrading")
			}
			if err := addKey(provider, key); err != nil {
				return false, err
			}
		}
		delete(raw, "api_key")
		changed = true
	}
	if !changed {
		return false, nil
	}
	credentialsMu.Lock()
	defer credentialsMu.Unlock()
	missing := make(map[string]string)
	for _, provider := range cloudProviders {
		key, ok := legacy[provider]
		if !ok {
			continue
		}
		saved, exists, err := getProviderAPIKeyLocked(provider)
		if err != nil {
			return false, err
		}
		if exists && saved != key {
			return false, fmt.Errorf("the system keyring already has a different %s API key; use your system password manager to remove the Voice Type %s entry before retrying", providerLabel(provider), providerLabel(provider))
		}
		if !exists {
			missing[provider] = key
		}
	}
	for _, provider := range cloudProviders {
		key, ok := missing[provider]
		if !ok {
			continue
		}
		if err := setProviderAPIKeyLocked(provider, key); err != nil {
			return false, err
		}
	}
	putConfig(raw, "profiles", profiles)
	return true, nil
}

func sanitizeLegacyCredentialBackup(name string) error {
	path, err := backupPath(name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	raw, err := decodeConfigDocument(data)
	if err != nil {
		return nil // Recovery already preserves damaged originals separately.
	}
	changed := false
	for _, field := range []string{"api_key", "api_keys"} {
		if _, exists := raw[field]; exists {
			delete(raw, field)
			changed = true
		}
	}
	var profiles []Profile
	if v, ok := raw["profiles"]; ok && json.Unmarshal(v, &profiles) == nil {
		for i := range profiles {
			if _, exists := profiles[i].Config["api_key"]; exists {
				delete(profiles[i].Config, "api_key")
				changed = true
			}
		}
		if changed {
			putConfig(raw, "profiles", profiles)
		}
	}
	if !changed {
		return nil
	}
	encoded, err := encodeConfigDocument(raw)
	if err != nil {
		return err
	}
	return atomicPrivateWrite(path, encoded)
}
func encodeConfigDocument(raw map[string]json.RawMessage) ([]byte, error) {
	encoded, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, errors.New("could not encode settings")
	}
	return append(encoded, '\n'), nil
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
	out := Settings{Revision: configRevision(data), Microphone: cfg.Microphone, Workspace: workspaceLoad(), Vocabulary: []VocabularyList{}, Profiles: []ProfileSummary{}, ActiveProfile: cfg.ActiveProfile, APIKeys: providerKeySummaries(), Notice: ConfigNotice()}
	for _, list := range cfg.Vocabulary {
		out.Vocabulary = append(out.Vocabulary, VocabularyList{Name: list.Name, Terms: cleanTerms(list.Terms)})
	}
	for _, p := range cfg.Profiles {
		summary := ProfileSummary{ID: p.ID, Name: p.Name, Provider: p.Provider}
		if p.Provider == providerLocal {
			summary.ModelID = p.ModelID()
			summary.StreamingMode = p.StreamingMode()
		} else {
			if p.Provider == providerOpenAI {
				summary.ModelID = defaultModelID
				summary.StreamingMode = p.StreamingMode()
				summary.Languages = p.Languages()
			}
		}
		out.Profiles = append(out.Profiles, summary)
	}
	return out, nil
}

func settingsForRevision(revision string) ([]byte, map[string]json.RawMessage, error) {
	data, raw, err := readConfigDocument()
	if err != nil {
		return nil, nil, err
	}
	if revision != configRevision(data) {
		return nil, nil, errors.New("settings changed elsewhere; reload before saving")
	}
	return data, raw, nil
}
func commitConfig(raw map[string]json.RawMessage, expected string) error {
	if _, err := configValidate(raw); err != nil {
		return err
	}
	encoded, err := encodeConfigDocument(raw)
	if err != nil {
		return err
	}
	latest, err := os.ReadFile(configFilePath())
	if err != nil {
		return fmt.Errorf("read settings before saving: %w", err)
	}
	if configRevision(latest) != expected {
		return errors.New("settings changed elsewhere; reload before saving")
	}
	return WritePrivateDocument(configFilePath(), "config.jsonc", encoded)
}

func SaveSettings(update SettingsUpdate) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	_, raw, err := settingsForRevision(update.Revision)
	if err != nil {
		return Settings{}, err
	}
	active := workspaceLoad()
	nextActive := active
	if update.Microphone != nil {
		putConfig(raw, "microphone", *update.Microphone)
	}
	if update.Vocabulary != nil {
		vocab := vocabLists{}
		for _, list := range *update.Vocabulary {
			name := strings.TrimSpace(list.Name)
			if list.OriginalName != "" && list.OriginalName == active {
				nextActive = name
			}
			vocab = append(vocab, vocabList{name, list.Terms})
		}
		if err := validateVocabulary(vocab); err != nil {
			return Settings{}, err
		}
		if nextActive != "" && vocab.index(nextActive) < 0 {
			nextActive = ""
		}
		var b strings.Builder
		b.WriteByte('{')
		for i, list := range vocab {
			if i > 0 {
				b.WriteByte(',')
			}
			name, _ := json.Marshal(list.Name)
			terms, _ := json.Marshal(cleanTerms(list.Terms))
			b.Write(name)
			b.WriteByte(':')
			b.Write(terms)
		}
		b.WriteByte('}')
		raw["vocabulary"] = json.RawMessage(b.String())
	}
	if err := commitConfig(raw, update.Revision); err != nil {
		return Settings{}, err
	}
	if nextActive != active {
		if err := workspaceSave(nextActive); err != nil {
			return Settings{}, fmt.Errorf("settings saved but vocabulary selection could not be updated: %w", err)
		}
	}
	return readSettings()
}

func UpsertProfile(update ProfileUpdate) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	_, raw, err := settingsForRevision(update.Revision)
	if err != nil {
		return Settings{}, err
	}
	cfg, err := configValidate(raw)
	if err != nil {
		return Settings{}, err
	}
	index := -1
	for i, p := range cfg.Profiles {
		if p.ID == update.ID {
			index = i
			break
		}
	}
	var profile Profile
	providerChanged := false
	if update.ID == "" {
		id, err := newProfileID()
		if err != nil {
			return Settings{}, err
		}
		profile = Profile{ID: id, Provider: update.Provider, Config: map[string]json.RawMessage{}}
		if strings.TrimSpace(update.Name) == "" {
			update.Name = nextProfileName(cfg.Profiles, update.Provider)
		}
	} else {
		if index < 0 {
			return Settings{}, errors.New("profile no longer exists")
		}
		profile = cfg.Profiles[index]
		if profile.Config == nil {
			profile.Config = map[string]json.RawMessage{}
		}
		if update.Provider != profile.Provider {
			providerChanged = true
			// Provider-specific settings do not carry across providers. Unknown
			// configuration fields remain intact for forward compatibility.
			delete(profile.Config, "model_id")
			delete(profile.Config, "streaming_mode")
			delete(profile.Config, "languages")
		}
		profile.Provider = update.Provider
	}
	profile.Name = strings.TrimSpace(update.Name)
	delete(profile.Config, "api_key")
	if update.Provider == providerLocal {
		modelChanged := update.ModelID != "" && update.ModelID != profile.ModelID()
		if update.ModelID != "" {
			putConfig(profile.Config, "model_id", update.ModelID)
		} else if index < 0 || providerChanged {
			putConfig(profile.Config, "model_id", localModelUnifiedEnglish)
		}
		if update.StreamingMode != "" {
			putConfig(profile.Config, "streaming_mode", update.StreamingMode)
		} else if index < 0 || providerChanged || modelChanged {
			putConfig(profile.Config, "streaming_mode", defaultStreamingMode(profile.ModelID()))
		}
	}
	if update.Provider == providerOpenAI {
		if update.StreamingMode != "" {
			putConfig(profile.Config, "streaming_mode", update.StreamingMode)
		} else if index < 0 || providerChanged {
			putConfig(profile.Config, "streaming_mode", streamingModeProcess)
		}
		if update.Languages != nil {
			putConfig(profile.Config, "languages", *update.Languages)
		}
	}
	if err := validateProfile(profile); err != nil {
		return Settings{}, err
	}
	if (update.Provider == providerOpenAI || update.Provider == providerOpenRouter) &&
		(update.ID == "" || providerChanged) {
		if _, saved, err := providerAPIKey(update.Provider); err != nil {
			return Settings{}, err
		} else if !saved {
			return Settings{}, fmt.Errorf("add an %s API key in API Keys before saving this profile", providerLabel(update.Provider))
		}
	}
	if index < 0 {
		first := len(cfg.Profiles) == 0
		cfg.Profiles = append(cfg.Profiles, profile)
		if first {
			cfg.ActiveProfile = profile.ID
		}
	} else {
		cfg.Profiles[index] = profile
	}
	if err := validateProfiles(cfg.Profiles, cfg.ActiveProfile); err != nil {
		return Settings{}, err
	}
	putConfig(raw, "profiles", cfg.Profiles)
	putConfig(raw, "active_profile", cfg.ActiveProfile)
	if err := commitConfig(raw, update.Revision); err != nil {
		return Settings{}, err
	}
	return readSettings()
}
func DeleteProfile(revision, id string) (Settings, error) {
	return changeProfileSelection(revision, id, true)
}
func SelectProfile(revision, id string) (Settings, error) {
	return changeProfileSelection(revision, id, false)
}
func changeProfileSelection(revision, id string, remove bool) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	_, raw, err := settingsForRevision(revision)
	if err != nil {
		return Settings{}, err
	}
	cfg, err := configValidate(raw)
	if err != nil {
		return Settings{}, err
	}
	index := -1
	for i, p := range cfg.Profiles {
		if p.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return Settings{}, errors.New("choose an existing profile")
	}
	if remove {
		cfg.Profiles = append(cfg.Profiles[:index], cfg.Profiles[index+1:]...)
		if cfg.ActiveProfile == id {
			cfg.ActiveProfile = ""
		}
	} else {
		cfg.ActiveProfile = id
	}
	putConfig(raw, "profiles", cfg.Profiles)
	putConfig(raw, "active_profile", cfg.ActiveProfile)
	if err := commitConfig(raw, revision); err != nil {
		return Settings{}, err
	}
	return readSettings()
}
func SaveAPIKey(provider, key string) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if _, _, err := readConfigDocument(); err != nil {
		return err
	}
	return saveProviderAPIKey(provider, key)
}

func DeleteAPIKey(provider string) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if _, _, err := readConfigDocument(); err != nil {
		return err
	}
	return deleteProviderAPIKey(provider)
}

func SelectWorkspace(name string) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	_, raw, err := readConfigDocument()
	if err != nil {
		return err
	}
	cfg, err := configValidate(raw)
	if err != nil {
		return err
	}
	if name != "" && (name == generalWorkspace || !cfg.hasWorkspace(name)) {
		return errors.New("choose an existing vocabulary or general only")
	}
	return workspaceSave(name)
}

// Retained for older management clients; normal loads now recover automatically.
func RecoveryRevision() (string, error) {
	data, err := os.ReadFile(configFilePath())
	return configRevision(data), err
}
func ResetBrokenSettings(revision string) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		return Settings{}, err
	}
	if configRevision(data) != revision {
		return Settings{}, errors.New("settings changed elsewhere; reload before resetting")
	}
	if _, err := configParse(data); err == nil {
		return Settings{}, errors.New("configuration is valid; reload instead of resetting")
	}
	return readSettings()
}

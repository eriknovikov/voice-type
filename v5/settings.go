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
)

// Settings contains masked profile metadata. Full credentials require an
// explicit reveal/copy action and never travel with routine settings reads.
type Settings struct {
	Revision      string           `json:"revision"`
	Port          int              `json:"port"`
	Microphone    string           `json:"microphone"`
	Vocabulary    []VocabularyList `json:"vocabulary"`
	Workspace     string           `json:"workspace"`
	Profiles      []ProfileSummary `json:"profiles"`
	ActiveProfile string           `json:"active_profile"`
	Notice        string           `json:"notice"`
}
type ProfileSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
}
type VocabularyList struct {
	OriginalName string   `json:"original_name,omitempty"`
	Name         string   `json:"name"`
	Terms        []string `json:"terms"`
}

// Absent fields are preserved, making independent autosaves safe to serialize.
type SettingsUpdate struct {
	Revision   string            `json:"revision"`
	Port       *int              `json:"port,omitempty"`
	Microphone *string           `json:"microphone,omitempty"`
	Vocabulary *[]VocabularyList `json:"vocabulary,omitempty"`
}
type ProfileUpdate struct {
	Revision string  `json:"revision"`
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	APIKey   *string `json:"api_key,omitempty"`
}

var settingsMu sync.Mutex
var configNotices sync.Map

func ConfigNotice() string {
	value, _ := configNotices.Load(configFilePath())
	text, _ := value.(string)
	return text
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
	return []byte("{\"profiles\":[],\"active_profile\":\"\",\"port\":3232,\"vocabulary\":{\"general\":[]}}\n")
}

// Called with settingsMu held. Desktop and standalone execution follow the
// exact same validation, migration and recovery path.
func readConfigDocument() ([]byte, map[string]json.RawMessage, error) {
	if configFilePath() == "" {
		return nil, nil, errors.New("cannot locate your home directory; restore HOME and reopen Voice Type")
	}
	data, notice, err := ReadPrivateDocument(configFilePath(), "config.jsonc", func(data []byte) error { _, err := configParse(data); return err })
	if err != nil {
		return nil, nil, err
	}
	if notice != "" {
		configNotices.Store(configFilePath(), notice)
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
		backup, err := backupPath("config-before-profiles.jsonc")
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := atomicPrivateWrite(backup, data); err != nil {
				return nil, nil, err
			}
		} else if err != nil {
			return nil, nil, err
		}
		putConfig(raw, "profiles", cfg.Profiles)
		putConfig(raw, "active_profile", cfg.ActiveProfile)
		delete(raw, "provider")
		delete(raw, "api_key")
		delete(raw, "api_keys")
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
		if err := WritePrivateDocument(configFilePath(), "config.jsonc", data); err != nil {
			return nil, nil, err
		}
	}
	return data, raw, nil
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
	out := Settings{Revision: configRevision(data), Port: cfg.Port, Microphone: cfg.Microphone, Workspace: workspaceLoad(), Vocabulary: []VocabularyList{}, Profiles: []ProfileSummary{}, ActiveProfile: cfg.ActiveProfile, Notice: ConfigNotice()}
	for _, list := range cfg.Vocabulary {
		out.Vocabulary = append(out.Vocabulary, VocabularyList{Name: list.Name, Terms: cleanTerms(list.Terms)})
	}
	for _, p := range cfg.Profiles {
		out.Profiles = append(out.Profiles, ProfileSummary{ID: p.ID, Name: p.Name, Provider: p.Provider, MaskedKey: maskedKey(p.APIKey())})
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
	if update.Port != nil {
		putConfig(raw, "port", *update.Port)
	}
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
		if update.Provider != profile.Provider {
			return Settings{}, errors.New("a profile's provider cannot be changed")
		}
	}
	profile.Name = strings.TrimSpace(update.Name)
	if update.APIKey != nil {
		putConfig(profile.Config, "api_key", strings.TrimSpace(*update.APIKey))
	}
	if err := validateProfile(profile); err != nil {
		return Settings{}, err
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
func RevealProfileKey(id string) (string, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	_, raw, err := readConfigDocument()
	if err != nil {
		return "", err
	}
	cfg, err := configValidate(raw)
	if err != nil {
		return "", err
	}
	for _, p := range cfg.Profiles {
		if p.ID == id {
			return p.APIKey(), nil
		}
	}
	return "", errors.New("profile no longer exists")
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

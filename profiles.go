package voicetype

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"strings"
)

// Profile keeps provider-specific configuration together. A future provider can
// validate a different configuration without requiring cloud credentials.
type Profile struct {
	ID       string                     `json:"id"`
	Name     string                     `json:"name"`
	Provider string                     `json:"provider"`
	Config   map[string]json.RawMessage `json:"config"`
	unknown  map[string]json.RawMessage
}

func (p *Profile) UnmarshalJSON(data []byte) error {
	type profileFields Profile
	var fields profileFields
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, name := range []string{"id", "name", "provider", "config"} {
		delete(raw, name)
	}
	*p = Profile(fields)
	p.unknown = raw
	return nil
}

func (p Profile) MarshalJSON() ([]byte, error) {
	fields := maps.Clone(p.unknown)
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	putConfig(fields, "id", p.ID)
	putConfig(fields, "name", p.Name)
	putConfig(fields, "provider", p.Provider)
	putConfig(fields, "config", p.Config)
	return json.Marshal(fields)
}

func (p Profile) APIKey() string {
	var key string
	_ = json.Unmarshal(p.Config["api_key"], &key)
	return strings.TrimSpace(key)
}

func (p Profile) ModelID() string {
	var id string
	_ = json.Unmarshal(p.Config["model_id"], &id)
	return strings.TrimSpace(id)
}

func (p Profile) StreamingMode() streamingMode {
	var mode streamingMode
	_ = json.Unmarshal(p.Config["streaming_mode"], &mode)
	if p.Provider == providerOpenAI && mode == "" {
		return streamingModeNone // Existing profiles keep their upload-after-stop behavior.
	}
	return mode
}

func (p Profile) Languages() []string {
	var languages []string
	_ = json.Unmarshal(p.Config["languages"], &languages)
	return append([]string(nil), languages...)
}

var languageHintPattern = regexp.MustCompile(`^([a-z]{2,3}|zh-(cn|tw|hk))$`)

func validateLanguageHints(languages []string) error {
	seen := make(map[string]bool)
	for _, code := range languages {
		if !languageHintPattern.MatchString(code) || seen[code] {
			return errors.New("language hints must be unique language codes, such as en, es, de, or zh-cn")
		}
		seen[code] = true
	}
	return nil
}

func (c Config) activeProfile() (Profile, bool) {
	for _, p := range c.Profiles {
		if p.ID == c.ActiveProfile {
			return p, true
		}
	}
	return Profile{}, false
}

func validateProfile(p Profile) error {
	if p.ID == "" || strings.TrimSpace(p.Name) == "" || strings.ContainsAny(p.Name, "\r\n") {
		return errors.New("profile ID and name must be nonempty")
	}
	if raw, ok := p.Config["api_key"]; ok {
		var key string
		if string(raw) == "null" || json.Unmarshal(raw, &key) != nil {
			return errors.New("profile API key must be a string")
		}
	}
	switch p.Provider {
	case providerOpenAI, providerOpenRouter:
		if p.Provider == providerOpenAI {
			if raw, ok := p.Config["streaming_mode"]; ok {
				var mode string
				if err := json.Unmarshal(raw, &mode); err != nil {
					return errors.New("invalid OpenAI transcription mode")
				}
			}
			if mode := p.StreamingMode(); mode != streamingModeNone && mode != streamingModeProcess {
				return errors.New("OpenAI supports upload while recording or record then upload")
			}
			if raw, ok := p.Config["languages"]; ok {
				var languages []string
				if err := json.Unmarshal(raw, &languages); err != nil {
					return errors.New("language hints must be a list of language codes")
				}
				if err := validateLanguageHints(languages); err != nil {
					return err
				}
			}
		}
	case providerLocal:
		if !validLocalModelID(p.ModelID()) {
			return errors.New("choose a supported local transcription model")
		}
		if err := validateStreamingMode(p.ModelID(), p.StreamingMode()); err != nil {
			return err
		}
	default:
		return errors.New("unsupported profile provider")
	}
	return nil
}

func validateProfiles(profiles []Profile, active string) error {
	ids, names := map[string]bool{}, map[string]bool{}
	found := active == ""
	for _, p := range profiles {
		if err := validateProfile(p); err != nil {
			return err
		}
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if ids[p.ID] || names[name] {
			return errors.New("profile IDs and names must be unique")
		}
		ids[p.ID], names[name] = true, true
		found = found || p.ID == active
	}
	if !found {
		return errors.New("active_profile does not name an existing profile")
	}
	return nil
}

func migrateLegacyProfiles(cfg Config) ([]Profile, string) {
	profiles := []Profile{}
	active := ""
	for _, provider := range []string{providerOpenAI, providerOpenRouter} {
		key, present := cfg.APIKeys[provider]
		if !present && provider == cfg.Provider {
			key = cfg.APIKey
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		encoded, _ := json.Marshal(key)
		p := Profile{ID: "legacy-" + provider, Name: providerLabel(provider) + " 1", Provider: provider, Config: map[string]json.RawMessage{"api_key": encoded}}
		profiles = append(profiles, p)
		if provider == cfg.Provider {
			active = p.ID
		}
	}
	return profiles, active
}

func providerLabel(provider string) string {
	switch provider {
	case providerOpenAI:
		return "OpenAI"
	case providerLocal:
		return "On-device"
	default:
		return "OpenRouter"
	}
}

func nextProfileName(profiles []Profile, provider string) string {
	for n := 1; ; n++ {
		name := fmt.Sprintf("%s %d", providerLabel(provider), n)
		found := false
		for _, p := range profiles {
			found = found || strings.EqualFold(p.Name, name)
		}
		if !found {
			return name
		}
	}
}

func newProfileID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", errors.New("could not create a profile ID")
	}
	return hex.EncodeToString(id[:]), nil
}

func maskedKey(key string) string {
	runes := []rune(key)
	if len(runes) <= 8 {
		return "••••••••"
	}
	return string(runes[:4]) + "••••" + string(runes[len(runes)-4:])
}

func validateVocabulary(vocab vocabLists) error {
	seen := map[string]bool{}
	for _, list := range vocab {
		name := strings.TrimSpace(list.Name)
		if name == "" || strings.ContainsAny(name, "\r\n") || seen[strings.ToLower(name)] {
			return errors.New("vocabulary names must be unique and nonempty")
		}
		seen[strings.ToLower(name)] = true
		for _, term := range list.Terms {
			if strings.ContainsAny(term, "<>\r\n") {
				return errors.New("terms cannot contain angle brackets or line breaks")
			}
		}
	}
	return nil
}

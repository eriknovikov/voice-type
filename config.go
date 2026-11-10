package voicetype

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

// Config holds file settings. Unknown fields remain compatible with older files.
type Config struct {
	Profiles      []Profile
	ActiveProfile string
	// Legacy fields are used only while migrating old files and by diagnostics.
	Provider   string
	APIKey     string
	APIKeys    map[string]string
	Microphone string
	// Vocabulary is the named term lists. generalWorkspace rides along with
	// every dictation; the rest are selected one at a time in the desktop app.
	Vocabulary vocabLists
}

// generalWorkspace is the reserved name for terms that are always sent. It is
// merged with whichever workspace is active rather than being one of them.
const generalWorkspace = "general"

// vocabLists holds the lists in the order the config file writes them, so
// the desktop editor follows the user's file order. A handful of
// entries makes a linear scan cheaper than a map plus the ordering it would
// still have to carry alongside.
type vocabLists []vocabList

// vocabList is one named list of terms.
type vocabList struct {
	Name  string
	Terms []string
}

func (v vocabLists) index(name string) int {
	for i := range v {
		if v[i].Name == name {
			return i
		}
	}
	return -1
}

// Tuned once, on purpose, and not user-facing. Changing one means editing this
// block and shipping a release -- which is the point.
const (
	// maxDurationSeconds caps one dictation. Without silence auto-stop, a
	// forgotten hotkey would otherwise record -- and bill -- indefinitely.
	maxDurationSeconds = 600
)

const (
	providerOpenAI     = "openai"
	providerOpenRouter = "openrouter"
	providerLocal      = "local"
	defaultModelID     = "gpt-transcribe"

	localModelUnifiedEnglish = "parakeet-unified-en"
	localModelParakeetV3     = "parakeet-tdt-v3"

	streamingModeProcess = "process"
	streamingModeLive    = "live"
	streamingModeNone    = "none"
)

type streamingMode string

func configDefaults() Config {
	return Config{
		Provider: providerOpenRouter,
	}
}

// sttSettings is a snapshot for one recording. Reloads never mutate it during
// capture or transcription.
type sttSettings struct {
	Provider      string
	Model         string
	StreamingMode streamingMode
	APIKey        string
	Vocabulary    []string
	Languages     []string
	Workspace     string
}

func providerModel(provider string) string {
	switch provider {
	case providerOpenRouter:
		return "openai/" + defaultModelID
	case providerLocal:
		return localModelUnifiedEnglish
	default:
		return defaultModelID
	}
}

func (s sttSettings) validateCredentials() error {
	switch s.Provider {
	case providerOpenAI, providerOpenRouter:
		if s.APIKey == "" {
			return errors.New("add the provider API key in API Keys")
		}
	case providerLocal:
		if !validLocalModelID(s.Model) {
			return errors.New("select a supported local model in General")
		}
		if err := validateStreamingMode(s.Model, s.StreamingMode); err != nil {
			return err
		}
	default:
		return errors.New("select a supported transcription profile in General")
	}
	return nil
}

func (c Config) resolveSTT(workspace string) (sttSettings, error) {
	if c.Profiles != nil {
		profile, ok := c.activeProfile()
		if !ok {
			return sttSettings{}, errors.New("select a profile in General before recording")
		}
		c.Provider = profile.Provider
	}
	if c.Provider != providerOpenAI && c.Provider != providerOpenRouter && c.Provider != providerLocal {
		return sttSettings{}, errors.New("provider must be openai, openrouter, or local")
	}
	s := sttSettings{Provider: c.Provider, Model: providerModel(c.Provider), Workspace: workspace}
	if c.Provider == providerOpenAI || c.Provider == providerOpenRouter {
		key, saved, err := providerAPIKey(c.Provider)
		if err != nil {
			return sttSettings{}, err
		}
		if !saved {
			return sttSettings{}, fmt.Errorf("add an %s API key in API Keys before recording", providerLabel(c.Provider))
		}
		s.APIKey = key
	}
	if profile, ok := c.activeProfile(); ok {
		if profile.Provider == providerLocal {
			s.Model = profile.ModelID()
		}
		s.StreamingMode = profile.StreamingMode()
		s.Languages = profile.Languages()
	}
	if c.Provider == providerOpenAI {
		if err := c.validateKeywords(workspace); err != nil {
			return sttSettings{}, err
		}
	}
	s.Vocabulary = c.activeVocabulary(workspace)
	return s, nil
}

func (c Config) validateKeywords(workspace string) error {
	for _, list := range c.Vocabulary {
		if list.Name != generalWorkspace && list.Name != workspace {
			continue
		}
		for i, term := range list.Terms {
			if strings.ContainsAny(term, "<>\r\n") {
				return fmt.Errorf("vocabulary workspace %q term %d: OpenAI keywords cannot contain angle brackets or line breaks", list.Name, i+1)
			}
		}
	}
	return nil
}

// workspaces lists the switchable workspaces -- everything but general -- in the
// order the config file declares them, which is the order the user reads.
func (c Config) workspaces() []string {
	out := make([]string, 0, len(c.Vocabulary))
	for _, list := range c.Vocabulary {
		if list.Name != generalWorkspace {
			out = append(out, list.Name)
		}
	}
	return out
}

// hasWorkspace reports whether the config file declares this workspace. An
// empty one still counts: it is a placeholder the user can set.
func (c Config) hasWorkspace(name string) bool { return c.Vocabulary.index(name) >= 0 }

// terms returns one workspace's terms.
func (c Config) terms(name string) []string {
	if i := c.Vocabulary.index(name); i >= 0 {
		return cleanTerms(c.Vocabulary[i].Terms)
	}
	return nil
}

// vocabularyFor merges the general terms with the named workspace. General goes
// first: a provider that truncates a long prompt keeps the tail, and the
// workspace the user switched to on purpose is the half worth keeping.
func (c Config) vocabularyFor(workspace string) []string {
	merged := append([]string{}, c.terms(generalWorkspace)...)
	if workspace != generalWorkspace {
		merged = append(merged, c.terms(workspace)...)
	}
	return cleanTerms(merged)
}

// activeVocabulary is vocabularyFor for the workspace the state file names. A
// workspace that has since been renamed or deleted in the config warns once and
// leaves the general terms doing their job.
func (c Config) activeVocabulary(workspace string) []string {
	warning := ""
	if workspace != "" && !c.hasWorkspace(workspace) {
		warning = fmt.Sprintf("%q is not in the config file -- sending %s only", workspace, generalWorkspace)
	}
	configWarn("workspace", warning)
	return c.vocabularyFor(workspace)
}

// cleanTerms trims, drops blanks, and removes case-insensitive duplicates,
// keeping the first spelling. Every surviving term is billed on every
// dictation, and general overlapping a workspace is the normal way to write it.
func cleanTerms(terms []string) []string {
	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		key := strings.ToLower(term)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, term)
	}
	return out
}

// parseVocabulary accepts the workspace object -- read as a token stream, which
// is what keeps the lists in the order the file writes them -- and still accepts
// the flat array v5 shipped first, which is exactly what the general list is now.
func parseVocabulary(raw json.RawMessage) (vocabLists, error) {
	var flat []string
	if err := json.Unmarshal(raw, &flat); err == nil {
		if len(flat) == 0 {
			return nil, nil
		}
		return vocabLists{{generalWorkspace, flat}}, nil
	}

	badShape := errors.New("must be an object of \"workspace\": [terms] (or a flat array), ignoring it")

	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, badShape
	}

	var lists vocabLists
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, badShape
		}
		var terms []string
		if err := dec.Decode(&terms); err != nil {
			return nil, badShape
		}
		name, _ := key.(string) // an object key is always a string
		if name = strings.TrimSpace(name); name == "" {
			return nil, errors.New("vocabulary names must be nonempty")
		}
		// JSON says a repeated key wins with its last value; the row stays where
		// it first appeared so the list still reads down the file.
		if i := lists.index(name); i >= 0 {
			lists[i].Terms = terms
		} else {
			// Validate original keywords before trimming can hide line breaks.
			lists = append(lists, vocabList{name, terms})
		}
	}
	if len(lists) == 0 {
		return nil, nil
	}
	return lists, nil
}

// configFilePath is the one and only config location: ~/.voice-type-config.jsonc,
// the same path install.sh writes. An empty result is reported by the loader;
// it must never terminate the desktop process or use a relative file.
func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".voice-type-config.jsonc")
}

// stripJsoncComments removes // line comments while leaving string literals
// untouched. Ported verbatim from stripJsoncComments in src/config.ts.
func stripJsoncComments(text string) string {
	var out []rune
	runes := []rune(text)
	inString := false
	escapeNext := false

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if escapeNext {
			out = append(out, ch)
			escapeNext = false
			continue
		}

		if inString {
			out = append(out, ch)
			if ch == '\\' {
				escapeNext = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}

		if ch == '/' && i+1 < len(runes) && runes[i+1] == '/' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			if i < len(runes) {
				out = append(out, runes[i])
			}
			continue
		}

		out = append(out, ch)
	}

	return string(out)
}

// stripTrailingCommas removes commas that are followed only by whitespace and a
// closing brace or bracket. Ported verbatim from src/config.ts -- this is the
// fix from commit fb4ed00, needed because stripping a trailing // comment can
// leave the comma before it dangling at the end of an object.
func stripTrailingCommas(text string) string {
	var out []rune
	runes := []rune(text)
	inString := false
	escapeNext := false

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if escapeNext {
			out = append(out, ch)
			escapeNext = false
			continue
		}

		if inString {
			out = append(out, ch)
			if ch == '\\' {
				escapeNext = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}

		if ch == ',' {
			j := i + 1
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			if j < len(runes) && (runes[j] == '}' || runes[j] == ']') {
				continue
			}
		}

		out = append(out, ch)
	}

	return string(out)
}

// lastWarning suppresses repeats. The config is re-read on every dictation, so
// a warning that fires once per read would bury the log in the same line.
var lastWarning sync.Map

func configWarn(field, reason string) {
	if reason == "" {
		lastWarning.Delete(field)
		return
	}
	if prev, seen := lastWarning.Load(field); seen && prev == reason {
		return
	}
	lastWarning.Store(field, reason)
	logf("CONFIG", "%s: %s", field, reason)
}

// Provider errors must not silently select another destination. Unknown fields
// remain compatible with older config files.
func configValidate(raw map[string]json.RawMessage) (Config, error) {
	cfg := configDefaults()
	if v, ok := raw["provider"]; ok {
		var provider string
		if err := json.Unmarshal(v, &provider); err != nil ||
			(provider != providerOpenAI && provider != providerOpenRouter) {
			return cfg, errors.New("provider must be openai or openrouter")
		}
		cfg.Provider = provider
	}

	if v, ok := raw["api_key"]; ok {
		if err := json.Unmarshal(v, &cfg.APIKey); err != nil {
			return cfg, errors.New("api_key must be a string")
		}
		cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	}

	if v, ok := raw["api_keys"]; ok {
		if err := json.Unmarshal(v, &cfg.APIKeys); err != nil {
			return cfg, errors.New("api_keys must map provider names to keys")
		}
	}
	if v, ok := raw["microphone"]; ok {
		if err := json.Unmarshal(v, &cfg.Microphone); err != nil {
			return cfg, errors.New("microphone must be a string")
		}
	}
	modelWarning := ""
	if v, ok := raw["model"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil || s != defaultModelID {
			modelWarning = "legacy model field is ignored; provider profiles choose the model"
		}
	}
	configWarn("model", modelWarning)

	if v, ok := raw["vocabulary"]; ok {
		var err error
		cfg.Vocabulary, err = parseVocabulary(v)
		if err != nil {
			return cfg, errors.New("vocabulary must contain named lists of terms")
		}
		if err := validateVocabulary(cfg.Vocabulary); err != nil {
			return cfg, err
		}
	}
	if v, ok := raw["profiles"]; ok {
		if err := json.Unmarshal(v, &cfg.Profiles); err != nil || cfg.Profiles == nil {
			return cfg, errors.New("profiles must be a list")
		}
		if v, ok := raw["active_profile"]; ok {
			if err := json.Unmarshal(v, &cfg.ActiveProfile); err != nil {
				return cfg, errors.New("active_profile must be a profile ID")
			}
		}
		if err := validateProfiles(cfg.Profiles, cfg.ActiveProfile); err != nil {
			return cfg, err
		}
	} else {
		cfg.Profiles, cfg.ActiveProfile = migrateLegacyProfiles(cfg)
	}
	if profile, ok := cfg.activeProfile(); ok {
		cfg.Provider = profile.Provider
	}
	return cfg, nil
}

// configParse turns raw JSONC bytes into a Config. A malformed file yields
// defaults plus an error; callers decide whether that is fatal.
func configParse(data []byte) (Config, error) {
	stripped := stripTrailingCommas(stripJsoncComments(string(data)))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripped), &raw); err != nil {
		return configDefaults(), errors.New("configuration is not valid JSON")
	}
	if raw == nil {
		return configDefaults(), errors.New("config must be a JSON object")
	}

	return configValidate(raw)
}

// configReadStrict and the settings API share recovery, migration, and validation.
func configReadStrict() (Config, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	_, raw, err := readConfigDocument()
	if err != nil {
		return Config{}, err
	}
	return configValidate(raw)
}


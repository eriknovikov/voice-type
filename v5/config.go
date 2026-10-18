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

// Config holds file settings. Model overrides belong to the process, not the
// file. Unknown fields remain compatible with older files.
type Config struct {
	Provider string
	APIKey   string
	Port     int
	// Vocabulary is the named term lists. generalWorkspace rides along with
	// every dictation; the rest are one at a time, picked with `voice-type
	// vocab set`, so a term only costs where it earns its keep.
	Vocabulary vocabLists
}

// generalWorkspace is the reserved name for terms that are always sent. It is
// merged with whichever workspace is active rather than being one of them.
const generalWorkspace = "general"

// vocabLists holds the lists in the order the config file writes them, so
// `voice-type vocab ls` reads straight down the user's own file. A handful of
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

const defaultPort = 3232

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
	defaultModelID     = "gpt-transcribe"
)

func configDefaults() Config {
	return Config{
		Provider: providerOpenRouter,
		Port:     defaultPort,
	}
}

// sttSettings is a snapshot for one recording. Reloads never mutate it during
// capture or transcription.
type sttSettings struct {
	Provider   string
	Model      string
	APIKey     string
	Vocabulary []string
	Workspace  string
}

func providerModel(provider string) string {
	if provider == providerOpenRouter {
		return "openai/" + defaultModelID
	}
	return defaultModelID
}

func (s sttSettings) supportedModel() bool { return s.Model == providerModel(s.Provider) }

func (c Config) keyEnv() string {
	if c.Provider == providerOpenAI {
		return "OPENAI_API_KEY"
	}
	return "OPENROUTER_API_KEY"
}

func (s sttSettings) validateCredentials() error {
	if s.APIKey == "" {
		return fmt.Errorf("no %s API key. Set %s or api_key in %s", s.Provider,
			(Config{Provider: s.Provider}).keyEnv(), configFilePath())
	}
	return nil
}

func (c Config) resolveSTT(modelOverride, workspace string) (sttSettings, error) {
	if c.Provider != providerOpenAI && c.Provider != providerOpenRouter {
		return sttSettings{}, errors.New("provider must be openai or openrouter")
	}
	model := modelOverride
	if model == "" || model == defaultModelID {
		model = providerModel(c.Provider)
	}
	s := sttSettings{Provider: c.Provider, Model: model, APIKey: configAPIKey(c), Workspace: workspace}
	if !s.supportedModel() {
		configWarn("override", "unsupported model override: vocabulary and model-specific hints are disabled")
		return s, nil
	}
	configWarn("override", "")
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
			continue
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
// the same path install.sh writes. Without a home directory there is nowhere
// else to look, so stop rather than fall back to a stray relative file.
func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "voice-type: cannot locate home directory: %v\n", err)
		os.Exit(1)
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

	portWarning := ""
	if v, ok := raw["port"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err == nil && n >= 1024 && n <= 65535 {
			cfg.Port = n
		} else {
			portWarning = fmt.Sprintf("invalid value, using default %d", cfg.Port)
		}
	}
	configWarn("port", portWarning)

	if v, ok := raw["api_key"]; ok {
		if err := json.Unmarshal(v, &cfg.APIKey); err != nil {
			return cfg, errors.New("api_key must be a string")
		}
		cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	}

	modelWarning := ""
	if v, ok := raw["model"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err != nil || s != defaultModelID {
			modelWarning = "legacy model field is ignored. Use --model for an unsupported override"
		}
	}
	configWarn("model", modelWarning)

	vocabularyWarning := ""
	if v, ok := raw["vocabulary"]; ok {
		vocabulary, err := parseVocabulary(v)
		if err != nil {
			vocabularyWarning = err.Error()
		} else {
			cfg.Vocabulary = vocabulary
		}
	}

	configWarn("vocabulary", vocabularyWarning)

	return cfg, nil
}

// configParse turns raw JSONC bytes into a Config. A malformed file yields
// defaults plus an error; callers decide whether that is fatal.
func configParse(data []byte) (Config, error) {
	stripped := stripTrailingCommas(stripJsoncComments(string(data)))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripped), &raw); err != nil {
		return configDefaults(), fmt.Errorf("could not parse config file: %w", err)
	}
	if raw == nil {
		return configDefaults(), errors.New("config must be a JSON object")
	}

	return configValidate(raw)
}

// Nothing writes the config. A missing file means defaults, but invalid files
// must block recording rather than silently using old credentials or routes.
func configReadStrict() (Config, error) {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return configValidate(nil)
		}
		return Config{}, fmt.Errorf("could not read %s: %w", configFilePath(), err)
	}
	cfg, err := configParse(data)
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", configFilePath(), err)
	}
	return cfg, nil
}

// Only the selected provider's environment key can replace the file key.
func configAPIKey(cfg Config) string {
	if k := strings.TrimSpace(os.Getenv(cfg.keyEnv())); k != "" {
		return k
	}
	return strings.TrimSpace(cfg.APIKey)
}

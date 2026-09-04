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

// Config holds the four user-facing settings. Unknown fields are ignored so
// older config files keep loading.
type Config struct {
	APIKey string
	Port   int
	// Model is an sttModels ID. Vocabulary travels with the audio, so names
	// come out spelled right instead of being corrected afterwards.
	Model string
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

// sttBias is how a model takes vocabulary conditioning. The shape has to match
// the provider namespace that OpenRouter forwards for that model.
type sttBias int

const (
	// biasNone is a model that measurably discards a vocabulary. Sending one
	// only pays to upload it.
	biasNone sttBias = iota
	// biasPrompt is the Whisper lineage: a sentence prepended to the decoder
	// context, which the model can echo back instead of transcribing.
	biasPrompt
	// biasPhraseList is Azure keyword biasing -- terms, not prose, weighted
	// during decoding. There is no prompt to echo.
	biasPhraseList
)

// sttSpec is one allowed model. Provider names the provider.options namespace;
// OpenRouter does not apply provider routing preferences to STT requests.
type sttSpec struct {
	ID         string
	Slug       string
	Provider   string
	Bias       sttBias
	USDPerHour float64
	FallbackID string
}

type sttRoute struct {
	Primary  sttSpec
	Fallback *sttSpec
}

// TakesVocabulary reports whether sending terms with the audio does anything.
func (s sttSpec) TakesVocabulary() bool { return s.Bias != biasNone }

const defaultModelID = "mai-transcribe-2"

// sttModels is the model allowlist shown by `voice-type models`.
var sttModels = []sttSpec{
	{ID: "mai-transcribe-2", Slug: "microsoft/mai-transcribe-2", Provider: "azure", Bias: biasPhraseList, USDPerHour: 0.100, FallbackID: "gpt-transcribe"},
	{ID: "gpt-4o-transcribe", Slug: "openai/gpt-4o-transcribe", Provider: "openai", Bias: biasPrompt, USDPerHour: 0.224},
	{ID: "gpt-transcribe", Slug: "openai/gpt-transcribe", Provider: "openai", Bias: biasPrompt, USDPerHour: 0.270},
	{ID: "whisper-large-v3", Slug: "openai/whisper-large-v3", Provider: "deepinfra", Bias: biasPrompt, USDPerHour: 0.027},
	{ID: "whisper-1", Slug: "openai/whisper-1", Provider: "openai", Bias: biasPrompt, USDPerHour: 0.364},
	{ID: "whisper-large-v3-turbo", Slug: "openai/whisper-large-v3-turbo", Provider: "deepinfra", Bias: biasPrompt, USDPerHour: 0.012},
	{ID: "gpt-4o-mini-transcribe", Slug: "openai/gpt-4o-mini-transcribe", Provider: "openai", Bias: biasPrompt, USDPerHour: 0.111},
	{ID: "parakeet-v3", Slug: "nvidia/parakeet-tdt-0.6b-v3", Provider: "together", Bias: biasNone, USDPerHour: 0.090},
	{ID: "whisper-large-v3-turbo-groq", Slug: "openai/whisper-large-v3-turbo", Provider: "groq", Bias: biasNone, USDPerHour: 0.012},
}

func sttLookup(id string) (sttSpec, bool) {
	for _, m := range sttModels {
		if m.ID == id {
			return m, true
		}
	}
	return sttSpec{}, false
}

func sttRouteFor(primary sttSpec) sttRoute {
	route := sttRoute{Primary: primary}
	if primary.FallbackID != "" {
		if fallback, ok := sttLookup(primary.FallbackID); ok {
			route.Fallback = &fallback
		}
	}
	return route
}

func validateSTTCatalog(models []sttSpec) error {
	byID := make(map[string]sttSpec, len(models))
	for _, model := range models {
		if _, exists := byID[model.ID]; exists {
			return fmt.Errorf("duplicate STT model %q", model.ID)
		}
		byID[model.ID] = model
	}
	for _, model := range models {
		if model.FallbackID == "" {
			continue
		}
		if model.FallbackID == model.ID {
			return fmt.Errorf("STT model %q falls back to itself", model.ID)
		}
		if _, ok := byID[model.FallbackID]; !ok {
			return fmt.Errorf("STT model %q has missing fallback %q", model.ID, model.FallbackID)
		}

		seen := map[string]bool{model.ID: true}
		for next := model.FallbackID; next != ""; next = byID[next].FallbackID {
			if seen[next] {
				return fmt.Errorf("STT fallback cycle includes %q", next)
			}
			seen[next] = true
			if _, ok := byID[next]; !ok {
				break
			}
		}
	}
	return nil
}

func configDefaults() Config {
	return Config{
		Port:   defaultPort,
		APIKey: "",
		Model:  defaultModelID,
	}
}

// modelSpec is always found: validation only ever stores an allowlisted ID.
func (c Config) modelSpec() sttSpec { m, _ := sttLookup(c.Model); return m }

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
		return c.Vocabulary[i].Terms
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
		terms := cleanTerms(flat)
		if len(terms) == 0 {
			return nil, nil
		}
		return vocabLists{{generalWorkspace, terms}}, nil
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
			lists[i].Terms = cleanTerms(terms)
		} else {
			lists = append(lists, vocabList{name, cleanTerms(terms)})
		}
	}
	if len(lists) == 0 {
		return nil, nil
	}
	return lists, nil
}

// configFilePath honours XDG_CONFIG_HOME, matching v4's resolution order.
func configFilePath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "voice-type.jsonc"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "voice-type.jsonc")
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

// configValidate applies the four known fields over the defaults, warning about
// (and then ignoring) values of the wrong type or out of range. Every other
// field is not touched at all, so both v4 configs and configs written by
// earlier v5 installers load cleanly.
func configValidate(raw map[string]json.RawMessage) Config {
	cfg := configDefaults()

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

	apiKeyWarning := ""
	if v, ok := raw["api_key"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			cfg.APIKey = s
		} else {
			apiKeyWarning = "must be a string, using default"
		}
	}
	configWarn("api_key", apiKeyWarning)

	modelWarning := ""
	if v, ok := raw["model"]; ok {
		var s string
		json.Unmarshal(v, &s) // anything but an allowlisted ID warns below
		if _, found := sttLookup(s); found {
			cfg.Model = s
		} else {
			modelWarning = fmt.Sprintf("%q is not a model voice-type ships with, using %s -- see `voice-type models`", s, cfg.Model)
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

	// A vocabulary the model discards is billed on every dictation for nothing.
	if spec := cfg.modelSpec(); len(cfg.Vocabulary) > 0 && !spec.TakesVocabulary() {
		vocabularyWarning = spec.ID + " ignores it -- see `voice-type models`"
	}
	configWarn("vocabulary", vocabularyWarning)

	return cfg
}

// configParse turns raw JSONC bytes into a Config. A malformed file yields
// defaults plus an error; callers decide whether that is fatal.
func configParse(data []byte) (Config, error) {
	stripped := stripTrailingCommas(stripJsoncComments(string(data)))

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stripped), &raw); err != nil {
		return configDefaults(), fmt.Errorf("could not parse config file: %w", err)
	}

	return configValidate(raw), nil
}

// configLoad reads the config file. Nothing writes it: voice-type only ever
// reads this file. A missing file simply means defaults.
func configLoad() Config {
	cfg, err := configReadStrict()
	if err != nil {
		configWarn("file", err.Error()+" -- using defaults")
		return configDefaults()
	}
	lastWarning.Delete("file")
	return cfg
}

// configReadStrict is configLoad without fallback, for commands that need to
// report a malformed file rather than silently continue with defaults.
func configReadStrict() (Config, error) {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return configValidate(nil), nil
		}
		return Config{}, fmt.Errorf("could not read %s: %w", configFilePath(), err)
	}
	cfg, err := configParse(data)
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", configFilePath(), err)
	}
	return cfg, nil
}

// configAPIKey prefers the environment over the config file.
func configAPIKey(cfg Config) string {
	if k := os.Getenv("OPENROUTER_API_KEY"); k != "" {
		return k
	}
	return cfg.APIKey
}

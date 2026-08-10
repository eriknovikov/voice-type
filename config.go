package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode"
)

// Config holds the fields v5 consumes. Every other field found in the file --
// including all of v4's browser/language settings -- is ignored silently.
type Config struct {
	Port         int
	APIKey       string
	Model        string
	MaxDuration  int
	PasteKey     string
	PasteDelayMs int
	TrimSilence  bool
}

const (
	defaultPort         = 3232
	defaultModel        = "nvidia/parakeet-tdt-0.6b-v3"
	defaultMaxDuration  = 600
	defaultPasteKey     = "ctrl+v"
	defaultPasteDelayMs = 300
	defaultTrimSilence  = true
)

func configDefaults() Config {
	return Config{
		Port:         defaultPort,
		APIKey:       "",
		Model:        defaultModel,
		MaxDuration:  defaultMaxDuration,
		PasteKey:     defaultPasteKey,
		PasteDelayMs: defaultPasteDelayMs,
		TrimSilence:  defaultTrimSilence,
	}
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

func configWarn(field, reason string) {
	logf("CONFIG", "%s: %s", field, reason)
}

// configValidate applies each known field over the defaults, warning about (and
// then ignoring) values of the wrong type or out of range. Unknown fields are
// not touched at all, so v4 configs load cleanly.
func configValidate(raw map[string]json.RawMessage) Config {
	cfg := configDefaults()

	if v, ok := raw["port"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err == nil && n >= 1024 && n <= 65535 {
			cfg.Port = n
		} else {
			configWarn("port", fmt.Sprintf("invalid value, using default %d", cfg.Port))
		}
	}

	if v, ok := raw["api_key"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			cfg.APIKey = s
		} else {
			configWarn("api_key", "must be a string, using default")
		}
	}

	if v, ok := raw["model"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			cfg.Model = s
		} else {
			configWarn("model", fmt.Sprintf("must be a non-empty string, using default %s", cfg.Model))
		}
	}

	if v, ok := raw["max_duration"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err == nil && n > 0 {
			cfg.MaxDuration = n
		} else {
			configWarn("max_duration", fmt.Sprintf("must be a positive integer, using default %d", cfg.MaxDuration))
		}
	}

	if v, ok := raw["paste_key"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			if _, perr := parseKeyChord(s); perr == nil {
				cfg.PasteKey = s
			} else {
				configWarn("paste_key", fmt.Sprintf("%v, using default %s", perr, cfg.PasteKey))
			}
		} else {
			configWarn("paste_key", fmt.Sprintf("must be a non-empty string, using default %s", cfg.PasteKey))
		}
	}

	if v, ok := raw["paste_delay_ms"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err == nil && n >= 0 {
			cfg.PasteDelayMs = n
		} else {
			configWarn("paste_delay_ms", fmt.Sprintf("must be a non-negative integer, using default %d", cfg.PasteDelayMs))
		}
	}

	if v, ok := raw["trim_silence"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			cfg.TrimSilence = b
		} else {
			configWarn("trim_silence", fmt.Sprintf("must be a boolean, using default %t", cfg.TrimSilence))
		}
	}

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

// configLoad reads the config file. v5 never writes it: a missing file simply
// means defaults, so migration can never clobber a v4 config.
func configLoad() Config {
	path := configFilePath()

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("CONFIG", "could not read %s: %v -- using defaults", path, err)
		}
		return configDefaults()
	}

	cfg, err := configParse(data)
	if err != nil {
		logf("CONFIG", "%v -- using defaults", err)
		return cfg
	}

	return cfg
}

// configAPIKey prefers the environment over the config file.
func configAPIKey(cfg Config) string {
	if k := os.Getenv("OPENROUTER_API_KEY"); k != "" {
		return k
	}
	return cfg.APIKey
}

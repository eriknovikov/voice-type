package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode"
)

// Config holds the only two things a user gets to set. Everything else is a
// tuned constant below: an option the user has to reason about is a cost, and
// none of these were ever worth that cost. Every other field found in the file
// -- v5's own earlier knobs, and all of v4's browser/language settings -- is
// ignored silently, so any existing config keeps loading.
type Config struct {
	Port   int
	APIKey string
}

const defaultPort = 3232

// Tuned once, on purpose, and not user-facing. Changing one means editing this
// block and shipping a release -- which is the point.
const (
	// sttModel is Parakeet: 16 kHz mono PCM16 native, so nothing resamples.
	sttModel = "nvidia/parakeet-tdt-0.6b-v3"
	// maxDurationSeconds caps one dictation. Without silence auto-stop, a
	// forgotten hotkey would otherwise record -- and bill -- indefinitely.
	maxDurationSeconds = 600
)

func configDefaults() Config {
	return Config{
		Port:   defaultPort,
		APIKey: "",
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

// configValidate applies the two known fields over the defaults, warning about
// (and then ignoring) values of the wrong type or out of range. Every other
// field is not touched at all, so both v4 configs and configs written by
// earlier v5 installers load cleanly.
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

// configLoad reads the config file. The daemon never writes it: a missing file
// simply means defaults, so migration can never clobber a v4 config.
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

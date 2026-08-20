package voicetype

import (
	"os"
	"reflect"
	"testing"
)

// Cases ported from tests/config.test.ts.
func TestStripJsoncComments(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"whole-line comment", "// c\n{}", "\n{}"},
		{"trailing comment", `{"a": 1 // c` + "\n}", `{"a": 1 ` + "\n}"},
		{"url inside a string is not a comment", `"http://x" // c`, `"http://x" `},
		{"slashes inside a string survive", `"a//b"`, `"a//b"`},
		{"escaped quote does not end the string", `{"a":"x\"//y"}`, `{"a":"x\"//y"}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripJsoncComments(c.in); got != c.want {
				t.Errorf("stripJsoncComments(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Cases ported from tests/config.test.ts, including the fb4ed00 fix.
func TestStripTrailingCommas(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"object", `{"a":1,}`, `{"a":1}`},
		{"object across newlines", `{"a":1,` + "\n\n\n}", `{"a":1` + "\n\n\n}"},
		{"array", "[1,2,]", "[1,2]"},
		{"comma inside a string survives", `{"a":"x, }"}`, `{"a":"x, }"}`},
		{"real comma untouched", `{"a":1,"b":2}`, `{"a":1,"b":2}`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripTrailingCommas(c.in); got != c.want {
				t.Errorf("stripTrailingCommas(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// A trailing // comment leaves the comma before it dangling -- the two strippers
// only work composed, in this order. This is the fb4ed00 regression.
func TestStrippersCompose(t *testing.T) {
	in := `{
    "port": 3232, // the port
    "api_key": "sk-or-x" // the key
}`
	cfg, err := configParse([]byte(in))
	if err != nil {
		t.Fatalf("configParse: %v", err)
	}
	if cfg.Port != 3232 || cfg.APIKey != "sk-or-x" {
		t.Errorf("got port=%d api_key=%q", cfg.Port, cfg.APIKey)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := configParse([]byte(`{}`))
	if err != nil {
		t.Fatalf("configParse: %v", err)
	}
	want := configDefaults()
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

// v4 configs must load without error, with every v4-only field ignored.
func TestConfigToleratesV4Fields(t *testing.T) {
	v4 := `{
    "port": 3232, // int 1024-65535, default 3232
    "lang": "en-US",
    "browser_type": "chromium",
    "browser_path": "/usr/bin/chromium-browser",
    "stream": true,
    "timeout": 0,
    "sound": false,
    "text": false,
    "punctuation": true
}`
	cfg, err := configParse([]byte(v4))
	if err != nil {
		t.Fatalf("v4 config must parse, got %v", err)
	}
	if cfg.Port != 3232 {
		t.Errorf("port = %d, want 3232", cfg.Port)
	}
}

// Configs written by earlier v5 installers set knobs that are constants now.
// They must load exactly like a v4 config: no error, no warning path, ignored.
func TestConfigToleratesRetiredV5Fields(t *testing.T) {
	old := `{
    "api_key": "sk-or-x",
    "port": 4000,
    "max_duration": 60,
    "paste_key": "ctrl+shift+v",
    "paste_delay_ms": 900,
    "trim_silence": false
}`
	cfg, err := configParse([]byte(old))
	if err != nil {
		t.Fatalf("retired fields must parse, got %v", err)
	}
	if cfg.Port != 4000 || cfg.APIKey != "sk-or-x" {
		t.Errorf("got %+v, want port=4000 api_key=sk-or-x", cfg)
	}
}

func TestConfigIgnoresUnknownFields(t *testing.T) {
	cfg, err := configParse([]byte(`{"totally_unknown": {"nested": [1,2,3]}, "port": 4000}`))
	if err != nil {
		t.Fatalf("unknown fields must not fail the parse, got %v", err)
	}
	if cfg.Port != 4000 {
		t.Errorf("port = %d, want 4000", cfg.Port)
	}
}

func TestConfigRejectsBadValues(t *testing.T) {
	cfg, err := configParse([]byte(`{"port": 80, "api_key": 42}`))
	if err != nil {
		t.Fatalf("configParse: %v", err)
	}
	def := configDefaults()
	if cfg.Port != def.Port {
		t.Errorf("out-of-range port should fall back to %d, got %d", def.Port, cfg.Port)
	}
	if cfg.APIKey != def.APIKey {
		t.Errorf("non-string api_key should fall back to %q, got %q", def.APIKey, cfg.APIKey)
	}
}

// The constants that replace user-facing knobs still have to be usable values.
func TestTunedConstantsAreValid(t *testing.T) {
	if maxDurationSeconds <= 0 {
		t.Errorf("maxDurationSeconds = %d, want positive", maxDurationSeconds)
	}
	if _, ok := sttLookup(defaultModelID); !ok {
		t.Errorf("default model %q is not on the allowlist", defaultModelID)
	}
}

// The model is picked from a closed list, and the vocabulary rides with it.
func TestConfigModelAndVocabulary(t *testing.T) {
	cfg, err := configParse([]byte(`{"model": "parakeet-v3", "vocabulary": ["Numbero", "Erik Novikov"]}`))
	if err != nil {
		t.Fatalf("configParse: %v", err)
	}
	if cfg.Model != "parakeet-v3" || len(cfg.Vocabulary) != 2 {
		t.Errorf("got model=%q vocabulary=%q", cfg.Model, cfg.Vocabulary)
	}

	// Anything else -- a typo, or the OpenRouter slug -- falls back rather than
	// posting a model name the endpoint would reject.
	cfg, _ = configParse([]byte(`{"model": "nvidia/parakeet-tdt-0.6b-v3"}`))
	if cfg.Model != defaultModelID {
		t.Errorf("model = %q, want the default %q", cfg.Model, defaultModelID)
	}
}

func TestConfigMalformedYieldsDefaults(t *testing.T) {
	cfg, err := configParse([]byte(`{"port": `))
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !reflect.DeepEqual(cfg, configDefaults()) {
		t.Errorf("malformed config should yield defaults, got %+v", cfg)
	}
}

func TestConfigAPIKeyPrefersEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "from-env")
	if got := configAPIKey(Config{APIKey: "from-file"}); got != "from-env" {
		t.Errorf("got %q, want from-env", got)
	}

	t.Setenv("OPENROUTER_API_KEY", "")
	if got := configAPIKey(Config{APIKey: "from-file"}); got != "from-file" {
		t.Errorf("got %q, want from-file", got)
	}
}

func TestConfigFilePathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := configFilePath(); got != "/xdg/voice-type.jsonc" {
		t.Errorf("got %q", got)
	}
}

// The real config on this machine is a v4 file; it must load cleanly.
func TestConfigLoadRealFileIfPresent(t *testing.T) {
	path := configFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no config at %s", path)
	}
	if _, err := configParse(data); err != nil {
		t.Errorf("real config at %s failed to parse: %v", path, err)
	}
}

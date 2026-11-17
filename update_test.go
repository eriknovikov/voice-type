package voicetype

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	for _, test := range []struct {
		latest, current string
		want            bool
	}{
		{"v6.2.0", "6.1.0", true},
		{"v6.1.1", "6.1.0", true},
		{"v7.0.0", "6.9.9", true},
		{"v6.1.0", "6.1.0", false},
		{"v6.0.9", "6.1.0", false},
		{"v5.9.9", "6.0.0", false},
		{"6.2.0", "v6.1.0", true},
		{"v6.2.0-rc1", "6.1.0", true},
	} {
		if got := newerVersion(test.latest, test.current); got != test.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", test.latest, test.current, got, test.want)
		}
	}
	for _, input := range []string{"", "dev", "v6", "v6.1", "v6.1.x", "v-1.0.0"} {
		if _, ok := numericVersion(input); ok {
			t.Errorf("numericVersion(%q) accepted", input)
		}
	}
}

func TestUpdateRefusesDevelopmentBuilds(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := RunUpdate("dev", &out, &errOut); code != 1 {
		t.Fatalf("dev update exit %d", code)
	}
	if !strings.Contains(errOut.String(), "Development builds do not self-update") {
		t.Fatalf("dev update output: %q", errOut.String())
	}
}

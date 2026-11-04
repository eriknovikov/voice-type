package voicetype

import "testing"

func TestLeanCLI(t *testing.T) {
	for _, args := range [][]string{nil, {"start"}, {"version"}, {"--version"}, {"start", "--model", "test-model"}, {"--model", "test-model", "start"}} {
		if _, err := parseCLI(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"vocab"}, {"config"}, {"models"}, {"config-port"}, {"version", "--model", "x"}, {"start", "unexpected"}, {"--model", ""}} {
		if _, err := parseCLI(args); err == nil {
			t.Fatalf("accepted unsupported arguments %v", args)
		}
	}
}

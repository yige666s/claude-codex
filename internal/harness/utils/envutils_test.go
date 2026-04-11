package utils

import "testing"

func TestEnvHelpers(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/claude")
	if got := GetClaudeConfigHomeDir(); got != "/tmp/claude" {
		t.Fatalf("unexpected config dir %q", got)
	}
	if !IsEnvTruthy("true") || !IsEnvTruthy(true) {
		t.Fatal("expected truthy values")
	}
	if !IsEnvDefinedFalsy("off") || !IsEnvDefinedFalsy(false) {
		t.Fatal("expected falsy values")
	}
	t.Setenv("NODE_OPTIONS", "--trace-warnings --max-old-space-size=2048")
	if !HasNodeOption("--trace-warnings") {
		t.Fatal("expected node option to be detected")
	}
	envs, err := ParseEnvVars([]string{"A=1", "B=two"})
	if err != nil || envs["B"] != "two" {
		t.Fatalf("unexpected env parse result %#v err=%v", envs, err)
	}
}

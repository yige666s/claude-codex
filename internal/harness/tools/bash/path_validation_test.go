package bash

import (
	"path/filepath"
	"testing"

	"claude-codex/internal/harness/permissions"
)

func TestResolvePathExpandsHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "/tmp/bash-home")

	got := resolvePath("~/logs/output.txt", "/workspace")

	if got != "/tmp/bash-home/logs/output.txt" {
		t.Fatalf("resolvePath() = %q, want %q", got, "/tmp/bash-home/logs/output.txt")
	}
}

func TestExtractCommandPathsCdDefaultsToHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/cd-home")

	got := extractCommandPaths("cd", nil, "/workspace")

	if len(got) != 1 || got[0] != "/tmp/cd-home" {
		t.Fatalf("extractCommandPaths(cd) = %#v, want home directory", got)
	}
}

func TestFilterOutFlagsRespectsDoubleDash(t *testing.T) {
	got := filterOutFlags([]string{"-rf", "--", "-/../etc/passwd", "plain.txt"})

	want := []string{"-/../etc/passwd", "plain.txt"}
	if len(got) != len(want) {
		t.Fatalf("filterOutFlags() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filterOutFlags()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCheckPathConstraintsFlagsDangerousOutputRedirect(t *testing.T) {
	result := CheckPathConstraints("echo hi > /etc/hosts", "/workspace")

	if result.Behavior != permissions.BehaviorAsk {
		t.Fatalf("Behavior = %q, want %q", result.Behavior, permissions.BehaviorAsk)
	}
	if result.BlockedPath != filepath.Clean("/etc/hosts") {
		t.Fatalf("BlockedPath = %q, want %q", result.BlockedPath, filepath.Clean("/etc/hosts"))
	}
}

func TestSplitSubcommandsKeepsQuotedSeparatorsTogether(t *testing.T) {
	got := splitSubcommands(`printf "a; b" && echo ok | sed 's/o/O/'`)

	want := []string{`printf "a; b"`, "echo ok", `sed 's/o/O/'`}
	if len(got) != len(want) {
		t.Fatalf("splitSubcommands() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitSubcommands()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

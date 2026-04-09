package bash

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
)

func TestCheckCommandPermission_AllowsReadOnlyCommands(t *testing.T) {
	result := CheckCommandPermission("git status", safeTempRoot(t))
	if result.Behavior != permissions.BehaviorAllow {
		t.Fatalf("expected allow, got %s (%s)", result.Behavior, result.Message)
	}
}

func TestCheckCommandPermission_PassthroughForNormalWriteCommand(t *testing.T) {
	result := CheckCommandPermission("mkdir build", safeTempRoot(t))
	if result.Behavior != permissions.BehaviorPassthrough {
		t.Fatalf("expected read-only path command to pass through, got %+v", result)
	}
}

func TestCheckCommandPermission_AsksOnDangerousRedirection(t *testing.T) {
	result := CheckCommandPermission("echo hi > /etc/passwd", safeTempRoot(t))
	if result.Behavior != permissions.BehaviorAsk {
		t.Fatalf("expected ask, got %s (%s)", result.Behavior, result.Message)
	}
	if !strings.Contains(result.Message, "output redirection") {
		t.Fatalf("expected redirection message, got %q", result.Message)
	}
}

func TestCheckCommandPermission_AsksOnCompoundCdAndGit(t *testing.T) {
	result := CheckCommandPermission("cd repo && git status", safeTempRoot(t))
	if result.Behavior != permissions.BehaviorAsk {
		t.Fatalf("expected ask, got %s (%s)", result.Behavior, result.Message)
	}
	if !strings.Contains(result.Message, "cd and git") {
		t.Fatalf("expected cd/git message, got %q", result.Message)
	}
}

func TestCheckCommandPermission_AsksOnMultipleCdCommands(t *testing.T) {
	result := CheckCommandPermission("cd a && cd b", safeTempRoot(t))
	if result.Behavior != permissions.BehaviorAsk {
		t.Fatalf("expected ask, got %s (%s)", result.Behavior, result.Message)
	}
	if !strings.Contains(result.Message, "multiple directory changes") {
		t.Fatalf("expected multiple cd message, got %q", result.Message)
	}
}

func TestCheckCommandPermission_AsksOnTooManySubcommands(t *testing.T) {
	parts := make([]string, maxSubcommandsForSafetyCheck+1)
	for i := range parts {
		parts[i] = "echo ok"
	}
	result := CheckCommandPermission(strings.Join(parts, "; "), safeTempRoot(t))
	if result.Behavior != permissions.BehaviorAsk {
		t.Fatalf("expected ask, got %s (%s)", result.Behavior, result.Message)
	}
	if !strings.Contains(result.Message, "too many") {
		t.Fatalf("expected too-many-subcommands message, got %q", result.Message)
	}
}

func TestExecute_RejectsPermissionEscalationBeforeRunning(t *testing.T) {
	root := safeTempRoot(t)
	tool := NewTool(root)
	payload, err := json.Marshal(map[string]any{
		"command": "echo blocked > /etc/passwd",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, err = tool.Execute(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected approval error, got %v", err)
	}
}

func TestExecute_RunsPermittedCommand(t *testing.T) {
	root := safeTempRoot(t)
	tool := NewTool(root)
	target := filepath.Join(root, "hello.txt")
	payload, err := json.Marshal(map[string]any{
		"command": "echo hello > hello.txt",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	result, err := tool.Execute(context.Background(), payload)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "" {
		t.Fatalf("expected empty output, got %q", result.Output)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "hello" {
		t.Fatalf("unexpected file contents: %q", string(data))
	}
}

func safeTempRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "bash-tool-test-")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

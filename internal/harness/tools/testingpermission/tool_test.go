package testingpermission

import (
	"context"
	"encoding/json"
	"testing"

	"claude-codex/internal/harness/permissions"
)

func TestTestingPermissionTool(t *testing.T) {
	tool := NewTool()
	if tool.Name() != ToolName {
		t.Fatalf("expected name %q, got %q", ToolName, tool.Name())
	}
	if tool.Permission() != permissions.LevelWrite {
		t.Fatalf("expected write permission, got %q", tool.Permission())
	}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output != ToolName+" executed successfully" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestEnabledFromEnv(t *testing.T) {
	t.Setenv("CLAUDE_GO_ENABLE_TESTING_TOOLS", "true")
	if !EnabledFromEnv() {
		t.Fatalf("expected testing tool env gate to enable")
	}
}

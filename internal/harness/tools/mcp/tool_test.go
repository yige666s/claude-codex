package mcp

import (
	"encoding/json"
	"testing"

	coremcp "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/permissions"
)

func TestRemoteToolUsesCanonicalSanitizedName(t *testing.T) {
	tool := NewRemoteTool("my server", coremcp.ToolDefinition{
		Name:        "read:item",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, nil)
	if got, want := tool.Name(), "mcp__my_server__read_item"; got != want {
		t.Fatalf("tool name = %q, want %q", got, want)
	}
}

func TestRemoteToolPermissionUsesReadOnlyAnnotationConservatively(t *testing.T) {
	readOnly := NewRemoteTool("source", coremcp.ToolDefinition{
		Name:        "lookup",
		Annotations: &coremcp.ToolAnnotations{ReadOnlyHint: true},
	}, nil)
	if got := readOnly.Permission(); got != permissions.LevelRead {
		t.Fatalf("read-only permission = %q, want read", got)
	}

	unknown := NewRemoteTool("source", coremcp.ToolDefinition{Name: "mutate"}, nil)
	if got := unknown.Permission(); got != permissions.LevelExecute {
		t.Fatalf("unannotated permission = %q, want execute", got)
	}
}

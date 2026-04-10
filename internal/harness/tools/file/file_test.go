package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/memdir"
)

func TestWriteToolRejectsSecretsInTeamMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE", filepath.Join(root, ".claude", "memory"))
	tool := NewWriteTool(root)

	teamPath := filepath.Join(memdir.GetTeamMemPath(root), "shared.md")
	input, err := json.Marshal(map[string]any{
		"path":    teamPath,
		"content": "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected secret-bearing team memory write to fail")
	}
	if !strings.Contains(err.Error(), "GitHub PAT") {
		t.Fatalf("expected GitHub PAT error, got %q", err)
	}

	if _, statErr := os.Stat(teamPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected file to remain unwritten, stat err=%v", statErr)
	}
}

func TestEditToolRejectsSecretsInTeamMemory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE", filepath.Join(root, ".claude", "memory"))
	teamDir := memdir.GetTeamMemPath(root)
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}

	teamPath := filepath.Join(teamDir, "shared.md")
	if err := os.WriteFile(teamPath, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewEditTool(root)
	input, err := json.Marshal(map[string]any{
		"path":       teamPath,
		"old_string": "world",
		"new_string": "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected secret-bearing team memory edit to fail")
	}
	if !strings.Contains(err.Error(), "GitHub PAT") {
		t.Fatalf("expected GitHub PAT error, got %q", err)
	}

	content, err := os.ReadFile(teamPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "hello world" {
		t.Fatalf("expected original file to remain unchanged, got %q", string(content))
	}
}

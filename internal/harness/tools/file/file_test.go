package file

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"claude-codex/internal/harness/memdir"
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

func TestReadToolNotifiesReadListeners(t *testing.T) {
	ResetReadListenersForTest()
	t.Cleanup(ResetReadListenersForTest)

	root := t.TempDir()
	path := filepath.Join(root, "notes.md")
	if err := os.WriteFile(path, []byte("# MAGIC DOC: Notes\n_body_\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var callCount atomic.Int32
	var gotPath string
	var gotContent string
	unregister := RegisterReadListener(func(readPath string, content string) {
		callCount.Add(1)
		gotPath = readPath
		gotContent = content
	})
	defer unregister()

	tool := NewReadTool(root)
	input, err := json.Marshal(map[string]any{"path": "notes.md"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("read execute: %v", err)
	}
	if result.Output != "# MAGIC DOC: Notes\n_body_\n" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected listener to run once, got %d", callCount.Load())
	}
	if gotPath != path {
		t.Fatalf("unexpected read path: %q", gotPath)
	}
	if gotContent != result.Output {
		t.Fatalf("unexpected read content: %q", gotContent)
	}
}

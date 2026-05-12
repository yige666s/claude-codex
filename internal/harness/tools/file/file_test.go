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
		"file_path": teamPath,
		"content":   "ghp_abcdefghijklmnopqrstuvwxyz1234567890AB",
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
		"file_path":  teamPath,
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
	input, err := json.Marshal(map[string]any{"file_path": "notes.md"})
	if err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("read execute: %v", err)
	}
	if result.Output != "     1\u2192# MAGIC DOC: Notes\n     2\u2192_body_" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if callCount.Load() != 1 {
		t.Fatalf("expected listener to run once, got %d", callCount.Load())
	}
	if gotPath != path {
		t.Fatalf("unexpected read path: %q", gotPath)
	}
	if gotContent != "# MAGIC DOC: Notes\n_body_\n" {
		t.Fatalf("unexpected read content: %q", gotContent)
	}
}

func TestFileToolNamesAndSchemasMatchTypescriptSurface(t *testing.T) {
	root := t.TempDir()
	tools := []struct {
		name   string
		schema json.RawMessage
	}{
		{name: NewReadTool(root).Name(), schema: NewReadTool(root).InputSchema()},
		{name: NewWriteTool(root).Name(), schema: NewWriteTool(root).InputSchema()},
		{name: NewEditTool(root).Name(), schema: NewEditTool(root).InputSchema()},
	}

	wantNames := []string{ReadToolName, WriteToolName, EditToolName}
	for i, want := range wantNames {
		if tools[i].name != want {
			t.Fatalf("tool %d name = %q, want %q", i, tools[i].name, want)
		}
		if !strings.Contains(string(tools[i].schema), `"file_path"`) {
			t.Fatalf("%s schema should expose file_path: %s", want, tools[i].schema)
		}
		if strings.Contains(string(tools[i].schema), `"path"`) {
			t.Fatalf("%s schema should not expose legacy path: %s", want, tools[i].schema)
		}
	}
	if schema := string(NewReadTool(root).InputSchema()); !strings.Contains(schema, `"offset"`) || !strings.Contains(schema, `"limit"`) {
		t.Fatalf("Read schema should expose offset and limit: %s", schema)
	}
}

func TestReadToolSupportsFilePathOffsetLimitAndLineNumbers(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.md")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input, err := json.Marshal(map[string]any{
		"file_path": "notes.md",
		"offset":    2,
		"limit":     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := NewReadTool(root).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output != "     2\u2192beta" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestReadToolStillAcceptsLegacyPathInput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "legacy.txt"), []byte("legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := NewReadTool(root).Execute(context.Background(), json.RawMessage(`{"path":"legacy.txt"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Output != "     1\u2192legacy" {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestWriteToolReturnsStructuredCreateAndUpdateOutputs(t *testing.T) {
	root := t.TempDir()
	tool := NewWriteTool(root)

	createResult, err := tool.Execute(context.Background(), json.RawMessage(`{"file_path":"story.txt","content":"hello\n"}`))
	if err != nil {
		t.Fatalf("create Execute() error = %v", err)
	}
	var create writeOutput
	if err := json.Unmarshal([]byte(createResult.Output), &create); err != nil {
		t.Fatalf("unmarshal create output: %v\n%s", err, createResult.Output)
	}
	if create.Type != "create" || create.FilePath != filepath.Join(root, "story.txt") || create.Content != "hello\n" || create.OriginalFile != nil || len(create.StructuredPatch) != 0 {
		t.Fatalf("unexpected create output: %+v", create)
	}

	updateResult, err := tool.Execute(context.Background(), json.RawMessage(`{"file_path":"story.txt","content":"goodbye\n"}`))
	if err != nil {
		t.Fatalf("update Execute() error = %v", err)
	}
	var update writeOutput
	if err := json.Unmarshal([]byte(updateResult.Output), &update); err != nil {
		t.Fatalf("unmarshal update output: %v\n%s", err, updateResult.Output)
	}
	if update.Type != "update" || update.OriginalFile == nil || *update.OriginalFile != "hello\n" || len(update.StructuredPatch) == 0 {
		t.Fatalf("unexpected update output: %+v", update)
	}
}

func TestEditToolReturnsStructuredOutputAndRequiresUniqueMatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "story.txt")
	if err := os.WriteFile(path, []byte("one two two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := NewEditTool(root).Execute(context.Background(), json.RawMessage(`{"file_path":"story.txt","old_string":"two","new_string":"three"}`))
	if err == nil || !strings.Contains(err.Error(), "appears 2 times") {
		t.Fatalf("expected duplicate old_string error, got %v", err)
	}

	result, err := NewEditTool(root).Execute(context.Background(), json.RawMessage(`{"file_path":"story.txt","old_string":"two","new_string":"three","replace_all":true}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var output editOutput
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("unmarshal edit output: %v\n%s", err, result.Output)
	}
	if output.FilePath != path || output.OldString != "two" || output.NewString != "three" || output.OriginalFile != "one two two\n" || !output.ReplaceAll || output.UserModified || len(output.StructuredPatch) == 0 {
		t.Fatalf("unexpected edit output: %+v", output)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "one three three\n" {
		t.Fatalf("unexpected file content: %q", string(content))
	}
}

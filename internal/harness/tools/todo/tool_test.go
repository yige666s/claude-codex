package todo

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTodoWriteUsesClaudeCodeTodoSchemaAndReturnsOldNewLists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tool := New()

	first, err := tool.Execute(context.Background(), json.RawMessage(`{
		"todos": [
			{"content":"Plan work","status":"pending","activeForm":"Planning work"},
			{"content":"Run tests","status":"in_progress","activeForm":"Running tests"}
		]
	}`))
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if !strings.Contains(first.Output, `"oldTodos":[]`) || !strings.Contains(first.Output, `"content":"Plan work"`) {
		t.Fatalf("expected old/new todo output, got %s", first.Output)
	}

	second, err := tool.Execute(context.Background(), json.RawMessage(`{
		"todos": [
			{"content":"Plan work","status":"completed","activeForm":"Planning work"},
			{"content":"Run tests","status":"completed","activeForm":"Running tests"}
		]
	}`))
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if !strings.Contains(second.Output, `"oldTodos":[`) || !strings.Contains(second.Output, `"newTodos":[`) {
		t.Fatalf("expected second old/new todo output, got %s", second.Output)
	}

	data, err := os.ReadFile(filepath.Join(home, ".claude", "todos.json"))
	if err != nil {
		t.Fatalf("read persisted todos: %v", err)
	}
	if strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("expected all-completed todos to clear persisted list, got %s", data)
	}
}

func TestTodoWriteRejectsLegacyPrioritySchema(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tool := New()

	_, err := tool.Execute(context.Background(), json.RawMessage(`{
		"todos": [
			{"id":"1","content":"Old item","status":"pending","priority":"high"}
		]
	}`))
	if err == nil {
		t.Fatal("expected legacy id/priority todo schema to be rejected")
	}
}

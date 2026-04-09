// Package todo implements the TodoWrite tool.
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

// TodoItem represents a single todo entry.
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`   // "pending"|"in_progress"|"completed"
	Priority string `json:"priority"` // "high"|"medium"|"low"
}

type Tool struct{}

func New() toolkit.Tool { return &Tool{} }

func (t *Tool) Name() string { return "TodoWrite" }
func (t *Tool) Description() string {
	return `Create and manage a persistent todo list to track tasks within the current coding session.

Use this tool to:
- Create a structured task checklist at the start of complex work
- Update status of individual tasks as you make progress
- Maintain visibility into what has been done and what remains

The todo list persists to disk at $HOME/.claude/todos.json.`
}
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "todos": {
      "type": "array",
      "description": "The complete updated todo list (replaces existing list)",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "string"},
          "content": {"type": "string"},
          "status": {"type": "string", "enum": ["pending", "in_progress", "completed"]},
          "priority": {"type": "string", "enum": ["high", "medium", "low"]}
        },
        "required": ["id", "content", "status", "priority"]
      }
    }
  },
  "required": ["todos"]
}`)
}
func (t *Tool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *Tool) IsConcurrencySafe() bool       { return false }

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Todos []TodoItem `json:"todos"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("TodoWrite: %w", err)
	}

	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return toolkit.Result{}, fmt.Errorf("TodoWrite: cannot create dir: %w", err)
	}

	data, _ := json.MarshalIndent(in.Todos, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "todos.json"), data, 0o644); err != nil {
		return toolkit.Result{}, fmt.Errorf("TodoWrite: write failed: %w", err)
	}

	pending, inProgress, completed := 0, 0, 0
	for _, item := range in.Todos {
		switch item.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}

	return toolkit.Result{Output: fmt.Sprintf(
		"Updated %d todos: %d pending, %d in progress, %d completed",
		len(in.Todos), pending, inProgress, completed,
	)}, nil
}

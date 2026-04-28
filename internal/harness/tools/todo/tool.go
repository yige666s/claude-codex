// Package todo implements the TodoWrite tool.
package todo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type TodoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

type Tool struct{}

func New() toolkit.Tool { return &Tool{} }

func (t *Tool) Name() string { return "TodoWrite" }
func (t *Tool) Description() string {
	return `Create and manage a session todo list to track task progress.`
}
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string","minLength":1},"status":{"type":"string","enum":["pending","in_progress","completed"]},"activeForm":{"type":"string","minLength":1}},"required":["content","status","activeForm"],"additionalProperties":false}}},"required":["todos"],"additionalProperties":false}`)
}
func (t *Tool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *Tool) IsConcurrencySafe() bool       { return false }

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Todos []TodoItem `json:"todos"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		return toolkit.Result{}, fmt.Errorf("TodoWrite: %w", err)
	}
	for i, item := range in.Todos {
		if strings.TrimSpace(item.Content) == "" {
			return toolkit.Result{}, fmt.Errorf("TodoWrite: todos[%d].content cannot be empty", i)
		}
		if strings.TrimSpace(item.ActiveForm) == "" {
			return toolkit.Result{}, fmt.Errorf("TodoWrite: todos[%d].activeForm cannot be empty", i)
		}
		switch item.Status {
		case "pending", "in_progress", "completed":
		default:
			return toolkit.Result{}, fmt.Errorf("TodoWrite: todos[%d].status is invalid: %s", i, item.Status)
		}
	}

	oldTodos, err := readTodos()
	if err != nil {
		return toolkit.Result{}, err
	}
	persisted := in.Todos
	if len(in.Todos) > 0 {
		allDone := true
		for _, item := range in.Todos {
			if item.Status != "completed" {
				allDone = false
				break
			}
		}
		if allDone {
			persisted = []TodoItem{}
		}
	}
	if err := writeTodos(persisted); err != nil {
		return toolkit.Result{}, err
	}

	data, err := json.Marshal(map[string]any{
		"oldTodos": oldTodos,
		"newTodos": in.Todos,
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func todoPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "todos.json"), nil
}

func readTodos() ([]TodoItem, error) {
	path, err := todoPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []TodoItem{}, nil
		}
		return nil, fmt.Errorf("TodoWrite: read failed: %w", err)
	}
	var todos []TodoItem
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil, fmt.Errorf("TodoWrite: parse existing todos: %w", err)
	}
	return todos, nil
}

func writeTodos(todos []TodoItem) error {
	path, err := todoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("TodoWrite: cannot create dir: %w", err)
	}
	data, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("TodoWrite: write failed: %w", err)
	}
	return nil
}

package worktree

import (
	"context"
	"encoding/json"

	"github.com/ding/claude-code/claude-go/internal/harness/coordinator"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type enterTool struct{ manager *coordinator.WorktreeManager }

type input struct {
	Branch string `json:"branch"`
}

func NewEnterTool(manager *coordinator.WorktreeManager) toolkit.Tool {
	return &enterTool{manager: manager}
}

func (t *enterTool) Name() string        { return "enter_worktree" }
func (t *enterTool) Description() string { return "Create or enter a git worktree for a branch." }
func (t *enterTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"branch":{"type":"string"}},"required":["branch"]}`)
}
func (t *enterTool) Permission() permissions.Level { return permissions.LevelWrite }

func (t *enterTool) IsConcurrencySafe() bool {
	return false // worktree operations modify git state
}
func (t *enterTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	path, err := t.manager.Enter(in.Branch)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: path}, nil
}

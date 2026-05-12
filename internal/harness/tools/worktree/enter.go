package worktree

import (
	"context"
	"encoding/json"

	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type enterTool struct{ manager *coordinator.WorktreeManager }
type exitTool struct{ manager *coordinator.WorktreeManager }

type enterInput struct {
	Branch string `json:"branch"`
}

type exitInput struct {
	Action         string `json:"action"`
	DiscardChanges bool   `json:"discard_changes,omitempty"`
}

func NewEnterTool(manager *coordinator.WorktreeManager) toolkit.Tool {
	return &enterTool{manager: manager}
}

func NewExitTool(manager *coordinator.WorktreeManager) toolkit.Tool {
	return &exitTool{manager: manager}
}

func (t *enterTool) Name() string        { return "EnterWorktree" }
func (t *enterTool) Description() string { return "Create or enter a git worktree for a branch." }
func (t *enterTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"branch":{"type":"string"}},"required":["branch"]}`)
}
func (t *enterTool) Permission() permissions.Level { return permissions.LevelWrite }

func (t *enterTool) IsConcurrencySafe() bool {
	return false // worktree operations modify git state
}
func (t *enterTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in enterInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	path, err := t.manager.Enter(in.Branch)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: path}, nil
}

func (t *exitTool) Name() string { return "ExitWorktree" }
func (t *exitTool) Description() string {
	return "Exit the active EnterWorktree session, keeping or safely removing its worktree."
}
func (t *exitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["keep","remove"],"description":"keep leaves the worktree on disk; remove deletes the worktree created in this session"},"discard_changes":{"type":"boolean","description":"Required true when removing a worktree with uncommitted files or commits"}},"required":["action"]}`)
}
func (t *exitTool) Permission() permissions.Level { return permissions.LevelWrite }
func (t *exitTool) IsConcurrencySafe() bool       { return false }
func (t *exitTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in exitInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	result, err := t.manager.Exit(coordinator.ExitWorktreeOptions{
		Action:         in.Action,
		DiscardChanges: in.DiscardChanges,
	})
	if err != nil {
		return toolkit.Result{}, err
	}
	output, err := result.JSON()
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: output}, nil
}

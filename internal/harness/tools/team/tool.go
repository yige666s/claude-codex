package team

import (
	"context"
	"encoding/json"
	"fmt"

	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type createTool struct{ manager *coordinator.Manager }
type deleteTool struct{ manager *coordinator.Manager }

type createInput struct {
	Name   string   `json:"name"`
	Agents []string `json:"agents,omitempty"`
}

type deleteInput struct {
	Name string `json:"name"`
}

func NewCreateTool(manager *coordinator.Manager) toolkit.Tool     { return &createTool{manager: manager} }
func NewDeleteTool(manager *coordinator.Manager) toolkit.Tool     { return &deleteTool{manager: manager} }
func NewTeamCreateTool(manager *coordinator.Manager) toolkit.Tool { return NewCreateTool(manager) }
func NewTeamDeleteTool(manager *coordinator.Manager) toolkit.Tool { return NewDeleteTool(manager) }

func (t *createTool) Name() string        { return "team_create" }
func (t *createTool) Description() string { return "Create a persisted team definition." }
func (t *createTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"agents":{"type":"array","items":{"type":"string"}}},"required":["name"]}`)
}
func (t *createTool) Permission() permissions.Level { return permissions.LevelWrite }

func (t *createTool) IsConcurrencySafe() bool {
	return false // team creation modifies shared state
}
func (t *createTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in createInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	_ = in.Agents
	if t.manager == nil {
		return toolkit.Result{}, fmt.Errorf("team manager is required")
	}
	team, err := t.manager.Create(in.Name)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: fmt.Sprintf("Created team %q (id: %s).", team.Name, team.ID)}, nil
}

func (t *deleteTool) Name() string        { return "team_delete" }
func (t *deleteTool) Description() string { return "Delete a persisted team definition." }
func (t *deleteTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)
}
func (t *deleteTool) Permission() permissions.Level { return permissions.LevelWrite }

func (t *deleteTool) IsConcurrencySafe() bool {
	return false // team deletion modifies shared state
}
func (t *deleteTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in deleteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if t.manager == nil {
		return toolkit.Result{}, fmt.Errorf("team manager is required")
	}
	removed, err := t.manager.Delete(in.Name)
	if err != nil {
		return toolkit.Result{}, err
	}
	if !removed {
		return toolkit.Result{}, fmt.Errorf("team not found: %s", in.Name)
	}
	return toolkit.Result{Output: fmt.Sprintf("Deleted team %q.", in.Name)}, nil
}

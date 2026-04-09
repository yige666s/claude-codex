package team

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/harness/coordinator"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
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
	// TODO: Implement when coordinator.Manager.Create is available
	return toolkit.Result{}, fmt.Errorf("team creation not yet implemented")
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
	// TODO: Implement when coordinator.Manager.Delete is available
	return toolkit.Result{}, fmt.Errorf("team deletion not yet implemented")
}

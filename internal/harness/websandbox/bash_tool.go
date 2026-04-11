package websandbox

import (
	"context"
	"encoding/json"
	"fmt"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type BashTool struct {
	runtime *Runtime
}

func NewBashTool(scope Scope, opts RuntimeOptions) toolkit.Tool {
	return &BashTool{
		runtime: NewRuntime(scope, opts),
	}
}

func (t *BashTool) Name() string { return "bash" }
func (t *BashTool) Description() string {
	return "Execute an approved script inside the web sandbox container."
}
func (t *BashTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
}
func (t *BashTool) Permission() permissions.Level { return permissions.LevelExecute }
func (t *BashTool) IsConcurrencySafe() bool       { return false }

func (t *BashTool) Execute(ctx context.Context, input json.RawMessage) (toolkit.Result, error) {
	command, err := ExtractCommand(input)
	if err != nil {
		return toolkit.Result{}, err
	}
	output, err := t.runtime.ExecuteCommand(ctx, command)
	if err != nil {
		return toolkit.Result{}, fmt.Errorf("web sandbox denied shell execution: %w", err)
	}
	return toolkit.Result{Output: output}, nil
}

type bashCommandInput struct {
	Command string `json:"command"`
}

func ExtractCommand(raw json.RawMessage) (string, error) {
	var in bashCommandInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", err
	}
	return in.Command, nil
}

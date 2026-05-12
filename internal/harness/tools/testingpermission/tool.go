package testingpermission

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const ToolName = "TestingPermission"

type Tool struct{}

func NewTool() toolkit.Tool {
	return &Tool{}
}

func EnabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDE_GO_ENABLE_TESTING_TOOLS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (t *Tool) Name() string {
	return ToolName
}

func (t *Tool) Description() string {
	return "Test tool that always asks for permission before executing. Used for end-to-end testing."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *Tool) IsConcurrencySafe() bool {
	return true
}

func (t *Tool) Execute(context.Context, json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{Output: ToolName + " executed successfully"}, nil
}

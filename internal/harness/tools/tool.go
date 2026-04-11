package tools

import (
	"context"
	"encoding/json"

	"claude-codex/internal/harness/permissions"
)

type Result struct {
	Output string `json:"output"`
}

type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Permission() permissions.Level
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
	// IsConcurrencySafe returns true if this tool can be executed concurrently with other tools.
	// Tools that modify shared state (file writes, bash commands) should return false.
	IsConcurrencySafe() bool
}

type Descriptor struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	InputSchema json.RawMessage   `json:"input_schema"`
	Permission  permissions.Level `json:"permission"`
}

func Describe(tool Tool) Descriptor {
	return Descriptor{
		Name:        tool.Name(),
		Description: tool.Description(),
		InputSchema: tool.InputSchema(),
		Permission:  tool.Permission(),
	}
}

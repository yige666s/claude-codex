// Package synthetic implements the SyntheticOutput tool.
package synthetic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Tool struct{}

func New() toolkit.Tool { return &Tool{} }

func (t *Tool) Name() string        { return "SyntheticOutput" }
func (t *Tool) Description() string { return "Emit structured output from a non-interactive session." }
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "output": {"type": "string", "description": "The structured output to emit"}
  },
  "required": ["output"]
}`)
}
func (t *Tool) Permission() permissions.Level { return permissions.LevelRead }
func (t *Tool) IsConcurrencySafe() bool       { return true }

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("SyntheticOutput: %w", err)
	}
	return toolkit.Result{Output: in.Output}, nil
}

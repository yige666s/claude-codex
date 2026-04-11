// Package sleep implements the Sleep tool.
package sleep

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const maxSleepMs = 60_000

type Tool struct{}

func New() toolkit.Tool { return &Tool{} }

func (t *Tool) Name() string { return "Sleep" }
func (t *Tool) Description() string {
	return `Sleep for a specified number of milliseconds.

Use this when:
- You need to wait for an external process to complete
- You want to poll with a delay between attempts

Max duration: 60000ms (60 seconds). Specify 0 to yield briefly.`
}
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "duration_ms": {"type": "integer", "description": "Number of milliseconds to sleep (max 60000)"}
  },
  "required": ["duration_ms"]
}`)
}
func (t *Tool) Permission() permissions.Level { return permissions.LevelRead }
func (t *Tool) IsConcurrencySafe() bool       { return true }

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		DurationMs int `json:"duration_ms"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("Sleep: %w", err)
	}
	if in.DurationMs < 0 {
		in.DurationMs = 0
	}
	if in.DurationMs > maxSleepMs {
		in.DurationMs = maxSleepMs
	}

	timer := time.NewTimer(time.Duration(in.DurationMs) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return toolkit.Result{}, ctx.Err()
	case <-timer.C:
	}

	return toolkit.Result{Output: fmt.Sprintf("Slept for %dms.", in.DurationMs)}, nil
}

package planmode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const exitDescription = `Use this tool when you are in plan mode and have finished writing your plan and are ready for user approval.

## How This Tool Works
- This tool signals that you're done planning and ready for the user to review and approve
- The user will see your plan when they review it

## When to Use This Tool
IMPORTANT: Only use this tool when the task requires planning the implementation steps of a task that requires writing code. For research tasks where you're gathering information, searching files, reading files or in general trying to understand the codebase - do NOT use this tool.

## Before Using This Tool
Ensure your plan is complete and unambiguous:
- If you have unresolved questions about requirements or approach, use AskUserQuestion first
- Once your plan is finalized, use THIS tool to request approval

Important: Do NOT use AskUserQuestion to ask "Is this plan okay?" or "Should I proceed?" - that's exactly what THIS tool does. ExitPlanMode inherently requests user approval of your plan.

## Examples

1. Initial task: "Search for and understand the implementation of vim mode in the codebase" - Do not use the exit plan mode tool because you are not planning the implementation steps of a task.
2. Initial task: "Help me implement yank mode for vim" - Use the exit plan mode tool after you have finished planning the implementation steps of the task.
3. Initial task: "Add a new feature to handle user authentication" - If unsure about auth method (OAuth, JWT, etc.), use AskUserQuestion first, then use exit plan mode tool after clarifying the approach.`

// ExitPlanModeTool implements the ExitPlanMode tool.
type ExitPlanModeTool struct{}

type allowedPrompt struct {
	Tool   string `json:"tool"`
	Prompt string `json:"prompt"`
}

type exitInput struct {
	AllowedPrompts []allowedPrompt `json:"allowedPrompts,omitempty"`
}

func NewExitTool() *ExitPlanModeTool {
	return &ExitPlanModeTool{}
}

func (t *ExitPlanModeTool) Name() string {
	return "ExitPlanMode"
}

func (t *ExitPlanModeTool) Description() string {
	return exitDescription
}

func (t *ExitPlanModeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "allowedPrompts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "tool": {"type": "string"},
          "prompt": {"type": "string"}
        },
        "required": ["tool", "prompt"]
      }
    }
  }
}`)
}

func (t *ExitPlanModeTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *ExitPlanModeTool) IsConcurrencySafe() bool {
	return false
}

func (t *ExitPlanModeTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var payload exitInput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return toolkit.Result{}, err
	}

	var sb strings.Builder
	sb.WriteString("Plan submitted for user approval.")

	if len(payload.AllowedPrompts) > 0 {
		sb.WriteString("\n\nAllowed prompts:\n")
		for _, ap := range payload.AllowedPrompts {
			fmt.Fprintf(&sb, "  - [%s] %s\n", ap.Tool, ap.Prompt)
		}
	}

	return toolkit.Result{Output: sb.String()}, nil
}

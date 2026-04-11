// Package planmode implements EnterPlanMode and ExitPlanMode tools.
package planmode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

// ---- EnterPlanMode ----

type enterTool struct{}

func NewEnterPlanMode() toolkit.Tool { return &enterTool{} }

func (t *enterTool) Name() string { return "EnterPlanMode" }
func (t *enterTool) Description() string {
	return `Use this tool proactively when you're about to start a non-trivial implementation task. Getting user sign-off on your approach before writing code prevents wasted effort and ensures alignment. This tool transitions you into plan mode where you can explore the codebase and design an implementation approach for user approval.

## When to Use This Tool

**Prefer using EnterPlanMode** for implementation tasks unless they're simple. Use it when ANY of these conditions apply:
1. New Feature Implementation — adding meaningful new functionality
2. Multiple Valid Approaches — the task can be solved in several different ways
3. Code Modifications — changes that affect existing behavior or structure
4. Architectural Decisions — choosing between patterns or technologies
5. Multi-File Changes — the task will likely touch more than 2-3 files
6. Unclear Requirements — you need to explore before understanding the full scope

## When NOT to Use This Tool

Only skip EnterPlanMode for simple tasks:
- Single-line or few-line fixes (typos, obvious bugs, small tweaks)
- Tasks where the user has given very specific, detailed instructions

## What Happens in Plan Mode

In plan mode, you'll:
1. Thoroughly explore the codebase using Glob, Grep, and Read tools
2. Understand existing patterns and architecture
3. Design an implementation approach
4. Present your plan to the user for approval
5. Use AskUserQuestion if you need to clarify approaches
6. Exit plan mode with ExitPlanMode when ready to implement`
}
func (t *enterTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}
func (t *enterTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *enterTool) IsConcurrencySafe() bool       { return false }
func (t *enterTool) Execute(_ context.Context, _ json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{Output: "Entering plan mode. Explore the codebase, design your approach, then call ExitPlanMode when ready for user approval."}, nil
}

// ---- ExitPlanMode ----

type exitTool struct{}

func NewExitPlanMode() toolkit.Tool { return &exitTool{} }

func (t *exitTool) Name() string { return "ExitPlanMode" }
func (t *exitTool) Description() string {
	return `Use this tool ONLY when the user explicitly asks to work in plan mode AND you have finished writing your plan and are ready for user approval.

## How This Tool Works
- You should have already written your plan
- This tool signals that you're done planning and ready for the user to review and approve
- The user will see your plan when they review it

## When to Use This Tool
IMPORTANT: Only use this tool when the task requires planning the implementation steps of a task that requires writing code.

## Before Using This Tool
Ensure your plan is complete and unambiguous:
- If you have unresolved questions about requirements or approach, use AskUserQuestion first
- Once your plan is finalized, use THIS tool to request approval

Do NOT use AskUserQuestion to ask "Is this plan okay?" — that's exactly what THIS tool does.`
}
func (t *exitTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "allowedPrompts": {
      "type": "array",
      "description": "Prompt-based permissions needed to implement the plan.",
      "items": {
        "type": "object",
        "properties": {
          "tool": {"type": "string", "enum": ["Bash"]},
          "prompt": {"type": "string", "description": "Semantic description of the action, e.g. 'run tests'"}
        },
        "required": ["tool", "prompt"]
      }
    }
  }
}`)
}
func (t *exitTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *exitTool) IsConcurrencySafe() bool       { return false }

func (t *exitTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		AllowedPrompts []struct {
			Tool   string `json:"tool"`
			Prompt string `json:"prompt"`
		} `json:"allowedPrompts"`
	}
	_ = json.Unmarshal(raw, &in) // optional field — ignore error

	if len(in.AllowedPrompts) == 0 {
		return toolkit.Result{Output: "Plan submitted for user approval."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Plan submitted for user approval.\n\nPermissions requested:\n")
	for _, p := range in.AllowedPrompts {
		fmt.Fprintf(&sb, "  - [%s] %s\n", p.Tool, p.Prompt)
	}
	return toolkit.Result{Output: strings.TrimRight(sb.String(), "\n")}, nil
}

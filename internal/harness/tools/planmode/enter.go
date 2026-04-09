package planmode

import (
	"context"
	"encoding/json"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

const enterDescription = `Use this tool proactively when you're about to start a non-trivial implementation task. Getting user sign-off on your approach before writing code prevents wasted effort and ensures alignment. This tool transitions you into plan mode where you can explore the codebase and design an implementation approach for user approval.

## When to Use This Tool

Prefer using EnterPlanMode for implementation tasks unless they're simple. Use it when ANY of these conditions apply:

1. New Feature Implementation: Adding meaningful new functionality
2. Multiple Valid Approaches: The task can be solved in several different ways
3. Code Modifications: Changes that affect existing behavior or structure
4. Architectural Decisions: The task requires choosing between patterns or technologies
5. Multi-File Changes: The task will likely touch more than 2-3 files
6. Unclear Requirements: You need to explore before understanding the full scope
7. User Preferences Matter: The implementation could reasonably go multiple ways

## When NOT to Use This Tool

Only skip EnterPlanMode for simple tasks:
- Single-line or few-line fixes (typos, obvious bugs, small tweaks)
- Adding a single function with clear requirements
- Tasks where the user has given very specific, detailed instructions
- Pure research/exploration tasks

## What Happens in Plan Mode

In plan mode, you'll:
1. Thoroughly explore the codebase using Glob, Grep, and Read tools
2. Understand existing patterns and architecture
3. Design an implementation approach
4. Present your plan to the user for approval
5. Use AskUserQuestion if you need to clarify approaches
6. Exit plan mode with ExitPlanMode when ready to implement

## Important Notes

- This tool REQUIRES user approval - they must consent to entering plan mode
- If unsure whether to use it, err on the side of planning`

// EnterPlanModeTool implements the EnterPlanMode tool.
type EnterPlanModeTool struct{}

func NewEnterTool() *EnterPlanModeTool {
	return &EnterPlanModeTool{}
}

func (t *EnterPlanModeTool) Name() string {
	return "EnterPlanMode"
}

func (t *EnterPlanModeTool) Description() string {
	return enterDescription
}

func (t *EnterPlanModeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

func (t *EnterPlanModeTool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *EnterPlanModeTool) IsConcurrencySafe() bool {
	return false
}

func (t *EnterPlanModeTool) Execute(_ context.Context, _ json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{
		Output: "Entering plan mode. Explore the codebase, design your approach, then call ExitPlanMode when ready for user approval.",
	}, nil
}

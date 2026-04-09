package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Runner func(ctx context.Context, workingDir, prompt string) (string, error)

type Tool struct {
	defaultWorkDir string
	run            Runner
}

type input struct {
	Prompt     string `json:"prompt"`
	WorkingDir string `json:"working_dir,omitempty"`
}

func NewTool(defaultWorkDir string, run Runner) *Tool {
	return &Tool{
		defaultWorkDir: defaultWorkDir,
		run:            run,
	}
}

func (t *Tool) Name() string {
	return "agent"
}

func (t *Tool) Description() string {
	return "Run a bounded sub-agent prompt in an isolated engine invocation."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string"},"working_dir":{"type":"string"}},"required":["prompt"]}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelExecute
}

func (t *Tool) IsConcurrencySafe() bool {
	return false // agent spawns sub-engine which may modify state
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	if t.run == nil {
		return toolkit.Result{}, fmt.Errorf("agent runner is not configured")
	}

	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if in.Prompt == "" {
		return toolkit.Result{}, fmt.Errorf("prompt is required")
	}

	workDir := t.defaultWorkDir
	if in.WorkingDir != "" {
		workDir = in.WorkingDir
	}

	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, err := t.run(ctx, workDir, in.Prompt)
		done <- result{output: output, err: err}
	}()

	select {
	case <-ctx.Done():
		return toolkit.Result{}, ctx.Err()
	case outcome := <-done:
		if outcome.err != nil {
			return toolkit.Result{}, outcome.err
		}
		return toolkit.Result{Output: outcome.output}, nil
	}
}

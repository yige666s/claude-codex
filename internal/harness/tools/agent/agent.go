package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Request struct {
	Prompt          string `json:"prompt"`
	Description     string `json:"description,omitempty"`
	SubagentType    string `json:"subagent_type,omitempty"`
	Model           string `json:"model,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
	WorkingDir      string `json:"working_dir,omitempty"`
	Name            string `json:"name,omitempty"`
	TeamName        string `json:"team_name,omitempty"`
	Mode            string `json:"mode,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
	Isolation       string `json:"isolation,omitempty"`
	MaxTurns        int    `json:"max_turns,omitempty"`
}

type Runner func(ctx context.Context, request Request) (string, error)

type Tool struct {
	defaultWorkDir string
	run            Runner
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
	return json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string"},"description":{"type":"string"},"subagent_type":{"type":"string"},"model":{"type":"string"},"cwd":{"type":"string"},"working_dir":{"type":"string"},"name":{"type":"string"},"team_name":{"type":"string"},"mode":{"type":"string"},"run_in_background":{"type":"boolean"},"isolation":{"type":"string","enum":["none","worktree","remote"]},"max_turns":{"type":"integer","minimum":1}},"required":["prompt"]}`)
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

	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return toolkit.Result{}, err
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.Description = strings.TrimSpace(req.Description)
	req.SubagentType = strings.TrimSpace(req.SubagentType)
	req.Model = strings.TrimSpace(req.Model)
	req.Cwd = strings.TrimSpace(req.Cwd)
	req.WorkingDir = strings.TrimSpace(req.WorkingDir)
	req.Name = strings.TrimSpace(req.Name)
	req.TeamName = strings.TrimSpace(req.TeamName)
	req.Mode = strings.TrimSpace(req.Mode)
	req.Isolation = strings.TrimSpace(strings.ToLower(req.Isolation))

	if req.Prompt == "" {
		return toolkit.Result{}, fmt.Errorf("prompt is required")
	}
	if req.RunInBackground {
		return toolkit.Result{}, fmt.Errorf("agent background execution is not implemented")
	}
	switch req.Isolation {
	case "", "none":
		req.Isolation = ""
	case "worktree", "remote":
		return toolkit.Result{}, fmt.Errorf("agent isolation %q is not implemented", req.Isolation)
	default:
		return toolkit.Result{}, fmt.Errorf("agent isolation %q is invalid", req.Isolation)
	}
	if req.MaxTurns < 0 {
		return toolkit.Result{}, fmt.Errorf("max_turns must be positive")
	}
	if req.Cwd != "" && req.WorkingDir != "" && req.Cwd != req.WorkingDir {
		return toolkit.Result{}, fmt.Errorf("cwd and working_dir must match when both are provided")
	}
	if req.WorkingDir == "" {
		req.WorkingDir = req.Cwd
	}
	if req.WorkingDir == "" {
		req.WorkingDir = t.defaultWorkDir
	}

	type result struct {
		output string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, err := t.run(ctx, req)
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

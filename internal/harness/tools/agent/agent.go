package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Runner func(ctx context.Context, req Request) (string, error)

type Tool struct {
	defaultWorkDir string
	run            Runner
}

// Request mirrors the surface-level Agent tool parameters that the model sees.
// The injected runner may choose to honor only a subset of them.
type Request struct {
	Description     string `json:"description,omitempty"`
	Prompt          string `json:"prompt"`
	SubagentType    string `json:"subagent_type,omitempty"`
	Model           string `json:"model,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
	Isolation       string `json:"isolation,omitempty"`
	WorkingDir      string `json:"working_dir,omitempty"`
	CWD             string `json:"cwd,omitempty"`
	Name            string `json:"name,omitempty"`
	TeamName        string `json:"team_name,omitempty"`
	Mode            string `json:"mode,omitempty"`
}

func (r *Request) normalize(defaultWorkDir string) error {
	if strings.TrimSpace(r.Prompt) == "" {
		return fmt.Errorf("prompt is required")
	}
	if r.RunInBackground {
		return fmt.Errorf("agent background execution is not supported")
	}
	if r.Isolation != "" {
		return fmt.Errorf("agent isolation %q is not supported", r.Isolation)
	}

	if r.CWD != "" && r.WorkingDir != "" && r.CWD != r.WorkingDir {
		return fmt.Errorf("cwd and working_dir must match when both are provided")
	}

	switch {
	case r.CWD != "":
		r.WorkingDir = r.CWD
	case r.WorkingDir == "":
		r.WorkingDir = defaultWorkDir
	}

	return nil
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
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"description":{"type":"string"},
			"prompt":{"type":"string"},
			"subagent_type":{"type":"string"},
			"model":{"type":"string","enum":["sonnet","opus","haiku"]},
			"run_in_background":{"type":"boolean"},
			"isolation":{"type":"string","enum":["worktree","remote"]},
			"working_dir":{"type":"string"},
			"cwd":{"type":"string"},
			"name":{"type":"string"},
			"team_name":{"type":"string"},
			"mode":{"type":"string"}
		},
		"required":["prompt"]
	}`)
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
	if err := req.normalize(t.defaultWorkDir); err != nil {
		return toolkit.Result{}, err
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

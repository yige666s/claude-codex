package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	toolkit "claude-codex/internal/harness/tools"
)

const sandboxBashDefaultTimeout = 60 * time.Second

type SandboxBashTool struct {
	runtime skills.PromptShellRuntime
}

type sandboxBashInput struct {
	Command         string `json:"command"`
	Description     string `json:"description,omitempty"`
	TimeoutMs       int    `json:"timeout,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
	MaxOutputBytes  int    `json:"max_output_bytes,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
}

func NewSandboxBashTool(runtime skills.PromptShellRuntime) *SandboxBashTool {
	return &SandboxBashTool{runtime: runtime}
}

func (t *SandboxBashTool) Name() string {
	return "Bash"
}

func (t *SandboxBashTool) Description() string {
	return "Execute an allowed skill shell command inside the configured sandbox container."
}

func (t *SandboxBashTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"The allowed shell command to execute in the skill sandbox"},"description":{"type":"string","description":"Brief description of what this command does"},"timeout":{"type":"integer","description":"Timeout in milliseconds"},"timeout_seconds":{"type":"integer","description":"Legacy timeout in seconds"},"max_output_bytes":{"type":"integer","description":"Maximum output bytes to return"},"run_in_background":{"type":"boolean","description":"Reserved for future background task support; currently rejected"}},"required":["command"]}`)
}

func (t *SandboxBashTool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *SandboxBashTool) IsConcurrencySafe() bool {
	return false
}

func (t *SandboxBashTool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	if t == nil || t.runtime == nil {
		return toolkit.Result{}, fmt.Errorf("skill sandbox shell runtime is not configured")
	}
	var payload sandboxBashInput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return toolkit.Result{}, err
	}
	command := strings.TrimSpace(payload.Command)
	if command == "" {
		return toolkit.Result{}, fmt.Errorf("command is required")
	}
	if payload.RunInBackground {
		return toolkit.Result{}, fmt.Errorf("background execution is not supported for sandboxed skill commands")
	}
	if err := t.runtime.ValidateCommand(command); err != nil {
		return toolkit.Result{}, err
	}
	timeout := sandboxBashDefaultTimeout
	if payload.TimeoutSeconds > 0 {
		timeout = time.Duration(payload.TimeoutSeconds) * time.Second
	} else if payload.TimeoutMs > 0 {
		timeout = time.Duration(payload.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := t.runtime.ExecuteCommand(ctx, command)
	if err != nil {
		return toolkit.Result{}, err
	}
	if payload.MaxOutputBytes > 0 && len(output) > payload.MaxOutputBytes {
		output = output[:payload.MaxOutputBytes] + "\n...[truncated]"
	}
	return toolkit.Result{Output: output}, nil
}

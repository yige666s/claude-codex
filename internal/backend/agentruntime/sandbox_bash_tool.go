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
	return "Execute an allowed skill shell command in the configured skill shell runtime."
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
	started := time.Now()
	output, err := t.runtime.ExecuteCommand(ctx, command)
	duration := time.Since(started)
	stats := SandboxExecutionStats{Duration: duration, OutputLen: len(output)}
	if provider, ok := t.runtime.(sandboxStatsProvider); ok {
		stats = provider.LastSandboxStats()
	}
	t.emitMetric(ctx, payload, command, duration, len(output), stats, err)
	if err != nil {
		return toolkit.Result{}, err
	}
	if payload.MaxOutputBytes > 0 && len(output) > payload.MaxOutputBytes {
		output = output[:payload.MaxOutputBytes] + "\n...[truncated]"
	}
	return toolkit.Result{Output: output}, nil
}

func (t *SandboxBashTool) emitMetric(ctx context.Context, payload sandboxBashInput, command string, duration time.Duration, outputBytes int, stats SandboxExecutionStats, runErr error) {
	description := strings.TrimSpace(payload.Description)
	if description == "" {
		description = truncateSandboxCommand(command, 96)
	}
	data := map[string]any{
		"description":  description,
		"duration_ms":  duration.Milliseconds(),
		"startup_ms":   stats.Startup.Milliseconds(),
		"output_bytes": outputBytes,
		"timeout_ms":   int64(sandboxBashDefaultTimeout / time.Millisecond),
		"success":      runErr == nil,
	}
	if payload.TimeoutSeconds > 0 {
		data["timeout_ms"] = int64((time.Duration(payload.TimeoutSeconds) * time.Second) / time.Millisecond)
	} else if payload.TimeoutMs > 0 {
		data["timeout_ms"] = payload.TimeoutMs
	}
	if runtime, ok := t.runtime.(*DockerSkillShellRuntime); ok && runtime != nil {
		data["runner"] = "docker"
		data["image"] = runtime.config.Image
		data["network"] = runtime.config.Network
		data["from_pool"] = stats.FromPool
	} else if runtime, ok := t.runtime.(*LocalSkillShellRuntime); ok && runtime != nil {
		data["runner"] = "local"
	} else {
		data["runner"] = "sandbox"
	}
	if runErr != nil {
		data["error"] = runErr.Error()
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	content := fmt.Sprintf("Sandbox command completed in %d ms", duration.Milliseconds())
	if runErr != nil {
		content = fmt.Sprintf("Sandbox command failed after %d ms", duration.Milliseconds())
	}
	emitJobEventFromContext(ctx, Event{Type: "sandbox_metric", Role: "tool", Content: content, Data: raw})
}

func truncateSandboxCommand(command string, limit int) string {
	command = strings.Join(strings.Fields(command), " ")
	if limit <= 0 || len(command) <= limit {
		return command
	}
	return command[:limit-3] + "..."
}

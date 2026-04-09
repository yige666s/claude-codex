package bash

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

const defaultTimeout = 30 * time.Second

type Tool struct {
	rootDir string
}

type input struct {
	Command        string `json:"command"`
	Workdir        string `json:"workdir,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	MaxOutputBytes int    `json:"max_output_bytes,omitempty"`
}

func NewTool(rootDir string) *Tool {
	return &Tool{rootDir: rootDir}
}

func (t *Tool) Name() string {
	return "bash"
}

func (t *Tool) Description() string {
	return "Execute a shell command from the project root."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"workdir":{"type":"string"},"timeout_seconds":{"type":"integer"},"max_output_bytes":{"type":"integer"}},"required":["command"]}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelExecute
}

func (t *Tool) IsConcurrencySafe() bool {
	return false // bash commands may modify shared state
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var payload input
	if err := json.Unmarshal(raw, &payload); err != nil {
		return toolkit.Result{}, err
	}
	if strings.TrimSpace(payload.Command) == "" {
		return toolkit.Result{}, fmt.Errorf("command is required")
	}

	workdir := t.rootDir
	if payload.Workdir != "" {
		resolved, err := toolkit.ResolvePath(t.rootDir, payload.Workdir)
		if err != nil {
			return toolkit.Result{}, err
		}
		workdir = resolved
	}

	timeout := defaultTimeout
	if payload.TimeoutSeconds > 0 {
		timeout = time.Duration(payload.TimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin, args := shellCommand(payload.Command)
	command := exec.CommandContext(ctx, bin, args...)
	command.Dir = workdir

	output, err := command.CombinedOutput()
	limit := payload.MaxOutputBytes
	if limit <= 0 {
		limit = 16 * 1024
	}

	text := truncate(string(output), limit)
	if err != nil {
		return toolkit.Result{}, fmt.Errorf("command failed: %w\n%s", err, text)
	}

	return toolkit.Result{Output: text}, nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-lc", command}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n...[truncated]"
}

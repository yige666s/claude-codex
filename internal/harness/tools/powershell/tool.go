package powershelltool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const (
	ToolName       = "PowerShell"
	defaultTimeout = 30 * time.Second
	maxTimeout     = 10 * time.Minute
	maxOutputBytes = 30 * 1024
)

type Tool struct {
	rootDir string
}

type input struct {
	Command                   string `json:"command"`
	TimeoutMilliseconds       int    `json:"timeout,omitempty"`
	Description               string `json:"description,omitempty"`
	RunInBackground           bool   `json:"run_in_background,omitempty"`
	DangerouslyDisableSandbox bool   `json:"dangerouslyDisableSandbox,omitempty"`
}

type output struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	ExitCode    int    `json:"exitCode"`
	Interrupted bool   `json:"interrupted"`
}

var blockedSleepPattern = regexp.MustCompile(`(?i)^(?:start-sleep|sleep)(?:\s+-s(?:econds)?)?\s+(\d+)\s*$`)

func NewTool(rootDir string) toolkit.Tool {
	return &Tool{rootDir: rootDir}
}

func (t *Tool) Name() string {
	return ToolName
}

func (t *Tool) Description() string {
	return "Execute a PowerShell command from the project root."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "The PowerShell command to execute."},
    "timeout": {"type": "number", "description": "Optional timeout in milliseconds."},
    "description": {"type": "string", "description": "Clear, concise description of what this command does."},
    "run_in_background": {"type": "boolean", "description": "Reserved for future background task support."},
    "dangerouslyDisableSandbox": {"type": "boolean", "description": "Reserved for future sandbox integration."}
  },
  "required": ["command"]
}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelExecute
}

func (t *Tool) IsConcurrencySafe() bool {
	return false
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var payload input
	if err := json.Unmarshal(raw, &payload); err != nil {
		return toolkit.Result{}, fmt.Errorf("%s: invalid input: %w", ToolName, err)
	}
	payload.Command = strings.TrimSpace(payload.Command)
	if payload.Command == "" {
		return toolkit.Result{}, fmt.Errorf("%s: command is required", ToolName)
	}
	if payload.RunInBackground {
		return toolkit.Result{}, fmt.Errorf("%s: background execution is not supported yet", ToolName)
	}
	if blocked := detectBlockedSleepPattern(payload.Command); blocked != "" {
		return toolkit.Result{}, fmt.Errorf("%s: blocked %s. run blocking commands in the background once background execution is supported", ToolName, blocked)
	}

	timeout, err := timeoutDuration(payload.TimeoutMilliseconds)
	if err != nil {
		return toolkit.Result{}, err
	}
	shell, args, err := shellCommand(payload.Command)
	if err != nil {
		return toolkit.Result{}, err
	}

	workdir := t.rootDir
	if strings.TrimSpace(workdir) == "" {
		workdir = "."
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, shell, args...)
	cmd.Dir = workdir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	out := output{
		Stdout:      truncate(stdout.String(), maxOutputBytes),
		Stderr:      truncate(stderr.String(), maxOutputBytes),
		ExitCode:    exitCode(runErr),
		Interrupted: execCtx.Err() == context.DeadlineExceeded || execCtx.Err() == context.Canceled,
	}
	data, err := json.Marshal(out)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func timeoutDuration(milliseconds int) (time.Duration, error) {
	if milliseconds <= 0 {
		return defaultTimeout, nil
	}
	timeout := time.Duration(milliseconds) * time.Millisecond
	if timeout > maxTimeout {
		return 0, fmt.Errorf("%s: timeout exceeds %d milliseconds", ToolName, maxTimeout.Milliseconds())
	}
	return timeout, nil
}

func shellCommand(command string) (string, []string, error) {
	for _, name := range shellCandidates() {
		if path, err := exec.LookPath(name); err == nil {
			return path, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}, nil
		}
	}
	return "", nil, fmt.Errorf("%s: PowerShell executable not found; install pwsh or powershell", ToolName)
}

func shellCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{"pwsh.exe", "pwsh", "powershell.exe", "powershell"}
	}
	return []string{"pwsh", "powershell"}
}

func detectBlockedSleepPattern(command string) string {
	parts := strings.FieldsFunc(command, func(r rune) bool {
		return r == ';' || r == '|' || r == '&' || r == '\r' || r == '\n'
	})
	if len(parts) == 0 {
		return ""
	}
	first := strings.TrimSpace(parts[0])
	match := blockedSleepPattern.FindStringSubmatch(first)
	if len(match) != 2 {
		return ""
	}
	seconds := 0
	_, _ = fmt.Sscanf(match[1], "%d", &seconds)
	if seconds < 2 {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(command), first))
	rest = strings.TrimLeft(rest, " \t;|&")
	if rest != "" {
		return fmt.Sprintf("Start-Sleep %d followed by: %s", seconds, rest)
	}
	return fmt.Sprintf("standalone Start-Sleep %d", seconds)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n...[truncated]"
}

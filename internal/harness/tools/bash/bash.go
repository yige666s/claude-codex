package bash

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

const (
	defaultTimeout               = 30 * time.Second
	maxSubcommandsForSafetyCheck = 50
)

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

	permissionResult := CheckCommandPermission(payload.Command, workdir)
	if permissionResult.Behavior == permissions.BehaviorAsk || permissionResult.Behavior == permissions.BehaviorDeny {
		if permissionResult.Message != "" {
			return toolkit.Result{}, fmt.Errorf("bash command requires approval: %s", permissionResult.Message)
		}
		return toolkit.Result{}, fmt.Errorf("bash command requires approval")
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

func CheckCommandPermission(command, cwd string) permissions.PermissionResult {
	if strings.TrimSpace(command) == "" {
		return permissions.Allow()
	}

	subcommands := splitSubcommands(command)
	if len(subcommands) > maxSubcommandsForSafetyCheck {
		return permissions.Ask(fmt.Sprintf("command splits into %d subcommands, too many to safety-check individually", len(subcommands)))
	}

	cdCount := 0
	hasGit := false
	for _, subcmd := range subcommands {
		if isNormalizedCommand(subcmd, "cd") {
			cdCount++
		}
		if isNormalizedCommand(subcmd, "git") {
			hasGit = true
		}
	}

	if cdCount > 1 {
		return permissions.Ask("multiple directory changes in one command require manual review")
	}
	if cdCount > 0 && hasGit {
		return permissions.Ask("compound commands with cd and git require manual review")
	}

	var (
		sawAsk   bool
		firstAsk permissions.PermissionResult
	)
	for _, subcmd := range subcommands {
		result := checkSingleCommandPermission(subcmd, cwd)
		if result.Behavior == permissions.BehaviorDeny {
			return result
		}
		if result.Behavior == permissions.BehaviorAsk {
			if !sawAsk {
				firstAsk = result
			}
			sawAsk = true
		}
	}

	pathResult := CheckPathConstraints(command, cwd)
	if pathResult.Behavior == permissions.BehaviorDeny {
		return pathResult
	}
	if pathResult.Behavior == permissions.BehaviorAsk && !sawAsk {
		return pathResult
	}
	if sawAsk {
		return firstAsk
	}

	allReadOnly := true
	for _, subcmd := range subcommands {
		if !IsCommandReadOnly(subcmd) {
			allReadOnly = false
			break
		}
	}
	if allReadOnly {
		return permissions.Allow()
	}

	return permissions.Passthrough("command requires approval from the session permission checker")
}

func checkSingleCommandPermission(command, cwd string) permissions.PermissionResult {
	if result := BashCommandIsSafe(command); result.Behavior == permissions.BehaviorAsk || result.Behavior == permissions.BehaviorDeny {
		return result
	}
	if result := CheckPathConstraints(command, cwd); result.Behavior == permissions.BehaviorAsk || result.Behavior == permissions.BehaviorDeny {
		return result
	}
	if IsCommandReadOnly(command) {
		return permissions.Allow()
	}
	return permissions.Passthrough("subcommand requires approval from the session permission checker")
}

func isNormalizedCommand(command, want string) bool {
	trimmed := strings.TrimSpace(stripSafeWrappersForPath(command))
	if trimmed == "" {
		return false
	}
	tokens := splitCommandTokens(trimmed)
	if len(tokens) == 0 {
		return false
	}
	base := strings.TrimLeftFunc(tokens[0], unicode.IsSpace)
	return base == want
}

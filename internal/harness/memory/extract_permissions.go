package memory

import (
	"path/filepath"
	"strings"

	"claude-codex/internal/harness/tool"
)

// readOnlyBashCommands is the set of bash command prefixes considered safe
// for the extraction agent (read-only operations).
var readOnlyBashPrefixes = []string{
	"cat ", "head ", "tail ", "grep ", "rg ", "find ",
	"ls ", "ls\n", "pwd", "echo ", "wc ", "stat ", "file ",
	"git log", "git diff", "git show", "git status",
}

// isReadOnlyBash returns true if the bash command looks safe (read-only).
func isReadOnlyBash(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range readOnlyBashPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// CreateAutoMemCanUseTool builds a CanUseToolFn that mirrors the
// createAutoMemCanUseTool permission model from extractMemories.ts:
//
//   - Read, Grep, Glob, Task tools → always allow (unrestricted reads)
//   - Bash → allow only read-only commands
//   - Edit, Write → allow only if the target path is within memDir
//   - Everything else → deny
//
// This is used to constrain the background extraction agent so it can
// search the codebase freely but can only persist content in the memory
// directory.
func CreateAutoMemCanUseTool(memDir string) tool.CanUseToolFn {
	absMemDir := filepath.Clean(memDir)

	return func(
		t tool.Tool,
		input map[string]interface{},
		_ *tool.ToolUseContext,
		_ interface{},
		_ string,
		_ *string,
	) (*tool.PermissionResult, error) {
		allow := &tool.PermissionResult{Behavior: tool.PermissionAllow}
		deny := func(reason string) (*tool.PermissionResult, error) {
			return &tool.PermissionResult{
				Behavior: tool.PermissionDeny,
				Reason:   reason,
			}, nil
		}

		switch t.Name() {
		// Unrestricted reads.
		case "Read", "Grep", "Glob", "Task":
			return allow, nil

		// Bash: only allow read-only commands.
		case "Bash":
			cmd, _ := input["command"].(string)
			if isReadOnlyBash(cmd) {
				return allow, nil
			}
			return deny("extraction agent may only run read-only bash commands")

		// Writes: only allowed inside the auto-memory directory.
		case "Edit", "Write":
			path, _ := input["file_path"].(string)
			if path == "" {
				// Some tools use "path" instead.
				path, _ = input["path"].(string)
			}
			if path == "" {
				return deny("extraction agent requires a file_path for write operations")
			}
			abs := filepath.Clean(path)
			if !strings.HasPrefix(abs, absMemDir+string(filepath.Separator)) && abs != absMemDir {
				return deny("extraction agent may only write within the memory directory: " + absMemDir)
			}
			return allow, nil

		default:
			return deny("extraction agent does not have permission to use tool: " + t.Name())
		}
	}
}

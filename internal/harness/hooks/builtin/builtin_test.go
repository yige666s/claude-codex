package builtin

import (
	"context"
	"errors"
	"testing"

	"claude-codex/internal/harness/hooks"
)

func TestPreToolUseHook_Name(t *testing.T) {
	hook := NewPreToolUseHook()
	if hook.Name() != "builtin:pretooluse" {
		t.Errorf("Expected name 'builtin:pretooluse', got %q", hook.Name())
	}
}

func TestPreToolUseHook_Event(t *testing.T) {
	hook := NewPreToolUseHook()
	if hook.Event() != hooks.EventPreToolUse {
		t.Errorf("Expected event EventPreToolUse, got %v", hook.Event())
	}
}

func TestPreToolUseHook_IsAsync(t *testing.T) {
	hook := NewPreToolUseHook()
	if hook.IsAsync() {
		t.Error("PreToolUse hook should be synchronous")
	}
}

func TestPreToolUseHook_Execute_NoTool(t *testing.T) {
	hook := NewPreToolUseHook()
	input := &hooks.HookInput{
		Event: hooks.EventPreToolUse,
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}
}

func TestPreToolUseHook_Execute_ValidTool(t *testing.T) {
	hook := NewPreToolUseHook()
	input := &hooks.HookInput{
		Event: hooks.EventPreToolUse,
		Tool: &hooks.ToolInfo{
			Name:  "Read",
			Input: map[string]any{"file_path": "/test/file.txt"},
		},
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}
	if result.PermissionDecision == nil {
		t.Fatal("Expected permission decision")
	}
	if result.PermissionDecision.Behavior != "allow" {
		t.Errorf("Expected behavior 'allow', got %q", result.PermissionDecision.Behavior)
	}
}

func TestPreToolUseHook_Execute_DangerousBashCommand(t *testing.T) {
	hook := NewPreToolUseHook()

	tests := []struct {
		name    string
		command string
		wantAsk bool
	}{
		{"rm -rf", "rm -rf /tmp/test", true},
		{"git reset --hard", "git reset --hard HEAD", true},
		{"git push --force", "git push --force origin main", true},
		{"safe command", "ls -la", false},
		{"echo", "echo 'hello'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &hooks.HookInput{
				Event: hooks.EventPreToolUse,
				Tool: &hooks.ToolInfo{
					Name:  "Bash",
					Input: map[string]any{"command": tt.command},
				},
			}

			result, err := hook.Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if !result.Continue {
				t.Error("Expected Continue=true")
			}
			if result.PermissionDecision == nil {
				t.Fatal("Expected permission decision")
			}

			gotAsk := result.PermissionDecision.Behavior == "ask"
			if gotAsk != tt.wantAsk {
				t.Errorf("Expected ask=%v, got %v (behavior=%s)",
					tt.wantAsk, gotAsk, result.PermissionDecision.Behavior)
			}
		})
	}
}

func TestPreToolUseHook_Execute_InvalidTool(t *testing.T) {
	hook := NewPreToolUseHook()
	input := &hooks.HookInput{
		Event: hooks.EventPreToolUse,
		Tool: &hooks.ToolInfo{
			Name:  "", // Invalid: empty name
			Input: map[string]any{},
		},
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Continue {
		t.Error("Expected Continue=false for invalid tool")
	}
	if result.BlockingError == "" {
		t.Error("Expected blocking error")
	}
}

func TestPostToolUseHook_Name(t *testing.T) {
	hook := NewPostToolUseHook()
	if hook.Name() != "builtin:posttooluse" {
		t.Errorf("Expected name 'builtin:posttooluse', got %q", hook.Name())
	}
}

func TestPostToolUseHook_Event(t *testing.T) {
	hook := NewPostToolUseHook()
	if hook.Event() != hooks.EventPostToolUse {
		t.Errorf("Expected event EventPostToolUse, got %v", hook.Event())
	}
}

func TestPostToolUseHook_IsAsync(t *testing.T) {
	hook := NewPostToolUseHook()
	if !hook.IsAsync() {
		t.Error("PostToolUse hook should be asynchronous")
	}
}

func TestPostToolUseHook_Execute_Success(t *testing.T) {
	hook := NewPostToolUseHook()
	input := &hooks.HookInput{
		Event: hooks.EventPostToolUse,
		Tool: &hooks.ToolInfo{
			Name:   "Read",
			Output: "file contents",
		},
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}
	if result.AdditionalContext == "" {
		t.Error("Expected additional context")
	}
}

func TestPostToolUseHook_Execute_Error(t *testing.T) {
	hook := NewPostToolUseHook()
	toolErr := errors.New("file not found")
	input := &hooks.HookInput{
		Event: hooks.EventPostToolUse,
		Tool: &hooks.ToolInfo{
			Name:  "Read",
			Error: toolErr,
		},
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}
	if result.SystemMessage == "" {
		t.Error("Expected system message for error")
	}
}

func TestSessionStartHook_Name(t *testing.T) {
	hook := NewSessionStartHook()
	if hook.Name() != "builtin:sessionstart" {
		t.Errorf("Expected name 'builtin:sessionstart', got %q", hook.Name())
	}
}

func TestSessionStartHook_Event(t *testing.T) {
	hook := NewSessionStartHook()
	if hook.Event() != hooks.EventSessionStart {
		t.Errorf("Expected event EventSessionStart, got %v", hook.Event())
	}
}

func TestSessionStartHook_IsAsync(t *testing.T) {
	hook := NewSessionStartHook()
	if hook.IsAsync() {
		t.Error("SessionStart hook should be synchronous")
	}
}

func TestSessionStartHook_Execute(t *testing.T) {
	hook := NewSessionStartHook()
	input := &hooks.HookInput{
		Event:      hooks.EventSessionStart,
		SessionID:  "test-session",
		WorkingDir: "/test/dir",
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}
	if result.AdditionalContext == "" {
		t.Error("Expected additional context")
	}
}

func TestSessionStartHook_Execute_WithInitialMessage(t *testing.T) {
	hook := NewSessionStartHook()
	input := &hooks.HookInput{
		Event:      hooks.EventSessionStart,
		SessionID:  "test-session",
		WorkingDir: "/test/dir",
		Metadata: map[string]any{
			"initialMessage": "Hello, Claude!",
		},
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.InitialUserMessage != "Hello, Claude!" {
		t.Errorf("Expected initial message 'Hello, Claude!', got %q", result.InitialUserMessage)
	}
}

func TestPermissionRequestHook_Name(t *testing.T) {
	hook := NewPermissionRequestHook()
	if hook.Name() != "builtin:permissionrequest" {
		t.Errorf("Expected name 'builtin:permissionrequest', got %q", hook.Name())
	}
}

func TestPermissionRequestHook_Event(t *testing.T) {
	hook := NewPermissionRequestHook()
	if hook.Event() != hooks.EventPermissionRequest {
		t.Errorf("Expected event EventPermissionRequest, got %v", hook.Event())
	}
}

func TestPermissionRequestHook_IsAsync(t *testing.T) {
	hook := NewPermissionRequestHook()
	if hook.IsAsync() {
		t.Error("PermissionRequest hook should be synchronous")
	}
}

func TestPermissionRequestHook_Execute_AutoAllowed(t *testing.T) {
	hook := NewPermissionRequestHook()

	tests := []struct {
		toolName string
		expected string
	}{
		{"Read", "allow"},
		{"Glob", "allow"},
		{"Grep", "allow"},
		{"LSP", "allow"},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			input := &hooks.HookInput{
				Event: hooks.EventPermissionRequest,
				Permission: &hooks.PermissionInfo{
					ToolName: tt.toolName,
					Input:    map[string]any{},
				},
			}

			result, err := hook.Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result.PermissionDecision == nil {
				t.Fatal("Expected permission decision")
			}
			if result.PermissionDecision.Behavior != tt.expected {
				t.Errorf("Expected behavior %q, got %q", tt.expected, result.PermissionDecision.Behavior)
			}
		})
	}
}

func TestPermissionRequestHook_Execute_Dangerous(t *testing.T) {
	hook := NewPermissionRequestHook()

	tests := []struct {
		toolName string
		expected string
	}{
		{"Bash", "ask"},
		{"Write", "ask"},
		{"Edit", "ask"},
		{"mcp__filesystem__delete_file", "ask"},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			input := &hooks.HookInput{
				Event: hooks.EventPermissionRequest,
				Permission: &hooks.PermissionInfo{
					ToolName: tt.toolName,
					Input:    map[string]any{},
				},
			}

			result, err := hook.Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if result.PermissionDecision == nil {
				t.Fatal("Expected permission decision")
			}
			if result.PermissionDecision.Behavior != tt.expected {
				t.Errorf("Expected behavior %q, got %q", tt.expected, result.PermissionDecision.Behavior)
			}
		})
	}
}

func TestPermissionRequestHook_Execute_WithMessage(t *testing.T) {
	hook := NewPermissionRequestHook()
	input := &hooks.HookInput{
		Event: hooks.EventPermissionRequest,
		Permission: &hooks.PermissionInfo{
			ToolName: "Bash",
			Input: map[string]any{
				"command": "ls -la",
			},
		},
	}

	result, err := hook.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.PermissionDecision == nil {
		t.Fatal("Expected permission decision")
	}
	if result.PermissionDecision.Message == "" {
		t.Error("Expected warning message for dangerous operation")
	}
}


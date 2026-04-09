package hooks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNewExecutor(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	if executor == nil {
		t.Fatal("Expected non-nil executor")
	}
	if executor.timeout != DefaultTimeout {
		t.Errorf("Expected default timeout %v, got %v", DefaultTimeout, executor.timeout)
	}
}

func TestNewExecutorWithTimeout(t *testing.T) {
	registry := NewRegistry()
	customTimeout := 5 * time.Second
	executor := NewExecutorWithTimeout(registry, customTimeout)

	if executor.timeout != customTimeout {
		t.Errorf("Expected timeout %v, got %v", customTimeout, executor.timeout)
	}
}

func TestExecute_NoHooks(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true for no hooks")
	}
}

func TestExecute_SingleHook(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	hook := &MockHook{
		name:  "test-hook",
		event: EventPreToolUse,
		result: &HookResult{
			Continue:      true,
			SystemMessage: "Test message",
		},
	}
	registry.Register(hook)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}
	if result.SystemMessage != "Test message" {
		t.Errorf("Expected system message 'Test message', got %q", result.SystemMessage)
	}
}

func TestExecute_MultipleHooks(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	hook1 := &MockHook{
		name:  "hook1",
		event: EventPreToolUse,
		result: &HookResult{
			Continue:          true,
			AdditionalContext: "Context 1",
		},
	}
	hook2 := &MockHook{
		name:  "hook2",
		event: EventPreToolUse,
		result: &HookResult{
			Continue:          true,
			AdditionalContext: "Context 2",
		},
	}

	registry.Register(hook1)
	registry.Register(hook2)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.AdditionalContexts) != 2 {
		t.Errorf("Expected 2 additional contexts, got %d", len(result.AdditionalContexts))
	}
}

func TestExecute_StopExecution(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	hook1 := &MockHook{
		name:  "hook1",
		event: EventPreToolUse,
		result: &HookResult{
			Continue:   false,
			StopReason: "Test stop",
		},
	}
	hook2 := &MockHook{
		name:  "hook2",
		event: EventPreToolUse,
		result: &HookResult{
			Continue: true,
		},
	}

	registry.Register(hook1)
	registry.Register(hook2)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Continue {
		t.Error("Expected Continue=false")
	}
	if result.StopReason != "Test stop" {
		t.Errorf("Expected stop reason 'Test stop', got %q", result.StopReason)
	}
}

func TestExecute_HookError(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	hook := &MockHook{
		name:  "error-hook",
		event: EventPreToolUse,
		err:   errors.New("hook error"),
	}
	registry.Register(hook)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.BlockingErrors) != 1 {
		t.Errorf("Expected 1 blocking error, got %d", len(result.BlockingErrors))
	}
}

func TestExecute_Timeout(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutorWithTimeout(registry, 100*time.Millisecond)

	hook := &MockHook{
		name:  "slow-hook",
		event: EventPreToolUse,
		result: &HookResult{Continue: true},
	}

	// Override Execute to simulate slow hook
	slowHook := &slowMockHook{
		MockHook: hook,
		delay:    200 * time.Millisecond,
	}
	registry.Register(slowHook)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.BlockingErrors) == 0 {
		t.Error("Expected blocking error for timeout")
	}
}

func TestExecute_AsyncHooks(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	asyncHook := &MockHook{
		name:  "async-hook",
		event: EventPostToolUse,
		async: true,
		result: &HookResult{Continue: true},
	}
	registry.Register(asyncHook)

	input := &HookInput{Event: EventPostToolUse}
	result, err := executor.Execute(context.Background(), EventPostToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Continue {
		t.Error("Expected Continue=true")
	}

	// Give async hook time to execute
	time.Sleep(50 * time.Millisecond)
}

func TestAggregateResults_PermissionDecision(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	tests := []struct {
		name     string
		results  []*HookResult
		expected string
	}{
		{
			name: "deny wins",
			results: []*HookResult{
				{Continue: true, PermissionDecision: &PermissionDecision{Behavior: "allow"}},
				{Continue: true, PermissionDecision: &PermissionDecision{Behavior: "deny"}},
			},
			expected: "deny",
		},
		{
			name: "ask over allow",
			results: []*HookResult{
				{Continue: true, PermissionDecision: &PermissionDecision{Behavior: "allow"}},
				{Continue: true, PermissionDecision: &PermissionDecision{Behavior: "ask"}},
			},
			expected: "ask",
		},
		{
			name: "first decision",
			results: []*HookResult{
				{Continue: true, PermissionDecision: &PermissionDecision{Behavior: "allow"}},
			},
			expected: "allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.aggregateResults(tt.results)
			if result.PermissionBehavior != tt.expected {
				t.Errorf("Expected permission behavior %q, got %q", tt.expected, result.PermissionBehavior)
			}
		})
	}
}

func TestAggregateResults_UpdatedInput(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	results := []*HookResult{
		{
			Continue: true,
			UpdatedInput: map[string]any{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			Continue: true,
			UpdatedInput: map[string]any{
				"key2": "overridden",
				"key3": "value3",
			},
		},
	}

	result := executor.aggregateResults(results)

	if result.UpdatedInput["key1"] != "value1" {
		t.Error("Expected key1=value1")
	}
	if result.UpdatedInput["key2"] != "overridden" {
		t.Error("Expected key2=overridden (later hook should override)")
	}
	if result.UpdatedInput["key3"] != "value3" {
		t.Error("Expected key3=value3")
	}
}

func TestExecuteHook_Panic(t *testing.T) {
	registry := NewRegistry()
	executor := NewExecutor(registry)

	panicHook := &panicMockHook{
		MockHook: &MockHook{
			name:  "panic-hook",
			event: EventPreToolUse,
		},
	}
	registry.Register(panicHook)

	input := &HookInput{Event: EventPreToolUse}
	result, err := executor.Execute(context.Background(), EventPreToolUse, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result.BlockingErrors) == 0 {
		t.Error("Expected blocking error for panic")
	}
}

// Helper types for testing

type slowMockHook struct {
	*MockHook
	delay time.Duration
}

func (h *slowMockHook) Execute(ctx context.Context, input *HookInput) (*HookResult, error) {
	select {
	case <-time.After(h.delay):
		return h.result, h.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type panicMockHook struct {
	*MockHook
}

func (h *panicMockHook) Execute(ctx context.Context, input *HookInput) (*HookResult, error) {
	panic(fmt.Sprintf("panic in hook %s", h.name))
}

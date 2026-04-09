package hooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewAsyncHookManager(t *testing.T) {
	manager := NewAsyncHookManager()
	if manager == nil {
		t.Fatal("Expected non-nil manager")
	}
	if manager.Count() != 0 {
		t.Errorf("Expected 0 pending hooks, got %d", manager.Count())
	}
}

func TestAsyncHookManager_Start(t *testing.T) {
	manager := NewAsyncHookManager()
	hook := &MockHook{
		name:  "test-hook",
		event: EventPostToolUse,
		async: true,
		result: &HookResult{Continue: true},
	}

	input := &HookInput{Event: EventPostToolUse}
	id, err := manager.Start(context.Background(), hook, input)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if id == "" {
		t.Error("Expected non-empty ID")
	}
	if manager.Count() != 1 {
		t.Errorf("Expected 1 pending hook, got %d", manager.Count())
	}
}

func TestAsyncHookManager_Wait(t *testing.T) {
	manager := NewAsyncHookManager()
	hook := &MockHook{
		name:  "test-hook",
		event: EventPostToolUse,
		async: true,
		result: &HookResult{
			Continue:          true,
			AdditionalContext: "Test context",
		},
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), hook, input)

	result, err := manager.Wait(id, 1*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.AdditionalContext != "Test context" {
		t.Errorf("Expected context 'Test context', got %q", result.AdditionalContext)
	}
	if manager.Count() != 0 {
		t.Errorf("Expected 0 pending hooks after wait, got %d", manager.Count())
	}
}

func TestAsyncHookManager_Wait_Timeout(t *testing.T) {
	manager := NewAsyncHookManager()
	slowHook := &slowMockHook{
		MockHook: &MockHook{
			name:   "slow-hook",
			event:  EventPostToolUse,
			async:  true,
			result: &HookResult{Continue: true},
		},
		delay: 500 * time.Millisecond,
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), slowHook, input)

	_, err := manager.Wait(id, 100*time.Millisecond)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestAsyncHookManager_Wait_NotFound(t *testing.T) {
	manager := NewAsyncHookManager()
	_, err := manager.Wait("nonexistent", 1*time.Second)
	if err == nil {
		t.Error("Expected error for nonexistent hook")
	}
}

func TestAsyncHookManager_GetStatus(t *testing.T) {
	manager := NewAsyncHookManager()
	hook := &MockHook{
		name:  "test-hook",
		event: EventPostToolUse,
		async: true,
		result: &HookResult{Continue: true},
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), hook, input)

	status, err := manager.GetStatus(id)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if status.ID != id {
		t.Errorf("Expected ID %s, got %s", id, status.ID)
	}
	if status.HookName != "test-hook" {
		t.Errorf("Expected hook name 'test-hook', got %s", status.HookName)
	}
}

func TestAsyncHookManager_GetStatus_NotFound(t *testing.T) {
	manager := NewAsyncHookManager()
	_, err := manager.GetStatus("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent hook")
	}
}

func TestAsyncHookManager_ListPending(t *testing.T) {
	manager := NewAsyncHookManager()

	hook1 := &MockHook{name: "hook1", event: EventPostToolUse, async: true, result: &HookResult{Continue: true}}
	hook2 := &MockHook{name: "hook2", event: EventPostToolUse, async: true, result: &HookResult{Continue: true}}

	input := &HookInput{Event: EventPostToolUse}
	manager.Start(context.Background(), hook1, input)
	manager.Start(context.Background(), hook2, input)

	statuses := manager.ListPending()
	if len(statuses) != 2 {
		t.Errorf("Expected 2 pending hooks, got %d", len(statuses))
	}
}

func TestAsyncHookManager_Cancel(t *testing.T) {
	manager := NewAsyncHookManager()
	hook := &slowMockHook{
		MockHook: &MockHook{
			name:   "slow-hook",
			event:  EventPostToolUse,
			async:  true,
			result: &HookResult{Continue: true},
		},
		delay: 1 * time.Second,
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), hook, input)

	err := manager.Cancel(id)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if manager.Count() != 0 {
		t.Errorf("Expected 0 pending hooks after cancel, got %d", manager.Count())
	}
}

func TestAsyncHookManager_Cancel_NotFound(t *testing.T) {
	manager := NewAsyncHookManager()
	err := manager.Cancel("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent hook")
	}
}

func TestAsyncHookManager_Cleanup(t *testing.T) {
	manager := NewAsyncHookManager()
	hook := &MockHook{
		name:   "test-hook",
		event:  EventPostToolUse,
		async:  true,
		result: &HookResult{Continue: true},
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), hook, input)

	// Wait for completion
	manager.Wait(id, 1*time.Second)

	// Manually add back to pending to simulate old completed hook
	manager.mu.Lock()
	manager.pending[id] = &AsyncHook{
		ID:        id,
		Hook:      hook,
		Input:     input,
		StartedAt: time.Now().Add(-2 * time.Hour),
		Result:    &HookResult{Continue: true},
	}
	manager.mu.Unlock()

	removed := manager.Cleanup(1 * time.Hour)
	if removed != 1 {
		t.Errorf("Expected 1 removed hook, got %d", removed)
	}
	if manager.Count() != 0 {
		t.Errorf("Expected 0 pending hooks after cleanup, got %d", manager.Count())
	}
}

func TestAsyncHookManager_Execute_Error(t *testing.T) {
	manager := NewAsyncHookManager()
	hook := &MockHook{
		name:   "error-hook",
		event:  EventPostToolUse,
		async:  true,
		err:    errors.New("hook error"),
		result: nil,
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), hook, input)

	_, err := manager.Wait(id, 1*time.Second)
	if err == nil {
		t.Error("Expected error from hook execution")
	}
}

func TestAsyncHookManager_Execute_Panic(t *testing.T) {
	manager := NewAsyncHookManager()
	panicHook := &panicMockHook{
		MockHook: &MockHook{
			name:  "panic-hook",
			event: EventPostToolUse,
			async: true,
		},
	}

	input := &HookInput{Event: EventPostToolUse}
	id, _ := manager.Start(context.Background(), panicHook, input)

	result, _ := manager.Wait(id, 1*time.Second)
	if result == nil {
		t.Fatal("Expected result even after panic")
	}
	if result.BlockingError == "" {
		t.Error("Expected blocking error for panic")
	}
}

func TestAsyncHookManager_Concurrency(t *testing.T) {
	manager := NewAsyncHookManager()

	// Start multiple hooks concurrently
	done := make(chan bool)
	for i := range 10 {
		go func(n int) {
			hook := &MockHook{
				name:   string(rune('a' + n)),
				event:  EventPostToolUse,
				async:  true,
				result: &HookResult{Continue: true},
			}
			input := &HookInput{Event: EventPostToolUse}
			manager.Start(context.Background(), hook, input)
			done <- true
		}(i)
	}

	// Wait for all starts
	for range 10 {
		<-done
	}

	if manager.Count() != 10 {
		t.Errorf("Expected 10 pending hooks, got %d", manager.Count())
	}
}

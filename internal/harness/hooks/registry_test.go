package hooks

import (
	"context"
	"testing"
	"time"
)

// MockHook is a test hook implementation.
type MockHook struct {
	name    string
	event   HookEvent
	async   bool
	timeout time.Duration
	result  *HookResult
	err     error
}

func (m *MockHook) Name() string                                                    { return m.name }
func (m *MockHook) Event() HookEvent                                                { return m.event }
func (m *MockHook) Execute(ctx context.Context, input *HookInput) (*HookResult, error) { return m.result, m.err }
func (m *MockHook) IsAsync() bool                                                   { return m.async }
func (m *MockHook) Timeout() time.Duration                                          { return m.timeout }

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("Expected non-nil registry")
	}
	if registry.Count() != 0 {
		t.Errorf("Expected empty registry, got %d hooks", registry.Count())
	}
}

func TestRegister(t *testing.T) {
	registry := NewRegistry()
	hook := &MockHook{
		name:  "test-hook",
		event: EventPreToolUse,
	}

	err := registry.Register(hook)
	if err != nil {
		t.Fatalf("Failed to register hook: %v", err)
	}

	if registry.Count() != 1 {
		t.Errorf("Expected 1 hook, got %d", registry.Count())
	}
}

func TestRegister_Nil(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(nil)
	if err == nil {
		t.Error("Expected error when registering nil hook")
	}
}

func TestRegister_Multiple(t *testing.T) {
	registry := NewRegistry()

	hook1 := &MockHook{name: "hook1", event: EventPreToolUse}
	hook2 := &MockHook{name: "hook2", event: EventPreToolUse}
	hook3 := &MockHook{name: "hook3", event: EventPostToolUse}

	registry.Register(hook1)
	registry.Register(hook2)
	registry.Register(hook3)

	if registry.Count() != 3 {
		t.Errorf("Expected 3 hooks, got %d", registry.Count())
	}

	if registry.CountForEvent(EventPreToolUse) != 2 {
		t.Errorf("Expected 2 PreToolUse hooks, got %d", registry.CountForEvent(EventPreToolUse))
	}

	if registry.CountForEvent(EventPostToolUse) != 1 {
		t.Errorf("Expected 1 PostToolUse hook, got %d", registry.CountForEvent(EventPostToolUse))
	}
}

func TestGetHooks(t *testing.T) {
	registry := NewRegistry()

	hook1 := &MockHook{name: "hook1", event: EventPreToolUse}
	hook2 := &MockHook{name: "hook2", event: EventPreToolUse}

	registry.Register(hook1)
	registry.Register(hook2)

	hooks := registry.GetHooks(EventPreToolUse)
	if len(hooks) != 2 {
		t.Errorf("Expected 2 hooks, got %d", len(hooks))
	}
}

func TestGetHooks_NoHooks(t *testing.T) {
	registry := NewRegistry()
	hooks := registry.GetHooks(EventPreToolUse)
	if hooks != nil {
		t.Errorf("Expected nil for no hooks, got %v", hooks)
	}
}

func TestGetHooks_Copy(t *testing.T) {
	registry := NewRegistry()
	hook := &MockHook{name: "hook1", event: EventPreToolUse}
	registry.Register(hook)

	hooks1 := registry.GetHooks(EventPreToolUse)
	hooks2 := registry.GetHooks(EventPreToolUse)

	// Modify first slice
	hooks1[0] = &MockHook{name: "modified", event: EventPreToolUse}

	// Second slice should be unchanged
	if hooks2[0].Name() != "hook1" {
		t.Error("GetHooks should return a copy, not the original slice")
	}
}

func TestUnregister(t *testing.T) {
	registry := NewRegistry()

	hook1 := &MockHook{name: "hook1", event: EventPreToolUse}
	hook2 := &MockHook{name: "hook2", event: EventPreToolUse}

	registry.Register(hook1)
	registry.Register(hook2)

	err := registry.Unregister("hook1", EventPreToolUse)
	if err != nil {
		t.Fatalf("Failed to unregister hook: %v", err)
	}

	if registry.CountForEvent(EventPreToolUse) != 1 {
		t.Errorf("Expected 1 hook after unregister, got %d", registry.CountForEvent(EventPreToolUse))
	}

	hooks := registry.GetHooks(EventPreToolUse)
	if hooks[0].Name() != "hook2" {
		t.Errorf("Expected hook2 to remain, got %s", hooks[0].Name())
	}
}

func TestUnregister_NotFound(t *testing.T) {
	registry := NewRegistry()
	err := registry.Unregister("nonexistent", EventPreToolUse)
	if err == nil {
		t.Error("Expected error when unregistering nonexistent hook")
	}
}

func TestClear(t *testing.T) {
	registry := NewRegistry()

	hook1 := &MockHook{name: "hook1", event: EventPreToolUse}
	hook2 := &MockHook{name: "hook2", event: EventPostToolUse}

	registry.Register(hook1)
	registry.Register(hook2)

	registry.Clear()

	if registry.Count() != 0 {
		t.Errorf("Expected 0 hooks after clear, got %d", registry.Count())
	}
}

func TestEvents(t *testing.T) {
	registry := NewRegistry()

	hook1 := &MockHook{name: "hook1", event: EventPreToolUse}
	hook2 := &MockHook{name: "hook2", event: EventPostToolUse}
	hook3 := &MockHook{name: "hook3", event: EventPreToolUse}

	registry.Register(hook1)
	registry.Register(hook2)
	registry.Register(hook3)

	events := registry.Events()
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}

	// Check that both events are present
	eventMap := make(map[HookEvent]bool)
	for _, event := range events {
		eventMap[event] = true
	}

	if !eventMap[EventPreToolUse] || !eventMap[EventPostToolUse] {
		t.Error("Expected both PreToolUse and PostToolUse events")
	}
}

func TestConcurrency(t *testing.T) {
	registry := NewRegistry()

	// Register hooks concurrently
	done := make(chan bool)
	for i := range 10 {
		go func(n int) {
			hook := &MockHook{
				name:  string(rune('a' + n)),
				event: EventPreToolUse,
			}
			registry.Register(hook)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}

	if registry.Count() != 10 {
		t.Errorf("Expected 10 hooks, got %d", registry.Count())
	}
}

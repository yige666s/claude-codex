package tools

import (
	"testing"
	"context"
	"time"
	"errors"
)

// Mock tool executor for testing
type mockToolExecutor struct {
	concurrentSafe bool
	executeFunc    func(ctx *ToolExecutionContext, call *ToolCall) (*ToolResult, error)
	validateFunc   func(input map[string]interface{}) error
}

func (m *mockToolExecutor) Execute(ctx *ToolExecutionContext, call *ToolCall) (*ToolResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, call)
	}
	return &ToolResult{
		ToolUseID: call.ID,
		Content:   "success",
		IsError:   false,
	}, nil
}

func (m *mockToolExecutor) IsConcurrentSafe() bool {
	return m.concurrentSafe
}

func (m *mockToolExecutor) ValidateInput(input map[string]interface{}) error {
	if m.validateFunc != nil {
		return m.validateFunc(input)
	}
	return nil
}

// Mock hook for testing
type mockHook struct {
	preFunc     func(ctx *ToolHookContext) (*ToolHookResult, error)
	postFunc    func(ctx *ToolHookContext) (*ToolHookResult, error)
	failureFunc func(ctx *ToolHookContext) (*ToolHookResult, error)
}

func (m *mockHook) PreToolUse(ctx *ToolHookContext) (*ToolHookResult, error) {
	if m.preFunc != nil {
		return m.preFunc(ctx)
	}
	return nil, nil
}

func (m *mockHook) PostToolUse(ctx *ToolHookContext) (*ToolHookResult, error) {
	if m.postFunc != nil {
		return m.postFunc(ctx)
	}
	return nil, nil
}

func (m *mockHook) PostToolUseFailure(ctx *ToolHookContext) (*ToolHookResult, error) {
	if m.failureFunc != nil {
		return m.failureFunc(ctx)
	}
	return nil, nil
}

func TestToolRegistry(t *testing.T) {
	registry := NewToolRegistry()

	// Test registration
	executor := &mockToolExecutor{concurrentSafe: true}
	registry.Register("test_tool", executor)

	// Test retrieval
	retrieved, ok := registry.Get("test_tool")
	if !ok {
		t.Error("expected tool to be registered")
	}
	if retrieved != executor {
		t.Error("expected same executor instance")
	}

	// Test unknown tool
	_, ok = registry.Get("unknown")
	if ok {
		t.Error("expected unknown tool to not be found")
	}
}

func TestAbortController(t *testing.T) {
	ctx := context.Background()
	controller := NewAbortController(ctx)

	// Test initial state
	if controller.IsAborted() {
		t.Error("expected controller to not be aborted initially")
	}

	// Test abort
	controller.Abort()
	if !controller.IsAborted() {
		t.Error("expected controller to be aborted after Abort()")
	}

	// Test context cancellation
	select {
	case <-controller.Context().Done():
		// Expected
	default:
		t.Error("expected context to be cancelled")
	}
}

func TestExecuteTool(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		registry := NewToolRegistry()
		executor := &mockToolExecutor{
			concurrentSafe: true,
			executeFunc: func(ctx *ToolExecutionContext, call *ToolCall) (*ToolResult, error) {
				return &ToolResult{
					ToolUseID: call.ID,
					Content:   "test result",
				}, nil
			},
		}
		registry.Register("test_tool", executor)

		ctx := &ToolExecutionContext{
			Context:         context.Background(),
			AbortController: NewAbortController(context.Background()),
		}

		call := &ToolCall{
			ID:    "test-1",
			Name:  "test_tool",
			Input: map[string]interface{}{},
		}

		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = false

		result, err := ExecuteTool(ctx, call, registry, opts)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if result.Result == nil {
			t.Error("expected result to be set")
		}
		if result.Result.Content != "test result" {
			t.Errorf("expected content 'test result', got %v", result.Result.Content)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		registry := NewToolRegistry()
		ctx := &ToolExecutionContext{
			Context: context.Background(),
		}

		call := &ToolCall{
			ID:    "test-1",
			Name:  "unknown_tool",
			Input: map[string]interface{}{},
		}

		opts := DefaultToolExecutionOptions()
		_, err := ExecuteTool(ctx, call, registry, opts)
		if err == nil {
			t.Error("expected error for unknown tool")
		}
	})

	t.Run("validation error", func(t *testing.T) {
		registry := NewToolRegistry()
		executor := &mockToolExecutor{
			validateFunc: func(input map[string]interface{}) error {
				return errors.New("validation failed")
			},
		}
		registry.Register("test_tool", executor)

		ctx := &ToolExecutionContext{
			Context: context.Background(),
		}

		call := &ToolCall{
			ID:    "test-1",
			Name:  "test_tool",
			Input: map[string]interface{}{},
		}

		opts := DefaultToolExecutionOptions()
		_, err := ExecuteTool(ctx, call, registry, opts)
		if err == nil {
			t.Error("expected validation error")
		}
	})
}

func TestPartitionToolCalls(t *testing.T) {
	registry := NewToolRegistry()

	// Register concurrent-safe tool
	registry.Register("concurrent_tool", &mockToolExecutor{concurrentSafe: true})

	// Register non-concurrent tool
	registry.Register("serial_tool", &mockToolExecutor{concurrentSafe: false})

	t.Run("all concurrent-safe", func(t *testing.T) {
		calls := []*ToolCall{
			{ID: "1", Name: "concurrent_tool"},
			{ID: "2", Name: "concurrent_tool"},
			{ID: "3", Name: "concurrent_tool"},
		}

		batches := partitionToolCalls(calls, registry)
		if len(batches) != 1 {
			t.Errorf("expected 1 batch, got %d", len(batches))
		}
		if len(batches[0]) != 3 {
			t.Errorf("expected batch size 3, got %d", len(batches[0]))
		}
	})

	t.Run("mixed concurrent and serial", func(t *testing.T) {
		calls := []*ToolCall{
			{ID: "1", Name: "concurrent_tool"},
			{ID: "2", Name: "serial_tool"},
			{ID: "3", Name: "concurrent_tool"},
		}

		batches := partitionToolCalls(calls, registry)
		if len(batches) != 3 {
			t.Errorf("expected 3 batches, got %d", len(batches))
		}
		// First batch: concurrent tool
		if len(batches[0]) != 1 {
			t.Errorf("expected first batch size 1, got %d", len(batches[0]))
		}
		// Second batch: serial tool
		if len(batches[1]) != 1 {
			t.Errorf("expected second batch size 1, got %d", len(batches[1]))
		}
		// Third batch: concurrent tool
		if len(batches[2]) != 1 {
			t.Errorf("expected third batch size 1, got %d", len(batches[2]))
		}
	})
}

func TestRunTools(t *testing.T) {
	t.Run("empty calls", func(t *testing.T) {
		registry := NewToolRegistry()
		ctx := context.Background()
		execCtx := &ToolExecutionContext{
			Context: ctx,
		}
		opts := DefaultToolExecutionOptions()

		results, err := RunTools(ctx, []*ToolCall{}, registry, execCtx, opts)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(results) != 0 {
			t.Errorf("expected 0 results, got %d", len(results))
		}
	})

	t.Run("single tool", func(t *testing.T) {
		registry := NewToolRegistry()
		registry.Register("test_tool", &mockToolExecutor{concurrentSafe: true})

		ctx := context.Background()
		execCtx := &ToolExecutionContext{
			Context: ctx,
		}
		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = false

		calls := []*ToolCall{
			{ID: "1", Name: "test_tool", Input: map[string]interface{}{}},
		}

		results, err := RunTools(ctx, calls, registry, execCtx, opts)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(results) != 1 {
			t.Errorf("expected 1 result, got %d", len(results))
		}
	})

	t.Run("multiple concurrent tools", func(t *testing.T) {
		registry := NewToolRegistry()

		callCount := 0
		executor := &mockToolExecutor{
			concurrentSafe: true,
			executeFunc: func(ctx *ToolExecutionContext, call *ToolCall) (*ToolResult, error) {
				callCount++
				time.Sleep(10 * time.Millisecond)
				return &ToolResult{
					ToolUseID: call.ID,
					Content:   "success",
				}, nil
			},
		}
		registry.Register("test_tool", executor)

		ctx := context.Background()
		execCtx := &ToolExecutionContext{
			Context: ctx,
		}
		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = false

		calls := []*ToolCall{
			{ID: "1", Name: "test_tool", Input: map[string]interface{}{}},
			{ID: "2", Name: "test_tool", Input: map[string]interface{}{}},
			{ID: "3", Name: "test_tool", Input: map[string]interface{}{}},
		}

		results, err := RunTools(ctx, calls, registry, execCtx, opts)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(results) != 3 {
			t.Errorf("expected 3 results, got %d", len(results))
		}
		if callCount != 3 {
			t.Errorf("expected 3 calls, got %d", callCount)
		}
	})
}

func TestStreamingToolExecutor(t *testing.T) {
	t.Run("add and execute tools", func(t *testing.T) {
		registry := NewToolRegistry()
		registry.Register("test_tool", &mockToolExecutor{concurrentSafe: true})

		ctx := &ToolExecutionContext{
			Context:         context.Background(),
			AbortController: NewAbortController(context.Background()),
		}
		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = false

		executor := NewStreamingToolExecutor(ctx, registry, opts)

		// Add tools
		executor.AddTool(&ToolCall{ID: "1", Name: "test_tool", Input: map[string]interface{}{}})
		executor.AddTool(&ToolCall{ID: "2", Name: "test_tool", Input: map[string]interface{}{}})

		// Wait for completion
		results, err := executor.WaitForCompletion(context.Background())
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	})

	t.Run("abort execution", func(t *testing.T) {
		registry := NewToolRegistry()
		registry.Register("slow_tool", &mockToolExecutor{
			concurrentSafe: true,
			executeFunc: func(ctx *ToolExecutionContext, call *ToolCall) (*ToolResult, error) {
				time.Sleep(1 * time.Second)
				return &ToolResult{ToolUseID: call.ID, Content: "done"}, nil
			},
		})

		ctx := &ToolExecutionContext{
			Context:         context.Background(),
			AbortController: NewAbortController(context.Background()),
		}
		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = false

		executor := NewStreamingToolExecutor(ctx, registry, opts)

		executor.AddTool(&ToolCall{ID: "1", Name: "slow_tool", Input: map[string]interface{}{}})

		// Abort immediately
		executor.Abort()

		status := executor.GetStatus()
		if status["1"] != ToolStatusAborted && status["1"] != ToolStatusQueued {
			t.Errorf("expected tool to be aborted or queued, got %v", status["1"])
		}
	})
}

func TestToolHooks(t *testing.T) {
	t.Run("pre-hook denies execution", func(t *testing.T) {
		registry := NewToolRegistry()
		registry.Register("test_tool", &mockToolExecutor{concurrentSafe: true})

		hook := &mockHook{
			preFunc: func(ctx *ToolHookContext) (*ToolHookResult, error) {
				return &ToolHookResult{
					Decision: PermissionDeny,
				}, nil
			},
		}
		registry.AddHook(hook)

		ctx := &ToolExecutionContext{
			Context: context.Background(),
		}

		call := &ToolCall{
			ID:    "test-1",
			Name:  "test_tool",
			Input: map[string]interface{}{},
		}

		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = true

		_, err := ExecuteTool(ctx, call, registry, opts)
		if err == nil {
			t.Error("expected error when hook denies execution")
		}
	})

	t.Run("post-hook adds context", func(t *testing.T) {
		registry := NewToolRegistry()
		registry.Register("test_tool", &mockToolExecutor{concurrentSafe: true})

		hook := &mockHook{
			postFunc: func(ctx *ToolHookContext) (*ToolHookResult, error) {
				return &ToolHookResult{
					AdditionalContext: []string{"hook context"},
				}, nil
			},
		}
		registry.AddHook(hook)

		ctx := &ToolExecutionContext{
			Context: context.Background(),
		}

		call := &ToolCall{
			ID:    "test-1",
			Name:  "test_tool",
			Input: map[string]interface{}{},
		}

		opts := DefaultToolExecutionOptions()
		opts.EnableHooks = true

		result, err := ExecuteTool(ctx, call, registry, opts)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if len(result.AdditionalContext) != 1 {
			t.Errorf("expected 1 additional context, got %d", len(result.AdditionalContext))
		}
		if result.AdditionalContext[0] != "hook context" {
			t.Errorf("expected 'hook context', got %v", result.AdditionalContext[0])
		}
	})
}

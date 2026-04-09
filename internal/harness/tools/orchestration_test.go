package tools

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// MockTool implements ToolExecutor for testing
type MockTool struct {
	name              string
	concurrencySafe   bool
	executionDelay    time.Duration
	executionCount    int
	mu                sync.Mutex
	shouldError       bool
	modifyContext     bool
	executionOrder    []int
	executionOrderMu  sync.Mutex
}

func NewMockTool(name string, concurrencySafe bool) *MockTool {
	return &MockTool{
		name:            name,
		concurrencySafe: concurrencySafe,
		executionDelay:  10 * time.Millisecond,
		executionOrder:  []int{},
	}
}

func (m *MockTool) Execute(ctx context.Context, input map[string]any) (*ToolResult, error) {
	m.mu.Lock()
	m.executionCount++
	count := m.executionCount
	m.mu.Unlock()

	m.executionOrderMu.Lock()
	m.executionOrder = append(m.executionOrder, count)
	m.executionOrderMu.Unlock()

	if m.executionDelay > 0 {
		time.Sleep(m.executionDelay)
	}

	if m.shouldError {
		return nil, fmt.Errorf("mock error from %s", m.name)
	}

	result := &ToolResult{
		Content: fmt.Sprintf("Result from %s (execution %d)", m.name, count),
		IsError: false,
	}

	if m.modifyContext {
		result.ContextModifier = func(ctx *ToolUseContext) *ToolUseContext {
			newCtx := *ctx
			newCtx.SessionID = fmt.Sprintf("%s-modified-%d", ctx.SessionID, count)
			return &newCtx
		}
	}

	return result, nil
}

func (m *MockTool) IsConcurrencySafe(input map[string]any) bool {
	return m.concurrencySafe
}

func (m *MockTool) Name() string {
	return m.name
}

func (m *MockTool) Description() string {
	return fmt.Sprintf("Mock tool: %s", m.name)
}

func (m *MockTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string"},
		},
	}
}

func (m *MockTool) GetExecutionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executionCount
}

func (m *MockTool) GetExecutionOrder() []int {
	m.executionOrderMu.Lock()
	defer m.executionOrderMu.Unlock()
	return append([]int{}, m.executionOrder...)
}

// TestOrchestratorConcurrentExecution tests concurrent tool execution
func TestOrchestratorConcurrentExecution(t *testing.T) {
	tool1 := NewMockTool("tool1", true)
	tool2 := NewMockTool("tool2", true)
	tool3 := NewMockTool("tool3", true)

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{tool1, tool2, tool3})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "tool1", Input: map[string]any{"input": "test1"}},
		{ID: "2", Name: "tool2", Input: map[string]any{"input": "test2"}},
		{ID: "3", Name: "tool3", Input: map[string]any{"input": "test3"}},
	}

	start := time.Now()
	updates, err := orchestrator.RunTools(context.Background(), blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 3 {
		t.Errorf("Expected 3 updates, got %d", len(updates))
	}

	// Concurrent execution should take roughly the same time as one execution
	// (all run in parallel), not 3x the time
	maxExpected := 50 * time.Millisecond
	if elapsed > maxExpected {
		t.Errorf("Concurrent execution took too long: %v (expected < %v)", elapsed, maxExpected)
	}

	// Verify all tools were executed
	if tool1.GetExecutionCount() != 1 {
		t.Errorf("tool1 execution count: expected 1, got %d", tool1.GetExecutionCount())
	}
	if tool2.GetExecutionCount() != 1 {
		t.Errorf("tool2 execution count: expected 1, got %d", tool2.GetExecutionCount())
	}
	if tool3.GetExecutionCount() != 1 {
		t.Errorf("tool3 execution count: expected 1, got %d", tool3.GetExecutionCount())
	}
}

// TestOrchestratorSerialExecution tests serial tool execution
func TestOrchestratorSerialExecution(t *testing.T) {
	tool1 := NewMockTool("tool1", false)
	tool2 := NewMockTool("tool2", false)
	tool3 := NewMockTool("tool3", false)

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{tool1, tool2, tool3})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "tool1", Input: map[string]any{"input": "test1"}},
		{ID: "2", Name: "tool2", Input: map[string]any{"input": "test2"}},
		{ID: "3", Name: "tool3", Input: map[string]any{"input": "test3"}},
	}

	start := time.Now()
	updates, err := orchestrator.RunTools(context.Background(), blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 3 {
		t.Errorf("Expected 3 updates, got %d", len(updates))
	}

	// Serial execution should take at least 3x the execution time
	minExpected := 25 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("Serial execution too fast: %v (expected >= %v)", elapsed, minExpected)
	}

	// Verify all tools were executed exactly once
	if tool1.GetExecutionCount() != 1 {
		t.Errorf("tool1 execution count: expected 1, got %d", tool1.GetExecutionCount())
	}
	if tool2.GetExecutionCount() != 1 {
		t.Errorf("tool2 execution count: expected 1, got %d", tool2.GetExecutionCount())
	}
	if tool3.GetExecutionCount() != 1 {
		t.Errorf("tool3 execution count: expected 1, got %d", tool3.GetExecutionCount())
	}
}

// TestOrchestratorMixedExecution tests mixed concurrent and serial execution
func TestOrchestratorMixedExecution(t *testing.T) {
	concurrent1 := NewMockTool("concurrent1", true)
	concurrent2 := NewMockTool("concurrent2", true)
	serial1 := NewMockTool("serial1", false)
	serial2 := NewMockTool("serial2", false)

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{
		concurrent1, concurrent2, serial1, serial2,
	})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "concurrent1", Input: map[string]any{}},
		{ID: "2", Name: "concurrent2", Input: map[string]any{}},
		{ID: "3", Name: "serial1", Input: map[string]any{}},
		{ID: "4", Name: "serial2", Input: map[string]any{}},
	}

	updates, err := orchestrator.RunTools(context.Background(), blocks)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 4 {
		t.Errorf("Expected 4 updates, got %d", len(updates))
	}

	// Verify all tools executed
	if concurrent1.GetExecutionCount() != 1 {
		t.Errorf("concurrent1 not executed")
	}
	if concurrent2.GetExecutionCount() != 1 {
		t.Errorf("concurrent2 not executed")
	}
	if serial1.GetExecutionCount() != 1 {
		t.Errorf("serial1 not executed")
	}
	if serial2.GetExecutionCount() != 1 {
		t.Errorf("serial2 not executed")
	}
}

// TestOrchestratorPartitioning tests batch partitioning logic
func TestOrchestratorPartitioning(t *testing.T) {
	concurrent := NewMockTool("concurrent", true)
	serial := NewMockTool("serial", false)

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{concurrent, serial})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "concurrent", Input: map[string]any{}},
		{ID: "2", Name: "concurrent", Input: map[string]any{}},
		{ID: "3", Name: "serial", Input: map[string]any{}},
		{ID: "4", Name: "concurrent", Input: map[string]any{}},
		{ID: "5", Name: "serial", Input: map[string]any{}},
	}

	batches := orchestrator.partitionToolCalls(blocks)

	// Expected batches:
	// 1. [concurrent, concurrent] - concurrent batch
	// 2. [serial] - serial batch
	// 3. [concurrent] - concurrent batch
	// 4. [serial] - serial batch

	if len(batches) != 4 {
		t.Errorf("Expected 4 batches, got %d", len(batches))
	}

	if !batches[0].IsConcurrencySafe || len(batches[0].Blocks) != 2 {
		t.Errorf("Batch 0 incorrect: safe=%v, len=%d", batches[0].IsConcurrencySafe, len(batches[0].Blocks))
	}

	if batches[1].IsConcurrencySafe || len(batches[1].Blocks) != 1 {
		t.Errorf("Batch 1 incorrect: safe=%v, len=%d", batches[1].IsConcurrencySafe, len(batches[1].Blocks))
	}

	if !batches[2].IsConcurrencySafe || len(batches[2].Blocks) != 1 {
		t.Errorf("Batch 2 incorrect: safe=%v, len=%d", batches[2].IsConcurrencySafe, len(batches[2].Blocks))
	}

	if batches[3].IsConcurrencySafe || len(batches[3].Blocks) != 1 {
		t.Errorf("Batch 3 incorrect: safe=%v, len=%d", batches[3].IsConcurrencySafe, len(batches[3].Blocks))
	}
}

// TestOrchestratorPermissionDenial tests permission denial handling
func TestOrchestratorPermissionDenial(t *testing.T) {
	tool := NewMockTool("tool", true)
	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{tool})

	canUseTool := func(ctx context.Context, toolName string, input map[string]any, toolUseID string) (bool, string, error) {
		return false, "Permission denied for testing", nil
	}

	orchestrator := NewOrchestrator(ctx, canUseTool)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "tool", Input: map[string]any{}},
	}

	updates, err := orchestrator.RunTools(context.Background(), blocks)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(updates))
	}

	// Tool should not have been executed
	if tool.GetExecutionCount() != 0 {
		t.Errorf("Tool should not have been executed")
	}

	// Check for error in result
	if len(updates[0].Message.Content) == 0 {
		t.Fatal("No content in update")
	}

	content := updates[0].Message.Content[0]
	if content.Type != "tool_result" {
		t.Errorf("Expected tool_result, got %s", content.Type)
	}

	if !content.IsError {
		t.Error("Expected error result")
	}
}

// TestOrchestratorToolError tests tool execution error handling
func TestOrchestratorToolError(t *testing.T) {
	tool := NewMockTool("tool", true)
	tool.shouldError = true

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{tool})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "tool", Input: map[string]any{}},
	}

	updates, err := orchestrator.RunTools(context.Background(), blocks)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(updates))
	}

	// Check for error in result
	content := updates[0].Message.Content[0]
	if !content.IsError {
		t.Error("Expected error result")
	}
}

// TestOrchestratorContextModifier tests context modification
func TestOrchestratorContextModifier(t *testing.T) {
	tool := NewMockTool("tool", true)
	tool.modifyContext = true

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{tool})
	originalSessionID := ctx.SessionID

	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "tool", Input: map[string]any{}},
	}

	updates, err := orchestrator.RunTools(context.Background(), blocks)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(updates))
	}

	// Check that context was modified
	if updates[0].NewContext == nil {
		t.Fatal("NewContext is nil")
	}

	if updates[0].NewContext.SessionID == originalSessionID {
		t.Error("Context was not modified")
	}
}

// TestOrchestratorConcurrencyLimit tests max concurrency limiting
func TestOrchestratorConcurrencyLimit(t *testing.T) {
	tools := make([]ToolExecutor, 20)
	for i := 0; i < 20; i++ {
		tool := NewMockTool(fmt.Sprintf("tool%d", i), true)
		tool.executionDelay = 50 * time.Millisecond
		tools[i] = tool
	}

	ctx := NewToolUseContext("/tmp", "test-session", tools)
	ctx.MaxConcurrency = 5
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := make([]*ToolUseBlock, 20)
	for i := 0; i < 20; i++ {
		blocks[i] = &ToolUseBlock{
			ID:    fmt.Sprintf("%d", i),
			Name:  fmt.Sprintf("tool%d", i),
			Input: map[string]any{},
		}
	}

	start := time.Now()
	updates, err := orchestrator.RunTools(context.Background(), blocks)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("RunTools failed: %v", err)
	}

	if len(updates) != 20 {
		t.Errorf("Expected 20 updates, got %d", len(updates))
	}

	// With max concurrency of 5 and 20 tools taking 50ms each,
	// execution should take at least 4 * 50ms = 200ms (4 batches of 5)
	minExpected := 180 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("Execution too fast for concurrency limit: %v (expected >= %v)", elapsed, minExpected)
	}
}

// TestOrchestratorInProgressTracking tests in-progress tool tracking
func TestOrchestratorInProgressTracking(t *testing.T) {
	tool := NewMockTool("tool", false)
	tool.executionDelay = 100 * time.Millisecond

	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{tool})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "tool", Input: map[string]any{}},
	}

	done := make(chan struct{})
	go func() {
		_, _ = orchestrator.RunTools(context.Background(), blocks)
		close(done)
	}()

	// Wait a bit for execution to start
	time.Sleep(20 * time.Millisecond)

	// Check in-progress tracking
	inProgress := orchestrator.GetInProgressToolUseIDs()
	if len(inProgress) != 1 {
		t.Errorf("Expected 1 in-progress tool, got %d", len(inProgress))
	}

	// Wait for completion
	<-done

	// Check that tracking was cleared
	inProgress = orchestrator.GetInProgressToolUseIDs()
	if len(inProgress) != 0 {
		t.Errorf("Expected 0 in-progress tools after completion, got %d", len(inProgress))
	}
}

// TestOrchestratorUnknownTool tests handling of unknown tools
func TestOrchestratorUnknownTool(t *testing.T) {
	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{})
	orchestrator := NewOrchestrator(ctx, nil)

	blocks := []*ToolUseBlock{
		{ID: "1", Name: "unknown_tool", Input: map[string]any{}},
	}

	_, err := orchestrator.RunTools(context.Background(), blocks)

	if err == nil {
		t.Error("Expected error for unknown tool")
	}
}

// TestOrchestratorEmptyBlocks tests handling of empty tool blocks
func TestOrchestratorEmptyBlocks(t *testing.T) {
	ctx := NewToolUseContext("/tmp", "test-session", []ToolExecutor{})
	orchestrator := NewOrchestrator(ctx, nil)

	updates, err := orchestrator.RunTools(context.Background(), []*ToolUseBlock{})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if updates != nil {
		t.Error("Expected nil updates for empty blocks")
	}
}

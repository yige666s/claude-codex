package tool

import (
	"context"
	"sync"

	"claude-codex/internal/public/types"
)

// Update represents a streaming update from tool execution.
type Update struct {
	ToolUseID  string
	Message    interface{}
	Result     *ToolResult
	Error      error
	Status     string // "queued", "running", "completed", "failed"
	NewContext *ToolUseContext
}

// StreamingExecutor manages concurrent tool execution with streaming updates.
type StreamingExecutor struct {
	tools          []Tool
	canUseTool     CanUseToolFn
	toolUseContext *ToolUseContext

	mu             sync.Mutex
	queue          []types.ToolUseBlock
	results        chan *Update
	wg             sync.WaitGroup
	ctx            context.Context
	cancel         context.CancelFunc
	discarded      bool
}

// NewStreamingExecutor creates a new streaming executor.
func NewStreamingExecutor(tools []Tool, canUseTool CanUseToolFn, toolUseContext *ToolUseContext) *StreamingExecutor {
	ctx, cancel := context.WithCancel(toolUseContext.Ctx)
	return &StreamingExecutor{
		tools:          tools,
		canUseTool:     canUseTool,
		toolUseContext: toolUseContext,
		queue:          make([]types.ToolUseBlock, 0),
		results:        make(chan *Update, 10),
		ctx:            ctx,
		cancel:         cancel,
	}
}

// QueueTool adds a tool use block to the execution queue.
func (se *StreamingExecutor) QueueTool(toolUse types.ToolUseBlock) {
	se.mu.Lock()
	defer se.mu.Unlock()

	if se.discarded {
		return
	}

	se.queue = append(se.queue, toolUse)
	se.wg.Add(1)

	go se.executeTool(toolUse)
}

// executeTool executes a single tool asynchronously.
func (se *StreamingExecutor) executeTool(toolUse types.ToolUseBlock) {
	defer se.wg.Done()

	// Send queued status
	se.results <- &Update{
		ToolUseID: toolUse.ID,
		Status:    "queued",
	}

	// Find the tool
	tool := FindToolByName(se.tools, toolUse.Name)
	if tool == nil {
		se.results <- &Update{
			ToolUseID: toolUse.ID,
			Status:    "failed",
			Error:     ErrToolNotFound,
		}
		return
	}

	// Send running status
	se.results <- &Update{
		ToolUseID: toolUse.ID,
		Status:    "running",
	}

	// Execute the tool
	result, err := tool.Call(se.ctx, toolUse.Input, se.toolUseContext)

	if err != nil {
		se.results <- &Update{
			ToolUseID: toolUse.ID,
			Status:    "failed",
			Error:     err,
		}
		return
	}

	// Send completed status
	se.results <- &Update{
		ToolUseID: toolUse.ID,
		Status:    "completed",
		Result:    result,
	}
}

// GetRemainingResults returns a channel that receives all remaining results.
func (se *StreamingExecutor) GetRemainingResults() <-chan *Update {
	go func() {
		se.wg.Wait()
		close(se.results)
	}()
	return se.results
}

// Discard cancels all pending tool executions and marks the executor as discarded.
func (se *StreamingExecutor) Discard() {
	se.mu.Lock()
	defer se.mu.Unlock()

	se.discarded = true
	se.cancel()

	// Drain the results channel
	go func() {
		se.wg.Wait()
		close(se.results)
	}()
}

// Wait waits for all queued tools to complete.
func (se *StreamingExecutor) Wait() {
	se.wg.Wait()
	close(se.results)
}

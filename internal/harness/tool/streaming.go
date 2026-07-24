package tool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"claude-codex/internal/public/types"
)

// ExecutionLifecycle provides host/plugin callbacks around every tool call.
// Implementations may rewrite input before permission checks and may rewrite
// successful results after execution.
type ExecutionLifecycle interface {
	BeforeTool(ctx context.Context, toolUseID, toolName string, input map[string]interface{}) (map[string]interface{}, error)
	AfterTool(ctx context.Context, toolUseID, toolName string, input map[string]interface{}, result *ToolResult, executionErr error) (*ToolResult, error)
}

// Update represents a streaming update from tool execution.
type Update struct {
	ToolUseID  string
	ToolName   string
	Message    interface{}
	Result     *ToolResult
	Error      error
	Status     string // "queued", "running", "completed", "failed"
	NewContext *ToolUseContext
}

// StreamingExecutor manages concurrent tool execution with streaming updates.
type StreamingExecutor struct {
	canUseTool     CanUseToolFn
	toolUseContext *ToolUseContext
	lifecycle      ExecutionLifecycle

	mu        sync.Mutex
	queue     []types.ToolUseBlock
	results   chan *Update
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	discarded bool
}

// NewStreamingExecutor creates a new streaming executor.
func NewStreamingExecutor(canUseTool CanUseToolFn, toolUseContext *ToolUseContext, lifecycles ...ExecutionLifecycle) *StreamingExecutor {
	ctx, cancel := context.WithCancel(toolUseContext.Ctx)
	var lifecycle ExecutionLifecycle
	if len(lifecycles) > 0 {
		lifecycle = lifecycles[0]
	}
	return &StreamingExecutor{
		canUseTool:     canUseTool,
		toolUseContext: toolUseContext,
		lifecycle:      lifecycle,
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
		ToolName:  toolUse.Name,
		Status:    "queued",
	}

	// Find the tool
	tool := se.toolUseContext.FindToolByName(toolUse.Name)
	if tool == nil {
		se.sendFailure(toolUse, nil, nil, ErrToolNotFound)
		return
	}

	// Send running status
	se.results <- &Update{
		ToolUseID: toolUse.ID,
		ToolName:  toolUse.Name,
		Status:    "running",
	}

	input := toolUse.Input
	if input == nil {
		input = map[string]interface{}{}
	}
	if se.lifecycle != nil {
		updatedInput, err := se.lifecycle.BeforeTool(se.ctx, toolUse.ID, toolUse.Name, input)
		if err != nil {
			se.sendFailure(toolUse, input, nil, err)
			return
		}
		if updatedInput != nil {
			input = updatedInput
		}
	}
	if se.canUseTool != nil {
		permission, err := se.canUseTool(tool, input, se.toolUseContext, nil, toolUse.ID, nil)
		if err != nil {
			se.sendFailure(toolUse, input, nil, err)
			return
		}
		if permission != nil {
			if permission.UpdatedInput != nil {
				input = permission.UpdatedInput
			}
			if permission.Behavior == PermissionDeny {
				se.sendFailure(toolUse, input, nil, ErrPermissionDenied)
				return
			}
		}
	}

	// Execute the tool
	result, err := tool.Call(se.ctx, input, se.toolUseContext)

	if err != nil {
		se.sendFailure(toolUse, input, nil, err)
		return
	}
	if se.lifecycle != nil {
		result, err = se.lifecycle.AfterTool(se.ctx, toolUse.ID, toolUse.Name, input, result, nil)
		if err != nil {
			se.results <- &Update{
				ToolUseID: toolUse.ID,
				ToolName:  toolUse.Name,
				Status:    "failed",
				Error:     err,
			}
			return
		}
	}

	// Send completed status
	se.results <- &Update{
		ToolUseID: toolUse.ID,
		ToolName:  toolUse.Name,
		Status:    "completed",
		Result:    result,
	}
}

func (se *StreamingExecutor) sendFailure(toolUse types.ToolUseBlock, input map[string]interface{}, result *ToolResult, executionErr error) {
	finalErr := executionErr
	if se.lifecycle != nil {
		_, hookErr := se.lifecycle.AfterTool(se.ctx, toolUse.ID, toolUse.Name, input, result, executionErr)
		if hookErr != nil {
			if finalErr == nil {
				finalErr = hookErr
			} else {
				finalErr = errors.Join(finalErr, hookErr)
			}
		}
	}
	if finalErr == nil {
		finalErr = fmt.Errorf("tool %s failed without an error", toolUse.Name)
	}
	se.results <- &Update{
		ToolUseID: toolUse.ID,
		ToolName:  toolUse.Name,
		Status:    "failed",
		Error:     finalErr,
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

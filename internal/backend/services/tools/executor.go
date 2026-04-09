package tools

import (
	"context"
	"fmt"
	"time"
)

// ExecuteTool executes a single tool with full lifecycle
func ExecuteTool(
	ctx *ToolExecutionContext,
	call *ToolCall,
	registry *ToolRegistry,
	opts *ToolExecutionOptions,
) (*ToolExecutionResult, error) {
	startTime := time.Now()
	result := &ToolExecutionResult{
		AdditionalContext: make([]string, 0),
	}

	// Get tool executor
	executor, ok := registry.Get(call.Name)
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}

	// Validate input
	if err := executor.ValidateInput(call.Input); err != nil {
		return nil, fmt.Errorf("invalid input for tool %s: %w", call.Name, err)
	}

	// Run pre-tool hooks
	if opts.EnableHooks {
		hookCtx := &ToolHookContext{
			ToolName:    call.Name,
			ToolInput:   call.Input,
			SessionID:   ctx.SessionID,
			QuerySource: ctx.QuerySource,
		}

		for _, hook := range registry.GetHooks() {
			hookResult, err := hook.PreToolUse(hookCtx)
			if err != nil {
				return nil, fmt.Errorf("pre-tool hook error: %w", err)
			}

			if hookResult != nil {
				// Check permission decision
				if hookResult.Decision == PermissionDeny {
					result.PermissionDecision = PermissionDeny
					return result, fmt.Errorf("tool execution denied by hook")
				}

				// Check blocking error
				if hookResult.BlockingError != nil {
					return nil, hookResult.BlockingError
				}

				// Collect additional context
				if len(hookResult.AdditionalContext) > 0 {
					result.AdditionalContext = append(result.AdditionalContext, hookResult.AdditionalContext...)
					result.ModifiedContext = true
				}
			}
		}
	}

	// Execute the tool
	toolResult, execErr := executor.Execute(ctx, call)
	result.Duration = time.Since(startTime)

	// Handle execution result
	if execErr != nil {
		result.Error = execErr

		// Run post-tool failure hooks
		if opts.EnableHooks {
			hookCtx := &ToolHookContext{
				ToolName:    call.Name,
				ToolInput:   call.Input,
				Error:       execErr,
				SessionID:   ctx.SessionID,
				QuerySource: ctx.QuerySource,
			}

			for _, hook := range registry.GetHooks() {
				hookResult, err := hook.PostToolUseFailure(hookCtx)
				if err != nil {
					// Log hook error but don't fail the execution
					continue
				}

				if hookResult != nil && len(hookResult.AdditionalContext) > 0 {
					result.AdditionalContext = append(result.AdditionalContext, hookResult.AdditionalContext...)
					result.ModifiedContext = true
				}
			}
		}

		return result, execErr
	}

	result.Result = toolResult

	// Run post-tool success hooks
	if opts.EnableHooks {
		hookCtx := &ToolHookContext{
			ToolName:    call.Name,
			ToolInput:   call.Input,
			ToolResult:  toolResult,
			SessionID:   ctx.SessionID,
			QuerySource: ctx.QuerySource,
		}

		for _, hook := range registry.GetHooks() {
			hookResult, err := hook.PostToolUse(hookCtx)
			if err != nil {
				// Log hook error but don't fail the execution
				continue
			}

			if hookResult != nil && len(hookResult.AdditionalContext) > 0 {
				result.AdditionalContext = append(result.AdditionalContext, hookResult.AdditionalContext...)
				result.ModifiedContext = true
			}
		}
	}

	return result, nil
}

// RunTools executes multiple tools with orchestration
func RunTools(
	ctx context.Context,
	calls []*ToolCall,
	registry *ToolRegistry,
	execCtx *ToolExecutionContext,
	opts *ToolExecutionOptions,
) ([]*ToolExecutionResult, error) {
	if len(calls) == 0 {
		return []*ToolExecutionResult{}, nil
	}

	// Set execution context
	if execCtx.Context == nil {
		execCtx.Context = ctx
	}
	if execCtx.AbortController == nil {
		execCtx.AbortController = NewAbortController(ctx)
	}

	// Partition tools into concurrent-safe and non-concurrent batches
	batches := partitionToolCalls(calls, registry)

	results := make([]*ToolExecutionResult, 0, len(calls))

	// Execute each batch
	for _, batch := range batches {
		if len(batch) == 1 {
			// Single tool - execute serially
			result, err := ExecuteTool(execCtx, batch[0], registry, opts)
			if err != nil {
				return results, err
			}
			results = append(results, result)
		} else {
			// Multiple concurrent-safe tools - execute concurrently
			batchResults, err := runToolsConcurrently(execCtx, batch, registry, opts)
			if err != nil {
				return results, err
			}
			results = append(results, batchResults...)
		}

		// Check for abort
		if execCtx.AbortController.IsAborted() {
			return results, context.Canceled
		}
	}

	return results, nil
}

// partitionToolCalls splits tools into batches based on concurrency safety
func partitionToolCalls(calls []*ToolCall, registry *ToolRegistry) [][]*ToolCall {
	batches := make([][]*ToolCall, 0)
	currentBatch := make([]*ToolCall, 0)
	allConcurrentSafe := true

	for _, call := range calls {
		executor, ok := registry.Get(call.Name)
		if !ok {
			// Unknown tool - treat as non-concurrent
			if len(currentBatch) > 0 {
				batches = append(batches, currentBatch)
				currentBatch = make([]*ToolCall, 0)
			}
			batches = append(batches, []*ToolCall{call})
			allConcurrentSafe = true
			continue
		}

		isConcurrentSafe := executor.IsConcurrentSafe()

		if !isConcurrentSafe {
			// Non-concurrent tool - flush current batch and add as single batch
			if len(currentBatch) > 0 {
				batches = append(batches, currentBatch)
				currentBatch = make([]*ToolCall, 0)
			}
			batches = append(batches, []*ToolCall{call})
			allConcurrentSafe = true
		} else {
			// Concurrent-safe tool - add to current batch
			if !allConcurrentSafe {
				// Start new batch
				currentBatch = []*ToolCall{call}
				allConcurrentSafe = true
			} else {
				currentBatch = append(currentBatch, call)
			}
		}
	}

	// Add remaining batch
	if len(currentBatch) > 0 {
		batches = append(batches, currentBatch)
	}

	return batches
}

// runToolsConcurrently executes multiple concurrent-safe tools in parallel
func runToolsConcurrently(
	ctx *ToolExecutionContext,
	calls []*ToolCall,
	registry *ToolRegistry,
	opts *ToolExecutionOptions,
) ([]*ToolExecutionResult, error) {
	results := make([]*ToolExecutionResult, len(calls))
	errors := make([]error, len(calls))

	// Create semaphore for concurrency control
	sem := make(chan struct{}, opts.MaxConcurrency)
	done := make(chan int, len(calls))

	// Execute tools concurrently
	for i, call := range calls {
		go func(index int, toolCall *ToolCall) {
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Execute tool
			result, err := ExecuteTool(ctx, toolCall, registry, opts)
			results[index] = result
			errors[index] = err

			done <- index
		}(i, call)
	}

	// Wait for all tools to complete
	for i := 0; i < len(calls); i++ {
		<-done
	}

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return results, fmt.Errorf("tool %s failed: %w", calls[i].Name, err)
		}
	}

	return results, nil
}

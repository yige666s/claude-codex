package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StreamingToolExecutor manages streaming execution of tools with result buffering
type StreamingToolExecutor struct {
	registry *ToolRegistry
	opts     *ToolExecutionOptions
	execCtx  *ToolExecutionContext

	mu            sync.Mutex
	queue         []*queuedTool
	executing     map[string]*queuedTool
	completed     map[string]*ToolExecutionResult
	nextYieldIdx  int
	activeCount   int
	allAdded      bool
	processingErr error
}

// queuedTool represents a tool in the execution queue
type queuedTool struct {
	call   *ToolCall
	index  int
	status ToolStatus
	result *ToolExecutionResult
	err    error
}

// NewStreamingToolExecutor creates a new streaming tool executor
func NewStreamingToolExecutor(
	ctx *ToolExecutionContext,
	registry *ToolRegistry,
	opts *ToolExecutionOptions,
) *StreamingToolExecutor {
	if opts == nil {
		opts = DefaultToolExecutionOptions()
	}

	return &StreamingToolExecutor{
		registry:  registry,
		opts:      opts,
		execCtx:   ctx,
		queue:     make([]*queuedTool, 0),
		executing: make(map[string]*queuedTool),
		completed: make(map[string]*ToolExecutionResult),
	}
}

// AddTool adds a tool to the execution queue
func (s *StreamingToolExecutor) AddTool(call *ToolCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.allAdded {
		return fmt.Errorf("cannot add tools after FinishAdding")
	}

	qt := &queuedTool{
		call:   call,
		index:  len(s.queue),
		status: ToolStatusQueued,
	}

	s.queue = append(s.queue, qt)

	// Start processing if possible
	go s.processQueue()

	return nil
}

// FinishAdding marks that no more tools will be added
func (s *StreamingToolExecutor) FinishAdding() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allAdded = true
}

// processQueue starts queued tools when concurrency allows
func (s *StreamingToolExecutor) processQueue() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if we can start more tools
	for s.activeCount < s.opts.MaxConcurrency {
		// Find next queued tool
		var nextTool *queuedTool
		for _, qt := range s.queue {
			if qt.status == ToolStatusQueued {
				nextTool = qt
				break
			}
		}

		if nextTool == nil {
			break
		}

		// Check if tool is concurrent-safe
		executor, ok := s.registry.Get(nextTool.call.Name)
		if !ok {
			nextTool.status = ToolStatusFailed
			nextTool.err = fmt.Errorf("unknown tool: %s", nextTool.call.Name)
			continue
		}

		// If not concurrent-safe, wait for all active tools to complete
		if !executor.IsConcurrentSafe() && s.activeCount > 0 {
			break
		}

		// Start tool execution
		nextTool.status = ToolStatusExecuting
		s.executing[nextTool.call.ID] = nextTool
		s.activeCount++

		go s.executeTool(nextTool)
	}
}

// executeTool executes a single tool
func (s *StreamingToolExecutor) executeTool(qt *queuedTool) {
	defer func() {
		s.mu.Lock()
		s.activeCount--
		delete(s.executing, qt.call.ID)
		s.mu.Unlock()

		// Process more tools
		go s.processQueue()
	}()

	// Execute the tool
	result, err := ExecuteTool(s.execCtx, qt.call, s.registry, s.opts)

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		qt.status = ToolStatusFailed
		qt.err = err
		qt.result = result
	} else {
		qt.status = ToolStatusCompleted
		qt.result = result
	}

	s.completed[qt.call.ID] = result
}

// GetRemainingResults yields results in order
func (s *StreamingToolExecutor) GetRemainingResults() ([]*ToolExecutionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	results := make([]*ToolExecutionResult, 0)

	// Yield results in order
	for s.nextYieldIdx < len(s.queue) {
		qt := s.queue[s.nextYieldIdx]

		// Check if result is ready
		if qt.status == ToolStatusCompleted || qt.status == ToolStatusFailed {
			if qt.err != nil {
				return results, qt.err
			}
			results = append(results, qt.result)
			qt.status = ToolStatusYielded
			s.nextYieldIdx++
		} else {
			// Not ready yet
			break
		}
	}

	return results, nil
}

// WaitForCompletion waits for all tools to complete
func (s *StreamingToolExecutor) WaitForCompletion(ctx context.Context) ([]*ToolExecutionResult, error) {
	s.FinishAdding()

	allResults := make([]*ToolExecutionResult, 0)

	// Poll for completion
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return allResults, ctx.Err()
		case <-ticker.C:
			s.mu.Lock()
			allDone := s.nextYieldIdx >= len(s.queue) && s.activeCount == 0
			s.mu.Unlock()

			// Try to yield more results
			results, err := s.GetRemainingResults()
			if err != nil {
				return allResults, err
			}
			allResults = append(allResults, results...)

			if allDone {
				return allResults, nil
			}
		}
	}
}

// Abort cancels all pending and executing tools
func (s *StreamingToolExecutor) Abort() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel execution context
	if s.execCtx.AbortController != nil {
		s.execCtx.AbortController.Abort()
	}

	// Mark all non-completed tools as aborted
	for _, qt := range s.queue {
		if qt.status == ToolStatusQueued || qt.status == ToolStatusExecuting {
			qt.status = ToolStatusAborted
			qt.err = context.Canceled
		}
	}
}

// GetStatus returns the current execution status
func (s *StreamingToolExecutor) GetStatus() map[string]ToolStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := make(map[string]ToolStatus)
	for _, qt := range s.queue {
		status[qt.call.ID] = qt.status
	}
	return status
}

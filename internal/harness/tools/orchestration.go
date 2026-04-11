package tools

import (
	"context"
	"fmt"
	"sync"

	"claude-codex/internal/public/types"
)

// Orchestrator manages tool execution with support for concurrent and serial execution.
type Orchestrator struct {
	context        *ToolUseContext
	canUseTool     CanUseToolFunc
	maxConcurrency int
}

// NewOrchestrator creates a new tool orchestrator.
func NewOrchestrator(ctx *ToolUseContext, canUseTool CanUseToolFunc) *Orchestrator {
	maxConcurrency := ctx.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 10
	}

	return &Orchestrator{
		context:        ctx,
		canUseTool:     canUseTool,
		maxConcurrency: maxConcurrency,
	}
}

// RunTools executes a batch of tool use blocks, handling concurrency automatically.
func (o *Orchestrator) RunTools(
	ctx context.Context,
	toolUseBlocks []*ToolUseBlock,
) ([]*MessageUpdate, error) {
	if len(toolUseBlocks) == 0 {
		return nil, nil
	}

	// Partition tool calls into concurrent and serial batches
	batches := o.partitionToolCalls(toolUseBlocks)

	var allUpdates []*MessageUpdate
	currentContext := o.context

	for _, batch := range batches {
		var updates []*MessageUpdate
		var err error

		if batch.IsConcurrencySafe {
			// Run concurrently
			updates, err = o.runToolsConcurrently(ctx, batch.Blocks, currentContext)
		} else {
			// Run serially
			updates, err = o.runToolsSerially(ctx, batch.Blocks, currentContext)
		}

		if err != nil {
			return allUpdates, err
		}

		// Update context from the last update
		if len(updates) > 0 && updates[len(updates)-1].NewContext != nil {
			currentContext = updates[len(updates)-1].NewContext
		}

		allUpdates = append(allUpdates, updates...)
	}

	return allUpdates, nil
}

// Batch represents a group of tool calls that can be executed together.
type Batch struct {
	IsConcurrencySafe bool
	Blocks            []*ToolUseBlock
}

// partitionToolCalls splits tool calls into batches based on concurrency safety.
func (o *Orchestrator) partitionToolCalls(toolUseBlocks []*ToolUseBlock) []*Batch {
	var batches []*Batch

	for _, block := range toolUseBlocks {
		tool := o.context.FindToolByName(block.Name)
		if tool == nil {
			// Unknown tool, treat as not concurrency-safe
			batches = append(batches, &Batch{
				IsConcurrencySafe: false,
				Blocks:            []*ToolUseBlock{block},
			})
			continue
		}

		isConcurrencySafe := false
		if block.Input != nil {
			// Check if this specific invocation is concurrency-safe
			isConcurrencySafe = tool.IsConcurrencySafe(block.Input)
		}

		// Try to add to the last batch if it has the same concurrency safety
		if len(batches) > 0 && batches[len(batches)-1].IsConcurrencySafe == isConcurrencySafe && isConcurrencySafe {
			batches[len(batches)-1].Blocks = append(batches[len(batches)-1].Blocks, block)
		} else {
			batches = append(batches, &Batch{
				IsConcurrencySafe: isConcurrencySafe,
				Blocks:            []*ToolUseBlock{block},
			})
		}
	}

	return batches
}

// runToolsSerially executes tools one at a time.
func (o *Orchestrator) runToolsSerially(
	ctx context.Context,
	blocks []*ToolUseBlock,
	toolContext *ToolUseContext,
) ([]*MessageUpdate, error) {
	var updates []*MessageUpdate
	currentContext := toolContext

	for _, block := range blocks {
		// Mark as in progress
		currentContext.AddInProgressToolUse(block.ID)

		// Execute the tool
		update, err := o.executeToolUse(ctx, block, currentContext)
		if err != nil {
			currentContext.RemoveInProgressToolUse(block.ID)
			return updates, fmt.Errorf("tool execution failed: %w", err)
		}

		// Update context if modified
		if update.NewContext != nil {
			currentContext = update.NewContext
		}

		// Mark as complete
		currentContext.RemoveInProgressToolUse(block.ID)

		updates = append(updates, update)
	}

	return updates, nil
}

// runToolsConcurrently executes tools in parallel with a concurrency limit.
func (o *Orchestrator) runToolsConcurrently(
	ctx context.Context,
	blocks []*ToolUseBlock,
	toolContext *ToolUseContext,
) ([]*MessageUpdate, error) {
	if len(blocks) == 0 {
		return nil, nil
	}

	// Use a semaphore to limit concurrency
	sem := make(chan struct{}, o.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	updates := make([]*MessageUpdate, len(blocks))
	errors := make([]error, len(blocks))

	// Queue to collect context modifiers
	contextModifiers := make(map[string]func(*ToolUseContext) *ToolUseContext)

	for i, block := range blocks {
		wg.Add(1)
		go func(idx int, b *ToolUseBlock) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Mark as in progress
			mu.Lock()
			toolContext.AddInProgressToolUse(b.ID)
			mu.Unlock()

			// Execute the tool
			update, err := o.executeToolUse(ctx, b, toolContext)

			// Mark as complete
			mu.Lock()
			toolContext.RemoveInProgressToolUse(b.ID)
			mu.Unlock()

			if err != nil {
				mu.Lock()
				errors[idx] = err
				mu.Unlock()
				return
			}

			mu.Lock()
			updates[idx] = update

			// Collect context modifier if present
			if update.NewContext != nil && update.NewContext != toolContext {
				// Store the modifier function
				contextModifiers[b.ID] = func(ctx *ToolUseContext) *ToolUseContext {
					return update.NewContext
				}
			}
			mu.Unlock()
		}(i, block)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("tool %s failed: %w", blocks[i].Name, err)
		}
	}

	// Apply context modifiers in order
	currentContext := toolContext
	for _, block := range blocks {
		if modifier, ok := contextModifiers[block.ID]; ok {
			currentContext = modifier(currentContext)
		}
	}

	// Update the last update with the final context
	if len(updates) > 0 {
		updates[len(updates)-1].NewContext = currentContext
	}

	return updates, nil
}

// executeToolUse executes a single tool use.
func (o *Orchestrator) executeToolUse(
	ctx context.Context,
	block *ToolUseBlock,
	toolContext *ToolUseContext,
) (*MessageUpdate, error) {
	// Find the tool
	tool := toolContext.FindToolByName(block.Name)
	if tool == nil {
		return nil, fmt.Errorf("tool not found: %s", block.Name)
	}

	// Check permissions
	if o.canUseTool != nil {
		allowed, reason, err := o.canUseTool(ctx, block.Name, block.Input, block.ID)
		if err != nil {
			return nil, fmt.Errorf("permission check failed: %w", err)
		}
		if !allowed {
			return &MessageUpdate{
				Message: &types.Message{
					Type: types.MessageTypeUser,
					Content: []types.ContentBlock{
						{
							Type:      "tool_result",
							ToolUseID: block.ID,
							Content:   fmt.Sprintf("Permission denied: %s", reason),
							IsError:   true,
						},
					},
				},
				NewContext: toolContext,
			}, nil
		}
	}

	// Execute the tool
	result, err := tool.Execute(ctx, block.Input)
	if err != nil {
		return &MessageUpdate{
			Message: &types.Message{
				Type: types.MessageTypeUser,
				Content: []types.ContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: block.ID,
						Content:   fmt.Sprintf("Tool execution error: %v", err),
						IsError:   true,
					},
				},
			},
			NewContext: toolContext,
		}, nil
	}

	// Create the result message
	message := &types.Message{
		Type: types.MessageTypeUser,
		Content: []types.ContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   result.Content,
				IsError:   result.IsError,
			},
		},
	}

	// Apply context modifier if present
	newContext := toolContext
	if result.ContextModifier != nil {
		newContext = result.ContextModifier(toolContext)
	}

	return &MessageUpdate{
		Message:    message,
		NewContext: newContext,
	}, nil
}

// GetInProgressToolUseIDs returns the IDs of tools currently executing.
func (o *Orchestrator) GetInProgressToolUseIDs() []string {
	var ids []string
	for id := range o.context.InProgressToolUseIDs {
		ids = append(ids, id)
	}
	return ids
}

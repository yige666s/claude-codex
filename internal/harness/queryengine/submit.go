// Package engine provides message submission and streaming functionality.
package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SubmitMessageHandler handles the full lifecycle of a message submission.
type SubmitMessageHandler struct {
	engine  *QueryEngine
	ctx     context.Context
	prompt  interface{}
	options *SubmitOptions
	out     chan<- SDKMessage

	// State for this submission
	startTime             time.Time
	turnCount             int
	lastStopReason        string
	currentMessageUsage   *Usage
	structuredOutputFromTool interface{}
	persistSession        bool
	errorLogWatermark     string
}

// newSubmitMessageHandler creates a new submission handler.
func newSubmitMessageHandler(
	engine *QueryEngine,
	ctx context.Context,
	prompt interface{},
	options *SubmitOptions,
	out chan<- SDKMessage,
) *SubmitMessageHandler {
	return &SubmitMessageHandler{
		engine:              engine,
		ctx:                 ctx,
		prompt:              prompt,
		options:             options,
		out:                 out,
		startTime:           time.Now(),
		currentMessageUsage: EmptyUsage(),
		persistSession:      true, // TODO: Check if session persistence is disabled
	}
}

// execute runs the full message submission flow.
func (h *SubmitMessageHandler) execute() error {
	// 1. Process user input
	if err := h.processUserInput(); err != nil {
		return h.yieldError("error_during_execution", err)
	}

	// 2. Handle orphaned permissions
	if err := h.handleOrphanedPermissions(); err != nil {
		return h.yieldError("error_during_execution", err)
	}

	// 3. Build SystemPrompt and context
	if err := h.buildSystemContext(); err != nil {
		return h.yieldError("error_during_execution", err)
	}

	// 4. Yield system init message
	if err := h.yieldSystemInit(); err != nil {
		return err
	}

	// 5. Check if we should query (or just return local command results)
	shouldQuery, err := h.shouldQuery()
	if err != nil {
		return h.yieldError("error_during_execution", err)
	}

	if !shouldQuery {
		return h.yieldLocalCommandResults()
	}

	// 6. Enter query loop
	if err := h.queryLoop(); err != nil {
		return err
	}

	// 7. Yield final result
	return h.yieldFinalResult()
}

// processUserInput processes the user's input message.
func (h *SubmitMessageHandler) processUserInput() error {
	// TODO: Implement user input processing
	// - Parse slash commands
	// - Handle special commands
	// - Build message objects
	// - Update session state
	return nil
}

// handleOrphanedPermissions handles any orphaned permissions from previous turns.
func (h *SubmitMessageHandler) handleOrphanedPermissions() error {
	if h.engine.config.OrphanedPermission == nil {
		return nil
	}

	if h.engine.hasHandledOrphanedPermission {
		return nil
	}

	// TODO: Implement orphaned permission handling
	// - Find matching tool
	// - Execute with granted permission
	// - Yield results
	h.engine.hasHandledOrphanedPermission = true

	return nil
}

// buildSystemContext builds the SystemPrompt and context.
func (h *SubmitMessageHandler) buildSystemContext() error {
	// TODO: Implement system context building
	// - Fetch SystemPrompt parts
	// - Build user context
	// - Handle custom prompts
	// - Load memory if configured
	return nil
}

// yieldSystemInit yields the system initialization message.
func (h *SubmitMessageHandler) yieldSystemInit() error {
	// TODO: Build and yield system init message with:
	// - Available tools
	// - MCP clients
	// - Permission mode
	// - Commands
	// - Agents
	// - Skills
	// - Plugins
	// - Fast mode state

	msg := SDKMessage{
		Type:      "system",
		Subtype:   "init",
		SessionID: h.engine.GetSessionID(),
		UUID:      uuid.New().String(),
	}

	select {
	case h.out <- msg:
		return nil
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

// shouldQuery determines if we should query the API or just return local results.
func (h *SubmitMessageHandler) shouldQuery() (bool, error) {
	// TODO: Check if there are only local commands that don't need API
	return true, nil
}

// yieldLocalCommandResults yields results from local commands without querying API.
func (h *SubmitMessageHandler) yieldLocalCommandResults() error {
	// TODO: Yield local command output messages
	return h.yieldSuccess("")
}

// queryLoop runs the main query loop with the API.
func (h *SubmitMessageHandler) queryLoop() error {
	// TODO: Implement the query loop
	// - Call query() function
	// - Stream responses
	// - Handle tool calls
	// - Track usage
	// - Handle snip replay
	// - Check budget limits
	// - Check max turns
	// - Handle structured output retries

	// For now, just simulate a simple response
	h.turnCount = 1
	h.lastStopReason = "end_turn"

	return nil
}

// yieldFinalResult yields the final result message.
func (h *SubmitMessageHandler) yieldFinalResult() error {
	// Check if result is successful
	// TODO: Implement proper result validation
	isSuccessful := h.lastStopReason == "end_turn"

	if !isSuccessful {
		return h.yieldError("error_during_execution", fmt.Errorf("query did not complete successfully"))
	}

	return h.yieldSuccess("Query completed successfully")
}

// yieldSuccess yields a success result message.
func (h *SubmitMessageHandler) yieldSuccess(result string) error {
	msg := SDKMessage{
		Type:          "result",
		Subtype:       "success",
		SessionID:     h.engine.GetSessionID(),
		UUID:          uuid.New().String(),
		DurationMS:    time.Since(h.startTime).Milliseconds(),
		NumTurns:      h.turnCount,
		Result:        result,
		StopReason:    h.lastStopReason,
		Usage:         h.engine.GetTotalUsage(),
		PermissionDenials: h.engine.GetPermissionDenials(),
		StructuredOutput: h.structuredOutputFromTool,
	}

	select {
	case h.out <- msg:
		return nil
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

// yieldError yields an error result message.
func (h *SubmitMessageHandler) yieldError(subtype string, err error) error {
	msg := SDKMessage{
		Type:       "result",
		Subtype:    subtype,
		SessionID:  h.engine.GetSessionID(),
		UUID:       uuid.New().String(),
		DurationMS: time.Since(h.startTime).Milliseconds(),
		IsError:    true,
		NumTurns:   h.turnCount,
		StopReason: h.lastStopReason,
		Usage:      h.engine.GetTotalUsage(),
		PermissionDenials: h.engine.GetPermissionDenials(),
		Errors:     []string{err.Error()},
	}

	select {
	case h.out <- msg:
		return err
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

// yieldMessage yields a message to the output channel.
func (h *SubmitMessageHandler) yieldMessage(msg SDKMessage) error {
	select {
	case h.out <- msg:
		return nil
	case <-h.ctx.Done():
		return h.ctx.Err()
	}
}

// handleStreamEvent processes a streaming event from the API.
func (h *SubmitMessageHandler) handleStreamEvent(event StreamEvent) error {
	switch event.Type {
	case "message_start":
		h.currentMessageUsage = EmptyUsage()
		if event.Usage != nil {
			h.currentMessageUsage = UpdateUsage(h.currentMessageUsage, event.Usage)
		}

	case "message_delta":
		if event.Usage != nil {
			h.currentMessageUsage = UpdateUsage(h.currentMessageUsage, event.Usage)
		}
		// Extract stop_reason from delta if present
		if delta, ok := event.Delta.(map[string]interface{}); ok {
			if stopReason, ok := delta["stop_reason"].(string); ok && stopReason != "" {
				h.lastStopReason = stopReason
			}
		}

	case "message_stop":
		// Accumulate current message usage into total
		h.engine.updateUsage(h.currentMessageUsage)
		h.currentMessageUsage = EmptyUsage()
	}

	// Yield stream event if partial messages are enabled
	if h.engine.config.IncludePartialMessages {
		msg := SDKMessage{
			Type:            "stream_event",
			Event:           event,
			SessionID:       h.engine.GetSessionID(),
			ParentToolUseID: nil,
			UUID:            uuid.New().String(),
		}
		return h.yieldMessage(msg)
	}

	return nil
}

// handleMessage processes a message from the query loop.
func (h *SubmitMessageHandler) handleMessage(msg Message) error {
	// Track turn count
	if msg.Type == "user" {
		h.turnCount++
	}

	// Handle different message types
	switch msg.Type {
	case "tombstone":
		// Skip tombstone messages
		return nil

	case "assistant":
		// Capture stop_reason if set
		if stopReason, ok := msg.Message.(map[string]interface{})["stop_reason"].(string); ok && stopReason != "" {
			h.lastStopReason = stopReason
		}
		h.engine.addMessage(msg)
		return h.yieldNormalizedMessage(msg)

	case "user":
		h.engine.addMessage(msg)
		return h.yieldNormalizedMessage(msg)

	case "progress":
		h.engine.addMessage(msg)
		return h.yieldNormalizedMessage(msg)

	case "attachment":
		h.engine.addMessage(msg)
		return h.handleAttachment(msg)

	case "system":
		return h.handleSystemMessage(msg)

	default:
		return nil
	}
}

// handleAttachment processes attachment messages.
func (h *SubmitMessageHandler) handleAttachment(msg Message) error {
	// TODO: Handle different attachment types
	// - structured_output
	// - max_turns_reached
	// - queued_command
	return nil
}

// handleSystemMessage processes system messages.
func (h *SubmitMessageHandler) handleSystemMessage(msg Message) error {
	// Handle snip replay if configured
	if h.engine.config.SnipReplay != nil {
		if result := h.engine.config.SnipReplay(msg, h.engine.mutableMessages); result != nil {
			if result.Executed {
				// Replace messages with snipped version
				h.engine.mu.Lock()
				h.engine.mutableMessages = result.Messages
				h.engine.mu.Unlock()
			}
		}
	}

	return nil
}

// yieldNormalizedMessage yields a normalized SDK message.
func (h *SubmitMessageHandler) yieldNormalizedMessage(msg Message) error {
	// TODO: Implement message normalization
	// Convert internal Message to SDKMessage format
	sdkMsg := SDKMessage{
		Type:      msg.Type,
		Subtype:   msg.Subtype,
		SessionID: h.engine.GetSessionID(),
		UUID:      msg.UUID,
		Timestamp: &msg.Timestamp,
	}

	return h.yieldMessage(sdkMsg)
}

// checkBudgetLimit checks if the budget limit has been exceeded.
func (h *SubmitMessageHandler) checkBudgetLimit() (bool, error) {
	if h.engine.config.MaxBudgetUSD == nil {
		return false, nil
	}

	// TODO: Calculate actual cost from usage
	totalCost := 0.0 // Placeholder

	return totalCost >= *h.engine.config.MaxBudgetUSD, nil
}

// checkMaxTurns checks if max turns has been reached.
func (h *SubmitMessageHandler) checkMaxTurns() bool {
	if h.engine.config.MaxTurns <= 0 {
		return false
	}
	return h.turnCount >= h.engine.config.MaxTurns
}

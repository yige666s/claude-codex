package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
)

const (
	MessageTypeAssistant = types.MessageTypeAssistant
)

// convertMessagesToInterfaces converts []types.Message to []interface{}
func convertMessagesToInterfaces(messages []types.Message) []interface{} {
	result := make([]interface{}, len(messages))
	for i, msg := range messages {
		result[i] = msg
	}
	return result
}

// queryLoop is the core execution loop with state machine.
// It handles tool use, recovery, compaction, and all state transitions.
func queryLoop(
	ctx context.Context,
	params *QueryParams,
	consumedCommandUUIDs *[]string,
	eventChan chan<- interface{},
) (Terminal, error) {
	// Immutable params - never reassigned during the query loop
	SystemPrompt := params.SystemPrompt
	userContext := params.UserContext
	systemContext := params.SystemContext
	canUseTool := params.CanUseTool
	fallbackModel := params.FallbackModel
	querySource := params.QuerySource
	maxTurns := params.MaxTurns
	skipCacheWrite := params.SkipCacheWrite

	deps := params.Deps
	if deps == nil {
		deps = productionDeps()
	}

	config := buildQueryConfig()

	// Mutable cross-iteration state
	state := &State{
		Messages:                     params.Messages,
		ToolUseContext:               params.ToolUseContext,
		MaxOutputTokensOverride:      params.MaxOutputTokensOverride,
		AutoCompactTracking:          nil,
		StopHookActive:               nil,
		MaxOutputTokensRecoveryCount: 0,
		HasAttemptedReactiveCompact:  false,
		TurnCount:                    1,
		PendingToolUseSummary:        nil,
		Transition:                   nil,
	}

	// Memory prefetch - fired once per user turn
	// The prefetch runs asynchronously while the model streams and tools execute
	// Consumed post-tools if settled, otherwise retried next iteration
	var pendingMemoryPrefetch *MemoryPrefetch
	if memoryPrefetcher := getMemoryPrefetcher(); memoryPrefetcher != nil {
		surfacedBytes := collectSurfacedMemoryBytes(state.Messages)
		readFileState := state.ToolUseContext.ReadFileState
		if readFileState == nil {
			readFileState = make(map[string]bool)
		}
		prefetchMessages := convertMessagesToPrefetchMessages(state.Messages)
		pendingMemoryPrefetch = memoryPrefetcher.StartRelevantMemoryPrefetch(
			ctx,
			prefetchMessages,
			surfacedBytes,
			readFileState,
		)
		if pendingMemoryPrefetch != nil {
			defer pendingMemoryPrefetch.Dispose()
		}
	}

	var budgetTracker *BudgetTracker
	if params.TokenBudget != nil && *params.TokenBudget > 0 {
		budgetTracker = createBudgetTracker()
	}
	totalTurnOutputTokens := 0

	// task_budget.remaining tracking across compaction boundaries
	var taskBudgetRemaining *int
	if params.TaskBudget != nil {
		taskBudgetRemaining = params.TaskBudget.Remaining
	}

	// Query tracking for analytics
	var queryTracking *tool.QueryChainTracking
	if state.ToolUseContext.QueryTracking != nil {
		queryTracking = state.ToolUseContext.QueryTracking
	} else {
		queryTracking = &tool.QueryChainTracking{
			ChainID: "default",
			Depth:   0,
		}
	}

	// Main loop - continues until terminal condition
	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return Terminal{Reason: TerminalReasonAbortedStreaming}, ctx.Err()
		default:
		}

		// Destructure state for this iteration
		messagesForQuery := state.Messages
		toolUseContext := state.ToolUseContext
		tracking := state.AutoCompactTracking
		maxOutputTokensRecoveryCount := state.MaxOutputTokensRecoveryCount
		hasAttemptedReactiveCompact := state.HasAttemptedReactiveCompact
		maxOutputTokensOverride := state.MaxOutputTokensOverride
		pendingToolUseSummary := state.PendingToolUseSummary
		stopHookActive := state.StopHookActive
		turnCount := state.TurnCount

		// Handle queued commands
		if queuedCommands := getQueuedCommands(messagesForQuery); len(queuedCommands) > 0 {
			for _, cmd := range queuedCommands {
				*consumedCommandUUIDs = append(*consumedCommandUUIDs, cmd.UUID)
				notifyCommandLifecycle(cmd.UUID, "started")
			}

			// Remove commands from queue and add to messages
			updatedMessages, commandMessages := processQueuedCommands(messagesForQuery, queuedCommands)

			for _, msg := range commandMessages {
				eventChan <- msg
			}

			state.Messages = updatedMessages
			state.Transition = &Continue{Reason: ContinueReasonQueuedCommand}
			continue
		}

		// Auto-compaction check
		var compactionResult *CompactionResult
		var snipTokensFreed int

		if shouldAutoCompact(messagesForQuery, tracking, toolUseContext) {
			result, err := performCompaction(ctx, deps, messagesForQuery, toolUseContext, eventChan)
			if err != nil {
				// Handle compaction failure
				if tracking != nil {
					tracking.ConsecutiveFailures++
				}
			} else {
				compactionResult = result

				// Update task budget if needed
				if params.TaskBudget != nil {
					preCompactContext := finalContextTokensFromLastResponse(messagesForQuery)
					remaining := taskBudgetRemaining
					if remaining == nil {
						total := params.TaskBudget.Total
						remaining = &total
					}
					newRemaining := max(0, *remaining-preCompactContext)
					taskBudgetRemaining = &newRemaining
				}

				// Reset tracking after successful compact
				tracking = &AutoCompactTrackingState{
					Compacted:           true,
					TurnID:              deps.UUID(),
					TurnCounter:         0,
					ConsecutiveFailures: 0,
				}

				// Emit post-compact messages
				for _, msg := range result.Messages {
					eventChan <- msg
				}

				messagesForQuery = result.Messages
			}
		}

		// Update tool use context with current messages
		toolUseContext.Messages = convertMessagesToInterfaces(messagesForQuery)

		// Initialize iteration state
		assistantMessages := []types.AssistantMessage{}
		toolResults := []types.Message{}
		toolUseBlocks := []types.ToolUseBlock{}
		needsFollowUp := false

		// Streaming tool executor setup
		var streamingToolExecutor *tool.StreamingExecutor
		if config.Gates.StreamingToolExecution {
			streamingToolExecutor = tool.NewStreamingExecutor(
				canUseTool,
				toolUseContext,
			)
		}

		// Determine current model
		currentModel := getRuntimeMainLoopModel(toolUseContext, messagesForQuery)

		// Check blocking limit before API call
		if shouldCheckBlockingLimit(compactionResult, querySource, snipTokensFreed) {
			isBlocking := checkBlockingLimit(messagesForQuery, snipTokensFreed, toolUseContext)
			if isBlocking {
				eventChan <- createAssistantAPIErrorMessage("Prompt too long", "invalid_request")
				return Terminal{Reason: TerminalReasonBlockingLimit}, nil
			}
		}

		// API call with streaming
		attemptWithFallback := true
		lastModelCallInputCount := 0
		lastModelCallModel := currentModel
		for attemptWithFallback {
			attemptWithFallback = false

			modelParams := &ModelCallParams{
				Messages:       prependUserContext(messagesForQuery, userContext),
				SystemPrompt:   appendSystemContext(SystemPrompt, systemContext),
				Model:          currentModel,
				MaxTokens:      getMaxTokens(maxOutputTokensOverride),
				Tools:          toolUseContext.Tools(),
				SkipCacheWrite: skipCacheWrite,
				TaskBudget:     buildTaskBudgetParam(params.TaskBudget, taskBudgetRemaining),
			}
			lastModelCallInputCount = len(modelParams.Messages)
			lastModelCallModel = modelParams.Model

			messageChan, err := deps.CallModel(ctx, modelParams)
			if err != nil {
				return Terminal{Reason: TerminalReasonModelError, Error: err}, err
			}

			// Process streaming messages
			streamingFallbackOccurred := false
			for msg := range messageChan {
				// Handle fallback during streaming
				if isFallbackError(msg) && fallbackModel != "" {
					streamingFallbackOccurred = true

					// Clear state for retry
					yieldMissingToolResultBlocks(assistantMessages, "Model fallback triggered", eventChan)
					assistantMessages = []types.AssistantMessage{}
					toolResults = []types.Message{}
					toolUseBlocks = []types.ToolUseBlock{}
					needsFollowUp = false

					if streamingToolExecutor != nil {
						streamingToolExecutor.Discard()
						streamingToolExecutor = tool.NewStreamingExecutor(
							canUseTool,
							toolUseContext,
						)
					}

					// Update to fallback model
					toolUseContext.Options.MainLoopModel = fallbackModel
					currentModel = fallbackModel
					messagesForQuery = stripSignatureBlocks(messagesForQuery)

					eventChan <- createSystemMessage(
						fmt.Sprintf("Switched to %s due to high demand", fallbackModel),
						"warning",
					)

					attemptWithFallback = true
					break
				}

				// Process message
				if msg.Type == types.MessageTypeAssistant {
					// msg is already types.Message, extract assistant data from it
					assistantMsg := types.AssistantMessage{
						Type:    string(msg.Type),
						Message: msg,
					}
					assistantMessages = append(assistantMessages, assistantMsg)
					eventChan <- assistantMsg

					// Extract tool use blocks
					for _, content := range assistantMsg.Message.Content {
						if content.Type == "tool_use" {
							toolUseBlock := types.ToolUseBlock{
								Type:  content.Type,
								ID:    content.ID,
								Name:  content.Name,
								Input: content.Input,
							}
							toolUseBlocks = append(toolUseBlocks, toolUseBlock)
							needsFollowUp = true

							// Start streaming tool execution if enabled
							if streamingToolExecutor != nil {
								streamingToolExecutor.QueueTool(toolUseBlock)
							}
						}
					}
				} else {
					eventChan <- msg
				}
			}

			if streamingFallbackOccurred {
				continue
			}
		}

		// Check for abort after streaming
		if toolUseContext.AbortController != nil && toolUseContext.AbortController.Signal.Aborted {
			if streamingToolExecutor != nil {
				// Consume remaining results
				for update := range streamingToolExecutor.GetRemainingResults() {
					if update.Message != nil {
						eventChan <- update.Message
					}
				}
			} else {
				yieldMissingToolResultBlocks(assistantMessages, "Interrupted by user", eventChan)
			}

			if toolUseContext.AbortController.Signal.Reason != "interrupt" {
				eventChan <- createUserInterruptionMessage(false)
			}
			return Terminal{Reason: TerminalReasonAbortedStreaming}, nil
		}
		if !hasMeaningfulAssistantMessages(assistantMessages) {
			err := fmt.Errorf("model call returned no assistant messages: no assistant text or tool calls (model=%s input_messages=%d assistant_messages=%d)", lastModelCallModel, lastModelCallInputCount, len(assistantMessages))
			return Terminal{Reason: TerminalReasonModelError, Error: err}, err
		}

		// Yield pending tool use summary from previous turn
		if pendingToolUseSummary != nil {
			select {
			case summary := <-pendingToolUseSummary:
				if summary != nil {
					eventChan <- summary
				}
			case <-time.After(100 * time.Millisecond):
				// Timeout waiting for summary
			}
		}

		// Handle prompt-too-long recovery
		if !needsFollowUp {
			lastMessage := getLastMessage(assistantMessages)
			if isWithheldPromptTooLong(lastMessage) && !hasAttemptedReactiveCompact {
				// Attempt reactive compaction
				result, err := performReactiveCompaction(ctx, deps, messagesForQuery, toolUseContext, eventChan)
				if err == nil && result != nil {
					state.Messages = result.Messages
					state.HasAttemptedReactiveCompact = true
					state.Transition = &Continue{Reason: ContinueReasonReactiveCompactRetry}
					continue
				}
			}
		}

		// Handle max_output_tokens recovery
		if !needsFollowUp {
			lastMessage := getLastMessage(assistantMessages)
			if isMaxOutputTokensError(lastMessage) && maxOutputTokensRecoveryCount < MaxOutputTokensRecoveryLimit {
				// Retry with increased tokens or escalated model
				state.MaxOutputTokensRecoveryCount++

				if maxOutputTokensRecoveryCount < 2 {
					// First two attempts: increase max_output_tokens
					newMax := getMaxTokens(maxOutputTokensOverride) + 4096
					state.MaxOutputTokensOverride = &newMax
					state.Transition = &Continue{Reason: ContinueReasonMaxOutputTokensRecovery}
				} else {
					// Third attempt: escalate to larger model
					state.Transition = &Continue{Reason: ContinueReasonMaxOutputTokensEscalate}
				}
				continue
			}
		}

		// If no follow-up needed, handle stop hooks and completion
		if !needsFollowUp {
			// Execute stop hooks
			stopHookResult, err := handleStopHooks(
				ctx,
				messagesForQuery,
				assistantMessages,
				SystemPrompt,
				userContext,
				systemContext,
				toolUseContext,
				querySource,
				stopHookActive,
				eventChan,
			)
			if err != nil {
				return Terminal{Reason: TerminalReasonModelError, Error: err}, err
			}

			// Handle stop hook results
			if stopHookResult.PreventContinuation {
				return Terminal{Reason: TerminalReasonStopHookPrevented, Messages: append(messagesForQuery, convertToMessages(assistantMessages)...)}, nil
			}

			if len(stopHookResult.BlockingErrors) > 0 {
				// Add blocking errors and retry
				for _, errMsg := range stopHookResult.BlockingErrors {
					toolResults = append(toolResults, errMsg)
				}

				state.Messages = append(messagesForQuery, convertToMessages(assistantMessages)...)
				state.Messages = append(state.Messages, toolResults...)
				state.StopHookActive = boolPtr(true)
				state.Transition = &Continue{Reason: ContinueReasonStopHookBlocking}
				continue
			}

			// Check token budget
			if budgetTracker != nil {
				totalTurnOutputTokens += estimateMessageTokens(convertToMessages(assistantMessages))
				decision := checkTokenBudget(
					budgetTracker,
					toolUseContext.AgentID,
					params.TokenBudget,
					totalTurnOutputTokens,
				)

				if decision.Action == "continue" {
					nudgeMsg := createUserMessage(decision.NudgeMessage, true)
					state.Messages = append(messagesForQuery, convertToMessages(assistantMessages)...)
					state.Messages = append(state.Messages, nudgeMsg)
					state.MaxOutputTokensRecoveryCount = 0
					state.HasAttemptedReactiveCompact = false
					state.MaxOutputTokensOverride = nil
					state.PendingToolUseSummary = nil
					state.StopHookActive = nil
					state.Transition = &Continue{Reason: ContinueReasonTokenBudgetContinuation}
					continue
				}
			}

			return Terminal{Reason: TerminalReasonCompleted, Messages: append(messagesForQuery, convertToMessages(assistantMessages)...)}, nil
		}

		// Execute tools
		shouldPreventContinuation := false
		updatedToolUseContext := toolUseContext

		var toolUpdates <-chan *tool.Update
		if streamingToolExecutor != nil {
			toolUpdates = streamingToolExecutor.GetRemainingResults()
		} else {
			toolUpdates = runTools(toolUseBlocks, assistantMessages, canUseTool, toolUseContext)
		}

		for update := range toolUpdates {
			if msg := messageFromToolUpdate(update); msg != nil {
				eventChan <- *msg

				// Type assert to check message type
				if isHookStoppedContinuation(*msg) {
					shouldPreventContinuation = true
				}

				// Add to tool results if it's a user message
				if msg.Type == types.MessageTypeUser {
					toolResults = append(toolResults, *msg)
				}
			}
			if update.NewContext != nil {
				updatedToolUseContext = update.NewContext
				updatedToolUseContext.QueryTracking = queryTracking
			}
		}

		// Generate tool use summary for next turn
		var nextPendingToolUseSummary chan *types.ToolUseSummaryMessage
		if config.Gates.EmitToolUseSummaries && len(toolUseBlocks) > 0 && !toolUseContext.AbortController.Signal.Aborted && toolUseContext.AgentID == "" {
			nextPendingToolUseSummary = generateToolUseSummaryAsync(
				toolUseBlocks,
				toolResults,
				assistantMessages,
				toolUseContext,
			)
		}

		// Check for abort during tool execution
		if toolUseContext.AbortController.Signal.Aborted {
			if toolUseContext.AbortController.Signal.Reason != "interrupt" {
				eventChan <- createUserInterruptionMessage(true)
			}
			return Terminal{Reason: TerminalReasonAbortedTools}, nil
		}

		// Check if hooks prevented continuation
		if shouldPreventContinuation {
			return Terminal{Reason: TerminalReasonHookStopped, Messages: append(append(messagesForQuery, convertToMessages(assistantMessages)...), toolResults...)}, nil
		}

		// Memory prefetch consume: only if settled and not already consumed.
		// If not settled yet, skip (zero-wait) and retry next iteration.
		// readFileState filters out memories the model already accessed.
		consumeReadFileState := updatedToolUseContext.ReadFileState
		if consumeReadFileState == nil {
			consumeReadFileState = make(map[string]bool)
		}
		if memoryMsgs := consumeMemoryPrefetch(
			pendingMemoryPrefetch,
			consumeReadFileState,
			turnCount,
		); len(memoryMsgs) > 0 {
			for _, msg := range memoryMsgs {
				eventChan <- msg
				toolResults = append(toolResults, msg)
			}
		}

		// Prepare for next turn
		nextTurnCount := turnCount + 1

		// Check max turns limit
		if maxTurns != nil && nextTurnCount > *maxTurns {
			eventChan <- createAttachmentMessage("max_turns_reached", *maxTurns, nextTurnCount)
			return Terminal{Reason: TerminalReasonMaxTurns, TurnCount: nextTurnCount}, nil
		}

		// Update state for next iteration
		state = &State{
			Messages:                     append(append(messagesForQuery, convertToMessages(assistantMessages)...), toolResults...),
			ToolUseContext:               updatedToolUseContext,
			AutoCompactTracking:          tracking,
			TurnCount:                    nextTurnCount,
			MaxOutputTokensRecoveryCount: 0,
			HasAttemptedReactiveCompact:  false,
			PendingToolUseSummary:        nextPendingToolUseSummary,
			MaxOutputTokensOverride:      nil,
			StopHookActive:               stopHookActive,
			Transition:                   &Continue{Reason: ContinueReasonNextTurn},
		}
	}
}

// Helper functions

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolPtr(b bool) *bool {
	return &b
}

func convertToMessages(assistantMessages []types.AssistantMessage) []types.Message {
	messages := make([]types.Message, len(assistantMessages))
	for i, msg := range assistantMessages {
		messages[i] = msg.Message
	}
	return messages
}

func getLastMessage(messages []types.AssistantMessage) *types.AssistantMessage {
	if len(messages) == 0 {
		return nil
	}
	return &messages[len(messages)-1]
}

func hasMeaningfulAssistantMessages(messages []types.AssistantMessage) bool {
	for _, msg := range messages {
		for _, block := range msg.Message.Content {
			if block.Type == "tool_use" && (strings.TrimSpace(block.ID) != "" || strings.TrimSpace(block.Name) != "") {
				return true
			}
			if strings.TrimSpace(block.Text) != "" || strings.TrimSpace(block.Content) != "" {
				return true
			}
		}
	}
	return false
}

func getQueuedCommands(messages []types.Message) []QueuedCommand {
	return drainQueuedCommands()
}

func processQueuedCommands(messages []types.Message, commands []QueuedCommand) ([]types.Message, []types.Message) {
	if len(commands) == 0 {
		return messages, nil
	}
	commandMessages := make([]types.Message, 0, len(commands))
	for _, cmd := range commands {
		msg := createUserMessage(cmd.Content, true)
		if cmd.UUID != "" {
			msg.UUID = cmd.UUID
		}
		commandMessages = append(commandMessages, msg)
	}
	updated := append(append([]types.Message(nil), messages...), commandMessages...)
	return updated, commandMessages
}

func finalContextTokensFromLastResponse(messages []types.Message) int {
	// TODO: Implement token counting
	return 0
}

func getRuntimeMainLoopModel(ctx *tool.ToolUseContext, messages []types.Message) string {
	if ctx != nil && strings.TrimSpace(ctx.Options.MainLoopModel) != "" {
		return ctx.Options.MainLoopModel
	}
	return "claude-3-5-sonnet-20241022"
}

func shouldCheckBlockingLimit(compactionResult *CompactionResult, querySource string, snipTokensFreed int) bool {
	if compactionResult != nil {
		return false
	}
	switch querySource {
	case "compact", "session_memory":
		return false
	default:
		return true
	}
}

func checkBlockingLimit(messages []types.Message, snipTokensFreed int, ctx *tool.ToolUseContext) bool {
	tokenCount := estimateMessageTokens(messages) - snipTokensFreed
	model := ""
	if ctx != nil {
		model = ctx.Options.MainLoopModel
	}
	return calculateTokenWarningState(tokenCount, model).IsAtBlockingLimit
}

func prependUserContext(messages []types.Message, userContext map[string]string) []types.Message {
	if len(userContext) == 0 {
		return messages
	}

	// Build a meta user message containing the user context entries.
	// This mirrors the TypeScript behaviour: user context is prepended as a
	// hidden/meta message at the start of the conversation so the model sees
	// CLAUDE.md content and the current date without it being part of the
	// visible history.
	var parts []string
	// Emit keys in a stable order: claudeMd first, currentDate second, rest alphabetical.
	order := []string{"claudeMd", "currentDate"}
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := userContext[k]; ok && v != "" {
			parts = append(parts, v)
			seen[k] = true
		}
	}
	for k, v := range userContext {
		if !seen[k] && v != "" {
			parts = append(parts, v)
		}
	}

	if len(parts) == 0 {
		return messages
	}

	contextMsg := types.Message{
		Type:   types.MessageTypeUser,
		IsMeta: true,
		Content: []types.ContentBlock{
			{Type: "text", Text: strings.Join(parts, "\n\n")},
		},
	}

	result := make([]types.Message, 0, len(messages)+1)
	result = append(result, contextMsg)
	result = append(result, messages...)
	return result
}

func appendSystemContext(SystemPrompt types.SystemPrompt, systemContext map[string]string) types.SystemPrompt {
	if len(systemContext) == 0 {
		return SystemPrompt
	}

	// Build context text from the systemContext map.
	// Stable key order: gitStatus first, then rest.
	var parts []string
	order := []string{"gitStatus", "cacheBreaker"}
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := systemContext[k]; ok && v != "" {
			parts = append(parts, v)
			seen[k] = true
		}
	}
	for k, v := range systemContext {
		if !seen[k] && v != "" {
			parts = append(parts, v)
		}
	}

	if len(parts) == 0 {
		return SystemPrompt
	}

	extra := strings.Join(parts, "\n\n")

	// Append to Content and add a new Part.
	updated := SystemPrompt
	if updated.Content != "" {
		updated.Content += "\n\n" + extra
	} else {
		updated.Content = extra
	}
	updated.Parts = append(updated.Parts, types.SystemPromptPart{
		Type:    "text",
		Content: extra,
		Cache:   true,
	})
	return updated
}

func getMaxTokens(override *int) int {
	if override != nil {
		return *override
	}
	return 8192
}

func buildTaskBudgetParam(budget *TaskBudget, remaining *int) *TaskBudget {
	if budget == nil {
		return nil
	}
	return &TaskBudget{
		Total:     budget.Total,
		Remaining: remaining,
	}
}

func isFallbackError(msg types.Message) bool {
	if !msg.IsApiErrorMessage {
		return false
	}
	subtype := strings.ToLower(msg.Subtype)
	if subtype == "overloaded_error" || subtype == "rate_limit_error" || subtype == "fallback" {
		return true
	}
	text := strings.ToLower(messageText(msg))
	return strings.Contains(text, "overloaded") ||
		strings.Contains(text, "high demand") ||
		strings.Contains(text, "rate limit")
}

func yieldMissingToolResultBlocks(assistantMessages []types.AssistantMessage, errorMessage string, eventChan chan<- interface{}) {
	for _, assistant := range assistantMessages {
		for _, block := range assistant.Message.Content {
			if block.Type != "tool_use" || block.ID == "" {
				continue
			}
			eventChan <- createToolResultMessage(block.ID, errorMessage, true)
		}
	}
}

func stripSignatureBlocks(messages []types.Message) []types.Message {
	out := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if len(msg.Content) == 0 {
			out = append(out, msg)
			continue
		}
		clone := msg
		clone.Content = clone.Content[:0]
		for _, block := range msg.Content {
			switch block.Type {
			case "signature", "redacted_signature":
				continue
			default:
				clone.Content = append(clone.Content, block)
			}
		}
		out = append(out, clone)
	}
	return out
}

func createSystemMessage(content, level string) types.Message {
	return types.Message{
		Type:      types.MessageTypeSystem,
		UUID:      types.UUID(),
		Timestamp: time.Now().UTC(),
		Subtype:   level,
		Content:   []types.ContentBlock{{Type: "text", Text: content}},
	}
}

func createUserInterruptionMessage(toolUse bool) types.Message {
	content := "User interrupted the response."
	if toolUse {
		content = "User interrupted tool execution."
	}
	return createUserMessage(content, true)
}

func isWithheldPromptTooLong(msg *types.AssistantMessage) bool {
	if msg == nil {
		return false
	}
	if msg.APIError == "prompt_too_long" {
		return true
	}
	return msg.Message.IsApiErrorMessage && strings.Contains(strings.ToLower(messageText(msg.Message)), "prompt too long")
}

func isMaxOutputTokensError(msg *types.AssistantMessage) bool {
	if msg == nil {
		return false
	}
	if msg.APIError == "max_output_tokens" || msg.Message.StopReason == "max_tokens" || msg.Message.StopReason == "max_output_tokens" {
		return true
	}
	return msg.Message.IsApiErrorMessage && strings.Contains(strings.ToLower(messageText(msg.Message)), "max output")
}

func isHookStoppedContinuation(msg types.Message) bool {
	if msg.Type != types.MessageTypeAttachment {
		return false
	}
	if msg.Subtype == "hook_stopped_continuation" {
		return true
	}
	if attachment, ok := msg.Attachment.(map[string]interface{}); ok {
		return attachment["type"] == "hook_stopped_continuation"
	}
	return false
}

func runTools(toolUseBlocks []types.ToolUseBlock, assistantMessages []types.AssistantMessage, canUseTool tool.CanUseToolFn, ctx *tool.ToolUseContext) <-chan *tool.Update {
	ch := make(chan *tool.Update, len(toolUseBlocks))
	go func() {
		defer close(ch)
		for _, block := range toolUseBlocks {
			toolDef := ctx.FindToolByName(block.Name)
			if toolDef == nil {
				ch <- &tool.Update{ToolUseID: block.ID, ToolName: block.Name, Status: "failed", Error: tool.ErrToolNotFound}
				continue
			}
			input := block.Input
			if input == nil {
				input = map[string]interface{}{}
			}
			if canUseTool != nil {
				permission, err := canUseTool(toolDef, input, ctx, assistantMessages, block.ID, nil)
				if err != nil {
					ch <- &tool.Update{ToolUseID: block.ID, ToolName: block.Name, Status: "failed", Error: err}
					continue
				}
				if permission != nil {
					if permission.UpdatedInput != nil {
						input = permission.UpdatedInput
					}
					if permission.Behavior == tool.PermissionDeny {
						ch <- &tool.Update{ToolUseID: block.ID, ToolName: block.Name, Status: "failed", Error: fmt.Errorf("permission denied: %s", permission.Reason)}
						continue
					}
				}
			}
			result, err := toolDef.Call(ctx.Ctx, input, ctx)
			if err != nil {
				ch <- &tool.Update{ToolUseID: block.ID, ToolName: block.Name, Status: "failed", Error: err}
				continue
			}
			if result != nil && result.ContextModifier != nil {
				nextCtx := *ctx
				result.ContextModifier(&nextCtx)
				ch <- &tool.Update{ToolUseID: block.ID, ToolName: block.Name, Status: "completed", Result: result, NewContext: &nextCtx}
				continue
			}
			ch <- &tool.Update{ToolUseID: block.ID, ToolName: block.Name, Status: "completed", Result: result}
		}
	}()
	return ch
}

func generateToolUseSummaryAsync(toolUseBlocks []types.ToolUseBlock, toolResults []types.Message, assistantMessages []types.AssistantMessage, ctx *tool.ToolUseContext) chan *types.ToolUseSummaryMessage {
	// TODO: Implement tool use summary generation
	ch := make(chan *types.ToolUseSummaryMessage, 1)
	close(ch)
	return ch
}

func createAttachmentMessage(msgType string, maxTurns, turnCount int) types.Message {
	return types.Message{
		Type:      types.MessageTypeAttachment,
		UUID:      types.UUID(),
		Timestamp: time.Now().UTC(),
		Subtype:   msgType,
		Attachment: map[string]interface{}{
			"type":       msgType,
			"max_turns":  maxTurns,
			"turn_count": turnCount,
		},
	}
}

func createAssistantAPIErrorMessage(content, errorType string) types.Message {
	return types.Message{
		Type:              types.MessageTypeAssistant,
		UUID:              types.UUID(),
		Timestamp:         time.Now().UTC(),
		Subtype:           errorType,
		IsApiErrorMessage: true,
		Content:           []types.ContentBlock{{Type: "text", Text: content}},
	}
}

func createUserMessage(content string, isMeta bool) types.Message {
	return types.Message{
		Type:      types.MessageTypeUser,
		UUID:      types.UUID(),
		Timestamp: time.Now().UTC(),
		IsMeta:    isMeta,
		Content:   []types.ContentBlock{{Type: "text", Text: content}},
	}
}

// QueuedCommand represents a queued command
type QueuedCommand struct {
	UUID    string
	Content string
}

func messageFromToolUpdate(update *tool.Update) *types.Message {
	if update == nil {
		return nil
	}
	if update.Message != nil {
		switch msg := update.Message.(type) {
		case types.Message:
			return &msg
		case *types.Message:
			return msg
		}
	}
	if update.Error != nil {
		msg := createToolResultMessage(update.ToolUseID, update.Error.Error(), true)
		msg.Message = map[string]interface{}{"tool_name": update.ToolName}
		return &msg
	}
	if update.Result != nil {
		msg := createToolResultMessage(update.ToolUseID, toolResultContent(update.Result), false)
		msg.Message = map[string]interface{}{"tool_name": update.ToolName}
		return &msg
	}
	return nil
}

func createToolResultMessage(toolUseID, content string, isError bool) types.Message {
	return types.Message{
		Type:      types.MessageTypeUser,
		UUID:      types.UUID(),
		Timestamp: time.Now().UTC(),
		Content: []types.ContentBlock{{
			Type:      "tool_result",
			ToolUseID: toolUseID,
			Content:   content,
			IsError:   isError,
		}},
	}
}

func toolResultContent(result *tool.ToolResult) string {
	if result == nil || result.Data == nil {
		return ""
	}
	switch data := result.Data.(type) {
	case string:
		return data
	case fmt.Stringer:
		return data.String()
	default:
		encoded, err := json.Marshal(data)
		if err != nil {
			return fmt.Sprint(data)
		}
		return string(encoded)
	}
}

func messageText(msg types.Message) string {
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Text != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(block.Text)
		}
		if block.Content != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(block.Content)
		}
	}
	if b.Len() == 0 && msg.Message != nil {
		return fmt.Sprint(msg.Message)
	}
	return b.String()
}

func estimateMessageTokens(messages []types.Message) int {
	chars := 0
	for _, msg := range messages {
		chars += len(messageText(msg))
		for _, block := range msg.Content {
			if block.Name != "" {
				chars += len(block.Name)
			}
			if len(block.Input) > 0 {
				if encoded, err := json.Marshal(block.Input); err == nil {
					chars += len(encoded)
				}
			}
		}
	}
	if chars == 0 {
		return 0
	}
	return max(1, chars/4)
}

package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/tool"
	"github.com/ding/claude-code/claude-go/internal/public/types"
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
	if isTokenBudgetEnabled() {
		budgetTracker = createBudgetTracker()
	}

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
				toolUseContext.Options.Tools,
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
		for attemptWithFallback {
			attemptWithFallback = false

			modelParams := &ModelCallParams{
				Messages:       prependUserContext(messagesForQuery, userContext),
				SystemPrompt:   appendSystemContext(SystemPrompt, systemContext),
				Model:          currentModel,
				MaxTokens:      getMaxTokens(maxOutputTokensOverride),
				Tools:          nil, // TODO: Convert tool.Tool to types.Tool
				SkipCacheWrite: skipCacheWrite,
				TaskBudget:     buildTaskBudgetParam(params.TaskBudget, taskBudgetRemaining),
			}

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
							toolUseContext.Options.Tools,
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
				return Terminal{Reason: TerminalReasonStopHookPrevented}, nil
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
				decision := checkTokenBudget(
					budgetTracker,
					toolUseContext.AgentID,
					getCurrentTurnTokenBudget(),
					getTurnOutputTokens(),
				)

				if decision.Action == "continue" {
					incrementBudgetContinuationCount()

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

			return Terminal{Reason: TerminalReasonCompleted}, nil
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
			if update.Message != nil {
				eventChan <- update.Message

				// Type assert to check message type
				if msg, ok := update.Message.(types.Message); ok {
					if isHookStoppedContinuation(msg) {
						shouldPreventContinuation = true
					}

					// Add to tool results if it's a user message
					if msg.Type == "user" {
						toolResults = append(toolResults, msg)
					}
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
			return Terminal{Reason: TerminalReasonHookStopped}, nil
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

func isTokenBudgetEnabled() bool {
	// TODO: Implement feature flag check
	return false
}

func getQueuedCommands(messages []types.Message) []QueuedCommand {
	// TODO: Implement queued command retrieval
	return nil
}

func processQueuedCommands(messages []types.Message, commands []QueuedCommand) ([]types.Message, []types.Message) {
	// TODO: Implement command processing
	return messages, nil
}


func finalContextTokensFromLastResponse(messages []types.Message) int {
	// TODO: Implement token counting
	return 0
}

func getRuntimeMainLoopModel(ctx *tool.ToolUseContext, messages []types.Message) string {
	// TODO: Implement model selection
	return "claude-3-5-sonnet-20241022"
}

func shouldCheckBlockingLimit(compactionResult *CompactionResult, querySource string, snipTokensFreed int) bool {
	// TODO: Implement blocking limit check logic
	return false
}

func checkBlockingLimit(messages []types.Message, snipTokensFreed int, ctx *tool.ToolUseContext) bool {
	// TODO: Implement blocking limit check
	return false
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
	// TODO: Implement fallback error detection
	return false
}

func yieldMissingToolResultBlocks(assistantMessages []types.AssistantMessage, errorMessage string, eventChan chan<- interface{}) {
	// TODO: Implement missing tool result block generation
}

func stripSignatureBlocks(messages []types.Message) []types.Message {
	// TODO: Implement signature block stripping
	return messages
}

func createSystemMessage(content, level string) types.Message {
	// TODO: Implement system message creation
	return types.Message{}
}

func createUserInterruptionMessage(toolUse bool) types.Message {
	// TODO: Implement user interruption message creation
	return types.Message{}
}

func isWithheldPromptTooLong(msg *types.AssistantMessage) bool {
	// TODO: Implement prompt too long detection
	return false
}

func isMaxOutputTokensError(msg *types.AssistantMessage) bool {
	// TODO: Implement max output tokens error detection
	return false
}


func isHookStoppedContinuation(msg types.Message) bool {
	// TODO: Implement hook stopped continuation detection
	return false
}

func runTools(toolUseBlocks []types.ToolUseBlock, assistantMessages []types.AssistantMessage, canUseTool tool.CanUseToolFn, ctx *tool.ToolUseContext) <-chan *tool.Update {
	// TODO: Implement tool execution
	ch := make(chan *tool.Update)
	close(ch)
	return ch
}

func generateToolUseSummaryAsync(toolUseBlocks []types.ToolUseBlock, toolResults []types.Message, assistantMessages []types.AssistantMessage, ctx *tool.ToolUseContext) chan *types.ToolUseSummaryMessage {
	// TODO: Implement tool use summary generation
	ch := make(chan *types.ToolUseSummaryMessage, 1)
	close(ch)
	return ch
}

func createAttachmentMessage(msgType string, maxTurns, turnCount int) types.Message {
	// TODO: Implement attachment message creation
	return types.Message{}
}

func createAssistantAPIErrorMessage(content, errorType string) types.Message {
	// TODO: Implement API error message creation
	return types.Message{}
}

func createUserMessage(content string, isMeta bool) types.Message {
	// TODO: Implement user message creation
	return types.Message{}
}

func getCurrentTurnTokenBudget() *int {
	// TODO: Implement token budget retrieval
	return nil
}

func getTurnOutputTokens() int {
	// TODO: Implement output token counting
	return 0
}

func incrementBudgetContinuationCount() {
	// TODO: Implement budget continuation count increment
}

// QueuedCommand represents a queued command
type QueuedCommand struct {
	UUID    string
	Content string
}

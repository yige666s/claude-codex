package query

import (
	"fmt"
	"strings"

	"claude-codex/internal/public/types"
)

// handleMaxOutputTokensRecovery retries max_output_tokens errors with a larger
// output allowance. Model selection is deliberately unchanged: the runtime has
// no provider-independent way to infer a "larger" model.
func handleMaxOutputTokensRecovery(
	state *State,
	assistantMessages []types.AssistantMessage,
) (*State, bool) {
	lastMessage := getLastMessage(assistantMessages)
	if lastMessage == nil || !isMaxOutputTokensError(lastMessage) {
		return state, false
	}

	if state.MaxOutputTokensRecoveryCount >= MaxOutputTokensRecoveryLimit {
		return state, false
	}

	newState := *state
	newState.MaxOutputTokensRecoveryCount++
	currentMax := getMaxTokens(state.MaxOutputTokensOverride)
	newMax := currentMax + 4096
	newState.MaxOutputTokensOverride = &newMax
	newState.Transition = &Continue{Reason: ContinueReasonMaxOutputTokensRecovery}

	return &newState, true
}

// handlePromptTooLongRecovery handles recovery from prompt_too_long errors.
// It attempts reactive compaction to reduce the prompt size.
func handlePromptTooLongRecovery(
	state *State,
	assistantMessages []types.AssistantMessage,
) bool {
	lastMessage := getLastMessage(assistantMessages)
	if lastMessage == nil {
		return false
	}

	// Check if this is a withheld prompt-too-long error
	if !isWithheldPromptTooLong(lastMessage) {
		return false
	}

	// Only attempt reactive compaction once per turn
	if state.HasAttemptedReactiveCompact {
		return false
	}

	return true
}

// isWithheldMaxOutputTokens checks if a message is a withheld max_output_tokens error.
// These errors are withheld from SDK callers until we know whether recovery can continue.
func isWithheldMaxOutputTokens(msg *types.AssistantMessage) bool {
	if msg == nil || msg.Type != "assistant" {
		return false
	}
	return msg.APIError == "max_output_tokens"
}

// recoverFromImageError handles image size and resize errors.
func recoverFromImageError(err error) (types.Message, bool) {
	// Check if this is an image size or resize error
	if isImageSizeError(err) || isImageResizeError(err) {
		return createAssistantAPIErrorMessage(err.Error(), "image_error"), true
	}
	return types.Message{}, false
}

// isImageSizeError checks if an error is an image size error.
func isImageSizeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "image") &&
		(strings.Contains(msg, "size") ||
			strings.Contains(msg, "too large") ||
			strings.Contains(msg, "exceeds") ||
			strings.Contains(msg, "maximum"))
}

// isImageResizeError checks if an error is an image resize error.
func isImageResizeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "image") &&
		(strings.Contains(msg, "resize") || strings.Contains(msg, "rescale"))
}

// handleFallbackError handles model fallback during streaming.
func handleFallbackError(
	state *State,
	fallbackModel string,
	assistantMessages []types.AssistantMessage,
	toolResults []types.Message,
	streamingToolExecutor interface{},
) (*State, error) {
	if fallbackModel == "" {
		return nil, fmt.Errorf("no fallback model available")
	}

	// Clear state for retry
	newState := *state
	newState.ToolUseContext.Options.MainLoopModel = fallbackModel

	// Strip signature blocks for fallback
	newState.Messages = stripSignatureBlocks(state.Messages)

	return &newState, nil
}

// Thinking block preservation rules:
// 1. A message with thinking/redacted_thinking must be part of a query with max_thinking_length > 0
// 2. A thinking block may not be the last message in a block
// 3. Thinking blocks must be preserved for the duration of an assistant trajectory
//    (a single turn, or if that turn includes tool_use then also its subsequent tool_result
//    and the following assistant message)

// preserveThinkingBlocks ensures thinking blocks are properly preserved.
func preserveThinkingBlocks(messages []types.Message) []types.Message {
	out := cloneMessages(messages)
	for i := range out {
		out[i].Content = trimTrailingThinkingBlocks(out[i].Content)
	}
	return out
}

// stripThinkingBlocks removes thinking blocks when needed (e.g., for fallback models).
func stripThinkingBlocks(messages []types.Message) []types.Message {
	out := cloneMessages(messages)
	for i := range out {
		filtered := out[i].Content[:0]
		for _, block := range out[i].Content {
			if isThinkingBlock(block) {
				continue
			}
			filtered = append(filtered, block)
		}
		out[i].Content = filtered
	}
	return out
}

func trimTrailingThinkingBlocks(blocks []types.ContentBlock) []types.ContentBlock {
	end := len(blocks)
	for end > 0 && isThinkingBlock(blocks[end-1]) {
		end--
	}
	return append([]types.ContentBlock(nil), blocks[:end]...)
}

func isThinkingBlock(block types.ContentBlock) bool {
	return block.Type == "thinking" || block.Type == "redacted_thinking"
}

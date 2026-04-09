package compact

import (
	"time"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// MicrocompactMessages removes old tool results to reduce context size.
// This is more sophisticated than snip - it selectively removes entire tool results
// based on age and importance, rather than just truncating them.
func MicrocompactMessages(messages []types.Message, options *MicrocompactOptions) *MicrocompactResult {
	if options == nil {
		options = DefaultMicrocompactOptions()
	}

	// Collect compactable tool IDs
	compactableIDs := collectCompactableToolIDs(messages)

	if len(compactableIDs) == 0 {
		return &MicrocompactResult{
			Messages:       messages,
			DeletedToolIDs: []string{},
			TokensSaved:    0,
		}
	}

	// Time-based microcompact: clear old tool results
	if options.TimeBasedEnabled {
		return timeBasedMicrocompact(messages, compactableIDs, options)
	}

	// Regular microcompact: remove tool results based on strategy
	return regularMicrocompact(messages, compactableIDs, options)
}

// MicrocompactOptions configures microcompaction behavior.
type MicrocompactOptions struct {
	// TimeBasedEnabled enables time-based clearing
	TimeBasedEnabled bool

	// TimeThresholdMinutes is the age threshold for clearing
	TimeThresholdMinutes int

	// ClearMessage replaces cleared content
	ClearMessage string

	// MaxToolResultsToKeep limits how many recent results to preserve
	MaxToolResultsToKeep int

	// QuerySource identifies the query context
	QuerySource string
}

// DefaultMicrocompactOptions returns default microcompact options.
func DefaultMicrocompactOptions() *MicrocompactOptions {
	return &MicrocompactOptions{
		TimeBasedEnabled:     true,
		TimeThresholdMinutes: 5,
		ClearMessage:         "[Old tool result content cleared]",
		MaxToolResultsToKeep: 10,
		QuerySource:          "",
	}
}

// collectCompactableToolIDs collects tool_use IDs for compactable tools.
func collectCompactableToolIDs(messages []types.Message) []string {
	var ids []string

	for _, msg := range messages {
		if msg.Type != types.MessageTypeAssistant {
			continue
		}

		for _, block := range msg.Content {
			if block.Type == "tool_use" && IsCompactable(block.Name) {
				ids = append(ids, block.ID)
			}
		}
	}

	return ids
}

// timeBasedMicrocompact clears tool results older than the threshold.
func timeBasedMicrocompact(messages []types.Message, compactableIDs []string, options *MicrocompactOptions) *MicrocompactResult {
	now := time.Now()
	threshold := time.Duration(options.TimeThresholdMinutes) * time.Minute

	// Find the last assistant message to calculate time gap
	var lastAssistantTime time.Time
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type == types.MessageTypeAssistant {
			lastAssistantTime = messages[i].Timestamp
			break
		}
	}

	// If no assistant message or gap is small, don't compact
	if lastAssistantTime.IsZero() || now.Sub(lastAssistantTime) < threshold {
		return &MicrocompactResult{
			Messages:       messages,
			DeletedToolIDs: []string{},
			TokensSaved:    0,
		}
	}

	// Create a set of compactable IDs for fast lookup
	compactableSet := make(map[string]bool)
	for _, id := range compactableIDs {
		compactableSet[id] = true
	}

	// Clear old tool results
	result := make([]types.Message, 0, len(messages))
	deletedIDs := []string{}
	tokensSaved := 0

	for _, msg := range messages {
		if msg.Type != types.MessageTypeUser {
			result = append(result, msg)
			continue
		}

		// Check if this message has tool results to clear
		hasOldToolResult := false
		newContent := make([]types.ContentBlock, 0, len(msg.Content))

		for _, block := range msg.Content {
			if block.Type == "tool_result" && compactableSet[block.ToolUseID] {
				// Clear the content
				hasOldToolResult = true
				deletedIDs = append(deletedIDs, block.ToolUseID)
				tokensSaved += len(block.Content) / 4 // Rough estimate

				clearedBlock := block
				clearedBlock.Content = options.ClearMessage
				newContent = append(newContent, clearedBlock)
			} else {
				newContent = append(newContent, block)
			}
		}

		if hasOldToolResult {
			newMsg := msg
			newMsg.Content = newContent
			result = append(result, newMsg)
		} else {
			result = append(result, msg)
		}
	}

	return &MicrocompactResult{
		Messages:       result,
		DeletedToolIDs: deletedIDs,
		TokensSaved:    tokensSaved,
	}
}

// regularMicrocompact removes tool results based on recency.
func regularMicrocompact(messages []types.Message, compactableIDs []string, options *MicrocompactOptions) *MicrocompactResult {
	// Keep only the most recent N tool results
	keepCount := options.MaxToolResultsToKeep
	if keepCount <= 0 {
		keepCount = 10
	}

	// Determine which IDs to keep (most recent ones)
	keepIDs := make(map[string]bool)
	if len(compactableIDs) <= keepCount {
		// Keep all
		for _, id := range compactableIDs {
			keepIDs[id] = true
		}
	} else {
		// Keep only the last N
		startIdx := len(compactableIDs) - keepCount
		for i := startIdx; i < len(compactableIDs); i++ {
			keepIDs[compactableIDs[i]] = true
		}
	}

	// Remove tool results not in keepIDs
	result := make([]types.Message, 0, len(messages))
	deletedIDs := []string{}
	tokensSaved := 0

	for _, msg := range messages {
		if msg.Type != types.MessageTypeUser {
			result = append(result, msg)
			continue
		}

		hasRemovedToolResult := false
		newContent := make([]types.ContentBlock, 0, len(msg.Content))

		for _, block := range msg.Content {
			if block.Type == "tool_result" && !keepIDs[block.ToolUseID] {
				// Remove this tool result
				hasRemovedToolResult = true
				deletedIDs = append(deletedIDs, block.ToolUseID)
				tokensSaved += len(block.Content) / 4

				// Replace with cleared message
				clearedBlock := block
				clearedBlock.Content = options.ClearMessage
				newContent = append(newContent, clearedBlock)
			} else {
				newContent = append(newContent, block)
			}
		}

		if hasRemovedToolResult {
			newMsg := msg
			newMsg.Content = newContent
			result = append(result, newMsg)
		} else {
			result = append(result, msg)
		}
	}

	return &MicrocompactResult{
		Messages:       result,
		DeletedToolIDs: deletedIDs,
		TokensSaved:    tokensSaved,
	}
}

// ShouldTriggerMicrocompact checks if microcompaction should be triggered.
func ShouldTriggerMicrocompact(messages []types.Message, options *MicrocompactOptions) bool {
	if options == nil {
		options = DefaultMicrocompactOptions()
	}

	// Count compactable tool results
	compactableIDs := collectCompactableToolIDs(messages)
	if len(compactableIDs) == 0 {
		return false
	}

	// If time-based is enabled, check time gap
	if options.TimeBasedEnabled {
		now := time.Now()
		threshold := time.Duration(options.TimeThresholdMinutes) * time.Minute

		var lastAssistantTime time.Time
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Type == types.MessageTypeAssistant {
				lastAssistantTime = messages[i].Timestamp
				break
			}
		}

		if !lastAssistantTime.IsZero() && now.Sub(lastAssistantTime) >= threshold {
			return true
		}
	}

	// Check if we have more tool results than we want to keep
	return len(compactableIDs) > options.MaxToolResultsToKeep
}

// EstimateMicrocompactSavings estimates how many tokens would be saved.
func EstimateMicrocompactSavings(messages []types.Message, options *MicrocompactOptions) int {
	if options == nil {
		options = DefaultMicrocompactOptions()
	}

	compactableIDs := collectCompactableToolIDs(messages)
	if len(compactableIDs) <= options.MaxToolResultsToKeep {
		return 0
	}

	// Count tokens in tool results that would be removed
	removeCount := len(compactableIDs) - options.MaxToolResultsToKeep
	removeIDs := make(map[string]bool)
	for i := 0; i < removeCount; i++ {
		removeIDs[compactableIDs[i]] = true
	}

	tokensSaved := 0
	for _, msg := range messages {
		if msg.Type != types.MessageTypeUser {
			continue
		}

		for _, block := range msg.Content {
			if block.Type == "tool_result" && removeIDs[block.ToolUseID] {
				tokensSaved += len(block.Content) / 4
			}
		}
	}

	return tokensSaved
}

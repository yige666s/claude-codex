package compact

import (
	"os"
	"strings"

	"claude-codex/internal/harness/memory"
	"claude-codex/internal/public/types"
)

// SessionMemoryCompactConfig configures session memory compaction thresholds.
type SessionMemoryCompactConfig struct {
	// MinTokens is the minimum tokens to preserve after compaction.
	MinTokens int
	// MinTextBlockMessages is the minimum message count with text blocks to keep.
	MinTextBlockMessages int
	// MaxTokens is the hard cap — stop expanding backwards once reached.
	MaxTokens int
}

// DefaultSMCompactConfig mirrors DEFAULT_SM_COMPACT_CONFIG in sessionMemoryCompact.ts.
var DefaultSMCompactConfig = SessionMemoryCompactConfig{
	MinTokens:            10_000,
	MinTextBlockMessages: 5,
	MaxTokens:            40_000,
}

// ShouldUseSessionMemoryCompaction returns true when session-memory-based
// compaction is enabled. Controlled by env vars (no GrowthBook in Go).
//
// Enable:  ENABLE_CLAUDE_CODE_SM_COMPACT=true
// Disable: DISABLE_CLAUDE_CODE_SM_COMPACT=true  (takes precedence)
func ShouldUseSessionMemoryCompaction() bool {
	if v := os.Getenv("DISABLE_CLAUDE_CODE_SM_COMPACT"); v == "true" || v == "1" {
		return false
	}
	v := os.Getenv("ENABLE_CLAUDE_CODE_SM_COMPACT")
	return v == "true" || v == "1"
}

// HasTextBlocks reports whether msg contains at least one text content block.
func HasTextBlocks(msg types.Message) bool {
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			return true
		}
	}
	return false
}

// isCompactBoundaryMsg returns true if the message is a compact boundary marker.
func isCompactBoundaryMsg(msg types.Message) bool {
	return msg.Subtype == "compact_boundary"
}

// estimateMsgTokens estimates token count for a slice of messages
// using the 4-chars-per-token heuristic.
func estimateMsgTokens(messages []types.Message) int {
	total := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			total += (len(block.Text) + 3) / 4
		}
	}
	return total
}

// AdjustIndexToPreserveAPIInvariants adjusts startIndex backwards so that:
//  1. tool_use / tool_result pairs are never split across the boundary.
//  2. assistant messages sharing a message ID (streaming thinking blocks) are
//     not orphaned.
//
// Mirrors adjustIndexToPreserveAPIInvariants in sessionMemoryCompact.ts.
func AdjustIndexToPreserveAPIInvariants(messages []types.Message, startIndex int) int {
	if startIndex <= 0 || startIndex >= len(messages) {
		return startIndex
	}

	adjusted := startIndex

	// Collect tool_result IDs from ALL kept messages.
	var allToolResultIDs []string
	for i := startIndex; i < len(messages); i++ {
		for _, block := range messages[i].Content {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				allToolResultIDs = append(allToolResultIDs, block.ToolUseID)
			}
		}
	}

	if len(allToolResultIDs) > 0 {
		// Build set of tool_use IDs already present in kept range.
		keptToolUseIDs := make(map[string]bool)
		for i := adjusted; i < len(messages); i++ {
			for _, block := range messages[i].Content {
				if block.Type == "tool_use" && block.ID != "" {
					keptToolUseIDs[block.ID] = true
				}
			}
		}

		// Build set of IDs that are still missing.
		needed := make(map[string]bool)
		for _, id := range allToolResultIDs {
			if !keptToolUseIDs[id] {
				needed[id] = true
			}
		}

		// Walk backwards to find the matching tool_use messages.
		for i := adjusted - 1; i >= 0 && len(needed) > 0; i-- {
			msg := messages[i]
			if msg.Type != types.MessageTypeAssistant {
				continue
			}
			for _, block := range msg.Content {
				if block.Type == "tool_use" && needed[block.ID] {
					adjusted = i
					delete(needed, block.ID)
				}
			}
		}
	}

	// Collect message IDs of assistant messages in kept range (for thinking blocks).
	keptMsgIDs := make(map[string]bool)
	for i := adjusted; i < len(messages); i++ {
		if messages[i].Type == types.MessageTypeAssistant && messages[i].UUID != "" {
			keptMsgIDs[messages[i].UUID] = true
		}
	}

	// Walk backwards to include any prior assistant messages with the same ID
	// (streaming produces separate messages for thinking vs tool_use).
	for i := adjusted - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Type == types.MessageTypeAssistant && msg.UUID != "" && keptMsgIDs[msg.UUID] {
			adjusted = i
		}
	}

	return adjusted
}

// CalculateMessagesToKeepIndex calculates the starting index of messages to
// preserve after session-memory compaction.
//
// It starts at lastSummarizedIndex+1 and expands backwards until:
//   - MinTokens AND MinTextBlockMessages are both met, or
//   - MaxTokens is hit (hard cap).
//
// It also respects compact boundary markers as a floor (matches TypeScript
// behaviour: never expand past the last compact boundary).
//
// Mirrors calculateMessagesToKeepIndex in sessionMemoryCompact.ts.
func CalculateMessagesToKeepIndex(
	messages []types.Message,
	lastSummarizedIndex int,
	cfg SessionMemoryCompactConfig,
) int {
	if len(messages) == 0 {
		return 0
	}

	// startIndex is the first message NOT yet summarised.
	startIndex := lastSummarizedIndex + 1
	if lastSummarizedIndex < 0 {
		startIndex = len(messages) // nothing kept initially
	}

	totalTokens := estimateMsgTokens(messages[startIndex:])
	textBlockCount := 0
	for i := startIndex; i < len(messages); i++ {
		if HasTextBlocks(messages[i]) {
			textBlockCount++
		}
	}

	// Already over the hard cap — trim, don't expand.
	if totalTokens >= cfg.MaxTokens {
		return AdjustIndexToPreserveAPIInvariants(messages, startIndex)
	}

	// Already meets both minimums — no need to expand.
	if totalTokens >= cfg.MinTokens && textBlockCount >= cfg.MinTextBlockMessages {
		return AdjustIndexToPreserveAPIInvariants(messages, startIndex)
	}

	// Find the floor: never expand past the last compact boundary.
	floor := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if isCompactBoundaryMsg(messages[i]) {
			floor = i + 1
			break
		}
	}

	// Expand backwards to meet minimums.
	for i := startIndex - 1; i >= floor; i-- {
		msgTokens := estimateMsgTokens(messages[i : i+1])
		totalTokens += msgTokens
		if HasTextBlocks(messages[i]) {
			textBlockCount++
		}
		startIndex = i

		if totalTokens >= cfg.MaxTokens {
			break
		}
		if totalTokens >= cfg.MinTokens && textBlockCount >= cfg.MinTextBlockMessages {
			break
		}
	}

	return AdjustIndexToPreserveAPIInvariants(messages, startIndex)
}

// TrySessionMemoryCompaction attempts to compact messages using the session
// memory summary instead of a fresh API summarisation call.
//
// Returns (nil, nil) when the preconditions are not met (compaction disabled,
// no session memory content, empty template, ID not found).
// Returns (result, nil) on success, (nil, err) on unexpected error.
//
// Mirrors trySessionMemoryCompaction in sessionMemoryCompact.ts.
func TrySessionMemoryCompaction(
	messages []types.Message,
	sm *memory.SessionMemory,
	cfg SessionMemoryCompactConfig,
	autoCompactThreshold int, // 0 means no threshold check
) (*CompactionResult, error) {
	if !ShouldUseSessionMemoryCompaction() {
		return nil, nil
	}

	if sm == nil {
		return nil, nil
	}

	// Wait for any in-progress extraction to finish.
	sm.WaitForExtraction(memory.DefaultExtractionWaitTimeout, memory.DefaultExtractionStaleThreshold)

	content, err := sm.LoadContent()
	if err != nil || content == "" {
		return nil, nil
	}
	if sm.IsEmpty() {
		return nil, nil
	}

	// Find cursor: the last message ID that has already been summarised.
	lastSummarizedID := sm.GetLastSummarizedMessageID()

	var lastSummarizedIndex int
	if lastSummarizedID != "" {
		lastSummarizedIndex = -1
		for i, msg := range messages {
			if msg.UUID == lastSummarizedID {
				lastSummarizedIndex = i
				break
			}
		}
		if lastSummarizedIndex == -1 {
			// ID not found — can't determine boundary, fall back.
			return nil, nil
		}
	} else {
		// Resumed session: treat all messages as summarised, expand from end.
		lastSummarizedIndex = len(messages) - 1
	}

	startIndex := CalculateMessagesToKeepIndex(messages, lastSummarizedIndex, cfg)

	// Drop old compact boundary markers from the kept slice.
	kept := make([]types.Message, 0, len(messages)-startIndex)
	for _, msg := range messages[startIndex:] {
		if !isCompactBoundaryMsg(msg) {
			kept = append(kept, msg)
		}
	}

	// Build the summary user message (session memory content prepended).
	// Truncate oversized sections before embedding in the summary message.
	// 12_000 tokens ≈ reasonable budget; mirrors truncateSessionMemoryForCompact.
	truncated, _ := sm.TruncateForCompact(12_000)
	summaryLines := []string{
		"<session-memory>",
		truncated,
		"</session-memory>",
		"",
		"The above is a summary of the session so far. Continue from where we left off.",
	}
	summaryMsg := types.Message{
		Type:             types.MessageTypeUser,
		IsMeta:           true,
		IsCompactSummary: true,
		Content: []types.ContentBlock{
			{Type: "text", Text: strings.Join(summaryLines, "\n")},
		},
	}

	// Boundary marker.
	boundaryMsg := types.Message{
		Type:    types.MessageTypeSystem,
		Subtype: "compact_boundary",
		Content: []types.ContentBlock{
			{Type: "text", Text: "[Session memory compaction boundary]"},
		},
	}

	result := append([]types.Message{boundaryMsg, summaryMsg}, kept...)

	// Threshold guard: if the compacted result is still too large, bail out.
	if autoCompactThreshold > 0 {
		postTokens := estimateMsgTokens(result)
		if postTokens >= autoCompactThreshold {
			return nil, nil
		}
	}

	compacted := len(messages) - len(result)
	if compacted < 0 {
		compacted = 0
	}

	return &CompactionResult{
		Messages:       result,
		CompactedCount: compacted,
		TokensSaved:    estimateMsgTokens(messages) - estimateMsgTokens(result),
	}, nil
}

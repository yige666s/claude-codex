package state

import (
	"encoding/json"
	"strings"
)

// CompressionConfig defines thresholds for context compression
type CompressionConfig struct {
	// MaxTokens is the soft limit before compression kicks in
	MaxTokens int
	// TargetTokens is the target size after compression
	TargetTokens int
	// PreserveRecent is the number of recent messages to always keep
	PreserveRecent int
}

// DefaultCompressionConfig returns sensible defaults
func DefaultCompressionConfig() CompressionConfig {
	return CompressionConfig{
		MaxTokens:      180000, // Start compressing at 180k tokens (leave room for response)
		TargetTokens:   120000, // Compress down to 120k tokens
		PreserveRecent: 4,      // Always keep last 4 messages (2 turns)
	}
}

// EstimateTokens estimates token count for a message
func (m *Message) EstimateTokens() int {
	total := 0

	// Content
	if m.Content != "" {
		total += estimateTokens(m.Content)
	}

	// Tool input/output
	if len(m.ToolInput) > 0 {
		total += len(m.ToolInput) / 4
	}
	if m.ToolOutput != "" {
		total += estimateTokens(m.ToolOutput)
	}

	// Overhead for message structure
	total += 10

	return total
}

// EstimateTokens estimates total token count for the session
func (s *Session) EstimateTokens() int {
	total := 0
	for _, msg := range s.Messages {
		total += msg.EstimateTokens()
	}
	return total
}

// NeedsCompression checks if the session should be compressed
func (s *Session) NeedsCompression(config CompressionConfig) bool {
	return s.EstimateTokens() > config.MaxTokens
}

// Compress reduces the session size by summarizing or removing old messages
func (s *Session) Compress(config CompressionConfig) error {
	currentTokens := s.EstimateTokens()
	if currentTokens <= config.MaxTokens {
		return nil // No compression needed
	}

	// Calculate how many tokens we need to remove
	tokensToRemove := currentTokens - config.TargetTokens
	if tokensToRemove <= 0 {
		return nil
	}

	// Preserve recent messages
	preserveCount := config.PreserveRecent
	if preserveCount > len(s.Messages) {
		preserveCount = len(s.Messages)
	}

	// Find the split point: compress old messages, keep recent ones
	compressibleEnd := len(s.Messages) - preserveCount
	if compressibleEnd <= 0 {
		return nil // Nothing to compress
	}

	// Strategy: Remove tool results and truncate long assistant messages
	var compressed []Message
	tokensRemoved := 0

	for i := 0; i < compressibleEnd && tokensRemoved < tokensToRemove; i++ {
		msg := s.Messages[i]
		msgTokens := msg.EstimateTokens()

		// Skip tool messages entirely (they're often verbose)
		if msg.Role == "tool" {
			tokensRemoved += msgTokens
			continue
		}

		// Truncate long assistant messages
		if msg.Role == "assistant" && len(msg.Content) > 1000 {
			truncated := msg
			truncated.Content = truncateContent(msg.Content, 500)
			tokensRemoved += msgTokens - truncated.EstimateTokens()
			compressed = append(compressed, truncated)
			continue
		}

		// Keep user messages and short assistant messages
		compressed = append(compressed, msg)
	}

	// Add a marker message to indicate compression occurred
	if tokensRemoved > 0 {
		compressed = append(compressed, Message{
			Role:    "user",
			Content: "[Previous conversation context was compressed to save tokens]",
			Hidden:  true,
		})
	}

	// Append the preserved recent messages
	compressed = append(compressed, s.Messages[compressibleEnd:]...)

	s.Messages = compressed
	return nil
}

// truncateContent truncates text to approximately maxChars, preserving sentence boundaries
func truncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}

	truncated := content[:maxChars]

	// Try to break at sentence boundary
	if idx := strings.LastIndexAny(truncated, ".!?"); idx > maxChars/2 {
		truncated = truncated[:idx+1]
	}

	return truncated + "\n[... truncated ...]"
}

// CompressToolOutput reduces verbose tool output
func CompressToolOutput(output string, maxLength int) string {
	if len(output) <= maxLength {
		return output
	}

	// For structured output (JSON), try to preserve structure
	if strings.HasPrefix(strings.TrimSpace(output), "{") || strings.HasPrefix(strings.TrimSpace(output), "[") {
		var data interface{}
		if err := json.Unmarshal([]byte(output), &data); err == nil {
			// It's valid JSON, just truncate with indicator
			return output[:maxLength] + "\n[... JSON truncated ...]"
		}
	}

	// For line-based output (logs, file contents), keep first and last portions
	lines := strings.Split(output, "\n")
	if len(lines) > 20 {
		keepLines := 10
		head := strings.Join(lines[:keepLines], "\n")
		tail := strings.Join(lines[len(lines)-keepLines:], "\n")
		return head + "\n\n[... " + string(rune(len(lines)-2*keepLines)) + " lines omitted ...]\n\n" + tail
	}

	// Simple truncation
	return output[:maxLength] + "\n[... truncated ...]"
}

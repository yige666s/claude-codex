package prefetch

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryPrefetcher handles asynchronous memory relevance search.
type MemoryPrefetcher struct {
	config *PrefetchConfig
}

// NewMemoryPrefetcher creates a new memory prefetcher.
func NewMemoryPrefetcher(config *PrefetchConfig) *MemoryPrefetcher {
	if config == nil {
		config = DefaultPrefetchConfig()
	}
	return &MemoryPrefetcher{
		config: config,
	}
}

// StartRelevantMemoryPrefetch starts the relevant memory search as an async prefetch.
// Extracts the last real user prompt from messages and kicks off a non-blocking search.
// Returns a handle with settlement tracking.
func (m *MemoryPrefetcher) StartRelevantMemoryPrefetch(
	ctx context.Context,
	messages []Message,
	surfacedBytes int,
	readFileState map[string]bool,
) *MemoryPrefetch {
	if !m.config.Enabled {
		return nil
	}

	// Find last user message
	lastUserMessage := findLastUserMessage(messages)
	if lastUserMessage == nil {
		return nil
	}

	input := getUserMessageText(lastUserMessage)
	// Single-word prompts lack enough context for meaningful term extraction
	if input == "" || !strings.Contains(strings.TrimSpace(input), " ") {
		return nil
	}

	// Check if we've already surfaced too many memories
	if surfacedBytes >= m.config.MaxSessionBytes {
		return nil
	}

	// Create child context with timeout
	ctx, cancel := context.WithTimeout(ctx, m.config.Timeout)

	// Create result channel
	resultChan := make(chan []MemoryAttachment, 1)

	prefetch := &MemoryPrefetch{
		ResultChan:          resultChan,
		SettledAt:           nil,
		ConsumedOnIteration: -1,
		Error:               nil,
		cancel:              cancel,
		firedAt:             time.Now(),
	}

	// Start async search
	go m.searchRelevantMemories(ctx, input, readFileState, resultChan, prefetch)

	return prefetch
}

// searchRelevantMemories performs the actual memory search asynchronously.
func (m *MemoryPrefetcher) searchRelevantMemories(
	ctx context.Context,
	input string,
	readFileState map[string]bool,
	resultChan chan<- []MemoryAttachment,
	prefetch *MemoryPrefetch,
) {
	defer func() {
		now := time.Now()
		prefetch.SettledAt = &now
		close(resultChan)
	}()

	// Extract search terms from input
	terms := extractSearchTerms(input)
	if len(terms) == 0 {
		resultChan <- []MemoryAttachment{}
		return
	}

	// Search for relevant memory files
	memories, err := m.findRelevantMemories(ctx, terms, readFileState)
	if err != nil {
		prefetch.Error = err
		resultChan <- []MemoryAttachment{}
		return
	}

	resultChan <- memories
}

// findRelevantMemories searches for memory files matching the search terms.
func (m *MemoryPrefetcher) findRelevantMemories(
	ctx context.Context,
	_ []string, // terms - TODO: use for actual search
	_ map[string]bool, // readFileState - TODO: use for filtering
) ([]MemoryAttachment, error) {
	// TODO: Implement actual memory search logic
	// This is a placeholder that would integrate with:
	// - File system search
	// - Memory file parsing
	// - Relevance scoring
	// - Filtering by readFileState

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Placeholder: return empty results
		return []MemoryAttachment{}, nil
	}
}

// Message represents a conversation message.
type Message struct {
	Type   string // "user" or "assistant"
	IsMeta bool
	Text   string
}

// findLastUserMessage finds the last non-meta user message.
func findLastUserMessage(messages []Message) *Message {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := &messages[i]
		if msg.Type == "user" && !msg.IsMeta {
			return msg
		}
	}
	return nil
}

// getUserMessageText extracts text from a user message.
func getUserMessageText(msg *Message) string {
	if msg == nil {
		return ""
	}
	return msg.Text
}

// extractSearchTerms extracts meaningful search terms from input text.
func extractSearchTerms(input string) []string {
	// Simple implementation: split by whitespace and filter short words
	words := strings.Fields(input)
	terms := make([]string, 0, len(words))

	for _, word := range words {
		// Filter out very short words and common stop words
		if len(word) >= 3 && !isStopWord(word) {
			terms = append(terms, strings.ToLower(word))
		}
	}

	return terms
}

// isStopWord checks if a word is a common stop word.
func isStopWord(word string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "are": true,
		"but": true, "not": true, "you": true, "all": true,
		"can": true, "her": true, "was": true, "one": true,
		"our": true, "out": true, "day": true, "get": true,
	}
	return stopWords[strings.ToLower(word)]
}

// CollectSurfacedMemories calculates total bytes of memories already surfaced.
func CollectSurfacedMemories(messages []Message) int {
	// TODO: Implement actual calculation
	// This would scan messages for memory attachments and sum their sizes
	return 0
}

// FilterDuplicateMemoryAttachments filters out memories that have already been read.
func FilterDuplicateMemoryAttachments(
	attachments []MemoryAttachment,
	readFileState map[string]bool,
) []MemoryAttachment {
	filtered := make([]MemoryAttachment, 0, len(attachments))
	for _, att := range attachments {
		if !readFileState[att.Path] {
			filtered = append(filtered, att)
		}
	}
	return filtered
}

// CreateMemoryAttachmentMessage creates a message from a memory attachment.
func CreateMemoryAttachmentMessage(att MemoryAttachment) Message {
	description := fmt.Sprintf("Memory: %s", att.Path)
	if att.MtimeMs > 0 {
		age := time.Since(time.UnixMilli(att.MtimeMs))
		description = fmt.Sprintf("Memory (saved %s ago): %s", formatDuration(age), att.Path)
	}

	return Message{
		Type:   "user",
		IsMeta: true,
		Text:   fmt.Sprintf("%s\n\n%s", description, att.Content),
	}
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

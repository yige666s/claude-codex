package prefetch

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"claude-codex/internal/harness/memdir"
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
	memoryDir string,
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
	terms := extractSearchTerms(input)
	if len(terms) == 0 || strings.TrimSpace(memoryDir) == "" {
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
		consumedOnIteration: -1,
		cancel:              cancel,
		firedAt:             time.Now(),
	}
	readSnapshot := make(map[string]bool, len(readFileState))
	for path, read := range readFileState {
		readSnapshot[path] = read
	}

	// Start async search
	go m.searchRelevantMemories(ctx, terms, memoryDir, m.config.MaxSessionBytes-surfacedBytes, readSnapshot, resultChan, prefetch)

	return prefetch
}

// searchRelevantMemories performs the actual memory search asynchronously.
func (m *MemoryPrefetcher) searchRelevantMemories(
	ctx context.Context,
	terms []string,
	memoryDir string,
	remainingBytes int,
	readFileState map[string]bool,
	resultChan chan<- []MemoryAttachment,
	prefetch *MemoryPrefetch,
) {
	var searchErr error
	defer func() {
		prefetch.settle(searchErr)
		close(resultChan)
	}()

	// Search for relevant memory files
	memories, err := m.findRelevantMemories(ctx, terms, memoryDir, remainingBytes, readFileState)
	if err != nil {
		searchErr = err
		resultChan <- []MemoryAttachment{}
		return
	}

	resultChan <- memories
}

// findRelevantMemories searches for memory files matching the search terms.
func (m *MemoryPrefetcher) findRelevantMemories(
	ctx context.Context,
	terms []string,
	memoryDir string,
	remainingBytes int,
	readFileState map[string]bool,
) ([]MemoryAttachment, error) {
	if remainingBytes <= 0 || strings.TrimSpace(memoryDir) == "" {
		return nil, nil
	}
	headers, err := memdir.ScanMemoryFiles(memoryDir, ctx)
	if err != nil {
		return nil, err
	}
	type scoredMemory struct {
		header memdir.MemoryHeader
		score  int
	}
	scored := make([]scoredMemory, 0, len(headers))
	for _, header := range headers {
		if readFileState[header.FilePath] || readFileState[header.Filename] {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{header.Filename, header.Description, header.Type}, " "))
		score := 0
		for _, term := range terms {
			if count := strings.Count(haystack, term); count > 0 {
				score += count
				if strings.Contains(strings.ToLower(header.Filename), term) {
					score += 2
				}
			}
		}
		if score > 0 {
			scored = append(scored, scoredMemory{header: header, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].header.MtimeMs > scored[j].header.MtimeMs
	})
	if len(scored) > 5 {
		scored = scored[:5]
	}

	attachments := make([]MemoryAttachment, 0, len(scored))
	for _, candidate := range scored {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if remainingBytes <= 0 {
			break
		}
		file, err := os.Open(candidate.header.FilePath)
		if err != nil {
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(file, int64(remainingBytes)))
		closeErr := file.Close()
		if readErr != nil {
			continue
		}
		if closeErr != nil {
			return nil, closeErr
		}
		content = trimIncompleteUTF8(content)
		if len(content) == 0 {
			continue
		}
		attachments = append(attachments, MemoryAttachment{
			Path:        candidate.header.FilePath,
			Content:     string(content),
			Type:        firstNonEmpty(candidate.header.Type, "memory"),
			Description: candidate.header.Description,
			MtimeMs:     candidate.header.MtimeMs,
		})
		remainingBytes -= len(content)
	}
	return attachments, nil
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
	words := strings.FieldsFunc(input, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	})
	terms := make([]string, 0, len(words))
	seen := make(map[string]struct{}, len(words))

	for _, word := range words {
		word = strings.ToLower(strings.TrimSpace(word))
		if utf8.RuneCountInString(word) >= 2 && !isStopWord(word) {
			if _, ok := seen[word]; !ok {
				seen[word] = struct{}{}
				terms = append(terms, word)
			}
		}
	}

	return terms
}

func trimIncompleteUTF8(content []byte) []byte {
	for len(content) > 0 && !utf8.Valid(content) {
		content = content[:len(content)-1]
	}
	return content
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	total := 0
	for _, message := range messages {
		if message.IsMeta && message.Type == "user" && strings.HasPrefix(strings.TrimSpace(message.Text), "Memory") {
			total += len(message.Text)
		}
	}
	return total
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

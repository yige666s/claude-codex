package query

import (
	"strings"
	"sync"

	"claude-codex/internal/harness/prefetch"
	"claude-codex/internal/public/types"
)

var (
	globalMemoryPrefetcher     *prefetch.MemoryPrefetcher
	globalMemoryPrefetcherOnce sync.Once
)

// MemoryPrefetch is an alias for prefetch.MemoryPrefetch
type MemoryPrefetch = prefetch.MemoryPrefetch

// getMemoryPrefetcher returns the global memory prefetcher instance.
func getMemoryPrefetcher() *prefetch.MemoryPrefetcher {
	globalMemoryPrefetcherOnce.Do(func() {
		globalMemoryPrefetcher = prefetch.NewMemoryPrefetcher(nil)
	})
	return globalMemoryPrefetcher
}

// collectSurfacedMemoryBytes calculates the total bytes of memory attachments
// already surfaced in the conversation.
func collectSurfacedMemoryBytes(messages []types.Message) int {
	total := 0
	for _, message := range messages {
		if !message.IsMeta || message.Type != types.MessageTypeUser {
			continue
		}
		for _, block := range message.Content {
			text := block.Text
			if text == "" {
				text = block.Content
			}
			if strings.HasPrefix(strings.TrimSpace(text), "Memory") {
				total += len(text)
			}
		}
	}
	return total
}

// convertMessagesToPrefetchMessages converts query messages to prefetch messages.
func convertMessagesToPrefetchMessages(messages []types.Message) []prefetch.Message {
	result := make([]prefetch.Message, 0, len(messages))
	for _, msg := range messages {
		prefetchMsg := prefetch.Message{
			Type:   string(msg.Type),
			IsMeta: msg.IsMeta,
		}

		// Extract text from content blocks
		for _, block := range msg.Content {
			if block.Type == "text" {
				prefetchMsg.Text += block.Text
			}
		}

		result = append(result, prefetchMsg)
	}
	return result
}

// consumeMemoryPrefetch consumes the memory prefetch results if settled.
// Returns memory attachment messages to be appended to tool results.
func consumeMemoryPrefetch(
	pendingMemoryPrefetch *MemoryPrefetch,
	readFileState map[string]bool,
	turnCount int,
) []types.Message {
	if pendingMemoryPrefetch == nil {
		return nil
	}

	// Only consume if settled and not already consumed
	if !pendingMemoryPrefetch.IsSettled() || pendingMemoryPrefetch.IsConsumed() {
		return nil
	}

	// Non-blocking receive
	select {
	case results, ok := <-pendingMemoryPrefetch.ResultChan:
		if !ok {
			return nil
		}

		filtered := prefetch.FilterDuplicateMemoryAttachments(results, readFileState)

		messages := make([]types.Message, 0, len(filtered))
		for _, att := range filtered {
			prefetchMsg := prefetch.CreateMemoryAttachmentMessage(att)

			msg := types.Message{
				Type:   types.MessageTypeUser,
				IsMeta: prefetchMsg.IsMeta,
				Content: []types.ContentBlock{
					{Type: "text", Text: prefetchMsg.Text},
				},
			}
			messages = append(messages, msg)

			// Mark path as read so it won't be surfaced again
			readFileState[att.Path] = true
		}

		pendingMemoryPrefetch.MarkConsumed(turnCount - 1)
		return messages

	default:
		// Not settled yet, skip
		return nil
	}
}

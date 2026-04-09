package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/anthropic"
	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// recentMessageWindow is the number of recent messages used to build the
// away-summary prompt. Matches RECENT_MESSAGE_WINDOW in awaySummary.ts.
const recentMessageWindow = 30

// smallFastModel is the default model used for away summaries.
// A lightweight model is sufficient — we only need 1-3 sentences.
const smallFastModel = "claude-haiku-4-5-20251001"

// buildAwaySummaryPrompt builds the system prompt for the away-summary call.
// sessionMemory may be empty when no session memory file exists yet.
func buildAwaySummaryPrompt(sessionMemory string) string {
	var sb strings.Builder
	if sessionMemory != "" {
		sb.WriteString("Session memory (broader context):\n")
		sb.WriteString(sessionMemory)
		sb.WriteString("\n\n")
	}
	sb.WriteString("The user stepped away and is coming back. Write exactly 1-3 short sentences. " +
		"Start by stating the high-level task — what they are building or debugging, not implementation details. " +
		"Next: the concrete next step. Skip status reports and commit recaps.")
	return sb.String()
}

// messagesToInputMessages converts types.Message slice to anthropic.InputMessage slice,
// keeping only user/assistant messages and translating their text content.
func messagesToInputMessages(messages []types.Message) []anthropic.InputMessage {
	var result []anthropic.InputMessage
	for _, msg := range messages {
		var role string
		switch msg.Type {
		case types.MessageTypeUser:
			role = "user"
		case types.MessageTypeAssistant:
			role = "assistant"
		default:
			continue // skip system, progress, etc.
		}

		// Collect text blocks only — tool_use/tool_result skipped for this lightweight call.
		var blocks []anthropic.ContentBlock
		for _, block := range msg.Content {
			if block.Type == "text" && block.Text != "" {
				blocks = append(blocks, anthropic.ContentBlock{
					Type: "text",
					Text: block.Text,
				})
			}
		}
		if len(blocks) == 0 {
			continue
		}

		result = append(result, anthropic.InputMessage{
			Role:    role,
			Content: blocks,
		})
	}
	return result
}

// GenerateAwaySummary generates a short "while you were away" recap for the
// current session. It uses the small/fast model and injects session-memory
// content when available.
//
// Returns ("", nil) when messages is empty or the context was cancelled.
// Returns ("", err) on unexpected API errors.
//
// Mirrors generateAwaySummary in src/services/awaySummary.ts.
func GenerateAwaySummary(
	ctx context.Context,
	messages []types.Message,
	sm *SessionMemory,
	client *anthropic.Client,
) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// Read session memory (best-effort — absence is not an error).
	var memoryContent string
	if sm != nil {
		if content, err := sm.LoadContent(); err == nil {
			memoryContent = content
		}
	}

	// Use only recent messages to stay within the small model's context.
	recent := messages
	if len(recent) > recentMessageWindow {
		recent = recent[len(recent)-recentMessageWindow:]
	}

	inputMessages := messagesToInputMessages(recent)

	// Append the instruction as the final user turn.
	inputMessages = append(inputMessages, anthropic.InputMessage{
		Role: "user",
		Content: []anthropic.ContentBlock{
			{Type: "text", Text: buildAwaySummaryPrompt(memoryContent)},
		},
	})

	req := anthropic.MessageRequest{
		Model:     smallFastModel,
		MaxTokens: 256,
		Messages:  inputMessages,
	}

	resp, err := client.CreateMessage(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return "", nil // context cancelled — not an error for the caller
		}
		return "", fmt.Errorf("away summary API call failed: %w", err)
	}

	// Extract first text block from the response.
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	return "", nil
}

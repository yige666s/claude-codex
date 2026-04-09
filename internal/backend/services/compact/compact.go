package compact

import (
	"context"
	"fmt"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// CompactConversation performs full conversation compaction
func CompactConversation(
	ctx context.Context,
	messages []api.InputMessage,
	model string,
	client *api.Client,
	suppressFollowUpQuestions bool,
	customInstructions string,
	isAutoCompact bool,
) (*CompactionResult, error) {
	// Strip images from messages to reduce token usage
	strippedMessages := StripImagesFromMessages(messages)

	// Build compaction prompt
	prompt := GetCompactPrompt(false, customInstructions)

	// Create compaction request
	compactionMessages := append(strippedMessages, api.InputMessage{
		Role: "user",
		Content: []api.ContentBlock{
			{
				Type: "text",
				Text: prompt,
			},
		},
	})

	// Call API to generate summary
	response, err := client.CreateMessage(ctx, api.MessageRequest{
		Model:     model,
		Messages:  compactionMessages,
		MaxTokens: 8192,
		System:    "",
	})

	if err != nil {
		return nil, fmt.Errorf("compaction API call failed: %w", err)
	}

	// Extract summary from response
	summary := ""
	for _, block := range response.Content {
		if block.Type == "text" {
			summary += block.Text
		}
	}

	if summary == "" {
		return nil, fmt.Errorf("no summary generated")
	}

	// Note: GetCompactUserSummaryMessage would be used to format the summary
	// for display to the user, but we don't need it in the result yet

	// Create compaction result
	result := &CompactionResult{
		Success:              true,
		CompactedMessages:    len(messages),
		TokensFreed:          0, // Would be calculated in real implementation
		NewBoundaryMessageID: response.ID,
		Error:                nil,
	}

	return result, nil
}

// PartialCompact performs partial compaction of recent messages
func PartialCompact(
	ctx context.Context,
	messages []api.InputMessage,
	model string,
	client *api.Client,
	direction string,
) (*CompactionResult, error) {
	// Determine which messages to compact based on direction
	var messagesToCompact []api.InputMessage

	if direction == "backward" {
		// Compact older messages, keep recent ones
		if len(messages) > 10 {
			messagesToCompact = messages[:len(messages)-10]
		} else {
			messagesToCompact = messages
		}
	} else {
		// Default: compact all
		messagesToCompact = messages
	}

	// Strip images
	strippedMessages := StripImagesFromMessages(messagesToCompact)

	// Build partial compaction prompt
	prompt := GetCompactPrompt(true, "")

	// Create compaction request
	compactionMessages := append(strippedMessages, api.InputMessage{
		Role: "user",
		Content: []api.ContentBlock{
			{
				Type: "text",
				Text: prompt,
			},
		},
	})

	// Call API
	response, err := client.CreateMessage(ctx, api.MessageRequest{
		Model:     model,
		Messages:  compactionMessages,
		MaxTokens: 4096,
	})

	if err != nil {
		return nil, fmt.Errorf("partial compaction failed: %w", err)
	}

	// Extract summary
	summary := ""
	for _, block := range response.Content {
		if block.Type == "text" {
			summary += block.Text
		}
	}

	result := &CompactionResult{
		Success:              true,
		CompactedMessages:    len(messagesToCompact),
		TokensFreed:          0,
		NewBoundaryMessageID: response.ID,
		Error:                nil,
	}

	return result, nil
}

// BuildPostCompactMessages builds the message array after compaction
func BuildPostCompactMessages(result *CompactionResult) []api.InputMessage {
	// In real implementation, this would:
	// 1. Create a compact boundary message
	// 2. Add the summary as a user message
	// 3. Append any preserved recent messages
	// 4. Add post-compact attachments (files, plans, etc.)

	messages := []api.InputMessage{}

	// Placeholder - would build proper message structure
	return messages
}

// ShouldExcludeFromPostCompactRestore checks if a file should be excluded from restoration
func ShouldExcludeFromPostCompactRestore(filename string) bool {
	// Exclude plan files, CLAUDE.md files, etc.
	// This is a simplified version
	excludedPatterns := []string{
		"CLAUDE.md",
		".claude/",
		"plan.md",
	}

	for _, pattern := range excludedPatterns {
		if contains(filename, pattern) {
			return true
		}
	}

	return false
}

// Helper function
func contains(s, substr string) bool {
	return findString(s, substr) != -1
}

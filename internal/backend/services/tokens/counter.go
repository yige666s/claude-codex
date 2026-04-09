package tokens

import (
	"context"

	api "github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// Counter implements TokenCounter interface
type Counter struct {
	client *api.Client
	model  string
}

// NewCounter creates a new token counter
func NewCounter(client *api.Client, model string) *Counter {
	return &Counter{
		client: client,
		model:  model,
	}
}

// CountTokensWithAPI counts tokens using the Anthropic API
func (c *Counter) CountTokensWithAPI(ctx context.Context, content string) (int, error) {
	// Special case for empty content
	if content == "" {
		return 0, nil
	}

	message := api.InputMessage{
		Role: "user",
		Content: []api.ContentBlock{
			{
				Type: "text",
				Text: content,
			},
		},
	}

	return c.CountMessagesTokensWithAPI(ctx, []api.InputMessage{message}, nil)
}

// CountMessagesTokensWithAPI counts tokens for messages and tools
func (c *Counter) CountMessagesTokensWithAPI(
	ctx context.Context,
	messages []api.InputMessage,
	tools []api.Tool,
) (int, error) {
	// Strip tool search fields before counting
	normalizedMessages := stripToolSearchFieldsFromMessages(messages)

	containsThinking := HasThinkingBlocks(normalizedMessages)

	// Build count tokens request
	params := api.CountTokensRequest{
		Model:    c.model,
		Messages: normalizedMessages,
		Tools:    tools,
	}

	// Add thinking configuration if needed
	if containsThinking {
		params.MaxTokens = TokenCountMaxTokens
		params.Thinking = &api.Thinking{
			Type:         "enabled",
			BudgetTokens: TokenCountThinkingBudget,
		}
	} else {
		params.MaxTokens = 1
	}

	// Call the API
	result, err := c.client.CountTokens(ctx, params)
	if err != nil {
		return 0, err
	}

	return result.InputTokens, nil
}

// stripToolSearchFieldsFromMessages removes tool search beta fields
func stripToolSearchFieldsFromMessages(messages []api.InputMessage) []api.InputMessage {
	normalized := make([]api.InputMessage, len(messages))

	for i, message := range messages {
		normalized[i] = message
		normalizedBlocks := make([]api.ContentBlock, 0, len(message.Content))

		for _, block := range message.Content {
			// Strip 'caller' from tool_use blocks (not in our struct, but keep for consistency)
			if block.Type == "tool_use" {
				cleanBlock := api.ContentBlock{
					Type:  "tool_use",
					ID:    block.ID,
					Name:  block.Name,
					Input: block.Input,
				}
				normalizedBlocks = append(normalizedBlocks, cleanBlock)
				continue
			}

			// For tool_result, we don't have nested content in our simple struct
			// Just pass through as-is
			normalizedBlocks = append(normalizedBlocks, block)
		}

		normalized[i].Content = normalizedBlocks
	}

	return normalized
}

// CountToolsTokens counts tokens for tools only
func (c *Counter) CountToolsTokens(ctx context.Context, tools []api.Tool) (int, error) {
	// Use a dummy message to get tool token count
	dummyMessage := api.InputMessage{
		Role: "user",
		Content: []api.ContentBlock{
			{
				Type: "text",
				Text: "foo",
			},
		},
	}

	return c.CountMessagesTokensWithAPI(ctx, []api.InputMessage{dummyMessage}, tools)
}

// CountTokensViaHaikuFallback uses Haiku model for token counting as fallback
func CountTokensViaHaikuFallback(
	ctx context.Context,
	client *api.Client,
	messages []api.InputMessage,
	tools []api.Tool,
) (int, error) {
	// Use Haiku for fast, cheap token counting
	counter := NewCounter(client, "claude-3-haiku-20240307")
	return counter.CountMessagesTokensWithAPI(ctx, messages, tools)
}

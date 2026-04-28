package provider

import (
	"context"
	"fmt"
	"strings"
)

const permissionClassifierSystemPrompt = "You are a permission classifier. Return strict JSON only."

// TextCompletionClient adapts a Provider to the narrow text completion surface
// used by the permissions auto-mode classifier.
type TextCompletionClient struct {
	Provider  Provider
	Model     string
	MaxTokens int
}

func (c TextCompletionClient) Complete(ctx context.Context, prompt string) (string, error) {
	if c.Provider == nil {
		return "", fmt.Errorf("provider is not configured")
	}
	maxTokens := c.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}
	response, err := c.Provider.CreateMessage(ctx, MessageRequest{
		Model:       c.Model,
		MaxTokens:   maxTokens,
		Temperature: 0,
		System:      permissionClassifierSystemPrompt,
		Messages: []Message{{
			Role:    "user",
			Content: prompt,
		}},
	})
	if err != nil {
		return "", err
	}
	var parts []string
	for _, block := range response.Content {
		if block.Type == "" || block.Type == "text" {
			text := strings.TrimSpace(block.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("provider returned empty classifier response")
	}
	return strings.Join(parts, "\n"), nil
}

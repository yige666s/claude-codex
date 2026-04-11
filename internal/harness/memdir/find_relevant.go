package memdir

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/anthropic"
)

// RelevantMemory represents a memory file selected as relevant
type RelevantMemory struct {
	Path    string
	MtimeMs int64
}

const selectMemoriesSystemPrompt = `You are selecting memories that will be useful to Claude Code as it processes a user's query. You will be given the user's query and a list of available memory files with their filenames and descriptions.

Return a list of filenames for the memories that will clearly be useful to Claude Code as it processes the user's query (up to 5). Only include memories that you are certain will be helpful based on their name and description.
- If you are unsure if a memory will be useful in processing the user's query, then do not include it in your list. Be selective and discerning.
- If there are no memories in the list that would clearly be useful, feel free to return an empty list.
- If a list of recently-used tools is provided, do not select memories that are usage reference or API documentation for those tools (Claude Code is already exercising them). DO still select memories containing warnings, gotchas, or known issues about those tools — active use is exactly when those matter.`

// FindRelevantMemories finds memory files relevant to a query by scanning memory file headers
// and asking Sonnet to select the most relevant ones
func FindRelevantMemories(
	query string,
	memoryDir string,
	ctx context.Context,
	client *anthropic.Client,
	recentTools []string,
	alreadySurfaced map[string]bool,
) ([]RelevantMemory, error) {
	// Scan memory files
	memories, err := ScanMemoryFiles(memoryDir, ctx)
	if err != nil {
		return nil, err
	}

	// Filter out already surfaced memories
	var filtered []MemoryHeader
	for _, m := range memories {
		if !alreadySurfaced[m.FilePath] {
			filtered = append(filtered, m)
		}
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	// Select relevant memories using AI
	selectedFilenames, err := selectRelevantMemories(query, filtered, ctx, client, recentTools)
	if err != nil {
		return nil, err
	}

	// Build result map
	byFilename := make(map[string]MemoryHeader)
	for _, m := range filtered {
		byFilename[m.Filename] = m
	}

	var result []RelevantMemory
	for _, filename := range selectedFilenames {
		if m, ok := byFilename[filename]; ok {
			result = append(result, RelevantMemory{
				Path:    m.FilePath,
				MtimeMs: m.MtimeMs,
			})
		}
	}

	return result, nil
}

// selectRelevantMemories uses AI to select relevant memories
func selectRelevantMemories(
	query string,
	memories []MemoryHeader,
	ctx context.Context,
	client *anthropic.Client,
	recentTools []string,
) ([]string, error) {
	validFilenames := make(map[string]bool)
	for _, m := range memories {
		validFilenames[m.Filename] = true
	}

	manifest := FormatMemoryManifest(memories)

	toolsSection := ""
	if len(recentTools) > 0 {
		toolsSection = fmt.Sprintf("\n\nRecently used tools: %s", strings.Join(recentTools, ", "))
	}

	userContent := fmt.Sprintf("Query: %s\n\nAvailable memories:\n%s%s", query, manifest, toolsSection)

	// Create request
	req := anthropic.MessageRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 256,
		System:    selectMemoriesSystemPrompt,
		Messages: []anthropic.InputMessage{
			{
				Role: "user",
				Content: []anthropic.ContentBlock{
					{
						Type: "text",
						Text: userContent,
					},
				},
			},
		},
	}

	// Call API
	resp, err := client.CreateMessage(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil // Context cancelled
		}
		return nil, err
	}

	// Extract text from response
	var textContent string
	for _, block := range resp.Content {
		if block.Type == "text" {
			textContent = block.Text
			break
		}
	}

	if textContent == "" {
		return nil, nil
	}

	// Parse JSON response
	var parsed struct {
		SelectedMemories []string `json:"selected_memories"`
	}

	if err := json.Unmarshal([]byte(textContent), &parsed); err != nil {
		return nil, err
	}

	// Filter to valid filenames
	var result []string
	for _, filename := range parsed.SelectedMemories {
		if validFilenames[filename] {
			result = append(result, filename)
		}
	}

	return result, nil
}

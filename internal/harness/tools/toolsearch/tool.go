// Package toolsearch implements the ToolSearch tool.
package toolsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

// Tool implements ToolSearch with an optional list of available tool names.
type Tool struct {
	// AvailableTools is the list of tool names to search against.
	// If nil, the search returns a "no tools" message.
	AvailableTools []string
}

func New(availableTools []string) toolkit.Tool {
	return &Tool{AvailableTools: availableTools}
}

func (t *Tool) Name() string { return "ToolSearch" }
func (t *Tool) Description() string {
	return `Search for available tools by name or description keyword.

Use "select:<tool_name>" for direct selection, or keywords to search.

Returns matching tool names. Use this to discover tools before deciding which to use.`
}
func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Query to find tools. Use \"select:<tool_name>\" for direct selection, or keywords to search."},
    "max_results": {"type": "number", "default": 5, "description": "Maximum number of results to return"}
  },
  "required": ["query"]
}`)
}
func (t *Tool) Permission() permissions.Level { return permissions.LevelRead }
func (t *Tool) IsConcurrencySafe() bool       { return true }

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("ToolSearch: %w", err)
	}
	if in.MaxResults <= 0 {
		in.MaxResults = 5
	}

	if len(t.AvailableTools) == 0 {
		return toolkit.Result{Output: "No tools available to search."}, nil
	}

	query := strings.ToLower(strings.TrimSpace(in.Query))

	// Direct selection via "select:<name>"
	if strings.HasPrefix(query, "select:") {
		name := strings.TrimPrefix(query, "select:")
		for _, toolName := range t.AvailableTools {
			if strings.EqualFold(toolName, name) {
				return toolkit.Result{Output: toolName}, nil
			}
		}
		return toolkit.Result{Output: fmt.Sprintf("Tool not found: %s", name)}, nil
	}

	var matches []string
	for _, toolName := range t.AvailableTools {
		if strings.Contains(strings.ToLower(toolName), query) {
			matches = append(matches, toolName)
			if len(matches) >= in.MaxResults {
				break
			}
		}
	}

	if len(matches) == 0 {
		return toolkit.Result{Output: fmt.Sprintf("No tools matching '%s'.", in.Query)}, nil
	}
	return toolkit.Result{Output: strings.Join(matches, "\n")}, nil
}

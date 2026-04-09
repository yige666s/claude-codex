package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	"github.com/ding/claude-code/claude-go/internal/harness/tools"
	"github.com/ding/claude-code/claude-go/internal/harness/anthropic"
)

// Mock tool for testing
type mockTool struct {
	name   string
	output string
	err    error
}

func (m *mockTool) Name() string                                { return m.name }
func (m *mockTool) Description() string                         { return "Mock tool" }
func (m *mockTool) InputSchema() json.RawMessage                { return json.RawMessage(`{}`) }
func (m *mockTool) Permission() permissions.Level               { return permissions.LevelRead }
func (m *mockTool) IsConcurrencySafe() bool                     { return true }
func (m *mockTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	if m.err != nil {
		return tools.Result{}, m.err
	}
	return tools.Result{Output: m.output}, nil
}

func TestBuildAPITools(t *testing.T) {
	executor := NewExecutor(nil)
	registry := tools.NewRegistry(
		&mockTool{name: "tool1", output: "result1"},
		&mockTool{name: "tool2", output: "result2"},
	)
	executor.SetToolRegistry(registry)

	t.Run("Wildcard", func(t *testing.T) {
		apiTools := executor.buildAPITools([]string{"*"})
		if len(apiTools) != 2 {
			t.Errorf("Expected 2 tools, got %d", len(apiTools))
		}
	})

	t.Run("Specific tools", func(t *testing.T) {
		apiTools := executor.buildAPITools([]string{"tool1"})
		if len(apiTools) != 1 {
			t.Errorf("Expected 1 tool, got %d", len(apiTools))
		}
		if apiTools[0].Name != "tool1" {
			t.Errorf("Expected tool1, got %s", apiTools[0].Name)
		}
	})

	t.Run("No registry", func(t *testing.T) {
		executor2 := NewExecutor(nil)
		apiTools := executor2.buildAPITools([]string{"*"})
		if apiTools != nil {
			t.Error("Expected nil when no registry")
		}
	})
}

func TestExtractToolUseBlocks(t *testing.T) {
	executor := NewExecutor(nil)

	content := []ContentBlock{
		{Type: "text", Text: "Hello"},
		{Type: "tool_use", ToolName: "tool1", ToolID: "id1"},
		{Type: "text", Text: "World"},
		{Type: "tool_use", ToolName: "tool2", ToolID: "id2"},
	}

	toolUses := executor.extractToolUseBlocks(content)
	if len(toolUses) != 2 {
		t.Errorf("Expected 2 tool uses, got %d", len(toolUses))
	}
	if toolUses[0].ToolName != "tool1" {
		t.Errorf("Expected tool1, got %s", toolUses[0].ToolName)
	}
	if toolUses[1].ToolName != "tool2" {
		t.Errorf("Expected tool2, got %s", toolUses[1].ToolName)
	}
}

func TestExecuteTools(t *testing.T) {
	executor := NewExecutor(nil)
	registry := tools.NewRegistry(
		&mockTool{name: "tool1", output: "result1"},
		&mockTool{name: "tool2", output: "result2"},
	)
	executor.SetToolRegistry(registry)

	t.Run("Success", func(t *testing.T) {
		toolUseBlocks := []ContentBlock{
			{
				Type:      "tool_use",
				ToolName:  "tool1",
				ToolID:    "id1",
				ToolInput: json.RawMessage(`{}`),
			},
		}

		results, err := executor.executeTools(context.Background(), toolUseBlocks)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Expected 1 result, got %d", len(results))
		}
		if results[0].Type != "tool_result" {
			t.Errorf("Expected tool_result, got %s", results[0].Type)
		}
		if results[0].ToolUseID != "id1" {
			t.Errorf("Expected id1, got %s", results[0].ToolUseID)
		}
		if results[0].Result != "result1" {
			t.Errorf("Expected result1, got %v", results[0].Result)
		}
		if results[0].IsError {
			t.Error("Expected no error")
		}
	})

	t.Run("Tool not found", func(t *testing.T) {
		toolUseBlocks := []ContentBlock{
			{
				Type:      "tool_use",
				ToolName:  "nonexistent",
				ToolID:    "id1",
				ToolInput: json.RawMessage(`{}`),
			},
		}

		results, err := executor.executeTools(context.Background(), toolUseBlocks)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Expected 1 result, got %d", len(results))
		}
		if !results[0].IsError {
			t.Error("Expected error result")
		}
	})

	t.Run("No registry", func(t *testing.T) {
		executor2 := NewExecutor(nil)
		toolUseBlocks := []ContentBlock{
			{Type: "tool_use", ToolName: "tool1", ToolID: "id1"},
		}

		_, err := executor2.executeTools(context.Background(), toolUseBlocks)
		if err == nil {
			t.Error("Expected error when no registry")
		}
	})
}

func TestConvertAPIResponse(t *testing.T) {
	executor := NewExecutor(nil)

	t.Run("Text content", func(t *testing.T) {
		resp := &anthropic.MessageResponse{
			Role: "assistant",
			Content: []anthropic.ContentBlock{
				{Type: "text", Text: "Hello"},
			},
		}

		msg := executor.convertAPIResponse(resp)
		if msg.Role != "assistant" {
			t.Errorf("Expected assistant, got %s", msg.Role)
		}
		if len(msg.Content) != 1 {
			t.Fatalf("Expected 1 content block, got %d", len(msg.Content))
		}
		if msg.Content[0].Type != "text" {
			t.Errorf("Expected text, got %s", msg.Content[0].Type)
		}
		if msg.Content[0].Text != "Hello" {
			t.Errorf("Expected Hello, got %s", msg.Content[0].Text)
		}
	})

	t.Run("Tool use content", func(t *testing.T) {
		resp := &anthropic.MessageResponse{
			Role: "assistant",
			Content: []anthropic.ContentBlock{
				{
					Type:  "tool_use",
					ID:    "tool-id-1",
					Name:  "Read",
					Input: json.RawMessage(`{"file":"test.txt"}`),
				},
			},
		}

		msg := executor.convertAPIResponse(resp)
		if len(msg.Content) != 1 {
			t.Fatalf("Expected 1 content block, got %d", len(msg.Content))
		}
		if msg.Content[0].Type != "tool_use" {
			t.Errorf("Expected tool_use, got %s", msg.Content[0].Type)
		}
		if msg.Content[0].ToolID != "tool-id-1" {
			t.Errorf("Expected tool-id-1, got %s", msg.Content[0].ToolID)
		}
		if msg.Content[0].ToolName != "Read" {
			t.Errorf("Expected Read, got %s", msg.Content[0].ToolName)
		}
	})
}

func TestConvertMessagesToAPI(t *testing.T) {
	executor := NewExecutor(nil)

	t.Run("Text messages", func(t *testing.T) {
		messages := []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "Hello"},
				},
			},
		}

		apiMessages := executor.convertMessagesToAPI(messages)
		if len(apiMessages) != 1 {
			t.Fatalf("Expected 1 message, got %d", len(apiMessages))
		}
		if apiMessages[0].Role != "user" {
			t.Errorf("Expected user, got %s", apiMessages[0].Role)
		}
		if len(apiMessages[0].Content) != 1 {
			t.Fatalf("Expected 1 content block, got %d", len(apiMessages[0].Content))
		}
		if apiMessages[0].Content[0].Text != "Hello" {
			t.Errorf("Expected Hello, got %s", apiMessages[0].Content[0].Text)
		}
	})

	t.Run("Tool result messages", func(t *testing.T) {
		messages := []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: "tool-id-1",
						Result:    "File content",
						IsError:   false,
					},
				},
			},
		}

		apiMessages := executor.convertMessagesToAPI(messages)
		if len(apiMessages) != 1 {
			t.Fatalf("Expected 1 message, got %d", len(apiMessages))
		}
		if len(apiMessages[0].Content) != 1 {
			t.Fatalf("Expected 1 content block, got %d", len(apiMessages[0].Content))
		}
		if apiMessages[0].Content[0].Type != "tool_result" {
			t.Errorf("Expected tool_result, got %s", apiMessages[0].Content[0].Type)
		}
		if apiMessages[0].Content[0].ToolUseID != "tool-id-1" {
			t.Errorf("Expected tool-id-1, got %s", apiMessages[0].Content[0].ToolUseID)
		}
		if apiMessages[0].Content[0].Content != "File content" {
			t.Errorf("Expected File content, got %s", apiMessages[0].Content[0].Content)
		}
	})
}

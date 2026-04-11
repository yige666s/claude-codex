package compact

import (
	"testing"
	"time"

	"claude-codex/internal/public/types"
)

func TestMicrocompactMessages_NoCompactableTools(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{
					Type: "tool_use",
					ID:   "tool-1",
					Name: "UnknownTool",
				},
			},
		},
	}

	result := MicrocompactMessages(messages, nil)

	if len(result.DeletedToolIDs) != 0 {
		t.Error("Should not delete non-compactable tools")
	}

	if result.TokensSaved != 0 {
		t.Error("Should not save tokens")
	}
}

func TestMicrocompactMessages_TimeBasedClearing(t *testing.T) {
	oldTime := time.Now().Add(-10 * time.Minute)

	messages := []types.Message{
		{
			Type:      types.MessageTypeAssistant,
			Timestamp: oldTime,
			Content: []types.ContentBlock{
				{
					Type: "tool_use",
					ID:   "tool-1",
					Name: "Read",
				},
			},
		},
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{
					Type:      "tool_result",
					ToolUseID: "tool-1",
					Content:   "large content here",
				},
			},
		},
	}

	options := &MicrocompactOptions{
		TimeBasedEnabled:     true,
		TimeThresholdMinutes: 5,
		ClearMessage:         "[Cleared]",
	}

	result := MicrocompactMessages(messages, options)

	if len(result.DeletedToolIDs) != 1 {
		t.Errorf("Expected 1 deleted tool, got %d", len(result.DeletedToolIDs))
	}

	if result.DeletedToolIDs[0] != "tool-1" {
		t.Error("Should delete tool-1")
	}

	// Check that content was cleared
	cleared := false
	for _, msg := range result.Messages {
		if msg.Type == types.MessageTypeUser {
			for _, block := range msg.Content {
				if block.Type == "tool_result" && block.Content == "[Cleared]" {
					cleared = true
				}
			}
		}
	}

	if !cleared {
		t.Error("Content should be cleared")
	}
}

func TestMicrocompactMessages_RegularCompaction(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
				{Type: "tool_use", ID: "tool-2", Name: "Read"},
				{Type: "tool_use", ID: "tool-3", Name: "Read"},
			},
		},
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolUseID: "tool-1", Content: "content1"},
				{Type: "tool_result", ToolUseID: "tool-2", Content: "content2"},
				{Type: "tool_result", ToolUseID: "tool-3", Content: "content3"},
			},
		},
	}

	options := &MicrocompactOptions{
		TimeBasedEnabled:     false,
		MaxToolResultsToKeep: 2,
		ClearMessage:         "[Cleared]",
	}

	result := MicrocompactMessages(messages, options)

	// Should delete tool-1 (oldest), keep tool-2 and tool-3
	if len(result.DeletedToolIDs) != 1 {
		t.Errorf("Expected 1 deleted tool, got %d", len(result.DeletedToolIDs))
	}

	if result.DeletedToolIDs[0] != "tool-1" {
		t.Errorf("Expected tool-1 to be deleted, got %s", result.DeletedToolIDs[0])
	}
}

func TestCollectCompactableToolIDs(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
				{Type: "tool_use", ID: "tool-2", Name: "UnknownTool"},
				{Type: "tool_use", ID: "tool-3", Name: "Bash"},
			},
		},
	}

	ids := collectCompactableToolIDs(messages)

	if len(ids) != 2 {
		t.Errorf("Expected 2 compactable IDs, got %d", len(ids))
	}

	// Should include Read and Bash, not UnknownTool
	hasRead := false
	hasBash := false
	for _, id := range ids {
		if id == "tool-1" {
			hasRead = true
		}
		if id == "tool-3" {
			hasBash = true
		}
	}

	if !hasRead || !hasBash {
		t.Error("Should collect Read and Bash tool IDs")
	}
}

func TestShouldTriggerMicrocompact_TooManyResults(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
				{Type: "tool_use", ID: "tool-2", Name: "Read"},
				{Type: "tool_use", ID: "tool-3", Name: "Read"},
				{Type: "tool_use", ID: "tool-4", Name: "Read"},
				{Type: "tool_use", ID: "tool-5", Name: "Read"},
			},
		},
	}

	options := &MicrocompactOptions{
		TimeBasedEnabled:     false,
		MaxToolResultsToKeep: 3,
	}

	should := ShouldTriggerMicrocompact(messages, options)

	if !should {
		t.Error("Should trigger microcompact when too many results")
	}
}

func TestShouldTriggerMicrocompact_TimeBased(t *testing.T) {
	oldTime := time.Now().Add(-10 * time.Minute)

	messages := []types.Message{
		{
			Type:      types.MessageTypeAssistant,
			Timestamp: oldTime,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
			},
		},
	}

	options := &MicrocompactOptions{
		TimeBasedEnabled:     true,
		TimeThresholdMinutes: 5,
		MaxToolResultsToKeep: 10,
	}

	should := ShouldTriggerMicrocompact(messages, options)

	if !should {
		t.Error("Should trigger microcompact when time threshold exceeded")
	}
}

func TestShouldTriggerMicrocompact_NoTrigger(t *testing.T) {
	messages := []types.Message{
		{
			Type:      types.MessageTypeAssistant,
			Timestamp: time.Now(),
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
			},
		},
	}

	options := &MicrocompactOptions{
		TimeBasedEnabled:     true,
		TimeThresholdMinutes: 5,
		MaxToolResultsToKeep: 10,
	}

	should := ShouldTriggerMicrocompact(messages, options)

	if should {
		t.Error("Should not trigger microcompact")
	}
}

func TestEstimateMicrocompactSavings(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
				{Type: "tool_use", ID: "tool-2", Name: "Read"},
				{Type: "tool_use", ID: "tool-3", Name: "Read"},
			},
		},
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolUseID: "tool-1", Content: "1234567890"}, // 10 bytes
				{Type: "tool_result", ToolUseID: "tool-2", Content: "1234567890"}, // 10 bytes
				{Type: "tool_result", ToolUseID: "tool-3", Content: "1234567890"}, // 10 bytes
			},
		},
	}

	options := &MicrocompactOptions{
		MaxToolResultsToKeep: 2,
	}

	savings := EstimateMicrocompactSavings(messages, options)

	// Should save ~2-3 tokens (10 bytes / 4)
	if savings < 1 || savings > 5 {
		t.Errorf("Expected ~2-3 tokens saved, got %d", savings)
	}
}

func TestEstimateMicrocompactSavings_NoSavings(t *testing.T) {
	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
			},
		},
	}

	options := &MicrocompactOptions{
		MaxToolResultsToKeep: 10,
	}

	savings := EstimateMicrocompactSavings(messages, options)

	if savings != 0 {
		t.Errorf("Expected 0 tokens saved, got %d", savings)
	}
}

func TestDefaultMicrocompactOptions(t *testing.T) {
	options := DefaultMicrocompactOptions()

	if !options.TimeBasedEnabled {
		t.Error("Time-based should be enabled by default")
	}

	if options.TimeThresholdMinutes != 5 {
		t.Errorf("Expected 5 minute threshold, got %d", options.TimeThresholdMinutes)
	}

	if options.MaxToolResultsToKeep != 10 {
		t.Errorf("Expected 10 max results, got %d", options.MaxToolResultsToKeep)
	}

	if options.ClearMessage == "" {
		t.Error("Clear message should not be empty")
	}
}

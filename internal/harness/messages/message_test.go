package messages

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateUserMessage(t *testing.T) {
	t.Run("creates basic user message", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "Hello, world!",
		})

		assert.Equal(t, "user", msg.Type)
		assert.NotEmpty(t, msg.UUID)
		assert.NotEmpty(t, msg.Timestamp)
		assert.Equal(t, RoleUser, msg.Message.Role)
		assert.Equal(t, "Hello, world!", msg.Message.Content)
	})

	t.Run("handles empty content", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "",
		})

		assert.Equal(t, NoContentMessage, msg.Message.Content)
	})

	t.Run("handles nil content", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: nil,
		})

		assert.Equal(t, NoContentMessage, msg.Message.Content)
	})

	t.Run("sets meta flag", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "test",
			IsMeta:  true,
		})

		assert.True(t, msg.IsMeta)
	})

	t.Run("sets virtual flag", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content:   "test",
			IsVirtual: true,
		})

		assert.True(t, msg.IsVirtual)
	})

	t.Run("uses provided UUID", func(t *testing.T) {
		customUUID := uuid.New().String()
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "test",
			UUID:    &customUUID,
		})

		assert.Equal(t, customUUID, msg.UUID)
	})

	t.Run("creates message with content blocks", func(t *testing.T) {
		blocks := []ContentBlock{
			&TextBlock{
				BlockType: ContentTypeText,
				Text:      "Hello",
			},
		}

		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: blocks,
		})

		assert.Equal(t, blocks, msg.Message.Content)
	})
}

func TestCreateAssistantMessage(t *testing.T) {
	t.Run("creates basic assistant message", func(t *testing.T) {
		content := []ContentBlock{
			&TextBlock{
				BlockType: ContentTypeText,
				Text:      "Hello!",
			},
		}

		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: content,
		})

		assert.Equal(t, "assistant", msg.Type)
		assert.NotEmpty(t, msg.UUID)
		assert.NotEmpty(t, msg.Timestamp)
		assert.Equal(t, RoleAssistant, msg.Message.Role)
		assert.Equal(t, content, msg.Message.Content)
		assert.Equal(t, SyntheticModel, msg.Message.Model)
	})

	t.Run("handles nil content", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: nil,
		})

		require.Len(t, msg.Message.Content, 1)
		textBlock, ok := msg.Message.Content[0].(*TextBlock)
		require.True(t, ok)
		assert.Equal(t, NoContentMessage, textBlock.Text)
	})

	t.Run("sets custom model", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "test"},
			},
			Model: "claude-3-opus",
		})

		assert.Equal(t, "claude-3-opus", msg.Message.Model)
	})

	t.Run("sets usage", func(t *testing.T) {
		usage := &Usage{
			InputTokens:  100,
			OutputTokens: 50,
		}

		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "test"},
			},
			Usage: usage,
		})

		assert.Equal(t, usage, msg.Message.Usage)
	})

	t.Run("sets virtual flag", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "test"},
			},
			IsVirtual: true,
		})

		assert.True(t, msg.IsVirtual)
	})
}

func TestCreateToolResultMessage(t *testing.T) {
	t.Run("creates tool result message", func(t *testing.T) {
		msg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-123",
			Content:   "Result content",
			IsError:   false,
		})

		assert.Equal(t, "user", msg.Type)
		blocks, ok := msg.Message.Content.([]ContentBlock)
		require.True(t, ok)
		require.Len(t, blocks, 1)

		toolResult, ok := blocks[0].(*ToolResultBlock)
		require.True(t, ok)
		assert.Equal(t, "tool-123", toolResult.ToolUseID)
		assert.Equal(t, "Result content", toolResult.Content)
		assert.False(t, toolResult.IsError)
	})

	t.Run("creates error tool result", func(t *testing.T) {
		msg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-456",
			Content:   "Error occurred",
			IsError:   true,
		})

		blocks, ok := msg.Message.Content.([]ContentBlock)
		require.True(t, ok)
		toolResult := blocks[0].(*ToolResultBlock)
		assert.True(t, toolResult.IsError)
	})

	t.Run("sets source tool assistant UUID", func(t *testing.T) {
		sourceUUID := uuid.New().String()
		msg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID:               "tool-789",
			Content:                 "Result",
			SourceToolAssistantUUID: &sourceUUID,
		})

		assert.Equal(t, &sourceUUID, msg.SourceToolAssistantUUID)
	})
}

func TestCreateInterruptMessage(t *testing.T) {
	t.Run("creates regular interrupt message", func(t *testing.T) {
		msg := CreateInterruptMessage(false)

		assert.Equal(t, "user", msg.Type)
		blocks, ok := msg.Message.Content.([]ContentBlock)
		require.True(t, ok)
		require.Len(t, blocks, 1)

		textBlock := blocks[0].(*TextBlock)
		assert.Equal(t, InterruptMessage, textBlock.Text)
	})

	t.Run("creates tool use interrupt message", func(t *testing.T) {
		msg := CreateInterruptMessage(true)

		blocks, ok := msg.Message.Content.([]ContentBlock)
		require.True(t, ok)
		textBlock := blocks[0].(*TextBlock)
		assert.Equal(t, InterruptMessageForToolUse, textBlock.Text)
	})
}

func TestIsToolUseMessage(t *testing.T) {
	t.Run("returns true for tool use message", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-123",
					Name:      "test_tool",
				},
			},
		})

		assert.True(t, IsToolUseMessage(msg))
	})

	t.Run("returns false for text message", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{
					BlockType: ContentTypeText,
					Text:      "Hello",
				},
			},
		})

		assert.False(t, IsToolUseMessage(msg))
	})

	t.Run("returns false for user message", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "Hello",
		})

		assert.False(t, IsToolUseMessage(msg))
	})
}

func TestIsToolResultMessage(t *testing.T) {
	t.Run("returns true for tool result message", func(t *testing.T) {
		msg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-123",
			Content:   "Result",
		})

		assert.True(t, IsToolResultMessage(msg))
	})

	t.Run("returns false for regular user message", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "Hello",
		})

		assert.False(t, IsToolResultMessage(msg))
	})

	t.Run("returns false for assistant message", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "Hello"},
			},
		})

		assert.False(t, IsToolResultMessage(msg))
	})
}

func TestExtractTextContent(t *testing.T) {
	t.Run("extracts text from assistant message", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{
					BlockType: ContentTypeText,
					Text:      "Hello, world!",
				},
			},
		})

		text := ExtractTextContent(msg)
		assert.Equal(t, "Hello, world!", text)
	})

	t.Run("extracts text from user message with string content", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "User message",
		})

		text := ExtractTextContent(msg)
		assert.Equal(t, "User message", text)
	})

	t.Run("extracts text from user message with blocks", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: []ContentBlock{
				&TextBlock{
					BlockType: ContentTypeText,
					Text:      "Block text",
				},
			},
		})

		text := ExtractTextContent(msg)
		assert.Equal(t, "Block text", text)
	})

	t.Run("returns empty string for tool use", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-123",
					Name:      "test",
				},
			},
		})

		text := ExtractTextContent(msg)
		assert.Equal(t, "", text)
	})
}

func TestGetToolUseID(t *testing.T) {
	t.Run("extracts tool use ID", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-abc-123",
					Name:      "test_tool",
				},
			},
		})

		toolUseID := GetToolUseID(msg)
		assert.Equal(t, "tool-abc-123", toolUseID)
	})

	t.Run("returns empty for non-tool-use message", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "Hello"},
			},
		})

		toolUseID := GetToolUseID(msg)
		assert.Equal(t, "", toolUseID)
	})
}

func TestGetToolResultID(t *testing.T) {
	t.Run("extracts tool result ID", func(t *testing.T) {
		msg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-xyz-789",
			Content:   "Result",
		})

		toolResultID := GetToolResultID(msg)
		assert.Equal(t, "tool-xyz-789", toolResultID)
	})

	t.Run("returns empty for non-tool-result message", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "Hello",
		})

		toolResultID := GetToolResultID(msg)
		assert.Equal(t, "", toolResultID)
	})
}

func TestIsSyntheticMessage(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{"interrupt message", InterruptMessage, true},
		{"interrupt for tool use", InterruptMessageForToolUse, true},
		{"cancel message", CancelMessage, true},
		{"reject message", RejectMessage, true},
		{"no response requested", NoResponseRequested, true},
		{"regular message", "Hello, world!", false},
		{"empty message", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := CreateUserMessage(CreateUserMessageOptions{
				Content: tt.content,
			})

			result := IsSyntheticMessage(msg)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetLastAssistantMessage(t *testing.T) {
	t.Run("returns last assistant message", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User 1"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 1"}},
			}),
			CreateUserMessage(CreateUserMessageOptions{Content: "User 2"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 2"}},
			}),
		}

		lastMsg := GetLastAssistantMessage(messages)
		require.NotNil(t, lastMsg)
		assert.Equal(t, "Assistant 2", ExtractTextContent(lastMsg))
	})

	t.Run("returns nil when no assistant messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User 1"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "User 2"}),
		}

		lastMsg := GetLastAssistantMessage(messages)
		assert.Nil(t, lastMsg)
	})
}

func TestHasToolCallsInLastAssistantTurn(t *testing.T) {
	t.Run("returns true when last assistant has tool calls", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&ToolUseBlock{
						BlockType: ContentTypeToolUse,
						ID:        "tool-123",
						Name:      "test",
					},
				},
			}),
		}

		result := HasToolCallsInLastAssistantTurn(messages)
		assert.True(t, result)
	})

	t.Run("returns false when last assistant has no tool calls", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "Hello"},
				},
			}),
		}

		result := HasToolCallsInLastAssistantTurn(messages)
		assert.False(t, result)
	})

	t.Run("returns false when no assistant messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User"}),
		}

		result := HasToolCallsInLastAssistantTurn(messages)
		assert.False(t, result)
	})
}

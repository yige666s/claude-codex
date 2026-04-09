package messages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveUUID(t *testing.T) {
	t.Run("derives deterministic UUID", func(t *testing.T) {
		parentUUID := "550e8400-e29b-41d4-a716-446655440000"

		uuid1 := DeriveUUID(parentUUID, 0)
		uuid2 := DeriveUUID(parentUUID, 0)

		assert.Equal(t, uuid1, uuid2, "Same input should produce same UUID")
	})

	t.Run("different indices produce different UUIDs", func(t *testing.T) {
		parentUUID := "550e8400-e29b-41d4-a716-446655440000"

		uuid1 := DeriveUUID(parentUUID, 0)
		uuid2 := DeriveUUID(parentUUID, 1)

		assert.NotEqual(t, uuid1, uuid2)
	})

	t.Run("uses first 24 chars of parent", func(t *testing.T) {
		parentUUID := "550e8400-e29b-41d4-a716-446655440000"

		uuid := DeriveUUID(parentUUID, 0)

		assert.Equal(t, parentUUID[:24], uuid[:24])
	})

	t.Run("handles short parent UUID", func(t *testing.T) {
		parentUUID := "short"

		uuid := DeriveUUID(parentUUID, 0)

		assert.Len(t, uuid, 36) // Should be padded to full length
	})
}

func TestNormalizeMessages(t *testing.T) {
	t.Run("normalizes single content block messages", func(t *testing.T) {
		messages := []Message{
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "Hello"},
				},
			}),
		}

		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 1)
		assert.Equal(t, "Hello", ExtractTextContent(normalized[0]))
	})

	t.Run("splits multi-block assistant messages", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "First"},
				&TextBlock{BlockType: ContentTypeText, Text: "Second"},
				&TextBlock{BlockType: ContentTypeText, Text: "Third"},
			},
		})

		messages := []Message{msg}
		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 3)
		assert.Equal(t, "First", ExtractTextContent(normalized[0]))
		assert.Equal(t, "Second", ExtractTextContent(normalized[1]))
		assert.Equal(t, "Third", ExtractTextContent(normalized[2]))
	})

	t.Run("generates new UUIDs for split messages", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "First"},
				&TextBlock{BlockType: ContentTypeText, Text: "Second"},
			},
		})

		messages := []Message{msg}
		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 2)
		uuid1 := normalized[0].GetUUID()
		uuid2 := normalized[1].GetUUID()

		assert.NotEqual(t, uuid1, uuid2)
		// First UUID should be derived from parent
		assert.Equal(t, DeriveUUID(msg.UUID, 0), uuid1)
		assert.Equal(t, DeriveUUID(msg.UUID, 1), uuid2)
	})

	t.Run("normalizes user messages with string content", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{
				Content: "Hello, world!",
			}),
		}

		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 1)
		assert.Equal(t, "Hello, world!", ExtractTextContent(normalized[0]))
	})

	t.Run("splits multi-block user messages", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "Block 1"},
				&TextBlock{BlockType: ContentTypeText, Text: "Block 2"},
			},
		})

		messages := []Message{msg}
		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 2)
		assert.Equal(t, "Block 1", ExtractTextContent(normalized[0]))
		assert.Equal(t, "Block 2", ExtractTextContent(normalized[1]))
	})

	t.Run("preserves message metadata", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "Test"},
			},
			IsVirtual: true,
		})

		messages := []Message{msg}
		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 1)
		normalizedAssistant, ok := normalized[0].(*NormalizedAssistantMessage)
		require.True(t, ok)
		assert.True(t, normalizedAssistant.IsVirtual)
	})

	t.Run("handles mixed message types", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User message"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "Assistant 1"},
					&TextBlock{BlockType: ContentTypeText, Text: "Assistant 2"},
				},
			}),
			CreateUserMessage(CreateUserMessageOptions{Content: "Another user message"}),
		}

		normalized := NormalizeMessages(messages)

		require.Len(t, normalized, 4)
		assert.Equal(t, "User message", ExtractTextContent(normalized[0]))
		assert.Equal(t, "Assistant 1", ExtractTextContent(normalized[1]))
		assert.Equal(t, "Assistant 2", ExtractTextContent(normalized[2]))
		assert.Equal(t, "Another user message", ExtractTextContent(normalized[3]))
	})
}

func TestIsNotEmptyMessage(t *testing.T) {
	t.Run("returns true for non-empty message", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "Hello, world!",
		})

		assert.True(t, IsNotEmptyMessage(msg))
	})

	t.Run("returns false for empty string", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "",
		})

		assert.False(t, IsNotEmptyMessage(msg))
	})

	t.Run("returns false for NoContentMessage", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: NoContentMessage,
		})

		assert.False(t, IsNotEmptyMessage(msg))
	})

	t.Run("returns false for InterruptMessageForToolUse", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: InterruptMessageForToolUse,
		})

		assert.False(t, IsNotEmptyMessage(msg))
	})

	t.Run("returns true for whitespace-only message", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: "   ",
		})

		// Note: IsNotEmptyMessage checks length > 0, not trimmed length
		assert.True(t, IsNotEmptyMessage(msg))
	})
}

func TestHashContent(t *testing.T) {
	t.Run("produces consistent hash", func(t *testing.T) {
		content := "Hello, world!"

		hash1 := HashContent(content)
		hash2 := HashContent(content)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("different content produces different hash", func(t *testing.T) {
		hash1 := HashContent("Hello")
		hash2 := HashContent("World")

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("produces hex string", func(t *testing.T) {
		hash := HashContent("test")

		assert.Len(t, hash, 64) // SHA-256 produces 64 hex characters
		assert.Regexp(t, "^[0-9a-f]+$", hash)
	})
}

func TestNormalizedMessageInterface(t *testing.T) {
	t.Run("NormalizedAssistantMessage implements NormalizedMessage", func(t *testing.T) {
		msg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "Test"},
			},
		})

		normalized := &NormalizedAssistantMessage{
			AssistantMessage: msg,
			ContentBlock:     msg.Message.Content[0],
		}

		var _ NormalizedMessage = normalized
		assert.Equal(t, msg.Message.Content[0], normalized.GetContentBlock())
	})

	t.Run("NormalizedUserMessage implements NormalizedMessage", func(t *testing.T) {
		msg := CreateUserMessage(CreateUserMessageOptions{
			Content: []ContentBlock{
				&TextBlock{BlockType: ContentTypeText, Text: "Test"},
			},
		})

		block := &TextBlock{BlockType: ContentTypeText, Text: "Test"}
		normalized := &NormalizedUserMessage{
			UserMessage:  msg,
			ContentBlock: block,
		}

		var _ NormalizedMessage = normalized
		assert.Equal(t, block, normalized.GetContentBlock())
	})
}

func TestNormalizeMessagesChaining(t *testing.T) {
	t.Run("maintains new chain flag across messages", func(t *testing.T) {
		messages := []Message{
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "Single"},
				},
			}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "Multi 1"},
					&TextBlock{BlockType: ContentTypeText, Text: "Multi 2"},
				},
			}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "After split"},
				},
			}),
		}

		normalized := NormalizeMessages(messages)

		// First message keeps original UUID (single block)
		assert.Equal(t, messages[0].GetUUID(), normalized[0].GetUUID())

		// Second message gets split and derives new UUIDs
		assert.Equal(t, DeriveUUID(messages[1].GetUUID(), 0), normalized[1].GetUUID())
		assert.Equal(t, DeriveUUID(messages[1].GetUUID(), 1), normalized[2].GetUUID())

		// Third message also gets derived UUID (chain continues)
		assert.Equal(t, DeriveUUID(messages[2].GetUUID(), 0), normalized[3].GetUUID())
	})
}

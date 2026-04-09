package messages

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultFilterOptions(t *testing.T) {
	opts := DefaultFilterOptions()

	assert.True(t, opts.IncludeToolUse)
	assert.True(t, opts.IncludeToolResults)
	assert.True(t, opts.IncludeSynthetic)
	assert.True(t, opts.IncludeEmpty)
	assert.True(t, opts.IncludeMeta)
	assert.True(t, opts.IncludeVirtual)
	assert.Equal(t, 0, opts.MinLength)
	assert.Equal(t, 0, opts.MaxLength)
}

func TestFilterMessages(t *testing.T) {
	t.Run("filters tool use messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User message"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&ToolUseBlock{
						BlockType: ContentTypeToolUse,
						ID:        "tool-123",
						Name:      "test",
					},
				},
			}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{
					&TextBlock{BlockType: ContentTypeText, Text: "Text message"},
				},
			}),
		}

		opts := DefaultFilterOptions()
		opts.IncludeToolUse = false

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 2)
		assert.False(t, IsToolUseMessage(filtered[1]))
	})

	t.Run("filters tool result messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User message"}),
			CreateToolResultMessage(CreateToolResultMessageOptions{
				ToolUseID: "tool-123",
				Content:   "Result",
			}),
			CreateUserMessage(CreateUserMessageOptions{Content: "Another message"}),
		}

		opts := DefaultFilterOptions()
		opts.IncludeToolResults = false

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 2)
		assert.False(t, IsToolResultMessage(filtered[1]))
	})

	t.Run("filters synthetic messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "Regular message"}),
			CreateInterruptMessage(false),
			CreateUserMessage(CreateUserMessageOptions{Content: CancelMessage}),
		}

		opts := DefaultFilterOptions()
		opts.IncludeSynthetic = false

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 1)
		assert.Equal(t, "Regular message", ExtractTextContent(filtered[0]))
	})

	t.Run("filters empty messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "Non-empty"}),
			CreateUserMessage(CreateUserMessageOptions{Content: NoContentMessage}),
			CreateUserMessage(CreateUserMessageOptions{Content: ""}),
		}

		opts := DefaultFilterOptions()
		opts.IncludeEmpty = false

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 1)
		assert.Equal(t, "Non-empty", ExtractTextContent(filtered[0]))
	})

	t.Run("filters meta messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{
				Content: "Regular",
				IsMeta:  false,
			}),
			CreateUserMessage(CreateUserMessageOptions{
				Content: "Meta",
				IsMeta:  true,
			}),
		}

		opts := DefaultFilterOptions()
		opts.IncludeMeta = false

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 1)
		assert.Equal(t, "Regular", ExtractTextContent(filtered[0]))
	})

	t.Run("filters virtual messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{
				Content:   "Real",
				IsVirtual: false,
			}),
			CreateUserMessage(CreateUserMessageOptions{
				Content:   "Virtual",
				IsVirtual: true,
			}),
		}

		opts := DefaultFilterOptions()
		opts.IncludeVirtual = false

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 1)
		assert.Equal(t, "Real", ExtractTextContent(filtered[0]))
	})

	t.Run("filters by minimum length", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "Hi"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "Hello, world!"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "Hey"}),
		}

		opts := DefaultFilterOptions()
		opts.MinLength = 5

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 1)
		assert.Equal(t, "Hello, world!", ExtractTextContent(filtered[0]))
	})

	t.Run("filters by maximum length", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "Short"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "This is a very long message"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "Hi"}),
		}

		opts := DefaultFilterOptions()
		opts.MaxLength = 10

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 2)
		assert.Equal(t, "Short", ExtractTextContent(filtered[0]))
		assert.Equal(t, "Hi", ExtractTextContent(filtered[1]))
	})

	t.Run("combines multiple filters", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{
				Content: "Good message",
				IsMeta:  false,
			}),
			CreateUserMessage(CreateUserMessageOptions{
				Content: "Meta message",
				IsMeta:  true,
			}),
			CreateUserMessage(CreateUserMessageOptions{Content: "Hi"}),
			CreateInterruptMessage(false),
		}

		opts := DefaultFilterOptions()
		opts.IncludeMeta = false
		opts.IncludeSynthetic = false
		opts.MinLength = 5

		filtered := FilterMessages(messages, opts)

		require.Len(t, filtered, 1)
		assert.Equal(t, "Good message", ExtractTextContent(filtered[0]))
	})
}

func TestFilterUnresolvedToolUses(t *testing.T) {
	t.Run("keeps resolved tool uses", func(t *testing.T) {
		toolUseMsg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-123",
					Name:      "test",
				},
			},
		})

		toolResultMsg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-123",
			Content:   "Result",
		})

		messages := []Message{toolUseMsg, toolResultMsg}
		filtered := FilterUnresolvedToolUses(messages)

		require.Len(t, filtered, 2)
	})

	t.Run("removes unresolved tool uses", func(t *testing.T) {
		toolUseMsg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-123",
					Name:      "test",
				},
			},
		})

		userMsg := CreateUserMessage(CreateUserMessageOptions{Content: "User message"})

		messages := []Message{toolUseMsg, userMsg}
		filtered := FilterUnresolvedToolUses(messages)

		require.Len(t, filtered, 1)
		assert.Equal(t, "User message", ExtractTextContent(filtered[0]))
	})

	t.Run("handles multiple tool uses", func(t *testing.T) {
		toolUse1 := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-1",
					Name:      "test1",
				},
			},
		})

		toolUse2 := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-2",
					Name:      "test2",
				},
			},
		})

		toolResult1 := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-1",
			Content:   "Result 1",
		})

		messages := []Message{toolUse1, toolUse2, toolResult1}
		filtered := FilterUnresolvedToolUses(messages)

		require.Len(t, filtered, 2)
		assert.True(t, IsToolUseMessage(filtered[0]))
		assert.True(t, IsToolResultMessage(filtered[1]))
	})
}

func TestFilterWhitespaceOnlyMessages(t *testing.T) {
	t.Run("removes whitespace-only messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "Real content"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "   "}),
			CreateUserMessage(CreateUserMessageOptions{Content: "\t\n"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "More content"}),
		}

		filtered := FilterWhitespaceOnlyMessages(messages)

		require.Len(t, filtered, 2)
		assert.Equal(t, "Real content", ExtractTextContent(filtered[0]))
		assert.Equal(t, "More content", ExtractTextContent(filtered[1]))
	})

	t.Run("keeps messages with content", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "  Hello  "}),
			CreateUserMessage(CreateUserMessageOptions{Content: "\nWorld\n"}),
		}

		filtered := FilterWhitespaceOnlyMessages(messages)

		require.Len(t, filtered, 2)
	})
}

func TestFilterByRole(t *testing.T) {
	t.Run("filters user messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User 1"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant"}},
			}),
			CreateUserMessage(CreateUserMessageOptions{Content: "User 2"}),
		}

		filtered := FilterByRole(messages, RoleUser)

		require.Len(t, filtered, 2)
		assert.Equal(t, "User 1", ExtractTextContent(filtered[0]))
		assert.Equal(t, "User 2", ExtractTextContent(filtered[1]))
	})

	t.Run("filters assistant messages", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 1"}},
			}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 2"}},
			}),
		}

		filtered := FilterByRole(messages, RoleAssistant)

		require.Len(t, filtered, 2)
		assert.Equal(t, "Assistant 1", ExtractTextContent(filtered[0]))
		assert.Equal(t, "Assistant 2", ExtractTextContent(filtered[1]))
	})
}

func TestFilterByTimeRange(t *testing.T) {
	t.Run("filters messages by time range", func(t *testing.T) {
		time1 := "2024-01-01T10:00:00Z"
		time2 := "2024-01-01T11:00:00Z"
		time3 := "2024-01-01T12:00:00Z"

		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{
				Content:   "Message 1",
				Timestamp: &time1,
			}),
			CreateUserMessage(CreateUserMessageOptions{
				Content:   "Message 2",
				Timestamp: &time2,
			}),
			CreateUserMessage(CreateUserMessageOptions{
				Content:   "Message 3",
				Timestamp: &time3,
			}),
		}

		filtered := FilterByTimeRange(messages, "2024-01-01T10:30:00Z", "2024-01-01T11:30:00Z")

		require.Len(t, filtered, 1)
		assert.Equal(t, "Message 2", ExtractTextContent(filtered[0]))
	})
}

func TestGroupByToolUse(t *testing.T) {
	t.Run("groups tool use with result", func(t *testing.T) {
		toolUseMsg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-123",
					Name:      "test",
				},
			},
		})

		toolResultMsg := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-123",
			Content:   "Result",
		})

		messages := []Message{toolUseMsg, toolResultMsg}
		groups := GroupByToolUse(messages)

		require.Len(t, groups, 1)
		assert.Equal(t, "tool-123", groups[0].ToolUseID)
		assert.NotNil(t, groups[0].ToolUseMessage)
		assert.NotNil(t, groups[0].ToolResultMessage)
	})

	t.Run("handles multiple tool use groups", func(t *testing.T) {
		toolUse1 := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-1",
					Name:      "test1",
				},
			},
		})

		toolUse2 := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-2",
					Name:      "test2",
				},
			},
		})

		toolResult1 := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-1",
			Content:   "Result 1",
		})

		toolResult2 := CreateToolResultMessage(CreateToolResultMessageOptions{
			ToolUseID: "tool-2",
			Content:   "Result 2",
		})

		messages := []Message{toolUse1, toolUse2, toolResult1, toolResult2}
		groups := GroupByToolUse(messages)

		require.Len(t, groups, 2)
		assert.Equal(t, "tool-1", groups[0].ToolUseID)
		assert.Equal(t, "tool-2", groups[1].ToolUseID)
	})

	t.Run("ignores unmatched tool uses", func(t *testing.T) {
		toolUseMsg := CreateAssistantMessage(CreateAssistantMessageOptions{
			Content: []ContentBlock{
				&ToolUseBlock{
					BlockType: ContentTypeToolUse,
					ID:        "tool-123",
					Name:      "test",
				},
			},
		})

		messages := []Message{toolUseMsg}
		groups := GroupByToolUse(messages)

		assert.Len(t, groups, 0)
	})
}

func TestCountMessagesByRole(t *testing.T) {
	t.Run("counts messages by role", func(t *testing.T) {
		messages := []Message{
			CreateUserMessage(CreateUserMessageOptions{Content: "User 1"}),
			CreateUserMessage(CreateUserMessageOptions{Content: "User 2"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 1"}},
			}),
			CreateUserMessage(CreateUserMessageOptions{Content: "User 3"}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 2"}},
			}),
			CreateAssistantMessage(CreateAssistantMessageOptions{
				Content: []ContentBlock{&TextBlock{BlockType: ContentTypeText, Text: "Assistant 3"}},
			}),
		}

		counts := CountMessagesByRole(messages)

		assert.Equal(t, 3, counts[RoleUser])
		assert.Equal(t, 3, counts[RoleAssistant])
	})

	t.Run("handles empty message list", func(t *testing.T) {
		messages := []Message{}
		counts := CountMessagesByRole(messages)

		assert.Equal(t, 0, counts[RoleUser])
		assert.Equal(t, 0, counts[RoleAssistant])
	})
}

func TestGetMessagesByUUIDs(t *testing.T) {
	t.Run("retrieves messages by UUIDs", func(t *testing.T) {
		msg1 := CreateUserMessage(CreateUserMessageOptions{Content: "Message 1"})
		msg2 := CreateUserMessage(CreateUserMessageOptions{Content: "Message 2"})
		msg3 := CreateUserMessage(CreateUserMessageOptions{Content: "Message 3"})

		messages := []Message{msg1, msg2, msg3}
		uuids := []string{msg1.UUID, msg3.UUID}

		filtered := GetMessagesByUUIDs(messages, uuids)

		require.Len(t, filtered, 2)
		assert.Equal(t, msg1.UUID, filtered[0].GetUUID())
		assert.Equal(t, msg3.UUID, filtered[1].GetUUID())
	})

	t.Run("handles non-existent UUIDs", func(t *testing.T) {
		msg1 := CreateUserMessage(CreateUserMessageOptions{Content: "Message 1"})
		messages := []Message{msg1}
		uuids := []string{"non-existent-uuid"}

		filtered := GetMessagesByUUIDs(messages, uuids)

		assert.Len(t, filtered, 0)
	})
}

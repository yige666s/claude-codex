// Package engine provides tests for the QueryEngine.
package engine

import (
	"context"
	"testing"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/tool"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

type mockTool struct {
	name        string
	description string
}

func (m *mockTool) Name() string                                                { return m.name }
func (m *mockTool) Aliases() []string                                           { return nil }
func (m *mockTool) SearchHint() string                                          { return "" }
func (m *mockTool) Description(input map[string]interface{}, opts tool.DescriptionOptions) (string, error) {
	return m.description, nil
}
func (m *mockTool) Call(ctx context.Context, args map[string]interface{}, toolCtx *tool.ToolUseContext) (*tool.ToolResult, error) {
	return &tool.ToolResult{Data: "mock result"}, nil
}
func (m *mockTool) InputSchema() *tool.ToolInputJSONSchema                     { return nil }
func (m *mockTool) OutputSchema() *tool.ToolInputJSONSchema                    { return nil }
func (m *mockTool) ValidateInput(input map[string]interface{}, toolCtx *tool.ToolUseContext) (tool.ValidationResult, error) {
	return tool.NewValidationSuccess(), nil
}
func (m *mockTool) CheckPermissions(input map[string]interface{}, toolCtx *tool.ToolUseContext) (*tool.PermissionResult, error) {
	return nil, nil
}
func (m *mockTool) IsEnabled() bool                                            { return true }
func (m *mockTool) IsConcurrencySafe(input map[string]interface{}) bool        { return true }
func (m *mockTool) IsReadOnly(input map[string]interface{}) bool               { return false }
func (m *mockTool) IsDestructive(input map[string]interface{}) bool            { return false }
func (m *mockTool) IsOpenWorld(input map[string]interface{}) bool              { return false }
func (m *mockTool) RequiresUserInteraction() bool                              { return false }
func (m *mockTool) InterruptBehavior() tool.InterruptBehavior                  { return tool.InterruptCancel }
func (m *mockTool) IsSearchOrReadCommand(input map[string]interface{}) *tool.SearchOrReadInfo {
	return nil
}
func (m *mockTool) UserFacingName(input map[string]interface{}) string         { return m.name }
func (m *mockTool) GetActivityDescription(input map[string]interface{}) string { return "" }
func (m *mockTool) ToAutoClassifierInput(input map[string]interface{}) interface{} {
	return nil
}
func (m *mockTool) GetPath(input map[string]interface{}) string { return "" }
func (m *mockTool) PreparePermissionMatcher(input map[string]interface{}) (func(pattern string) bool, error) {
	return nil, nil
}
func (m *mockTool) BackfillObservableInput(input map[string]interface{}) {}
func (m *mockTool) InputsEquivalent(a, b map[string]interface{}) bool   { return false }
func (m *mockTool) IsTransparentWrapper() bool                           { return false }
func (m *mockTool) MaxResultSizeChars() int                              { return 0 }
func (m *mockTool) IsMCP() bool                                          { return false }
func (m *mockTool) IsLSP() bool                                          { return false }
func (m *mockTool) ShouldDefer() bool                                    { return false }
func (m *mockTool) AlwaysLoad() bool                                     { return false }
func (m *mockTool) MCPInfo() *tool.MCPInfo                               { return nil }
func (m *mockTool) Strict() bool                                         { return false }

func TestNewQueryEngine(t *testing.T) {
	config := QueryEngineConfig{
		Cwd:   "/test",
		Tools: []tool.Tool{&mockTool{name: "test_tool"}},
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	}

	engine := NewQueryEngine(config)

	assert.NotNil(t, engine)
	assert.Equal(t, "/test", engine.config.Cwd)
	assert.Len(t, engine.config.Tools, 1)
	assert.Empty(t, engine.mutableMessages)
	assert.NotNil(t, engine.totalUsage)
	assert.Empty(t, engine.permissionDenials)
}

func TestNewQueryEngineWithInitialMessages(t *testing.T) {
	initialMessages := []Message{
		{
			Type:      "user",
			UUID:      uuid.New().String(),
			Timestamp: time.Now(),
			Content:   "Hello",
		},
	}

	config := QueryEngineConfig{
		Cwd:             "/test",
		InitialMessages: initialMessages,
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	}

	engine := NewQueryEngine(config)

	assert.Len(t, engine.mutableMessages, 1)
	assert.Equal(t, "user", engine.mutableMessages[0].Type)
}

func TestQueryEngineGetMessages(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	})

	msg := Message{
		Type:      "user",
		UUID:      uuid.New().String(),
		Timestamp: time.Now(),
		Content:   "Test message",
	}

	engine.addMessage(msg)

	messages := engine.GetMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Type)

	// Verify it's a copy (modifying returned slice doesn't affect internal state)
	messages[0].Type = "modified"
	assert.Equal(t, "user", engine.mutableMessages[0].Type)
}

func TestQueryEngineInterrupt(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	})

	// Interrupt should cancel the abort context
	engine.Interrupt()

	select {
	case <-engine.abortCtx.Done():
		// Expected: context should be cancelled
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Context was not cancelled after interrupt")
	}
}

func TestQueryEngineSetModel(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		UserSpecifiedModel: "claude-3-opus",
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	})

	assert.Equal(t, "claude-3-opus", engine.config.UserSpecifiedModel)

	engine.SetModel("claude-3-sonnet")
	assert.Equal(t, "claude-3-sonnet", engine.config.UserSpecifiedModel)
}

func TestQueryEnginePermissionTracking(t *testing.T) {
	deniedTool := &mockTool{name: "denied_tool"}

	engine := NewQueryEngine(QueryEngineConfig{
		Tools: []tool.Tool{deniedTool},
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "deny", Reason: "test denial"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	})

	// Simulate permission check
	result, err := engine.wrapCanUseTool(
		deniedTool,
		map[string]interface{}{},
		nil,
		nil,
		"test-tool-use-id",
		false,
	)

	require.NoError(t, err)
	assert.Equal(t, "deny", result.Behavior)

	// Verify denial was tracked
	denials := engine.GetPermissionDenials()
	assert.Len(t, denials, 1)
	assert.Equal(t, "denied_tool", denials[0].ToolName)
	assert.Equal(t, "test-tool-use-id", denials[0].ToolUseID)
}

func TestQueryEngineUsageTracking(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	})

	// Add some usage
	engine.updateUsage(&Usage{
		InputTokens:  100,
		OutputTokens: 50,
	})

	usage := engine.GetTotalUsage()
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)

	// Add more usage
	engine.updateUsage(&Usage{
		InputTokens:  200,
		OutputTokens: 100,
	})

	usage = engine.GetTotalUsage()
	assert.Equal(t, 300, usage.InputTokens)
	assert.Equal(t, 150, usage.OutputTokens)
}

func TestAccumulateUsage(t *testing.T) {
	tests := []struct {
		name     string
		total    *Usage
		current  *Usage
		expected *Usage
	}{
		{
			name:  "nil total",
			total: nil,
			current: &Usage{
				InputTokens:  100,
				OutputTokens: 50,
			},
			expected: &Usage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		},
		{
			name: "nil current",
			total: &Usage{
				InputTokens:  100,
				OutputTokens: 50,
			},
			current: nil,
			expected: &Usage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		},
		{
			name: "both non-nil",
			total: &Usage{
				InputTokens:  100,
				OutputTokens: 50,
			},
			current: &Usage{
				InputTokens:  200,
				OutputTokens: 100,
			},
			expected: &Usage{
				InputTokens:  300,
				OutputTokens: 150,
			},
		},
		{
			name: "with cache tokens",
			total: &Usage{
				InputTokens:              100,
				OutputTokens:             50,
				CacheCreationInputTokens: 10,
				CacheReadInputTokens:     5,
			},
			current: &Usage{
				InputTokens:              200,
				OutputTokens:             100,
				CacheCreationInputTokens: 20,
				CacheReadInputTokens:     15,
			},
			expected: &Usage{
				InputTokens:              300,
				OutputTokens:             150,
				CacheCreationInputTokens: 30,
				CacheReadInputTokens:     20,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AccumulateUsage(tt.total, tt.current)
			assert.Equal(t, tt.expected.InputTokens, result.InputTokens)
			assert.Equal(t, tt.expected.OutputTokens, result.OutputTokens)
			assert.Equal(t, tt.expected.CacheCreationInputTokens, result.CacheCreationInputTokens)
			assert.Equal(t, tt.expected.CacheReadInputTokens, result.CacheReadInputTokens)
		})
	}
}

func TestSubmitMessage(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd: "/test",
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	})

	ctx := context.Background()
	ch, err := engine.SubmitMessage(ctx, "Hello, world!", nil)

	require.NoError(t, err)
	require.NotNil(t, ch)

	// Read messages from channel
	var messages []SDKMessage
	for msg := range ch {
		messages = append(messages, msg)
	}

	// Should receive at least a result message
	assert.NotEmpty(t, messages)

	// Last message should be a result
	lastMsg := messages[len(messages)-1]
	assert.Equal(t, "result", lastMsg.Type)
}

func TestAskConvenienceFunction(t *testing.T) {
	config := QueryEngineConfig{
		Cwd: "/test",
		CanUseTool: func(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
			return &PermissionResult{Behavior: "allow"}, nil
		},
		GetAppState: func() interface{} { return nil },
		SetAppState: func(f func(interface{}) interface{}) {},
	}

	ctx := context.Background()
	ch, err := Ask(ctx, config, "Test prompt")

	require.NoError(t, err)
	require.NotNil(t, ch)

	// Consume channel
	for range ch {
	}
}

func TestSDKCompatToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "read_file",
			expected: "read_file",
		},
		{
			name:     "name with special chars",
			input:    "mcp__server__tool",
			expected: "mcp__server__tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sdkCompatToolName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEmptyUsage(t *testing.T) {
	usage := EmptyUsage()
	assert.NotNil(t, usage)
	assert.Equal(t, 0, usage.InputTokens)
	assert.Equal(t, 0, usage.OutputTokens)
	assert.Equal(t, 0, usage.CacheCreationInputTokens)
	assert.Equal(t, 0, usage.CacheReadInputTokens)
}

func TestUpdateUsage(t *testing.T) {
	total := &Usage{
		InputTokens:  100,
		OutputTokens: 50,
	}

	delta := &Usage{
		InputTokens:  50,
		OutputTokens: 25,
	}

	result := UpdateUsage(total, delta)
	assert.Equal(t, 150, result.InputTokens)
	assert.Equal(t, 75, result.OutputTokens)
}

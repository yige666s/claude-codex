// Package engine provides tests for the QueryEngine adapter.
package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/query"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/tool"
	toolkit "claude-codex/internal/harness/tools"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTool struct {
	name        string
	description string
}

func (m *mockTool) Name() string       { return m.name }
func (m *mockTool) Aliases() []string  { return nil }
func (m *mockTool) SearchHint() string { return "" }
func (m *mockTool) Description(input map[string]interface{}, opts tool.DescriptionOptions) (string, error) {
	return m.description, nil
}
func (m *mockTool) Call(ctx context.Context, args map[string]interface{}, toolCtx *tool.ToolUseContext) (*tool.ToolResult, error) {
	return &tool.ToolResult{Data: "mock result"}, nil
}
func (m *mockTool) InputSchema() *tool.ToolInputJSONSchema  { return nil }
func (m *mockTool) OutputSchema() *tool.ToolInputJSONSchema { return nil }
func (m *mockTool) ValidateInput(input map[string]interface{}, toolCtx *tool.ToolUseContext) (tool.ValidationResult, error) {
	return tool.NewValidationSuccess(), nil
}
func (m *mockTool) CheckPermissions(input map[string]interface{}, toolCtx *tool.ToolUseContext) (*tool.PermissionResult, error) {
	return nil, nil
}
func (m *mockTool) IsEnabled() bool                                     { return true }
func (m *mockTool) IsConcurrencySafe(input map[string]interface{}) bool { return true }
func (m *mockTool) IsReadOnly(input map[string]interface{}) bool        { return false }
func (m *mockTool) IsDestructive(input map[string]interface{}) bool     { return false }
func (m *mockTool) IsOpenWorld(input map[string]interface{}) bool       { return false }
func (m *mockTool) RequiresUserInteraction() bool                       { return false }
func (m *mockTool) InterruptBehavior() tool.InterruptBehavior           { return tool.InterruptCancel }
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
func (m *mockTool) InputsEquivalent(a, b map[string]interface{}) bool    { return false }
func (m *mockTool) IsTransparentWrapper() bool                           { return false }
func (m *mockTool) MaxResultSizeChars() int                              { return 0 }
func (m *mockTool) IsMCP() bool                                          { return false }
func (m *mockTool) IsLSP() bool                                          { return false }
func (m *mockTool) ShouldDefer() bool                                    { return false }
func (m *mockTool) AlwaysLoad() bool                                     { return false }
func (m *mockTool) MCPInfo() *tool.MCPInfo                               { return nil }
func (m *mockTool) Strict() bool                                         { return false }

func allowAllCanUseTool(tool tool.Tool, input map[string]interface{}, toolCtx *tool.ToolUseContext, assistantMessage interface{}, toolUseID string, forceDecision bool) (*PermissionResult, error) {
	return &PermissionResult{Behavior: "allow"}, nil
}

type adapterPlanner struct {
	call plannerapi.ToolCall
}

func (p adapterPlanner) Next(_ context.Context, session *state.Session, _ []toolkit.Descriptor) (plannerapi.Plan, error) {
	last := session.LastMessage()
	if last != nil && last.Role == "tool" {
		return plannerapi.Plan{
			AssistantText: "handled: " + last.ToolOutput,
			StopReason:    "end_turn",
		}, nil
	}
	return plannerapi.Plan{
		ToolCalls:  []plannerapi.ToolCall{p.call},
		StopReason: "tool_use",
	}, nil
}

type streamingAdapterPlanner struct{}

func (streamingAdapterPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (plannerapi.Plan, error) {
	return plannerapi.Plan{AssistantText: "hello", StopReason: "end_turn"}, nil
}

func (streamingAdapterPlanner) StreamNext(_ context.Context, _ *state.Session, _ []toolkit.Descriptor, onChunk func(string)) (plannerapi.Plan, error) {
	onChunk("hel")
	onChunk("lo")
	return plannerapi.Plan{AssistantText: "hello", StopReason: "end_turn"}, nil
}

type terminalPlanner struct{}

func (terminalPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (plannerapi.Plan, error) {
	return plannerapi.Plan{AssistantText: "done", StopReason: "end_turn"}, nil
}

type emptyAdapterPlanner struct{}

func (emptyAdapterPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (plannerapi.Plan, error) {
	return plannerapi.Plan{StopReason: "end_turn"}, nil
}

func TestNewQueryEngine_InitializesAdapter(t *testing.T) {
	cache := query.NewFileStateCache(16, 16*1024)
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:                t.TempDir(),
		Tools:              []tool.Tool{&mockTool{name: "test_tool", description: "test tool"}},
		ReadFileCache:      cache,
		UserSpecifiedModel: "claude-3-opus",
		CanUseTool:         allowAllCanUseTool,
	})

	require.NotNil(t, engine)
	require.NotNil(t, engine.inner)
	require.NotNil(t, engine.innerConfig)

	assert.Equal(t, cache, engine.GetReadFileState())
	assert.Equal(t, "claude-3-opus", engine.innerConfig.UserSpecifiedModel)
	require.Len(t, engine.innerConfig.Tools, 1)
	assert.Equal(t, "test_tool", engine.innerConfig.Tools[0].Name())
	assert.NotEmpty(t, engine.GetSessionID())
	assert.Equal(t, engine.GetSessionID(), engine.GetSessionID())
}

func TestSubmitMessage_PropagatesQueryModelErrors(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:           t.TempDir(),
		SessionID:     "empty-response-session",
		Planner:       emptyAdapterPlanner{},
		FallbackModel: "test-model",
	})

	stream, err := engine.SubmitMessage(context.Background(), "hello", nil)
	require.NoError(t, err)

	var final SDKMessage
	for msg := range stream {
		final = msg
	}

	require.Equal(t, "result", final.Type)
	require.True(t, final.IsError)
	require.Equal(t, "error_during_execution", final.Subtype)
	require.NotEmpty(t, final.Errors)
	assert.Contains(t, final.Errors[0], "no assistant text or tool calls")
}

func TestNewQueryEngine_WithInitialMessages(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd: t.TempDir(),
		InitialMessages: []Message{
			{
				Type:      "user",
				UUID:      uuid.New().String(),
				Timestamp: time.Now(),
				Content:   "Hello",
			},
		},
		CanUseTool: allowAllCanUseTool,
	})

	messages := engine.GetMessages()
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Type)
	assert.Equal(t, "Hello", messages[0].Content)
}

func TestQueryEngineSetModel_UpdatesUnderlyingConfig(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:                t.TempDir(),
		UserSpecifiedModel: "claude-3-opus",
		CanUseTool:         allowAllCanUseTool,
	})

	engine.SetModel("claude-3-sonnet")

	assert.Equal(t, "claude-3-sonnet", engine.config.UserSpecifiedModel)
	assert.Equal(t, "claude-3-sonnet", engine.innerConfig.UserSpecifiedModel)
}

func TestQueryEngineSubmitMessage_DelegatesToQueryEngine(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:           t.TempDir(),
		SessionID:     "session-under-test",
		FallbackModel: "claude-sonnet-4-6",
		CanUseTool:    allowAllCanUseTool,
	})

	ch, err := engine.SubmitMessage(context.Background(), "Hello, world!", nil)
	require.NoError(t, err)

	var messages []SDKMessage
	for msg := range ch {
		messages = append(messages, msg)
	}

	require.NotEmpty(t, messages)
	last := messages[len(messages)-1]
	assert.Equal(t, "result", last.Type)
	assert.Equal(t, "session-under-test", last.SessionID)
	assert.NotEqual(t, "Not yet implemented", last.Result)

	stored := engine.GetMessages()
	require.NotEmpty(t, stored)
	assert.Equal(t, "user", stored[0].Type)
}

func TestAskConvenienceFunction_UsesSameAdapterFlow(t *testing.T) {
	ch, err := Ask(context.Background(), QueryEngineConfig{
		Cwd:           t.TempDir(),
		FallbackModel: "claude-sonnet-4-6",
		CanUseTool:    allowAllCanUseTool,
	}, "Test prompt")
	require.NoError(t, err)

	var messages []SDKMessage
	for msg := range ch {
		messages = append(messages, msg)
	}

	require.NotEmpty(t, messages)
	assert.Equal(t, "result", messages[len(messages)-1].Type)
}

func TestQueryEngineSubmitMessage_RunsPlannerBackedRuntime(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:           t.TempDir(),
		SessionID:     "planner-session",
		FallbackModel: "claude-sonnet-4-6",
		CanUseTool:    allowAllCanUseTool,
		Planner: adapterPlanner{
			call: plannerapi.ToolCall{
				ID:    "tool-1",
				Name:  "fake_tool",
				Input: json.RawMessage(`{"path":"README.md"}`),
			},
		},
		ToolDescriptors: []toolkit.Descriptor{{Name: "fake_tool"}},
		ExecuteTool: func(context.Context, string, string, []byte) (string, error) {
			return "tool output", nil
		},
		MaxTurns: 3,
	})

	ch, err := engine.SubmitMessage(context.Background(), "run tool", nil)
	require.NoError(t, err)

	var messages []SDKMessage
	for msg := range ch {
		messages = append(messages, msg)
	}

	require.NotEmpty(t, messages)
	assert.Equal(t, "result", messages[len(messages)-1].Type)

	stored := engine.GetMessages()
	require.NotEmpty(t, stored)
	assert.Equal(t, "assistant", stored[len(stored)-1].Type)
	assert.Equal(t, "handled: tool output", stored[len(stored)-1].Content)
}

func TestQueryEngineSubmitMessage_EmitsStreamingPlannerEventsWithoutPersistingThem(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:           t.TempDir(),
		SessionID:     "streaming-session",
		FallbackModel: "claude-sonnet-4-6",
		CanUseTool:    allowAllCanUseTool,
		Planner:       streamingAdapterPlanner{},
	})

	ch, err := engine.SubmitMessage(context.Background(), "say hi", nil)
	require.NoError(t, err)

	var sawDelta bool
	for msg := range ch {
		if msg.Type != "stream_event" {
			continue
		}
		event, ok := msg.Event.(map[string]any)
		require.True(t, ok)
		if event["type"] != "content_block_delta" {
			continue
		}
		delta, ok := event["delta"].(map[string]any)
		require.True(t, ok)
		if delta["type"] == "text_delta" && delta["text"] == "hel" {
			sawDelta = true
		}
	}
	assert.True(t, sawDelta)

	stored := engine.GetMessages()
	require.Len(t, stored, 2)
	assert.Equal(t, "user", stored[0].Type)
	assert.Equal(t, "assistant", stored[1].Type)
	assert.Equal(t, "hello", stored[1].Content)
}

func TestQueryEngineSubmitMessage_ParsesTokenBudget(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:        t.TempDir(),
		CanUseTool: allowAllCanUseTool,
		Planner:    terminalPlanner{},
	})

	ch, err := engine.SubmitMessage(context.Background(), "+1k keep going", nil)
	require.NoError(t, err)
	for range ch {
	}

	require.NotNil(t, engine.innerConfig.TokenBudget)
	assert.Equal(t, 1000, *engine.innerConfig.TokenBudget)
}

func TestQueryEngineInterrupt_DoesNotPanic(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:        t.TempDir(),
		CanUseTool: allowAllCanUseTool,
	})

	engine.Interrupt()
}

func TestQueryEnginePermissionDenials_StartEmpty(t *testing.T) {
	engine := NewQueryEngine(QueryEngineConfig{
		Cwd:        t.TempDir(),
		CanUseTool: allowAllCanUseTool,
	})

	assert.Empty(t, engine.GetPermissionDenials())
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

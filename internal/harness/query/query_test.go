package query

import (
	"context"
	"testing"

	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock dependencies for testing

type mockModelCaller struct {
	messages chan types.Message
	err      error
}

func (m *mockModelCaller) call(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.messages, nil
}

type mockCompactService struct {
	result *CompactionResult
	err    error
}

func (m *mockCompactService) Compact(ctx context.Context, messages []types.Message) (*CompactionResult, error) {
	return m.result, m.err
}

func (m *mockCompactService) IsAutoCompactEnabled() bool {
	return true
}

func (m *mockCompactService) CalculateTokenWarningState(tokenCount int, model string) TokenWarningState {
	return TokenWarningState{
		IsAtBlockingLimit: tokenCount > 100000,
		IsNearLimit:       tokenCount > 80000,
	}
}

// Test basic query execution

func TestQuery_BasicExecution(t *testing.T) {
	ctx := context.Background()

	// Create mock message channel
	messageChan := make(chan types.Message, 1)
	messageChan <- types.Message{
		Type: types.MessageTypeAssistant,
		Content: []types.ContentBlock{
			{Type: "text", Text: "Hello, world!"},
		},
		StopReason: "end_turn",
	}
	close(messageChan)

	mockCaller := &mockModelCaller{messages: messageChan}

	params := &QueryParams{
		Messages:       []types.Message{},
		UserContext:    map[string]string{},
		SystemContext:  map[string]string{},
		CanUseTool:     func(t tool.Tool, input map[string]interface{}, ctx *tool.ToolUseContext, msg interface{}, id string, force *string) (*tool.PermissionResult, error) {
			return &tool.PermissionResult{Behavior: "allow"}, nil
		},
		ToolUseContext: &tool.ToolUseContext{
			Ctx: context.Background(),
			QueryTracking: &tool.QueryChainTracking{
				ChainID: "test-chain",
				Depth:   0,
			},
		},
		QuerySource:    "test",
		Deps: &QueryDeps{
			CallModel: mockCaller.call,
			UUID:      func() string { return "test-uuid" },
		},
	}

	eventChan, terminalChan, err := Query(ctx, params)
	require.NoError(t, err)

	// Collect events
	var events []interface{}
	for event := range eventChan {
		events = append(events, event)
	}

	// Get terminal result
	terminal := <-terminalChan

	assert.Equal(t, TerminalReasonCompleted, terminal.Reason)
	assert.Greater(t, len(events), 0)
}

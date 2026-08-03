package query

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	corehooks "claude-codex/internal/harness/hooks"
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

type sequenceModelCaller struct {
	calls int
	steps [][]types.Message
	seen  []*ModelCallParams
}

type captureStopHook struct {
	called bool
	input  *corehooks.HookInput
}

type persistentBlockingStopHook struct {
	calls       int
	activeFlags []bool
}

type recordingToolHook struct {
	event  corehooks.HookEvent
	mu     sync.Mutex
	inputs []*corehooks.HookInput
	result *corehooks.HookResult
}

type countingEchoTool struct {
	*tool.BaseTool
	mu    sync.Mutex
	calls int
}

func (h *recordingToolHook) Name() string               { return "record-" + string(h.event) }
func (h *recordingToolHook) Event() corehooks.HookEvent { return h.event }
func (h *recordingToolHook) IsAsync() bool              { return false }
func (h *recordingToolHook) Timeout() time.Duration     { return time.Second }
func (h *recordingToolHook) Execute(_ context.Context, input *corehooks.HookInput) (*corehooks.HookResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.inputs = append(h.inputs, input)
	if h.result != nil {
		return h.result, nil
	}
	return &corehooks.HookResult{Continue: true}, nil
}

func (h *recordingToolHook) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.inputs)
}

func newCountingEchoTool() *countingEchoTool {
	return &countingEchoTool{BaseTool: tool.NewToolBuilder("echo").Build()}
}

func (t *countingEchoTool) Call(_ context.Context, args map[string]interface{}, _ *tool.ToolUseContext) (*tool.ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return &tool.ToolResult{Data: fmt.Sprintf("echo:%v", args["value"])}, nil
}

func (t *countingEchoTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func (h *captureStopHook) Name() string               { return "capture-stop" }
func (h *captureStopHook) Event() corehooks.HookEvent { return corehooks.EventStop }
func (h *captureStopHook) IsAsync() bool              { return false }
func (h *captureStopHook) Timeout() time.Duration     { return time.Second }
func (h *captureStopHook) Execute(_ context.Context, input *corehooks.HookInput) (*corehooks.HookResult, error) {
	h.called = true
	h.input = input
	return &corehooks.HookResult{Continue: true}, nil
}

func (h *persistentBlockingStopHook) Name() string               { return "persistent-blocking-stop" }
func (h *persistentBlockingStopHook) Event() corehooks.HookEvent { return corehooks.EventStop }
func (h *persistentBlockingStopHook) IsAsync() bool              { return false }
func (h *persistentBlockingStopHook) Timeout() time.Duration     { return time.Second }
func (h *persistentBlockingStopHook) Execute(_ context.Context, input *corehooks.HookInput) (*corehooks.HookResult, error) {
	h.calls++
	active, _ := input.Metadata["stop_hook_active"].(bool)
	h.activeFlags = append(h.activeFlags, active)
	return &corehooks.HookResult{Continue: true, BlockingError: "still blocked"}, nil
}

func (s *sequenceModelCaller) call(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
	s.seen = append(s.seen, params)
	idx := s.calls
	if idx >= len(s.steps) {
		idx = len(s.steps) - 1
	}
	ch := make(chan types.Message, len(s.steps[idx]))
	for _, msg := range s.steps[idx] {
		ch <- msg
	}
	close(ch)
	s.calls++
	return ch, nil
}

type echoTool struct {
	*tool.BaseTool
}

func newEchoTool() *echoTool {
	return &echoTool{BaseTool: tool.NewToolBuilder("echo").Build()}
}

func (t *echoTool) Call(ctx context.Context, args map[string]interface{}, toolCtx *tool.ToolUseContext) (*tool.ToolResult, error) {
	return &tool.ToolResult{Data: fmt.Sprintf("echo:%v", args["value"])}, nil
}

func (m *mockModelCaller) call(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.messages, nil
}

type emptyChannelModelCaller struct{}

func (emptyChannelModelCaller) call(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
	ch := make(chan types.Message)
	close(ch)
	return ch, nil
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

func TestQuery_AutoCompactUsesCompactServiceBeforeModel(t *testing.T) {
	t.Setenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", "1")

	caller := &sequenceModelCaller{steps: [][]types.Message{{
		{
			Type:       types.MessageTypeAssistant,
			Content:    []types.ContentBlock{{Type: "text", Text: "after compact"}},
			StopReason: "end_turn",
		},
	}}}
	compactMsg := types.Message{
		Type:    types.MessageTypeUser,
		Content: []types.ContentBlock{{Type: "text", Text: "compacted summary"}},
	}
	compactSvc := &mockCompactService{
		result: &CompactionResult{Messages: []types.Message{compactMsg}},
	}
	large := strings.Repeat("x", 9000)
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.Options.MainLoopModel = "claude-test"
	toolCtx.AbortController = tool.NewAbortController()

	events, terminal, err := Query(context.Background(), &QueryParams{
		Messages:       []types.Message{{Type: types.MessageTypeUser, Content: []types.ContentBlock{{Type: "text", Text: large}}}},
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		Deps: &QueryDeps{
			CallModel:      caller.call,
			UUID:           func() string { return "compact-turn" },
			CompactService: compactSvc,
		},
	})
	require.NoError(t, err)
	for range events {
	}
	require.Equal(t, TerminalReasonCompleted, (<-terminal).Reason)
	require.Len(t, caller.seen, 1)
	require.Len(t, caller.seen[0].Messages, 1)
	assert.Equal(t, "compacted summary", caller.seen[0].Messages[0].Content[0].Text)
}

func TestQueryRunsConfiguredStopHooksBeforeCompleting(t *testing.T) {
	hook := &captureStopHook{}
	registry := corehooks.NewRegistry()
	require.NoError(t, registry.Register(hook))
	caller := &sequenceModelCaller{steps: [][]types.Message{{{
		Type:       types.MessageTypeAssistant,
		Content:    []types.ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn",
	}}}}
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.AbortController = tool.NewAbortController()
	toolCtx.SessionID = "hook-session"

	events, terminal, err := Query(context.Background(), &QueryParams{
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		HookExecutor:   corehooks.NewExecutor(registry),
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "hook-turn" },
		},
	})
	require.NoError(t, err)
	for range events {
	}
	require.Equal(t, TerminalReasonCompleted, (<-terminal).Reason)
	require.True(t, hook.called)
	require.NotNil(t, hook.input)
	assert.Equal(t, "hook-session", hook.input.SessionID)
}

func TestQueryPersistentBlockingStopHookRetriesOnceThenTerminates(t *testing.T) {
	hook := &persistentBlockingStopHook{}
	registry := corehooks.NewRegistry()
	require.NoError(t, registry.Register(hook))
	caller := &sequenceModelCaller{steps: [][]types.Message{
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "first"}}, StopReason: "end_turn"}},
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "retry"}}, StopReason: "end_turn"}},
	}}
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.AbortController = tool.NewAbortController()

	events, terminal, err := Query(context.Background(), &QueryParams{
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		HookExecutor:   corehooks.NewExecutor(registry),
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "hook-turn" },
		},
	})
	require.NoError(t, err)
	for range events {
	}
	require.Equal(t, TerminalReasonStopHookPrevented, (<-terminal).Reason)
	assert.Equal(t, 2, caller.calls)
	assert.Equal(t, 2, hook.calls)
	assert.Equal(t, []bool{false, true}, hook.activeFlags)
}

func TestQuery_QueuedCommandLifecycle(t *testing.T) {
	ClearQueuedCommands()
	defer ClearQueuedCommands()

	var lifecycle []string
	SetCommandLifecycleListener(func(uuid string, event string) {
		lifecycle = append(lifecycle, uuid+":"+event)
	})
	defer SetCommandLifecycleListener(nil)
	EnqueueCommand(QueuedCommand{UUID: "cmd-1", Content: "queued command"})

	caller := &sequenceModelCaller{steps: [][]types.Message{{
		{
			Type:       types.MessageTypeAssistant,
			Content:    []types.ContentBlock{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		},
	}}}
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.AbortController = tool.NewAbortController()

	events, terminal, err := Query(context.Background(), &QueryParams{
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "turn" },
		},
	})
	require.NoError(t, err)
	for range events {
	}
	require.Equal(t, TerminalReasonCompleted, (<-terminal).Reason)
	assert.Equal(t, []string{"cmd-1:started", "cmd-1:completed"}, lifecycle)
	require.Len(t, caller.seen, 1)
	require.NotEmpty(t, caller.seen[0].Messages)
	assert.Equal(t, "queued command", caller.seen[0].Messages[len(caller.seen[0].Messages)-1].Content[0].Text)
}

func TestQuery_TokenBudgetContinuesUntilProgressDiminishes(t *testing.T) {
	caller := &sequenceModelCaller{steps: [][]types.Message{
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "a"}}, StopReason: "end_turn"}},
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "b"}}, StopReason: "end_turn"}},
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "c"}}, StopReason: "end_turn"}},
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "d"}}, StopReason: "end_turn"}},
		{{Type: types.MessageTypeAssistant, Content: []types.ContentBlock{{Type: "text", Text: "e"}}, StopReason: "end_turn"}},
	}}
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.AbortController = tool.NewAbortController()
	budget := 1000

	events, terminal, err := Query(context.Background(), &QueryParams{
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		TokenBudget:    &budget,
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "turn" },
		},
	})
	require.NoError(t, err)
	for range events {
	}
	require.Equal(t, TerminalReasonCompleted, (<-terminal).Reason)
	assert.Greater(t, caller.calls, 1)
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
		Messages:      []types.Message{},
		UserContext:   map[string]string{},
		SystemContext: map[string]string{},
		CanUseTool: func(t tool.Tool, input map[string]interface{}, ctx *tool.ToolUseContext, msg interface{}, id string, force *string) (*tool.PermissionResult, error) {
			return &tool.PermissionResult{Behavior: "allow"}, nil
		},
		ToolUseContext: &tool.ToolUseContext{
			Ctx: context.Background(),
			QueryTracking: &tool.QueryChainTracking{
				ChainID: "test-chain",
				Depth:   0,
			},
		},
		QuerySource: "test",
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

func TestQuery_EmptyModelChannelIsModelError(t *testing.T) {
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.AbortController = tool.NewAbortController()
	toolCtx.Options.MainLoopModel = "test-model"

	eventChan, terminalChan, err := Query(context.Background(), &QueryParams{
		Messages:       []types.Message{{Type: types.MessageTypeUser, Content: []types.ContentBlock{{Type: "text", Text: "hello"}}}},
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		Deps: &QueryDeps{
			CallModel: emptyChannelModelCaller{}.call,
			UUID:      func() string { return "test-uuid" },
		},
	})
	require.NoError(t, err)
	for range eventChan {
	}
	terminal := <-terminalChan
	require.Equal(t, TerminalReasonModelError, terminal.Reason)
	require.Error(t, terminal.Error)
	assert.Contains(t, terminal.Error.Error(), "no assistant text or tool calls")
	assert.Contains(t, terminal.Error.Error(), "model=test-model")
}

func TestQuery_ToolUseFeedsToolResultBackToModel(t *testing.T) {
	ctx := context.Background()
	caller := &sequenceModelCaller{
		steps: [][]types.Message{
			{
				{
					Type: types.MessageTypeAssistant,
					Content: []types.ContentBlock{
						{
							Type: "tool_use",
							ID:   "tool-1",
							Name: "echo",
							Input: map[string]interface{}{
								"value": "ok",
							},
						},
					},
					StopReason: "tool_use",
				},
			},
			{
				{
					Type: types.MessageTypeAssistant,
					Content: []types.ContentBlock{
						{Type: "text", Text: "done"},
					},
					StopReason: "end_turn",
				},
			},
		},
	}

	toolCtx := tool.NewToolUseContext(ctx)
	toolCtx.SetTools([]tool.Tool{newEchoTool()})
	toolCtx.Options.MainLoopModel = "claude-test"
	toolCtx.AbortController = tool.NewAbortController()

	params := &QueryParams{
		Messages: []types.Message{},
		CanUseTool: func(t tool.Tool, input map[string]interface{}, ctx *tool.ToolUseContext, msg interface{}, id string, force *string) (*tool.PermissionResult, error) {
			return &tool.PermissionResult{Behavior: tool.PermissionAllow, UpdatedInput: input}, nil
		},
		ToolUseContext: toolCtx,
		QuerySource:    "test",
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "test-uuid" },
		},
	}

	eventChan, terminalChan, err := Query(ctx, params)
	require.NoError(t, err)

	var toolResult *types.Message
	for event := range eventChan {
		msg, ok := event.(types.Message)
		if !ok || msg.Type != types.MessageTypeUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID == "tool-1" {
				copy := msg
				toolResult = &copy
			}
		}
	}

	terminal := <-terminalChan
	require.Equal(t, TerminalReasonCompleted, terminal.Reason)
	require.Equal(t, 2, caller.calls)
	require.NotNil(t, toolResult)
	require.Len(t, toolResult.Content, 1)
	assert.Equal(t, "echo:ok", toolResult.Content[0].Content)
}

func TestQuery_ToolLifecycleHooksWrapStreamingExecution(t *testing.T) {
	ctx := context.Background()
	caller := &sequenceModelCaller{steps: [][]types.Message{
		{{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{{
				Type:  "tool_use",
				ID:    "tool-hook-1",
				Name:  "echo",
				Input: map[string]interface{}{"value": "original"},
			}},
			StopReason: "tool_use",
		}},
		{{
			Type:       types.MessageTypeAssistant,
			Content:    []types.ContentBlock{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		}},
	}}
	pre := &recordingToolHook{
		event: corehooks.EventPreToolUse,
		result: &corehooks.HookResult{
			Continue:     true,
			UpdatedInput: map[string]any{"value": "rewritten"},
		},
	}
	post := &recordingToolHook{
		event: corehooks.EventPostToolUse,
		result: &corehooks.HookResult{
			Continue:             true,
			UpdatedMCPToolOutput: "post-processed",
		},
	}
	failed := &recordingToolHook{event: corehooks.EventPostToolUseFailure}
	registry := corehooks.NewRegistry()
	require.NoError(t, registry.Register(pre))
	require.NoError(t, registry.Register(post))
	require.NoError(t, registry.Register(failed))

	toolCtx := tool.NewToolUseContext(ctx)
	toolCtx.SetTools([]tool.Tool{newEchoTool()})
	toolCtx.Options.MainLoopModel = "claude-test"
	toolCtx.AbortController = tool.NewAbortController()
	toolCtx.SessionID = "tool-hook-session"

	events, terminal, err := Query(ctx, &QueryParams{
		ToolUseContext: toolCtx,
		HookExecutor:   corehooks.NewExecutor(registry),
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "tool-hook-turn" },
		},
	})
	require.NoError(t, err)
	var output string
	for event := range events {
		msg, ok := event.(types.Message)
		if !ok {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID == "tool-hook-1" {
				output = block.Content
			}
		}
	}
	require.Equal(t, TerminalReasonCompleted, (<-terminal).Reason)
	assert.Equal(t, "post-processed", output)
	assert.Equal(t, 1, pre.callCount())
	assert.Equal(t, 1, post.callCount())
	assert.Zero(t, failed.callCount())
	require.Len(t, post.inputs, 1)
	assert.Equal(t, "rewritten", post.inputs[0].Tool.Input["value"])
	assert.Equal(t, "echo:rewritten", post.inputs[0].Tool.Output)
	assert.Equal(t, "tool-hook-session", post.inputs[0].SessionID)
}

func TestQuery_BlockingPreToolHookPreventsToolCall(t *testing.T) {
	ctx := context.Background()
	caller := &sequenceModelCaller{steps: [][]types.Message{{{
		Type: types.MessageTypeAssistant,
		Content: []types.ContentBlock{{
			Type:  "tool_use",
			ID:    "tool-hook-blocked",
			Name:  "echo",
			Input: map[string]interface{}{"value": "blocked"},
		}},
		StopReason: "tool_use",
	}}}}
	pre := &recordingToolHook{
		event: corehooks.EventPreToolUse,
		result: &corehooks.HookResult{
			Continue:   false,
			StopReason: "policy blocked tool",
		},
	}
	failed := &recordingToolHook{event: corehooks.EventPostToolUseFailure}
	registry := corehooks.NewRegistry()
	require.NoError(t, registry.Register(pre))
	require.NoError(t, registry.Register(failed))
	echo := newCountingEchoTool()
	toolCtx := tool.NewToolUseContext(ctx)
	toolCtx.SetTools([]tool.Tool{echo})
	toolCtx.AbortController = tool.NewAbortController()

	events, terminal, err := Query(ctx, &QueryParams{
		ToolUseContext: toolCtx,
		HookExecutor:   corehooks.NewExecutor(registry),
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "tool-hook-blocked-turn" },
		},
	})
	require.NoError(t, err)
	for range events {
	}
	assert.Equal(t, TerminalReasonHookStopped, (<-terminal).Reason)
	assert.Zero(t, echo.callCount())
	assert.Equal(t, 1, pre.callCount())
	assert.Equal(t, 1, failed.callCount())
}

func TestRunTools_MissingToolTriggersFailureHook(t *testing.T) {
	failed := &recordingToolHook{event: corehooks.EventPostToolUseFailure}
	registry := corehooks.NewRegistry()
	require.NoError(t, registry.Register(failed))

	toolCtx := tool.NewToolUseContext(context.Background())
	updates := runTools(
		[]types.ToolUseBlock{{
			ID:    "missing-tool-use",
			Name:  "missing-tool",
			Input: map[string]interface{}{"value": "input"},
		}},
		nil,
		nil,
		toolCtx,
		newToolHookLifecycle(corehooks.NewExecutor(registry), toolCtx, "test"),
	)

	update := <-updates
	require.NotNil(t, update)
	assert.ErrorIs(t, update.Error, tool.ErrToolNotFound)
	assert.Equal(t, "failed", update.Status)
	assert.Equal(t, 1, failed.callCount())
	require.Len(t, failed.inputs, 1)
	assert.ErrorIs(t, failed.inputs[0].Tool.Error, tool.ErrToolNotFound)
}

func TestQuery_MaxOutputRecoveryIncreasesTokensWithoutFakeModelEscalation(t *testing.T) {
	maxed := func() types.Message {
		return types.Message{
			Type:       types.MessageTypeAssistant,
			Content:    []types.ContentBlock{{Type: "text", Text: "partial"}},
			StopReason: "max_tokens",
		}
	}
	caller := &sequenceModelCaller{steps: [][]types.Message{
		{maxed()},
		{maxed()},
		{maxed()},
		{{
			Type:       types.MessageTypeAssistant,
			Content:    []types.ContentBlock{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		}},
	}}
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.Options.MainLoopModel = "configured-model"
	toolCtx.AbortController = tool.NewAbortController()

	events, terminal, err := Query(context.Background(), &QueryParams{
		ToolUseContext: toolCtx,
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "max-output-turn" },
		},
	})
	require.NoError(t, err)
	for range events {
	}
	require.Equal(t, TerminalReasonCompleted, (<-terminal).Reason)
	require.Len(t, caller.seen, 4)
	assert.Equal(t, []int{8192, 12288, 16384, 20480}, []int{
		caller.seen[0].MaxTokens,
		caller.seen[1].MaxTokens,
		caller.seen[2].MaxTokens,
		caller.seen[3].MaxTokens,
	})
	for _, call := range caller.seen {
		assert.Equal(t, "configured-model", call.Model)
	}
}

func TestQuery_ToolUseDenialFeedsErroredToolResult(t *testing.T) {
	ctx := context.Background()
	caller := &sequenceModelCaller{
		steps: [][]types.Message{
			{
				{
					Type: types.MessageTypeAssistant,
					Content: []types.ContentBlock{
						{
							Type:  "tool_use",
							ID:    "tool-1",
							Name:  "echo",
							Input: map[string]interface{}{"value": "blocked"},
						},
					},
					StopReason: "tool_use",
				},
			},
			{
				{
					Type:       types.MessageTypeAssistant,
					Content:    []types.ContentBlock{{Type: "text", Text: "stopped"}},
					StopReason: "end_turn",
				},
			},
		},
	}
	toolCtx := tool.NewToolUseContext(ctx)
	toolCtx.SetTools([]tool.Tool{newEchoTool()})
	toolCtx.AbortController = tool.NewAbortController()

	params := &QueryParams{
		ToolUseContext: toolCtx,
		CanUseTool: func(t tool.Tool, input map[string]interface{}, ctx *tool.ToolUseContext, msg interface{}, id string, force *string) (*tool.PermissionResult, error) {
			return &tool.PermissionResult{Behavior: tool.PermissionDeny, Reason: "policy"}, nil
		},
		Deps: &QueryDeps{
			CallModel: caller.call,
			UUID:      func() string { return "test-uuid" },
		},
	}

	eventChan, terminalChan, err := Query(ctx, params)
	require.NoError(t, err)

	var denied *types.Message
	for event := range eventChan {
		msg, ok := event.(types.Message)
		if !ok {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID == "tool-1" {
				copy := msg
				denied = &copy
			}
		}
	}
	terminal := <-terminalChan

	require.Equal(t, TerminalReasonCompleted, terminal.Reason)
	require.NotNil(t, denied)
	require.True(t, denied.Content[0].IsError)
	assert.Contains(t, denied.Content[0].Content, "permission denied")
}

func TestQueryHelpers_CreateMessagesAndDetectRecoverableErrors(t *testing.T) {
	user := createUserMessage("continue", true)
	require.Equal(t, types.MessageTypeUser, user.Type)
	require.True(t, user.IsMeta)
	require.NotEmpty(t, user.UUID)
	require.Len(t, user.Content, 1)
	assert.Equal(t, "continue", user.Content[0].Text)

	apiErr := createAssistantAPIErrorMessage("Prompt too long", "invalid_request")
	require.Equal(t, types.MessageTypeAssistant, apiErr.Type)
	require.True(t, apiErr.IsApiErrorMessage)
	require.Len(t, apiErr.Content, 1)
	assert.Equal(t, "Prompt too long", apiErr.Content[0].Text)

	assistantErr := types.AssistantMessage{Type: "assistant", Message: apiErr, APIError: "prompt_too_long"}
	assert.True(t, isWithheldPromptTooLong(&assistantErr))

	fallback := types.Message{
		Type:              types.MessageTypeAssistant,
		Subtype:           "overloaded_error",
		IsApiErrorMessage: true,
		Content:           []types.ContentBlock{{Type: "text", Text: "overloaded"}},
	}
	assert.True(t, isFallbackError(fallback))
}

func TestQueryCompactDecisionUsesTokenThresholdsAndEnvGates(t *testing.T) {
	t.Setenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", "1")
	t.Setenv("DISABLE_AUTO_COMPACT", "")
	large := types.Message{
		Type:    types.MessageTypeUser,
		Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x", 10000)}},
	}
	toolCtx := tool.NewToolUseContext(context.Background())
	toolCtx.Options.MainLoopModel = "claude-sonnet-4-6"

	require.True(t, shouldAutoCompact([]types.Message{large}, nil, toolCtx))

	t.Setenv("DISABLE_AUTO_COMPACT", "1")
	require.False(t, shouldAutoCompact([]types.Message{large}, nil, toolCtx))
}

func TestQueryCompactDecisionStopsAfterRepeatedFailures(t *testing.T) {
	t.Setenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", "1")
	large := types.Message{
		Type:    types.MessageTypeUser,
		Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x", 10000)}},
	}
	toolCtx := tool.NewToolUseContext(context.Background())
	tracking := &AutoCompactTrackingState{ConsecutiveFailures: 3}

	require.False(t, shouldAutoCompact([]types.Message{large}, tracking, toolCtx))
}

func TestRecoveryHelpersDetectImagesAndStripThinkingBlocks(t *testing.T) {
	assert.True(t, isImageSizeError(fmt.Errorf("image exceeds maximum size")))
	assert.True(t, isImageResizeError(fmt.Errorf("failed to resize image")))

	messages := []types.Message{
		{
			Type: types.MessageTypeAssistant,
			Content: []types.ContentBlock{
				{Type: "thinking", Text: "hidden"},
				{Type: "text", Text: "visible"},
				{Type: "redacted_thinking"},
			},
		},
	}

	stripped := stripThinkingBlocks(messages)
	require.Len(t, stripped[0].Content, 1)
	assert.Equal(t, "text", stripped[0].Content[0].Type)

	preserved := preserveThinkingBlocks(messages)
	require.Len(t, preserved[0].Content, 2)
	assert.Equal(t, "thinking", preserved[0].Content[0].Type)
	assert.Equal(t, "text", preserved[0].Content[1].Type)
}

package query

import (
	"context"

	corehooks "claude-codex/internal/harness/hooks"
	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
)

// handleStopHooks executes stop hooks and returns the result.
// It also triggers session memory extraction when thresholds are met.
func handleStopHooks(
	ctx context.Context,
	messagesForQuery []types.Message,
	assistantMessages []types.AssistantMessage,
	SystemPrompt types.SystemPrompt,
	userContext map[string]string,
	systemContext map[string]string,
	toolUseContext *tool.ToolUseContext,
	hookExecutor *corehooks.Executor,
	querySource string,
	stopHookActive *bool,
	eventChan chan<- interface{},
) (*StopHookResult, error) {
	hookResult, err := executeStopHooks(ctx, hookExecutor, &corehooks.HookInput{
		Event:     corehooks.EventStop,
		SessionID: toolContextSessionID(toolUseContext),
		AgentID:   toolContextAgentID(toolUseContext),
		Metadata: map[string]any{
			"query_source":     querySource,
			"message_count":    len(messagesForQuery),
			"stop_hook_active": stopHookActive != nil && *stopHookActive,
		},
	})
	if err != nil {
		return nil, err
	}

	// Execute task/teammate hooks (no-op until Agent/Coordinator is implemented).
	_, _ = executeTaskCompletedHooks(ctx, nil)
	_, _ = executeTeammateIdleHooks(ctx, nil)

	// Session memory extraction: fires in background when thresholds are met.
	// Only runs on the main REPL thread (querySource == "repl_main_thread" or "").
	tryExtractSessionMemory(SessionMemoryExtractionParams{
		Ctx:          ctx,
		Messages:     messagesForQuery,
		ToolUseCtx:   toolUseContext,
		QuerySource:  querySource,
		SystemPrompt: SystemPrompt,
		UserContext:  userContext,
		SystemCtx:    systemContext,
	})

	result := &StopHookResult{
		BlockingErrors:      []types.Message{},
		PreventContinuation: hookResult != nil && !hookResult.Continue,
	}
	if hookResult != nil {
		for _, blocking := range hookResult.BlockingErrors {
			result.BlockingErrors = append(result.BlockingErrors, createUserMessage(blocking, true))
		}
	}
	return result, nil
}

// executeStopHooks runs configured stop hooks (shell hooks).
func executeStopHooks(ctx context.Context, executor *corehooks.Executor, input *corehooks.HookInput) (*corehooks.AggregatedResult, error) {
	if executor == nil {
		return &corehooks.AggregatedResult{Continue: true}, nil
	}
	return executor.Execute(ctx, corehooks.EventStop, input)
}

func toolContextSessionID(ctx *tool.ToolUseContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.SessionID
}

func toolContextAgentID(ctx *tool.ToolUseContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.AgentID
}

// executeTaskCompletedHooks runs task completed hooks.
func executeTaskCompletedHooks(ctx context.Context, hookContext interface{}) ([]HookResult, error) {
	// TODO: Integrate with the hooks.Registry once available.
	return nil, nil
}

// executeTeammateIdleHooks runs teammate idle hooks.
func executeTeammateIdleHooks(ctx context.Context, hookContext interface{}) ([]HookResult, error) {
	// TODO: Integrate with the hooks.Registry once available.
	return nil, nil
}

// HookResult represents the result of a hook execution.
type HookResult struct {
	BlockingError       string
	PreventContinuation bool
	StopReason          string
}

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corehooks "claude-codex/internal/harness/hooks"
	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
)

var errToolHookStopped = errors.New("tool execution stopped by hook")

type toolHookLifecycle struct {
	executor    *corehooks.Executor
	toolCtx     *tool.ToolUseContext
	querySource string
}

func newToolHookLifecycle(executor *corehooks.Executor, toolCtx *tool.ToolUseContext, querySource string) tool.ExecutionLifecycle {
	if executor == nil {
		return nil
	}
	return &toolHookLifecycle{executor: executor, toolCtx: toolCtx, querySource: querySource}
}

func (l *toolHookLifecycle) BeforeTool(ctx context.Context, toolUseID, toolName string, input map[string]interface{}) (map[string]interface{}, error) {
	baseInput := cloneToolInput(input)
	result, err := l.executor.Execute(ctx, corehooks.EventPreToolUse, l.hookInput(
		corehooks.EventPreToolUse,
		toolUseID,
		toolName,
		baseInput,
		nil,
		nil,
	))
	if err != nil {
		return nil, err
	}
	if reason := blockingToolHookReason(result); reason != "" {
		return nil, fmt.Errorf("%w: %s", errToolHookStopped, reason)
	}
	for key, value := range result.UpdatedInput {
		baseInput[key] = value
	}
	if result.PermissionBehavior == "deny" {
		reason := result.PermissionDecisionReason
		if reason == "" {
			reason = "permission denied by PreToolUse hook"
		}
		return nil, fmt.Errorf("%w: %s", errToolHookStopped, reason)
	}
	if result.PermissionBehavior == "ask" && l.toolCtx != nil && l.toolCtx.IsNonInteractiveSession {
		reason := result.PermissionDecisionReason
		if reason == "" {
			reason = "PreToolUse hook requires interactive permission"
		}
		return nil, fmt.Errorf("%w: %s", errToolHookStopped, reason)
	}
	return baseInput, nil
}

func (l *toolHookLifecycle) AfterTool(
	ctx context.Context,
	toolUseID, toolName string,
	input map[string]interface{},
	result *tool.ToolResult,
	executionErr error,
) (*tool.ToolResult, error) {
	event := corehooks.EventPostToolUse
	if executionErr != nil {
		event = corehooks.EventPostToolUseFailure
	}
	hookResult, err := l.executor.Execute(ctx, event, l.hookInput(
		event,
		toolUseID,
		toolName,
		cloneToolInput(input),
		result,
		executionErr,
	))
	if err != nil {
		return result, err
	}
	if reason := blockingToolHookReason(hookResult); reason != "" {
		return result, fmt.Errorf("%w: %s", errToolHookStopped, reason)
	}
	if hookResult.UpdatedMCPToolOutput != nil {
		if result == nil {
			result = &tool.ToolResult{}
		}
		result.Data = hookResult.UpdatedMCPToolOutput
	}
	return result, nil
}

func (l *toolHookLifecycle) hookInput(
	event corehooks.HookEvent,
	toolUseID, toolName string,
	input map[string]interface{},
	result *tool.ToolResult,
	executionErr error,
) *corehooks.HookInput {
	output := ""
	if result != nil {
		output = toolResultContent(result)
	}
	workingDir := ""
	if l.toolCtx != nil && l.toolCtx.Callbacks != nil && l.toolCtx.Callbacks.GetWorkingDirectory != nil {
		workingDir = l.toolCtx.Callbacks.GetWorkingDirectory()
	}
	return &corehooks.HookInput{
		Event:      event,
		SessionID:  toolContextSessionID(l.toolCtx),
		WorkingDir: workingDir,
		AgentID:    toolContextAgentID(l.toolCtx),
		Tool: &corehooks.ToolInfo{
			Name:   toolName,
			Input:  input,
			Output: output,
			Error:  executionErr,
			IsMCP:  strings.HasPrefix(toolName, "mcp__"),
		},
		Metadata: map[string]any{
			"tool_use_id":  toolUseID,
			"query_source": l.querySource,
		},
	}
}

func cloneToolInput(input map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func blockingToolHookReason(result *corehooks.AggregatedResult) string {
	if result == nil {
		return ""
	}
	if !result.Continue {
		if result.StopReason != "" {
			return result.StopReason
		}
		return "hook prevented tool execution"
	}
	if len(result.BlockingErrors) > 0 {
		return strings.Join(result.BlockingErrors, "; ")
	}
	return ""
}

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

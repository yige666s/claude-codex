package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AgentToolResult holds the completed result of a background agent tool invocation.
type AgentToolResult struct {
	AgentID     AgentID
	Output      string
	Metadata    map[string]interface{}
	TokensUsed  int
	TurnCount   int
	ToolCount   int
}

// AsyncLifecycleConfig configures runAsyncAgentLifecycle.
type AsyncLifecycleConfig struct {
	TaskID        string
	Description   string
	MakeStream    func(ctx context.Context) (<-chan Message, error)
	OnProgress    func(update ProgressUpdate)
	OnComplete    func(result *AgentToolResult)
	OnKilled      func(taskID string, partial *string)
	OnFailed      func(taskID string, err error)
}

// filterIncompleteToolCalls removes messages that contain tool_use blocks
// without a matching tool_result in the conversation history.
//
// This prevents the resumed agent from seeing dangling tool calls that were
// never answered. Mirrors filterIncompleteToolCalls in runAgent.ts.
func filterIncompleteToolCalls(messages []Message) []Message {
	// Collect all tool_result IDs present in the history.
	resultIDs := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "tool_result" && block.ToolUseID != "" {
				resultIDs[block.ToolUseID] = true
			}
		}
	}

	// Keep only messages that don't have unmatched tool_use blocks.
	var result []Message
	for _, msg := range messages {
		if msg.Role != "assistant" {
			result = append(result, msg)
			continue
		}
		hasUnmatched := false
		for _, block := range msg.Content {
			if block.Type == "tool_use" && block.ToolID != "" && !resultIDs[block.ToolID] {
				hasUnmatched = true
				break
			}
		}
		if !hasUnmatched {
			result = append(result, msg)
		}
	}
	return result
}

// finalizeAgentTool builds the AgentToolResult from a completed message stream.
// Mirrors finalizeAgentTool in agentToolUtils.ts.
func finalizeAgentTool(messages []Message, agentID AgentID, metadata map[string]interface{}) *AgentToolResult {
	output := extractTextOutput(messages)
	tokenCount := 0
	toolCount := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				toolCount++
			}
		}
	}
	return &AgentToolResult{
		AgentID:    agentID,
		Output:     output,
		Metadata:   metadata,
		TurnCount:  countAssistantMessages(messages),
		ToolCount:  toolCount,
		TokensUsed: tokenCount,
	}
}

// extractPartialResult extracts whatever output is available when an async agent
// is killed mid-run. Returns nil when nothing useful is available.
// Mirrors extractPartialResult in agentToolUtils.ts.
func extractPartialResult(messages []Message) *string {
	// Walk backwards to find the last meaningful assistant text.
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "assistant" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				s := strings.TrimSpace(block.Text)
				return &s
			}
		}
	}
	return nil
}

// runAsyncAgentLifecycle drives a background agent from spawn to task-notification.
// It starts the stream, accumulates messages, and calls the appropriate callback
// on completion, kill, or failure.
//
// Mirrors runAsyncAgentLifecycle in agentToolUtils.ts.
func runAsyncAgentLifecycle(ctx context.Context, cfg AsyncLifecycleConfig) {
	stream, err := cfg.MakeStream(ctx)
	if err != nil {
		if cfg.OnFailed != nil {
			cfg.OnFailed(cfg.TaskID, fmt.Errorf("failed to start agent stream: %w", err))
		}
		return
	}

	var accumulated []Message
	tokenCount := 0
	toolCount := 0

	agentID := AgentID(cfg.TaskID)

	for {
		select {
		case <-ctx.Done():
			// Context cancelled — treat as kill.
			partial := extractPartialResult(accumulated)
			if cfg.OnKilled != nil {
				cfg.OnKilled(cfg.TaskID, partial)
			}
			return

		case msg, ok := <-stream:
			if !ok {
				// Stream closed — agent completed.
				result := finalizeAgentTool(accumulated, agentID, map[string]interface{}{
					"description": cfg.Description,
					"taskId":      cfg.TaskID,
				})
				result.TokensUsed = tokenCount
				result.ToolCount = toolCount
				if cfg.OnComplete != nil {
					cfg.OnComplete(result)
				}
				return
			}

			accumulated = append(accumulated, msg)

			// Track tool usage for progress.
			for _, block := range msg.Content {
				if block.Type == "tool_use" {
					toolCount++
				}
			}

			// Emit progress updates.
			if cfg.OnProgress != nil && msg.Role == "assistant" {
				cfg.OnProgress(ProgressUpdate{
					AgentID:    agentID,
					TurnNumber: countAssistantMessages(accumulated),
					Status:     StatusRunning,
					Summary:    buildProgressSummary(accumulated, toolCount),
					Timestamp:  time.Now(),
				})
			}
		}
	}
}

// buildProgressSummary generates a brief progress line for an in-flight agent.
func buildProgressSummary(messages []Message, toolCount int) string {
	turns := countAssistantMessages(messages)
	if toolCount > 0 {
		return fmt.Sprintf("Turn %d, %d tool calls", turns, toolCount)
	}
	return fmt.Sprintf("Turn %d", turns)
}

// countAssistantMessages counts messages with role "assistant".
func countAssistantMessages(messages []Message) int {
	n := 0
	for _, m := range messages {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

// extractTextOutput collects all assistant text blocks into a single string.
func extractTextOutput(messages []Message) string {
	var parts []string
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				parts = append(parts, strings.TrimSpace(block.Text))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

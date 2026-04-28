package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	workctx "claude-codex/internal/harness/context"
	"claude-codex/internal/harness/messages"
	queryengine "claude-codex/internal/harness/queryengine"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	publictypes "claude-codex/internal/public/types"
)

const defaultQueryFallbackModel = "claude-sonnet-4-6"

type queryRuntime struct {
	engine *Engine
}

func newQueryRuntime(engine *Engine) engineRuntime {
	return &queryRuntime{engine: engine}
}

func (r *queryRuntime) Descriptors() []toolkit.Descriptor {
	return r.engine.registry.Descriptors()
}

func (r *queryRuntime) ExecuteTool(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error) {
	tool, err := r.engine.registry.Get(name)
	if err != nil {
		return toolkit.Result{}, err
	}
	if r.engine.permissions != nil {
		if err := r.engine.permissions.AuthorizeRequest(ctx, buildPermissionRequest(tool.Name(), tool.Permission(), input)); err != nil {
			return toolkit.Result{}, err
		}
	}
	return tool.Execute(ctx, input)
}

func (r *queryRuntime) Run(ctx context.Context, session *state.Session, prompt string, recordUserMessage bool) (Result, error) {
	if session == nil {
		return Result{}, fmt.Errorf("session is required")
	}

	interactionID := fmt.Sprintf("interaction-%d", time.Now().UnixNano())
	r.engine.recordTrace(session.ID, "interaction.start", "interaction", map[string]any{
		"span_id":       interactionID,
		"prompt":        prompt,
		"prompt_length": len(prompt),
		"prompt_source": promptSource(recordUserMessage),
		"working_dir":   session.WorkingDir,
		"runtime":       "queryengine",
	})

	engine := queryengine.NewQueryEngine(queryengine.QueryEngineConfig{
		Cwd:                    runtimeWorkingDir(r.engine.workingDir, session.WorkingDir),
		SessionID:              session.ID,
		InitialMessages:        r.initialQueryMessages(session),
		FallbackModel:          defaultQueryFallbackModel,
		ReplayUserMessages:     false,
		IncludePartialMessages: false,
		Planner:                r.engine.planner,
		ToolDescriptors:        r.engine.registry.Descriptors(),
		ExecuteTool: func(ctx context.Context, name string, input []byte) (string, error) {
			result, err := r.ExecuteTool(ctx, name, json.RawMessage(input))
			return result.Output, err
		},
		MaxTurns: r.engine.maxTurns,
	})

	stream, err := engine.SubmitMessage(ctx, prompt, &queryengine.SubmitOptions{
		IsMeta: !recordUserMessage,
	})
	if err != nil {
		r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
			"span_id": interactionID,
			"status":  "error",
			"error":   err.Error(),
			"runtime": "queryengine",
		})
		return Result{}, err
	}

	var final queryengine.SDKMessage
	for msg := range stream {
		final = msg
	}

	if final.IsError && len(final.Errors) > 0 {
		err := errors.New(strings.Join(final.Errors, "; "))
		r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
			"span_id": interactionID,
			"status":  "error",
			"error":   err.Error(),
			"runtime": "queryengine",
		})
		return Result{}, err
	}

	syncSessionFromQueryMessages(session, engine.GetMessages())
	output := lastAssistantMessage(session.Messages)
	if output == "" {
		output = final.Result
	}

	r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
		"span_id":         interactionID,
		"status":          "ok",
		"output_chars":    len(output),
		"tool_call_count": countAssistantToolCalls(session.Messages),
		"runtime":         "queryengine",
	})

	return Result{
		Output:  output,
		Session: session,
	}, nil
}

func runtimeWorkingDir(engineDir, sessionDir string) string {
	if engineDir != "" {
		return engineDir
	}
	return sessionDir
}

func (r *queryRuntime) initialQueryMessages(session *state.Session) []queryengine.Message {
	initial := sessionToQueryMessages(session)
	if session == nil || len(session.Messages) != 0 || r.engine.workingDir == "" {
		return initial
	}

	wsCtx := workctx.Collect(r.engine.workingDir)
	initial = append(initial, queryengine.Message{
		Type:      "user",
		Timestamp: time.Now().UTC(),
		UUID:      fmt.Sprintf("%s-workspace", session.ID),
		IsMeta:    true,
		Content:   wsCtx.SystemPrompt(),
	})
	initial = append(initial, queryengine.Message{
		Type:      "assistant",
		Timestamp: time.Now().UTC(),
		UUID:      fmt.Sprintf("%s-workspace-ack", session.ID),
		Content:   "Understood. I have the workspace context.",
	})

	if r.engine.skillManager != nil {
		if r.engine.skillListingManager == nil {
			r.engine.skillListingManager = messages.NewSkillListingManager()
		}
		allSkills := r.engine.skillManager.ListUserInvocableSkills()
		attachment := r.engine.skillListingManager.GetSkillListingAttachment(allSkills, 200000)
		if attachment != nil {
			initial = append(initial, queryengine.Message{
				Type:      "user",
				Timestamp: time.Now().UTC(),
				UUID:      fmt.Sprintf("%s-skills", session.ID),
				IsMeta:    true,
				Content:   attachment.ToSystemReminder(),
			})
		}
	}

	return initial
}

func sessionToQueryMessages(session *state.Session) []queryengine.Message {
	if session == nil || len(session.Messages) == 0 {
		return nil
	}

	out := make([]queryengine.Message, 0, len(session.Messages))
	for i, msg := range session.Messages {
		out = append(out, stateToQueryMessage(session.ID, i, msg))
	}
	return out
}

func stateToQueryMessage(sessionID string, idx int, msg state.Message) queryengine.Message {
	timestamp := msg.CreatedAt
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	queryMsg := queryengine.Message{
		UUID:      fmt.Sprintf("%s-%d", sessionID, idx),
		Timestamp: timestamp,
	}

	switch msg.Role {
	case "assistant":
		queryMsg.Type = "assistant"
		queryMsg.Content = assistantContentBlocks(msg)
	case "tool":
		queryMsg.Type = "tool"
		queryMsg.ToolUseID = msg.ToolCallID
		queryMsg.Content = []publictypes.ContentBlock{{
			Type:      "tool_result",
			ToolUseID: msg.ToolCallID,
			Content:   msg.ToolOutput,
		}}
		queryMsg.Message = map[string]any{
			"tool_name":  msg.ToolName,
			"tool_input": rawMessageToMap(msg.ToolInput),
		}
	default:
		queryMsg.Type = "user"
		queryMsg.Content = msg.Content
		queryMsg.IsMeta = msg.Hidden
	}

	return queryMsg
}

func assistantContentBlocks(msg state.Message) interface{} {
	if len(msg.ToolCalls) == 0 {
		return msg.Content
	}

	blocks := make([]publictypes.ContentBlock, 0, len(msg.ToolCalls)+1)
	if strings.TrimSpace(msg.Content) != "" {
		blocks = append(blocks, publictypes.ContentBlock{
			Type: "text",
			Text: msg.Content,
		})
	}
	for _, call := range msg.ToolCalls {
		blocks = append(blocks, publictypes.ContentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: rawMessageToMap(call.Input),
		})
	}
	return blocks
}

func rawMessageToMap(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}

	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func syncSessionFromQueryMessages(session *state.Session, messages []queryengine.Message) {
	converted := make([]state.Message, 0, len(messages))
	usage := state.Usage{}
	updatedAt := session.StartedAt

	for _, msg := range messages {
		convertedMsg, ok := queryToStateMessage(msg)
		if !ok {
			continue
		}
		converted = append(converted, convertedMsg)
		recordSessionUsage(&usage, convertedMsg)
		if convertedMsg.CreatedAt.After(updatedAt) {
			updatedAt = convertedMsg.CreatedAt
		}
	}

	session.Messages = converted
	session.Usage = usage
	if !updatedAt.IsZero() {
		session.UpdatedAt = updatedAt
	}
}

func queryToStateMessage(msg queryengine.Message) (state.Message, bool) {
	createdAt := msg.Timestamp
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	switch msg.Type {
	case "user":
		if toolMsg, ok := queryToolResultUserMessage(msg, createdAt); ok {
			return toolMsg, true
		}
		return state.Message{
			Role:      "user",
			Content:   contentText(msg.Content),
			Hidden:    msg.IsMeta,
			CreatedAt: createdAt,
		}, true
	case "assistant":
		content, toolCalls := queryAssistantContent(msg.Content)
		return state.Message{
			Role:      "assistant",
			Content:   content,
			ToolCalls: toolCalls,
			CreatedAt: createdAt,
		}, true
	case "tool":
		toolName, toolInput := queryToolMetadata(msg.Message)
		return state.Message{
			Role:       "tool",
			ToolCallID: msg.ToolUseID,
			ToolName:   toolName,
			ToolInput:  toolInput,
			ToolOutput: queryToolOutput(msg.Content),
			CreatedAt:  createdAt,
		}, true
	default:
		return state.Message{}, false
	}
}

func queryToolResultUserMessage(msg queryengine.Message, createdAt time.Time) (state.Message, bool) {
	blocks, ok := msg.Content.([]publictypes.ContentBlock)
	if !ok {
		return state.Message{}, false
	}
	for _, block := range blocks {
		if block.Type != "tool_result" {
			continue
		}
		toolName, toolInput := queryToolMetadata(msg.Message)
		return state.Message{
			Role:       "tool",
			ToolCallID: block.ToolUseID,
			ToolName:   toolName,
			ToolInput:  toolInput,
			ToolOutput: firstNonEmpty(block.Content, block.Text),
			CreatedAt:  createdAt,
		}, true
	}
	return state.Message{}, false
}

func contentText(content interface{}) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []publictypes.ContentBlock:
		parts := make([]string, 0, len(typed))
		for _, block := range typed {
			if block.Type == "text" && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func queryAssistantContent(content interface{}) (string, []state.ToolCall) {
	switch typed := content.(type) {
	case string:
		return typed, nil
	case []publictypes.ContentBlock:
		textParts := make([]string, 0, len(typed))
		toolCalls := make([]state.ToolCall, 0)
		for _, block := range typed {
			switch block.Type {
			case "text":
				if block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			case "tool_use":
				input, _ := json.Marshal(block.Input)
				toolCalls = append(toolCalls, state.ToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: input,
				})
			}
		}
		return strings.Join(textParts, "\n"), toolCalls
	default:
		return "", nil
	}
}

func queryToolMetadata(meta interface{}) (string, json.RawMessage) {
	values, ok := meta.(map[string]any)
	if !ok {
		return "", nil
	}

	name, _ := values["tool_name"].(string)
	inputMap, _ := values["tool_input"].(map[string]any)
	if inputMap == nil {
		return name, nil
	}

	data, err := json.Marshal(inputMap)
	if err != nil {
		return name, nil
	}
	return name, data
}

func queryToolOutput(content interface{}) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []publictypes.ContentBlock:
		for _, block := range typed {
			if block.Type == "tool_result" && block.Content != "" {
				return block.Content
			}
			if block.Type == "text" && block.Text != "" {
				return block.Text
			}
		}
	}
	return ""
}

func recordSessionUsage(usage *state.Usage, message state.Message) {
	switch message.Role {
	case "user":
		usage.RecordInput(message.Content)
	case "assistant":
		usage.RecordOutput(message.Content)
	case "tool":
		usage.RecordOutput(message.ToolOutput)
	}
}

func lastAssistantMessage(messages []state.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}

func countAssistantToolCalls(messages []state.Message) int {
	total := 0
	for _, msg := range messages {
		if msg.Role == "assistant" {
			total += len(msg.ToolCalls)
		}
	}
	return total
}

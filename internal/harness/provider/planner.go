package provider

import (
	"context"
	"encoding/json"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type Planner struct {
	provider Provider
	model    string
}

func NewPlanner(provider Provider, model string) *Planner {
	return &Planner{provider: provider, model: model}
}

func (p *Planner) Next(ctx context.Context, session *state.Session, tools []toolkit.Descriptor) (engine.Plan, error) {
	request := MessageRequest{
		Model:     p.model,
		MaxTokens: 8096,
		Messages:  toProviderMessages(session.Messages),
		Tools:     toProviderTools(tools),
	}

	response, err := p.provider.CreateMessage(ctx, request)
	if err != nil {
		return engine.Plan{}, err
	}

	return planFromResponse(response), nil
}

func toProviderMessages(messages []state.Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			if msg.Content == "" {
				continue
			}
			out = append(out, Message{
				Role:    "user",
				Content: msg.Content,
			})
		case "assistant":
			message := Message{
				Role:    "assistant",
				Content: msg.Content,
			}
			if len(msg.ToolCalls) > 0 {
				message.ToolCalls = make([]ToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					message.ToolCalls[i] = ToolCall{
						ID:    tc.ID,
						Name:  tc.Name,
						Input: tc.Input,
					}
				}
			}
			out = append(out, message)
		case "tool":
			out = append(out, Message{
				Role:       "tool",
				Content:    msg.ToolOutput,
				ToolCallID: msg.ToolCallID,
			})
		}
	}
	return out
}

func toProviderTools(descs []toolkit.Descriptor) []Tool {
	if len(descs) == 0 {
		return nil
	}
	tools := make([]Tool, len(descs))
	for i, d := range descs {
		var schema map[string]interface{}
		if len(d.InputSchema) > 0 {
			_ = json.Unmarshal(d.InputSchema, &schema)
		}
		if len(schema) == 0 {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}
		tools[i] = Tool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schema,
		}
	}
	return tools
}

func planFromResponse(resp *MessageResponse) engine.Plan {
	if resp == nil {
		return engine.Plan{}
	}
	toolCalls := make([]engine.ToolCall, len(resp.ToolCalls))
	for i, tc := range resp.ToolCalls {
		toolCalls[i] = engine.ToolCall{
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		}
	}

	text := ""
	for _, block := range resp.Content {
		if block.Type == "text" && block.Text != "" {
			if text != "" {
				text += "\n"
			}
			text += block.Text
		}
	}

	stopReason := resp.StopReason
	if len(toolCalls) > 0 && stopReason == "" {
		stopReason = "tool_use"
	}
	if len(toolCalls) == 0 && stopReason == "" {
		stopReason = "end_turn"
	}

	return engine.Plan{
		AssistantText: text,
		ToolCalls:     toolCalls,
		StopReason:    stopReason,
	}
}

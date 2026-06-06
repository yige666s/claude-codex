package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"claude-codex/internal/harness/plannerapi"
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

func (p *Planner) Next(ctx context.Context, session *state.Session, tools []toolkit.Descriptor) (plannerapi.Plan, error) {
	request := MessageRequest{
		Model:          p.model,
		MaxTokens:      8096,
		Messages:       toProviderMessages(session.Messages),
		Tools:          toProviderTools(tools),
		ThinkingConfig: ThinkingConfigFromContext(ctx),
	}

	response, err := p.provider.CreateMessage(ctx, request)
	if err != nil {
		return plannerapi.Plan{}, err
	}

	return validateProviderPlan(p.provider, p.model, planFromResponse(response))
}

func (p *Planner) StreamNext(ctx context.Context, session *state.Session, tools []toolkit.Descriptor, onChunk func(string)) (plannerapi.Plan, error) {
	if streaming, ok := p.provider.(StreamingProvider); ok {
		request := MessageRequest{
			Model:          p.model,
			MaxTokens:      8096,
			Messages:       toProviderMessages(session.Messages),
			Tools:          toProviderTools(tools),
			Stream:         true,
			ThinkingConfig: ThinkingConfigFromContext(ctx),
		}
		response, err := streaming.StreamMessage(ctx, request, onChunk)
		if err != nil {
			if errors.Is(err, ErrNoStreamCandidates) {
				return p.Next(ctx, session, tools)
			}
			return plannerapi.Plan{}, err
		}
		return validateProviderPlan(p.provider, p.model, planFromResponse(response))
	}
	plan, err := p.Next(ctx, session, tools)
	if err != nil {
		return plannerapi.Plan{}, err
	}
	if onChunk != nil && plan.AssistantText != "" {
		onChunk(plan.AssistantText)
	}
	return plan, nil
}

func validateProviderPlan(provider Provider, model string, plan plannerapi.Plan) (plannerapi.Plan, error) {
	if strings.TrimSpace(plan.AssistantText) != "" || len(plan.ToolCalls) > 0 {
		return plan, nil
	}
	providerName := "unknown"
	if provider != nil {
		providerName = provider.Name()
	}
	return plannerapi.Plan{}, fmt.Errorf("%s/%s empty response: no assistant text or tool calls", providerName, model)
}

func toProviderMessages(messages []state.Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			content := providerMessageContent(msg)
			if content == nil {
				continue
			}
			out = append(out, Message{
				Role:    "user",
				Content: content,
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
						ID:               tc.ID,
						Name:             tc.Name,
						Input:            tc.Input,
						ThoughtSignature: tc.ThoughtSignature,
					}
				}
			}
			out = append(out, message)
		case "tool":
			out = append(out, Message{
				Role:       "tool",
				Content:    msg.ToolOutput,
				ToolCallID: msg.ToolCallID,
				ToolName:   msg.ToolName,
			})
		}
	}
	return out
}

func providerMessageContent(msg state.Message) interface{} {
	if len(msg.ContentBlocks) == 0 {
		if msg.Content == "" {
			return nil
		}
		return msg.Content
	}
	blocks := make([]ContentBlock, 0, len(msg.ContentBlocks))
	for _, block := range msg.ContentBlocks {
		blocks = append(blocks, ContentBlock{
			Type:   block.Type,
			Text:   firstNonEmptyContent(block.Text, block.Content),
			Source: block.Source,
		})
	}
	if len(blocks) == 1 && blocks[0].Type == "text" && blocks[0].Text != "" {
		return blocks[0].Text
	}
	return blocks
}

func firstNonEmptyContent(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

func planFromResponse(resp *MessageResponse) plannerapi.Plan {
	if resp == nil {
		return plannerapi.Plan{}
	}
	toolCalls := make([]plannerapi.ToolCall, len(resp.ToolCalls))
	for i, tc := range resp.ToolCalls {
		toolCalls[i] = plannerapi.ToolCall{
			ID:               tc.ID,
			Name:             tc.Name,
			Input:            tc.Input,
			ThoughtSignature: tc.ThoughtSignature,
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

	return plannerapi.Plan{
		AssistantText: text,
		ToolCalls:     toolCalls,
		StopReason:    stopReason,
	}
}

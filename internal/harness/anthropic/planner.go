package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ding/claude-code/claude-go/internal/harness/engine"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Planner struct {
	client *Client
	model  string
}

func NewPlanner(client *Client, model string) *Planner {
	return &Planner{client: client, model: model}
}

// Next calls the Anthropic API synchronously and returns the next plan (text or tool calls).
func (p *Planner) Next(ctx context.Context, session *state.Session, tools []toolkit.Descriptor) (engine.Plan, error) {
	request := MessageRequest{
		Model:     p.model,
		MaxTokens: 8096,
		Messages:  toAnthropicMessages(session.Messages),
		Tools:     toAnthropicTools(tools),
	}

	response, err := p.client.CreateMessage(ctx, request)
	if err != nil {
		return engine.Plan{}, err
	}

	return planFromBlocks(response.Content, response.StopReason), nil
}

// StreamNext streams the Anthropic response, calling onChunk for each text delta.
// If the model invokes tools, it returns a Plan with ToolCalls populated.
func (p *Planner) StreamNext(ctx context.Context, session *state.Session, tools []toolkit.Descriptor, onChunk func(string)) (engine.Plan, error) {
	request := MessageRequest{
		Model:     p.model,
		MaxTokens: 8096,
		Messages:  toAnthropicMessages(session.Messages),
		Tools:     toAnthropicTools(tools),
	}

	events, errs := p.client.StreamMessages(ctx, request)

	var textSB strings.Builder
	stopReason := "end_turn"

	// Track in-progress tool_use blocks during streaming
	type streamToolCall struct {
		id    string
		name  string
		input strings.Builder
	}
	var toolCalls []streamToolCall
	var currentToolIdx = -1

	for event := range events {
		switch event.Event {
		case "content_block_start":
			var block struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal(event.Data, &block); err == nil {
				if block.ContentBlock.Type == "tool_use" {
					toolCalls = append(toolCalls, streamToolCall{
						id:   block.ContentBlock.ID,
						name: block.ContentBlock.Name,
					})
					currentToolIdx = len(toolCalls) - 1
				}
			}
		case "content_block_delta":
			var delta struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal(event.Data, &delta); err == nil {
				switch delta.Delta.Type {
				case "text_delta":
					if delta.Delta.Text != "" {
						textSB.WriteString(delta.Delta.Text)
						onChunk(delta.Delta.Text)
					}
				case "input_json_delta":
					if currentToolIdx >= 0 && delta.Delta.PartialJSON != "" {
						toolCalls[currentToolIdx].input.WriteString(delta.Delta.PartialJSON)
					}
				}
			}
		case "message_delta":
			var md struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
			}
			if err := json.Unmarshal(event.Data, &md); err == nil && md.Delta.StopReason != "" {
				stopReason = md.Delta.StopReason
			}
		case "error":
			var apiErr struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(event.Data, &apiErr); err == nil {
				return engine.Plan{}, fmt.Errorf("stream error: %s", apiErr.Error.Message)
			}
		}
	}

	if err := <-errs; err != nil {
		return engine.Plan{}, err
	}

	// Convert accumulated tool calls
	var engineToolCalls []engine.ToolCall
	for _, tc := range toolCalls {
		inputJSON := tc.input.String()
		if inputJSON == "" {
			inputJSON = "{}"
		}
		engineToolCalls = append(engineToolCalls, engine.ToolCall{
			ID:    tc.id,
			Name:  tc.name,
			Input: json.RawMessage(inputJSON),
		})
	}

	return engine.Plan{
		AssistantText: textSB.String(),
		ToolCalls:     engineToolCalls,
		StopReason:    stopReason,
	}, nil
}

// planFromBlocks converts Anthropic response content blocks into an engine Plan.
func planFromBlocks(blocks []ContentBlock, stopReason string) engine.Plan {
	var textParts []string
	var toolCalls []engine.ToolCall

	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			toolCalls = append(toolCalls, engine.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}

	return engine.Plan{
		AssistantText: strings.TrimSpace(strings.Join(textParts, "\n")),
		ToolCalls:     toolCalls,
		StopReason:    stopReason,
	}
}

// toAnthropicTools converts toolkit descriptors to Anthropic tool definitions.
func toAnthropicTools(descs []toolkit.Descriptor) []Tool {
	if len(descs) == 0 {
		return nil
	}
	tools := make([]Tool, len(descs))
	for i, d := range descs {
		schema := d.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools[i] = Tool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: schema,
		}
	}
	return tools
}

// toAnthropicMessages converts session messages into Anthropic API format.
// Tool results are grouped into user messages and tool-use calls are embedded
// as content blocks in the assistant message they belong to.
func toAnthropicMessages(messages []state.Message) []InputMessage {
	var result []InputMessage

	// Collect pending tool results to batch into a single user message
	var pendingToolResults []ContentBlock
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		result = append(result, InputMessage{
			Role:    "user",
			Content: pendingToolResults,
		})
		pendingToolResults = nil
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			flushToolResults()
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			result = append(result, InputMessage{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: msg.Content},
				},
			})
		case "assistant":
			flushToolResults()
			// Build content blocks for assistant message
			var blocks []ContentBlock
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, ContentBlock{
					Type: "text",
					Text: msg.Content,
				})
			}
			// Add tool_use blocks if present
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, ContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
			if len(blocks) > 0 {
				result = append(result, InputMessage{
					Role:    "assistant",
					Content: blocks,
				})
			}
		case "tool":
			// Tool results batch up as content blocks in a user message
			pendingToolResults = append(pendingToolResults, ContentBlock{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   msg.ToolOutput,
			})
		}
	}
	flushToolResults()

	return result
}

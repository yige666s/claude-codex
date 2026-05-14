package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const vertexAnthropicVersion = "vertex-2023-10-16"

type vertexAnthropicRequest struct {
	AnthropicVersion string                   `json:"anthropic_version"`
	MaxTokens        int                      `json:"max_tokens,omitempty"`
	Stream           bool                     `json:"stream"`
	Temperature      float64                  `json:"temperature,omitempty"`
	TopP             float64                  `json:"top_p,omitempty"`
	System           string                   `json:"system,omitempty"`
	Tools            []vertexAnthropicTool    `json:"tools,omitempty"`
	Messages         []vertexAnthropicMessage `json:"messages"`
}

type vertexAnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type vertexAnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type vertexAnthropicRequestBlock struct {
	Type      string                        `json:"type"`
	Text      string                        `json:"text,omitempty"`
	Source    *vertexAnthropicContentSource `json:"source,omitempty"`
	ID        string                        `json:"id,omitempty"`
	Name      string                        `json:"name,omitempty"`
	Input     interface{}                   `json:"input,omitempty"`
	ToolUseID string                        `json:"tool_use_id,omitempty"`
	Content   interface{}                   `json:"content,omitempty"`
}

type vertexAnthropicContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type vertexAnthropicResponse struct {
	ID         string                         `json:"id"`
	Type       string                         `json:"type"`
	Role       string                         `json:"role"`
	Model      string                         `json:"model"`
	Content    []vertexAnthropicResponseBlock `json:"content"`
	StopReason string                         `json:"stop_reason"`
	Usage      vertexAnthropicUsage           `json:"usage"`
}

type vertexAnthropicResponseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type vertexAnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (p *VertexProvider) createAnthropicMessage(ctx context.Context, request MessageRequest, model vertexModelResource) (*MessageResponse, error) {
	reqBody := vertexAnthropicRequestFromUnified(request)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s:rawPredict", p.endpointBaseURL(model.Location), strings.TrimLeft(model.Path, "/"))
	parsed, statusCode, status, data, err := p.sendAnthropicRawPredict(ctx, url, body)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusUnauthorized {
		if refreshErr := p.refreshAccessToken(ctx); refreshErr == nil {
			parsed, statusCode, status, data, err = p.sendAnthropicRawPredict(ctx, url, body)
			if err != nil {
				return nil, err
			}
		}
	}
	if statusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("vertex anthropic request failed: %s: %s", status, string(data))
	}
	return vertexAnthropicResponseToUnified(request.Model, model.ModelID, *parsed), nil
}

func (p *VertexProvider) sendAnthropicRawPredict(ctx context.Context, url string, body []byte) (*vertexAnthropicResponse, int, string, []byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, "", nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.currentToken())

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, resp.Status, data, nil
	}
	var parsed vertexAnthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, resp.Status, nil, err
	}
	return &parsed, resp.StatusCode, resp.Status, nil, nil
}

func vertexAnthropicRequestFromUnified(request MessageRequest) vertexAnthropicRequest {
	req := vertexAnthropicRequest{
		AnthropicVersion: vertexAnthropicVersion,
		MaxTokens:        request.MaxTokens,
		Stream:           false,
		Temperature:      request.Temperature,
		TopP:             request.TopP,
		System:           request.System,
		Messages:         vertexAnthropicMessagesFromUnified(request.Messages),
	}
	if len(request.Tools) > 0 {
		req.Tools = make([]vertexAnthropicTool, len(request.Tools))
		for i, tool := range request.Tools {
			req.Tools[i] = vertexAnthropicTool{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			}
		}
	}
	return req
}

func vertexAnthropicMessagesFromUnified(messages []Message) []vertexAnthropicMessage {
	out := make([]vertexAnthropicMessage, 0, len(messages))
	var pendingToolResults []vertexAnthropicRequestBlock
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		out = append(out, vertexAnthropicMessage{Role: "user", Content: pendingToolResults})
		pendingToolResults = nil
	}

	for _, msg := range messages {
		if msg.Role == "tool" || msg.ToolCallID != "" {
			if msg.ToolCallID == "" {
				continue
			}
			pendingToolResults = append(pendingToolResults, vertexAnthropicRequestBlock{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   toolResultContent(msg.Content),
			})
			continue
		}

		flushToolResults()
		role := msg.Role
		if role != "assistant" {
			role = "user"
		}
		blocks := vertexAnthropicBlocksFromContent(msg.Content)
		for _, call := range msg.ToolCalls {
			blocks = append(blocks, vertexAnthropicRequestBlock{
				Type:  "tool_use",
				ID:    call.ID,
				Name:  call.Name,
				Input: rawToolInput(call.Input),
			})
		}
		if len(blocks) > 0 {
			out = append(out, vertexAnthropicMessage{Role: role, Content: blocks})
		}
	}
	flushToolResults()
	return out
}

func vertexAnthropicBlocksFromContent(content interface{}) []vertexAnthropicRequestBlock {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []vertexAnthropicRequestBlock{{Type: "text", Text: v}}
	case []ContentBlock:
		blocks := make([]vertexAnthropicRequestBlock, 0, len(v))
		for _, block := range v {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					blocks = append(blocks, vertexAnthropicRequestBlock{Type: "text", Text: block.Text})
				}
			case "image":
				if converted, ok := vertexAnthropicMediaBlock("image", block); ok {
					blocks = append(blocks, converted)
				}
			case "file":
				if converted, ok := vertexAnthropicMediaBlock("document", block); ok {
					blocks = append(blocks, converted)
				}
			}
		}
		return blocks
	default:
		text := geminiTextContent(content)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []vertexAnthropicRequestBlock{{Type: "text", Text: text}}
	}
}

func vertexAnthropicMediaBlock(kind string, block ContentBlock) (vertexAnthropicRequestBlock, bool) {
	mediaType := sourceString(block.Source, "media_type", "mime_type", "mimeType")
	data := sourceString(block.Source, "data", "base64")
	if mediaType != "" && data != "" {
		return vertexAnthropicRequestBlock{
			Type: kind,
			Source: &vertexAnthropicContentSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      data,
			},
		}, true
	}
	url := sourceString(block.Source, "url", "uri", "file_uri", "fileUri")
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return vertexAnthropicRequestBlock{
			Type:   kind,
			Source: &vertexAnthropicContentSource{Type: "url", URL: url},
		}, true
	}
	return vertexAnthropicRequestBlock{}, false
}

func rawToolInput(input json.RawMessage) interface{} {
	if len(input) == 0 {
		return map[string]interface{}{}
	}
	var decoded interface{}
	if err := json.Unmarshal(input, &decoded); err != nil {
		return map[string]interface{}{}
	}
	if decoded == nil {
		return map[string]interface{}{}
	}
	return decoded
}

func vertexAnthropicResponseToUnified(requestModel, modelID string, resp vertexAnthropicResponse) *MessageResponse {
	var content []ContentBlock
	var toolCalls []ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				content = append(content, ContentBlock{Type: "text", Text: block.Text})
			}
		case "tool_use":
			input := block.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}
	model := firstNonEmptyString(resp.Model, requestModel, modelID)
	id := firstNonEmptyString(resp.ID, fmt.Sprintf("vertex-anthropic-%d", time.Now().Unix()))
	role := firstNonEmptyString(resp.Role, "assistant")
	stopReason := resp.StopReason
	if len(toolCalls) > 0 && stopReason == "" {
		stopReason = "tool_use"
	}
	return &MessageResponse{
		ID:         id,
		Model:      model,
		Role:       role,
		Content:    content,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}
}

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

// GeminiProvider implements Provider for Google Gemini API
type GeminiProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	config     Config
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(cfg Config) (*GeminiProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	apiKey := cfg.APIKey
	if apiKey == "" && cfg.Token != "" {
		apiKey = cfg.Token
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}

	return &GeminiProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		config: cfg,
	}, nil
}

// geminiRequest represents Gemini API request format
type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    []geminiSafetySetting  `json:"safetySettings,omitempty"`
	Tools             []geminiTool           `json:"tools,omitempty"`
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *geminiBlob             `json:"inlineData,omitempty"`
	FileData         *geminiFileData         `json:"fileData,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiBlob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"`
}

type geminiGenerationConfig struct {
	Temperature     float64 `json:"temperature,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type geminiSafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// geminiResponse represents Gemini API response format
type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content       geminiContent `json:"content"`
	FinishReason  string        `json:"finishReason"`
	Index         int           `json:"index"`
	SafetyRatings []interface{} `json:"safetyRatings,omitempty"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// CreateMessage sends a message request to Gemini API
func (p *GeminiProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	// Convert unified request to Gemini format
	geminiReq := geminiRequest{
		Contents: make([]geminiContent, 0, len(request.Messages)+1),
		GenerationConfig: geminiGenerationConfig{
			Temperature:     request.Temperature,
			TopP:            request.TopP,
			MaxOutputTokens: request.MaxTokens,
		},
	}

	if request.System != "" {
		geminiReq.SystemInstruction = &geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: request.System}},
		}
	}

	geminiReq.Contents = append(geminiReq.Contents, geminiContentsFromMessages(request.Messages)...)

	// Convert tools if present
	if len(request.Tools) > 0 {
		functionDecls := make([]geminiFunctionDeclaration, len(request.Tools))
		for i, tool := range request.Tools {
			functionDecls[i] = geminiFunctionDeclaration{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			}
		}
		geminiReq.Tools = []geminiTool{
			{FunctionDeclarations: functionDecls},
		}
	}

	// Marshal request
	body, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}

	// Build URL with model and API key
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, request.Model, p.apiKey)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini request failed: %s: %s", resp.Status, string(data))
	}

	// Parse response
	var geminiResp geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, err
	}

	if len(geminiResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	// Convert to unified format
	candidate := geminiResp.Candidates[0]
	var contentBlocks []ContentBlock
	var toolCalls []ToolCall
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			contentBlocks = append(contentBlocks, ContentBlock{
				Type: "text",
				Text: part.Text,
			})
		}
		if part.FunctionCall != nil {
			input, _ := json.Marshal(part.FunctionCall.Args)
			if len(input) == 0 {
				input = []byte(`{}`)
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:    fmt.Sprintf("gemini-call-%d", len(toolCalls)+1),
				Name:  part.FunctionCall.Name,
				Input: json.RawMessage(input),
			})
		}
	}
	stopReason := candidate.FinishReason
	if len(toolCalls) > 0 && stopReason == "" {
		stopReason = "tool_use"
	}

	return &MessageResponse{
		ID:         fmt.Sprintf("gemini-%d", time.Now().Unix()),
		Model:      request.Model,
		Role:       "assistant",
		Content:    contentBlocks,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:  geminiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

func geminiContentsFromMessages(messages []Message) []geminiContent {
	contents := make([]geminiContent, 0, len(messages))
	var pendingFunctionResponses []geminiPart
	flushFunctionResponses := func() {
		if len(pendingFunctionResponses) == 0 {
			return
		}
		contents = append(contents, geminiContent{Role: "user", Parts: pendingFunctionResponses})
		pendingFunctionResponses = nil
	}

	for _, msg := range messages {
		if msg.Role == "tool" || msg.ToolCallID != "" {
			if msg.ToolCallID == "" {
				continue
			}
			name := firstNonEmptyString(msg.ToolName, msg.ToolCallID)
			pendingFunctionResponses = append(pendingFunctionResponses, geminiPart{FunctionResponse: &geminiFunctionResponse{
				Name: name,
				Response: map[string]interface{}{
					"name":    name,
					"content": geminiTextContent(msg.Content),
				},
			}})
			continue
		}

		flushFunctionResponses()
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		parts := geminiPartsFromContent(msg.Content)
		if parts == nil {
			parts = make([]geminiPart, 0, len(msg.ToolCalls)+1)
		}
		for _, call := range msg.ToolCalls {
			args := map[string]interface{}{}
			if len(call.Input) > 0 {
				_ = json.Unmarshal(call.Input, &args)
			}
			parts = append(parts, geminiPart{FunctionCall: &geminiFunctionCall{Name: call.Name, Args: args}})
		}
		if len(parts) > 0 {
			contents = append(contents, geminiContent{Role: role, Parts: parts})
		}
	}
	flushFunctionResponses()
	return contents
}

func geminiPartsFromContent(content interface{}) []geminiPart {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []geminiPart{{Text: v}}
	case []ContentBlock:
		parts := make([]geminiPart, 0, len(v))
		for _, block := range v {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					parts = append(parts, geminiPart{Text: block.Text})
				}
			case "image", "file":
				if part, ok := geminiMediaPart(block); ok {
					parts = append(parts, part)
				}
			}
		}
		return parts
	default:
		text := geminiTextContent(content)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []geminiPart{{Text: text}}
	}
}

func geminiMediaPart(block ContentBlock) (geminiPart, bool) {
	mediaType := sourceString(block.Source, "media_type", "mime_type", "mimeType")
	data := sourceString(block.Source, "data", "base64")
	if mediaType != "" && data != "" {
		return geminiPart{InlineData: &geminiBlob{MimeType: mediaType, Data: data}}, true
	}
	fileURI := sourceString(block.Source, "file_uri", "fileUri", "url", "uri")
	if mediaType != "" && fileURI != "" {
		return geminiPart{FileData: &geminiFileData{MimeType: mediaType, FileURI: fileURI}}, true
	}
	return geminiPart{}, false
}

func sourceString(source map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if source == nil {
			return ""
		}
		switch value := source[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func geminiTextContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []ContentBlock:
		var texts []string
		for _, block := range v {
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, "\n")
	default:
		if content == nil {
			return ""
		}
		return fmt.Sprintf("%v", content)
	}
}

// Name returns the provider name
func (p *GeminiProvider) Name() string {
	return "gemini"
}

// SupportedModels returns supported Gemini models
func (p *GeminiProvider) SupportedModels() []string {
	return []string{
		"gemini-pro",
		"gemini-pro-vision",
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-2.0-flash-exp",
	}
}

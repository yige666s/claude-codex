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
	Contents         []geminiContent         `json:"contents"`
	GenerationConfig geminiGenerationConfig  `json:"generationConfig,omitempty"`
	SafetySettings   []geminiSafetySetting   `json:"safetySettings,omitempty"`
	Tools            []geminiTool            `json:"tools,omitempty"`
}

type geminiContent struct {
	Role  string        `json:"role"`
	Parts []geminiPart  `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
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

// geminiResponse represents Gemini API response format
type geminiResponse struct {
	Candidates     []geminiCandidate     `json:"candidates"`
	UsageMetadata  geminiUsageMetadata   `json:"usageMetadata,omitempty"`
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

	// Add system message as first user message if present
	if request.System != "" {
		geminiReq.Contents = append(geminiReq.Contents, geminiContent{
			Role: "user",
			Parts: []geminiPart{
				{Text: "System: " + request.System},
			},
		})
	}

	// Convert messages
	for _, msg := range request.Messages {
		role := msg.Role
		// Gemini uses "user" and "model" roles
		if role == "assistant" {
			role = "model"
		}

		content := ""
		switch v := msg.Content.(type) {
		case string:
			content = v
		case []ContentBlock:
			// Concatenate text blocks
			var texts []string
			for _, block := range v {
				if block.Type == "text" {
					texts = append(texts, block.Text)
				}
			}
			content = strings.Join(texts, "\n")
		}

		geminiReq.Contents = append(geminiReq.Contents, geminiContent{
			Role: role,
			Parts: []geminiPart{
				{Text: content},
			},
		})
	}

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
	for _, part := range candidate.Content.Parts {
		contentBlocks = append(contentBlocks, ContentBlock{
			Type: "text",
			Text: part.Text,
		})
	}

	return &MessageResponse{
		ID:         fmt.Sprintf("gemini-%d", time.Now().Unix()),
		Model:      request.Model,
		Role:       "assistant",
		Content:    contentBlocks,
		StopReason: candidate.FinishReason,
		Usage: Usage{
			InputTokens:  geminiResp.UsageMetadata.PromptTokenCount,
			OutputTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
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

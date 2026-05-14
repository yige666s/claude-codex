package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/googleauth"
)

// VertexProvider implements Provider for Google and partner models on Vertex AI.
type VertexProvider struct {
	mu                sync.Mutex
	token             string
	tokenRefresher    func(context.Context) (string, error)
	baseURL           string
	baseURLConfigured bool
	projectID         string
	location          string
	anthropicLocation string
	httpClient        *http.Client
	config            Config
}

func NewVertexProvider(cfg Config) (*VertexProvider, error) {
	token := firstNonEmptyString(
		cfg.Token,
		cfg.APIKey,
		os.Getenv("VERTEX_ACCESS_TOKEN"),
		os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"),
		os.Getenv("GOOGLE_ACCESS_TOKEN"),
	)
	projectID := firstNonEmptyString(
		os.Getenv("VERTEX_PROJECT_ID"),
		os.Getenv("GOOGLE_CLOUD_PROJECT"),
		os.Getenv("GCLOUD_PROJECT"),
	)
	location := firstNonEmptyString(
		os.Getenv("VERTEX_LOCATION"),
		os.Getenv("GOOGLE_CLOUD_LOCATION"),
		os.Getenv("CLOUD_ML_REGION"),
		"us-central1",
	)
	anthropicLocation := firstNonEmptyString(
		os.Getenv("VERTEX_ANTHROPIC_LOCATION"),
		os.Getenv("GOOGLE_CLOUD_ANTHROPIC_LOCATION"),
		"global",
	)
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	baseURLConfigured := baseURL != ""
	if baseURL == "" {
		baseURL = vertexEndpointBaseURL(location)
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	tokenRefresher := googleauth.GcloudAccessToken
	if source, ok, err := googleauth.NewServiceAccountTokenSourceFromEnv(client); ok {
		if err != nil {
			tokenRefresher = func(context.Context) (string, error) {
				return "", err
			}
		} else {
			tokenRefresher = source.AccessToken
		}
	}
	return &VertexProvider{
		token:             token,
		tokenRefresher:    tokenRefresher,
		baseURL:           baseURL,
		baseURLConfigured: baseURLConfigured,
		projectID:         projectID,
		location:          location,
		anthropicLocation: anthropicLocation,
		httpClient:        client,
		config:            cfg,
	}, nil
}

func (p *VertexProvider) Name() string {
	return "vertex"
}

func (p *VertexProvider) SupportedModels() []string {
	return []string{
		"gemini-1.5-pro",
		"gemini-1.5-flash",
		"gemini-2.0-flash",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"claude-sonnet-4-5",
	}
}

func (p *VertexProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	if p.currentToken() == "" {
		if err := p.refreshAccessToken(ctx); err != nil {
			return nil, fmt.Errorf("vertex access token is required; set GOOGLE_APPLICATION_CREDENTIALS, GOOGLE_APPLICATION_CREDENTIALS_JSON, VERTEX_ACCESS_TOKEN, or run gcloud auth print-access-token: %w", err)
		}
	}
	model, err := p.modelResource(request.Model)
	if err != nil {
		return nil, err
	}
	if model.Publisher == "anthropic" {
		return p.createAnthropicMessage(ctx, request, model)
	}
	reqBody := vertexGeminiRequest(request)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s:generateContent", p.endpointBaseURL(model.Location), strings.TrimLeft(model.Path, "/"))
	parsed, statusCode, status, data, err := p.sendGenerateContent(ctx, url, body)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusUnauthorized {
		if refreshErr := p.refreshAccessToken(ctx); refreshErr == nil {
			parsed, statusCode, status, data, err = p.sendGenerateContent(ctx, url, body)
			if err != nil {
				return nil, err
			}
		}
	}
	if statusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("vertex request failed: %s: %s", status, string(data))
	}
	return geminiResponseToUnified(request.Model, *parsed)
}

func (p *VertexProvider) StreamMessage(ctx context.Context, request MessageRequest, onChunk func(string)) (*MessageResponse, error) {
	if p.currentToken() == "" {
		if err := p.refreshAccessToken(ctx); err != nil {
			return nil, fmt.Errorf("vertex access token is required; set GOOGLE_APPLICATION_CREDENTIALS, GOOGLE_APPLICATION_CREDENTIALS_JSON, VERTEX_ACCESS_TOKEN, or run gcloud auth print-access-token: %w", err)
		}
	}
	model, err := p.modelResource(request.Model)
	if err != nil {
		return nil, err
	}
	if model.Publisher == "anthropic" {
		resp, err := p.createAnthropicMessage(ctx, request, model)
		if err != nil {
			return nil, err
		}
		if onChunk != nil {
			for _, block := range resp.Content {
				if block.Type == "text" && block.Text != "" {
					onChunk(block.Text)
				}
			}
		}
		return resp, nil
	}
	reqBody := vertexGeminiRequest(request)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse", p.endpointBaseURL(model.Location), strings.TrimLeft(model.Path, "/"))
	parsed, statusCode, status, data, err := p.sendStreamGenerateContent(ctx, url, request.Model, body, onChunk)
	if err != nil {
		return nil, err
	}
	if statusCode == http.StatusUnauthorized {
		if refreshErr := p.refreshAccessToken(ctx); refreshErr == nil {
			parsed, statusCode, status, data, err = p.sendStreamGenerateContent(ctx, url, request.Model, body, onChunk)
			if err != nil {
				return nil, err
			}
		}
	}
	if statusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("vertex stream request failed: %s: %s", status, string(data))
	}
	return parsed, nil
}

func (p *VertexProvider) sendGenerateContent(ctx context.Context, url string, body []byte) (*geminiResponse, int, string, []byte, error) {
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
	var parsed geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, resp.Status, nil, err
	}
	return &parsed, resp.StatusCode, resp.Status, nil, nil
}

func (p *VertexProvider) sendStreamGenerateContent(ctx context.Context, url, model string, body []byte, onChunk func(string)) (*MessageResponse, int, string, []byte, error) {
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
	parsed, err := parseGeminiStreamResponse(model, resp.Body, "vertex", onChunk)
	if err != nil {
		return nil, resp.StatusCode, resp.Status, nil, err
	}
	return parsed, resp.StatusCode, resp.Status, nil, nil
}

func (p *VertexProvider) currentToken() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.token
}

func (p *VertexProvider) setToken(token string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.token = token
}

func (p *VertexProvider) refreshAccessToken(ctx context.Context) error {
	if p.tokenRefresher == nil {
		return fmt.Errorf("no token refresher configured")
	}
	token, err := p.tokenRefresher(ctx)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token refresher returned empty token")
	}
	p.setToken(token)
	return nil
}

type vertexModelResource struct {
	Path      string
	Publisher string
	Location  string
	ModelID   string
}

func (p *VertexProvider) modelResource(model string) (vertexModelResource, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = p.config.Model
	}
	if strings.Contains(model, "/") {
		resource := parseVertexModelResource(model)
		resource.Path = strings.TrimLeft(model, "/")
		return resource, nil
	}
	if p.projectID == "" {
		return vertexModelResource{}, fmt.Errorf("vertex project ID is required for short model names; set VERTEX_PROJECT_ID or GOOGLE_CLOUD_PROJECT")
	}
	publisher := "google"
	location := p.location
	if isVertexAnthropicModel(model) {
		publisher = "anthropic"
		location = p.anthropicLocation
	}
	return vertexModelResource{
		Path:      fmt.Sprintf("projects/%s/locations/%s/publishers/%s/models/%s", p.projectID, location, publisher, model),
		Publisher: publisher,
		Location:  location,
		ModelID:   model,
	}, nil
}

func parseVertexModelResource(path string) vertexModelResource {
	resource := vertexModelResource{Path: strings.TrimLeft(path, "/")}
	parts := strings.Split(resource.Path, "/")
	for i := 0; i+1 < len(parts); i++ {
		switch parts[i] {
		case "locations":
			resource.Location = parts[i+1]
		case "publishers":
			resource.Publisher = strings.ToLower(parts[i+1])
		case "models":
			resource.ModelID = parts[i+1]
		}
	}
	if resource.Publisher == "" && isVertexAnthropicModel(resource.Path) {
		resource.Publisher = "anthropic"
	}
	return resource
}

func isVertexAnthropicModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "claude-") || strings.Contains(model, "/publishers/anthropic/")
}

func (p *VertexProvider) endpointBaseURL(location string) string {
	if p.baseURLConfigured {
		return p.baseURL
	}
	return vertexEndpointBaseURL(location)
}

func vertexEndpointBaseURL(location string) string {
	location = strings.ToLower(strings.TrimSpace(location))
	switch location {
	case "global":
		return "https://aiplatform.googleapis.com/v1"
	case "us":
		return "https://aiplatform.us.rep.googleapis.com/v1"
	case "eu":
		return "https://aiplatform.eu.rep.googleapis.com/v1"
	case "":
		return "https://us-central1-aiplatform.googleapis.com/v1"
	default:
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1", location)
	}
}

func vertexGeminiRequest(request MessageRequest) geminiRequest {
	req := geminiRequest{
		Contents: make([]geminiContent, 0, len(request.Messages)),
		GenerationConfig: geminiGenerationConfig{
			Temperature:     request.Temperature,
			TopP:            request.TopP,
			MaxOutputTokens: request.MaxTokens,
		},
	}
	if request.System != "" {
		req.SystemInstruction = &geminiContent{Role: "user", Parts: []geminiPart{{Text: request.System}}}
	}
	req.Contents = append(req.Contents, geminiContentsFromMessages(request.Messages)...)
	if len(request.Tools) > 0 {
		functionDecls := make([]geminiFunctionDeclaration, len(request.Tools))
		for i, tool := range request.Tools {
			functionDecls[i] = geminiFunctionDeclaration{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			}
		}
		req.Tools = []geminiTool{{FunctionDeclarations: functionDecls}}
	}
	return req
}

func toolResultContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []ContentBlock:
		var text []string
		for _, block := range v {
			if block.Type == "text" && block.Text != "" {
				text = append(text, block.Text)
			}
		}
		return strings.Join(text, "\n")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func geminiResponseToUnified(model string, resp geminiResponse) (*MessageResponse, error) {
	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}
	candidate := resp.Candidates[0]
	var contentBlocks []ContentBlock
	var toolCalls []ToolCall
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			contentBlocks = append(contentBlocks, ContentBlock{Type: "text", Text: part.Text})
		}
		if part.FunctionCall != nil {
			input, _ := json.Marshal(part.FunctionCall.Args)
			if len(input) == 0 {
				input = []byte(`{}`)
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:    fmt.Sprintf("vertex-call-%d", len(toolCalls)+1),
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
		ID:         fmt.Sprintf("vertex-%d", time.Now().Unix()),
		Model:      model,
		Role:       "assistant",
		Content:    contentBlocks,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:  resp.UsageMetadata.PromptTokenCount,
			OutputTokens: resp.UsageMetadata.CandidatesTokenCount,
		},
	}, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

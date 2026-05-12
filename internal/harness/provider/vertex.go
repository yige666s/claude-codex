package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// VertexProvider implements Provider for Gemini models on Vertex AI.
type VertexProvider struct {
	mu             sync.Mutex
	token          string
	tokenRefresher func(context.Context) (string, error)
	baseURL        string
	projectID      string
	location       string
	httpClient     *http.Client
	config         Config
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
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1", location)
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 600 * time.Second
	}
	return &VertexProvider{
		token:          token,
		tokenRefresher: gcloudAccessToken,
		baseURL:        baseURL,
		projectID:      projectID,
		location:       location,
		httpClient:     &http.Client{Timeout: timeout},
		config:         cfg,
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
	}
}

func (p *VertexProvider) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	if p.currentToken() == "" {
		if err := p.refreshAccessToken(ctx); err != nil {
			return nil, fmt.Errorf("vertex access token is required; set VERTEX_ACCESS_TOKEN or GOOGLE_OAUTH_ACCESS_TOKEN, or run gcloud auth application-default login: %w", err)
		}
	}
	modelPath, err := p.modelPath(request.Model)
	if err != nil {
		return nil, err
	}
	reqBody := vertexGeminiRequest(request)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s:generateContent", p.baseURL, strings.TrimLeft(modelPath, "/"))
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

func gcloudAccessToken(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (p *VertexProvider) modelPath(model string) (string, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		model = p.config.Model
	}
	if strings.Contains(model, "/") {
		return model, nil
	}
	if p.projectID == "" {
		return "", fmt.Errorf("vertex project ID is required for short model names; set VERTEX_PROJECT_ID or GOOGLE_CLOUD_PROJECT")
	}
	return fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s", p.projectID, p.location, model), nil
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

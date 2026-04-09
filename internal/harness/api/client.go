package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const (
	defaultBaseURL    = "https://api.anthropic.com"
	defaultMaxRetries = 3
	defaultTimeout    = 60 * time.Second
	apiVersion        = "2023-06-01"
)

// Client represents the Claude API client
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	retryConfig RetryConfig
	userAgent  string
}

// NewClient creates a new Claude API client
func NewClient(opts ClientOptions) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = defaultMaxRetries
	}
	if opts.Timeout == 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "claude-go/1.0"
	}

	return &Client{
		apiKey:  opts.APIKey,
		baseURL: opts.BaseURL,
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		retryConfig: RetryConfig{
			MaxRetries:     opts.MaxRetries,
			InitialBackoff: 1 * time.Second,
			MaxBackoff:     60 * time.Second,
			BackoffFactor:  2.0,
		},
		userAgent: opts.UserAgent,
	}
}

// CreateMessage sends a non-streaming request to the API
func (c *Client) CreateMessage(ctx context.Context, req CreateMessageRequest) (*Response, error) {
	req.Stream = false

	var resp *Response
	err := c.withRetry(ctx, func() error {
		var err error
		resp, err = c.doRequest(ctx, req)
		return err
	})

	return resp, err
}

// CreateMessageStream sends a streaming request to the API
func (c *Client) CreateMessageStream(ctx context.Context, req CreateMessageRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, c.handleErrorResponse(resp)
	}

	eventChan := make(chan StreamEvent, 10)
	go c.processStream(ctx, resp.Body, eventChan)

	return eventChan, nil
}

// doRequest performs a non-streaming HTTP request
func (c *Client) doRequest(ctx context.Context, req CreateMessageRequest) (*Response, error) {
	start := time.Now()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp)
	}

	var apiResp Response
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	apiResp.RequestID = resp.Header.Get("request-id")
	apiResp.ResponseTime = time.Since(start)

	return &apiResp, nil
}

// setHeaders sets common headers for API requests
func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
}

// handleErrorResponse processes error responses from the API
func (c *Client) handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	return &APIError{
		StatusCode: resp.StatusCode,
		Type:       errResp.Error.Type,
		Message:    errResp.Error.Message,
	}
}

// processStream reads and processes streaming events
func (c *Client) processStream(ctx context.Context, body io.ReadCloser, eventChan chan<- StreamEvent) {
	defer close(eventChan)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		// SSE format: "data: {...}"
		if len(line) > 6 && line[:6] == "data: " {
			data := line[6:]

			var event StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				// Send error event
				eventChan <- StreamEvent{
					Type: "error",
					Error: &ErrorBlock{
						Type:    "parse_error",
						Message: err.Error(),
					},
				}
				return
			}

			eventChan <- event
		}
	}

	if err := scanner.Err(); err != nil {
		eventChan <- StreamEvent{
			Type: "error",
			Error: &ErrorBlock{
				Type:    "stream_error",
				Message: err.Error(),
			},
		}
	}
}

// withRetry executes a function with exponential backoff retry logic
func (c *Client) withRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	backoff := c.retryConfig.InitialBackoff

	for attempt := 0; attempt <= c.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			backoff = time.Duration(float64(backoff) * c.retryConfig.BackoffFactor)
			if backoff > c.retryConfig.MaxBackoff {
				backoff = c.retryConfig.MaxBackoff
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on certain errors
		if apiErr, ok := err.(*APIError); ok {
			if !c.shouldRetry(apiErr) {
				return err
			}
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// shouldRetry determines if an error is retryable
func (c *Client) shouldRetry(err *APIError) bool {
	// Retry on rate limits and server errors
	return err.StatusCode == 429 || err.StatusCode >= 500
}

// APIError represents an API error
type APIError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}

// CountTokens estimates token count for a message
// This is a simple approximation - for accurate counts, use the tokenization API
func CountTokens(text string) int {
	// Rough approximation: ~4 characters per token
	return int(math.Ceil(float64(len(text)) / 4.0))
}

// ValidateRequest validates a CreateMessageRequest
func ValidateRequest(req CreateMessageRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages cannot be empty")
	}
	if req.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be positive")
	}
	if req.MaxTokens > 4096 {
		return fmt.Errorf("max_tokens cannot exceed 4096")
	}
	return nil
}

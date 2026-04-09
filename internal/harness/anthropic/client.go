package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ding/claude-code/claude-go/internal/public/ratelimit"
)

type Client struct {
	apiKey        string
	baseURL       string
	httpClient    *http.Client
	retryConfig   RetryConfig
	rateLimiter   *ratelimit.Tracker
}

func NewClient(apiKey, baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retryConfig: DefaultRetryConfig(),
		rateLimiter: ratelimit.NewTracker(),
	}
}

// GetRateLimiter returns the rate limit tracker
func (c *Client) GetRateLimiter() *ratelimit.Tracker {
	return c.rateLimiter
}

func (c *Client) CreateMessage(ctx context.Context, request MessageRequest) (*MessageResponse, error) {
	var response *MessageResponse

	err := withRetry(ctx, c.retryConfig, func() error {
		body, err := json.Marshal(request)
		if err != nil {
			return err
		}

		httpRequest, err := c.newRequest(ctx, bytes.NewReader(body))
		if err != nil {
			return err
		}

		httpResponse, err := c.httpClient.Do(httpRequest)
		if err != nil {
			return err
		}
		defer httpResponse.Body.Close()

		// Process rate limit headers from response
		if c.rateLimiter != nil {
			c.rateLimiter.ProcessResponseHeaders(httpResponse.Header)
		}

		if httpResponse.StatusCode >= http.StatusBadRequest {
			data, _ := io.ReadAll(httpResponse.Body)

			// Process rate limit error
			if c.rateLimiter != nil && httpResponse.StatusCode == http.StatusTooManyRequests {
				c.rateLimiter.ProcessError(httpResponse.StatusCode, httpResponse.Header)
			}

			return &HTTPError{
				StatusCode: httpResponse.StatusCode,
				Status:     httpResponse.Status,
				Body:       string(data),
			}
		}

		var parsed MessageResponse
		if err := json.NewDecoder(httpResponse.Body).Decode(&parsed); err != nil {
			return err
		}

		response = &parsed
		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

func (c *Client) newRequest(ctx context.Context, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.messageURL(), body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("content-type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	if c.apiKey != "" {
		request.Header.Set("x-api-key", c.apiKey)
	}

	return request, nil
}

func (c *Client) messageURL() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL + "/messages"
	}
	return c.baseURL + "/v1/messages"
}

// CountTokens counts tokens for messages and tools
func (c *Client) CountTokens(ctx context.Context, request CountTokensRequest) (*CountTokensResponse, error) {
	var response *CountTokensResponse

	err := withRetry(ctx, c.retryConfig, func() error {
		body, err := json.Marshal(request)
		if err != nil {
			return err
		}

		httpRequest, err := c.newCountTokensRequest(ctx, bytes.NewReader(body))
		if err != nil {
			return err
		}

		httpResponse, err := c.httpClient.Do(httpRequest)
		if err != nil {
			return err
		}
		defer httpResponse.Body.Close()

		// Process rate limit headers from response
		if c.rateLimiter != nil {
			c.rateLimiter.ProcessResponseHeaders(httpResponse.Header)
		}

		if httpResponse.StatusCode >= http.StatusBadRequest {
			data, _ := io.ReadAll(httpResponse.Body)

			// Process rate limit error
			if c.rateLimiter != nil && httpResponse.StatusCode == http.StatusTooManyRequests {
				c.rateLimiter.ProcessError(httpResponse.StatusCode, httpResponse.Header)
			}

			return &HTTPError{
				StatusCode: httpResponse.StatusCode,
				Status:     httpResponse.Status,
				Body:       string(data),
			}
		}

		var parsed CountTokensResponse
		if err := json.NewDecoder(httpResponse.Body).Decode(&parsed); err != nil {
			return err
		}

		response = &parsed
		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

func (c *Client) newCountTokensRequest(ctx context.Context, body io.Reader) (*http.Request, error) {
	url := c.countTokensURL()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("content-type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Set("anthropic-beta", "token-counting-2024-11-01")
	if c.apiKey != "" {
		request.Header.Set("x-api-key", c.apiKey)
	}

	return request, nil
}

func (c *Client) countTokensURL() string {
	if strings.HasSuffix(c.baseURL, "/v1") {
		return c.baseURL + "/messages/count_tokens"
	}
	return c.baseURL + "/v1/messages/count_tokens"
}

package api

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name string
		opts ClientOptions
		want struct {
			baseURL    string
			maxRetries int
			userAgent  string
		}
	}{
		{
			name: "default options",
			opts: ClientOptions{
				APIKey: "test-key",
			},
			want: struct {
				baseURL    string
				maxRetries int
				userAgent  string
			}{
				baseURL:    defaultBaseURL,
				maxRetries: defaultMaxRetries,
				userAgent:  "claude-go/1.0",
			},
		},
		{
			name: "custom options",
			opts: ClientOptions{
				APIKey:     "test-key",
				BaseURL:    "https://custom.api.com",
				MaxRetries: 5,
				UserAgent:  "custom-agent",
			},
			want: struct {
				baseURL    string
				maxRetries int
				userAgent  string
			}{
				baseURL:    "https://custom.api.com",
				maxRetries: 5,
				userAgent:  "custom-agent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.opts)

			if client.baseURL != tt.want.baseURL {
				t.Errorf("baseURL = %v, want %v", client.baseURL, tt.want.baseURL)
			}
			if client.retryConfig.MaxRetries != tt.want.maxRetries {
				t.Errorf("maxRetries = %v, want %v", client.retryConfig.MaxRetries, tt.want.maxRetries)
			}
			if client.userAgent != tt.want.userAgent {
				t.Errorf("userAgent = %v, want %v", client.userAgent, tt.want.userAgent)
			}
		})
	}
}

func TestCountTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{
			name: "empty string",
			text: "",
			want: 0,
		},
		{
			name: "short text",
			text: "Hello",
			want: 2,
		},
		{
			name: "longer text",
			text: "This is a longer text that should have more tokens",
			want: 13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountTokens(tt.text)
			if got != tt.want {
				t.Errorf("CountTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     CreateMessageRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request",
			req: CreateMessageRequest{
				Model:     "claude-3-sonnet-20240229",
				Messages:  []Message{{Role: "user", Content: "Hello"}},
				MaxTokens: 1024,
			},
			wantErr: false,
		},
		{
			name: "missing model",
			req: CreateMessageRequest{
				Messages:  []Message{{Role: "user", Content: "Hello"}},
				MaxTokens: 1024,
			},
			wantErr: true,
			errMsg:  "model is required",
		},
		{
			name: "empty messages",
			req: CreateMessageRequest{
				Model:     "claude-3-sonnet-20240229",
				Messages:  []Message{},
				MaxTokens: 1024,
			},
			wantErr: true,
			errMsg:  "messages cannot be empty",
		},
		{
			name: "invalid max tokens",
			req: CreateMessageRequest{
				Model:     "claude-3-sonnet-20240229",
				Messages:  []Message{{Role: "user", Content: "Hello"}},
				MaxTokens: 0,
			},
			wantErr: true,
			errMsg:  "max_tokens must be positive",
		},
		{
			name: "max tokens too high",
			req: CreateMessageRequest{
				Model:     "claude-3-sonnet-20240229",
				Messages:  []Message{{Role: "user", Content: "Hello"}},
				MaxTokens: 5000,
			},
			wantErr: true,
			errMsg:  "max_tokens cannot exceed 4096",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequest(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateRequest() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	err := &APIError{
		StatusCode: 429,
		Type:       "rate_limit_error",
		Message:    "Rate limit exceeded",
	}

	expected := "API error 429 (rate_limit_error): Rate limit exceeded"
	if err.Error() != expected {
		t.Errorf("APIError.Error() = %v, want %v", err.Error(), expected)
	}
}

func TestShouldRetry(t *testing.T) {
	client := NewClient(ClientOptions{APIKey: "test"})

	tests := []struct {
		name string
		err  *APIError
		want bool
	}{
		{
			name: "rate limit error",
			err:  &APIError{StatusCode: 429},
			want: true,
		},
		{
			name: "server error",
			err:  &APIError{StatusCode: 500},
			want: true,
		},
		{
			name: "bad request",
			err:  &APIError{StatusCode: 400},
			want: false,
		},
		{
			name: "unauthorized",
			err:  &APIError{StatusCode: 401},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.shouldRetry(tt.err)
			if got != tt.want {
				t.Errorf("shouldRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithRetry(t *testing.T) {
	client := NewClient(ClientOptions{
		APIKey:     "test",
		MaxRetries: 2,
	})
	client.retryConfig.InitialBackoff = 10 * time.Millisecond
	client.retryConfig.MaxBackoff = 50 * time.Millisecond

	t.Run("success on first try", func(t *testing.T) {
		attempts := 0
		err := client.withRetry(context.Background(), func() error {
			attempts++
			return nil
		})

		if err != nil {
			t.Errorf("withRetry() error = %v, want nil", err)
		}
		if attempts != 1 {
			t.Errorf("attempts = %v, want 1", attempts)
		}
	})

	t.Run("success after retry", func(t *testing.T) {
		attempts := 0
		err := client.withRetry(context.Background(), func() error {
			attempts++
			if attempts < 2 {
				return &APIError{StatusCode: 500}
			}
			return nil
		})

		if err != nil {
			t.Errorf("withRetry() error = %v, want nil", err)
		}
		if attempts != 2 {
			t.Errorf("attempts = %v, want 2", attempts)
		}
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		attempts := 0
		err := client.withRetry(context.Background(), func() error {
			attempts++
			return &APIError{StatusCode: 500}
		})

		if err == nil {
			t.Error("withRetry() error = nil, want error")
		}
		if attempts != 3 { // initial + 2 retries
			t.Errorf("attempts = %v, want 3", attempts)
		}
	})

	t.Run("non-retryable error", func(t *testing.T) {
		attempts := 0
		err := client.withRetry(context.Background(), func() error {
			attempts++
			return &APIError{StatusCode: 400}
		})

		if err == nil {
			t.Error("withRetry() error = nil, want error")
		}
		if attempts != 1 {
			t.Errorf("attempts = %v, want 1", attempts)
		}
	})
}

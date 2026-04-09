package api

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParsePromptTooLongTokenCounts(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectActual  int
		expectLimit   int
		expectOK      bool
	}{
		{
			name:         "Standard format",
			input:        "prompt is too long: 137500 tokens > 135000 maximum",
			expectActual: 137500,
			expectLimit:  135000,
			expectOK:     true,
		},
		{
			name:         "Case insensitive",
			input:        "Prompt Is Too Long: 100000 tokens > 95000 maximum",
			expectActual: 100000,
			expectLimit:  95000,
			expectOK:     true,
		},
		{
			name:         "No match",
			input:        "some other error",
			expectActual: 0,
			expectLimit:  0,
			expectOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, limit, ok := ParsePromptTooLongTokenCounts(tt.input)
			if ok != tt.expectOK {
				t.Errorf("expected ok=%v, got %v", tt.expectOK, ok)
			}
			if ok && (actual != tt.expectActual || limit != tt.expectLimit) {
				t.Errorf("expected (%d, %d), got (%d, %d)",
					tt.expectActual, tt.expectLimit, actual, limit)
			}
		})
	}
}

func TestGetPromptTooLongTokenGap(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		expectGap  int
		expectOK   bool
	}{
		{
			name:      "Valid gap",
			input:     "prompt is too long: 137500 tokens > 135000 maximum",
			expectGap: 2500,
			expectOK:  true,
		},
		{
			name:      "No gap",
			input:     "prompt is too long: 135000 tokens > 135000 maximum",
			expectGap: 0,
			expectOK:  false,
		},
		{
			name:      "Invalid format",
			input:     "some error",
			expectGap: 0,
			expectOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gap, ok := GetPromptTooLongTokenGap(tt.input)
			if ok != tt.expectOK {
				t.Errorf("expected ok=%v, got %v", tt.expectOK, ok)
			}
			if ok && gap != tt.expectGap {
				t.Errorf("expected gap=%d, got %d", tt.expectGap, gap)
			}
		})
	}
}

func TestIsMediaSizeError(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Image exceeds maximum",
			input:    "image exceeds maximum size",
			expected: true,
		},
		{
			name:     "Image dimensions exceed many-image",
			input:    "image dimensions exceed many-image limit",
			expected: true,
		},
		{
			name:     "PDF page limit",
			input:    "maximum of 10 PDF pages exceeded",
			expected: true,
		},
		{
			name:     "Not a media error",
			input:    "some other error",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMediaSizeError(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIs529Error(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "529 in message",
			err:      errors.New("API error 529"),
			expected: true,
		},
		{
			name:     "overloaded_error",
			err:      errors.New(`{"type":"overloaded_error"}`),
			expected: true,
		},
		{
			name:     "Not 529",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Is529Error(tt.err)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClassifyAPIError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		statusCode int
		expected   ErrorClassification
	}{
		{
			name:       "529 rate limit",
			err:        errors.New("overloaded"),
			statusCode: 529,
			expected:   ErrorClassRateLimit,
		},
		{
			name:       "429 rate limit",
			err:        errors.New("too many requests"),
			statusCode: 429,
			expected:   ErrorClassRateLimit,
		},
		{
			name:       "401 auth",
			err:        errors.New("unauthorized"),
			statusCode: 401,
			expected:   ErrorClassAuthenticationFailed,
		},
		{
			name:       "500 server error",
			err:        errors.New("internal error"),
			statusCode: 500,
			expected:   ErrorClassServerError,
		},
		{
			name:       "SSL error",
			err:        errors.New("SSL certificate error"),
			statusCode: 0,
			expected:   ErrorClassSSLCertError,
		},
		{
			name:       "Connection error",
			err:        errors.New("connection timeout"),
			statusCode: 0,
			expected:   ErrorClassConnectionError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyAPIError(tt.err, tt.statusCode)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		name        string
		attempt     int
		baseDelayMS int
		expectMin   time.Duration
		expectMax   time.Duration
	}{
		{
			name:        "First attempt",
			attempt:     0,
			baseDelayMS: 500,
			expectMin:   500 * time.Millisecond,
			expectMax:   500 * time.Millisecond,
		},
		{
			name:        "Second attempt",
			attempt:     1,
			baseDelayMS: 500,
			expectMin:   1000 * time.Millisecond,
			expectMax:   1000 * time.Millisecond,
		},
		{
			name:        "Third attempt",
			attempt:     2,
			baseDelayMS: 500,
			expectMin:   2000 * time.Millisecond,
			expectMax:   2000 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateBackoff(tt.attempt, tt.baseDelayMS)
			if result < tt.expectMin || result > tt.expectMax {
				t.Errorf("expected between %v and %v, got %v",
					tt.expectMin, tt.expectMax, result)
			}
		})
	}
}

func TestRetryStateShouldRetry(t *testing.T) {
	tests := []struct {
		name     string
		state    RetryState
		opts     RetryOptions
		expected bool
	}{
		{
			name: "Below max retries",
			state: RetryState{
				Attempt:    2,
				LastError:  errors.New("500 error"),
				LastStatusCode: 500,
			},
			opts: RetryOptions{
				MaxRetries: 5,
			},
			expected: true,
		},
		{
			name: "Exceeded max retries",
			state: RetryState{
				Attempt:    10,
				LastError:  errors.New("500 error"),
				LastStatusCode: 500,
			},
			opts: RetryOptions{
				MaxRetries: 5,
			},
			expected: false,
		},
		{
			name: "Too many 529 errors",
			state: RetryState{
				Attempt:              2,
				Consecutive529Errors: 3,
				LastError:            errors.New("529 error"),
				LastStatusCode:       529,
			},
			opts: RetryOptions{
				MaxRetries:  10,
				QuerySource: "unknown_source",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.state.ShouldRetry(tt.opts)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestShouldRetry529(t *testing.T) {
	tests := []struct {
		name        string
		querySource string
		expected    bool
	}{
		{
			name:        "Foreground source",
			querySource: "repl_main_thread",
			expected:    true,
		},
		{
			name:        "Background source",
			querySource: "background_task",
			expected:    false,
		},
		{
			name:        "Empty source (default retry)",
			querySource: "",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldRetry529(tt.querySource)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestWithRetry(t *testing.T) {
	t.Run("Success on first try", func(t *testing.T) {
		ctx := context.Background()
		opts := RetryOptions{MaxRetries: 3}

		callCount := 0
		err := WithRetry(ctx, opts, func(ctx context.Context) error {
			callCount++
			return nil
		})

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount != 1 {
			t.Errorf("expected 1 call, got %d", callCount)
		}
	})

	t.Run("Success after retries", func(t *testing.T) {
		ctx := context.Background()
		opts := RetryOptions{MaxRetries: 5}

		callCount := 0
		err := WithRetry(ctx, opts, func(ctx context.Context) error {
			callCount++
			if callCount < 3 {
				// Return a 500 error which is retryable
				return errors.New("500 internal server error")
			}
			return nil
		})

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if callCount != 3 {
			t.Errorf("expected 3 calls, got %d", callCount)
		}
	})

	t.Run("Max retries exceeded", func(t *testing.T) {
		ctx := context.Background()
		opts := RetryOptions{MaxRetries: 2}

		callCount := 0
		err := WithRetry(ctx, opts, func(ctx context.Context) error {
			callCount++
			return errors.New("persistent error")
		})

		if err == nil {
			t.Error("expected error, got nil")
		}

		var cannotRetry *CannotRetryError
		if !errors.As(err, &cannotRetry) {
			t.Errorf("expected CannotRetryError, got %T", err)
		}
	})
}

func TestUtilizationMethods(t *testing.T) {
	t.Run("IsRateLimitApproaching", func(t *testing.T) {
		util90 := 90.0
		util50 := 50.0

		u := &Utilization{
			FiveHour: &RateLimit{Utilization: &util90},
			SevenDay: &RateLimit{Utilization: &util50},
		}

		if !u.IsRateLimitApproaching(80.0) {
			t.Error("expected rate limit approaching")
		}

		if u.IsRateLimitApproaching(95.0) {
			t.Error("expected rate limit not approaching")
		}
	})

	t.Run("GetHighestUtilization", func(t *testing.T) {
		util90 := 90.0
		util50 := 50.0
		util75 := 75.0

		u := &Utilization{
			FiveHour: &RateLimit{Utilization: &util90},
			SevenDay: &RateLimit{Utilization: &util50},
			SevenDayOpus: &RateLimit{Utilization: &util75},
		}

		highest := u.GetHighestUtilization()
		if highest != 90.0 {
			t.Errorf("expected 90.0, got %f", highest)
		}
	})
}

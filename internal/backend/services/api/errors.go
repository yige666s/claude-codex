package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// IsPromptTooLongError checks if an error message indicates prompt is too long
func IsPromptTooLongError(errorMsg string) bool {
	return strings.Contains(strings.ToLower(errorMsg), "prompt is too long")
}

// ParsePromptTooLongTokenCounts extracts actual and limit token counts from error message
func ParsePromptTooLongTokenCounts(rawMessage string) (actualTokens, limitTokens int, ok bool) {
	// Match pattern like "prompt is too long: 137500 tokens > 135000 maximum"
	re := regexp.MustCompile(`(?i)prompt is too long[^0-9]*(\d+)\s*tokens?\s*>\s*(\d+)`)
	matches := re.FindStringSubmatch(rawMessage)

	if len(matches) < 3 {
		return 0, 0, false
	}

	actual, err1 := strconv.Atoi(matches[1])
	limit, err2 := strconv.Atoi(matches[2])

	if err1 != nil || err2 != nil {
		return 0, 0, false
	}

	return actual, limit, true
}

// GetPromptTooLongTokenGap returns how many tokens over the limit
func GetPromptTooLongTokenGap(errorMsg string) (int, bool) {
	actual, limit, ok := ParsePromptTooLongTokenCounts(errorMsg)
	if !ok {
		return 0, false
	}

	gap := actual - limit
	if gap <= 0 {
		return 0, false
	}

	return gap, true
}

// IsMediaSizeError checks if error is related to media size limits
func IsMediaSizeError(raw string) bool {
	lower := strings.ToLower(raw)

	// Check for image size errors
	if strings.Contains(lower, "image exceeds") && strings.Contains(lower, "maximum") {
		return true
	}

	// Check for many-image dimension errors
	if strings.Contains(lower, "image dimensions exceed") && strings.Contains(lower, "many-image") {
		return true
	}

	// Check for PDF page limit errors
	pdfPattern := regexp.MustCompile(`maximum of \d+ PDF pages`)
	return pdfPattern.MatchString(raw)
}

// Is529Error checks if error is a 529 overload error
func Is529Error(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	return strings.Contains(errMsg, "529") ||
		strings.Contains(errMsg, "overloaded_error")
}

// Is429Error checks if error is a 429 rate limit error
func Is429Error(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "429")
}

// IsTransientCapacityError checks if error is a transient capacity issue
func IsTransientCapacityError(err error) bool {
	return Is529Error(err) || Is429Error(err)
}

// IsConnectionError checks if error is a connection error
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "connection") ||
		strings.Contains(errMsg, "econnreset") ||
		strings.Contains(errMsg, "epipe") ||
		strings.Contains(errMsg, "timeout")
}

// IsSSLError checks if error is an SSL certificate error
func IsSSLError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "ssl") ||
		strings.Contains(errMsg, "certificate") ||
		strings.Contains(errMsg, "tls")
}

// ClassifyAPIError categorizes an API error
func ClassifyAPIError(err error, statusCode int) ErrorClassification {
	if err == nil {
		return ErrorClassUnknown
	}

	// Check for SSL errors first
	if IsSSLError(err) {
		return ErrorClassSSLCertError
	}

	// Check for connection errors
	if IsConnectionError(err) {
		return ErrorClassConnectionError
	}

	// Check status code based errors
	switch statusCode {
	case 529:
		return ErrorClassRateLimit
	case 429:
		return ErrorClassRateLimit
	case 401, 403:
		return ErrorClassAuthenticationFailed
	case 400:
		return ErrorClassInvalidRequest
	default:
		if statusCode >= 500 {
			return ErrorClassServerError
		}
	}

	return ErrorClassUnknown
}

// ShouldRetry529 checks if a 529 error should be retried based on query source
func ShouldRetry529(querySource string) bool {
	// If no query source specified, default to retry (conservative)
	if querySource == "" {
		return true
	}

	return Foreground529RetrySources[querySource]
}

// IsRetryableError checks if an error should be retried
func IsRetryableError(err error, statusCode int, isClaudeAISubscriber bool, isEnterpriseSubscriber bool) bool {
	if err == nil {
		return false
	}

	// Retry on 529 overload errors
	if Is529Error(err) {
		return true
	}

	// Retry on 429 for non-ClaudeAI subscribers or enterprise users
	if Is429Error(err) {
		return !isClaudeAISubscriber || isEnterpriseSubscriber
	}

	// Retry on 401/403 auth errors (will refresh tokens)
	if statusCode == 401 || statusCode == 403 {
		return true
	}

	// Retry on 5xx server errors
	if statusCode >= 500 {
		return true
	}

	// Retry on connection errors
	if IsConnectionError(err) {
		return true
	}

	return false
}

// FormatAPIError formats an API error for display
func FormatAPIError(err error, statusCode int) string {
	if err == nil {
		return "Unknown error"
	}

	classification := ClassifyAPIError(err, statusCode)

	switch classification {
	case ErrorClassRateLimit:
		if statusCode == 529 {
			return fmt.Sprintf("%s: %s", APIErrorMessagePrefix, Repeated529ErrorMessage)
		}
		return fmt.Sprintf("%s: Rate limit exceeded", APIErrorMessagePrefix)

	case ErrorClassAuthenticationFailed:
		return fmt.Sprintf("%s: %s", APIErrorMessagePrefix, InvalidAPIKeyErrorMessage)

	case ErrorClassConnectionError:
		return fmt.Sprintf("%s: Connection error - %s", APIErrorMessagePrefix, err.Error())

	case ErrorClassSSLCertError:
		return fmt.Sprintf("%s: SSL certificate error - %s", APIErrorMessagePrefix, err.Error())

	case ErrorClassServerError:
		return fmt.Sprintf("%s: Server error (status %d)", APIErrorMessagePrefix, statusCode)

	case ErrorClassInvalidRequest:
		if IsPromptTooLongError(err.Error()) {
			return fmt.Sprintf("%s: %s", APIErrorMessagePrefix, PromptTooLongErrorMessage)
		}
		if IsMediaSizeError(err.Error()) {
			return fmt.Sprintf("%s: Media size exceeds limits", APIErrorMessagePrefix)
		}
		return fmt.Sprintf("%s: Invalid request - %s", APIErrorMessagePrefix, err.Error())

	default:
		return fmt.Sprintf("%s: %s", APIErrorMessagePrefix, err.Error())
	}
}

// ExtractRetryAfterSeconds extracts retry-after value from error
func ExtractRetryAfterSeconds(err error) (int, bool) {
	if err == nil {
		return 0, false
	}

	// Try to extract from error message
	// Pattern: "retry after X seconds" or "retry-after: X"
	re := regexp.MustCompile(`(?i)retry[- ]after[:\s]+(\d+)`)
	matches := re.FindStringSubmatch(err.Error())

	if len(matches) < 2 {
		return 0, false
	}

	seconds, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}

	return seconds, true
}

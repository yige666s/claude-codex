package agentruntime

import (
	"net/http"
	"regexp"
	"strings"
)

type APIError struct {
	Error     string `json:"error,omitempty"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func normalizeAPIResponse(w http.ResponseWriter, status int, value any) any {
	if status < http.StatusBadRequest {
		return value
	}
	message, ok := legacyErrorMessage(value)
	if !ok {
		return value
	}
	return APIError{
		Error:     sanitizeAPIErrorMessage(message),
		Code:      errorCodeForStatus(status, message),
		Message:   sanitizeAPIErrorMessage(message),
		RequestID: w.Header().Get("X-Request-ID"),
	}
}

func legacyErrorMessage(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]string:
		message := strings.TrimSpace(typed["error"])
		return message, message != ""
	case map[string]any:
		message, _ := typed["error"].(string)
		message = strings.TrimSpace(message)
		return message, message != ""
	default:
		return "", false
	}
}

func errorCodeForStatus(status int, message string) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	default:
		if status >= http.StatusInternalServerError {
			return "internal_error"
		}
	}
	if strings.TrimSpace(message) == "" {
		return "request_error"
	}
	return "request_error"
}

func sanitizeAPIErrorMessage(message string) string {
	text := strings.TrimSpace(message)
	if text == "" {
		return "Request failed."
	}
	if isCredentialErrorMessage(text) {
		return "A service credential is missing or unavailable. Ask an administrator to verify the provider setup."
	}
	if isSandboxErrorMessage(text) {
		return "The tool sandbox could not start. Ask an administrator to check the sandbox configuration."
	}
	if internalPathPattern.MatchString(text) || stackLikePattern.MatchString(text) {
		return "The request failed because of an internal service configuration issue."
	}
	return text
}

func isCredentialErrorMessage(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "google_application_credentials") ||
		strings.Contains(lower, "vertex_access_token") ||
		strings.Contains(lower, "vertex_service_account") ||
		strings.Contains(lower, "service account") ||
		strings.Contains(lower, "vertex-service-account") ||
		strings.Contains(lower, "access token is required") ||
		strings.Contains(lower, "credential")
}

func isSandboxErrorMessage(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "sandbox") ||
		strings.Contains(lower, "oci runtime") ||
		strings.Contains(lower, "docker:") ||
		strings.Contains(lower, "operation not permitted") ||
		strings.Contains(lower, "seccomp") ||
		strings.Contains(lower, "apparmor")
}

var (
	internalPathPattern = regexp.MustCompile(`(?i)(^|\s)(/run/agentapi|/opt/agentapi|/var/lib/agentapi|/workspace|/tmp|secrets/)\S*`)
	stackLikePattern    = regexp.MustCompile(`(?i)(\n\s*at\s+\S+\s*\(|goroutine \d+ \[|\.go:\d+|\.ts:\d+|\.tsx:\d+)`)
)

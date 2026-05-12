package agentruntime

import (
	"net/http"
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
		Error:     message,
		Code:      errorCodeForStatus(status, message),
		Message:   message,
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

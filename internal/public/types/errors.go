package types

// ErrorType represents the type of error.
type ErrorType string

const (
	ErrorTypeValidation   ErrorType = "validation"
	ErrorTypePermission   ErrorType = "permission"
	ErrorTypeNotFound     ErrorType = "not_found"
	ErrorTypeTimeout      ErrorType = "timeout"
	ErrorTypeNetwork      ErrorType = "network"
	ErrorTypeAPI          ErrorType = "api"
	ErrorTypeInternal     ErrorType = "internal"
	ErrorTypeConfiguration ErrorType = "configuration"
)

// Error represents a structured error with additional context.
type Error struct {
	Type       ErrorType              `json:"type"`
	Message    string                 `json:"message"`
	Code       string                 `json:"code,omitempty"`
	Details    string                 `json:"details,omitempty"`
	Suggestion string                 `json:"suggestion,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	Cause      error                  `json:"-"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Details != "" {
		return e.Message + ": " + e.Details
	}
	return e.Message
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a new structured error.
func NewError(errType ErrorType, message string) *Error {
	return &Error{
		Type:     errType,
		Message:  message,
		Metadata: make(map[string]interface{}),
	}
}

// NewValidationError creates a validation error.
func NewValidationError(message, details string) *Error {
	return &Error{
		Type:     ErrorTypeValidation,
		Message:  message,
		Details:  details,
		Metadata: make(map[string]interface{}),
	}
}

// NewPermissionError creates a permission error.
func NewPermissionError(message, suggestion string) *Error {
	return &Error{
		Type:       ErrorTypePermission,
		Message:    message,
		Suggestion: suggestion,
		Metadata:   make(map[string]interface{}),
	}
}

// NewNotFoundError creates a not found error.
func NewNotFoundError(resource, identifier string) *Error {
	return &Error{
		Type:     ErrorTypeNotFound,
		Message:  resource + " not found",
		Details:  identifier,
		Metadata: make(map[string]interface{}),
	}
}

// WithCode adds an error code.
func (e *Error) WithCode(code string) *Error {
	e.Code = code
	return e
}

// WithDetails adds error details.
func (e *Error) WithDetails(details string) *Error {
	e.Details = details
	return e
}

// WithSuggestion adds a suggestion for fixing the error.
func (e *Error) WithSuggestion(suggestion string) *Error {
	e.Suggestion = suggestion
	return e
}

// WithMetadata adds metadata to the error.
func (e *Error) WithMetadata(key string, value interface{}) *Error {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

// WithCause adds the underlying cause.
func (e *Error) WithCause(cause error) *Error {
	e.Cause = cause
	return e
}

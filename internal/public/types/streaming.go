package types

import "encoding/json"

// StreamEventType represents the type of streaming event.
type StreamEventType string

const (
	StreamEventMessageStart       StreamEventType = "message_start"
	StreamEventMessageDelta       StreamEventType = "message_delta"
	StreamEventMessageStop        StreamEventType = "message_stop"
	StreamEventContentBlockStart  StreamEventType = "content_block_start"
	StreamEventContentBlockDelta  StreamEventType = "content_block_delta"
	StreamEventContentBlockStop   StreamEventType = "content_block_stop"
	StreamEventPing               StreamEventType = "ping"
	StreamEventError              StreamEventType = "error"
)

// StreamEvent represents a streaming event from the API.
type StreamEvent struct {
	Type    StreamEventType `json:"type"`
	Message interface{}     `json:"message,omitempty"`
	Index   int             `json:"index,omitempty"`
	Delta   interface{}     `json:"delta,omitempty"`
	Usage   *Usage          `json:"usage,omitempty"`
	Error   *StreamError    `json:"error,omitempty"`
}

// StreamError represents an error in a stream.
type StreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// TextDelta represents a text content delta.
type TextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolUseDelta represents a tool use delta.
type ToolUseDelta struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// StreamHandler handles streaming events.
type StreamHandler interface {
	// OnEvent is called for each streaming event.
	OnEvent(event StreamEvent) error

	// OnComplete is called when the stream completes successfully.
	OnComplete()

	// OnError is called when the stream encounters an error.
	OnError(err error)
}

// StreamCallback is a function that handles streaming events.
type StreamCallback func(event StreamEvent) error

// NewStreamEvent creates a new stream event.
func NewStreamEvent(eventType StreamEventType) *StreamEvent {
	return &StreamEvent{
		Type: eventType,
	}
}

// WithMessage adds a message to the stream event.
func (se *StreamEvent) WithMessage(message interface{}) *StreamEvent {
	se.Message = message
	return se
}

// WithDelta adds a delta to the stream event.
func (se *StreamEvent) WithDelta(delta interface{}) *StreamEvent {
	se.Delta = delta
	return se
}

// WithUsage adds usage information to the stream event.
func (se *StreamEvent) WithUsage(usage *Usage) *StreamEvent {
	se.Usage = usage
	return se
}

// WithError adds an error to the stream event.
func (se *StreamEvent) WithError(errType, message string) *StreamEvent {
	se.Error = &StreamError{
		Type:    errType,
		Message: message,
	}
	return se
}

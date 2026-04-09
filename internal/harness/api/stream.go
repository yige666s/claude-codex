package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// StreamProcessor handles streaming responses from the API
type StreamProcessor struct {
	mu              sync.Mutex
	contentBuilder  strings.Builder
	currentIndex    int
	totalInputTokens  int
	totalOutputTokens int
	stopReason      string
	messageID       string
	model           string
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor() *StreamProcessor {
	return &StreamProcessor{
		currentIndex: -1,
	}
}

// ProcessEvent processes a single streaming event
func (sp *StreamProcessor) ProcessEvent(event StreamEvent) error {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	switch event.Type {
	case "message_start":
		// Initialize message metadata
		return nil

	case "content_block_start":
		sp.currentIndex = event.Index
		return nil

	case "content_block_delta":
		if event.Delta != nil && event.Delta.Type == "text_delta" {
			sp.contentBuilder.WriteString(event.Delta.Text)
		}
		return nil

	case "content_block_stop":
		// Content block completed
		return nil

	case "message_delta":
		if event.Usage != nil {
			sp.totalOutputTokens = event.Usage.OutputTokens
		}
		return nil

	case "message_stop":
		// Message completed
		return nil

	case "error":
		if event.Error != nil {
			return fmt.Errorf("stream error: %s", event.Error.Message)
		}
		return fmt.Errorf("unknown stream error")

	default:
		// Unknown event type, ignore
		return nil
	}
}

// GetContent returns the accumulated content
func (sp *StreamProcessor) GetContent() string {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.contentBuilder.String()
}

// GetUsage returns the token usage statistics
func (sp *StreamProcessor) GetUsage() Usage {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return Usage{
		InputTokens:  sp.totalInputTokens,
		OutputTokens: sp.totalOutputTokens,
	}
}

// StreamToResponse converts a stream of events into a complete Response
func StreamToResponse(ctx context.Context, eventChan <-chan StreamEvent) (*Response, error) {
	processor := NewStreamProcessor()
	var lastError error

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-eventChan:
			if !ok {
				// Channel closed, return accumulated response
				if lastError != nil {
					return nil, lastError
				}

				usage := processor.GetUsage()
				content := processor.GetContent()

				return &Response{
					Type: "message",
					Role: "assistant",
					Content: []ContentBlock{
						{
							Type: "text",
							Text: content,
						},
					},
					Usage: usage,
				}, nil
			}

			if err := processor.ProcessEvent(event); err != nil {
				lastError = err
			}
		}
	}
}

// StreamHandler is a callback function for handling streaming events
type StreamHandler func(event StreamEvent) error

// ProcessStream processes a stream with a custom handler
func ProcessStream(ctx context.Context, eventChan <-chan StreamEvent, handler StreamHandler) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-eventChan:
			if !ok {
				return nil
			}

			if err := handler(event); err != nil {
				return err
			}
		}
	}
}

// TextStreamHandler is a simple handler that accumulates text
type TextStreamHandler struct {
	mu      sync.Mutex
	builder strings.Builder
	OnText  func(text string) // Optional callback for each text delta
}

// NewTextStreamHandler creates a new text stream handler
func NewTextStreamHandler() *TextStreamHandler {
	return &TextStreamHandler{}
}

// Handle processes a streaming event
func (h *TextStreamHandler) Handle(event StreamEvent) error {
	if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "text_delta" {
		h.mu.Lock()
		h.builder.WriteString(event.Delta.Text)
		h.mu.Unlock()

		if h.OnText != nil {
			h.OnText(event.Delta.Text)
		}
	}

	if event.Type == "error" {
		if event.Error != nil {
			return fmt.Errorf("stream error: %s", event.Error.Message)
		}
		return fmt.Errorf("unknown stream error")
	}

	return nil
}

// GetText returns the accumulated text
func (h *TextStreamHandler) GetText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.builder.String()
}

// StreamCollector collects all events from a stream
type StreamCollector struct {
	mu     sync.Mutex
	events []StreamEvent
}

// NewStreamCollector creates a new stream collector
func NewStreamCollector() *StreamCollector {
	return &StreamCollector{
		events: make([]StreamEvent, 0),
	}
}

// Collect adds an event to the collection
func (sc *StreamCollector) Collect(event StreamEvent) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.events = append(sc.events, event)
	return nil
}

// GetEvents returns all collected events
func (sc *StreamCollector) GetEvents() []StreamEvent {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	result := make([]StreamEvent, len(sc.events))
	copy(result, sc.events)
	return result
}

// GetEventCount returns the number of collected events
func (sc *StreamCollector) GetEventCount() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.events)
}

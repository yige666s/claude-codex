package types

import "time"

// EventType represents the type of event.
type EventType string

const (
	EventTypeSessionStart    EventType = "session_start"
	EventTypeSessionEnd      EventType = "session_end"
	EventTypeMessageSent     EventType = "message_sent"
	EventTypeMessageReceived EventType = "message_received"
	EventTypeToolCall        EventType = "tool_call"
	EventTypeToolResult      EventType = "tool_result"
	EventTypeError           EventType = "error"
	EventTypePermission      EventType = "permission"
	EventTypeCompaction      EventType = "compaction"
	EventTypeAgentSpawn      EventType = "agent_spawn"
	EventTypeAgentComplete   EventType = "agent_complete"
)

// Event represents a system event.
type Event struct {
	ID        string                 `json:"id"`
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	SessionID string                 `json:"session_id,omitempty"`
	Data      interface{}            `json:"data,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// EventHandler is a function that handles events.
type EventHandler func(event Event)

// EventBus manages event subscriptions and publishing.
type EventBus interface {
	// Subscribe subscribes to events of a specific type.
	Subscribe(eventType EventType, handler EventHandler) string

	// Unsubscribe removes a subscription.
	Unsubscribe(subscriptionID string)

	// Publish publishes an event to all subscribers.
	Publish(event Event)

	// PublishAsync publishes an event asynchronously.
	PublishAsync(event Event)
}

// NewEvent creates a new event.
func NewEvent(eventType EventType, data interface{}) *Event {
	return &Event{
		ID:        UUID(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Data:      data,
		Metadata:  make(map[string]interface{}),
	}
}

// WithSessionID adds a session ID to the event.
func (e *Event) WithSessionID(sessionID string) *Event {
	e.SessionID = sessionID
	return e
}

// WithMetadata adds metadata to the event.
func (e *Event) WithMetadata(key string, value interface{}) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]interface{})
	}
	e.Metadata[key] = value
	return e
}

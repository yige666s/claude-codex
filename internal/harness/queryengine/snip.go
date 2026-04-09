// Package engine provides history snipping and replay functionality.
package engine

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SnipConfig contains configuration for history snipping.
type SnipConfig struct {
	// MaxMessages is the maximum number of messages before snipping
	MaxMessages int

	// PreserveRecentCount is the number of recent messages to preserve
	PreserveRecentCount int

	// PreserveSystemMessages preserves system messages during snipping
	PreserveSystemMessages bool

	// Force forces snipping even if under threshold
	Force bool
}

// DefaultSnipConfig returns the default snip configuration.
func DefaultSnipConfig() SnipConfig {
	return SnipConfig{
		MaxMessages:            100,
		PreserveRecentCount:    20,
		PreserveSystemMessages: true,
		Force:                  false,
	}
}

// SnipResult contains the result of a snip operation.
type SnipResult struct {
	// Messages is the snipped message history
	Messages []Message

	// Executed indicates if snipping was actually performed
	Executed bool

	// RemovedCount is the number of messages removed
	RemovedCount int

	// BoundaryMessage is the compact boundary message inserted
	BoundaryMessage *Message

	// PreservedSegment identifies the preserved portion
	PreservedSegment *PreservedSegment
}

// SnipCompactor handles history snipping and compaction.
type SnipCompactor struct {
	config SnipConfig
}

// NewSnipCompactor creates a new snip compactor.
func NewSnipCompactor(config SnipConfig) *SnipCompactor {
	return &SnipCompactor{
		config: config,
	}
}

// ShouldSnip determines if snipping should be performed.
func (sc *SnipCompactor) ShouldSnip(messages []Message) bool {
	if sc.config.Force {
		return true
	}
	return len(messages) > sc.config.MaxMessages
}

// Snip performs history snipping on the message list.
func (sc *SnipCompactor) Snip(messages []Message) (*SnipResult, error) {
	if !sc.ShouldSnip(messages) {
		return &SnipResult{
			Messages: messages,
			Executed: false,
		}, nil
	}

	if len(messages) <= sc.config.PreserveRecentCount {
		return &SnipResult{
			Messages: messages,
			Executed: false,
		}, nil
	}

	// Calculate split point
	splitPoint := len(messages) - sc.config.PreserveRecentCount
	if splitPoint < 0 {
		splitPoint = 0
	}

	// Preserve system messages if configured
	var preserved []Message
	if sc.config.PreserveSystemMessages {
		for i := 0; i < splitPoint; i++ {
			if messages[i].Type == "system" {
				preserved = append(preserved, messages[i])
			}
		}
	}

	// Get the tail UUID (last message before split)
	var tailUUID string
	if splitPoint > 0 {
		tailUUID = messages[splitPoint-1].UUID
	}

	// Create compact boundary message
	boundaryMsg := Message{
		Type:      "system",
		Subtype:   "compact_boundary",
		UUID:      uuid.New().String(),
		Timestamp: time.Now(),
		CompactMetadata: &CompactMetadata{
			PreservedSegment: &PreservedSegment{
				TailUUID: tailUUID,
			},
		},
	}

	// Build new message list
	newMessages := make([]Message, 0, len(preserved)+1+sc.config.PreserveRecentCount)
	newMessages = append(newMessages, preserved...)
	newMessages = append(newMessages, boundaryMsg)
	newMessages = append(newMessages, messages[splitPoint:]...)

	removedCount := len(messages) - len(newMessages)

	return &SnipResult{
		Messages:        newMessages,
		Executed:        true,
		RemovedCount:    removedCount,
		BoundaryMessage: &boundaryMsg,
		PreservedSegment: &PreservedSegment{
			TailUUID: tailUUID,
		},
	}, nil
}

// SnipCompactIfNeeded performs snipping if needed based on configuration.
func SnipCompactIfNeeded(messages []Message, config SnipConfig) (*SnipResult, error) {
	compactor := NewSnipCompactor(config)
	return compactor.Snip(messages)
}

// IsSnipBoundaryMessage checks if a message is a snip boundary.
func IsSnipBoundaryMessage(msg Message) bool {
	return msg.Type == "system" && msg.Subtype == "compact_boundary"
}

// SnipProjection provides utilities for projecting snipped history.
type SnipProjection struct {
	messages []Message
}

// NewSnipProjection creates a new snip projection.
func NewSnipProjection(messages []Message) *SnipProjection {
	return &SnipProjection{
		messages: messages,
	}
}

// GetVisibleMessages returns messages that should be visible after snipping.
func (sp *SnipProjection) GetVisibleMessages() []Message {
	var visible []Message
	foundBoundary := false

	for i := len(sp.messages) - 1; i >= 0; i-- {
		msg := sp.messages[i]

		if IsSnipBoundaryMessage(msg) {
			foundBoundary = true
			visible = append([]Message{msg}, visible...)
			break
		}

		visible = append([]Message{msg}, visible...)
	}

	// If no boundary found, return all messages
	if !foundBoundary {
		return sp.messages
	}

	return visible
}

// GetPreservedSystemMessages returns system messages preserved during snipping.
func (sp *SnipProjection) GetPreservedSystemMessages() []Message {
	var preserved []Message
	foundBoundary := false

	for _, msg := range sp.messages {
		if IsSnipBoundaryMessage(msg) {
			foundBoundary = true
			continue
		}

		if !foundBoundary && msg.Type == "system" {
			preserved = append(preserved, msg)
		}

		if foundBoundary {
			break
		}
	}

	return preserved
}

// GetSnipBoundary returns the snip boundary message if present.
func (sp *SnipProjection) GetSnipBoundary() *Message {
	for _, msg := range sp.messages {
		if IsSnipBoundaryMessage(msg) {
			return &msg
		}
	}
	return nil
}

// HasSnipBoundary checks if the message list contains a snip boundary.
func (sp *SnipProjection) HasSnipBoundary() bool {
	return sp.GetSnipBoundary() != nil
}

// GetMessagesSinceBoundary returns messages after the snip boundary.
func (sp *SnipProjection) GetMessagesSinceBoundary() []Message {
	var afterBoundary []Message
	foundBoundary := false

	for _, msg := range sp.messages {
		if foundBoundary {
			afterBoundary = append(afterBoundary, msg)
			continue
		}

		if IsSnipBoundaryMessage(msg) {
			foundBoundary = true
		}
	}

	return afterBoundary
}

// ReplaySnippedHistory replays snipped history for a new session.
func ReplaySnippedHistory(messages []Message, config SnipConfig) (*SnipResult, error) {
	projection := NewSnipProjection(messages)

	// If already snipped, return visible messages
	if projection.HasSnipBoundary() {
		return &SnipResult{
			Messages: projection.GetVisibleMessages(),
			Executed: false,
		}, nil
	}

	// Otherwise, perform snipping
	return SnipCompactIfNeeded(messages, config)
}

// SnipStats provides statistics about snipped history.
type SnipStats struct {
	TotalMessages     int
	VisibleMessages   int
	HiddenMessages    int
	PreservedSystem   int
	HasBoundary       bool
	BoundaryTimestamp *time.Time
}

// GetSnipStats calculates statistics for snipped history.
func GetSnipStats(messages []Message) SnipStats {
	projection := NewSnipProjection(messages)
	visible := projection.GetVisibleMessages()
	preserved := projection.GetPreservedSystemMessages()
	boundary := projection.GetSnipBoundary()

	stats := SnipStats{
		TotalMessages:   len(messages),
		VisibleMessages: len(visible),
		HiddenMessages:  len(messages) - len(visible),
		PreservedSystem: len(preserved),
		HasBoundary:     boundary != nil,
	}

	if boundary != nil {
		stats.BoundaryTimestamp = &boundary.Timestamp
	}

	return stats
}

// ValidateSnipResult validates that a snip result is well-formed.
func ValidateSnipResult(result *SnipResult) error {
	if result == nil {
		return fmt.Errorf("snip result is nil")
	}

	if !result.Executed {
		return nil
	}

	if result.BoundaryMessage == nil {
		return fmt.Errorf("executed snip must have boundary message")
	}

	if !IsSnipBoundaryMessage(*result.BoundaryMessage) {
		return fmt.Errorf("boundary message is not a valid snip boundary")
	}

	if result.PreservedSegment == nil {
		return fmt.Errorf("executed snip must have preserved segment")
	}

	// Verify boundary message exists in result messages
	found := false
	for _, msg := range result.Messages {
		if msg.UUID == result.BoundaryMessage.UUID {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("boundary message not found in result messages")
	}

	return nil
}

// MergeSnippedSessions merges two snipped sessions.
func MergeSnippedSessions(session1, session2 []Message) ([]Message, error) {
	proj1 := NewSnipProjection(session1)
	proj2 := NewSnipProjection(session2)

	// Get preserved system messages from both
	preserved1 := proj1.GetPreservedSystemMessages()
	preserved2 := proj2.GetPreservedSystemMessages()

	// Merge preserved messages (deduplicate by UUID)
	preservedMap := make(map[string]Message)
	for _, msg := range preserved1 {
		preservedMap[msg.UUID] = msg
	}
	for _, msg := range preserved2 {
		preservedMap[msg.UUID] = msg
	}

	var preserved []Message
	for _, msg := range preservedMap {
		preserved = append(preserved, msg)
	}

	// Get messages after boundary from both sessions
	after1 := proj1.GetMessagesSinceBoundary()
	after2 := proj2.GetMessagesSinceBoundary()

	// Create new boundary
	boundaryMsg := Message{
		Type:      "system",
		Subtype:   "compact_boundary",
		UUID:      uuid.New().String(),
		Timestamp: time.Now(),
		CompactMetadata: &CompactMetadata{
			PreservedSegment: &PreservedSegment{
				TailUUID: "", // No specific tail for merged sessions
			},
		},
	}

	// Build merged message list
	merged := make([]Message, 0, len(preserved)+1+len(after1)+len(after2))
	merged = append(merged, preserved...)
	merged = append(merged, boundaryMsg)
	merged = append(merged, after1...)
	merged = append(merged, after2...)

	return merged, nil
}

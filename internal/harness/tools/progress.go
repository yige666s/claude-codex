package tools

import (
	"context"
	"encoding/json"
)

// ProgressEvent represents a progress update during tool execution
type ProgressEvent struct {
	ToolName string  `json:"tool_name"`
	Status   string  `json:"status"` // "started", "progress", "completed", "failed"
	Message  string  `json:"message,omitempty"`
	Progress float64 `json:"progress,omitempty"` // 0.0 to 1.0
}

// ProgressReporter allows tools to report progress during execution
type ProgressReporter interface {
	Report(event ProgressEvent)
}

// ProgressCallback is a function that receives progress events
type ProgressCallback func(ProgressEvent)

// NoOpProgressReporter is a progress reporter that does nothing
type NoOpProgressReporter struct{}

func (n NoOpProgressReporter) Report(event ProgressEvent) {}

// ChannelProgressReporter sends progress events to a channel
type ChannelProgressReporter struct {
	ch chan<- ProgressEvent
}

func NewChannelProgressReporter(ch chan<- ProgressEvent) *ChannelProgressReporter {
	return &ChannelProgressReporter{ch: ch}
}

func (c *ChannelProgressReporter) Report(event ProgressEvent) {
	select {
	case c.ch <- event:
	default:
		// Don't block if channel is full
	}
}

// ProgressAwareTool is an optional interface that tools can implement
// to receive a progress reporter during execution
type ProgressAwareTool interface {
	Tool
	ExecuteWithProgress(ctx context.Context, input json.RawMessage, reporter ProgressReporter) (Result, error)
}

// Result now includes optional progress information
type ResultWithProgress struct {
	Result
	Events []ProgressEvent `json:"events,omitempty"`
}

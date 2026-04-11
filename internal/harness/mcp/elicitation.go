package mcp

import (
	"context"
	"sync"
)

type ElicitationWaitingState struct {
	ActionLabel string `json:"action_label"`
	ShowCancel  bool   `json:"show_cancel,omitempty"`
}

type ElicitationRequestEvent struct {
	ServerName       string
	RequestID        string
	Params           map[string]any
	WaitingState     *ElicitationWaitingState
	Completed        bool
	OnWaitingDismiss func(action string)
}

type ElicitationResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

type ElicitationRegistry struct {
	mu      sync.Mutex
	queue   []ElicitationRequestEvent
	pending map[string]chan ElicitationResult
}

func NewElicitationRegistry() *ElicitationRegistry {
	return &ElicitationRegistry{
		pending: make(map[string]chan ElicitationResult),
	}
}

func (r *ElicitationRegistry) Enqueue(serverName, requestID string, params map[string]any, waiting *ElicitationWaitingState) <-chan ElicitationResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan ElicitationResult, 1)
	r.pending[requestID] = ch
	r.queue = append(r.queue, ElicitationRequestEvent{
		ServerName:   serverName,
		RequestID:    requestID,
		Params:       cloneMapAny(params),
		WaitingState: waiting,
	})
	return ch
}

func (r *ElicitationRegistry) Resolve(requestID string, result ElicitationResult) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := r.pending[requestID]
	if ch == nil {
		return false
	}
	delete(r.pending, requestID)
	ch <- result
	close(ch)
	for i := range r.queue {
		if r.queue[i].RequestID == requestID {
			r.queue = append(r.queue[:i], r.queue[i+1:]...)
			break
		}
	}
	return true
}

func (r *ElicitationRegistry) MarkCompleted(serverName, requestID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.queue {
		if r.queue[i].ServerName == serverName && r.queue[i].RequestID == requestID {
			r.queue[i].Completed = true
			return true
		}
	}
	return false
}

func (r *ElicitationRegistry) Queue() []ElicitationRequestEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ElicitationRequestEvent, len(r.queue))
	copy(out, r.queue)
	return out
}

func HandleElicitationWithContext(ctx context.Context, registry *ElicitationRegistry, serverName, requestID string, params map[string]any, waiting *ElicitationWaitingState) (ElicitationResult, error) {
	if registry == nil {
		return ElicitationResult{Action: "cancel"}, nil
	}
	ch := registry.Enqueue(serverName, requestID, params, waiting)
	select {
	case <-ctx.Done():
		registry.Resolve(requestID, ElicitationResult{Action: "cancel"})
		return ElicitationResult{Action: "cancel"}, ctx.Err()
	case result := <-ch:
		return result, nil
	}
}

func cloneMapAny(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

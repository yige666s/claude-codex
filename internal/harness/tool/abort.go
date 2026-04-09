package tool

import (
	"context"
	"sync"
)

// AbortController provides cancellation control for tool execution.
type AbortController struct {
	Signal *AbortSignal
	mu     sync.Mutex
}

// AbortSignal represents the signal state of an abort controller.
type AbortSignal struct {
	Aborted bool
	Reason  string
	mu      sync.RWMutex
}

// NewAbortController creates a new abort controller.
func NewAbortController() *AbortController {
	return &AbortController{
		Signal: &AbortSignal{},
	}
}

// Abort aborts the operation with the given reason.
func (ac *AbortController) Abort(reason string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.Signal.mu.Lock()
	defer ac.Signal.mu.Unlock()

	ac.Signal.Aborted = true
	ac.Signal.Reason = reason
}

// IsAborted returns whether the operation has been aborted.
func (as *AbortSignal) IsAborted() bool {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return as.Aborted
}

// GetReason returns the abort reason.
func (as *AbortSignal) GetReason() string {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return as.Reason
}

// Context returns a context that is cancelled when the signal is aborted.
func (as *AbortSignal) Context(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)

	go func() {
		for {
			if as.IsAborted() {
				cancel()
				return
			}
			// Check every 100ms
			select {
			case <-ctx.Done():
				return
			case <-parent.Done():
				cancel()
				return
			default:
				// Continue checking
			}
		}
	}()

	return ctx, cancel
}

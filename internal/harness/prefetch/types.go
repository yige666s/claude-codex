package prefetch

import (
	"context"
	"sync"
	"time"
)

// MemoryPrefetch represents a memory relevance-selector prefetch handle.
// The promise runs asynchronously while the main model streams and tools execute.
// At the collect point (post-tools), the caller checks SettledAt to consume-if-ready
// or skip-and-retry-next-iteration — the prefetch never blocks the turn.
type MemoryPrefetch struct {
	// Result channel that delivers the memory attachments
	ResultChan <-chan []MemoryAttachment

	mu                  sync.RWMutex
	settledAt           *time.Time
	consumedOnIteration int
	err                 error

	// cancel function to abort the prefetch
	cancel context.CancelFunc

	// firedAt tracks when the prefetch was started (for telemetry)
	firedAt time.Time
}

// MemoryAttachment represents a memory file attachment.
type MemoryAttachment struct {
	Path        string
	Content     string
	Type        string // "memory"
	Description string
	MtimeMs     int64
}

// PrefetchConfig configures memory prefetch behavior.
type PrefetchConfig struct {
	// MaxSessionBytes is the maximum total bytes of memories to surface in a session
	MaxSessionBytes int

	// Enabled controls whether memory prefetch is active
	Enabled bool

	// Timeout for the prefetch operation
	Timeout time.Duration
}

// DefaultPrefetchConfig returns the default configuration.
func DefaultPrefetchConfig() *PrefetchConfig {
	return &PrefetchConfig{
		MaxSessionBytes: 100000, // 100KB
		Enabled:         true,
		Timeout:         30 * time.Second,
	}
}

// Dispose cancels the prefetch and emits terminal telemetry.
// This is called automatically when the prefetch is no longer needed.
func (p *MemoryPrefetch) Dispose() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}

	// Emit telemetry
	latencyMs := time.Since(p.firedAt).Milliseconds()
	p.mu.RLock()
	hiddenByFirstIteration := p.settledAt != nil && p.consumedOnIteration == 0
	consumedOnIteration := p.consumedOnIteration
	p.mu.RUnlock()

	// Log telemetry event
	logPrefetchTelemetry(hiddenByFirstIteration, consumedOnIteration, latencyMs)
}

// IsSettled returns true if the prefetch has completed.
func (p *MemoryPrefetch) IsSettled() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.settledAt != nil
}

// IsConsumed returns true if the prefetch result has been consumed.
func (p *MemoryPrefetch) IsConsumed() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.consumedOnIteration >= 0
}

// SettledAt returns when the prefetch completed.
func (p *MemoryPrefetch) SettledAt() *time.Time {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.settledAt == nil {
		return nil
	}
	settledAt := *p.settledAt
	return &settledAt
}

// Error returns the terminal prefetch error, if any.
func (p *MemoryPrefetch) Error() error {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.err
}

// ConsumedOnIteration reports the iteration that consumed the result.
func (p *MemoryPrefetch) ConsumedOnIteration() int {
	if p == nil {
		return -1
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.consumedOnIteration
}

// MarkConsumed records which query iteration consumed the result.
func (p *MemoryPrefetch) MarkConsumed(iteration int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.consumedOnIteration = iteration
	p.mu.Unlock()
}

func (p *MemoryPrefetch) settle(err error) {
	now := time.Now()
	p.mu.Lock()
	p.settledAt = &now
	p.err = err
	p.mu.Unlock()
}

// logPrefetchTelemetry logs telemetry for memory prefetch.
func logPrefetchTelemetry(hiddenByFirstIteration bool, consumedOnIteration int, latencyMs int64) {
	// TODO: Integrate with analytics system
	// For now, just a placeholder
	_ = hiddenByFirstIteration
	_ = consumedOnIteration
	_ = latencyMs
}

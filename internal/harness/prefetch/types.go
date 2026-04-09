package prefetch

import (
	"context"
	"time"
)

// MemoryPrefetch represents a memory relevance-selector prefetch handle.
// The promise runs asynchronously while the main model streams and tools execute.
// At the collect point (post-tools), the caller checks SettledAt to consume-if-ready
// or skip-and-retry-next-iteration — the prefetch never blocks the turn.
type MemoryPrefetch struct {
	// Result channel that delivers the memory attachments
	ResultChan <-chan []MemoryAttachment

	// SettledAt is set when the prefetch completes (success or error)
	// nil until the promise settles
	SettledAt *time.Time

	// ConsumedOnIteration tracks which iteration consumed this prefetch
	// -1 until consumed
	ConsumedOnIteration int

	// Error holds any error that occurred during prefetch
	Error error

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
	if p.cancel != nil {
		p.cancel()
	}

	// Emit telemetry
	latencyMs := time.Since(p.firedAt).Milliseconds()
	hiddenByFirstIteration := p.SettledAt != nil && p.ConsumedOnIteration == 0

	// Log telemetry event
	logPrefetchTelemetry(hiddenByFirstIteration, p.ConsumedOnIteration, latencyMs)
}

// IsSettled returns true if the prefetch has completed.
func (p *MemoryPrefetch) IsSettled() bool {
	return p.SettledAt != nil
}

// IsConsumed returns true if the prefetch result has been consumed.
func (p *MemoryPrefetch) IsConsumed() bool {
	return p.ConsumedOnIteration >= 0
}

// logPrefetchTelemetry logs telemetry for memory prefetch.
func logPrefetchTelemetry(hiddenByFirstIteration bool, consumedOnIteration int, latencyMs int64) {
	// TODO: Integrate with analytics system
	// For now, just a placeholder
	_ = hiddenByFirstIteration
	_ = consumedOnIteration
	_ = latencyMs
}

package cost

import (
	"sync"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// Tracker manages cost and usage tracking across models and sessions.
type Tracker struct {
	mu                              sync.RWMutex
	totalCostUSD                    float64
	totalAPIDuration                int64
	totalAPIDurationWithoutRetries  int64
	totalToolDuration               int64
	totalLinesAdded                 int
	totalLinesRemoved               int
	totalInputTokens                int
	totalOutputTokens               int
	totalCacheCreationInputTokens   int
	totalCacheReadInputTokens       int
	totalWebSearchRequests          int
	modelUsage                      map[string]*ModelUsage
	hasUnknownModelCost             bool
}

// ModelUsage tracks usage statistics for a specific model.
type ModelUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	WebSearchRequests        int     `json:"web_search_requests"`
	CostUSD                  float64 `json:"cost_usd"`
	ContextWindow            int     `json:"context_window"`
	MaxOutputTokens          int     `json:"max_output_tokens"`
}

// NewTracker creates a new cost tracker.
func NewTracker() *Tracker {
	return &Tracker{
		modelUsage: make(map[string]*ModelUsage),
	}
}

// AddUsage adds usage data for a model.
func (t *Tracker) AddUsage(model string, usage *types.Usage, cost float64, contextWindow, maxOutputTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Update totals
	t.totalCostUSD += cost
	t.totalInputTokens += usage.InputTokens
	t.totalOutputTokens += usage.OutputTokens
	t.totalCacheCreationInputTokens += usage.CacheCreationInputTokens
	t.totalCacheReadInputTokens += usage.CacheReadInputTokens

	// Update model-specific usage
	mu, exists := t.modelUsage[model]
	if !exists {
		mu = &ModelUsage{
			ContextWindow:   contextWindow,
			MaxOutputTokens: maxOutputTokens,
		}
		t.modelUsage[model] = mu
	}

	mu.InputTokens += usage.InputTokens
	mu.OutputTokens += usage.OutputTokens
	mu.CacheReadInputTokens += usage.CacheReadInputTokens
	mu.CacheCreationInputTokens += usage.CacheCreationInputTokens
	mu.CostUSD += cost
}

// AddAPIDuration adds API call duration.
func (t *Tracker) AddAPIDuration(duration int64, withRetries bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalAPIDuration += duration
	if !withRetries {
		t.totalAPIDurationWithoutRetries += duration
	}
}

// AddToolDuration adds tool execution duration.
func (t *Tracker) AddToolDuration(duration int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalToolDuration += duration
}

// AddLinesChanged adds lines added/removed counts.
func (t *Tracker) AddLinesChanged(added, removed int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalLinesAdded += added
	t.totalLinesRemoved += removed
}

// AddWebSearchRequests adds web search request count.
func (t *Tracker) AddWebSearchRequests(count int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalWebSearchRequests += count
}

// GetTotalCostUSD returns the total cost in USD.
func (t *Tracker) GetTotalCostUSD() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalCostUSD
}

// GetTotalInputTokens returns total input tokens.
func (t *Tracker) GetTotalInputTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalInputTokens
}

// GetTotalOutputTokens returns total output tokens.
func (t *Tracker) GetTotalOutputTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalOutputTokens
}

// GetTotalCacheCreationInputTokens returns total cache creation tokens.
func (t *Tracker) GetTotalCacheCreationInputTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalCacheCreationInputTokens
}

// GetTotalCacheReadInputTokens returns total cache read tokens.
func (t *Tracker) GetTotalCacheReadInputTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalCacheReadInputTokens
}

// GetTotalAPIDuration returns total API duration in milliseconds.
func (t *Tracker) GetTotalAPIDuration() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalAPIDuration
}

// GetTotalAPIDurationWithoutRetries returns API duration excluding retries.
func (t *Tracker) GetTotalAPIDurationWithoutRetries() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalAPIDurationWithoutRetries
}

// GetTotalToolDuration returns total tool execution duration.
func (t *Tracker) GetTotalToolDuration() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalToolDuration
}

// GetTotalLinesAdded returns total lines added.
func (t *Tracker) GetTotalLinesAdded() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalLinesAdded
}

// GetTotalLinesRemoved returns total lines removed.
func (t *Tracker) GetTotalLinesRemoved() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalLinesRemoved
}

// GetTotalWebSearchRequests returns total web search requests.
func (t *Tracker) GetTotalWebSearchRequests() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.totalWebSearchRequests
}

// GetModelUsage returns usage for all models.
func (t *Tracker) GetModelUsage() map[string]*ModelUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make(map[string]*ModelUsage, len(t.modelUsage))
	for k, v := range t.modelUsage {
		copy := *v
		result[k] = &copy
	}
	return result
}

// GetUsageForModel returns usage for a specific model.
func (t *Tracker) GetUsageForModel(model string) *ModelUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if mu, exists := t.modelUsage[model]; exists {
		copy := *mu
		return &copy
	}
	return nil
}

// SetHasUnknownModelCost marks that an unknown model was used.
func (t *Tracker) SetHasUnknownModelCost(value bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hasUnknownModelCost = value
}

// HasUnknownModelCost returns whether an unknown model was used.
func (t *Tracker) HasUnknownModelCost() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.hasUnknownModelCost
}

// Reset clears all tracked data.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalCostUSD = 0
	t.totalAPIDuration = 0
	t.totalAPIDurationWithoutRetries = 0
	t.totalToolDuration = 0
	t.totalLinesAdded = 0
	t.totalLinesRemoved = 0
	t.totalInputTokens = 0
	t.totalOutputTokens = 0
	t.totalCacheCreationInputTokens = 0
	t.totalCacheReadInputTokens = 0
	t.totalWebSearchRequests = 0
	t.modelUsage = make(map[string]*ModelUsage)
	t.hasUnknownModelCost = false
}

// Snapshot returns a complete snapshot of current state.
type Snapshot struct {
	TotalCostUSD                    float64                `json:"total_cost_usd"`
	TotalAPIDuration                int64                  `json:"total_api_duration"`
	TotalAPIDurationWithoutRetries  int64                  `json:"total_api_duration_without_retries"`
	TotalToolDuration               int64                  `json:"total_tool_duration"`
	TotalLinesAdded                 int                    `json:"total_lines_added"`
	TotalLinesRemoved               int                    `json:"total_lines_removed"`
	TotalInputTokens                int                    `json:"total_input_tokens"`
	TotalOutputTokens               int                    `json:"total_output_tokens"`
	TotalCacheCreationInputTokens   int                    `json:"total_cache_creation_input_tokens"`
	TotalCacheReadInputTokens       int                    `json:"total_cache_read_input_tokens"`
	TotalWebSearchRequests          int                    `json:"total_web_search_requests"`
	ModelUsage                      map[string]*ModelUsage `json:"model_usage"`
	HasUnknownModelCost             bool                   `json:"has_unknown_model_cost"`
}

// GetSnapshot returns a snapshot of current state.
func (t *Tracker) GetSnapshot() *Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return &Snapshot{
		TotalCostUSD:                    t.totalCostUSD,
		TotalAPIDuration:                t.totalAPIDuration,
		TotalAPIDurationWithoutRetries:  t.totalAPIDurationWithoutRetries,
		TotalToolDuration:               t.totalToolDuration,
		TotalLinesAdded:                 t.totalLinesAdded,
		TotalLinesRemoved:               t.totalLinesRemoved,
		TotalInputTokens:                t.totalInputTokens,
		TotalOutputTokens:               t.totalOutputTokens,
		TotalCacheCreationInputTokens:   t.totalCacheCreationInputTokens,
		TotalCacheReadInputTokens:       t.totalCacheReadInputTokens,
		TotalWebSearchRequests:          t.totalWebSearchRequests,
		ModelUsage:                      t.GetModelUsage(),
		HasUnknownModelCost:             t.hasUnknownModelCost,
	}
}

// RestoreFromSnapshot restores state from a snapshot.
func (t *Tracker) RestoreFromSnapshot(s *Snapshot) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalCostUSD = s.TotalCostUSD
	t.totalAPIDuration = s.TotalAPIDuration
	t.totalAPIDurationWithoutRetries = s.TotalAPIDurationWithoutRetries
	t.totalToolDuration = s.TotalToolDuration
	t.totalLinesAdded = s.TotalLinesAdded
	t.totalLinesRemoved = s.TotalLinesRemoved
	t.totalInputTokens = s.TotalInputTokens
	t.totalOutputTokens = s.TotalOutputTokens
	t.totalCacheCreationInputTokens = s.TotalCacheCreationInputTokens
	t.totalCacheReadInputTokens = s.TotalCacheReadInputTokens
	t.totalWebSearchRequests = s.TotalWebSearchRequests
	t.hasUnknownModelCost = s.HasUnknownModelCost

	t.modelUsage = make(map[string]*ModelUsage, len(s.ModelUsage))
	for k, v := range s.ModelUsage {
		copy := *v
		t.modelUsage[k] = &copy
	}
}

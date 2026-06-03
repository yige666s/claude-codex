package agentruntime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/plannerapi"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type llmScopeContextKey struct{}

type LLMScope struct {
	UserID    string
	SessionID string
	JobID     string
	SkillName string
	RequestID string
}

func WithLLMScope(ctx context.Context, scope LLMScope) context.Context {
	return context.WithValue(ctx, llmScopeContextKey{}, scope)
}

func llmScopeFromContext(ctx context.Context) LLMScope {
	scope, _ := ctx.Value(llmScopeContextKey{}).(LLMScope)
	if scope.RequestID == "" {
		scope.RequestID = requestIDFromContext(ctx)
	}
	return scope
}

type LLMGovernanceConfig struct {
	Provider               string
	Model                  string
	VertexLocation         string
	ModelRoutes            string
	MaxAttempts            int
	RetryBackoff           time.Duration
	ChatTimeout            time.Duration
	SkillTimeout           time.Duration
	DailyTokenQuota        int
	DailyRequestQuota      int
	DailyCostQuotaUSD      float64
	InputCostPerMillion    float64
	OutputCostPerMillion   float64
	FailureThreshold       int
	CircuitBreakerCooldown time.Duration
}

func (c LLMGovernanceConfig) normalized() LLMGovernanceConfig {
	return c.normalizedWithOptions(defaultLLMModelOptions())
}

func (c LLMGovernanceConfig) normalizedWithOptions(options []LLMModelOption) LLMGovernanceConfig {
	options = normalizeLLMModelOptions(options)
	if option, ok := llmModelOptionFor(c.Model, options); ok {
		if c.Provider == "" {
			c.Provider = option.Provider
		}
		if c.VertexLocation == "" {
			c.VertexLocation = option.VertexLocation
		}
		if c.ModelRoutes == "" {
			c.ModelRoutes = LLMModelRoutesWithDefault("", option.ID)
		} else if !modelRoutesCompatibleWithProvider(c.ModelRoutes, c.Provider, options) {
			c.ModelRoutes = resetModelRoutes(c.ModelRoutes, option.ID)
		}
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 1
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 250 * time.Millisecond
	}
	if c.ChatTimeout <= 0 {
		c.ChatTimeout = 60 * time.Second
	}
	if c.SkillTimeout <= 0 {
		c.SkillTimeout = c.ChatTimeout
	}
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = 3
	}
	if c.CircuitBreakerCooldown <= 0 {
		c.CircuitBreakerCooldown = time.Minute
	}
	return c
}

type LLMBackend struct {
	Name     string
	Provider string
	Model    string
	Planner  plannerapi.Planner
}

type LLMUsageRecord struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	SessionID        string    `json:"session_id"`
	RequestID        string    `json:"request_id,omitempty"`
	SkillName        string    `json:"skill_name,omitempty"`
	PromptID         string    `json:"prompt_id,omitempty"`
	PromptVersion    string    `json:"prompt_version,omitempty"`
	PromptHash       string    `json:"prompt_hash,omitempty"`
	ExperimentID     string    `json:"experiment_id,omitempty"`
	VariantID        string    `json:"variant_id,omitempty"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	InputTokens      int       `json:"input_tokens"`
	OutputTokens     int       `json:"output_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	Attempt          int       `json:"attempt"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
	LatencyMs        int64     `json:"latency_ms"`
	TTFTMs           int64     `json:"ttft_ms,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type LLMUsageSummary struct {
	Requests         int     `json:"requests"`
	InputTokens      int     `json:"input_tokens"`
	OutputTokens     int     `json:"output_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

type LLMUsageAdminFilter struct {
	UserID        string
	Since         time.Time
	Limit         int
	PromptID      string
	PromptVersion string
	PromptHash    string
	ExperimentID  string
	VariantID     string
}

type LLMUsageAdminSummary struct {
	Since            time.Time            `json:"since"`
	Requests         int                  `json:"requests"`
	Successes        int                  `json:"successes"`
	Failures         int                  `json:"failures"`
	InputTokens      int                  `json:"input_tokens"`
	OutputTokens     int                  `json:"output_tokens"`
	TotalTokens      int                  `json:"total_tokens"`
	EstimatedCostUSD float64              `json:"estimated_cost_usd"`
	AverageLatencyMs float64              `json:"average_latency_ms"`
	ByProvider       []LLMUsageAdminGroup `json:"by_provider"`
	Recent           []LLMUsageRecord     `json:"recent"`
}

type LLMUsageAdminGroup struct {
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	Status           string  `json:"status"`
	Requests         int     `json:"requests"`
	TotalTokens      int     `json:"total_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

type LLMQuotaAdjustment struct {
	ID                    string    `json:"id"`
	UserID                string    `json:"user_id"`
	ActorUserID           string    `json:"actor_user_id,omitempty"`
	Reason                string    `json:"reason,omitempty"`
	RequestDelta          int       `json:"request_delta"`
	InputTokenDelta       int       `json:"input_token_delta"`
	OutputTokenDelta      int       `json:"output_token_delta"`
	TotalTokenDelta       int       `json:"total_token_delta"`
	EstimatedCostDeltaUSD float64   `json:"estimated_cost_delta_usd"`
	CreatedAt             time.Time `json:"created_at"`
}

type LLMQuotaAdminSummary struct {
	Since             time.Time            `json:"since"`
	RawUsage          LLMUsageSummary      `json:"raw_usage"`
	Adjustments       LLMUsageSummary      `json:"adjustments"`
	EffectiveUsage    LLMUsageSummary      `json:"effective_usage"`
	RecentAdjustments []LLMQuotaAdjustment `json:"recent_adjustments"`
}

type LLMUsageStore interface {
	LLMUsageAdminStore
	RecordLLMUsage(ctx context.Context, record LLMUsageRecord) error
	SumLLMUsage(ctx context.Context, userID string, since time.Time) (LLMUsageSummary, error)
}

type LLMUsageAdminStore interface {
	SummarizeLLMUsage(ctx context.Context, filter LLMUsageAdminFilter) (LLMUsageAdminSummary, error)
	SummarizeLLMQuota(ctx context.Context, userID string, since time.Time, limit int) (LLMQuotaAdminSummary, error)
	RecordLLMQuotaAdjustment(ctx context.Context, adjustment LLMQuotaAdjustment) error
}

type LLMGovernanceStatus struct {
	Backends []LLMBackendStatus `json:"backends"`
	Config   map[string]any     `json:"config"`
}

type LLMBackendStatus struct {
	Name                string     `json:"name"`
	Provider            string     `json:"provider"`
	Model               string     `json:"model"`
	Healthy             bool       `json:"healthy"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt         *time.Time `json:"last_error_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	DisabledUntil       *time.Time `json:"disabled_until,omitempty"`
}

type GovernedPlanner struct {
	backends []governedBackend
	store    LLMUsageStore
	config   LLMGovernanceConfig
	mu       sync.Mutex
}

type governedBackend struct {
	LLMBackend
	state llmBackendRuntimeState
}

type llmBackendRuntimeState struct {
	consecutiveFailures int
	lastSuccessAt       time.Time
	lastErrorAt         time.Time
	lastError           string
	disabledUntil       time.Time
}

func NewGovernedPlanner(backends []LLMBackend, store LLMUsageStore, config LLMGovernanceConfig) (*GovernedPlanner, error) {
	if len(backends) == 0 {
		return nil, fmt.Errorf("at least one LLM backend is required")
	}
	out := make([]governedBackend, 0, len(backends))
	for i, backend := range backends {
		if backend.Planner == nil {
			return nil, fmt.Errorf("llm backend %d planner is nil", i)
		}
		if strings.TrimSpace(backend.Name) == "" {
			backend.Name = backend.Provider
			if backend.Name == "" {
				backend.Name = fmt.Sprintf("backend-%d", i+1)
			}
		}
		out = append(out, governedBackend{LLMBackend: backend})
	}
	return &GovernedPlanner{backends: out, store: store, config: config.normalized()}, nil
}

func (p *GovernedPlanner) Next(ctx context.Context, session *state.Session, tools []toolkit.Descriptor) (plannerapi.Plan, error) {
	return p.execute(ctx, session, tools, nil)
}

func (p *GovernedPlanner) StreamNext(ctx context.Context, session *state.Session, tools []toolkit.Descriptor, onChunk func(string)) (plannerapi.Plan, error) {
	return p.execute(ctx, session, tools, onChunk)
}

func (p *GovernedPlanner) Status() LLMGovernanceStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	backends := make([]LLMBackendStatus, 0, len(p.backends))
	now := time.Now().UTC()
	for _, backend := range p.backends {
		status := LLMBackendStatus{
			Name:                backend.Name,
			Provider:            backend.Provider,
			Model:               backend.Model,
			Healthy:             !backend.state.disabledUntil.After(now),
			ConsecutiveFailures: backend.state.consecutiveFailures,
			LastError:           backend.state.lastError,
		}
		if !backend.state.lastSuccessAt.IsZero() {
			value := backend.state.lastSuccessAt
			status.LastSuccessAt = &value
		}
		if !backend.state.lastErrorAt.IsZero() {
			value := backend.state.lastErrorAt
			status.LastErrorAt = &value
		}
		if backend.state.disabledUntil.After(now) {
			value := backend.state.disabledUntil
			status.DisabledUntil = &value
		}
		backends = append(backends, status)
	}
	return LLMGovernanceStatus{
		Backends: backends,
		Config:   llmGovernanceConfigStatusMap(p.config),
	}
}

func (p *GovernedPlanner) execute(ctx context.Context, session *state.Session, tools []toolkit.Descriptor, onChunk func(string)) (plannerapi.Plan, error) {
	scope := llmScopeFromContext(ctx)
	if err := p.checkQuota(ctx, scope); err != nil {
		return plannerapi.Plan{}, err
	}
	inputTokens := estimateSessionTokens(session)
	retryPolicy := RetryPolicy{
		MaxAttempts: p.config.MaxAttempts,
		BaseDelay:   p.config.RetryBackoff,
		MaxDelay:    30 * time.Second,
		Jitter:      0.2,
	}
	var lastErr error
	for attempt := 1; attempt <= retryPolicy.Attempts(); attempt++ {
		for idx := range p.backends {
			if !p.backendAvailable(idx) {
				continue
			}
			backend := p.backends[idx]
			plan, latency, ttft, err := p.callBackend(ctx, backend, session, tools, onChunk)
			if err == nil {
				outputTokens := estimatePlanTokens(plan)
				_ = p.record(ctx, scope, backend.LLMBackend, attempt, "success", "", inputTokens, outputTokens, latency, ttft)
				p.markSuccess(idx)
				return plan, nil
			}
			lastErr = err
			_ = p.record(ctx, scope, backend.LLMBackend, attempt, "error", err.Error(), inputTokens, 0, latency, ttft)
			if !isRetryableLLMError(err) {
				p.markRequestError(idx, err)
				return plannerapi.Plan{}, err
			}
			p.markFailure(idx, err)
		}
		if attempt < retryPolicy.Attempts() {
			if err := retryPolicy.Sleep(ctx, attempt, lastErr); err != nil {
				return plannerapi.Plan{}, err
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no healthy LLM backend is currently available")
	}
	return plannerapi.Plan{}, lastErr
}

func (p *GovernedPlanner) callBackend(ctx context.Context, backend governedBackend, session *state.Session, tools []toolkit.Descriptor, onChunk func(string)) (plannerapi.Plan, int64, int64, error) {
	timeout := p.config.ChatTimeout
	if strings.TrimSpace(llmScopeFromContext(ctx).SkillName) != "" {
		timeout = p.config.SkillTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now()
	var (
		plan plannerapi.Plan
		err  error
	)
	var firstTokenAt time.Time
	wrappedChunk := onChunk
	if onChunk != nil {
		wrappedChunk = func(token string) {
			if firstTokenAt.IsZero() && token != "" {
				firstTokenAt = time.Now()
			}
			onChunk(token)
		}
	}
	if onChunk != nil {
		if streaming, ok := backend.Planner.(engine.StreamingPlanner); ok {
			plan, err = streaming.StreamNext(callCtx, session, tools, wrappedChunk)
		} else {
			plan, err = backend.Planner.Next(callCtx, session, tools)
			if err == nil && plan.AssistantText != "" {
				wrappedChunk(plan.AssistantText)
			}
		}
	} else {
		plan, err = backend.Planner.Next(callCtx, session, tools)
	}
	latency := time.Since(started).Milliseconds()
	ttft := latency
	if !firstTokenAt.IsZero() {
		ttft = firstTokenAt.Sub(started).Milliseconds()
	}
	if err != nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("llm call timed out after %s: %w", timeout, err)
	}
	return plan, latency, ttft, err
}

func (p *GovernedPlanner) backendAvailable(index int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.backends) {
		return false
	}
	return !p.backends[index].state.disabledUntil.After(time.Now().UTC())
}

func (p *GovernedPlanner) markSuccess(index int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.backends) {
		return
	}
	p.backends[index].state.consecutiveFailures = 0
	p.backends[index].state.lastSuccessAt = time.Now().UTC()
	p.backends[index].state.lastError = ""
	p.backends[index].state.disabledUntil = time.Time{}
}

func (p *GovernedPlanner) markFailure(index int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.backends) {
		return
	}
	state := &p.backends[index].state
	state.consecutiveFailures++
	state.lastErrorAt = time.Now().UTC()
	state.lastError = err.Error()
	if state.consecutiveFailures >= p.config.FailureThreshold {
		state.disabledUntil = time.Now().UTC().Add(p.config.CircuitBreakerCooldown)
	}
}

func (p *GovernedPlanner) markRequestError(index int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.backends) {
		return
	}
	state := &p.backends[index].state
	state.lastErrorAt = time.Now().UTC()
	state.lastError = err.Error()
}

func (p *GovernedPlanner) checkQuota(ctx context.Context, scope LLMScope) error {
	if p.store == nil || strings.TrimSpace(scope.UserID) == "" {
		return nil
	}
	if p.config.DailyTokenQuota <= 0 && p.config.DailyRequestQuota <= 0 && p.config.DailyCostQuotaUSD <= 0 {
		return nil
	}
	summary, err := p.store.SumLLMUsage(ctx, scope.UserID, startOfUTCDay(time.Now()))
	if err != nil {
		return fmt.Errorf("check llm quota: %w", err)
	}
	if p.config.DailyRequestQuota > 0 && summary.Requests >= p.config.DailyRequestQuota {
		return fmt.Errorf("daily LLM request quota exceeded")
	}
	if p.config.DailyTokenQuota > 0 && summary.TotalTokens >= p.config.DailyTokenQuota {
		return fmt.Errorf("daily LLM token quota exceeded")
	}
	if p.config.DailyCostQuotaUSD > 0 && summary.EstimatedCostUSD >= p.config.DailyCostQuotaUSD {
		return fmt.Errorf("daily LLM cost quota exceeded")
	}
	return nil
}

func (p *GovernedPlanner) record(ctx context.Context, scope LLMScope, backend LLMBackend, attempt int, status, errorText string, inputTokens, outputTokens int, latencyMs, ttftMs int64) error {
	if p.store == nil || strings.TrimSpace(scope.UserID) == "" {
		return nil
	}
	totalTokens := inputTokens + outputTokens
	prompt := promptMetadataFromContext(ctx)
	return p.store.RecordLLMUsage(ctx, LLMUsageRecord{
		ID:               newLLMUsageID(),
		UserID:           scope.UserID,
		SessionID:        scope.SessionID,
		RequestID:        scope.RequestID,
		SkillName:        scope.SkillName,
		PromptID:         prompt.PromptID,
		PromptVersion:    prompt.PromptVersion,
		PromptHash:       prompt.PromptHash,
		ExperimentID:     prompt.ExperimentID,
		VariantID:        prompt.VariantID,
		Provider:         firstNonEmptyString(backend.Provider, backend.Name),
		Model:            backend.Model,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		TotalTokens:      totalTokens,
		EstimatedCostUSD: estimateLLMCost(inputTokens, outputTokens, p.config.InputCostPerMillion, p.config.OutputCostPerMillion),
		Attempt:          attempt,
		Status:           status,
		Error:            truncateString(errorText, 2000),
		LatencyMs:        latencyMs,
		TTFTMs:           ttftMs,
		CreatedAt:        time.Now().UTC(),
	})
}

func estimateSessionTokens(session *state.Session) int {
	if session == nil {
		return 0
	}
	total := 0
	for _, msg := range session.Messages {
		total += estimateTextTokens(msg.Content)
		total += estimateTextTokens(msg.ToolOutput)
		if len(msg.ToolInput) > 0 {
			total += estimateTextTokens(string(msg.ToolInput))
		}
	}
	return total
}

func estimatePlanTokens(plan plannerapi.Plan) int {
	total := estimateTextTokens(plan.AssistantText)
	for _, call := range plan.ToolCalls {
		total += estimateTextTokens(call.Name)
		total += estimateTextTokens(string(call.Input))
	}
	return total
}

func estimateTextTokens(value string) int {
	if value == "" {
		return 0
	}
	tokens := len(value) / 4
	if len(value)%4 != 0 {
		tokens++
	}
	if tokens == 0 {
		tokens = 1
	}
	return tokens
}

func estimateLLMCost(inputTokens, outputTokens int, inputPerMillion, outputPerMillion float64) float64 {
	if inputPerMillion < 0 {
		inputPerMillion = 0
	}
	if outputPerMillion < 0 {
		outputPerMillion = 0
	}
	cost := (float64(inputTokens)*inputPerMillion + float64(outputTokens)*outputPerMillion) / 1_000_000
	return math.Round(cost*1_000_000) / 1_000_000
}

func isRetryableLLMError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"timeout", "deadline exceeded", "temporary", "connection reset", "connection refused",
		"eof", "too many requests", "rate limit", "429", "500", "502", "503", "504",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func startOfUTCDay(now time.Time) time.Time {
	year, month, day := now.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func newLLMUsageID() string {
	var buffer [4]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "llm-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return "llm-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(buffer[:])
}

func newLLMQuotaAdjustmentID() string {
	var buffer [4]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "llmquota-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return "llmquota-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(buffer[:])
}

func truncateString(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type SQLLLMUsageStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

type MemoryLLMUsageStore struct {
	mu          sync.Mutex
	records     []LLMUsageRecord
	adjustments []LLMQuotaAdjustment
}

func NewMemoryLLMUsageStore() *MemoryLLMUsageStore {
	return &MemoryLLMUsageStore{}
}

func (s *MemoryLLMUsageStore) RecordLLMUsage(_ context.Context, record LLMUsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.ID == "" {
		record.ID = newLLMUsageID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	s.records = append(s.records, record)
	return nil
}

func (s *MemoryLLMUsageStore) SumLLMUsage(_ context.Context, userID string, since time.Time) (LLMUsageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := s.rawUsageLocked(userID, since)
	applyQuotaAdjustments(&summary, s.adjustmentSummaryLocked(userID, since))
	return clampLLMUsageSummary(summary), nil
}

func (s *MemoryLLMUsageStore) rawUsageLocked(userID string, since time.Time) LLMUsageSummary {
	var summary LLMUsageSummary
	for _, record := range s.records {
		if record.UserID != userID || record.Status != "success" || record.CreatedAt.Before(since) {
			continue
		}
		summary.Requests++
		summary.InputTokens += record.InputTokens
		summary.OutputTokens += record.OutputTokens
		summary.TotalTokens += record.TotalTokens
		summary.EstimatedCostUSD += record.EstimatedCostUSD
	}
	return summary
}

func (s *MemoryLLMUsageStore) adjustmentSummaryLocked(userID string, since time.Time) LLMUsageSummary {
	var summary LLMUsageSummary
	for _, adjustment := range s.adjustments {
		if adjustment.UserID != userID || adjustment.CreatedAt.Before(since) {
			continue
		}
		applyQuotaAdjustment(&summary, adjustment)
	}
	return summary
}

func (s *MemoryLLMUsageStore) SummarizeLLMUsage(_ context.Context, filter LLMUsageAdminFilter) (LLMUsageAdminSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return summarizeLLMUsageRecords(s.records, filter), nil
}

func (s *MemoryLLMUsageStore) RecordLLMQuotaAdjustment(_ context.Context, adjustment LLMQuotaAdjustment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	adjustment = normalizeLLMQuotaAdjustment(adjustment)
	s.adjustments = append(s.adjustments, adjustment)
	return nil
}

func (s *MemoryLLMUsageStore) SummarizeLLMQuota(_ context.Context, userID string, since time.Time, limit int) (LLMQuotaAdminSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	raw := s.rawUsageLocked(userID, since)
	adjustments := s.adjustmentSummaryLocked(userID, since)
	effective := raw
	applyQuotaAdjustments(&effective, adjustments)
	recent := make([]LLMQuotaAdjustment, 0, minInt(len(s.adjustments), limit))
	for i := len(s.adjustments) - 1; i >= 0 && len(recent) < limit; i-- {
		adjustment := s.adjustments[i]
		if adjustment.UserID == userID && !adjustment.CreatedAt.Before(since) {
			recent = append(recent, adjustment)
		}
	}
	return LLMQuotaAdminSummary{
		Since:             since,
		RawUsage:          raw,
		Adjustments:       adjustments,
		EffectiveUsage:    clampLLMUsageSummary(effective),
		RecentAdjustments: recent,
	}, nil
}

func NewSQLLLMUsageStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLLLMUsageStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLLLMUsageStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLLLMUsageStore) Init(ctx context.Context) error {
	if err := requireSQLColumns(ctx, s.db, "agent_llm_usage",
		"id", "user_id", "session_id", "request_id", "skill_name", "provider", "model",
		"prompt_id", "prompt_version", "prompt_hash", "experiment_id", "variant_id",
		"input_tokens", "output_tokens", "total_tokens", "estimated_cost_usd", "attempt",
		"status", "error", "latency_ms", "ttft_ms", "created_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_llm_quota_adjustments",
		"id", "user_id", "actor_user_id", "reason", "request_delta", "input_token_delta",
		"output_token_delta", "total_token_delta", "estimated_cost_delta_usd", "created_at",
	)
}

func (s *SQLLLMUsageStore) RecordLLMUsage(ctx context.Context, record LLMUsageRecord) error {
	if record.ID == "" {
		record.ID = newLLMUsageID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_llm_usage (
	id, user_id, session_id, request_id, skill_name, prompt_id, prompt_version, prompt_hash, experiment_id, variant_id, provider, model,
	input_tokens, output_tokens, total_tokens, estimated_cost_usd,
	attempt, status, error, latency_ms, ttft_ms, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID, record.UserID, record.SessionID, record.RequestID, record.SkillName,
		record.PromptID, record.PromptVersion, record.PromptHash, record.ExperimentID, record.VariantID,
		record.Provider, record.Model, record.InputTokens, record.OutputTokens, record.TotalTokens,
		record.EstimatedCostUSD, record.Attempt, record.Status, record.Error, record.LatencyMs, record.TTFTMs,
		sqlTimeValue(record.CreatedAt, s.dialect))
	return err
}

func (s *SQLLLMUsageStore) SumLLMUsage(ctx context.Context, userID string, since time.Time) (LLMUsageSummary, error) {
	summary, err := s.rawLLMUsage(ctx, userID, since)
	if err != nil {
		return LLMUsageSummary{}, err
	}
	adjustments, err := s.sumLLMQuotaAdjustments(ctx, userID, since)
	if err != nil {
		return LLMUsageSummary{}, err
	}
	applyQuotaAdjustments(&summary, adjustments)
	return clampLLMUsageSummary(summary), nil
}

func (s *SQLLLMUsageStore) rawLLMUsage(ctx context.Context, userID string, since time.Time) (LLMUsageSummary, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.SumLLMUsageSuccess(ctx, dbsqlc.SumLLMUsageSuccessParams{UserID: userID, CreatedAt: since.UTC()})
		if err != nil {
			return LLMUsageSummary{}, err
		}
		return LLMUsageSummary{
			Requests:         int(row.Requests),
			InputTokens:      int(row.InputTokens),
			OutputTokens:     int(row.OutputTokens),
			TotalTokens:      int(row.TotalTokens),
			EstimatedCostUSD: row.EstimatedCostUsd,
		}, nil
	}
	var summary LLMUsageSummary
	err := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT COUNT(*), COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(estimated_cost_usd), 0)
FROM agent_llm_usage
WHERE user_id = ? AND created_at >= ? AND status = 'success'`), userID, sqlTimeValue(since, s.dialect)).Scan(
		&summary.Requests,
		&summary.InputTokens,
		&summary.OutputTokens,
		&summary.TotalTokens,
		&summary.EstimatedCostUSD,
	)
	return summary, err
}

func (s *SQLLLMUsageStore) sumLLMQuotaAdjustments(ctx context.Context, userID string, since time.Time) (LLMUsageSummary, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.SumLLMQuotaAdjustments(ctx, dbsqlc.SumLLMQuotaAdjustmentsParams{UserID: userID, CreatedAt: since.UTC()})
		if err != nil {
			return LLMUsageSummary{}, err
		}
		return LLMUsageSummary{
			Requests:         int(row.Requests),
			InputTokens:      int(row.InputTokens),
			OutputTokens:     int(row.OutputTokens),
			TotalTokens:      int(row.TotalTokens),
			EstimatedCostUSD: row.EstimatedCostUsd,
		}, nil
	}
	var summary LLMUsageSummary
	err := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT COALESCE(SUM(request_delta), 0), COALESCE(SUM(input_token_delta), 0), COALESCE(SUM(output_token_delta), 0), COALESCE(SUM(total_token_delta), 0), COALESCE(SUM(estimated_cost_delta_usd), 0)
FROM agent_llm_quota_adjustments
WHERE user_id = ? AND created_at >= ?`), userID, sqlTimeValue(since, s.dialect)).Scan(
		&summary.Requests,
		&summary.InputTokens,
		&summary.OutputTokens,
		&summary.TotalTokens,
		&summary.EstimatedCostUSD,
	)
	return summary, err
}

func (s *SQLLLMUsageStore) SummarizeLLMUsage(ctx context.Context, filter LLMUsageAdminFilter) (LLMUsageAdminSummary, error) {
	filter = normalizeLLMUsageAdminFilter(filter)
	if s.dialect == SQLDialectPostgres && s.queries != nil && !llmUsageFilterHasPromptExperiment(filter) {
		userID := strings.TrimSpace(filter.UserID)
		totals, err := s.queries.SummarizeLLMUsageTotals(ctx, dbsqlc.SummarizeLLMUsageTotalsParams{Since: filter.Since.UTC(), UserID: userID})
		if err != nil {
			return LLMUsageAdminSummary{}, err
		}
		summary := LLMUsageAdminSummary{
			Since:            filter.Since,
			Requests:         int(totals.Requests),
			Successes:        int(totals.Successes),
			Failures:         int(totals.Failures),
			InputTokens:      int(totals.InputTokens),
			OutputTokens:     int(totals.OutputTokens),
			TotalTokens:      int(totals.TotalTokens),
			EstimatedCostUSD: totals.EstimatedCostUsd,
			AverageLatencyMs: totals.AverageLatencyMs,
		}
		groups, err := s.queries.ListLLMUsageGroups(ctx, dbsqlc.ListLLMUsageGroupsParams{Since: filter.Since.UTC(), UserID: userID})
		if err != nil {
			return LLMUsageAdminSummary{}, err
		}
		for _, group := range groups {
			summary.ByProvider = append(summary.ByProvider, LLMUsageAdminGroup{
				Provider:         group.Provider,
				Model:            group.Model,
				Status:           group.Status,
				Requests:         int(group.Requests),
				TotalTokens:      int(group.TotalTokens),
				EstimatedCostUSD: math.Round(group.EstimatedCostUsd*1_000_000) / 1_000_000,
			})
		}
		recent, err := s.listRecentLLMUsage(ctx, filter)
		if err != nil {
			return LLMUsageAdminSummary{}, err
		}
		summary.Recent = recent
		summary.EstimatedCostUSD = math.Round(summary.EstimatedCostUSD*1_000_000) / 1_000_000
		return summary, nil
	}
	where, args := llmUsageWhere(filter, s.dialect)

	summary := LLMUsageAdminSummary{Since: filter.Since}
	err := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(input_tokens), 0),
	COALESCE(SUM(output_tokens), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(estimated_cost_usd), 0),
	COALESCE(AVG(NULLIF(latency_ms, 0)), 0)
FROM agent_llm_usage`+where), args...).Scan(
		&summary.Requests,
		&summary.Successes,
		&summary.Failures,
		&summary.InputTokens,
		&summary.OutputTokens,
		&summary.TotalTokens,
		&summary.EstimatedCostUSD,
		&summary.AverageLatencyMs,
	)
	if err != nil {
		return LLMUsageAdminSummary{}, err
	}

	groupRows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT provider, model, status, COUNT(*), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(estimated_cost_usd), 0)
FROM agent_llm_usage`+where+`
GROUP BY provider, model, status
ORDER BY COALESCE(SUM(estimated_cost_usd), 0) DESC, COUNT(*) DESC`), args...)
	if err != nil {
		return LLMUsageAdminSummary{}, err
	}
	defer groupRows.Close()
	for groupRows.Next() {
		var group LLMUsageAdminGroup
		if err := groupRows.Scan(&group.Provider, &group.Model, &group.Status, &group.Requests, &group.TotalTokens, &group.EstimatedCostUSD); err != nil {
			return LLMUsageAdminSummary{}, err
		}
		group.EstimatedCostUSD = math.Round(group.EstimatedCostUSD*1_000_000) / 1_000_000
		summary.ByProvider = append(summary.ByProvider, group)
	}
	if err := groupRows.Err(); err != nil {
		return LLMUsageAdminSummary{}, err
	}

	recentArgs := append([]any{}, args...)
	recentArgs = append(recentArgs, filter.Limit)
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT id, user_id, session_id, request_id, skill_name, prompt_id, prompt_version, prompt_hash, experiment_id, variant_id, provider, model, input_tokens, output_tokens, total_tokens, estimated_cost_usd, attempt, status, error, latency_ms, ttft_ms, created_at FROM agent_llm_usage`+where+` ORDER BY created_at DESC LIMIT ?`), recentArgs...)
	if err != nil {
		return LLMUsageAdminSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanLLMUsageRecord(rows)
		if err != nil {
			return LLMUsageAdminSummary{}, err
		}
		summary.Recent = append(summary.Recent, record)
	}
	if err := rows.Err(); err != nil {
		return LLMUsageAdminSummary{}, err
	}
	summary.EstimatedCostUSD = math.Round(summary.EstimatedCostUSD*1_000_000) / 1_000_000
	return summary, nil
}

func (s *SQLLLMUsageStore) listRecentLLMUsage(ctx context.Context, filter LLMUsageAdminFilter) ([]LLMUsageRecord, error) {
	where, args := llmUsageWhere(filter, s.dialect)
	args = append(args, filter.Limit)
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT id, user_id, session_id, request_id, skill_name, prompt_id, prompt_version, prompt_hash, experiment_id, variant_id, provider, model, input_tokens, output_tokens, total_tokens, estimated_cost_usd, attempt, status, error, latency_ms, ttft_ms, created_at FROM agent_llm_usage`+where+` ORDER BY created_at DESC LIMIT ?`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LLMUsageRecord{}
	for rows.Next() {
		record, err := scanLLMUsageRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLLLMUsageStore) RecordLLMQuotaAdjustment(ctx context.Context, adjustment LLMQuotaAdjustment) error {
	adjustment = normalizeLLMQuotaAdjustment(adjustment)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.InsertLLMQuotaAdjustment(ctx, dbsqlc.InsertLLMQuotaAdjustmentParams{
			ID:                    adjustment.ID,
			UserID:                adjustment.UserID,
			ActorUserID:           sqlNullString(adjustment.ActorUserID),
			Reason:                sqlNullString(adjustment.Reason),
			RequestDelta:          int32(adjustment.RequestDelta),
			InputTokenDelta:       int32(adjustment.InputTokenDelta),
			OutputTokenDelta:      int32(adjustment.OutputTokenDelta),
			TotalTokenDelta:       int32(adjustment.TotalTokenDelta),
			EstimatedCostDeltaUsd: adjustment.EstimatedCostDeltaUSD,
			CreatedAt:             adjustment.CreatedAt.UTC(),
		})
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_llm_quota_adjustments (
	id, user_id, actor_user_id, reason, request_delta, input_token_delta,
	output_token_delta, total_token_delta, estimated_cost_delta_usd, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		adjustment.ID,
		adjustment.UserID,
		nullableString(adjustment.ActorUserID),
		nullableString(adjustment.Reason),
		adjustment.RequestDelta,
		adjustment.InputTokenDelta,
		adjustment.OutputTokenDelta,
		adjustment.TotalTokenDelta,
		adjustment.EstimatedCostDeltaUSD,
		sqlTimeValue(adjustment.CreatedAt, s.dialect),
	)
	return err
}

func (s *SQLLLMUsageStore) SummarizeLLMQuota(ctx context.Context, userID string, since time.Time, limit int) (LLMQuotaAdminSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	raw, err := s.rawLLMUsage(ctx, userID, since)
	if err != nil {
		return LLMQuotaAdminSummary{}, err
	}
	adjustments, err := s.sumLLMQuotaAdjustments(ctx, userID, since)
	if err != nil {
		return LLMQuotaAdminSummary{}, err
	}
	effective := raw
	applyQuotaAdjustments(&effective, adjustments)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListLLMQuotaAdjustments(ctx, dbsqlc.ListLLMQuotaAdjustmentsParams{
			UserID:    userID,
			CreatedAt: since.UTC(),
			Limit:     int32(limit),
		})
		if err != nil {
			return LLMQuotaAdminSummary{}, err
		}
		return LLMQuotaAdminSummary{
			Since:             since,
			RawUsage:          raw,
			Adjustments:       adjustments,
			EffectiveUsage:    clampLLMUsageSummary(effective),
			RecentAdjustments: llmQuotaAdjustmentsFromSQLC(rows),
		}, nil
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT id, user_id, actor_user_id, reason, request_delta, input_token_delta, output_token_delta, total_token_delta, estimated_cost_delta_usd, created_at
FROM agent_llm_quota_adjustments
WHERE user_id = ? AND created_at >= ?
ORDER BY created_at DESC
LIMIT ?`), userID, sqlTimeValue(since, s.dialect), limit)
	if err != nil {
		return LLMQuotaAdminSummary{}, err
	}
	defer rows.Close()
	recent := make([]LLMQuotaAdjustment, 0)
	for rows.Next() {
		adjustment, err := scanLLMQuotaAdjustment(rows)
		if err != nil {
			return LLMQuotaAdminSummary{}, err
		}
		recent = append(recent, adjustment)
	}
	if err := rows.Err(); err != nil {
		return LLMQuotaAdminSummary{}, err
	}
	return LLMQuotaAdminSummary{
		Since:             since,
		RawUsage:          raw,
		Adjustments:       adjustments,
		EffectiveUsage:    clampLLMUsageSummary(effective),
		RecentAdjustments: recent,
	}, nil
}

type llmUsageScanner interface {
	Scan(dest ...any) error
}

func scanLLMUsageRecord(row llmUsageScanner) (LLMUsageRecord, error) {
	var record LLMUsageRecord
	var requestID, skillName, errorText sql.NullString
	var createdAt any
	if err := row.Scan(
		&record.ID,
		&record.UserID,
		&record.SessionID,
		&requestID,
		&skillName,
		&record.PromptID,
		&record.PromptVersion,
		&record.PromptHash,
		&record.ExperimentID,
		&record.VariantID,
		&record.Provider,
		&record.Model,
		&record.InputTokens,
		&record.OutputTokens,
		&record.TotalTokens,
		&record.EstimatedCostUSD,
		&record.Attempt,
		&record.Status,
		&errorText,
		&record.LatencyMs,
		&record.TTFTMs,
		&createdAt,
	); err != nil {
		return LLMUsageRecord{}, err
	}
	record.RequestID = requestID.String
	record.SkillName = skillName.String
	record.Error = errorText.String
	parsed, err := parseSQLTime(createdAt)
	if err != nil {
		return LLMUsageRecord{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}

func scanLLMQuotaAdjustment(row llmUsageScanner) (LLMQuotaAdjustment, error) {
	var adjustment LLMQuotaAdjustment
	var actorUserID, reason sql.NullString
	var createdAt any
	if err := row.Scan(
		&adjustment.ID,
		&adjustment.UserID,
		&actorUserID,
		&reason,
		&adjustment.RequestDelta,
		&adjustment.InputTokenDelta,
		&adjustment.OutputTokenDelta,
		&adjustment.TotalTokenDelta,
		&adjustment.EstimatedCostDeltaUSD,
		&createdAt,
	); err != nil {
		return LLMQuotaAdjustment{}, err
	}
	adjustment.ActorUserID = actorUserID.String
	adjustment.Reason = reason.String
	parsed, err := parseSQLTime(createdAt)
	if err != nil {
		return LLMQuotaAdjustment{}, err
	}
	adjustment.CreatedAt = parsed
	return adjustment, nil
}

func llmUsageRecordFromSQLC(row dbsqlc.AgentLlmUsage) LLMUsageRecord {
	return LLMUsageRecord{
		ID:               row.ID,
		UserID:           row.UserID,
		SessionID:        row.SessionID,
		RequestID:        row.RequestID.String,
		SkillName:        row.SkillName.String,
		Provider:         row.Provider,
		Model:            row.Model,
		InputTokens:      int(row.InputTokens),
		OutputTokens:     int(row.OutputTokens),
		TotalTokens:      int(row.TotalTokens),
		EstimatedCostUSD: row.EstimatedCostUsd,
		Attempt:          int(row.Attempt),
		Status:           row.Status,
		Error:            row.Error.String,
		LatencyMs:        row.LatencyMs,
		TTFTMs:           row.TtftMs,
		CreatedAt:        row.CreatedAt.UTC(),
	}
}

func llmUsageRecordsFromSQLC(rows []dbsqlc.AgentLlmUsage) []LLMUsageRecord {
	out := make([]LLMUsageRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, llmUsageRecordFromSQLC(row))
	}
	return out
}

func llmQuotaAdjustmentFromSQLC(row dbsqlc.AgentLlmQuotaAdjustment) LLMQuotaAdjustment {
	return LLMQuotaAdjustment{
		ID:                    row.ID,
		UserID:                row.UserID,
		ActorUserID:           row.ActorUserID.String,
		Reason:                row.Reason.String,
		RequestDelta:          int(row.RequestDelta),
		InputTokenDelta:       int(row.InputTokenDelta),
		OutputTokenDelta:      int(row.OutputTokenDelta),
		TotalTokenDelta:       int(row.TotalTokenDelta),
		EstimatedCostDeltaUSD: row.EstimatedCostDeltaUsd,
		CreatedAt:             row.CreatedAt.UTC(),
	}
}

func llmQuotaAdjustmentsFromSQLC(rows []dbsqlc.AgentLlmQuotaAdjustment) []LLMQuotaAdjustment {
	out := make([]LLMQuotaAdjustment, 0, len(rows))
	for _, row := range rows {
		out = append(out, llmQuotaAdjustmentFromSQLC(row))
	}
	return out
}

func normalizeLLMQuotaAdjustment(adjustment LLMQuotaAdjustment) LLMQuotaAdjustment {
	if adjustment.ID == "" {
		adjustment.ID = newLLMQuotaAdjustmentID()
	}
	adjustment.UserID = strings.TrimSpace(adjustment.UserID)
	adjustment.ActorUserID = strings.TrimSpace(adjustment.ActorUserID)
	adjustment.Reason = truncateString(strings.TrimSpace(adjustment.Reason), 500)
	if adjustment.CreatedAt.IsZero() {
		adjustment.CreatedAt = time.Now().UTC()
	}
	adjustment.EstimatedCostDeltaUSD = math.Round(adjustment.EstimatedCostDeltaUSD*1_000_000) / 1_000_000
	return adjustment
}

func applyQuotaAdjustment(summary *LLMUsageSummary, adjustment LLMQuotaAdjustment) {
	if summary == nil {
		return
	}
	summary.Requests += adjustment.RequestDelta
	summary.InputTokens += adjustment.InputTokenDelta
	summary.OutputTokens += adjustment.OutputTokenDelta
	summary.TotalTokens += adjustment.TotalTokenDelta
	summary.EstimatedCostUSD += adjustment.EstimatedCostDeltaUSD
}

func applyQuotaAdjustments(summary *LLMUsageSummary, adjustments LLMUsageSummary) {
	if summary == nil {
		return
	}
	summary.Requests += adjustments.Requests
	summary.InputTokens += adjustments.InputTokens
	summary.OutputTokens += adjustments.OutputTokens
	summary.TotalTokens += adjustments.TotalTokens
	summary.EstimatedCostUSD += adjustments.EstimatedCostUSD
}

func clampLLMUsageSummary(summary LLMUsageSummary) LLMUsageSummary {
	if summary.Requests < 0 {
		summary.Requests = 0
	}
	if summary.InputTokens < 0 {
		summary.InputTokens = 0
	}
	if summary.OutputTokens < 0 {
		summary.OutputTokens = 0
	}
	if summary.TotalTokens < 0 {
		summary.TotalTokens = 0
	}
	if summary.EstimatedCostUSD < 0 {
		summary.EstimatedCostUSD = 0
	}
	summary.EstimatedCostUSD = math.Round(summary.EstimatedCostUSD*1_000_000) / 1_000_000
	return summary
}

func normalizeLLMUsageAdminFilter(filter LLMUsageAdminFilter) LLMUsageAdminFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.PromptID = strings.TrimSpace(filter.PromptID)
	filter.PromptVersion = strings.TrimSpace(filter.PromptVersion)
	filter.PromptHash = strings.TrimSpace(filter.PromptHash)
	filter.ExperimentID = strings.TrimSpace(filter.ExperimentID)
	filter.VariantID = strings.TrimSpace(filter.VariantID)
	if filter.Since.IsZero() {
		filter.Since = startOfUTCDay(time.Now())
	}
	if filter.Limit <= 0 || filter.Limit > 1000 {
		filter.Limit = 200
	}
	return filter
}

func llmUsageFilterHasPromptExperiment(filter LLMUsageAdminFilter) bool {
	return filter.PromptID != "" || filter.PromptVersion != "" || filter.PromptHash != "" || filter.ExperimentID != "" || filter.VariantID != ""
}

func llmUsageWhere(filter LLMUsageAdminFilter, dialect SQLDialect) (string, []any) {
	where := ` WHERE created_at >= ?`
	args := []any{sqlTimeValue(filter.Since, dialect)}
	if filter.UserID != "" {
		where += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.PromptID != "" {
		where += ` AND prompt_id = ?`
		args = append(args, filter.PromptID)
	}
	if filter.PromptVersion != "" {
		where += ` AND prompt_version = ?`
		args = append(args, filter.PromptVersion)
	}
	if filter.PromptHash != "" {
		where += ` AND prompt_hash = ?`
		args = append(args, filter.PromptHash)
	}
	if filter.ExperimentID != "" {
		where += ` AND experiment_id = ?`
		args = append(args, filter.ExperimentID)
	}
	if filter.VariantID != "" {
		where += ` AND variant_id = ?`
		args = append(args, filter.VariantID)
	}
	return where, args
}

func summarizeLLMUsageRecords(records []LLMUsageRecord, filter LLMUsageAdminFilter) LLMUsageAdminSummary {
	filter = normalizeLLMUsageAdminFilter(filter)
	summary := LLMUsageAdminSummary{
		Since:  filter.Since,
		Recent: make([]LLMUsageRecord, 0, minInt(len(records), filter.Limit)),
	}
	groups := make(map[string]*LLMUsageAdminGroup)
	var latencyTotal int64
	var latencyCount int
	for _, record := range records {
		if strings.TrimSpace(filter.UserID) != "" && record.UserID != filter.UserID {
			continue
		}
		if filter.PromptID != "" && record.PromptID != filter.PromptID {
			continue
		}
		if filter.PromptVersion != "" && record.PromptVersion != filter.PromptVersion {
			continue
		}
		if filter.PromptHash != "" && record.PromptHash != filter.PromptHash {
			continue
		}
		if filter.ExperimentID != "" && record.ExperimentID != filter.ExperimentID {
			continue
		}
		if filter.VariantID != "" && record.VariantID != filter.VariantID {
			continue
		}
		if record.CreatedAt.Before(filter.Since) {
			continue
		}
		summary.Requests++
		if record.Status == "success" {
			summary.Successes++
		} else {
			summary.Failures++
		}
		summary.InputTokens += record.InputTokens
		summary.OutputTokens += record.OutputTokens
		summary.TotalTokens += record.TotalTokens
		summary.EstimatedCostUSD += record.EstimatedCostUSD
		if record.LatencyMs > 0 {
			latencyTotal += record.LatencyMs
			latencyCount++
		}
		key := strings.Join([]string{record.Provider, record.Model, record.Status}, "\x00")
		group := groups[key]
		if group == nil {
			group = &LLMUsageAdminGroup{Provider: record.Provider, Model: record.Model, Status: record.Status}
			groups[key] = group
		}
		group.Requests++
		group.TotalTokens += record.TotalTokens
		group.EstimatedCostUSD += record.EstimatedCostUSD
		if len(summary.Recent) < filter.Limit {
			summary.Recent = append(summary.Recent, record)
		}
	}
	if latencyCount > 0 {
		summary.AverageLatencyMs = float64(latencyTotal) / float64(latencyCount)
	}
	summary.EstimatedCostUSD = math.Round(summary.EstimatedCostUSD*1_000_000) / 1_000_000
	summary.ByProvider = make([]LLMUsageAdminGroup, 0, len(groups))
	for _, group := range groups {
		group.EstimatedCostUSD = math.Round(group.EstimatedCostUSD*1_000_000) / 1_000_000
		summary.ByProvider = append(summary.ByProvider, *group)
	}
	sort.Slice(summary.ByProvider, func(i, j int) bool {
		if summary.ByProvider[i].EstimatedCostUSD == summary.ByProvider[j].EstimatedCostUSD {
			return summary.ByProvider[i].Requests > summary.ByProvider[j].Requests
		}
		return summary.ByProvider[i].EstimatedCostUSD > summary.ByProvider[j].EstimatedCostUSD
	})
	return summary
}

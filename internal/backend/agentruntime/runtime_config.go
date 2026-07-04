package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
)

const (
	llmGovernanceConfigKey = "llm_governance"
	llmModelCatalogKey     = "llm_model_catalog"
)

type LLMGovernanceConfigPatch struct {
	Provider                *string  `json:"provider,omitempty"`
	Model                   *string  `json:"model,omitempty"`
	VertexLocation          *string  `json:"vertex_location,omitempty"`
	ModelRoutes             *string  `json:"model_routes,omitempty"`
	MaxAttempts             *int     `json:"max_attempts,omitempty"`
	RetryBackoffMS          *int64   `json:"retry_backoff_ms,omitempty"`
	ChatTimeoutMS           *int64   `json:"chat_timeout_ms,omitempty"`
	SkillTimeoutMS          *int64   `json:"skill_timeout_ms,omitempty"`
	MaxLoopDurationMS       *int64   `json:"max_loop_duration_ms,omitempty"`
	MaxLoopActions          *int     `json:"max_loop_actions,omitempty"`
	MaxBranchCount          *int     `json:"max_branch_count,omitempty"`
	MaxBranchConcurrency    *int     `json:"max_branch_concurrency,omitempty"`
	MaxParallelBranches     *int     `json:"max_parallel_branches,omitempty"`
	ParallelBranchTimeoutMS *int64   `json:"parallel_branch_timeout_ms,omitempty"`
	ParallelMaxToolCalls    *int     `json:"parallel_max_tool_calls,omitempty"`
	ParallelMaxSources      *int     `json:"parallel_max_sources,omitempty"`
	ParallelMaxTokens       *int     `json:"parallel_max_tokens,omitempty"`
	EvaluatorTimeoutMS      *int64   `json:"evaluator_timeout_ms,omitempty"`
	ConflictTimeoutMS       *int64   `json:"conflict_reconciliation_timeout_ms,omitempty"`
	MaxSourcesPerBranch     *int     `json:"max_sources_per_branch,omitempty"`
	SearchQualityThreshold  *float64 `json:"search_quality_threshold,omitempty"`
	AutomaticTriggerEnabled *bool    `json:"automatic_trigger_enabled,omitempty"`
	RiskyWriteApprovalMode  *string  `json:"risky_write_approval_mode,omitempty"`
	DailyTokenQuota         *int     `json:"daily_token_quota,omitempty"`
	DailyRequestQuota       *int     `json:"daily_request_quota,omitempty"`
	APIRateLimitPerMinute   *int     `json:"api_rate_limit_per_minute,omitempty"`
	DailyCostQuotaUSD       *float64 `json:"daily_cost_quota_usd,omitempty"`
	InputCostPerMillion     *float64 `json:"input_cost_per_million,omitempty"`
	OutputCostPerMillion    *float64 `json:"output_cost_per_million,omitempty"`
	FailureThreshold        *int     `json:"failure_threshold,omitempty"`
	CircuitCooldownSeconds  *int     `json:"circuit_cooldown_seconds,omitempty"`
}

type LLMGovernanceConfigStore interface {
	LoadLLMGovernanceConfig(ctx context.Context) (LLMGovernanceConfig, bool, error)
	SaveLLMGovernanceConfig(ctx context.Context, config LLMGovernanceConfig) error
	LoadLLMModelCatalog(ctx context.Context) ([]LLMModelOption, bool, error)
	SaveLLMModelCatalog(ctx context.Context, options []LLMModelOption) error
}

type LLMGovernanceConfigValidator func(ctx context.Context, config LLMGovernanceConfig) error

type LLMModelOption struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Provider       string `json:"provider"`
	VertexLocation string `json:"vertex_location"`
}

var allowedLLMModelOptions = []LLMModelOption{
	{ID: "simple", Label: "Simple Local Planner", Provider: "simple"},
	{ID: "deepseek-chat", Label: "DeepSeek Chat", Provider: "deepseek"},
	{ID: "deepseek-reasoner", Label: "DeepSeek Reasoner", Provider: "deepseek"},
	{ID: "nvidia/nemotron-3-ultra-550b-a55b", Label: "NVIDIA Nemotron 3 Ultra 550B", Provider: "nvidia"},
	{ID: "nvidia/nemotron-3-super-120b-a12b", Label: "NVIDIA Nemotron 3 Super 120B", Provider: "nvidia"},
	{ID: "nvidia/nemotron-3-nano-30b-a3b", Label: "NVIDIA Nemotron 3 Nano 30B", Provider: "nvidia"},
	{ID: "deepseek-ai/deepseek-v4-flash", Label: "DeepSeek V4 Flash (NVIDIA NIM)", Provider: "nvidia"},
	{ID: "qwen/qwen3-coder-480b-a35b-instruct", Label: "Qwen3 Coder 480B (NVIDIA NIM)", Provider: "nvidia"},
	{ID: "qwen/qwen3-next-80b-a3b-instruct", Label: "Qwen3 Next 80B (NVIDIA NIM)", Provider: "nvidia"},
	{ID: "gemini-3.5-flash", Label: "Gemini 3.5 Flash", Provider: "vertex", VertexLocation: "global"},
	{ID: "gemini-3.1-flash-lite", Label: "Gemini 3.1 Flash Lite", Provider: "vertex", VertexLocation: "global"},
	{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro", Provider: "vertex", VertexLocation: "us-central1"},
	{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash", Provider: "vertex", VertexLocation: "us-central1"},
	{ID: "google/gemini-3.1-pro-preview", Label: "Gemini 3.1 Pro Preview (ShortAPI)", Provider: "shortapi"},
}

func AllowedLLMModelOptions() []LLMModelOption {
	return defaultLLMModelOptions()
}

func defaultLLMModelOptions() []LLMModelOption {
	out := make([]LLMModelOption, len(allowedLLMModelOptions))
	copy(out, allowedLLMModelOptions)
	return out
}

func LLMModelOptionFor(model string) (LLMModelOption, bool) {
	return llmModelOptionFor(model, allowedLLMModelOptions)
}

func llmModelOptionFor(model string, options []LLMModelOption) (LLMModelOption, bool) {
	model = strings.TrimSpace(model)
	for _, option := range options {
		if option.ID == model {
			return option, true
		}
	}
	return LLMModelOption{}, false
}

func canonicalLLMModelProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "short":
		return "shortapi"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func isAllowedLLMModelProvider(provider string, options []LLMModelOption) bool {
	provider = canonicalLLMModelProvider(provider)
	if provider == "" {
		return true
	}
	for _, option := range options {
		if option.Provider == provider {
			return true
		}
	}
	return false
}

func LLMModelRoutesWithDefault(routes, model string) string {
	return setDefaultModelRoute(routes, model)
}

type LLMGovernanceConfigManager struct {
	mu            sync.RWMutex
	config        LLMGovernanceConfig
	allowedModels []LLMModelOption
	store         LLMGovernanceConfigStore
	validator     LLMGovernanceConfigValidator
}

func NewLLMGovernanceConfigManager(config LLMGovernanceConfig, store LLMGovernanceConfigStore) *LLMGovernanceConfigManager {
	allowedModels := defaultLLMModelOptions()
	return &LLMGovernanceConfigManager{config: config.normalizedWithOptions(allowedModels), allowedModels: allowedModels, store: store}
}

func (m *LLMGovernanceConfigManager) SetValidator(validator LLMGovernanceConfigValidator) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.validator = validator
	m.mu.Unlock()
}

func (m *LLMGovernanceConfigManager) loadAllowedModels(ctx context.Context) ([]LLMModelOption, error) {
	if m == nil {
		return defaultLLMModelOptions(), nil
	}
	m.mu.RLock()
	allowedModels := copyLLMModelOptions(m.allowedModels)
	m.mu.RUnlock()
	if len(allowedModels) == 0 {
		allowedModels = defaultLLMModelOptions()
	}
	if m.store == nil {
		return normalizeLLMModelOptions(allowedModels), nil
	}
	models, modelsOK, err := m.store.LoadLLMModelCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if modelsOK {
		var changed bool
		allowedModels, changed = mergeDefaultLLMModelOptions(models)
		if changed {
			if err := m.store.SaveLLMModelCatalog(ctx, allowedModels); err != nil {
				return nil, err
			}
		}
	} else {
		if err := m.store.SaveLLMModelCatalog(ctx, allowedModels); err != nil {
			return nil, err
		}
	}
	return normalizeLLMModelOptions(allowedModels), nil
}

func (m *LLMGovernanceConfigManager) Load(ctx context.Context) error {
	if m == nil {
		return nil
	}
	allowedModels, err := m.loadAllowedModels(ctx)
	if err != nil {
		return err
	}
	if m.store == nil {
		m.mu.Lock()
		m.allowedModels = allowedModels
		m.config = m.config.normalizedWithOptions(allowedModels)
		m.mu.Unlock()
		return nil
	}
	config, ok, err := m.store.LoadLLMGovernanceConfig(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.allowedModels = allowedModels
	if ok {
		config = mergeLLMGovernanceConfigDefaults(m.config, config)
		m.config = config.normalizedWithOptions(allowedModels)
	} else {
		m.config = m.config.normalizedWithOptions(allowedModels)
	}
	m.mu.Unlock()
	return nil
}

// LoadAndSyncStartupConfig is the service-start path: startup/env config is
// authoritative at process boot, then persisted so runtime reads and admin
// writes operate on the SQL-backed config row.
func (m *LLMGovernanceConfigManager) LoadAndSyncStartupConfig(ctx context.Context) error {
	if m == nil {
		return nil
	}
	allowedModels, err := m.loadAllowedModels(ctx)
	if err != nil {
		return err
	}
	m.mu.RLock()
	config := m.config
	m.mu.RUnlock()
	config = config.normalizedWithOptions(allowedModels)
	if m.store != nil {
		if err := m.store.SaveLLMGovernanceConfig(ctx, config); err != nil {
			return err
		}
	}
	m.mu.Lock()
	m.allowedModels = allowedModels
	m.config = config
	m.mu.Unlock()
	return nil
}

func (m *LLMGovernanceConfigManager) Get() LLMGovernanceConfig {
	if m == nil {
		return LLMGovernanceConfig{}.normalized()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *LLMGovernanceConfigManager) StatusMap() map[string]any {
	if m == nil {
		return llmGovernanceConfigStatusMapWithModels(LLMGovernanceConfig{}.normalized(), defaultLLMModelOptions())
	}
	m.mu.RLock()
	config := m.config
	allowedModels := copyLLMModelOptions(m.allowedModels)
	m.mu.RUnlock()
	return llmGovernanceConfigStatusMapWithModels(config, allowedModels)
}

func (m *LLMGovernanceConfigManager) Update(ctx context.Context, patch LLMGovernanceConfigPatch) (LLMGovernanceConfig, error) {
	if m == nil {
		return LLMGovernanceConfig{}, fmt.Errorf("llm governance config is not configured")
	}
	m.mu.RLock()
	current := m.config
	allowedModels := copyLLMModelOptions(m.allowedModels)
	validator := m.validator
	m.mu.RUnlock()
	next, err := applyLLMGovernanceConfigPatchWithOptions(current, patch, allowedModels)
	if err != nil {
		return LLMGovernanceConfig{}, err
	}
	if validator != nil {
		if err := validator(ctx, next); err != nil {
			return LLMGovernanceConfig{}, err
		}
	}
	if m.store != nil {
		if err := m.store.SaveLLMGovernanceConfig(ctx, next); err != nil {
			return LLMGovernanceConfig{}, err
		}
	}
	m.mu.Lock()
	m.config = next
	m.mu.Unlock()
	return next, nil
}

func llmGovernanceConfigStatusMap(config LLMGovernanceConfig) map[string]any {
	return llmGovernanceConfigStatusMapWithModels(config, defaultLLMModelOptions())
}

func llmGovernanceConfigStatusMapWithModels(config LLMGovernanceConfig, allowedModels []LLMModelOption) map[string]any {
	allowedModels = normalizeLLMModelOptions(allowedModels)
	config = config.normalizedWithOptions(allowedModels)
	return map[string]any{
		"provider":                           config.Provider,
		"model":                              config.Model,
		"vertex_location":                    config.VertexLocation,
		"model_routes":                       config.ModelRoutes,
		"allowed_models":                     allowedModels,
		"max_attempts":                       config.MaxAttempts,
		"retry_backoff_ms":                   config.RetryBackoff.Milliseconds(),
		"chat_timeout_ms":                    config.ChatTimeout.Milliseconds(),
		"skill_timeout_ms":                   config.SkillTimeout.Milliseconds(),
		"max_loop_duration_ms":               config.MaxLoopDuration.Milliseconds(),
		"max_loop_actions":                   config.MaxLoopActions,
		"max_branch_count":                   config.MaxBranchCount,
		"max_branch_concurrency":             config.MaxBranchConcurrency,
		"max_parallel_branches":              config.MaxParallelBranches,
		"parallel_branch_timeout_ms":         config.ParallelBranchTimeout.Milliseconds(),
		"parallel_max_tool_calls":            config.ParallelMaxToolCalls,
		"parallel_max_sources":               config.ParallelMaxSources,
		"parallel_max_tokens":                config.ParallelMaxTokens,
		"evaluator_timeout_ms":               config.EvaluatorTimeout.Milliseconds(),
		"conflict_reconciliation_timeout_ms": config.ConflictTimeout.Milliseconds(),
		"max_sources_per_branch":             config.MaxSourcesPerBranch,
		"search_quality_threshold":           config.SearchQualityThreshold,
		"automatic_trigger_enabled":          config.AutomaticTriggerEnabled,
		"risky_write_approval_mode":          config.RiskyWriteApprovalMode,
		"daily_token_quota":                  config.DailyTokenQuota,
		"daily_request_quota":                config.DailyRequestQuota,
		"api_rate_limit_per_minute":          config.APIRateLimitPerMinute,
		"daily_cost_quota_usd":               config.DailyCostQuotaUSD,
		"input_cost_per_million":             config.InputCostPerMillion,
		"output_cost_per_million":            config.OutputCostPerMillion,
		"failure_threshold":                  config.FailureThreshold,
		"circuit_cooldown_seconds":           int(config.CircuitBreakerCooldown.Seconds()),
	}
}

func applyLLMGovernanceConfigPatch(config LLMGovernanceConfig, patch LLMGovernanceConfigPatch) (LLMGovernanceConfig, error) {
	return applyLLMGovernanceConfigPatchWithOptions(config, patch, defaultLLMModelOptions())
}

func applyLLMGovernanceConfigPatchWithOptions(config LLMGovernanceConfig, patch LLMGovernanceConfigPatch, allowedModels []LLMModelOption) (LLMGovernanceConfig, error) {
	allowedModels = normalizeLLMModelOptions(allowedModels)
	next := config
	modelProviderChanged := false
	if patch.Provider != nil {
		provider := canonicalLLMModelProvider(*patch.Provider)
		if !isAllowedLLMModelProvider(provider, allowedModels) {
			return LLMGovernanceConfig{}, fmt.Errorf("provider %q is not in the model catalog", provider)
		}
		if provider != "" {
			next.Provider = provider
		}
	}
	if patch.Model != nil {
		model := strings.TrimSpace(*patch.Model)
		option, ok := llmModelOptionFor(model, allowedModels)
		if !ok {
			return LLMGovernanceConfig{}, fmt.Errorf("model %q is not allowed", model)
		}
		providerChanged := canonicalLLMModelProvider(next.Provider) != canonicalLLMModelProvider(option.Provider)
		modelProviderChanged = providerChanged
		next.Provider = option.Provider
		next.Model = option.ID
		next.VertexLocation = option.VertexLocation
		if providerChanged {
			next.ModelRoutes = resetModelRoutes(next.ModelRoutes, option.ID)
		} else {
			next.ModelRoutes = setPrimaryModelRoutes(next.ModelRoutes, option.ID)
		}
	}
	if patch.VertexLocation != nil {
		location := strings.TrimSpace(*patch.VertexLocation)
		if next.Model != "" {
			option, ok := llmModelOptionFor(next.Model, allowedModels)
			if ok && option.VertexLocation != "" && location != "" && location != option.VertexLocation {
				return LLMGovernanceConfig{}, fmt.Errorf("vertex_location for %s must be %s", next.Model, option.VertexLocation)
			}
		}
		if location != "" {
			next.VertexLocation = location
		}
	}
	if patch.ModelRoutes != nil {
		submittedRoutes := strings.TrimSpace(*patch.ModelRoutes)
		if modelProviderChanged && submittedRoutes == strings.TrimSpace(config.ModelRoutes) {
			next.ModelRoutes = resetModelRoutes(submittedRoutes, next.Model)
		} else {
			next.ModelRoutes = submittedRoutes
		}
		if next.Model != "" {
			next.ModelRoutes = setDefaultModelRoute(next.ModelRoutes, next.Model)
		}
	}
	if patch.MaxAttempts != nil {
		if *patch.MaxAttempts <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_attempts must be greater than 0")
		}
		next.MaxAttempts = *patch.MaxAttempts
	}
	if patch.RetryBackoffMS != nil {
		if *patch.RetryBackoffMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("retry_backoff_ms must be greater than 0")
		}
		next.RetryBackoff = time.Duration(*patch.RetryBackoffMS) * time.Millisecond
	}
	if patch.ChatTimeoutMS != nil {
		if *patch.ChatTimeoutMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("chat_timeout_ms must be greater than 0")
		}
		next.ChatTimeout = time.Duration(*patch.ChatTimeoutMS) * time.Millisecond
	}
	if patch.SkillTimeoutMS != nil {
		if *patch.SkillTimeoutMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("skill_timeout_ms must be greater than 0")
		}
		next.SkillTimeout = time.Duration(*patch.SkillTimeoutMS) * time.Millisecond
	}
	if patch.MaxLoopDurationMS != nil {
		if *patch.MaxLoopDurationMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_loop_duration_ms must be greater than 0")
		}
		next.MaxLoopDuration = time.Duration(*patch.MaxLoopDurationMS) * time.Millisecond
	}
	if patch.MaxLoopActions != nil {
		if *patch.MaxLoopActions <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_loop_actions must be greater than 0")
		}
		next.MaxLoopActions = *patch.MaxLoopActions
	}
	if patch.MaxBranchCount != nil {
		if *patch.MaxBranchCount <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_branch_count must be greater than 0")
		}
		if *patch.MaxBranchCount > deepAgentParallelDefaultMaxBranches {
			return LLMGovernanceConfig{}, fmt.Errorf("max_branch_count must be %d or less", deepAgentParallelDefaultMaxBranches)
		}
		next.MaxBranchCount = *patch.MaxBranchCount
	}
	if patch.MaxBranchConcurrency != nil {
		if *patch.MaxBranchConcurrency <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_branch_concurrency must be greater than 0")
		}
		if *patch.MaxBranchConcurrency > deepAgentParallelDefaultMaxBranches {
			return LLMGovernanceConfig{}, fmt.Errorf("max_branch_concurrency must be %d or less", deepAgentParallelDefaultMaxBranches)
		}
		next.MaxBranchConcurrency = *patch.MaxBranchConcurrency
	}
	if patch.MaxParallelBranches != nil {
		if *patch.MaxParallelBranches <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_parallel_branches must be greater than 0")
		}
		if *patch.MaxParallelBranches > deepAgentParallelDefaultMaxBranches {
			return LLMGovernanceConfig{}, fmt.Errorf("max_parallel_branches must be %d or less", deepAgentParallelDefaultMaxBranches)
		}
		next.MaxParallelBranches = *patch.MaxParallelBranches
	}
	if patch.ParallelBranchTimeoutMS != nil {
		if *patch.ParallelBranchTimeoutMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("parallel_branch_timeout_ms must be greater than 0")
		}
		next.ParallelBranchTimeout = time.Duration(*patch.ParallelBranchTimeoutMS) * time.Millisecond
	}
	if patch.ParallelMaxToolCalls != nil {
		if *patch.ParallelMaxToolCalls <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("parallel_max_tool_calls must be greater than 0")
		}
		next.ParallelMaxToolCalls = *patch.ParallelMaxToolCalls
	}
	if patch.ParallelMaxSources != nil {
		if *patch.ParallelMaxSources <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("parallel_max_sources must be greater than 0")
		}
		next.ParallelMaxSources = *patch.ParallelMaxSources
	}
	if patch.ParallelMaxTokens != nil {
		if *patch.ParallelMaxTokens <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("parallel_max_tokens must be greater than 0")
		}
		next.ParallelMaxTokens = *patch.ParallelMaxTokens
	}
	if patch.EvaluatorTimeoutMS != nil {
		if *patch.EvaluatorTimeoutMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("evaluator_timeout_ms must be greater than 0")
		}
		next.EvaluatorTimeout = time.Duration(*patch.EvaluatorTimeoutMS) * time.Millisecond
	}
	if patch.ConflictTimeoutMS != nil {
		if *patch.ConflictTimeoutMS <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("conflict_reconciliation_timeout_ms must be greater than 0")
		}
		next.ConflictTimeout = time.Duration(*patch.ConflictTimeoutMS) * time.Millisecond
	}
	if patch.MaxSourcesPerBranch != nil {
		if *patch.MaxSourcesPerBranch <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("max_sources_per_branch must be greater than 0")
		}
		next.MaxSourcesPerBranch = *patch.MaxSourcesPerBranch
	}
	if patch.SearchQualityThreshold != nil {
		if *patch.SearchQualityThreshold < 0 || *patch.SearchQualityThreshold > 1 {
			return LLMGovernanceConfig{}, fmt.Errorf("search_quality_threshold must be between 0 and 1")
		}
		next.SearchQualityThreshold = *patch.SearchQualityThreshold
	}
	if patch.AutomaticTriggerEnabled != nil {
		next.AutomaticTriggerEnabled = *patch.AutomaticTriggerEnabled
	}
	if patch.RiskyWriteApprovalMode != nil {
		mode := normalizeRiskyWriteApprovalMode(*patch.RiskyWriteApprovalMode)
		if mode != strings.ToLower(strings.TrimSpace(*patch.RiskyWriteApprovalMode)) {
			return LLMGovernanceConfig{}, fmt.Errorf("risky_write_approval_mode must be allow, review, or block")
		}
		next.RiskyWriteApprovalMode = mode
	}
	if patch.DailyTokenQuota != nil {
		if *patch.DailyTokenQuota < 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("daily_token_quota must be 0 or greater")
		}
		next.DailyTokenQuota = *patch.DailyTokenQuota
	}
	if patch.DailyRequestQuota != nil {
		if *patch.DailyRequestQuota < 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("daily_request_quota must be 0 or greater")
		}
		next.DailyRequestQuota = *patch.DailyRequestQuota
	}
	if patch.APIRateLimitPerMinute != nil {
		if *patch.APIRateLimitPerMinute < 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("api_rate_limit_per_minute must be 0 or greater")
		}
		next.APIRateLimitPerMinute = *patch.APIRateLimitPerMinute
	}
	if patch.DailyCostQuotaUSD != nil {
		if *patch.DailyCostQuotaUSD < 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("daily_cost_quota_usd must be 0 or greater")
		}
		next.DailyCostQuotaUSD = *patch.DailyCostQuotaUSD
	}
	if patch.InputCostPerMillion != nil {
		if *patch.InputCostPerMillion < 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("input_cost_per_million must be 0 or greater")
		}
		next.InputCostPerMillion = *patch.InputCostPerMillion
	}
	if patch.OutputCostPerMillion != nil {
		if *patch.OutputCostPerMillion < 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("output_cost_per_million must be 0 or greater")
		}
		next.OutputCostPerMillion = *patch.OutputCostPerMillion
	}
	if patch.FailureThreshold != nil {
		if *patch.FailureThreshold <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("failure_threshold must be greater than 0")
		}
		next.FailureThreshold = *patch.FailureThreshold
	}
	if patch.CircuitCooldownSeconds != nil {
		if *patch.CircuitCooldownSeconds <= 0 {
			return LLMGovernanceConfig{}, fmt.Errorf("circuit_cooldown_seconds must be greater than 0")
		}
		next.CircuitBreakerCooldown = time.Duration(*patch.CircuitCooldownSeconds) * time.Second
	}
	return next.normalizedWithOptions(allowedModels), nil
}

func mergeLLMGovernanceConfigDefaults(defaults, loaded LLMGovernanceConfig) LLMGovernanceConfig {
	if strings.TrimSpace(loaded.Provider) == "" {
		loaded.Provider = defaults.Provider
	}
	if strings.TrimSpace(loaded.Model) == "" {
		loaded.Model = defaults.Model
	}
	if strings.TrimSpace(loaded.VertexLocation) == "" {
		loaded.VertexLocation = defaults.VertexLocation
	}
	if strings.TrimSpace(loaded.ModelRoutes) == "" {
		loaded.ModelRoutes = defaults.ModelRoutes
	}
	if loaded.APIRateLimitPerMinute < 0 {
		loaded.APIRateLimitPerMinute = defaults.APIRateLimitPerMinute
	}
	return loaded
}

func setDefaultModelRoute(routes, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return strings.TrimSpace(routes)
	}
	items := splitRuntimeConfigCSV(routes)
	out := make([]string, 0, len(items)+1)
	found := false
	for _, item := range items {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if key == "default" {
			out = append(out, "default="+model)
			found = true
			continue
		}
		out = append(out, item)
	}
	if !found {
		out = append([]string{"default=" + model}, out...)
	}
	return strings.Join(out, ",")
}

func setPrimaryModelRoutes(routes, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return strings.TrimSpace(routes)
	}
	items := splitRuntimeConfigCSV(routes)
	out := make([]string, 0, len(items)+1)
	foundDefault := false
	for _, item := range items {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if key == "default" {
			foundDefault = true
			out = append(out, "default="+model)
			continue
		}
		if key == "chat" || strings.HasPrefix(key, "chat:") {
			out = append(out, key+"="+model)
			continue
		}
		out = append(out, item)
	}
	if !foundDefault {
		out = append([]string{"default=" + model}, out...)
	}
	if len(out) == 0 {
		return "default=" + model
	}
	return strings.Join(out, ",")
}

func resetModelRoutes(routes, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return strings.TrimSpace(routes)
	}
	items := splitRuntimeConfigCSV(routes)
	out := make([]string, 0, len(items)+1)
	foundDefault := false
	for _, item := range items {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if key == "default" {
			foundDefault = true
		}
		out = append(out, key+"="+model)
	}
	if !foundDefault {
		out = append([]string{"default=" + model}, out...)
	}
	if len(out) == 0 {
		return "default=" + model
	}
	return strings.Join(out, ",")
}

func modelRoutesCompatibleWithCatalog(routes string, options []LLMModelOption) bool {
	if strings.TrimSpace(routes) == "" {
		return true
	}
	options = normalizeLLMModelOptions(options)
	for _, item := range splitRuntimeConfigCSV(routes) {
		_, model, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, ok := llmModelOptionFor(strings.TrimSpace(model), options); !ok {
			return false
		}
	}
	return true
}

func splitRuntimeConfigCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func normalizeLLMModelOptions(options []LLMModelOption) []LLMModelOption {
	if len(options) == 0 {
		return defaultLLMModelOptions()
	}
	out := make([]LLMModelOption, 0, len(options))
	seen := map[string]struct{}{}
	for _, option := range options {
		option.ID = strings.TrimSpace(option.ID)
		option.Label = strings.TrimSpace(option.Label)
		option.Provider = canonicalLLMModelProvider(option.Provider)
		option.VertexLocation = strings.TrimSpace(option.VertexLocation)
		if option.ID == "" || option.Provider == "" {
			continue
		}
		if option.Label == "" {
			option.Label = option.ID
		}
		if _, ok := seen[option.ID]; ok {
			continue
		}
		seen[option.ID] = struct{}{}
		out = append(out, option)
	}
	if len(out) == 0 {
		return defaultLLMModelOptions()
	}
	return out
}

func mergeDefaultLLMModelOptions(options []LLMModelOption) ([]LLMModelOption, bool) {
	out := normalizeLLMModelOptions(options)
	seen := make(map[string]struct{}, len(out))
	for _, option := range out {
		seen[option.ID] = struct{}{}
	}
	changed := false
	for _, option := range defaultLLMModelOptions() {
		if _, ok := seen[option.ID]; ok {
			continue
		}
		out = append(out, option)
		seen[option.ID] = struct{}{}
		changed = true
	}
	return out, changed
}

func copyLLMModelOptions(options []LLMModelOption) []LLMModelOption {
	out := make([]LLMModelOption, len(options))
	copy(out, options)
	return out
}

type llmGovernanceConfigPayload struct {
	Provider                string  `json:"provider,omitempty"`
	Model                   string  `json:"model,omitempty"`
	VertexLocation          string  `json:"vertex_location,omitempty"`
	ModelRoutes             string  `json:"model_routes,omitempty"`
	MaxAttempts             int     `json:"max_attempts"`
	RetryBackoffMS          int64   `json:"retry_backoff_ms"`
	ChatTimeoutMS           int64   `json:"chat_timeout_ms"`
	SkillTimeoutMS          int64   `json:"skill_timeout_ms"`
	MaxLoopDurationMS       int64   `json:"max_loop_duration_ms"`
	MaxLoopActions          int     `json:"max_loop_actions"`
	MaxBranchCount          int     `json:"max_branch_count"`
	MaxBranchConcurrency    int     `json:"max_branch_concurrency"`
	MaxParallelBranches     int     `json:"max_parallel_branches"`
	ParallelBranchTimeoutMS int64   `json:"parallel_branch_timeout_ms"`
	ParallelMaxToolCalls    int     `json:"parallel_max_tool_calls"`
	ParallelMaxSources      int     `json:"parallel_max_sources"`
	ParallelMaxTokens       int     `json:"parallel_max_tokens"`
	EvaluatorTimeoutMS      int64   `json:"evaluator_timeout_ms"`
	ConflictTimeoutMS       int64   `json:"conflict_reconciliation_timeout_ms"`
	MaxSourcesPerBranch     int     `json:"max_sources_per_branch"`
	SearchQualityThreshold  float64 `json:"search_quality_threshold"`
	AutomaticTriggerEnabled bool    `json:"automatic_trigger_enabled"`
	RiskyWriteApprovalMode  string  `json:"risky_write_approval_mode"`
	DailyTokenQuota         int     `json:"daily_token_quota"`
	DailyRequestQuota       int     `json:"daily_request_quota"`
	APIRateLimitPerMinute   *int    `json:"api_rate_limit_per_minute,omitempty"`
	DailyCostQuotaUSD       float64 `json:"daily_cost_quota_usd"`
	InputCostPerMillion     float64 `json:"input_cost_per_million"`
	OutputCostPerMillion    float64 `json:"output_cost_per_million"`
	FailureThreshold        int     `json:"failure_threshold"`
	CircuitCooldownSeconds  int     `json:"circuit_cooldown_seconds"`
}

type llmModelCatalogPayload struct {
	Models []LLMModelOption `json:"models"`
}

func llmGovernanceConfigToPayload(config LLMGovernanceConfig) llmGovernanceConfigPayload {
	return llmGovernanceConfigPayload{
		Provider:                config.Provider,
		Model:                   config.Model,
		VertexLocation:          config.VertexLocation,
		ModelRoutes:             config.ModelRoutes,
		MaxAttempts:             config.MaxAttempts,
		RetryBackoffMS:          config.RetryBackoff.Milliseconds(),
		ChatTimeoutMS:           config.ChatTimeout.Milliseconds(),
		SkillTimeoutMS:          config.SkillTimeout.Milliseconds(),
		MaxLoopDurationMS:       config.MaxLoopDuration.Milliseconds(),
		MaxLoopActions:          config.MaxLoopActions,
		MaxBranchCount:          config.MaxBranchCount,
		MaxBranchConcurrency:    config.MaxBranchConcurrency,
		MaxParallelBranches:     config.MaxParallelBranches,
		ParallelBranchTimeoutMS: config.ParallelBranchTimeout.Milliseconds(),
		ParallelMaxToolCalls:    config.ParallelMaxToolCalls,
		ParallelMaxSources:      config.ParallelMaxSources,
		ParallelMaxTokens:       config.ParallelMaxTokens,
		EvaluatorTimeoutMS:      config.EvaluatorTimeout.Milliseconds(),
		ConflictTimeoutMS:       config.ConflictTimeout.Milliseconds(),
		MaxSourcesPerBranch:     config.MaxSourcesPerBranch,
		SearchQualityThreshold:  config.SearchQualityThreshold,
		AutomaticTriggerEnabled: config.AutomaticTriggerEnabled,
		RiskyWriteApprovalMode:  config.RiskyWriteApprovalMode,
		DailyTokenQuota:         config.DailyTokenQuota,
		DailyRequestQuota:       config.DailyRequestQuota,
		APIRateLimitPerMinute:   &config.APIRateLimitPerMinute,
		DailyCostQuotaUSD:       config.DailyCostQuotaUSD,
		InputCostPerMillion:     config.InputCostPerMillion,
		OutputCostPerMillion:    config.OutputCostPerMillion,
		FailureThreshold:        config.FailureThreshold,
		CircuitCooldownSeconds:  int(config.CircuitBreakerCooldown.Seconds()),
	}
}

func llmGovernanceConfigFromPayload(payload llmGovernanceConfigPayload) LLMGovernanceConfig {
	return LLMGovernanceConfig{
		Provider:                payload.Provider,
		Model:                   payload.Model,
		VertexLocation:          payload.VertexLocation,
		ModelRoutes:             payload.ModelRoutes,
		MaxAttempts:             payload.MaxAttempts,
		RetryBackoff:            time.Duration(payload.RetryBackoffMS) * time.Millisecond,
		ChatTimeout:             time.Duration(payload.ChatTimeoutMS) * time.Millisecond,
		SkillTimeout:            time.Duration(payload.SkillTimeoutMS) * time.Millisecond,
		MaxLoopDuration:         time.Duration(payload.MaxLoopDurationMS) * time.Millisecond,
		MaxLoopActions:          payload.MaxLoopActions,
		MaxBranchCount:          payload.MaxBranchCount,
		MaxBranchConcurrency:    payload.MaxBranchConcurrency,
		MaxParallelBranches:     payload.MaxParallelBranches,
		ParallelBranchTimeout:   time.Duration(payload.ParallelBranchTimeoutMS) * time.Millisecond,
		ParallelMaxToolCalls:    payload.ParallelMaxToolCalls,
		ParallelMaxSources:      payload.ParallelMaxSources,
		ParallelMaxTokens:       payload.ParallelMaxTokens,
		EvaluatorTimeout:        time.Duration(payload.EvaluatorTimeoutMS) * time.Millisecond,
		ConflictTimeout:         time.Duration(payload.ConflictTimeoutMS) * time.Millisecond,
		MaxSourcesPerBranch:     payload.MaxSourcesPerBranch,
		SearchQualityThreshold:  payload.SearchQualityThreshold,
		AutomaticTriggerEnabled: payload.AutomaticTriggerEnabled,
		RiskyWriteApprovalMode:  payload.RiskyWriteApprovalMode,
		DailyTokenQuota:         payload.DailyTokenQuota,
		DailyRequestQuota:       payload.DailyRequestQuota,
		APIRateLimitPerMinute:   apiRateLimitPerMinuteFromPayload(payload.APIRateLimitPerMinute),
		DailyCostQuotaUSD:       payload.DailyCostQuotaUSD,
		InputCostPerMillion:     payload.InputCostPerMillion,
		OutputCostPerMillion:    payload.OutputCostPerMillion,
		FailureThreshold:        payload.FailureThreshold,
		CircuitBreakerCooldown:  time.Duration(payload.CircuitCooldownSeconds) * time.Second,
	}
}

func apiRateLimitPerMinuteFromPayload(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func llmModelCatalogToPayload(options []LLMModelOption) llmModelCatalogPayload {
	return llmModelCatalogPayload{Models: normalizeLLMModelOptions(options)}
}

func llmModelCatalogFromPayload(payload llmModelCatalogPayload) []LLMModelOption {
	return normalizeLLMModelOptions(payload.Models)
}

func (s *SQLRuntimeConfigStore) loadRuntimeConfigPayload(ctx context.Context, key string) (string, bool, error) {
	if s == nil || s.db == nil {
		return "", false, fmt.Errorf("sql runtime config store is not configured")
	}
	var raw string
	var err error
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		raw, err = s.queries.GetRuntimeConfig(ctx, key)
	} else {
		err = s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT payload FROM agent_runtime_config WHERE config_key = ?`), key).Scan(&raw)
	}
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return raw, true, nil
}

func (s *SQLRuntimeConfigStore) saveRuntimeConfigPayload(ctx context.Context, key, raw string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql runtime config store is not configured")
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.UpsertRuntimeConfig(ctx, dbsqlc.UpsertRuntimeConfigParams{
			ConfigKey: key,
			Payload:   raw,
			UpdatedAt: time.Now().UTC(),
		})
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_runtime_config (config_key, payload, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (config_key) DO UPDATE SET
	payload = excluded.payload,
	updated_at = excluded.updated_at`), key, raw, sqlTimeValue(time.Now().UTC(), s.dialect))
	return err
}

type SQLRuntimeConfigStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

func NewSQLRuntimeConfigStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLRuntimeConfigStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLRuntimeConfigStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLRuntimeConfigStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql runtime config store is not configured")
	}
	return requireSQLColumns(ctx, s.db, "agent_runtime_config",
		"config_key",
		"payload",
		"updated_at",
	)
}

func (s *SQLRuntimeConfigStore) LoadLLMGovernanceConfig(ctx context.Context) (LLMGovernanceConfig, bool, error) {
	raw, ok, err := s.loadRuntimeConfigPayload(ctx, llmGovernanceConfigKey)
	if err != nil {
		return LLMGovernanceConfig{}, false, err
	}
	if !ok {
		return LLMGovernanceConfig{}, false, nil
	}
	var payload llmGovernanceConfigPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return LLMGovernanceConfig{}, false, err
	}
	return llmGovernanceConfigFromPayload(payload), true, nil
}

func (s *SQLRuntimeConfigStore) SaveLLMGovernanceConfig(ctx context.Context, config LLMGovernanceConfig) error {
	raw, err := json.Marshal(llmGovernanceConfigToPayload(config))
	if err != nil {
		return err
	}
	return s.saveRuntimeConfigPayload(ctx, llmGovernanceConfigKey, string(raw))
}

func (s *SQLRuntimeConfigStore) LoadLLMModelCatalog(ctx context.Context) ([]LLMModelOption, bool, error) {
	raw, ok, err := s.loadRuntimeConfigPayload(ctx, llmModelCatalogKey)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	var payload llmModelCatalogPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, false, err
	}
	return llmModelCatalogFromPayload(payload), true, nil
}

func (s *SQLRuntimeConfigStore) SaveLLMModelCatalog(ctx context.Context, options []LLMModelOption) error {
	raw, err := json.Marshal(llmModelCatalogToPayload(options))
	if err != nil {
		return err
	}
	return s.saveRuntimeConfigPayload(ctx, llmModelCatalogKey, string(raw))
}

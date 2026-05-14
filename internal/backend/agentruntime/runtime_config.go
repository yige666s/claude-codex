package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const llmGovernanceConfigKey = "llm_governance"

type LLMGovernanceConfigPatch struct {
	MaxAttempts            *int     `json:"max_attempts,omitempty"`
	RetryBackoffMS         *int64   `json:"retry_backoff_ms,omitempty"`
	ChatTimeoutMS          *int64   `json:"chat_timeout_ms,omitempty"`
	SkillTimeoutMS         *int64   `json:"skill_timeout_ms,omitempty"`
	DailyTokenQuota        *int     `json:"daily_token_quota,omitempty"`
	DailyRequestQuota      *int     `json:"daily_request_quota,omitempty"`
	DailyCostQuotaUSD      *float64 `json:"daily_cost_quota_usd,omitempty"`
	InputCostPerMillion    *float64 `json:"input_cost_per_million,omitempty"`
	OutputCostPerMillion   *float64 `json:"output_cost_per_million,omitempty"`
	FailureThreshold       *int     `json:"failure_threshold,omitempty"`
	CircuitCooldownSeconds *int     `json:"circuit_cooldown_seconds,omitempty"`
}

type LLMGovernanceConfigStore interface {
	LoadLLMGovernanceConfig(ctx context.Context) (LLMGovernanceConfig, bool, error)
	SaveLLMGovernanceConfig(ctx context.Context, config LLMGovernanceConfig) error
}

type LLMGovernanceConfigManager struct {
	mu     sync.RWMutex
	config LLMGovernanceConfig
	store  LLMGovernanceConfigStore
}

func NewLLMGovernanceConfigManager(config LLMGovernanceConfig, store LLMGovernanceConfigStore) *LLMGovernanceConfigManager {
	return &LLMGovernanceConfigManager{config: config.normalized(), store: store}
}

func (m *LLMGovernanceConfigManager) Load(ctx context.Context) error {
	if m == nil || m.store == nil {
		return nil
	}
	config, ok, err := m.store.LoadLLMGovernanceConfig(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	m.mu.Lock()
	m.config = config.normalized()
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
	return llmGovernanceConfigStatusMap(m.Get())
}

func (m *LLMGovernanceConfigManager) Update(ctx context.Context, patch LLMGovernanceConfigPatch) (LLMGovernanceConfig, error) {
	if m == nil {
		return LLMGovernanceConfig{}, fmt.Errorf("llm governance config is not configured")
	}
	m.mu.RLock()
	current := m.config
	m.mu.RUnlock()
	next, err := applyLLMGovernanceConfigPatch(current, patch)
	if err != nil {
		return LLMGovernanceConfig{}, err
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
	config = config.normalized()
	return map[string]any{
		"max_attempts":             config.MaxAttempts,
		"retry_backoff_ms":         config.RetryBackoff.Milliseconds(),
		"chat_timeout_ms":          config.ChatTimeout.Milliseconds(),
		"skill_timeout_ms":         config.SkillTimeout.Milliseconds(),
		"daily_token_quota":        config.DailyTokenQuota,
		"daily_request_quota":      config.DailyRequestQuota,
		"daily_cost_quota_usd":     config.DailyCostQuotaUSD,
		"input_cost_per_million":   config.InputCostPerMillion,
		"output_cost_per_million":  config.OutputCostPerMillion,
		"failure_threshold":        config.FailureThreshold,
		"circuit_cooldown_seconds": int(config.CircuitBreakerCooldown.Seconds()),
	}
}

func applyLLMGovernanceConfigPatch(config LLMGovernanceConfig, patch LLMGovernanceConfigPatch) (LLMGovernanceConfig, error) {
	next := config
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
	return next.normalized(), nil
}

type llmGovernanceConfigPayload struct {
	MaxAttempts            int     `json:"max_attempts"`
	RetryBackoffMS         int64   `json:"retry_backoff_ms"`
	ChatTimeoutMS          int64   `json:"chat_timeout_ms"`
	SkillTimeoutMS         int64   `json:"skill_timeout_ms"`
	DailyTokenQuota        int     `json:"daily_token_quota"`
	DailyRequestQuota      int     `json:"daily_request_quota"`
	DailyCostQuotaUSD      float64 `json:"daily_cost_quota_usd"`
	InputCostPerMillion    float64 `json:"input_cost_per_million"`
	OutputCostPerMillion   float64 `json:"output_cost_per_million"`
	FailureThreshold       int     `json:"failure_threshold"`
	CircuitCooldownSeconds int     `json:"circuit_cooldown_seconds"`
}

func llmGovernanceConfigToPayload(config LLMGovernanceConfig) llmGovernanceConfigPayload {
	config = config.normalized()
	return llmGovernanceConfigPayload{
		MaxAttempts:            config.MaxAttempts,
		RetryBackoffMS:         config.RetryBackoff.Milliseconds(),
		ChatTimeoutMS:          config.ChatTimeout.Milliseconds(),
		SkillTimeoutMS:         config.SkillTimeout.Milliseconds(),
		DailyTokenQuota:        config.DailyTokenQuota,
		DailyRequestQuota:      config.DailyRequestQuota,
		DailyCostQuotaUSD:      config.DailyCostQuotaUSD,
		InputCostPerMillion:    config.InputCostPerMillion,
		OutputCostPerMillion:   config.OutputCostPerMillion,
		FailureThreshold:       config.FailureThreshold,
		CircuitCooldownSeconds: int(config.CircuitBreakerCooldown.Seconds()),
	}
}

func llmGovernanceConfigFromPayload(payload llmGovernanceConfigPayload) LLMGovernanceConfig {
	return LLMGovernanceConfig{
		MaxAttempts:            payload.MaxAttempts,
		RetryBackoff:           time.Duration(payload.RetryBackoffMS) * time.Millisecond,
		ChatTimeout:            time.Duration(payload.ChatTimeoutMS) * time.Millisecond,
		SkillTimeout:           time.Duration(payload.SkillTimeoutMS) * time.Millisecond,
		DailyTokenQuota:        payload.DailyTokenQuota,
		DailyRequestQuota:      payload.DailyRequestQuota,
		DailyCostQuotaUSD:      payload.DailyCostQuotaUSD,
		InputCostPerMillion:    payload.InputCostPerMillion,
		OutputCostPerMillion:   payload.OutputCostPerMillion,
		FailureThreshold:       payload.FailureThreshold,
		CircuitBreakerCooldown: time.Duration(payload.CircuitCooldownSeconds) * time.Second,
	}.normalized()
}

type SQLRuntimeConfigStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLRuntimeConfigStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLRuntimeConfigStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLRuntimeConfigStore{db: db, dialect: dialect}
}

func (s *SQLRuntimeConfigStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql runtime config store is not configured")
	}
	timeType := s.dialect.TimeType()
	if err := RunSQLMigrations(ctx, s.db, s.dialect, []SQLMigration{{
		Version: 6,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS agent_runtime_config (
	config_key TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	updated_at ` + timeType + ` NOT NULL
)`,
		},
	}}); err != nil {
		return err
	}
	return ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_runtime_config", "updated_at")
}

func (s *SQLRuntimeConfigStore) LoadLLMGovernanceConfig(ctx context.Context) (LLMGovernanceConfig, bool, error) {
	if s == nil || s.db == nil {
		return LLMGovernanceConfig{}, false, fmt.Errorf("sql runtime config store is not configured")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT payload FROM agent_runtime_config WHERE config_key = ?`), llmGovernanceConfigKey).Scan(&raw)
	if err == sql.ErrNoRows {
		return LLMGovernanceConfig{}, false, nil
	}
	if err != nil {
		return LLMGovernanceConfig{}, false, err
	}
	var payload llmGovernanceConfigPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return LLMGovernanceConfig{}, false, err
	}
	return llmGovernanceConfigFromPayload(payload), true, nil
}

func (s *SQLRuntimeConfigStore) SaveLLMGovernanceConfig(ctx context.Context, config LLMGovernanceConfig) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql runtime config store is not configured")
	}
	raw, err := json.Marshal(llmGovernanceConfigToPayload(config))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_runtime_config (config_key, payload, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (config_key) DO UPDATE SET
	payload = excluded.payload,
	updated_at = excluded.updated_at`), llmGovernanceConfigKey, string(raw), sqlTimeValue(time.Now().UTC(), s.dialect))
	return err
}

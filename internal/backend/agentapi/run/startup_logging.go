package run

import (
	"strings"

	"claude-codex/internal/backend/agentapi/bootstrap"
	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
)

type startupStatus struct {
	JobWorkerStarted                 bool
	JobEventFanoutStarted            bool
	MessageAttachmentWorkerStarted   bool
	MessageArchiveWorkerStarted      bool
	MessageSearchIndexManagerStarted bool
}

func logStartup(cfg startupconfig.Config, llmCfg bootstrap.LLMConfig, llmConfigManager *agentruntime.LLMGovernanceConfigManager, authService *agentruntime.AuthService, status startupStatus) {
	logInfof("agent API listening on %s", cfg.Addr)
	logInfof("data dir: %s", cfg.DataDir)
	logInfof("store backend: %s", cfg.StoreBackend)
	logInfof("workspace: %s", cfg.Workspace)
	if strings.TrimSpace(cfg.UserWorkspaceRoot) != "" {
		logInfof("user workspace root: %s", cfg.UserWorkspaceRoot)
	}
	logInfof("llm provider: %s model: %s", llmCfg.Provider, llmCfg.Model)
	if strings.TrimSpace(cfg.LLMFallbacks) != "" {
		logInfof("llm fallbacks: %s", cfg.LLMFallbacks)
	}
	if strings.TrimSpace(cfg.LLMModelRoutes) != "" {
		logInfof("llm model routes: %s", cfg.LLMModelRoutes)
	}
	effectiveLLMConfig := llmConfigManager.Get()
	logInfof("llm governance: attempts=%d chat_timeout=%s skill_timeout=%s daily_token_quota=%d daily_request_quota=%d daily_cost_quota_usd=%.4f", effectiveLLMConfig.MaxAttempts, effectiveLLMConfig.ChatTimeout, effectiveLLMConfig.SkillTimeout, effectiveLLMConfig.DailyTokenQuota, effectiveLLMConfig.DailyRequestQuota, effectiveLLMConfig.DailyCostQuotaUSD)
	logInfof("auth mode: %s", cfg.AuthMode)
	logInfof("admin API enabled: %t", strings.TrimSpace(cfg.AdminToken) != "")
	logInfof("user system enabled: %t", authService != nil)
	logInfof("rate limit backend: %s", cfg.RateLimitBackend)
	logInfof("cache backend: %s ttl=%s fail_open=%t prefix=%s", cfg.CacheBackend, cfg.CacheDefaultTTL, cfg.CacheFailOpen, cfg.CachePrefix)
	logInfof("message context cache backend: %s ttl=%s", cfg.MessageContextCacheBackend, cfg.MessageContextCacheTTL)
	if strings.TrimSpace(cfg.LiveVoiceName) != "" || strings.TrimSpace(cfg.LiveLanguageCode) != "" {
		logInfof("live voice: voice=%s language=%s", cfg.LiveVoiceName, cfg.LiveLanguageCode)
	}
	logInfof("live setup prompt cache backend: %s ttl=%s", cfg.LiveSetupPromptCacheBackend, cfg.LiveSetupPromptCacheTTL)
	logInfof("session list cache backend: %s ttl=%s", cfg.SessionListCacheBackend, cfg.SessionListCacheTTL)
	logInfof("message events backend: %s kafka_consumer=%t topic=%s", cfg.MessageEventsBackend, cfg.MessageEventsKafkaConsumerEnabled, cfg.MessageEventsKafkaTopic)
	logInfof("job queue: redis stream=%s group=%s worker_enabled=%t worker_started=%t claim_idle=%s lock_ttl=%s", cfg.JobQueueStream, cfg.JobQueueConsumerGroup, cfg.JobWorkerEnabled, status.JobWorkerStarted, cfg.JobQueueClaimIdle, cfg.JobQueueLockTTL)
	logInfof("job event stream: enabled=%t prefix=%s ttl=%s max_len=%d", cfg.JobEventStreamEnabled, cfg.JobEventStreamPrefix, cfg.JobEventStreamTTL, cfg.JobEventStreamMaxLen)
	logInfof("chat event stream: enabled=%t prefix=%s ttl=%s max_len=%d block=%s", cfg.ChatEventStreamEnabled, cfg.ChatEventStreamPrefix, cfg.ChatEventStreamTTL, cfg.ChatEventStreamMaxLen, cfg.ChatEventStreamBlock)
	logInfof("job event fanout: enabled=%t started=%t channel=%s", cfg.JobEventFanoutEnabled, status.JobEventFanoutStarted, cfg.JobEventFanoutChannel)
	logInfof("message attachment worker: enabled=%t started=%t batch=%d interval=%s", cfg.MessageAttachmentWorkerEnabled, status.MessageAttachmentWorkerStarted, cfg.MessageAttachmentWorkerBatchSize, cfg.MessageAttachmentWorkerPollInterval)
	logInfof("message archive worker: enabled=%t started=%t after=%s batch=%d interval=%s prefix=%s clear_pg_payload=%t", cfg.MessageArchiveWorkerEnabled, status.MessageArchiveWorkerStarted, cfg.MessageArchiveAfter, cfg.MessageArchiveWorkerBatchSize, cfg.MessageArchiveWorkerPollInterval, cfg.MessageArchivePrefix, cfg.MessageArchiveClearPGPayload)
	logInfof("message search index manager: enabled=%t started=%t analyzer=%s search_analyzer=%s downgrade_after=%s close_after=%s interval=%s", cfg.MessageSearchIndexManagementEnabled, status.MessageSearchIndexManagerStarted, cfg.MessageSearchIndexAnalyzer, cfg.MessageSearchIndexSearchAnalyzer, cfg.MessageSearchIndexDowngradeAfter, cfg.MessageSearchIndexCloseAfter, cfg.MessageSearchIndexMaintenanceInterval)
	logInfof("operation rate limits enabled: %t", strings.TrimSpace(cfg.OperationRateLimits) != "")
	logInfof("artifact store: %s", cfg.ArtifactStore)
	logInfof("asset max bytes: %d", cfg.AssetMaxBytes)
	if cfg.RetentionDays > 0 {
		logInfof("retention days: %d", cfg.RetentionDays)
	}
	if cfg.LocalArtifactStagingRetention > 0 {
		logInfof("local artifact staging retention: %s", cfg.LocalArtifactStagingRetention)
	}
	logInfof("skill publication: code-defined registry sync; database status controls enablement")
	logInfof("dangerous tools enabled: %t", cfg.AllowDangerousTools)
	logInfof("skill shell timeout: %s", cfg.SkillShellTimeout)
	logInfof("skill shell runner: %s image=%s network=%s memory=%s cpus=%s pids=%d", cfg.SkillShellRunner, cfg.SkillSandboxImage, cfg.SkillSandboxNetwork, cfg.SkillSandboxMemory, cfg.SkillSandboxCPUs, cfg.SkillSandboxPidsLimit)
	if strings.TrimSpace(cfg.NetworkAllowlist) == "" {
		logInfof("network allowlist: disabled (all domains allowed)")
	} else {
		logInfof("network allowlist: %s", cfg.NetworkAllowlist)
	}
	logInfof("cors allowed origins: %s", cfg.CORSAllowedOrigins)
	logInfof("csrf enabled: %t", cfg.CSRFEnabled)
	logInfof("daily evaluation: enabled=%t schedule=UTC+8 %02d:%02d batch_limit=%d explicit_users=%d", cfg.EvalDailyEnabled, cfg.EvalDailyHour, cfg.EvalDailyMinute, cfg.EvalDailyBatchLimit, len(startupconfig.SplitCSV(cfg.EvalDailyUserIDs)))
}

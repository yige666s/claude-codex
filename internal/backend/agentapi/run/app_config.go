package run

import (
	"time"

	"claude-codex/internal/backend/agentapi/bootstrap"
	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
)

func storeConfigFromStartup(cfg startupconfig.Config) storeConfig {
	return storeConfig{
		backend:            cfg.StoreBackend,
		dataDir:            cfg.DataDir,
		objectBaseURL:      cfg.ObjectBaseURL,
		objectToken:        cfg.ObjectToken,
		objectTimeout:      cfg.ObjectTimeout,
		sqlDriver:          cfg.SQLDriver,
		sqlDSN:             cfg.SQLDSN,
		sqlDialect:         cfg.SQLDialect,
		sqlMaxOpen:         cfg.SQLMaxOpen,
		sqlMaxIdle:         cfg.SQLMaxIdle,
		sqlConnMaxLifetime: cfg.SQLConnMaxLifetime,
	}
}

func buildStartupLLMConfig(cfg startupconfig.Config) bootstrap.LLMConfig {
	llmCfg, err := bootstrap.BuildLLMConfig(cfg.LLMProvider, cfg.Model, cfg.APIKey, cfg.APIToken, cfg.APIBaseURL, 600)
	if err != nil {
		logFatal(err)
	}
	llmCfg.Model = bootstrap.RoutedModel(llmCfg.Model, cfg.LLMModelRoutes, agentruntime.Scope{})
	if option, ok := agentruntime.LLMModelOptionFor(llmCfg.Model); ok {
		llmCfg.Provider = option.Provider
		llmCfg.Model = option.ID
		llmCfg.VertexLocation = option.VertexLocation
	}
	return llmCfg
}

func llmGovernanceConfigFromStartup(cfg startupconfig.Config, llmCfg bootstrap.LLMConfig) agentruntime.LLMGovernanceConfig {
	return agentruntime.LLMGovernanceConfig{
		Provider:               llmCfg.Provider,
		Model:                  llmCfg.Model,
		VertexLocation:         llmCfg.VertexLocation,
		ModelRoutes:            agentruntime.LLMModelRoutesWithDefault(cfg.LLMModelRoutes, llmCfg.Model),
		MaxAttempts:            cfg.LLMMaxAttempts,
		RetryBackoff:           cfg.LLMRetryBackoff,
		ChatTimeout:            cfg.LLMChatTimeout,
		SkillTimeout:           cfg.LLMSkillTimeout,
		DailyTokenQuota:        cfg.LLMDailyTokenQuota,
		DailyRequestQuota:      cfg.LLMDailyRequestQuota,
		DailyCostQuotaUSD:      cfg.LLMDailyCostQuotaUSD,
		InputCostPerMillion:    cfg.LLMInputCostPerMillion,
		OutputCostPerMillion:   cfg.LLMOutputCostPerMillion,
		FailureThreshold:       cfg.LLMFailureThreshold,
		CircuitBreakerCooldown: cfg.LLMCircuitCooldown,
	}
}

func skillShellSandboxConfigFromStartup(cfg startupconfig.Config) agentruntime.SkillShellSandboxConfig {
	return agentruntime.SkillShellSandboxConfig{
		Runner:       cfg.SkillShellRunner,
		Image:        cfg.SkillSandboxImage,
		Network:      cfg.SkillSandboxNetwork,
		Memory:       cfg.SkillSandboxMemory,
		CPUs:         cfg.SkillSandboxCPUs,
		PidsLimit:    cfg.SkillSandboxPidsLimit,
		TmpfsSize:    cfg.SkillSandboxTmpfsSize,
		WarmPoolSize: cfg.SkillSandboxWarmPoolSize,
	}
}

func runtimeConfigFromStartup(cfg startupconfig.Config, skillShellSandboxConfig agentruntime.SkillShellSandboxConfig) agentruntime.RuntimeConfig {
	return agentruntime.RuntimeConfig{
		DefaultWorkingDir:     cfg.Workspace,
		UserWorkspaceRoot:     cfg.UserWorkspaceRoot,
		AllowCustomWorkingDir: cfg.AllowCustomWorkingDir,
		TurnTimeout:           cfg.TurnTimeout,
		SkillShellTimeout:     cfg.SkillShellTimeout,
		MessageSearch:         messageSearchConfigFromStartup(cfg),
		MemoryVector:          memoryVectorConfigFromStartup(cfg),
		Live:                  liveConfigFromStartup(cfg),
		SkillShellSandbox:     skillShellSandboxConfig,
	}
}

func messageSearchConfigFromStartup(cfg startupconfig.Config) agentruntime.MessageSearchConfig {
	return agentruntime.MessageSearchConfig{
		Backend:                    cfg.MessageSearchBackend,
		Endpoint:                   cfg.MessageSearchEndpoint,
		Index:                      cfg.MessageSearchIndex,
		APIKey:                     cfg.MessageSearchAPIKey,
		Username:                   cfg.MessageSearchUsername,
		Password:                   cfg.MessageSearchPassword,
		Timeout:                    cfg.MessageSearchTimeout,
		IndexManagementEnabled:     cfg.MessageSearchIndexManagementEnabled,
		IndexLifecyclePolicy:       cfg.MessageSearchIndexLifecyclePolicy,
		IndexTemplateName:          cfg.MessageSearchIndexTemplate,
		IndexWriteAlias:            cfg.MessageSearchIndexWriteAlias,
		IndexAnalyzer:              cfg.MessageSearchIndexAnalyzer,
		IndexSearchAnalyzer:        cfg.MessageSearchIndexSearchAnalyzer,
		IndexDowngradeAfter:        cfg.MessageSearchIndexDowngradeAfter,
		IndexCloseAfter:            cfg.MessageSearchIndexCloseAfter,
		IndexMaintenanceInterval:   cfg.MessageSearchIndexMaintenanceInterval,
		IndexMaintenanceBatchLimit: cfg.MessageSearchIndexMaintenanceBatchLimit,
		QdrantEndpoint:             cfg.MessageSearchQdrantEndpoint,
		QdrantCollection:           cfg.MessageSearchQdrantCollection,
		QdrantAPIKey:               cfg.MessageSearchQdrantAPIKey,
		QdrantScoreThreshold:       cfg.MessageSearchQdrantScoreThreshold,
		EmbeddingProvider:          cfg.MessageSearchEmbeddingProvider,
		EmbeddingEndpoint:          cfg.MessageSearchEmbeddingEndpoint,
		EmbeddingAPIKey:            cfg.MessageSearchEmbeddingAPIKey,
		EmbeddingAccessToken:       cfg.MessageSearchEmbeddingAccessToken,
		EmbeddingModel:             cfg.MessageSearchEmbeddingModel,
		EmbeddingDimensions:        cfg.MessageSearchEmbeddingDimensions,
		EmbeddingTimeout:           cfg.MessageSearchTimeout,
		EmbeddingProjectID:         cfg.MessageSearchEmbeddingProjectID,
		EmbeddingLocation:          cfg.MessageSearchEmbeddingLocation,
		EmbeddingTaskType:          cfg.MessageSearchEmbeddingTaskType,
		EmbeddingIndexTaskType:     cfg.MessageSearchEmbeddingIndexTaskType,
		EmbeddingAutoTruncate:      cfg.MessageSearchEmbeddingAutoTruncate,
		RRFK:                       cfg.MessageSearchRRFK,
	}
}

func memoryVectorConfigFromStartup(cfg startupconfig.Config) agentruntime.MemoryVectorConfig {
	return agentruntime.MemoryVectorConfig{
		Enabled:                cfg.MemoryVectorEnabled,
		QdrantEndpoint:         cfg.MemoryVectorQdrantEndpoint,
		QdrantCollection:       cfg.MemoryVectorQdrantCollection,
		QdrantAPIKey:           cfg.MemoryVectorQdrantAPIKey,
		QdrantScoreThreshold:   cfg.MemoryVectorQdrantScoreThreshold,
		EmbeddingProvider:      cfg.MemoryVectorEmbeddingProvider,
		EmbeddingEndpoint:      cfg.MemoryVectorEmbeddingEndpoint,
		EmbeddingAPIKey:        cfg.MemoryVectorEmbeddingAPIKey,
		EmbeddingAccessToken:   cfg.MemoryVectorEmbeddingAccessToken,
		EmbeddingModel:         cfg.MemoryVectorEmbeddingModel,
		EmbeddingDimensions:    cfg.MemoryVectorEmbeddingDimensions,
		EmbeddingTimeout:       cfg.MessageSearchTimeout,
		EmbeddingProjectID:     cfg.MemoryVectorEmbeddingProjectID,
		EmbeddingLocation:      cfg.MemoryVectorEmbeddingLocation,
		EmbeddingTaskType:      cfg.MemoryVectorEmbeddingTaskType,
		EmbeddingIndexTaskType: cfg.MemoryVectorEmbeddingIndexTaskType,
		EmbeddingAutoTruncate:  cfg.MemoryVectorEmbeddingAutoTruncate,
		Timeout:                cfg.MessageSearchTimeout,
		RRFK:                   cfg.MemoryVectorRRFK,
	}
}

func liveConfigFromStartup(cfg startupconfig.Config) agentruntime.LiveConfig {
	return agentruntime.LiveConfig{
		Enabled:                    cfg.LiveEnabled,
		Provider:                   cfg.LiveProvider,
		Model:                      cfg.LiveModel,
		VertexProjectID:            cfg.LiveVertexProjectID,
		VertexLocation:             cfg.LiveVertexLocation,
		VertexBaseURL:              cfg.LiveVertexBaseURL,
		VertexAPIVersion:           cfg.LiveVertexAPIVersion,
		InputAudioMIMEType:         cfg.LiveInputAudioMIME,
		OutputAudioMIMEType:        cfg.LiveOutputAudioMIME,
		InputTranscriptionEnabled:  cfg.LiveInputTranscription,
		OutputTranscriptionEnabled: cfg.LiveOutputTranscription,
		LiveVADStartSensitivity:    cfg.LiveVADStartSensitivity,
		LiveVADEndSensitivity:      cfg.LiveVADEndSensitivity,
		LiveVADPrefixPadding:       cfg.LiveVADPrefixPadding,
		LiveVADSilenceDuration:     cfg.LiveVADSilenceDuration,
		SessionTimeout:             cfg.LiveSessionTimeout,
	}
}

func authConfigFromStartup(cfg startupconfig.Config) authConfig {
	return authConfig{
		mode:                cfg.AuthMode,
		userHeader:          cfg.UserHeader,
		authToken:           cfg.AuthToken,
		jwtSecret:           cfg.JWTSecret,
		jwtIssuer:           cfg.JWTIssuer,
		jwtAudience:         cfg.JWTAudience,
		jwtUserClaim:        cfg.JWTUserClaim,
		sessionCookieName:   cfg.SessionCookieName,
		sessionCookieSecret: cfg.SessionCookieSecret,
		trustedUserHeader:   cfg.TrustedUserHeader,
		trustedSecretHeader: cfg.TrustedSecretHeader,
		trustedSecret:       cfg.TrustedSecret,
	}
}

func authServiceConfigFromStartup(cfg startupconfig.Config) authServiceConfig {
	return authServiceConfig{
		jwtSecret:                 cfg.JWTSecret,
		jwtIssuer:                 cfg.JWTIssuer,
		jwtAudience:               cfg.JWTAudience,
		accessTTL:                 cfg.AuthAccessTTL,
		refreshTTL:                cfg.AuthRefreshTTL,
		emailVerificationRequired: cfg.EmailVerificationRequired,
		emailVerificationTTL:      cfg.EmailVerificationTTL,
		emailProvider:             cfg.EmailProvider,
		emailFrom:                 cfg.EmailFrom,
		emailPublicBaseURL:        cfg.EmailPublicBaseURL,
		resendAPIKey:              cfg.ResendAPIKey,
		resendBaseURL:             cfg.ResendBaseURL,
	}
}

func artifactConfigFromStartup(cfg startupconfig.Config, storeCfg storeConfig) artifactConfig {
	return artifactConfig{
		store:       cfg.ArtifactStore,
		dataDir:     cfg.DataDir,
		sql:         storeCfg,
		s3Endpoint:  cfg.ArtifactS3Endpoint,
		s3AccessKey: cfg.ArtifactS3AccessKey,
		s3SecretKey: cfg.ArtifactS3SecretKey,
		s3Bucket:    cfg.ArtifactS3Bucket,
		s3Prefix:    cfg.ArtifactS3Prefix,
		s3SSL:       cfg.ArtifactS3SSL,
		maxBytes:    cfg.AssetMaxBytes,
	}
}

func kafkaMessageEventConfigFromStartup(cfg startupconfig.Config) agentruntime.KafkaMessageEventConfig {
	return agentruntime.KafkaMessageEventConfig{
		Brokers:        startupconfig.SplitCSV(cfg.MessageEventsKafkaBrokers),
		Topic:          cfg.MessageEventsKafkaTopic,
		ClientID:       cfg.MessageEventsKafkaClientID,
		GroupID:        cfg.MessageEventsKafkaConsumerGroup,
		DLQTopic:       cfg.MessageEventsKafkaDLQTopic,
		RetryAttempts:  cfg.MessageEventsKafkaRetryAttempts,
		RetryBackoff:   cfg.MessageEventsKafkaRetryBackoff,
		ProcessTimeout: cfg.MessageEventsKafkaProcessTimeout,
	}
}

func dailyEvaluationConfigFromStartup(cfg startupconfig.Config) agentruntime.DailyEvaluationConfig {
	return agentruntime.DailyEvaluationConfig{
		Enabled:     cfg.EvalDailyEnabled,
		Location:    time.FixedZone("UTC+8", 8*60*60),
		Hour:        cfg.EvalDailyHour,
		Minute:      cfg.EvalDailyMinute,
		SubjectType: agentruntime.EvaluationSubjectJob,
		UserIDs:     startupconfig.SplitCSV(cfg.EvalDailyUserIDs),
		BatchLimit:  cfg.EvalDailyBatchLimit,
		Timeout:     cfg.EvalDailyTimeout,
		Thresholds: agentruntime.EvaluationThresholds{
			MinSuccessRate:   0.85,
			MaxToolErrorRate: 0.05,
			MaxLLMErrorRate:  0.05,
			MaxHighRiskCount: 0,
			MaxP95LatencyMS:  10000,
		},
	}
}

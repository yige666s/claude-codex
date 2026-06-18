package run

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"claude-codex/internal/backend/agentapi/bootstrap"
	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	workerlifecycle "claude-codex/internal/backend/workers"
)

const (
	workerKafkaMessageConsumer  = "kafka_message_event_consumer"
	workerMessageAttachment     = "message_attachment_worker"
	workerMessageArchive        = "message_archive_worker"
	workerMessageSearchIndex    = "message_search_index_manager"
	workerJobEventFanout        = "job_event_fanout"
	workerJobQueue              = "job_worker"
	workerRetentionPrune        = "retention_prune"
	workerLocalArtifactPrune    = "local_artifact_prune"
	workerSkillSandboxImageWarm = "skill_sandbox_image_warm"
	workerSkillSandboxWarmPool  = "skill_sandbox_warm_pool"
)

func Run(_ context.Context, cfg startupconfig.Config) {
	appLogger := slog.Default()
	workerGroup := workerlifecycle.New(context.Background(), appLogger)
	llmCfg := buildStartupLLMConfig(cfg)
	storeCfg := storeConfigFromStartup(cfg)
	llmUsageStore := buildLLMUsageStore(storeCfg)
	riskStore := buildRiskStore(storeCfg)
	jobStore := buildJobStore(storeCfg)
	skillExecutionStore := buildSkillExecutionStore(storeCfg)
	toolCallLedgerStore := buildToolCallLedgerStore(storeCfg)
	evaluationStore := buildEvaluationStore(storeCfg)
	promptStore := buildPromptStore(storeCfg)
	cacheStore, cacheRedisClient := bootstrap.BuildCacheStore(cfg.CacheBackend, cfg.CacheRedisURL, cfg.CachePrefix, cfg.CacheDefaultTTL)
	if cacheRedisClient != nil {
		defer func() {
			if err := cacheRedisClient.Close(); err != nil {
				logInfof("close cache redis client: %v", err)
			}
		}()
	}
	cacheMetrics := agentruntime.NewCacheMetrics()
	promptStore = agentruntime.NewCacheInvalidatingPromptStore(promptStore, cacheStore)
	llmGovernanceCfg := llmGovernanceConfigFromStartup(cfg, llmCfg)
	llmConfigManager := agentruntime.NewLLMGovernanceConfigManager(llmGovernanceCfg, buildRuntimeConfigStore(storeCfg))
	if err := llmConfigManager.Load(context.Background()); err != nil {
		logFatalf("load llm governance config: %v", err)
	}

	skillManager := loadSkills(startupconfig.SplitCSV(cfg.SkillDirs))
	skillRegistrySetup := buildSkillRegistrySetup(storeCfg, skillManager)
	skillCatalog := skillRegistrySetup.catalog
	skillShellSandboxConfig := skillShellSandboxConfigFromStartup(cfg)
	if skillShellSandboxConfig.DockerEnabled() {
		images := append([]string{skillShellSandboxConfig.Image}, startupconfig.SplitCSV(cfg.SkillSandboxPrepullImages)...)
		workerGroup.Start(workerSkillSandboxImageWarm, func(ctx context.Context) error {
			warmSkillSandboxImages(ctx, images)
			return nil
		})
		workerGroup.Start(workerSkillSandboxWarmPool, func(ctx context.Context) error {
			startSkillSandboxWarmPool(ctx, skillShellSandboxConfig, startupconfig.SplitCSV(cfg.SkillSandboxPrepullImages), cfg.SkillSandboxWarmPoolSize)
			return nil
		})
	}
	engineFactory, llmStatusFn := buildEngineFactory(engineFactoryConfig{
		startupCfg:              cfg,
		llmCfg:                  llmCfg,
		skillCatalog:            skillCatalog,
		skillShellSandboxConfig: skillShellSandboxConfig,
		llmConfigManager:        llmConfigManager,
		llmUsageStore:           llmUsageStore,
		riskStore:               riskStore,
		toolCallLedger:          toolCallLedgerStore,
	})

	sessionStore, memoryService := buildStores(storeCfg)
	auth := buildAuthenticator(authConfigFromStartup(cfg))
	limiter := bootstrap.BuildRateLimiter(cfg.RateLimitBackend, cfg.RedisURL, cfg.RateLimit, time.Minute, cfg.RedisFailOpen)
	authService := buildAuthService(cfg.EnableUserSystem, storeCfg, authServiceConfigFromStartup(cfg))
	artifactService := buildArtifactService(artifactConfigFromStartup(cfg, storeCfg))
	assetInsightStore := buildAssetInsightStore(storeCfg)
	workflowStore := buildWorkflowStore(storeCfg)
	deepAgentEvidenceRepo := buildDeepAgentEvidenceRepository(storeCfg)
	loopGoalStore := buildLoopGoalStore(storeCfg)
	loopTriggerStore := buildLoopTriggerStore(storeCfg)
	runtimeConfig := runtimeConfigFromStartup(cfg, skillShellSandboxConfig)
	runtimeConfig.Logger = appLogger
	runtimeConfig.CacheStore = cacheStore
	runtimeConfig.CacheMetrics = cacheMetrics
	runtimeConfig.CacheDefaultTTL = cfg.CacheDefaultTTL
	runtimeConfig.CacheFailOpen = cfg.CacheFailOpen
	runtime := agentruntime.NewRuntime(
		runtimeConfig,
		sessionStore,
		memoryService,
		skillCatalog,
		engineFactory,
	)
	runtime.SetWorkflowStore(workflowStore)
	runtime.SetDeepAgentEvidenceRepository(deepAgentEvidenceRepo)
	runtime.SetLoopGoalStore(loopGoalStore)
	runtime.SetLoopTriggerStore(loopTriggerStore)
	runtime.SetLoopTriggerQuotaChecker(func(ctx context.Context, req agentruntime.LoopTriggerRequest) error {
		return agentruntime.CheckLoopTriggerQuota(ctx, llmUsageStore, llmConfigManager.Get(), req.UserID)
	})
	runtime.SetLoopTriggerPolicy(loopTriggerPolicyFromStartup(cfg))
	runtime.SetToolCallLedgerStore(toolCallLedgerStore)
	runtime.SetPromptStore(promptStore)
	kafkaConfig := kafkaMessageEventConfigFromStartup(cfg)
	publishKafkaEvents, localVectorIndexing := bootstrap.MessageEventsBackendMode(cfg.MessageEventsBackend)
	runtime.SetLocalMessageVectorIndexing(localVectorIndexing)
	var kafkaMessagePublisher agentruntime.MessageEventPublisher
	var kafkaPublisherCloser interface{ Close() error }
	if publishKafkaEvents {
		publisher, closer := bootstrap.BuildKafkaMessageEventPublisher(kafkaConfig)
		kafkaMessagePublisher = publisher
		runtime.SetMessageEventPublisher(publisher)
		kafkaPublisherCloser = closer
		defer func() {
			if kafkaPublisherCloser != nil {
				if err := kafkaPublisherCloser.Close(); err != nil {
					logInfof("close kafka message event publisher: %v", err)
				}
			}
		}()
	}
	var kafkaProcessedLockRedisClient interface{ Close() error }
	if cfg.MessageEventsKafkaConsumerEnabled {
		kafkaConsumer, redisClient := buildKafkaMessageEventConsumerWorker(
			kafkaConfig,
			runtimeConfig.MessageSearch,
			sessionStore,
			cfg.MessageEventsProcessedLockBackend,
			cfg.MessageEventsProcessedLockRedisURL,
			cfg.MessageEventsProcessedLockTTL,
		)
		kafkaProcessedLockRedisClient = redisClient
		workerGroup.Start(workerKafkaMessageConsumer, kafkaConsumer.Run, workerlifecycle.WithStop(func(context.Context) error {
			return kafkaConsumer.Close()
		}))
		defer func() {
			if kafkaProcessedLockRedisClient != nil {
				if err := kafkaProcessedLockRedisClient.Close(); err != nil {
					logInfof("close kafka message event processed lock redis client: %v", err)
				}
			}
		}()
	}
	var messageContextRedisClient bootstrap.RedisHealthCloser
	var sessionListRedisClient bootstrap.RedisHealthCloser
	var messageSequenceRedisClient bootstrap.RedisHealthCloser
	var liveSetupPromptRedisClient bootstrap.RedisHealthCloser
	if setter, ok := sessionStore.(interface {
		SetMessageSequenceAllocator(agentruntime.MessageSequenceAllocator)
	}); ok {
		allocator, redisClient := bootstrap.BuildMessageSequenceAllocator(cfg.MessageSequenceBackend, cfg.MessageSequenceRedisURL)
		setter.SetMessageSequenceAllocator(allocator)
		messageSequenceRedisClient = redisClient
		if messageSequenceRedisClient != nil {
			defer func() {
				if err := messageSequenceRedisClient.Close(); err != nil {
					logInfof("close message sequence redis client: %v", err)
				}
			}()
		}
	}
	if setter, ok := sessionStore.(interface {
		SetSessionListCache(agentruntime.SessionListCache)
	}); ok {
		cache, redisClient := bootstrap.BuildSessionListCache(cfg.SessionListCacheBackend, cfg.SessionListCacheRedisURL, cfg.SessionListCacheTTL)
		setter.SetSessionListCache(cache)
		sessionListRedisClient = redisClient
		if sessionListRedisClient != nil {
			defer func() {
				if err := sessionListRedisClient.Close(); err != nil {
					logInfof("close session list redis client: %v", err)
				}
			}()
		}
	}
	if _, ok := sessionStore.(agentruntime.MessageRepository); ok {
		cache, redisClient := bootstrap.BuildMessageContextCache(cfg.MessageContextCacheBackend, cfg.MessageContextCacheRedisURL, cfg.MessageContextCacheTTL)
		messageContextRedisClient = redisClient
		runtime.SetMessageContextCache(cache)
		if messageContextRedisClient != nil {
			defer func() {
				if err := messageContextRedisClient.Close(); err != nil {
					logInfof("close message context redis client: %v", err)
				}
			}()
		}
	}
	if cfg.LiveEnabled {
		cache, redisClient := bootstrap.BuildLiveSetupPromptCache(cfg.LiveSetupPromptCacheBackend, cfg.LiveSetupPromptCacheRedisURL, cfg.LiveSetupPromptCacheTTL)
		liveSetupPromptRedisClient = redisClient
		runtime.SetLiveSetupPromptCache(cache)
		if liveSetupPromptRedisClient != nil {
			defer func() {
				if err := liveSetupPromptRedisClient.Close(); err != nil {
					logInfof("close live setup prompt redis client: %v", err)
				}
			}()
		}
	}
	if kafkaMessagePublisher != nil {
		runtime.SetMessageEventPublisher(kafkaMessagePublisher)
	}
	llmMemoryExtractor := agentruntime.NewLLMMemoryExtractor(engineFactory)
	llmMemoryExtractor.PromptResolver = agentruntime.NewCachedPromptResolver(promptStore, nil, cacheStore, cfg.CacheDefaultTTL, cfg.CacheFailOpen, cacheMetrics)
	runtime.SetMemoryExtractor(agentruntime.NewHybridMemoryExtractor(
		llmMemoryExtractor,
		agentruntime.NewRuleMemoryExtractor(),
	))
	llmEpisodeSummarizer := agentruntime.NewLLMMemoryEpisodeSummarizer(engineFactory)
	llmEpisodeSummarizer.Timeout = cfg.EpisodicMemorySummarizeTimeout
	llmEpisodeSummarizer.PromptResolver = agentruntime.NewCachedPromptResolver(promptStore, nil, cacheStore, cfg.CacheDefaultTTL, cfg.CacheFailOpen, cacheMetrics)
	runtime.SetMemoryEpisodeSummarizer(agentruntime.NewHybridMemoryEpisodeSummarizer(
		llmEpisodeSummarizer,
		agentruntime.RuleMemoryEpisodeSummarizer{},
	))
	runtime.SetMemoryOrganizer(agentruntime.NewHybridMemoryOrganizer(
		agentruntime.NewLLMMemoryOrganizer(engineFactory),
		agentruntime.NewRuleMemoryOrganizer(),
	))
	runtime.SetArtifactService(artifactService)
	runtime.SetAssetInsightStore(assetInsightStore)
	var messageArchiveObjectStore *agentruntime.MessageArchiveObjectStore
	if setter, ok := sessionStore.(interface {
		SetMessageArchiveObjectStore(agentruntime.ObjectStore, string)
	}); ok && artifactService != nil && artifactService.Objects != nil {
		setter.SetMessageArchiveObjectStore(artifactService.Objects, cfg.MessageArchivePrefix)
		messageArchiveObjectStore = agentruntime.NewMessageArchiveObjectStore(artifactService.Objects, cfg.MessageArchivePrefix)
	}
	attachmentWorkerStarted := false
	if cfg.MessageAttachmentWorkerEnabled {
		if queue, ok := sessionStore.(agentruntime.MessageAttachmentProcessingQueue); ok && artifactService != nil && artifactService.Objects != nil {
			worker := agentruntime.NewMessageAttachmentWorker(queue, artifactService, agentruntime.MessageAttachmentWorkerConfig{
				BatchSize:             cfg.MessageAttachmentWorkerBatchSize,
				PollInterval:          cfg.MessageAttachmentWorkerPollInterval,
				ProcessTimeout:        cfg.MessageAttachmentWorkerProcessTimeout,
				ThumbnailMaxDimension: cfg.MessageAttachmentThumbnailMaxDimension,
				ContentIndexer:        buildMessageAttachmentContentIndexer(runtimeConfig.MessageSearch, sessionStore),
			}, nil)
			workerGroup.Start(workerMessageAttachment, worker.Run)
			attachmentWorkerStarted = true
		} else {
			logInfof("message attachment worker disabled: SQL message attachment queue and artifact object store are required")
		}
	}
	archiveWorkerStarted := false
	if cfg.MessageArchiveWorkerEnabled {
		if queue, ok := sessionStore.(agentruntime.MessageArchiveQueue); ok && messageArchiveObjectStore != nil {
			worker := agentruntime.NewMessageArchiveWorker(queue, messageArchiveObjectStore, agentruntime.MessageArchiveWorkerConfig{
				ArchiveAfter:   cfg.MessageArchiveAfter,
				BatchSize:      cfg.MessageArchiveWorkerBatchSize,
				PollInterval:   cfg.MessageArchiveWorkerPollInterval,
				ProcessTimeout: cfg.MessageArchiveWorkerProcessTimeout,
				ClearPGPayload: cfg.MessageArchiveClearPGPayload,
			}, nil)
			workerGroup.Start(workerMessageArchive, worker.Run)
			archiveWorkerStarted = true
		} else {
			logInfof("message archive worker disabled: SQL message archive queue and artifact object store are required")
		}
	}
	messageSearchIndexManagerStarted := false
	if cfg.MessageSearchIndexManagementEnabled {
		normalizedBackend := strings.ToLower(strings.TrimSpace(cfg.MessageSearchBackend))
		if normalizedBackend == "elastic" || normalizedBackend == "fulltext" || normalizedBackend == "full-text" {
			normalizedBackend = "elasticsearch"
		}
		if normalizedBackend != "elasticsearch" && normalizedBackend != "hybrid" {
			logInfof("message search index manager disabled: backend %s does not use Elasticsearch lifecycle management", cfg.MessageSearchBackend)
		} else if strings.TrimSpace(cfg.MessageSearchEndpoint) == "" {
			logInfof("message search index manager disabled: message search endpoint is required")
		} else {
			manager := agentruntime.NewElasticsearchMessageIndexManagerWithLogger(runtimeConfig.MessageSearch, runLogger("message_search_index_manager"))
			workerGroup.Start(workerMessageSearchIndex, manager.Run)
			messageSearchIndexManagerStarted = true
		}
	}
	runtime.SetJobStore(jobStore)
	runtime.SetSkillExecutionStore(skillExecutionStore)
	riskScanner := agentruntime.NewBasicRiskScanner()
	runtime.SetRiskScanner(riskScanner)
	runtime.SetRiskRecorder(func(ctx context.Context, event agentruntime.RiskEvent) {
		if err := riskStore.RecordRiskEvent(ctx, event); err != nil {
			logInfof("record risk event: %v", err)
		}
	})
	jobQueue, jobQueueRedisClient := bootstrap.BuildRedisJobQueue(cfg.JobQueueRedisURL, agentruntime.RedisJobQueueConfig{
		Stream:       cfg.JobQueueStream,
		Group:        cfg.JobQueueConsumerGroup,
		Consumer:     cfg.JobQueueConsumer,
		BlockTimeout: cfg.JobQueueBlockTimeout,
		ClaimIdle:    cfg.JobQueueClaimIdle,
		LockTTL:      cfg.JobQueueLockTTL,
	})
	runtime.SetJobQueue(jobQueue)
	defer func() {
		if err := jobQueueRedisClient.Close(); err != nil {
			logInfof("close job queue redis client: %v", err)
		}
	}()
	jobEventFanoutStarted := false
	if cfg.JobEventFanoutEnabled {
		fanout := agentruntime.NewRedisJobEventFanout(jobQueueRedisClient, agentruntime.RedisJobEventFanoutConfig{
			Channel: cfg.JobEventFanoutChannel,
			Origin:  cfg.JobEventFanoutOrigin,
		}, nil)
		runtime.SetJobEventFanout(fanout)
		workerGroup.Start(workerJobEventFanout, func(ctx context.Context) error {
			return fanout.Run(ctx, runtime.PublishRemoteJobEvent)
		})
		jobEventFanoutStarted = true
	}
	jobWorkerStarted := false
	if cfg.JobWorkerEnabled {
		worker := agentruntime.NewJobWorkerWithLogger(jobQueue, runtime, agentruntime.JobWorkerConfig{LockTTL: cfg.JobQueueLockTTL}, runLogger("job_worker"))
		workerGroup.Start(workerJobQueue, worker.Run)
		jobWorkerStarted = true
	}
	server := agentruntime.NewServerWithLogger(
		runtime,
		auth,
		limiter,
		runLogger("http_server"),
	)
	server.SetAuthService(authService)
	server.SetAuditLogger(buildAuditLogger(storeCfg))
	server.SetRiskStore(riskStore)
	server.SetRiskScanner(riskScanner)
	server.SetOperationRateLimiter(agentruntime.NewOperationRateLimiter(parseOperationRateLimits(cfg.OperationRateLimits)))
	server.SetAdminToken(cfg.AdminToken)
	server.SetSkillRegistry(skillRegistrySetup.registry)
	server.SetLLMUsageStore(llmUsageStore)
	server.SetEvaluationStore(evaluationStore)
	server.SetPromptStore(promptStore)
	server.SetEvaluationJudge(evaluationJudgeFromStartup(cfg, llmCfg, llmConfigManager, llmUsageStore))
	server.SetLLMGovernanceConfigManager(llmConfigManager)
	server.SetLoopWebhookSecrets(parseLoopWebhookSecrets(cfg.LoopWebhookSecrets))
	stopDailyEvaluation := server.StartDailyEvaluationScheduler(dailyEvaluationConfigFromStartup(cfg))
	defer stopDailyEvaluation()
	stopLoopAutomation := server.StartLoopTriggerAutomationScheduler(agentruntime.LoopTriggerAutomationConfig{
		Enabled:      cfg.LoopAutomationEnabled,
		PollInterval: cfg.LoopAutomationInterval,
	})
	defer stopLoopAutomation()
	server.SetLLMStatusProvider(llmStatusFn)
	if cfg.LoopAutomationEnabled {
		server.AddReadinessCheck("loop_automation", server.LoopTriggerAutomationReadinessCheck())
	}
	server.AddReadinessCheck("llm_config", func(ctx context.Context) error {
		return bootstrap.LLMConfigReadinessCheck(bootstrap.ApplyRuntimeLLMConfig(llmCfg, llmConfigManager.Get()))(ctx)
	})
	server.AddReadinessCheck("llm", agentruntime.LLMReadinessCheck(llmStatusFn))
	if strings.EqualFold(strings.TrimSpace(storeCfg.backend), "sql") {
		readyDB := openSQLDB(storeCfg)
		server.AddReadinessCheck("sql", readyDB.PingContext)
	}
	if strings.EqualFold(strings.TrimSpace(cfg.RateLimitBackend), "redis") {
		server.AddReadinessCheck("redis", agentruntime.RedisReadinessCheck(limiter))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.CacheBackend), "redis") && cacheRedisClient != nil {
		server.AddReadinessCheck("cache", agentruntime.RedisClientReadinessCheck(cacheRedisClient))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.MessageContextCacheBackend), "redis") && messageContextRedisClient != nil {
		server.AddReadinessCheck("message_context_cache", agentruntime.RedisClientReadinessCheck(messageContextRedisClient))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.SessionListCacheBackend), "redis") && sessionListRedisClient != nil {
		server.AddReadinessCheck("session_list_cache", agentruntime.RedisClientReadinessCheck(sessionListRedisClient))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.MessageSequenceBackend), "redis") && messageSequenceRedisClient != nil {
		server.AddReadinessCheck("message_sequence", agentruntime.RedisClientReadinessCheck(messageSequenceRedisClient))
	}
	if strings.EqualFold(strings.TrimSpace(cfg.LiveSetupPromptCacheBackend), "redis") && liveSetupPromptRedisClient != nil {
		server.AddReadinessCheck("live_setup_prompt_cache", agentruntime.RedisClientReadinessCheck(liveSetupPromptRedisClient))
	}
	server.AddReadinessCheck("job_queue", agentruntime.RedisClientReadinessCheck(jobQueueRedisClient))
	if publishKafkaEvents || cfg.MessageEventsKafkaConsumerEnabled {
		server.AddReadinessCheck("kafka_message_events", agentruntime.KafkaBrokerReadinessCheck(kafkaConfig.Brokers))
	}
	if artifactService != nil && artifactService.Objects != nil {
		server.AddReadinessCheck("object_store", agentruntime.ObjectStoreReadinessCheck(artifactService.Objects, "agentapi"))
	}
	server.AddReadinessCheck("workers", workerGroup.ReadinessCheck())
	if err := server.SetWebSecurity(agentruntime.WebSecurityConfig{
		CORSAllowedOrigins:   startupconfig.SplitCSV(cfg.CORSAllowedOrigins),
		CORSAllowCredentials: cfg.CORSAllowCredentials,
		SessionCookieName:    cfg.SessionCookieName,
		CSRFTokenCookieName:  cfg.CSRFCookieName,
		CSRFHeaderName:       cfg.CSRFHeaderName,
		CookieDomain:         cfg.SessionCookieDomain,
		CookiePath:           "/",
		CookieSecure:         cfg.SessionCookieSecure,
		CookieHTTPOnly:       true,
		CookieSameSite:       agentruntime.ParseSameSite(cfg.SessionCookieSameSite),
		EnableCSRF:           cfg.CSRFEnabled,
		RequestTimeout:       cfg.RequestTimeout,
	}); err != nil {
		logFatal(err)
	}
	startRetentionPruneWorker(workerGroup, runtime, authService, cfg.RetentionDays)
	startLocalUploadedArtifactPruneWorker(workerGroup, runtime, cfg.LocalArtifactStagingRetention, 24*time.Hour)
	defer func() {
		shutdownTimeout := cfg.ShutdownTimeout
		if shutdownTimeout <= 0 {
			shutdownTimeout = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := workerGroup.Stop(ctx); err != nil {
			logInfof("worker shutdown failed: %v", err)
		}
	}()

	logStartup(cfg, llmCfg, llmConfigManager, authService, startupStatus{
		JobWorkerStarted:                 jobWorkerStarted,
		JobEventFanoutStarted:            jobEventFanoutStarted,
		MessageAttachmentWorkerStarted:   attachmentWorkerStarted,
		MessageArchiveWorkerStarted:      archiveWorkerStarted,
		MessageSearchIndexManagerStarted: messageSearchIndexManagerStarted,
	})
	if err := httpListenAndServe(cfg.Addr, server, cfg.ShutdownTimeout); err != nil {
		logFatal(err)
	}
}

func loopTriggerPolicyFromStartup(cfg startupconfig.Config) agentruntime.LoopTriggerPolicy {
	policy := agentruntime.DefaultLoopTriggerPolicy()
	policy.ScheduleEnabled = cfg.LoopScheduleTriggersEnabled
	policy.MonitorEnabled = cfg.LoopMonitorTriggersEnabled
	policy.EvalRepairEnabled = cfg.LoopEvalRepairTriggersEnabled
	policy.WebhookEnabled = cfg.LoopWebhookTriggersEnabled
	policy.ReleaseGate = agentruntime.LoopReleaseGateReport{
		CriticalTestsPassed:        cfg.LoopReleaseGateCriticalTestsPassed,
		TemplateReplayPassCount:    cfg.LoopReleaseGateTemplateReplayPassCount,
		GovernanceKillSwitchPassed: cfg.LoopReleaseGateKillSwitchPassed,
		QuotaGuardPassed:           cfg.LoopReleaseGateQuotaGuardPassed,
	}
	for source := range parseLoopWebhookSecrets(cfg.LoopWebhookSecrets) {
		policy.WebhookAllowedSources = append(policy.WebhookAllowedSources, source)
	}
	return policy
}

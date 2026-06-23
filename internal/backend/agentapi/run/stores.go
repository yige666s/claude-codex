package run

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/harness/skills"
)

type storeConfig struct {
	backend            string
	dataDir            string
	objectBaseURL      string
	objectToken        string
	objectTimeout      time.Duration
	sqlDriver          string
	sqlDSN             string
	sqlDialect         string
	sqlMaxOpen         int
	sqlMaxIdle         int
	sqlConnMaxLifetime time.Duration
}

func buildStores(cfg storeConfig) (agentruntime.SessionStore, agentruntime.MemoryService) {
	switch strings.ToLower(strings.TrimSpace(cfg.backend)) {
	case "object":
		var objects agentruntime.ObjectStore
		if strings.TrimSpace(cfg.objectBaseURL) != "" {
			objects = &agentruntime.HTTPObjectStore{
				BaseURL: cfg.objectBaseURL,
				Token:   cfg.objectToken,
				Client:  &http.Client{Timeout: cfg.objectTimeout},
			}
		} else {
			objects = agentruntime.NewFileObjectStore(filepath.Join(cfg.dataDir, "objects"))
		}
		return agentruntime.NewObjectSessionStore(objects, "agentapi"), agentruntime.NewObjectMemoryService(objects, "agentapi")
	case "sql":
		db := openSQLDB(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		sessionStore := agentruntime.NewSQLSessionStoreWithDialect(db, dialect)
		memoryService := agentruntime.NewSQLMemoryServiceWithDialect(db, dialect)
		if err := sessionStore.Init(ctx); err != nil {
			logFatalf("init sql session store: %v", err)
		}
		if err := memoryService.Init(ctx); err != nil {
			logFatalf("init sql memory service: %v", err)
		}
		return sessionStore, memoryService
	default:
		return agentruntime.NewFileSessionStore(cfg.dataDir), agentruntime.NewFileMemoryService(cfg.dataDir)
	}
}

type skillRegistrySetup struct {
	catalog  agentruntime.SkillCatalog
	registry agentruntime.SkillRegistryAdminStore
}

func buildSkillRegistrySetup(cfg storeConfig, skillManager *skills.SkillManager) skillRegistrySetup {
	if !strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		return skillRegistrySetup{catalog: agentruntime.NewPublishedSkillCatalog(skillManager, nil, true)}
	}
	db := openSQLDB(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
	registry := agentruntime.NewSQLSkillRegistryWithDialect(db, dialect)
	if err := registry.Init(ctx); err != nil {
		logFatalf("init sql skill registry: %v", err)
	}
	if err := registry.SyncLoadedSkills(ctx, skillManager.ListSkills()); err != nil {
		logFatalf("sync sql skill registry: %v", err)
	}
	records, err := registry.ListSkills(ctx)
	if err != nil {
		logFatalf("load sql skill registry: %v", err)
	}
	logInfof("skill registry: sql records=%d published=%d", len(records), countPublishedSkillRecords(records))
	return skillRegistrySetup{
		catalog:  agentruntime.NewRegistrySkillCatalog(skillManager, records),
		registry: registry,
	}
}

func filteredSkillManager(catalog agentruntime.SkillCatalog) *skills.SkillManager {
	manager := skills.NewSkillManager()
	if catalog == nil {
		return manager
	}
	if err := manager.RegisterLoadedSkills(catalog.ListUserInvocableSkills()); err != nil {
		logInfof("warning: failed to build published skill manager: %v", err)
	}
	return manager
}

func buildLLMUsageStore(cfg storeConfig) agentruntime.LLMUsageStore {
	if !strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		logInfof("warning: LLM usage records are in-memory because store-backend is not sql")
		return agentruntime.NewMemoryLLMUsageStore()
	}
	db := openSQLDB(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
	store := agentruntime.NewSQLLLMUsageStoreWithDialect(db, dialect)
	if err := store.Init(ctx); err != nil {
		logFatalf("init sql llm usage store: %v", err)
	}
	return store
}

func buildRuntimeConfigStore(cfg storeConfig) agentruntime.LLMGovernanceConfigStore {
	if !strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		logInfof("warning: runtime config changes are in-memory because store-backend is not sql")
		return nil
	}
	db := openSQLDB(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
	store := agentruntime.NewSQLRuntimeConfigStoreWithDialect(db, dialect)
	if err := store.Init(ctx); err != nil {
		logFatalf("init sql runtime config store: %v", err)
	}
	return store
}

func buildAssetInsightStore(cfg storeConfig) agentruntime.AssetInsightStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLAssetInsightStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql asset insight store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryAssetInsightStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory asset insight store: %v", err)
	}
	return store
}

func buildSkillExecutionStore(cfg storeConfig) agentruntime.SkillExecutionStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLSkillExecutionStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql skill execution store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemorySkillExecutionStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory skill execution store: %v", err)
	}
	return store
}

func buildWorkflowStore(cfg storeConfig) agentruntime.WorkflowStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLWorkflowStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql workflow store: %v", err)
		}
		return store
	}
	return agentruntime.NewMemoryWorkflowStore()
}

func buildDeepAgentEvidenceRepository(cfg storeConfig) agentruntime.DeepAgentEvidenceRepository {
	var repo agentruntime.DeepAgentEvidenceRepository
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		repo = agentruntime.NewSQLDeepAgentEvidenceRepositoryWithDialect(db, dialect)
	} else {
		repo = agentruntime.NewMemoryDeepAgentEvidenceRepository()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := repo.Init(ctx); err != nil {
		logFatalf("init deep agent evidence repository: %v", err)
	}
	return repo
}

func buildToolCallLedgerStore(cfg storeConfig) agentruntime.ToolCallLedgerStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLToolCallLedgerStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql tool call ledger store: %v", err)
		}
		return store
	}
	return agentruntime.NewMemoryToolCallLedgerStore()
}

func buildEvaluationStore(cfg storeConfig) agentruntime.EvaluationStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLEvaluationStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql evaluation store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryEvaluationStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory evaluation store: %v", err)
	}
	return store
}

func buildPromptStore(cfg storeConfig) agentruntime.PromptStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLPromptStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql prompt store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryPromptStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory prompt store: %v", err)
	}
	return store
}

func buildConnectorStore(cfg storeConfig) agentruntime.ConnectorStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLConnectorStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init connector store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryConnectorStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init connector store: %v", err)
	}
	return store
}

func buildConnectorTokenVault(cfg storeConfig) agentruntime.ConnectorTokenVault {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		vault := agentruntime.NewSQLConnectorTokenVaultWithDialect(db, dialect)
		if err := vault.Init(ctx); err != nil {
			logFatalf("init connector token vault: %v", err)
		}
		return vault
	}
	return agentruntime.NewMemoryConnectorTokenVault()
}

func buildMCPConnectorStore(cfg storeConfig) agentruntime.MCPConnectorStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLMCPConnectorStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init mcp connector store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryMCPConnectorStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory mcp connector store: %v", err)
	}
	return store
}

func buildBrowserPushStore(cfg storeConfig) agentruntime.BrowserPushStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLBrowserPushStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init browser push store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryBrowserPushStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory browser push store: %v", err)
	}
	return store
}

func countPublishedSkillRecords(records []agentruntime.SkillRegistryRecord) int {
	count := 0
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Status), agentruntime.SkillStatusPublished) {
			count++
		}
	}
	return count
}

func buildJobStore(cfg storeConfig) agentruntime.JobStore {
	var store agentruntime.JobStore
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store = agentruntime.NewSQLJobStoreWithDialect(db, dialect)
	} else {
		store = agentruntime.NewMemoryJobStore()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Init(ctx); err != nil {
		logFatalf("init job store: %v", err)
	}
	return store
}

func buildLoopGoalStore(cfg storeConfig) agentruntime.LoopGoalStore {
	var store agentruntime.LoopGoalStore
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store = agentruntime.NewSQLLoopGoalStoreWithDialect(db, dialect)
	} else {
		store = agentruntime.NewMemoryLoopGoalStore()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Init(ctx); err != nil {
		logFatalf("init loop goal store: %v", err)
	}
	return store
}

func buildLoopTriggerStore(cfg storeConfig) agentruntime.LoopTriggerStore {
	var store agentruntime.LoopTriggerStore
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store = agentruntime.NewSQLLoopTriggerStoreWithDialect(db, dialect)
	} else {
		store = agentruntime.NewMemoryLoopTriggerStore()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Init(ctx); err != nil {
		logFatalf("init loop trigger store: %v", err)
	}
	return store
}

func buildAuditLogger(cfg storeConfig) agentruntime.AuditLogger {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		logger := agentruntime.NewSQLAuditLoggerWithDialect(db, dialect)
		if err := logger.Init(ctx); err != nil {
			logFatalf("init sql audit logger: %v", err)
		}
		return logger
	}
	logger := agentruntime.NewMemoryAuditLogger()
	if err := logger.Init(ctx); err != nil {
		logFatalf("init memory audit logger: %v", err)
	}
	return logger
}

func buildRiskStore(cfg storeConfig) agentruntime.RiskStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLRiskStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			logFatalf("init sql risk store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryRiskStore()
	if err := store.Init(ctx); err != nil {
		logFatalf("init memory risk store: %v", err)
	}
	return store
}

func openSQLDB(cfg storeConfig) *sql.DB {
	if strings.TrimSpace(cfg.sqlDriver) == "" || strings.TrimSpace(cfg.sqlDSN) == "" {
		logFatal("store-backend=sql requires -sql-driver and -sql-dsn")
	}
	db, err := sql.Open(cfg.sqlDriver, cfg.sqlDSN)
	if err != nil {
		logFatalf("open sql store: %v", err)
	}
	if cfg.sqlMaxOpen > 0 {
		db.SetMaxOpenConns(cfg.sqlMaxOpen)
	}
	if cfg.sqlMaxIdle > 0 {
		db.SetMaxIdleConns(cfg.sqlMaxIdle)
	}
	if cfg.sqlConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.sqlConnMaxLifetime)
	}
	dialect := agentruntime.ParseSQLDialect(startupconfig.FirstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
	if dialect == agentruntime.SQLDialectPostgres {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := agentruntime.RunPostgresGooseMigrations(ctx, db, dialect); err != nil {
			_ = db.Close()
			logFatalf("run postgres migrations: %v", err)
		}
	}
	return db
}

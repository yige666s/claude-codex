package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/googleauth"
	"claude-codex/internal/harness/anthropic"
	"claude-codex/internal/harness/engine"
	providerbackend "claude-codex/internal/harness/provider"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/tools"
	bashtool "claude-codex/internal/harness/tools/bash"
	filetool "claude-codex/internal/harness/tools/file"
	searchtool "claude-codex/internal/harness/tools/search"
	skilltool "claude-codex/internal/harness/tools/skill"
	webtool "claude-codex/internal/harness/tools/web"
)

func main() {
	addr := flag.String("addr", ":8081", "HTTP server address")
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for user-scoped sessions and memory")
	storeBackend := flag.String("store-backend", "file", "storage backend: file, object, or sql")
	objectBaseURL := flag.String("object-base-url", os.Getenv("AGENT_API_OBJECT_BASE_URL"), "HTTP object store base URL for store-backend=object")
	objectToken := flag.String("object-token", os.Getenv("AGENT_API_OBJECT_TOKEN"), "bearer token for HTTP object store")
	objectTimeout := flag.Duration("object-timeout", envDuration("AGENT_API_OBJECT_TIMEOUT", 10*time.Second), "HTTP object store request timeout")
	artifactStore := flag.String("artifact-store", firstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_STORE"), "file"), "artifact object store: file or s3")
	artifactS3Endpoint := flag.String("artifact-s3-endpoint", firstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_ENDPOINT"), "localhost:9000"), "S3/R2-compatible endpoint for artifacts")
	artifactS3AccessKey := flag.String("artifact-s3-access-key", firstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_ACCESS_KEY"), "minioadmin"), "S3/R2 access key for artifacts")
	artifactS3SecretKey := flag.String("artifact-s3-secret-key", firstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_SECRET_KEY"), "minioadmin"), "S3/R2 secret key for artifacts")
	artifactS3Bucket := flag.String("artifact-s3-bucket", firstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_BUCKET"), "agentapi"), "S3/R2 bucket for artifacts")
	artifactS3Prefix := flag.String("artifact-s3-prefix", os.Getenv("AGENT_API_ARTIFACT_S3_PREFIX"), "S3/R2 key prefix for artifacts")
	artifactS3SSL := flag.Bool("artifact-s3-ssl", envBool("AGENT_API_ARTIFACT_S3_SSL", false), "use HTTPS for S3/R2 artifacts")
	assetMaxBytes := flag.Int64("asset-max-bytes", envInt64("AGENT_API_ASSET_MAX_BYTES", agentruntime.DefaultMaxAssetBytes), "max bytes for attachments and generated artifacts")
	sqlDriver := flag.String("sql-driver", os.Getenv("AGENT_API_SQL_DRIVER"), "database/sql driver name for store-backend=sql")
	sqlDSN := flag.String("sql-dsn", os.Getenv("AGENT_API_SQL_DSN"), "database/sql DSN for store-backend=sql")
	sqlDialect := flag.String("sql-dialect", os.Getenv("AGENT_API_SQL_DIALECT"), "SQL dialect: question or postgres")
	sqlMaxOpen := flag.Int("sql-max-open-conns", envInt("AGENT_API_SQL_MAX_OPEN_CONNS", 20), "max open SQL connections")
	sqlMaxIdle := flag.Int("sql-max-idle-conns", envInt("AGENT_API_SQL_MAX_IDLE_CONNS", 10), "max idle SQL connections")
	sqlConnMaxLifetime := flag.Duration("sql-conn-max-lifetime", envDuration("AGENT_API_SQL_CONN_MAX_LIFETIME", 30*time.Minute), "max SQL connection lifetime")
	workspace := flag.String("workspace", mustWorkingDir(), "default working directory")
	userWorkspaceRoot := flag.String("user-workspace-root", os.Getenv("AGENT_API_USER_WORKSPACE_ROOT"), "root directory for per-user sandboxed workspaces")
	allowCustomWorkingDir := flag.Bool("allow-custom-working-dir", envBool("AGENT_API_ALLOW_CUSTOM_WORKING_DIR", false), "allow request-provided working_dir when no user workspace root is configured")
	llmProvider := flag.String("llm-provider", firstNonEmpty(os.Getenv("AGENT_API_LLM_PROVIDER"), os.Getenv("CLAUDE_CODE_PROVIDER"), "anthropic"), "LLM provider: anthropic, openai, qwen, gemini, vertex, or custom")
	apiKey := flag.String("api-key", "", "LLM API key; env fallback depends on -llm-provider")
	apiToken := flag.String("api-token", "", "LLM bearer/OAuth token; env fallback depends on -llm-provider")
	apiBaseURL := flag.String("api-base-url", "", "LLM API base URL; use with openai-compatible custom providers")
	model := flag.String("model", "", "model to use; provider default if empty")
	llmFallbacks := flag.String("llm-fallbacks", os.Getenv("AGENT_API_LLM_FALLBACKS"), "comma-separated fallback LLM specs provider[:model], credentials resolved from env")
	llmModelRoutes := flag.String("llm-model-routes", os.Getenv("AGENT_API_LLM_MODEL_ROUTES"), "comma-separated model routes: default=model,chat=model,skill:skill-name=model")
	llmMaxAttempts := flag.Int("llm-max-attempts", envInt("AGENT_API_LLM_MAX_ATTEMPTS", 2), "max LLM attempts across retries and fallbacks")
	llmRetryBackoff := flag.Duration("llm-retry-backoff", envDuration("AGENT_API_LLM_RETRY_BACKOFF", 300*time.Millisecond), "base backoff between retry rounds")
	llmChatTimeout := flag.Duration("llm-chat-timeout", envDuration("AGENT_API_LLM_CHAT_TIMEOUT", 60*time.Second), "timeout for one chat LLM provider call")
	llmSkillTimeout := flag.Duration("llm-skill-timeout", envDuration("AGENT_API_LLM_SKILL_TIMEOUT", 90*time.Second), "timeout for one skill/workflow LLM provider call")
	llmDailyTokenQuota := flag.Int("llm-daily-token-quota", envInt("AGENT_API_LLM_DAILY_TOKEN_QUOTA", 0), "daily successful LLM token quota per user; 0 disables")
	llmDailyRequestQuota := flag.Int("llm-daily-request-quota", envInt("AGENT_API_LLM_DAILY_REQUEST_QUOTA", 0), "daily successful LLM request quota per user; 0 disables")
	llmDailyCostQuotaUSD := flag.Float64("llm-daily-cost-quota-usd", envFloat64("AGENT_API_LLM_DAILY_COST_QUOTA_USD", 0), "daily estimated LLM cost quota per user in USD; 0 disables")
	llmInputCostPerMillion := flag.Float64("llm-input-cost-per-million", envFloat64("AGENT_API_LLM_INPUT_COST_PER_MILLION", 0.30), "estimated input token cost per 1M tokens")
	llmOutputCostPerMillion := flag.Float64("llm-output-cost-per-million", envFloat64("AGENT_API_LLM_OUTPUT_COST_PER_MILLION", 2.50), "estimated output token cost per 1M tokens")
	llmFailureThreshold := flag.Int("llm-failure-threshold", envInt("AGENT_API_LLM_FAILURE_THRESHOLD", 3), "consecutive retryable failures before temporarily disabling a backend")
	llmCircuitCooldown := flag.Duration("llm-circuit-cooldown", envDuration("AGENT_API_LLM_CIRCUIT_COOLDOWN", time.Minute), "duration to skip a backend after circuit breaker opens")
	authMode := flag.String("auth-mode", firstNonEmpty(os.Getenv("AGENT_API_AUTH_MODE"), "auto"), "auth mode: auto, jwt, cookie, trusted-header, header, none")
	authToken := flag.String("auth-token", os.Getenv("AGENT_API_AUTH_TOKEN"), "optional bearer token required for API requests")
	userHeader := flag.String("user-header", "X-User-ID", "header containing authenticated consumer user ID")
	jwtSecret := flag.String("jwt-secret", os.Getenv("AGENT_API_JWT_SECRET"), "HS256 JWT secret")
	jwtIssuer := flag.String("jwt-issuer", os.Getenv("AGENT_API_JWT_ISSUER"), "expected JWT issuer")
	jwtAudience := flag.String("jwt-audience", os.Getenv("AGENT_API_JWT_AUDIENCE"), "expected JWT audience")
	jwtUserClaim := flag.String("jwt-user-claim", firstNonEmpty(os.Getenv("AGENT_API_JWT_USER_CLAIM"), "sub"), "JWT claim containing consumer user ID")
	enableUserSystem := flag.Bool("enable-user-system", envBool("AGENT_API_ENABLE_USER_SYSTEM", false), "enable built-in consumer user registration/login APIs")
	authAccessTTL := flag.Duration("auth-access-ttl", envDuration("AGENT_API_AUTH_ACCESS_TTL", 15*time.Minute), "access token TTL for built-in user system")
	authRefreshTTL := flag.Duration("auth-refresh-ttl", envDuration("AGENT_API_AUTH_REFRESH_TTL", 30*24*time.Hour), "refresh token TTL for built-in user system")
	emailVerificationRequired := flag.Bool("email-verification-required", envBool("AGENT_API_EMAIL_VERIFICATION_REQUIRED", false), "require email verification before built-in users can log in")
	emailVerificationTTL := flag.Duration("email-verification-ttl", envDuration("AGENT_API_EMAIL_VERIFICATION_TTL", 24*time.Hour), "email verification token TTL")
	emailProvider := flag.String("email-provider", os.Getenv("AGENT_API_EMAIL_PROVIDER"), "email provider for auth emails: resend or empty")
	emailFrom := flag.String("email-from", os.Getenv("AGENT_API_EMAIL_FROM"), "from address for auth emails")
	emailPublicBaseURL := flag.String("email-public-base-url", os.Getenv("AGENT_API_EMAIL_PUBLIC_BASE_URL"), "public base URL used in auth email links")
	resendAPIKey := flag.String("resend-api-key", os.Getenv("AGENT_API_RESEND_API_KEY"), "Resend API key for auth emails")
	resendBaseURL := flag.String("resend-base-url", os.Getenv("AGENT_API_RESEND_BASE_URL"), "optional Resend API base URL")
	sessionCookieName := flag.String("session-cookie-name", firstNonEmpty(os.Getenv("AGENT_API_SESSION_COOKIE_NAME"), "agentapi_session"), "signed JWT session cookie name")
	sessionCookieSecret := flag.String("session-cookie-secret", os.Getenv("AGENT_API_SESSION_COOKIE_SECRET"), "HS256 secret for session cookie JWT")
	sessionCookieDomain := flag.String("session-cookie-domain", os.Getenv("AGENT_API_SESSION_COOKIE_DOMAIN"), "optional domain for session and CSRF cookies")
	sessionCookieSecure := flag.Bool("session-cookie-secure", envBool("AGENT_API_SESSION_COOKIE_SECURE", false), "set Secure on session and CSRF cookies")
	sessionCookieSameSite := flag.String("session-cookie-samesite", firstNonEmpty(os.Getenv("AGENT_API_SESSION_COOKIE_SAMESITE"), "lax"), "cookie SameSite policy: lax, strict, none")
	csrfEnabled := flag.Bool("csrf-enabled", envBool("AGENT_API_CSRF_ENABLED", false), "enable double-submit CSRF protection for session-cookie requests")
	csrfCookieName := flag.String("csrf-cookie-name", firstNonEmpty(os.Getenv("AGENT_API_CSRF_COOKIE_NAME"), "agentapi_csrf"), "CSRF token cookie name")
	csrfHeaderName := flag.String("csrf-header-name", firstNonEmpty(os.Getenv("AGENT_API_CSRF_HEADER_NAME"), "X-CSRF-Token"), "CSRF request header name")
	corsAllowedOrigins := flag.String("cors-allowed-origins", os.Getenv("AGENT_API_CORS_ALLOWED_ORIGINS"), "comma-separated browser origins allowed for CORS")
	corsAllowCredentials := flag.Bool("cors-allow-credentials", envBool("AGENT_API_CORS_ALLOW_CREDENTIALS", true), "allow credentials for CORS allowlisted origins")
	adminToken := flag.String("admin-token", os.Getenv("AGENT_API_ADMIN_TOKEN"), "shared token required for admin APIs")
	trustedUserHeader := flag.String("trusted-user-header", firstNonEmpty(os.Getenv("AGENT_API_TRUSTED_USER_HEADER"), "X-User-ID"), "trusted gateway user ID header")
	trustedSecretHeader := flag.String("trusted-secret-header", os.Getenv("AGENT_API_TRUSTED_SECRET_HEADER"), "header required for trusted-header auth")
	trustedSecret := flag.String("trusted-secret", os.Getenv("AGENT_API_TRUSTED_SECRET"), "secret value required for trusted-header auth")
	allowDangerousTools := flag.Bool("allow-dangerous-tools", false, "enable write/edit/bash tools and write/execute permissions")
	networkAllowlist := flag.String("network-allowlist", os.Getenv("AGENT_API_NETWORK_ALLOWLIST"), "comma-separated domains allowed for WebFetch/WebSearch; empty disables app-level web allowlist")
	skillDirs := flag.String("skill-dirs", os.Getenv("AGENT_API_SKILL_DIRS"), "comma-separated directories containing skill-name/SKILL.md folders")
	skillPolicy := flag.String("skill-policy", firstNonEmpty(os.Getenv("AGENT_API_SKILL_POLICY"), "allowlist"), "skill policy: allowlist or all")
	publishedSkills := flag.String("published-skills", os.Getenv("AGENT_API_PUBLISHED_SKILLS"), "comma-separated skill names published for consumer users")
	rateLimitBackend := flag.String("rate-limit-backend", firstNonEmpty(os.Getenv("AGENT_API_RATE_LIMIT_BACKEND"), "memory"), "rate limit backend: memory, redis, gateway, none")
	rateLimit := flag.Int("rate-limit", 60, "max requests per user per minute")
	operationRateLimits := flag.String("operation-rate-limits", os.Getenv("AGENT_API_OPERATION_RATE_LIMITS"), "comma-separated operation rate limits such as chat_message=60/m,job_create=20/m,data_export=5/h")
	redisURL := flag.String("redis-url", os.Getenv("AGENT_API_REDIS_URL"), "Redis URL for distributed rate limiting")
	redisFailOpen := flag.Bool("redis-fail-open", envBool("AGENT_API_REDIS_FAIL_OPEN", false), "allow requests when Redis rate limit is unavailable")
	retentionDays := flag.Int("retention-days", envInt("AGENT_API_RETENTION_DAYS", 0), "delete sessions and memory older than this many days on startup; 0 disables")
	shutdownTimeout := flag.Duration("shutdown-timeout", envDuration("AGENT_API_SHUTDOWN_TIMEOUT", 30*time.Second), "max time to drain HTTP requests and active agent work after SIGINT/SIGTERM")
	turnTimeout := flag.Duration("turn-timeout", 2*time.Minute, "max duration for one agent turn")
	skillShellTimeout := flag.Duration("skill-shell-timeout", envDuration("AGENT_API_SKILL_SHELL_TIMEOUT", 90*time.Second), "max duration for skill frontmatter shell commands")
	skillShellRunner := flag.String("skill-shell-runner", firstNonEmpty(os.Getenv("AGENT_API_SKILL_SHELL_RUNNER"), agentruntime.DefaultSkillShellRunner), "skill shell runner: local or docker")
	skillSandboxImage := flag.String("skill-sandbox-image", firstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_IMAGE"), agentruntime.DefaultSkillSandboxImage), "Docker image for skill shell runner=docker")
	skillSandboxNetwork := flag.String("skill-sandbox-network", firstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_NETWORK"), agentruntime.DefaultSkillSandboxNetwork), "Docker network mode for skill shell runner=docker; use none by default or bridge when a skill needs egress")
	skillSandboxMemory := flag.String("skill-sandbox-memory", firstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_MEMORY"), agentruntime.DefaultSkillSandboxMemory), "Docker memory limit for skill shell runner=docker")
	skillSandboxCPUs := flag.String("skill-sandbox-cpus", firstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_CPUS"), agentruntime.DefaultSkillSandboxCPUs), "Docker CPU quota for skill shell runner=docker")
	skillSandboxPidsLimit := flag.Int("skill-sandbox-pids-limit", envInt("AGENT_API_SKILL_SANDBOX_PIDS_LIMIT", agentruntime.DefaultSkillSandboxPidsLimit), "Docker pids limit for skill shell runner=docker")
	skillSandboxTmpfsSize := flag.String("skill-sandbox-tmpfs-size", firstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_TMPFS_SIZE"), agentruntime.DefaultSkillSandboxTmpfsSize), "Docker /tmp tmpfs size for skill shell runner=docker")
	flag.Parse()

	llmCfg, err := buildLLMConfig(*llmProvider, *model, *apiKey, *apiToken, *apiBaseURL, 600)
	if err != nil {
		log.Fatal(err)
	}

	storeCfg := storeConfig{
		backend:            *storeBackend,
		dataDir:            *dataDir,
		objectBaseURL:      *objectBaseURL,
		objectToken:        *objectToken,
		objectTimeout:      *objectTimeout,
		sqlDriver:          *sqlDriver,
		sqlDSN:             *sqlDSN,
		sqlDialect:         *sqlDialect,
		sqlMaxOpen:         *sqlMaxOpen,
		sqlMaxIdle:         *sqlMaxIdle,
		sqlConnMaxLifetime: *sqlConnMaxLifetime,
	}
	llmUsageStore := buildLLMUsageStore(storeCfg)
	riskStore := buildRiskStore(storeCfg)
	jobStore := buildJobStore(storeCfg)
	skillExecutionStore := buildSkillExecutionStore(storeCfg)
	llmGovernanceCfg := agentruntime.LLMGovernanceConfig{
		MaxAttempts:            *llmMaxAttempts,
		RetryBackoff:           *llmRetryBackoff,
		ChatTimeout:            *llmChatTimeout,
		SkillTimeout:           *llmSkillTimeout,
		DailyTokenQuota:        *llmDailyTokenQuota,
		DailyRequestQuota:      *llmDailyRequestQuota,
		DailyCostQuotaUSD:      *llmDailyCostQuotaUSD,
		InputCostPerMillion:    *llmInputCostPerMillion,
		OutputCostPerMillion:   *llmOutputCostPerMillion,
		FailureThreshold:       *llmFailureThreshold,
		CircuitBreakerCooldown: *llmCircuitCooldown,
	}
	var llmStatusMu sync.RWMutex
	var llmStatusProvider func() agentruntime.LLMGovernanceStatus

	skillManager := loadSkills(splitCSV(*skillDirs))
	skillRegistrySetup := buildSkillRegistrySetup(storeCfg, skillManager, splitCSV(*publishedSkills), strings.EqualFold(*skillPolicy, "all"))
	skillCatalog := skillRegistrySetup.catalog
	globalAllowed := allowedToolNames(*allowDangerousTools)
	globalNetworkAllowlist := splitCSV(*networkAllowlist)
	engineFactory := func(scope agentruntime.Scope) agentruntime.Runner {
		root := scope.WorkingDir
		if root == "" {
			root = *workspace
		}
		publishedSkillManager := filteredSkillManager(skillCatalog)
		effectiveAllowed := effectiveAllowedToolNames(globalAllowed, scope)
		registry := buildRegistry(root, publishedSkillManager, *allowDangerousTools, scope.Artifacts, scope.ArtifactMaxBytes, scopedNetworkAllowlist(globalNetworkAllowlist, scope.NetworkAllowlist), effectiveAllowed)
		checker := agentruntime.NewProductPermissionCheckerWithReporter(agentruntime.ToolPolicy{
			AllowWriteExecute: *allowDangerousTools,
			AllowedTools:      effectiveAllowed,
			SafeWriteTools:    []string{agentruntime.ArtifactToolName},
		}, func(ctx context.Context, denial agentruntime.ToolDenialRecord) {
			metadata := map[string]any{
				"tool_name":  denial.ToolName,
				"level":      denial.Level,
				"summary":    denial.Summary,
				"skill_name": scope.SkillName,
				"metadata":   denial.Metadata,
			}
			if err := riskStore.RecordRiskEvent(ctx, agentruntime.RiskEvent{
				UserID:     scope.UserID,
				SessionID:  scope.SessionID,
				Operation:  "tool_denied",
				Reason:     denial.Reason,
				RiskLevel:  agentruntime.RiskLevelMedium,
				ScoreDelta: 10,
				Metadata:   metadata,
			}); err != nil {
				log.Printf("record tool denial risk event: %v", err)
			}
		})
		planner, err := newGovernedPlannerForScope(llmCfg, *llmFallbacks, *llmModelRoutes, scope, llmUsageStore, llmGovernanceCfg)
		if err != nil {
			log.Fatal(err)
		}
		llmStatusMu.Lock()
		llmStatusProvider = planner.Status
		llmStatusMu.Unlock()
		eng := engine.NewWithDir(planner, registry, checker, 0, root)
		eng.SetSkillManager(publishedSkillManager)
		return eng
	}

	sessionStore, memoryService := buildStores(storeCfg)
	auth := buildAuthenticator(authConfig{
		mode:                *authMode,
		userHeader:          *userHeader,
		authToken:           *authToken,
		jwtSecret:           *jwtSecret,
		jwtIssuer:           *jwtIssuer,
		jwtAudience:         *jwtAudience,
		jwtUserClaim:        *jwtUserClaim,
		sessionCookieName:   *sessionCookieName,
		sessionCookieSecret: *sessionCookieSecret,
		trustedUserHeader:   *trustedUserHeader,
		trustedSecretHeader: *trustedSecretHeader,
		trustedSecret:       *trustedSecret,
	})
	limiter := buildRateLimiter(*rateLimitBackend, *redisURL, *rateLimit, time.Minute, *redisFailOpen)
	authService := buildAuthService(*enableUserSystem, storeConfig{
		backend:            *storeBackend,
		dataDir:            *dataDir,
		sqlDriver:          *sqlDriver,
		sqlDSN:             *sqlDSN,
		sqlDialect:         *sqlDialect,
		sqlMaxOpen:         *sqlMaxOpen,
		sqlMaxIdle:         *sqlMaxIdle,
		sqlConnMaxLifetime: *sqlConnMaxLifetime,
	}, authServiceConfig{
		jwtSecret:                 *jwtSecret,
		jwtIssuer:                 *jwtIssuer,
		jwtAudience:               *jwtAudience,
		accessTTL:                 *authAccessTTL,
		refreshTTL:                *authRefreshTTL,
		emailVerificationRequired: *emailVerificationRequired,
		emailVerificationTTL:      *emailVerificationTTL,
		emailProvider:             *emailProvider,
		emailFrom:                 *emailFrom,
		emailPublicBaseURL:        *emailPublicBaseURL,
		resendAPIKey:              *resendAPIKey,
		resendBaseURL:             *resendBaseURL,
	})
	artifactService := buildArtifactService(artifactConfig{
		store:       *artifactStore,
		dataDir:     *dataDir,
		sql:         storeConfig{backend: *storeBackend, sqlDriver: *sqlDriver, sqlDSN: *sqlDSN, sqlDialect: *sqlDialect, sqlMaxOpen: *sqlMaxOpen, sqlMaxIdle: *sqlMaxIdle, sqlConnMaxLifetime: *sqlConnMaxLifetime},
		s3Endpoint:  *artifactS3Endpoint,
		s3AccessKey: *artifactS3AccessKey,
		s3SecretKey: *artifactS3SecretKey,
		s3Bucket:    *artifactS3Bucket,
		s3Prefix:    *artifactS3Prefix,
		s3SSL:       *artifactS3SSL,
		maxBytes:    *assetMaxBytes,
	})
	runtime := agentruntime.NewRuntime(
		agentruntime.RuntimeConfig{
			DefaultWorkingDir:     *workspace,
			UserWorkspaceRoot:     *userWorkspaceRoot,
			AllowCustomWorkingDir: *allowCustomWorkingDir,
			TurnTimeout:           *turnTimeout,
			SkillShellTimeout:     *skillShellTimeout,
			SkillShellSandbox: agentruntime.SkillShellSandboxConfig{
				Runner:    *skillShellRunner,
				Image:     *skillSandboxImage,
				Network:   *skillSandboxNetwork,
				Memory:    *skillSandboxMemory,
				CPUs:      *skillSandboxCPUs,
				PidsLimit: *skillSandboxPidsLimit,
				TmpfsSize: *skillSandboxTmpfsSize,
			},
		},
		sessionStore,
		memoryService,
		skillCatalog,
		engineFactory,
	)
	runtime.SetMemoryExtractor(agentruntime.NewHybridMemoryExtractor(
		agentruntime.NewLLMMemoryExtractor(engineFactory),
		agentruntime.NewRuleMemoryExtractor(),
	))
	runtime.SetMemoryOrganizer(agentruntime.NewHybridMemoryOrganizer(
		agentruntime.NewLLMMemoryOrganizer(engineFactory),
		agentruntime.NewRuleMemoryOrganizer(),
	))
	runtime.SetArtifactService(artifactService)
	runtime.SetJobStore(jobStore)
	runtime.SetSkillExecutionStore(skillExecutionStore)
	riskScanner := agentruntime.NewBasicRiskScanner()
	runtime.SetRiskScanner(riskScanner)
	runtime.SetRiskRecorder(func(ctx context.Context, event agentruntime.RiskEvent) {
		if err := riskStore.RecordRiskEvent(ctx, event); err != nil {
			log.Printf("record risk event: %v", err)
		}
	})
	server := agentruntime.NewServer(
		runtime,
		auth,
		limiter,
		log.Default(),
	)
	server.SetAuthService(authService)
	server.SetAuditLogger(buildAuditLogger(storeCfg))
	server.SetRiskStore(riskStore)
	server.SetRiskScanner(riskScanner)
	server.SetOperationRateLimiter(agentruntime.NewOperationRateLimiter(parseOperationRateLimits(*operationRateLimits)))
	server.SetAdminToken(*adminToken)
	server.SetSkillRegistry(skillRegistrySetup.registry)
	server.SetLLMUsageStore(llmUsageStore)
	llmStatusFn := func() agentruntime.LLMGovernanceStatus {
		llmStatusMu.RLock()
		provider := llmStatusProvider
		llmStatusMu.RUnlock()
		if provider == nil {
			return agentruntime.LLMGovernanceStatus{
				Backends: []agentruntime.LLMBackendStatus{{
					Name:     llmCfg.Provider,
					Provider: llmCfg.Provider,
					Model:    llmCfg.Model,
					Healthy:  true,
				}},
				Config: map[string]any{"status": "planner has not been used yet"},
			}
		}
		return provider()
	}
	server.SetLLMStatusProvider(llmStatusFn)
	server.AddReadinessCheck("llm_config", llmConfigReadinessCheck(llmCfg))
	server.AddReadinessCheck("llm", agentruntime.LLMReadinessCheck(llmStatusFn))
	if strings.EqualFold(strings.TrimSpace(storeCfg.backend), "sql") {
		readyDB := openSQLDB(storeCfg)
		server.AddReadinessCheck("sql", readyDB.PingContext)
	}
	if strings.EqualFold(strings.TrimSpace(*rateLimitBackend), "redis") {
		server.AddReadinessCheck("redis", agentruntime.RedisReadinessCheck(limiter))
	}
	if artifactService != nil && artifactService.Objects != nil {
		server.AddReadinessCheck("object_store", agentruntime.ObjectStoreReadinessCheck(artifactService.Objects, "agentapi"))
	}
	if err := server.SetWebSecurity(agentruntime.WebSecurityConfig{
		CORSAllowedOrigins:   splitCSV(*corsAllowedOrigins),
		CORSAllowCredentials: *corsAllowCredentials,
		SessionCookieName:    *sessionCookieName,
		CSRFTokenCookieName:  *csrfCookieName,
		CSRFHeaderName:       *csrfHeaderName,
		CookieDomain:         *sessionCookieDomain,
		CookiePath:           "/",
		CookieSecure:         *sessionCookieSecure,
		CookieHTTPOnly:       true,
		CookieSameSite:       agentruntime.ParseSameSite(*sessionCookieSameSite),
		EnableCSRF:           *csrfEnabled,
	}); err != nil {
		log.Fatal(err)
	}
	runRetentionPrune(runtime, authService, *retentionDays)

	log.Printf("agent API listening on %s", *addr)
	log.Printf("data dir: %s", *dataDir)
	log.Printf("store backend: %s", *storeBackend)
	log.Printf("workspace: %s", *workspace)
	if strings.TrimSpace(*userWorkspaceRoot) != "" {
		log.Printf("user workspace root: %s", *userWorkspaceRoot)
	}
	log.Printf("llm provider: %s model: %s", llmCfg.Provider, llmCfg.Model)
	if strings.TrimSpace(*llmFallbacks) != "" {
		log.Printf("llm fallbacks: %s", *llmFallbacks)
	}
	if strings.TrimSpace(*llmModelRoutes) != "" {
		log.Printf("llm model routes: %s", *llmModelRoutes)
	}
	log.Printf("llm governance: attempts=%d chat_timeout=%s skill_timeout=%s daily_token_quota=%d daily_request_quota=%d daily_cost_quota_usd=%.4f", *llmMaxAttempts, *llmChatTimeout, *llmSkillTimeout, *llmDailyTokenQuota, *llmDailyRequestQuota, *llmDailyCostQuotaUSD)
	log.Printf("auth mode: %s", *authMode)
	log.Printf("admin API enabled: %t", strings.TrimSpace(*adminToken) != "")
	log.Printf("user system enabled: %t", authService != nil)
	log.Printf("rate limit backend: %s", *rateLimitBackend)
	log.Printf("operation rate limits enabled: %t", true)
	log.Printf("artifact store: %s", *artifactStore)
	log.Printf("asset max bytes: %d", *assetMaxBytes)
	if *retentionDays > 0 {
		log.Printf("retention days: %d", *retentionDays)
	}
	log.Printf("skill policy: %s published=%d", *skillPolicy, len(splitCSV(*publishedSkills)))
	log.Printf("dangerous tools enabled: %t", *allowDangerousTools)
	log.Printf("skill shell timeout: %s", *skillShellTimeout)
	log.Printf("skill shell runner: %s image=%s network=%s memory=%s cpus=%s pids=%d", *skillShellRunner, *skillSandboxImage, *skillSandboxNetwork, *skillSandboxMemory, *skillSandboxCPUs, *skillSandboxPidsLimit)
	if strings.TrimSpace(*networkAllowlist) == "" {
		log.Printf("network allowlist: disabled (all domains allowed)")
	} else {
		log.Printf("network allowlist: %s", *networkAllowlist)
	}
	log.Printf("cors allowed origins: %s", *corsAllowedOrigins)
	log.Printf("csrf enabled: %t", *csrfEnabled)
	if err := httpListenAndServe(*addr, server, *shutdownTimeout); err != nil {
		log.Fatal(err)
	}
}

var httpListenAndServe = func(addr string, handler *agentruntime.Server, shutdownTimeout time.Duration) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalCtx.Done():
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 30 * time.Second
	}
	log.Printf("shutdown signal received; draining active requests and jobs for up to %s", shutdownTimeout)
	if handler != nil {
		handler.BeginShutdown()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	runtimeErrCh := make(chan error, 1)
	go func() {
		if handler == nil {
			runtimeErrCh <- nil
			return
		}
		runtimeErrCh <- handler.Shutdown(shutdownCtx)
	}()
	httpErr := httpServer.Shutdown(shutdownCtx)
	runtimeErr := <-runtimeErrCh
	if errors.Is(httpErr, http.ErrServerClosed) {
		httpErr = nil
	}
	if httpErr != nil || runtimeErr != nil {
		return errors.Join(httpErr, runtimeErr)
	}
	log.Printf("graceful shutdown complete")
	return nil
}

func buildRegistry(root string, skillManager *skills.SkillManager, allowDangerous bool, artifactWriter agentruntime.ArtifactWriter, artifactMaxBytes int64, networkAllowlist []string, allowedTools []string) *tools.Registry {
	allowed := toolNameSet(allowedTools)
	enabled := func(name string) bool {
		return len(allowed) == 0 || allowed[name]
	}
	toolList := make([]tools.Tool, 0, 8)
	if enabled("Read") {
		toolList = append(toolList, filetool.NewReadTool(root))
	}
	if enabled("Glob") {
		toolList = append(toolList, searchtool.NewGlobTool(root))
	}
	if enabled("Grep") {
		toolList = append(toolList, searchtool.NewGrepTool(root))
	}
	if enabled("WebSearch") {
		toolList = append(toolList, webtool.NewSearchToolWithAllowlist(nil, networkAllowlist))
	}
	if enabled("WebFetch") {
		toolList = append(toolList, webtool.NewFetchToolWithAllowlist(nil, networkAllowlist))
	}
	if enabled("Skill") {
		toolList = append(toolList, skilltool.NewToolWithOptions(skillManager, skilltool.Options{
			DefaultDir:    root,
			RouteRunAsJob: true,
		}))
	}
	if artifactWriter != nil && enabled(agentruntime.ArtifactToolName) {
		toolList = append(toolList, agentruntime.NewArtifactToolWithLimit(artifactWriter, root, artifactMaxBytes))
	}
	if allowDangerous {
		if enabled("Write") {
			toolList = append(toolList, filetool.NewWriteTool(root))
		}
		if enabled("Edit") {
			toolList = append(toolList, filetool.NewEditTool(root))
		}
		if enabled("Bash") {
			toolList = append(toolList, bashtool.NewTool(root))
		}
	}
	return tools.NewRegistry(toolList...)
}

type llmConfig struct {
	Provider string
	Model    string
	APIKey   string
	Token    string
	BaseURL  string
	Timeout  int
}

func buildLLMConfig(providerName, model, apiKey, apiToken, apiBaseURL string, timeout int) (llmConfig, error) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if providerName == "" {
		providerName = "anthropic"
	}
	defaults, err := providerbackend.NewFactory().DefaultConfig(providerName)
	if err != nil {
		return llmConfig{}, err
	}
	cfg := llmConfig{
		Provider: defaults.Provider,
		Model:    firstNonEmpty(model, defaults.Model),
		BaseURL:  firstNonEmpty(apiBaseURL, providerEnvBaseURL(providerName), defaults.BaseURL),
		APIKey:   firstNonEmpty(apiKey, providerEnvAPIKey(providerName)),
		Token:    firstNonEmpty(apiToken, providerEnvToken(providerName)),
		Timeout:  timeout,
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaults.Timeout
	}
	if requiresCredential(cfg.Provider) && cfg.APIKey == "" && cfg.Token == "" && !providerHasAmbientCredential(cfg.Provider) {
		return llmConfig{}, fmt.Errorf("credential required for llm provider %q", cfg.Provider)
	}
	if isCustomProvider(providerName) && strings.TrimSpace(cfg.BaseURL) == "" {
		return llmConfig{}, fmt.Errorf("custom provider requires -api-base-url or AGENT_API_LLM_BASE_URL")
	}
	return cfg, nil
}

func newPlanner(cfg llmConfig) (engine.Planner, error) {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic", "claude":
		credential := firstNonEmpty(cfg.APIKey, cfg.Token)
		client := anthropic.NewClient(credential, cfg.BaseURL, time.Duration(cfg.Timeout)*time.Second)
		return anthropic.NewPlanner(client, cfg.Model), nil
	case "custom", "openai-compatible", "baseurl":
		provider, err := providerbackend.NewOpenAIProvider(providerbackend.Config{
			Provider: "openai",
			APIKey:   firstNonEmpty(cfg.APIKey, cfg.Token),
			BaseURL:  cfg.BaseURL,
			Model:    cfg.Model,
			Timeout:  cfg.Timeout,
		})
		if err != nil {
			return nil, err
		}
		return providerbackend.NewPlanner(provider, cfg.Model), nil
	default:
		provider, err := providerbackend.NewFactory().CreateProvider(providerbackend.Config{
			Provider: cfg.Provider,
			APIKey:   cfg.APIKey,
			Token:    cfg.Token,
			BaseURL:  cfg.BaseURL,
			Model:    cfg.Model,
			Timeout:  cfg.Timeout,
		})
		if err != nil {
			return nil, err
		}
		return providerbackend.NewPlanner(provider, cfg.Model), nil
	}
}

func newGovernedPlannerForScope(primary llmConfig, fallbackSpec, modelRoutes string, scope agentruntime.Scope, usageStore agentruntime.LLMUsageStore, governance agentruntime.LLMGovernanceConfig) (*agentruntime.GovernedPlanner, error) {
	primary.Model = routedModel(primary.Model, modelRoutes, scope)
	configs := []llmConfig{primary}
	for _, fallback := range parseLLMFallbacks(fallbackSpec, primary.Timeout) {
		if fallback.Model == "" {
			fallback.Model = primary.Model
		}
		configs = append(configs, fallback)
	}
	backends := make([]agentruntime.LLMBackend, 0, len(configs))
	for i, cfg := range configs {
		planner, err := newPlanner(cfg)
		if err != nil {
			return nil, err
		}
		name := cfg.Provider
		if i > 0 {
			name = fmt.Sprintf("%s-fallback-%d", cfg.Provider, i)
		}
		backends = append(backends, agentruntime.LLMBackend{
			Name:     name,
			Provider: cfg.Provider,
			Model:    cfg.Model,
			Planner:  planner,
		})
	}
	return agentruntime.NewGovernedPlanner(backends, usageStore, governance)
}

func parseLLMFallbacks(value string, timeout int) []llmConfig {
	specs := splitCSV(value)
	out := make([]llmConfig, 0, len(specs))
	for _, spec := range specs {
		parts := strings.SplitN(spec, ":", 2)
		providerName := strings.TrimSpace(parts[0])
		if providerName == "" {
			continue
		}
		model := ""
		if len(parts) == 2 {
			model = strings.TrimSpace(parts[1])
		}
		cfg, err := buildLLMConfig(providerName, model, "", "", "", timeout)
		if err != nil {
			log.Printf("warning: skipping llm fallback %q: %v", spec, err)
			continue
		}
		out = append(out, cfg)
	}
	return out
}

func routedModel(currentModel, routes string, scope agentruntime.Scope) string {
	routeMap := parseModelRoutes(routes)
	if scope.SkillName != "" {
		if model := routeMap["skill:"+scope.SkillName]; model != "" {
			return model
		}
	}
	if scope.SkillScoped {
		if model := routeMap["skill"]; model != "" {
			return model
		}
	}
	if model := routeMap["chat"]; model != "" && !scope.SkillScoped {
		return model
	}
	if model := routeMap["default"]; model != "" {
		return model
	}
	return currentModel
}

func parseModelRoutes(value string) map[string]string {
	out := make(map[string]string)
	for _, item := range splitCSV(value) {
		key, model, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		model = strings.TrimSpace(model)
		if key != "" && model != "" {
			out[key] = model
		}
	}
	return out
}

func providerEnvAPIKey(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "anthropic", "claude":
		return firstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("CLAUDE_API_KEY"))
	case "openai", "gpt", "custom", "openai-compatible", "baseurl":
		return firstNonEmpty(os.Getenv("OPENAI_API_KEY"), os.Getenv("AGENT_API_LLM_API_KEY"))
	case "qwen", "dashscope", "aliyun":
		return firstNonEmpty(os.Getenv("DASHSCOPE_API_KEY"), os.Getenv("QWEN_API_KEY"), os.Getenv("ALIBABA_CLOUD_API_KEY"), os.Getenv("AGENT_API_LLM_API_KEY"))
	case "gemini", "google":
		return firstNonEmpty(os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY"))
	case "vertex", "gcp":
		return ""
	default:
		return os.Getenv("AGENT_API_LLM_API_KEY")
	}
}

func providerEnvToken(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "vertex", "gcp":
		return firstNonEmpty(os.Getenv("VERTEX_ACCESS_TOKEN"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("GOOGLE_ACCESS_TOKEN"))
	default:
		return os.Getenv("AGENT_API_LLM_TOKEN")
	}
}

func providerHasAmbientCredential(providerName string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "vertex", "gcp":
		return googleauth.HasGoogleApplicationCredentialsEnv()
	default:
		return false
	}
}

func providerEnvBaseURL(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "anthropic", "claude":
		return os.Getenv("ANTHROPIC_BASE_URL")
	case "openai", "gpt":
		return os.Getenv("OPENAI_BASE_URL")
	case "qwen", "dashscope", "aliyun":
		return firstNonEmpty(os.Getenv("DASHSCOPE_BASE_URL"), os.Getenv("QWEN_BASE_URL"), os.Getenv("AGENT_API_LLM_BASE_URL"))
	case "gemini", "google":
		return os.Getenv("GEMINI_BASE_URL")
	case "vertex", "gcp":
		return os.Getenv("VERTEX_BASE_URL")
	case "custom", "openai-compatible", "baseurl":
		return firstNonEmpty(os.Getenv("AGENT_API_LLM_BASE_URL"), os.Getenv("OPENAI_BASE_URL"))
	default:
		return os.Getenv("AGENT_API_LLM_BASE_URL")
	}
}

func requiresCredential(providerName string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "simple":
		return false
	default:
		return true
	}
}

func isCustomProvider(providerName string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "custom", "openai-compatible", "baseurl":
		return true
	default:
		return false
	}
}

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
		dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		sessionStore := agentruntime.NewSQLSessionStoreWithDialect(db, dialect)
		memoryService := agentruntime.NewSQLMemoryServiceWithDialect(db, dialect)
		if err := sessionStore.Init(ctx); err != nil {
			log.Fatalf("init sql session store: %v", err)
		}
		if err := memoryService.Init(ctx); err != nil {
			log.Fatalf("init sql memory service: %v", err)
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

func buildSkillRegistrySetup(cfg storeConfig, skillManager *skills.SkillManager, published []string, allowAll bool) skillRegistrySetup {
	if !strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		return skillRegistrySetup{catalog: agentruntime.NewPublishedSkillCatalog(skillManager, published, allowAll)}
	}
	db := openSQLDB(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
	registry := agentruntime.NewSQLSkillRegistryWithDialect(db, dialect)
	if err := registry.Init(ctx); err != nil {
		log.Fatalf("init sql skill registry: %v", err)
	}
	if err := registry.SyncLoadedSkills(ctx, skillManager.ListSkills(), agentruntime.SkillPublicationPolicy{
		AllowAll: allowAll,
		Names:    published,
	}); err != nil {
		log.Fatalf("sync sql skill registry: %v", err)
	}
	records, err := registry.ListSkills(ctx)
	if err != nil {
		log.Fatalf("load sql skill registry: %v", err)
	}
	log.Printf("skill registry: sql records=%d published=%d", len(records), countPublishedSkillRecords(records))
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
		log.Printf("warning: failed to build published skill manager: %v", err)
	}
	return manager
}

func buildLLMUsageStore(cfg storeConfig) agentruntime.LLMUsageStore {
	if !strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		log.Printf("warning: LLM usage records are in-memory because store-backend is not sql")
		return agentruntime.NewMemoryLLMUsageStore()
	}
	db := openSQLDB(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
	store := agentruntime.NewSQLLLMUsageStoreWithDialect(db, dialect)
	if err := store.Init(ctx); err != nil {
		log.Fatalf("init sql llm usage store: %v", err)
	}
	return store
}

func buildSkillExecutionStore(cfg storeConfig) agentruntime.SkillExecutionStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLSkillExecutionStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			log.Fatalf("init sql skill execution store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemorySkillExecutionStore()
	if err := store.Init(ctx); err != nil {
		log.Fatalf("init memory skill execution store: %v", err)
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
		dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store = agentruntime.NewSQLJobStoreWithDialect(db, dialect)
	} else {
		store = agentruntime.NewMemoryJobStore()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Init(ctx); err != nil {
		log.Fatalf("init job store: %v", err)
	}
	return store
}

func buildAuditLogger(cfg storeConfig) agentruntime.AuditLogger {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		logger := agentruntime.NewSQLAuditLoggerWithDialect(db, dialect)
		if err := logger.Init(ctx); err != nil {
			log.Fatalf("init sql audit logger: %v", err)
		}
		return logger
	}
	logger := agentruntime.NewMemoryAuditLogger()
	if err := logger.Init(ctx); err != nil {
		log.Fatalf("init memory audit logger: %v", err)
	}
	return logger
}

func buildRiskStore(cfg storeConfig) agentruntime.RiskStore {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if strings.EqualFold(strings.TrimSpace(cfg.backend), "sql") {
		db := openSQLDB(cfg)
		dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sqlDialect, cfg.sqlDriver))
		store := agentruntime.NewSQLRiskStoreWithDialect(db, dialect)
		if err := store.Init(ctx); err != nil {
			log.Fatalf("init sql risk store: %v", err)
		}
		return store
	}
	store := agentruntime.NewMemoryRiskStore()
	if err := store.Init(ctx); err != nil {
		log.Fatalf("init memory risk store: %v", err)
	}
	return store
}

func parseOperationRateLimits(value string) map[string]agentruntime.OperationLimit {
	limits := map[string]agentruntime.OperationLimit{}
	for _, item := range splitCSV(value) {
		key, raw, ok := strings.Cut(item, "=")
		if !ok {
			key, raw, ok = strings.Cut(item, ":")
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if !ok || key == "" || raw == "" {
			continue
		}
		limit, window, ok := parseOperationRateLimit(raw)
		if !ok {
			continue
		}
		limits[key] = agentruntime.OperationLimit{Limit: limit, Window: window}
	}
	return limits
}

func parseOperationRateLimit(value string) (int, time.Duration, bool) {
	limitPart, windowPart, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok {
		return 0, 0, false
	}
	limit, err := strconv.Atoi(strings.TrimSpace(limitPart))
	if err != nil || limit <= 0 {
		return 0, 0, false
	}
	switch strings.ToLower(strings.TrimSpace(windowPart)) {
	case "s", "sec", "second":
		return limit, time.Second, true
	case "m", "min", "minute":
		return limit, time.Minute, true
	case "h", "hr", "hour":
		return limit, time.Hour, true
	case "d", "day":
		return limit, 24 * time.Hour, true
	default:
		duration, err := time.ParseDuration(strings.TrimSpace(windowPart))
		if err != nil || duration <= 0 {
			return 0, 0, false
		}
		return limit, duration, true
	}
}

type authConfig struct {
	mode                string
	userHeader          string
	authToken           string
	jwtSecret           string
	jwtIssuer           string
	jwtAudience         string
	jwtUserClaim        string
	sessionCookieName   string
	sessionCookieSecret string
	trustedUserHeader   string
	trustedSecretHeader string
	trustedSecret       string
}

func buildAuthenticator(cfg authConfig) agentruntime.Authenticator {
	mode := strings.ToLower(strings.TrimSpace(cfg.mode))
	jwt := agentruntime.JWTAuthenticator{
		Secret:    cfg.jwtSecret,
		UserClaim: cfg.jwtUserClaim,
		Issuer:    cfg.jwtIssuer,
		Audience:  cfg.jwtAudience,
	}
	switch mode {
	case "jwt":
		return jwt
	case "cookie", "session-cookie":
		return agentruntime.SessionCookieAuthenticator{
			CookieName: cfg.sessionCookieName,
			JWTAuthenticator: agentruntime.JWTAuthenticator{
				Secret:    firstNonEmpty(cfg.sessionCookieSecret, cfg.jwtSecret),
				UserClaim: cfg.jwtUserClaim,
				Issuer:    cfg.jwtIssuer,
				Audience:  cfg.jwtAudience,
			},
		}
	case "trusted-header", "gateway":
		return agentruntime.TrustedHeaderAuthenticator{
			UserHeader:     cfg.trustedUserHeader,
			RequiredHeader: cfg.trustedSecretHeader,
			RequiredValue:  cfg.trustedSecret,
		}
	case "header":
		return agentruntime.HeaderAuthenticator{UserHeader: cfg.userHeader, BearerToken: cfg.authToken}
	case "none":
		return agentruntime.HeaderAuthenticator{UserHeader: cfg.userHeader}
	default:
		var chain agentruntime.CompositeAuthenticator
		if cfg.jwtSecret != "" {
			chain = append(chain, jwt)
		}
		if cfg.sessionCookieSecret != "" {
			chain = append(chain, agentruntime.SessionCookieAuthenticator{
				CookieName:       cfg.sessionCookieName,
				JWTAuthenticator: agentruntime.JWTAuthenticator{Secret: cfg.sessionCookieSecret, UserClaim: cfg.jwtUserClaim, Issuer: cfg.jwtIssuer, Audience: cfg.jwtAudience},
			})
		}
		if cfg.trustedSecretHeader != "" && cfg.trustedSecret != "" {
			chain = append(chain, agentruntime.TrustedHeaderAuthenticator{
				UserHeader:     cfg.trustedUserHeader,
				RequiredHeader: cfg.trustedSecretHeader,
				RequiredValue:  cfg.trustedSecret,
			})
		}
		chain = append(chain, agentruntime.HeaderAuthenticator{UserHeader: cfg.userHeader, BearerToken: cfg.authToken})
		return chain
	}
}

func buildRateLimiter(backend, redisURL string, limit int, window time.Duration, redisFailOpen bool) agentruntime.RateLimitPolicy {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "redis":
		limiter, err := agentruntime.NewRedisRateLimiter(redisURL, limit, window, redisFailOpen)
		if err != nil {
			log.Fatalf("init redis rate limiter: %v", err)
		}
		return limiter
	case "gateway", "none", "off", "disabled":
		return agentruntime.NoopRateLimiter{}
	default:
		return agentruntime.NewRateLimiter(limit, window)
	}
}

func llmConfigReadinessCheck(cfg llmConfig) func(context.Context) error {
	return func(context.Context) error {
		provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
		if provider == "" {
			return fmt.Errorf("llm provider is required")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return fmt.Errorf("llm model is required")
		}
		if isCustomProvider(provider) && strings.TrimSpace(cfg.BaseURL) == "" {
			return fmt.Errorf("custom llm provider requires base URL")
		}
		if requiresCredential(provider) && strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(cfg.Token) == "" && !providerHasAmbientCredential(provider) {
			return fmt.Errorf("llm credential is required for provider %q", provider)
		}
		switch provider {
		case "vertex", "gcp":
			if strings.TrimSpace(cfg.Token) == "" && strings.TrimSpace(cfg.APIKey) == "" && !googleauth.HasGoogleApplicationCredentialsEnv() {
				return fmt.Errorf("vertex credential is required; set GOOGLE_APPLICATION_CREDENTIALS, GOOGLE_APPLICATION_CREDENTIALS_JSON, or VERTEX_ACCESS_TOKEN")
			}
			if !strings.Contains(strings.TrimSpace(cfg.Model), "/") && firstNonEmpty(os.Getenv("VERTEX_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCLOUD_PROJECT")) == "" {
				return fmt.Errorf("vertex project ID is required for short model names")
			}
		}
		return nil
	}
}

type artifactConfig struct {
	store       string
	dataDir     string
	sql         storeConfig
	s3Endpoint  string
	s3AccessKey string
	s3SecretKey string
	s3Bucket    string
	s3Prefix    string
	s3SSL       bool
	maxBytes    int64
}

func buildArtifactService(cfg artifactConfig) *agentruntime.ArtifactService {
	if !strings.EqualFold(strings.TrimSpace(cfg.sql.backend), "sql") {
		log.Printf("warning: artifact metadata requires sql store; artifacts disabled")
		return nil
	}
	db := openSQLDB(cfg.sql)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(firstNonEmpty(cfg.sql.sqlDialect, cfg.sql.sqlDriver))
	meta := agentruntime.NewSQLArtifactStoreWithDialect(db, dialect)
	if err := meta.Init(ctx); err != nil {
		log.Fatalf("init sql artifact store: %v", err)
	}
	var objects agentruntime.ObjectStore
	switch strings.ToLower(strings.TrimSpace(cfg.store)) {
	case "s3", "minio":
		store, err := agentruntime.NewS3ObjectStore(ctx, agentruntime.S3ObjectStoreConfig{
			Endpoint:        cfg.s3Endpoint,
			AccessKeyID:     cfg.s3AccessKey,
			SecretAccessKey: cfg.s3SecretKey,
			Bucket:          cfg.s3Bucket,
			Prefix:          cfg.s3Prefix,
			UseSSL:          cfg.s3SSL,
		})
		if err != nil {
			log.Fatalf("init artifact s3 store: %v", err)
		}
		objects = store
	default:
		objects = agentruntime.NewFileObjectStore(filepath.Join(cfg.dataDir, "artifacts"))
	}
	return agentruntime.NewArtifactServiceWithPolicy(meta, objects, "", agentruntime.AssetPolicy{MaxBytes: cfg.maxBytes})
}

func runRetentionPrune(runtime *agentruntime.Runtime, authService *agentruntime.AuthService, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	counts, err := runtime.PruneBefore(ctx, cutoff)
	if err != nil {
		log.Printf("warning: retention prune failed: %v", err)
	} else {
		log.Printf("retention prune complete: sessions=%d memories=%d", counts["sessions"], counts["memories"])
	}
	if authService != nil {
		count, err := authService.PruneExpiredRefreshTokens(ctx, cutoff)
		if err != nil {
			log.Printf("warning: refresh token prune failed: %v", err)
		} else {
			log.Printf("refresh token prune complete: tokens=%d", count)
		}
	}
}

type authServiceConfig struct {
	jwtSecret                 string
	jwtIssuer                 string
	jwtAudience               string
	accessTTL                 time.Duration
	refreshTTL                time.Duration
	emailVerificationRequired bool
	emailVerificationTTL      time.Duration
	emailProvider             string
	emailFrom                 string
	emailPublicBaseURL        string
	resendAPIKey              string
	resendBaseURL             string
}

func buildAuthService(enabled bool, storeCfg storeConfig, authCfg authServiceConfig) *agentruntime.AuthService {
	if !enabled {
		return nil
	}
	if strings.TrimSpace(authCfg.jwtSecret) == "" {
		log.Fatal("enable-user-system requires -jwt-secret or AGENT_API_JWT_SECRET")
	}
	if !strings.EqualFold(strings.TrimSpace(storeCfg.backend), "sql") {
		log.Fatal("enable-user-system currently requires -store-backend sql")
	}
	db := openSQLDB(storeCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialect := agentruntime.ParseSQLDialect(firstNonEmpty(storeCfg.sqlDialect, storeCfg.sqlDriver))
	store := agentruntime.NewSQLUserStoreWithDialect(db, dialect)
	if err := store.Init(ctx); err != nil {
		log.Fatalf("init sql user store: %v", err)
	}
	if authCfg.emailVerificationRequired && strings.TrimSpace(authCfg.emailProvider) == "" {
		log.Fatal("email verification requires -email-provider or AGENT_API_EMAIL_PROVIDER")
	}
	return &agentruntime.AuthService{
		Store:                     store,
		JWTSecret:                 authCfg.jwtSecret,
		Issuer:                    authCfg.jwtIssuer,
		Audience:                  authCfg.jwtAudience,
		AccessTTL:                 authCfg.accessTTL,
		RefreshTTL:                authCfg.refreshTTL,
		EmailVerificationRequired: authCfg.emailVerificationRequired,
		EmailVerificationTTL:      authCfg.emailVerificationTTL,
		PublicBaseURL:             authCfg.emailPublicBaseURL,
		Mailer:                    buildMailer(authCfg),
	}
}

func buildMailer(authCfg authServiceConfig) agentruntime.Mailer {
	switch strings.ToLower(strings.TrimSpace(authCfg.emailProvider)) {
	case "":
		return nil
	case "resend":
		return agentruntime.ResendMailer{
			APIKey:  authCfg.resendAPIKey,
			From:    authCfg.emailFrom,
			BaseURL: authCfg.resendBaseURL,
		}
	default:
		log.Fatalf("unsupported email provider %q", authCfg.emailProvider)
		return nil
	}
}

func openSQLDB(cfg storeConfig) *sql.DB {
	if strings.TrimSpace(cfg.sqlDriver) == "" || strings.TrimSpace(cfg.sqlDSN) == "" {
		log.Fatal("store-backend=sql requires -sql-driver and -sql-dsn")
	}
	db, err := sql.Open(cfg.sqlDriver, cfg.sqlDSN)
	if err != nil {
		log.Fatalf("open sql store: %v", err)
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
	return db
}

func allowedToolNames(allowDangerous bool) []string {
	names := []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch", "Skill", agentruntime.ArtifactToolName}
	if allowDangerous {
		names = append(names, "Write", "Edit", "Bash")
	}
	return names
}

func consumerChatToolNames() []string {
	return []string{"WebSearch", "WebFetch", "Skill"}
}

func effectiveAllowedToolNames(global []string, scope agentruntime.Scope) []string {
	if scope.SkillScoped {
		if len(cleanCSVValues(scope.AllowedTools)) == 0 {
			return []string{"__no_tools_allowed__"}
		}
		return scopedAllowedTools(global, scope.AllowedTools)
	}
	return scopedAllowedTools(global, consumerChatToolNames())
}

func toolNameSet(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func scopedAllowedTools(global, scoped []string) []string {
	if len(scoped) == 0 {
		return global
	}
	globalSet := make(map[string]bool, len(global))
	for _, name := range global {
		globalSet[name] = true
	}
	out := make([]string, 0, len(scoped))
	for _, name := range scoped {
		if globalSet[name] {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return []string{"__no_tools_allowed__"}
	}
	return out
}

func scopedNetworkAllowlist(global, scoped []string) []string {
	scoped = cleanCSVValues(scoped)
	if len(scoped) == 0 {
		return global
	}
	global = cleanCSVValues(global)
	if len(global) == 0 {
		return scoped
	}
	globalSet := make(map[string]bool, len(global))
	for _, name := range global {
		globalSet[strings.ToLower(name)] = true
	}
	out := make([]string, 0, len(scoped))
	for _, name := range scoped {
		if globalSet[strings.ToLower(name)] {
			out = append(out, name)
		}
	}
	return out
}

func cleanCSVValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
	}
	return out
}

func loadSkills(skillDirs []string) *skills.SkillManager {
	manager := skills.NewSkillManager()
	if err := manager.LoadBundledSkills(); err != nil {
		log.Printf("warning: failed to load bundled skills: %v", err)
	}
	for _, dir := range skillDirs {
		dir = strings.TrimSpace(os.ExpandEnv(dir))
		if dir == "" {
			continue
		}
		if err := manager.LoadSkillsFromDirectory(dir, skills.SourceFile); err != nil {
			log.Printf("warning: failed to load skills from %s: %v", dir, err)
		}
	}
	stats := manager.GetStats()
	log.Printf("skills loaded: total=%d bundled=%d dynamic=%d user_invocable=%d", stats.TotalSkills, stats.BundledSkills, stats.DynamicSkills, stats.UserInvocable)
	return manager
}

func defaultDataDir() string {
	if value := strings.TrimSpace(os.Getenv("AGENT_API_DATA_DIR")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".agentapi"
	}
	return filepath.Join(home, ".claude-codex", "agentapi")
}

func mustWorkingDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	var out int
	if _, err := fmt.Sscanf(value, "%d", &out); err != nil {
		return fallback
	}
	return out
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envFloat64(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	out, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return out
}

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	appconfig "claude-codex/internal/app/config"
	"claude-codex/internal/backend/agentruntime"
	"github.com/spf13/cobra"
)

// Config contains agentapi startup configuration after environment defaults and CLI overrides are applied.
type Config struct {
	Addr                                     string
	DataDir                                  string
	StoreBackend                             string
	ObjectBaseURL                            string
	ObjectToken                              string
	ObjectTimeout                            time.Duration
	ArtifactStore                            string
	ArtifactS3Endpoint                       string
	ArtifactS3AccessKey                      string
	ArtifactS3SecretKey                      string
	ArtifactS3Bucket                         string
	ArtifactS3Prefix                         string
	ArtifactS3SSL                            bool
	AssetMaxBytes                            int64
	SQLDriver                                string
	SQLDSN                                   string
	SQLDialect                               string
	SQLMaxOpen                               int
	SQLMaxIdle                               int
	SQLConnMaxLifetime                       time.Duration
	MessageSearchBackend                     string
	MessageSearchEndpoint                    string
	MessageSearchIndex                       string
	MessageSearchAPIKey                      string
	MessageSearchUsername                    string
	MessageSearchPassword                    string
	MessageSearchTimeout                     time.Duration
	MessageSearchIndexManagementEnabled      bool
	MessageSearchIndexLifecyclePolicy        string
	MessageSearchIndexTemplate               string
	MessageSearchIndexWriteAlias             string
	MessageSearchIndexAnalyzer               string
	MessageSearchIndexSearchAnalyzer         string
	MessageSearchIndexDowngradeAfter         time.Duration
	MessageSearchIndexCloseAfter             time.Duration
	MessageSearchIndexMaintenanceInterval    time.Duration
	MessageSearchIndexMaintenanceBatchLimit  int
	MessageSearchQdrantEndpoint              string
	MessageSearchQdrantCollection            string
	MessageSearchQdrantAPIKey                string
	MessageSearchQdrantScoreThreshold        float64
	MessageSearchEmbeddingProvider           string
	MessageSearchEmbeddingEndpoint           string
	MessageSearchEmbeddingAPIKey             string
	MessageSearchEmbeddingAccessToken        string
	MessageSearchEmbeddingModel              string
	MessageSearchEmbeddingDimensions         int
	MessageSearchEmbeddingProjectID          string
	MessageSearchEmbeddingLocation           string
	MessageSearchEmbeddingTaskType           string
	MessageSearchEmbeddingIndexTaskType      string
	MessageSearchEmbeddingAutoTruncate       bool
	MessageSearchRRFK                        int
	MessageSearchQueryRewriteEnabled         bool
	MessageSearchDynamicTopKEnabled          bool
	MessageSearchMinRecallWindow             int
	MessageSearchMaxRecallWindow             int
	MessageSearchMultiTurnEnabled            bool
	MessageSearchRerankEnabled               bool
	MessageSearchRerankCandidateLimit        int
	MessageSearchLowConfidenceScore          float64
	MemoryVectorEnabled                      bool
	MemoryVectorQdrantEndpoint               string
	MemoryVectorQdrantCollection             string
	MemoryVectorEpisodeQdrantCollection      string
	MemoryVectorQdrantAPIKey                 string
	MemoryVectorQdrantScoreThreshold         float64
	MemoryVectorEmbeddingProvider            string
	MemoryVectorEmbeddingEndpoint            string
	MemoryVectorEmbeddingAPIKey              string
	MemoryVectorEmbeddingAccessToken         string
	MemoryVectorEmbeddingModel               string
	MemoryVectorEmbeddingDimensions          int
	MemoryVectorEmbeddingProjectID           string
	MemoryVectorEmbeddingLocation            string
	MemoryVectorEmbeddingTaskType            string
	MemoryVectorEmbeddingIndexTaskType       string
	MemoryVectorEmbeddingAutoTruncate        bool
	MemoryVectorRRFK                         int
	MemoryVectorRerankEnabled                bool
	MemoryVectorRerankEndpoint               string
	MemoryVectorRerankAPIKey                 string
	MemoryVectorRerankModel                  string
	MemoryVectorRerankCandidateLimit         int
	MemoryVectorRerankResultLimit            int
	MemoryVectorRerankTimeout                time.Duration
	MemoryVectorRerankTruncate               string
	MemoryRecallEnabled                      bool
	MemoryRecallConditionalEnabled           bool
	MemoryRecallAsyncEnabled                 bool
	MemoryRecallTimeout                      time.Duration
	MemoryRecallMinQueryRunes                int
	MemoryRecallRecentContextMessages        int
	MemoryRecallRecentContextMaxRunes        int
	MemoryRecallForceInterval                int
	MemoryRecallComplexTokenThreshold        int
	MemoryRecallEmbeddingEnabled             bool
	MemoryRecallEmbeddingSimilarityThreshold float64
	MemoryRecallEmbeddingWindow              int
	MemoryRecallIntentClassifierEnabled      bool
	MemoryRecallIntentClassifierThreshold    float64
	MemoryRecallIntentClassifierContextTurns int
	MemoryRecallQueryRewriteEnabled          bool
	MemoryRecallLLMTriggerEnabled            bool
	MemoryRecallLLMTriggerTimeout            time.Duration
	MemoryPolicyPath                         string
	MemoryPolicyVersion                      string
	MemoryPolicyReloadInterval               time.Duration
	MemoryPolicyStrictEval                   bool
	EpisodicMemoryEnabled                    bool
	EpisodicMemoryCaptureEnabled             bool
	EpisodicMemoryContextEnabled             bool
	EpisodicMemoryMinMessages                int
	EpisodicMemoryMaxMessages                int
	EpisodicMemoryInjectLimit                int
	EpisodicMemoryTTL                        time.Duration
	EpisodicMemorySummarizeTimeout           time.Duration
	Workspace                                string
	UserWorkspaceRoot                        string
	AllowCustomWorkingDir                    bool
	Timezone                                 string
	Locale                                   string
	LLMProvider                              string
	APIKey                                   string
	APIToken                                 string
	APIBaseURL                               string
	Model                                    string
	LLMFallbacks                             string
	LLMModelRoutes                           string
	LLMMaxAttempts                           int
	LLMRetryBackoff                          time.Duration
	LLMChatTimeout                           time.Duration
	LLMSkillTimeout                          time.Duration
	LLMDailyTokenQuota                       int
	LLMDailyRequestQuota                     int
	LLMDailyCostQuotaUSD                     float64
	LLMInputCostPerMillion                   float64
	LLMOutputCostPerMillion                  float64
	LLMFailureThreshold                      int
	LLMCircuitCooldown                       time.Duration
	LiveEnabled                              bool
	LiveProvider                             string
	LiveModel                                string
	LiveVertexProjectID                      string
	LiveVertexLocation                       string
	LiveVertexBaseURL                        string
	LiveVertexAPIVersion                     string
	LiveXAIAPIKey                            string
	LiveXAIBaseURL                           string
	LiveInputAudioMIME                       string
	LiveOutputAudioMIME                      string
	LiveVoiceName                            string
	LiveLanguageCode                         string
	LiveInputTranscription                   bool
	LiveOutputTranscription                  bool
	LiveVADStartSensitivity                  string
	LiveVADEndSensitivity                    string
	LiveVADThreshold                         float64
	LiveVADPrefixPadding                     time.Duration
	LiveVADSilenceDuration                   time.Duration
	LiveSessionTimeout                       time.Duration
	LiveSetupPromptCacheBackend              string
	LiveSetupPromptCacheRedisURL             string
	LiveSetupPromptCacheTTL                  time.Duration
	AuthMode                                 string
	AuthToken                                string
	UserHeader                               string
	JWTSecret                                string
	JWTIssuer                                string
	JWTAudience                              string
	JWTUserClaim                             string
	EnableUserSystem                         bool
	AuthAccessTTL                            time.Duration
	AuthRefreshTTL                           time.Duration
	EmailVerificationRequired                bool
	EmailVerificationTTL                     time.Duration
	EmailProvider                            string
	EmailFrom                                string
	EmailPublicBaseURL                       string
	ResendAPIKey                             string
	ResendBaseURL                            string
	SessionCookieName                        string
	SessionCookieSecret                      string
	SessionCookieDomain                      string
	SessionCookieSecure                      bool
	SessionCookieSameSite                    string
	CSRFEnabled                              bool
	CSRFCookieName                           string
	CSRFHeaderName                           string
	CORSAllowedOrigins                       string
	CORSAllowCredentials                     bool
	AdminToken                               string
	EvalDailyEnabled                         bool
	EvalDailyHour                            int
	EvalDailyMinute                          int
	EvalDailyUserIDs                         string
	EvalDailyBatchLimit                      int
	EvalDailyTimeout                         time.Duration
	EvalJudgeEnabled                         bool
	EvalJudgeModel                           string
	EvalJudgePromptVersion                   string
	TrustedUserHeader                        string
	TrustedSecretHeader                      string
	TrustedSecret                            string
	AllowDangerousTools                      bool
	NetworkAllowlist                         string
	PluginDir                                string
	SkillDirs                                string
	MCPServersJSON                           string
	MCPServers                               []appconfig.MCPServerConfig
	RateLimitBackend                         string
	RateLimit                                int
	OperationRateLimits                      string
	CacheBackend                             string
	CacheRedisURL                            string
	CachePrefix                              string
	CacheDefaultTTL                          time.Duration
	CacheFailOpen                            bool
	RedisURL                                 string
	RedisFailOpen                            bool
	MessageContextCacheBackend               string
	MessageContextCacheRedisURL              string
	MessageContextCacheTTL                   time.Duration
	SessionListCacheBackend                  string
	SessionListCacheRedisURL                 string
	SessionListCacheTTL                      time.Duration
	MessageSequenceBackend                   string
	MessageSequenceRedisURL                  string
	MessageEventsBackend                     string
	MessageEventsKafkaBrokers                string
	MessageEventsKafkaTopic                  string
	MessageEventsKafkaClientID               string
	MessageEventsKafkaConsumerEnabled        bool
	MessageEventsKafkaConsumerGroup          string
	MessageEventsKafkaDLQTopic               string
	MessageEventsKafkaRetryAttempts          int
	MessageEventsKafkaRetryBackoff           time.Duration
	MessageEventsKafkaProcessTimeout         time.Duration
	MessageEventsProcessedLockBackend        string
	MessageEventsProcessedLockRedisURL       string
	MessageEventsProcessedLockTTL            time.Duration
	JobQueueRedisURL                         string
	JobQueueStream                           string
	JobQueueConsumerGroup                    string
	JobQueueConsumer                         string
	JobQueueBlockTimeout                     time.Duration
	JobQueueClaimIdle                        time.Duration
	JobQueueLockTTL                          time.Duration
	JobWorkerEnabled                         bool
	JobEventStreamEnabled                    bool
	JobEventStreamPrefix                     string
	JobEventStreamTTL                        time.Duration
	JobEventStreamMaxLen                     int
	ChatEventStreamEnabled                   bool
	ChatEventStreamPrefix                    string
	ChatEventStreamTTL                       time.Duration
	ChatEventStreamMaxLen                    int
	ChatEventStreamBlock                     time.Duration
	JobEventFanoutEnabled                    bool
	JobEventFanoutChannel                    string
	JobEventFanoutOrigin                     string
	WebPushVAPIDPublicKey                    string
	WebPushVAPIDPrivateKey                   string
	WebPushVAPIDSubject                      string
	WebPushTTLSeconds                        int
	MessageAttachmentWorkerEnabled           bool
	MessageAttachmentWorkerBatchSize         int
	MessageAttachmentWorkerPollInterval      time.Duration
	MessageAttachmentWorkerProcessTimeout    time.Duration
	MessageAttachmentThumbnailMaxDimension   int
	MessageArchiveWorkerEnabled              bool
	MessageArchiveAfter                      time.Duration
	MessageArchiveWorkerBatchSize            int
	MessageArchiveWorkerPollInterval         time.Duration
	MessageArchiveWorkerProcessTimeout       time.Duration
	MessageArchivePrefix                     string
	MessageArchiveClearPGPayload             bool
	RetentionDays                            int
	LocalArtifactStagingRetention            time.Duration
	ShutdownTimeout                          time.Duration
	RequestTimeout                           time.Duration
	TurnTimeout                              time.Duration
	DeepAgentV2Enabled                       bool
	DeepAgentV2ShadowRoute                   bool
	DeepResearchOrchestratorWorkerEnabled    bool
	DeepResearchWorkerBackend                string
	DeepResearchMaxWorkers                   int
	DeepResearchMaxConcurrency               int
	DeepResearchWorkerTimeout                time.Duration
	DeepResearchTotalTimeout                 time.Duration
	DeepResearchMaxRetries                   int
	DeepResearchReplanEnabled                bool
	DeepResearchMaxReplans                   int
	DeepResearchReplanEveryBatches           int
	DeepResearchFallbackLegacy               bool
	DeepResearchRequireSources               bool
	DeepResearchMinSuccessfulWorkers         int
	LoopAutomationEnabled                    bool
	LoopScheduleTriggersEnabled              bool
	LoopWebhookTriggersEnabled               bool
	LoopMonitorTriggersEnabled               bool
	LoopEvalRepairTriggersEnabled            bool
	LoopConnectorTriggersEnabled             bool
	LoopTriggerTTL                           time.Duration
	SkillShellTimeout                        time.Duration
	SkillShellRunner                         string
	SkillSandboxImage                        string
	SkillSandboxNetwork                      string
	SkillSandboxMemory                       string
	SkillSandboxCPUs                         string
	SkillSandboxPidsLimit                    int
	SkillSandboxTmpfsSize                    string
	SkillSandboxPrepullImages                string
	SkillSandboxWarmPoolSize                 int
}

// Default returns startup configuration populated from process environment fallbacks.
func Default() Config {
	return Config{
		Addr:                                     ":8081",
		DataDir:                                  DefaultDataDir(),
		StoreBackend:                             FirstNonEmpty(os.Getenv("AGENT_API_STORE_BACKEND"), "file"),
		ObjectBaseURL:                            os.Getenv("AGENT_API_OBJECT_BASE_URL"),
		ObjectToken:                              os.Getenv("AGENT_API_OBJECT_TOKEN"),
		ObjectTimeout:                            EnvDuration("AGENT_API_OBJECT_TIMEOUT", 10*time.Second),
		ArtifactStore:                            FirstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_STORE"), "file"),
		ArtifactS3Endpoint:                       FirstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_ENDPOINT"), "localhost:9000"),
		ArtifactS3AccessKey:                      FirstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_ACCESS_KEY"), "minioadmin"),
		ArtifactS3SecretKey:                      FirstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_SECRET_KEY"), "minioadmin"),
		ArtifactS3Bucket:                         FirstNonEmpty(os.Getenv("AGENT_API_ARTIFACT_S3_BUCKET"), "agentapi"),
		ArtifactS3Prefix:                         os.Getenv("AGENT_API_ARTIFACT_S3_PREFIX"),
		ArtifactS3SSL:                            EnvBool("AGENT_API_ARTIFACT_S3_SSL", false),
		AssetMaxBytes:                            EnvInt64("AGENT_API_ASSET_MAX_BYTES", agentruntime.DefaultMaxAssetBytes),
		SQLDriver:                                os.Getenv("AGENT_API_SQL_DRIVER"),
		SQLDSN:                                   os.Getenv("AGENT_API_SQL_DSN"),
		SQLDialect:                               os.Getenv("AGENT_API_SQL_DIALECT"),
		SQLMaxOpen:                               EnvInt("AGENT_API_SQL_MAX_OPEN_CONNS", 20),
		SQLMaxIdle:                               EnvInt("AGENT_API_SQL_MAX_IDLE_CONNS", 10),
		SQLConnMaxLifetime:                       EnvDuration("AGENT_API_SQL_CONN_MAX_LIFETIME", 30*time.Minute),
		MessageSearchBackend:                     FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_BACKEND"), "sql"),
		MessageSearchEndpoint:                    FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_ENDPOINT"), os.Getenv("AGENT_API_MESSAGE_SEARCH_URL")),
		MessageSearchIndex:                       FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_INDEX"), "agent_messages"),
		MessageSearchAPIKey:                      os.Getenv("AGENT_API_MESSAGE_SEARCH_API_KEY"),
		MessageSearchUsername:                    os.Getenv("AGENT_API_MESSAGE_SEARCH_USERNAME"),
		MessageSearchPassword:                    os.Getenv("AGENT_API_MESSAGE_SEARCH_PASSWORD"),
		MessageSearchTimeout:                     EnvDuration("AGENT_API_MESSAGE_SEARCH_TIMEOUT", 5*time.Second),
		MessageSearchIndexManagementEnabled:      EnvBool("AGENT_API_MESSAGE_SEARCH_INDEX_MANAGEMENT_ENABLED", true),
		MessageSearchIndexLifecyclePolicy:        os.Getenv("AGENT_API_MESSAGE_SEARCH_INDEX_LIFECYCLE_POLICY"),
		MessageSearchIndexTemplate:               os.Getenv("AGENT_API_MESSAGE_SEARCH_INDEX_TEMPLATE"),
		MessageSearchIndexWriteAlias:             os.Getenv("AGENT_API_MESSAGE_SEARCH_INDEX_WRITE_ALIAS"),
		MessageSearchIndexAnalyzer:               FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_INDEX_ANALYZER"), "ik_max_word"),
		MessageSearchIndexSearchAnalyzer:         FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_INDEX_SEARCH_ANALYZER"), "ik_smart"),
		MessageSearchIndexDowngradeAfter:         EnvDuration("AGENT_API_MESSAGE_SEARCH_INDEX_DOWNGRADE_AFTER", 90*24*time.Hour),
		MessageSearchIndexCloseAfter:             EnvDuration("AGENT_API_MESSAGE_SEARCH_INDEX_CLOSE_AFTER", 180*24*time.Hour),
		MessageSearchIndexMaintenanceInterval:    EnvDuration("AGENT_API_MESSAGE_SEARCH_INDEX_MAINTENANCE_INTERVAL", 24*time.Hour),
		MessageSearchIndexMaintenanceBatchLimit:  EnvInt("AGENT_API_MESSAGE_SEARCH_INDEX_MAINTENANCE_BATCH_LIMIT", 50),
		MessageSearchQdrantEndpoint:              FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_QDRANT_ENDPOINT"), os.Getenv("AGENT_API_QDRANT_ENDPOINT")),
		MessageSearchQdrantCollection:            FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_QDRANT_COLLECTION"), "agent_messages"),
		MessageSearchQdrantAPIKey:                FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_QDRANT_API_KEY"), os.Getenv("AGENT_API_QDRANT_API_KEY")),
		MessageSearchQdrantScoreThreshold:        EnvFloat64("AGENT_API_MESSAGE_SEARCH_QDRANT_SCORE_THRESHOLD", 0),
		MessageSearchEmbeddingProvider:           FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROVIDER"), os.Getenv("AGENT_API_EMBEDDING_PROVIDER")),
		MessageSearchEmbeddingEndpoint:           FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_ENDPOINT"), os.Getenv("AGENT_API_EMBEDDING_ENDPOINT")),
		MessageSearchEmbeddingAPIKey:             FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_API_KEY"), os.Getenv("AGENT_API_EMBEDDING_API_KEY"), os.Getenv("NVIDIA_API_KEY"), os.Getenv("NGC_API_KEY"), os.Getenv("OPENAI_API_KEY")),
		MessageSearchEmbeddingAccessToken:        FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_TOKEN"), os.Getenv("VERTEX_ACCESS_TOKEN"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("GOOGLE_ACCESS_TOKEN")),
		MessageSearchEmbeddingModel:              os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_MODEL"),
		MessageSearchEmbeddingDimensions:         EnvInt("AGENT_API_MESSAGE_SEARCH_EMBEDDING_DIMENSIONS", 0),
		MessageSearchEmbeddingProjectID:          FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROJECT_ID"), os.Getenv("VERTEX_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCLOUD_PROJECT")),
		MessageSearchEmbeddingLocation:           FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_LOCATION"), os.Getenv("VERTEX_LOCATION"), os.Getenv("GOOGLE_CLOUD_LOCATION"), os.Getenv("CLOUD_ML_REGION"), "global"),
		MessageSearchEmbeddingTaskType:           FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_TASK_TYPE"), "RETRIEVAL_QUERY"),
		MessageSearchEmbeddingIndexTaskType:      FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_INDEX_TASK_TYPE"), "RETRIEVAL_DOCUMENT"),
		MessageSearchEmbeddingAutoTruncate:       EnvBool("AGENT_API_MESSAGE_SEARCH_EMBEDDING_AUTO_TRUNCATE", true),
		MessageSearchRRFK:                        EnvInt("AGENT_API_MESSAGE_SEARCH_RRF_K", 60),
		MessageSearchQueryRewriteEnabled:         EnvBool("AGENT_API_MESSAGE_SEARCH_QUERY_REWRITE_ENABLED", true),
		MessageSearchDynamicTopKEnabled:          EnvBool("AGENT_API_MESSAGE_SEARCH_DYNAMIC_TOPK_ENABLED", true),
		MessageSearchMinRecallWindow:             EnvInt("AGENT_API_MESSAGE_SEARCH_MIN_RECALL_WINDOW", 50),
		MessageSearchMaxRecallWindow:             EnvInt("AGENT_API_MESSAGE_SEARCH_MAX_RECALL_WINDOW", 120),
		MessageSearchMultiTurnEnabled:            EnvBool("AGENT_API_MESSAGE_SEARCH_MULTI_TURN_ENABLED", true),
		MessageSearchRerankEnabled:               EnvBool("AGENT_API_MESSAGE_SEARCH_RERANK_ENABLED", true),
		MessageSearchRerankCandidateLimit:        EnvInt("AGENT_API_MESSAGE_SEARCH_RERANK_CANDIDATE_LIMIT", 50),
		MessageSearchLowConfidenceScore:          EnvFloat64("AGENT_API_MESSAGE_SEARCH_LOW_CONFIDENCE_SCORE", 0.04),
		MemoryVectorEnabled:                      EnvBool("AGENT_API_MEMORY_VECTOR_ENABLED", true),
		MemoryVectorQdrantEndpoint:               FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_QDRANT_ENDPOINT"), os.Getenv("AGENT_API_MESSAGE_SEARCH_QDRANT_ENDPOINT"), os.Getenv("AGENT_API_QDRANT_ENDPOINT")),
		MemoryVectorQdrantCollection:             FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_QDRANT_COLLECTION"), "agent_memories"),
		MemoryVectorEpisodeQdrantCollection:      FirstNonEmpty(os.Getenv("AGENT_API_EPISODIC_MEMORY_QDRANT_COLLECTION"), "agent_memory_episodes"),
		MemoryVectorQdrantAPIKey:                 FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_QDRANT_API_KEY"), os.Getenv("AGENT_API_MESSAGE_SEARCH_QDRANT_API_KEY"), os.Getenv("AGENT_API_QDRANT_API_KEY")),
		MemoryVectorQdrantScoreThreshold:         EnvFloat64("AGENT_API_MEMORY_VECTOR_QDRANT_SCORE_THRESHOLD", EnvFloat64("AGENT_API_MESSAGE_SEARCH_QDRANT_SCORE_THRESHOLD", 0)),
		MemoryVectorEmbeddingProvider:            FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_PROVIDER"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROVIDER"), os.Getenv("AGENT_API_EMBEDDING_PROVIDER")),
		MemoryVectorEmbeddingEndpoint:            FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_ENDPOINT"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_ENDPOINT"), os.Getenv("AGENT_API_EMBEDDING_ENDPOINT")),
		MemoryVectorEmbeddingAPIKey:              FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_API_KEY"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_API_KEY"), os.Getenv("AGENT_API_EMBEDDING_API_KEY"), os.Getenv("NVIDIA_API_KEY"), os.Getenv("NGC_API_KEY"), os.Getenv("OPENAI_API_KEY")),
		MemoryVectorEmbeddingAccessToken:         FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_TOKEN"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_TOKEN"), os.Getenv("VERTEX_ACCESS_TOKEN"), os.Getenv("GOOGLE_OAUTH_ACCESS_TOKEN"), os.Getenv("GOOGLE_ACCESS_TOKEN")),
		MemoryVectorEmbeddingModel:               FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_MODEL"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_MODEL")),
		MemoryVectorEmbeddingDimensions:          EnvInt("AGENT_API_MEMORY_VECTOR_EMBEDDING_DIMENSIONS", EnvInt("AGENT_API_MESSAGE_SEARCH_EMBEDDING_DIMENSIONS", 0)),
		MemoryVectorEmbeddingProjectID:           FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_PROJECT_ID"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROJECT_ID"), os.Getenv("VERTEX_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCLOUD_PROJECT")),
		MemoryVectorEmbeddingLocation:            FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_LOCATION"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_LOCATION"), os.Getenv("VERTEX_LOCATION"), os.Getenv("GOOGLE_CLOUD_LOCATION"), os.Getenv("CLOUD_ML_REGION"), "global"),
		MemoryVectorEmbeddingTaskType:            FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_TASK_TYPE"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_TASK_TYPE"), "RETRIEVAL_QUERY"),
		MemoryVectorEmbeddingIndexTaskType:       FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_INDEX_TASK_TYPE"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_INDEX_TASK_TYPE"), "RETRIEVAL_DOCUMENT"),
		MemoryVectorEmbeddingAutoTruncate:        EnvBool("AGENT_API_MEMORY_VECTOR_EMBEDDING_AUTO_TRUNCATE", EnvBool("AGENT_API_MESSAGE_SEARCH_EMBEDDING_AUTO_TRUNCATE", true)),
		MemoryVectorRRFK:                         EnvInt("AGENT_API_MEMORY_VECTOR_RRF_K", EnvInt("AGENT_API_MESSAGE_SEARCH_RRF_K", 60)),
		MemoryVectorRerankEnabled:                EnvBool("AGENT_API_MEMORY_VECTOR_RERANK_ENABLED", true),
		MemoryVectorRerankEndpoint:               FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_RERANK_ENDPOINT"), os.Getenv("AGENT_API_RERANK_ENDPOINT"), "https://ai.api.nvidia.com/v1/retrieval/nvidia/llama-nemotron-rerank-1b-v2/reranking"),
		MemoryVectorRerankAPIKey:                 FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_RERANK_API_KEY"), os.Getenv("AGENT_API_RERANK_API_KEY"), os.Getenv("AGENT_API_MEMORY_VECTOR_EMBEDDING_API_KEY"), os.Getenv("AGENT_API_MESSAGE_SEARCH_EMBEDDING_API_KEY"), os.Getenv("AGENT_API_EMBEDDING_API_KEY"), os.Getenv("NVIDIA_API_KEY"), os.Getenv("NGC_API_KEY")),
		MemoryVectorRerankModel:                  FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_RERANK_MODEL"), os.Getenv("AGENT_API_RERANK_MODEL"), "nvidia/llama-nemotron-rerank-1b-v2"),
		MemoryVectorRerankCandidateLimit:         EnvInt("AGENT_API_MEMORY_VECTOR_RERANK_CANDIDATE_LIMIT", 50),
		MemoryVectorRerankResultLimit:            EnvInt("AGENT_API_MEMORY_VECTOR_RERANK_RESULT_LIMIT", 5),
		MemoryVectorRerankTimeout:                EnvDuration("AGENT_API_MEMORY_VECTOR_RERANK_TIMEOUT", EnvDuration("AGENT_API_RERANK_TIMEOUT", 5*time.Second)),
		MemoryVectorRerankTruncate:               FirstNonEmpty(os.Getenv("AGENT_API_MEMORY_VECTOR_RERANK_TRUNCATE"), os.Getenv("AGENT_API_RERANK_TRUNCATE"), "END"),
		MemoryRecallEnabled:                      EnvBool("AGENT_API_MEMORY_RECALL_ENABLED", true),
		MemoryRecallConditionalEnabled:           EnvBool("AGENT_API_MEMORY_RECALL_CONDITIONAL_ENABLED", true),
		MemoryRecallAsyncEnabled:                 EnvBool("AGENT_API_MEMORY_RECALL_ASYNC_ENABLED", true),
		MemoryRecallTimeout:                      EnvDuration("AGENT_API_MEMORY_RECALL_TIMEOUT", 1200*time.Millisecond),
		MemoryRecallMinQueryRunes:                EnvInt("AGENT_API_MEMORY_RECALL_MIN_QUERY_RUNES", 8),
		MemoryRecallRecentContextMessages:        EnvInt("AGENT_API_MEMORY_RECALL_RECENT_CONTEXT_MESSAGES", 4),
		MemoryRecallRecentContextMaxRunes:        EnvInt("AGENT_API_MEMORY_RECALL_RECENT_CONTEXT_RUNES", 400),
		MemoryRecallForceInterval:                EnvInt("AGENT_API_MEMORY_RECALL_FORCE_INTERVAL", 10),
		MemoryRecallComplexTokenThreshold:        EnvInt("AGENT_API_MEMORY_RECALL_COMPLEX_TOKEN_THRESHOLD", 200),
		MemoryRecallEmbeddingEnabled:             EnvBool("AGENT_API_MEMORY_RECALL_EMBEDDING_ENABLED", true),
		MemoryRecallEmbeddingSimilarityThreshold: EnvFloat64("AGENT_API_MEMORY_RECALL_EMBEDDING_SIMILARITY_THRESHOLD", 0.75),
		MemoryRecallEmbeddingWindow:              EnvInt("AGENT_API_MEMORY_RECALL_EMBEDDING_WINDOW", 3),
		MemoryRecallIntentClassifierEnabled:      EnvBool("AGENT_API_MEMORY_RECALL_INTENT_CLASSIFIER_ENABLED", true),
		MemoryRecallIntentClassifierThreshold:    EnvFloat64("AGENT_API_MEMORY_RECALL_INTENT_CLASSIFIER_THRESHOLD", 0.6),
		MemoryRecallIntentClassifierContextTurns: EnvInt("AGENT_API_MEMORY_RECALL_INTENT_CLASSIFIER_CONTEXT_TURNS", 4),
		MemoryRecallQueryRewriteEnabled:          EnvBool("AGENT_API_MEMORY_RECALL_QUERY_REWRITE_ENABLED", true),
		MemoryRecallLLMTriggerEnabled:            EnvBool("AGENT_API_MEMORY_RECALL_LLM_TRIGGER_ENABLED", true),
		MemoryRecallLLMTriggerTimeout:            EnvDuration("AGENT_API_MEMORY_RECALL_LLM_TRIGGER_TIMEOUT", 900*time.Millisecond),
		MemoryPolicyPath:                         os.Getenv("AGENT_API_MEMORY_POLICY_PATH"),
		MemoryPolicyVersion:                      os.Getenv("AGENT_API_MEMORY_POLICY_VERSION"),
		MemoryPolicyReloadInterval:               EnvDuration("AGENT_API_MEMORY_POLICY_RELOAD_INTERVAL", defaultMemoryPolicyReloadInterval(os.Getenv("AGENT_API_MEMORY_POLICY_PATH"))),
		MemoryPolicyStrictEval:                   EnvBool("AGENT_API_MEMORY_POLICY_STRICT_EVAL", false),
		EpisodicMemoryEnabled:                    EnvBool("AGENT_API_EPISODIC_MEMORY_ENABLED", true),
		EpisodicMemoryCaptureEnabled:             EnvBool("AGENT_API_EPISODIC_MEMORY_CAPTURE_ENABLED", true),
		EpisodicMemoryContextEnabled:             EnvBool("AGENT_API_EPISODIC_MEMORY_CONTEXT_ENABLED", true),
		EpisodicMemoryMinMessages:                EnvInt("AGENT_API_EPISODIC_MEMORY_MIN_MESSAGES", 4),
		EpisodicMemoryMaxMessages:                EnvInt("AGENT_API_EPISODIC_MEMORY_MAX_MESSAGES", 40),
		EpisodicMemoryInjectLimit:                EnvInt("AGENT_API_EPISODIC_MEMORY_INJECT_LIMIT", 5),
		EpisodicMemoryTTL:                        EnvDuration("AGENT_API_EPISODIC_MEMORY_TTL", 180*24*time.Hour),
		EpisodicMemorySummarizeTimeout:           EnvDuration("AGENT_API_EPISODIC_MEMORY_SUMMARIZE_TIMEOUT", 8*time.Second),
		Workspace:                                MustWorkingDir(),
		UserWorkspaceRoot:                        os.Getenv("AGENT_API_USER_WORKSPACE_ROOT"),
		AllowCustomWorkingDir:                    EnvBool("AGENT_API_ALLOW_CUSTOM_WORKING_DIR", false),
		Timezone:                                 os.Getenv("AGENT_API_TIMEZONE"),
		Locale:                                   os.Getenv("AGENT_API_LOCALE"),
		LLMProvider:                              FirstNonEmpty(os.Getenv("AGENT_API_LLM_PROVIDER"), os.Getenv("CLAUDE_CODE_PROVIDER"), "anthropic"),
		APIKey:                                   "",
		APIToken:                                 "",
		APIBaseURL:                               "",
		Model:                                    FirstNonEmpty(os.Getenv("AGENT_API_MODEL"), os.Getenv("AGENT_API_LLM_MODEL")),
		LLMFallbacks:                             os.Getenv("AGENT_API_LLM_FALLBACKS"),
		LLMModelRoutes:                           os.Getenv("AGENT_API_LLM_MODEL_ROUTES"),
		LLMMaxAttempts:                           EnvInt("AGENT_API_LLM_MAX_ATTEMPTS", 2),
		LLMRetryBackoff:                          EnvDuration("AGENT_API_LLM_RETRY_BACKOFF", 300*time.Millisecond),
		LLMChatTimeout:                           EnvDuration("AGENT_API_LLM_CHAT_TIMEOUT", 60*time.Second),
		LLMSkillTimeout:                          EnvDuration("AGENT_API_LLM_SKILL_TIMEOUT", 90*time.Second),
		LLMDailyTokenQuota:                       EnvInt("AGENT_API_LLM_DAILY_TOKEN_QUOTA", 0),
		LLMDailyRequestQuota:                     EnvInt("AGENT_API_LLM_DAILY_REQUEST_QUOTA", 0),
		LLMDailyCostQuotaUSD:                     EnvFloat64("AGENT_API_LLM_DAILY_COST_QUOTA_USD", 0),
		LLMInputCostPerMillion:                   EnvFloat64("AGENT_API_LLM_INPUT_COST_PER_MILLION", 0.30),
		LLMOutputCostPerMillion:                  EnvFloat64("AGENT_API_LLM_OUTPUT_COST_PER_MILLION", 2.50),
		LLMFailureThreshold:                      EnvInt("AGENT_API_LLM_FAILURE_THRESHOLD", 3),
		LLMCircuitCooldown:                       EnvDuration("AGENT_API_LLM_CIRCUIT_COOLDOWN", time.Minute),
		LiveEnabled:                              EnvBool("AGENT_API_LIVE_ENABLED", false),
		LiveProvider:                             FirstNonEmpty(os.Getenv("AGENT_API_LIVE_PROVIDER"), "xai"),
		LiveModel:                                FirstNonEmpty(os.Getenv("AGENT_API_LIVE_MODEL"), os.Getenv("XAI_LIVE_MODEL"), "grok-voice-latest"),
		LiveVertexProjectID:                      FirstNonEmpty(os.Getenv("AGENT_API_LIVE_VERTEX_PROJECT_ID"), os.Getenv("GOCLAW_VERTEX_PROJECT_ID"), os.Getenv("VERTEX_PROJECT_ID"), os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GCLOUD_PROJECT")),
		LiveVertexLocation:                       FirstNonEmpty(os.Getenv("AGENT_API_LIVE_VERTEX_LOCATION"), os.Getenv("VERTEX_LOCATION"), os.Getenv("GOOGLE_CLOUD_LOCATION"), os.Getenv("CLOUD_ML_REGION"), "us-central1"),
		LiveVertexBaseURL:                        os.Getenv("AGENT_API_LIVE_VERTEX_BASE_URL"),
		LiveVertexAPIVersion:                     FirstNonEmpty(os.Getenv("AGENT_API_LIVE_VERTEX_API_VERSION"), "v1beta1"),
		LiveXAIAPIKey:                            FirstNonEmpty(os.Getenv("AGENT_API_LIVE_XAI_API_KEY"), os.Getenv("XAI_API_KEY")),
		LiveXAIBaseURL:                           FirstNonEmpty(os.Getenv("AGENT_API_LIVE_XAI_BASE_URL"), os.Getenv("XAI_LIVE_BASE_URL")),
		LiveInputAudioMIME:                       FirstNonEmpty(os.Getenv("AGENT_API_LIVE_INPUT_AUDIO_MIME_TYPE"), os.Getenv("XAI_LIVE_INPUT_MIME"), "audio/pcm;rate=16000"),
		LiveOutputAudioMIME:                      FirstNonEmpty(os.Getenv("AGENT_API_LIVE_OUTPUT_AUDIO_MIME_TYPE"), os.Getenv("XAI_LIVE_OUTPUT_MIME"), "audio/pcm;rate=16000"),
		LiveVoiceName:                            FirstNonEmpty(os.Getenv("AGENT_API_LIVE_VOICE_NAME"), os.Getenv("XAI_LIVE_VOICE"), "ara"),
		LiveLanguageCode:                         FirstNonEmpty(os.Getenv("AGENT_API_LIVE_LANGUAGE_CODE"), os.Getenv("XAI_LIVE_LANGUAGE_HINT"), "zh"),
		LiveInputTranscription:                   EnvBool("AGENT_API_LIVE_INPUT_TRANSCRIPTION_ENABLED", true),
		LiveOutputTranscription:                  EnvBool("AGENT_API_LIVE_OUTPUT_TRANSCRIPTION_ENABLED", true),
		LiveVADStartSensitivity:                  FirstNonEmpty(os.Getenv("AGENT_API_LIVE_VAD_START_SENSITIVITY"), "START_SENSITIVITY_HIGH"),
		LiveVADEndSensitivity:                    FirstNonEmpty(os.Getenv("AGENT_API_LIVE_VAD_END_SENSITIVITY"), "END_SENSITIVITY_HIGH"),
		LiveVADThreshold:                         EnvFloat64("AGENT_API_LIVE_VAD_THRESHOLD", EnvFloat64("XAI_LIVE_VAD_THRESHOLD", 0.75)),
		LiveVADPrefixPadding:                     EnvDuration("AGENT_API_LIVE_VAD_PREFIX_PADDING", EnvDuration("XAI_LIVE_VAD_PREFIX_PADDING", 333*time.Millisecond)),
		LiveVADSilenceDuration:                   EnvDuration("AGENT_API_LIVE_VAD_SILENCE_DURATION", EnvDuration("XAI_LIVE_VAD_SILENCE_DURATION", time.Second)),
		LiveSessionTimeout:                       EnvDuration("AGENT_API_LIVE_SESSION_TIMEOUT", EnvDuration("XAI_LIVE_TIMEOUT", 10*time.Minute)),
		LiveSetupPromptCacheBackend:              FirstNonEmpty(os.Getenv("AGENT_API_LIVE_SETUP_PROMPT_CACHE_BACKEND"), os.Getenv("AGENT_API_CACHE_BACKEND"), "memory"),
		LiveSetupPromptCacheRedisURL:             FirstNonEmpty(os.Getenv("AGENT_API_LIVE_SETUP_PROMPT_CACHE_REDIS_URL"), os.Getenv("AGENT_API_MESSAGE_CONTEXT_CACHE_REDIS_URL"), os.Getenv("AGENT_API_CACHE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		LiveSetupPromptCacheTTL:                  EnvDuration("AGENT_API_LIVE_SETUP_PROMPT_CACHE_TTL", EnvDuration("AGENT_API_CACHE_DEFAULT_TTL", time.Minute)),
		AuthMode:                                 FirstNonEmpty(os.Getenv("AGENT_API_AUTH_MODE"), "auto"),
		AuthToken:                                os.Getenv("AGENT_API_AUTH_TOKEN"),
		UserHeader:                               "X-User-ID",
		JWTSecret:                                os.Getenv("AGENT_API_JWT_SECRET"),
		JWTIssuer:                                os.Getenv("AGENT_API_JWT_ISSUER"),
		JWTAudience:                              os.Getenv("AGENT_API_JWT_AUDIENCE"),
		JWTUserClaim:                             FirstNonEmpty(os.Getenv("AGENT_API_JWT_USER_CLAIM"), "sub"),
		EnableUserSystem:                         EnvBool("AGENT_API_ENABLE_USER_SYSTEM", false),
		AuthAccessTTL:                            EnvDuration("AGENT_API_AUTH_ACCESS_TTL", 15*time.Minute),
		AuthRefreshTTL:                           EnvDuration("AGENT_API_AUTH_REFRESH_TTL", 30*24*time.Hour),
		EmailVerificationRequired:                EnvBool("AGENT_API_EMAIL_VERIFICATION_REQUIRED", false),
		EmailVerificationTTL:                     EnvDuration("AGENT_API_EMAIL_VERIFICATION_TTL", 24*time.Hour),
		EmailProvider:                            os.Getenv("AGENT_API_EMAIL_PROVIDER"),
		EmailFrom:                                os.Getenv("AGENT_API_EMAIL_FROM"),
		EmailPublicBaseURL:                       os.Getenv("AGENT_API_EMAIL_PUBLIC_BASE_URL"),
		ResendAPIKey:                             os.Getenv("AGENT_API_RESEND_API_KEY"),
		ResendBaseURL:                            os.Getenv("AGENT_API_RESEND_BASE_URL"),
		SessionCookieName:                        FirstNonEmpty(os.Getenv("AGENT_API_SESSION_COOKIE_NAME"), "agentapi_session"),
		SessionCookieSecret:                      os.Getenv("AGENT_API_SESSION_COOKIE_SECRET"),
		SessionCookieDomain:                      os.Getenv("AGENT_API_SESSION_COOKIE_DOMAIN"),
		SessionCookieSecure:                      EnvBool("AGENT_API_SESSION_COOKIE_SECURE", false),
		SessionCookieSameSite:                    FirstNonEmpty(os.Getenv("AGENT_API_SESSION_COOKIE_SAMESITE"), "lax"),
		CSRFEnabled:                              EnvBool("AGENT_API_CSRF_ENABLED", false),
		CSRFCookieName:                           FirstNonEmpty(os.Getenv("AGENT_API_CSRF_COOKIE_NAME"), "agentapi_csrf"),
		CSRFHeaderName:                           FirstNonEmpty(os.Getenv("AGENT_API_CSRF_HEADER_NAME"), "X-CSRF-Token"),
		CORSAllowedOrigins:                       os.Getenv("AGENT_API_CORS_ALLOWED_ORIGINS"),
		CORSAllowCredentials:                     EnvBool("AGENT_API_CORS_ALLOW_CREDENTIALS", true),
		AdminToken:                               os.Getenv("AGENT_API_ADMIN_TOKEN"),
		EvalDailyEnabled:                         EnvBool("AGENT_API_EVAL_DAILY_ENABLED", true),
		EvalDailyHour:                            EnvInt("AGENT_API_EVAL_DAILY_HOUR", 5),
		EvalDailyMinute:                          EnvInt("AGENT_API_EVAL_DAILY_MINUTE", 0),
		EvalDailyUserIDs:                         os.Getenv("AGENT_API_EVAL_DAILY_USER_IDS"),
		EvalDailyBatchLimit:                      EnvInt("AGENT_API_EVAL_DAILY_BATCH_LIMIT", 200),
		EvalDailyTimeout:                         EnvDuration("AGENT_API_EVAL_DAILY_TIMEOUT", 10*time.Minute),
		EvalJudgeEnabled:                         EnvBool("AGENT_API_EVAL_JUDGE_ENABLED", strings.TrimSpace(os.Getenv("AGENT_API_EVAL_JUDGE_MODEL")) != ""),
		EvalJudgeModel:                           os.Getenv("AGENT_API_EVAL_JUDGE_MODEL"),
		EvalJudgePromptVersion:                   FirstNonEmpty(os.Getenv("AGENT_API_EVAL_JUDGE_PROMPT_VERSION"), agentruntime.DefaultGoldenJudgePromptVersion),
		TrustedUserHeader:                        FirstNonEmpty(os.Getenv("AGENT_API_TRUSTED_USER_HEADER"), "X-User-ID"),
		TrustedSecretHeader:                      os.Getenv("AGENT_API_TRUSTED_SECRET_HEADER"),
		TrustedSecret:                            os.Getenv("AGENT_API_TRUSTED_SECRET"),
		AllowDangerousTools:                      false,
		NetworkAllowlist:                         os.Getenv("AGENT_API_NETWORK_ALLOWLIST"),
		PluginDir:                                os.Getenv("AGENT_API_PLUGIN_DIR"),
		SkillDirs:                                os.Getenv("AGENT_API_SKILL_DIRS"),
		MCPServersJSON:                           os.Getenv("AGENT_API_MCP_SERVERS"),
		RateLimitBackend:                         FirstNonEmpty(os.Getenv("AGENT_API_RATE_LIMIT_BACKEND"), "none"),
		RateLimit:                                EnvInt("AGENT_API_RATE_LIMIT", 60),
		OperationRateLimits:                      os.Getenv("AGENT_API_OPERATION_RATE_LIMITS"),
		CacheBackend:                             FirstNonEmpty(os.Getenv("AGENT_API_CACHE_BACKEND"), "memory"),
		CacheRedisURL:                            FirstNonEmpty(os.Getenv("AGENT_API_CACHE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		CachePrefix:                              FirstNonEmpty(os.Getenv("AGENT_API_CACHE_PREFIX"), "agent:cache"),
		CacheDefaultTTL:                          EnvDuration("AGENT_API_CACHE_DEFAULT_TTL", 10*time.Minute),
		CacheFailOpen:                            EnvBool("AGENT_API_CACHE_FAIL_OPEN", EnvBool("AGENT_API_REDIS_FAIL_OPEN", false)),
		RedisURL:                                 os.Getenv("AGENT_API_REDIS_URL"),
		RedisFailOpen:                            EnvBool("AGENT_API_REDIS_FAIL_OPEN", false),
		MessageContextCacheBackend:               FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_CONTEXT_CACHE_BACKEND"), os.Getenv("AGENT_API_CACHE_BACKEND"), "memory"),
		MessageContextCacheRedisURL:              FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_CONTEXT_CACHE_REDIS_URL"), os.Getenv("AGENT_API_CACHE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		MessageContextCacheTTL:                   EnvDuration("AGENT_API_MESSAGE_CONTEXT_CACHE_TTL", EnvDuration("AGENT_API_CACHE_DEFAULT_TTL", 24*time.Hour)),
		SessionListCacheBackend:                  FirstNonEmpty(os.Getenv("AGENT_API_SESSION_LIST_CACHE_BACKEND"), "none"),
		SessionListCacheRedisURL:                 FirstNonEmpty(os.Getenv("AGENT_API_SESSION_LIST_CACHE_REDIS_URL"), os.Getenv("AGENT_API_CACHE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		SessionListCacheTTL:                      EnvDuration("AGENT_API_SESSION_LIST_CACHE_TTL", EnvDuration("AGENT_API_CACHE_DEFAULT_TTL", 10*time.Minute)),
		MessageSequenceBackend:                   FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEQUENCE_BACKEND"), "redis"),
		MessageSequenceRedisURL:                  FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_SEQUENCE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		MessageEventsBackend:                     FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_EVENTS_BACKEND"), "local"),
		MessageEventsKafkaBrokers:                os.Getenv("AGENT_API_MESSAGE_EVENTS_KAFKA_BROKERS"),
		MessageEventsKafkaTopic:                  FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_EVENTS_KAFKA_TOPIC"), "agent.messages"),
		MessageEventsKafkaClientID:               FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_EVENTS_KAFKA_CLIENT_ID"), "agentapi"),
		MessageEventsKafkaConsumerEnabled:        EnvBool("AGENT_API_MESSAGE_EVENTS_KAFKA_CONSUMER_ENABLED", false),
		MessageEventsKafkaConsumerGroup:          FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_EVENTS_KAFKA_CONSUMER_GROUP"), "agentapi-message-workers"),
		MessageEventsKafkaDLQTopic:               os.Getenv("AGENT_API_MESSAGE_EVENTS_KAFKA_DLQ_TOPIC"),
		MessageEventsKafkaRetryAttempts:          EnvInt("AGENT_API_MESSAGE_EVENTS_KAFKA_RETRY_ATTEMPTS", 3),
		MessageEventsKafkaRetryBackoff:           EnvDuration("AGENT_API_MESSAGE_EVENTS_KAFKA_RETRY_BACKOFF", time.Second),
		MessageEventsKafkaProcessTimeout:         EnvDuration("AGENT_API_MESSAGE_EVENTS_KAFKA_PROCESS_TIMEOUT", 30*time.Second),
		MessageEventsProcessedLockBackend:        FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_EVENTS_PROCESSED_LOCK_BACKEND"), "redis"),
		MessageEventsProcessedLockRedisURL:       FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_EVENTS_PROCESSED_LOCK_REDIS_URL"), os.Getenv("AGENT_API_MESSAGE_CONTEXT_CACHE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		MessageEventsProcessedLockTTL:            EnvDuration("AGENT_API_MESSAGE_EVENTS_PROCESSED_LOCK_TTL", 24*time.Hour),
		JobQueueRedisURL:                         FirstNonEmpty(os.Getenv("AGENT_API_JOB_QUEUE_REDIS_URL"), os.Getenv("AGENT_API_REDIS_URL")),
		JobQueueStream:                           FirstNonEmpty(os.Getenv("AGENT_API_JOB_QUEUE_STREAM"), agentruntime.DefaultJobQueueStream),
		JobQueueConsumerGroup:                    FirstNonEmpty(os.Getenv("AGENT_API_JOB_QUEUE_CONSUMER_GROUP"), agentruntime.DefaultJobQueueConsumerGroup),
		JobQueueConsumer:                         os.Getenv("AGENT_API_JOB_QUEUE_CONSUMER"),
		JobQueueBlockTimeout:                     EnvDuration("AGENT_API_JOB_QUEUE_BLOCK_TIMEOUT", agentruntime.DefaultJobQueueBlockTimeout),
		JobQueueClaimIdle:                        EnvDuration("AGENT_API_JOB_QUEUE_CLAIM_IDLE", agentruntime.DefaultJobQueueClaimIdle),
		JobQueueLockTTL:                          EnvDuration("AGENT_API_JOB_QUEUE_LOCK_TTL", agentruntime.DefaultJobQueueLockTTL),
		JobWorkerEnabled:                         EnvBool("AGENT_API_JOB_WORKER_ENABLED", true),
		JobEventStreamEnabled:                    EnvBool("AGENT_API_JOB_EVENT_STREAM_ENABLED", true),
		JobEventStreamPrefix:                     FirstNonEmpty(os.Getenv("AGENT_API_JOB_EVENT_STREAM_PREFIX"), agentruntime.DefaultJobEventStreamPrefix),
		JobEventStreamTTL:                        EnvDuration("AGENT_API_JOB_EVENT_STREAM_TTL", agentruntime.DefaultJobEventStreamTTL),
		JobEventStreamMaxLen:                     EnvInt("AGENT_API_JOB_EVENT_STREAM_MAX_LEN", 10000),
		ChatEventStreamEnabled:                   EnvBool("AGENT_API_CHAT_EVENT_STREAM_ENABLED", true),
		ChatEventStreamPrefix:                    FirstNonEmpty(os.Getenv("AGENT_API_CHAT_EVENT_STREAM_PREFIX"), agentruntime.DefaultChatEventStreamPrefix),
		ChatEventStreamTTL:                       EnvDuration("AGENT_API_CHAT_EVENT_STREAM_TTL", agentruntime.DefaultChatEventStreamTTL),
		ChatEventStreamMaxLen:                    EnvInt("AGENT_API_CHAT_EVENT_STREAM_MAX_LEN", 10000),
		ChatEventStreamBlock:                     EnvDuration("AGENT_API_CHAT_EVENT_STREAM_BLOCK", agentruntime.DefaultChatStreamBlockRead),
		JobEventFanoutEnabled:                    EnvBool("AGENT_API_JOB_EVENT_FANOUT_ENABLED", true),
		JobEventFanoutChannel:                    FirstNonEmpty(os.Getenv("AGENT_API_JOB_EVENT_FANOUT_CHANNEL"), agentruntime.DefaultJobEventFanoutChannel),
		JobEventFanoutOrigin:                     os.Getenv("AGENT_API_JOB_EVENT_FANOUT_ORIGIN"),
		WebPushVAPIDPublicKey:                    os.Getenv("AGENT_API_WEB_PUSH_VAPID_PUBLIC_KEY"),
		WebPushVAPIDPrivateKey:                   os.Getenv("AGENT_API_WEB_PUSH_VAPID_PRIVATE_KEY"),
		WebPushVAPIDSubject:                      FirstNonEmpty(os.Getenv("AGENT_API_WEB_PUSH_VAPID_SUBJECT"), os.Getenv("AGENT_API_EMAIL_PUBLIC_BASE_URL")),
		WebPushTTLSeconds:                        EnvInt("AGENT_API_WEB_PUSH_TTL_SECONDS", 12*60*60),
		MessageAttachmentWorkerEnabled:           EnvBool("AGENT_API_MESSAGE_ATTACHMENT_WORKER_ENABLED", true),
		MessageAttachmentWorkerBatchSize:         EnvInt("AGENT_API_MESSAGE_ATTACHMENT_WORKER_BATCH_SIZE", 25),
		MessageAttachmentWorkerPollInterval:      EnvDuration("AGENT_API_MESSAGE_ATTACHMENT_WORKER_POLL_INTERVAL", 5*time.Second),
		MessageAttachmentWorkerProcessTimeout:    EnvDuration("AGENT_API_MESSAGE_ATTACHMENT_WORKER_PROCESS_TIMEOUT", 30*time.Second),
		MessageAttachmentThumbnailMaxDimension:   EnvInt("AGENT_API_MESSAGE_ATTACHMENT_THUMBNAIL_MAX_DIMENSION", 512),
		MessageArchiveWorkerEnabled:              EnvBool("AGENT_API_MESSAGE_ARCHIVE_WORKER_ENABLED", false),
		MessageArchiveAfter:                      EnvDuration("AGENT_API_MESSAGE_ARCHIVE_AFTER", 30*24*time.Hour),
		MessageArchiveWorkerBatchSize:            EnvInt("AGENT_API_MESSAGE_ARCHIVE_WORKER_BATCH_SIZE", 100),
		MessageArchiveWorkerPollInterval:         EnvDuration("AGENT_API_MESSAGE_ARCHIVE_WORKER_POLL_INTERVAL", time.Hour),
		MessageArchiveWorkerProcessTimeout:       EnvDuration("AGENT_API_MESSAGE_ARCHIVE_WORKER_PROCESS_TIMEOUT", 2*time.Minute),
		MessageArchivePrefix:                     FirstNonEmpty(os.Getenv("AGENT_API_MESSAGE_ARCHIVE_PREFIX"), "message-archive"),
		MessageArchiveClearPGPayload:             EnvBool("AGENT_API_MESSAGE_ARCHIVE_CLEAR_PG_PAYLOAD", true),
		RetentionDays:                            EnvInt("AGENT_API_RETENTION_DAYS", 0),
		LocalArtifactStagingRetention:            EnvDuration("AGENT_API_LOCAL_ARTIFACT_STAGING_RETENTION", 24*time.Hour),
		ShutdownTimeout:                          EnvDuration("AGENT_API_SHUTDOWN_TIMEOUT", 30*time.Second),
		RequestTimeout:                           EnvDuration("AGENT_API_REQUEST_TIMEOUT", 0),
		TurnTimeout:                              2 * time.Minute,
		DeepAgentV2Enabled:                       EnvBool("AGENT_API_DEEP_AGENT_V2_ENABLED", false),
		DeepAgentV2ShadowRoute:                   EnvBool("AGENT_API_DEEP_AGENT_V2_SHADOW_ROUTE", false),
		DeepResearchOrchestratorWorkerEnabled:    EnvBool("AGENT_API_DEEP_RESEARCH_ORCHESTRATOR_WORKER_ENABLED", false),
		DeepResearchWorkerBackend:                FirstNonEmpty(os.Getenv("AGENT_API_DEEP_RESEARCH_WORKER_BACKEND"), "inline"),
		DeepResearchMaxWorkers:                   EnvInt("AGENT_API_DEEP_RESEARCH_MAX_WORKERS", 8),
		DeepResearchMaxConcurrency:               EnvInt("AGENT_API_DEEP_RESEARCH_MAX_CONCURRENCY", 3),
		DeepResearchWorkerTimeout:                EnvDuration("AGENT_API_DEEP_RESEARCH_WORKER_TIMEOUT", 5*time.Minute),
		DeepResearchTotalTimeout:                 EnvDuration("AGENT_API_DEEP_RESEARCH_TOTAL_TIMEOUT", 20*time.Minute),
		DeepResearchMaxRetries:                   EnvInt("AGENT_API_DEEP_RESEARCH_MAX_RETRIES", 2),
		DeepResearchReplanEnabled:                EnvBool("AGENT_API_DEEP_RESEARCH_REPLAN_ENABLED", true),
		DeepResearchMaxReplans:                   EnvInt("AGENT_API_DEEP_RESEARCH_MAX_REPLANS", 3),
		DeepResearchReplanEveryBatches:           EnvInt("AGENT_API_DEEP_RESEARCH_REPLAN_EVERY_BATCHES", 1),
		DeepResearchFallbackLegacy:               EnvBool("AGENT_API_DEEP_RESEARCH_FALLBACK_LEGACY", true),
		DeepResearchRequireSources:               EnvBool("AGENT_API_DEEP_RESEARCH_REQUIRE_SOURCES", true),
		DeepResearchMinSuccessfulWorkers:         EnvInt("AGENT_API_DEEP_RESEARCH_MIN_SUCCESSFUL_WORKERS", 1),
		LoopAutomationEnabled:                    EnvBool("AGENT_API_LOOP_AUTOMATION_ENABLED", false),
		LoopScheduleTriggersEnabled:              EnvBool("AGENT_API_LOOP_SCHEDULE_TRIGGERS_ENABLED", false),
		LoopWebhookTriggersEnabled:               EnvBool("AGENT_API_LOOP_WEBHOOK_TRIGGERS_ENABLED", false),
		LoopMonitorTriggersEnabled:               EnvBool("AGENT_API_LOOP_MONITOR_TRIGGERS_ENABLED", false),
		LoopEvalRepairTriggersEnabled:            EnvBool("AGENT_API_LOOP_EVAL_REPAIR_TRIGGERS_ENABLED", false),
		LoopConnectorTriggersEnabled:             EnvBool("AGENT_API_LOOP_CONNECTOR_TRIGGERS_ENABLED", false),
		LoopTriggerTTL:                           EnvDuration("AGENT_API_LOOP_TRIGGER_TTL", 7*24*time.Hour),
		SkillShellTimeout:                        EnvDuration("AGENT_API_SKILL_SHELL_TIMEOUT", 90*time.Second),
		SkillShellRunner:                         FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SHELL_RUNNER"), "docker"),
		SkillSandboxImage:                        FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_IMAGE"), agentruntime.DefaultSkillSandboxImage),
		SkillSandboxNetwork:                      FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_NETWORK"), agentruntime.DefaultSkillSandboxNetwork),
		SkillSandboxMemory:                       FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_MEMORY"), agentruntime.DefaultSkillSandboxMemory),
		SkillSandboxCPUs:                         FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_CPUS"), agentruntime.DefaultSkillSandboxCPUs),
		SkillSandboxPidsLimit:                    EnvInt("AGENT_API_SKILL_SANDBOX_PIDS_LIMIT", agentruntime.DefaultSkillSandboxPidsLimit),
		SkillSandboxTmpfsSize:                    FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_TMPFS_SIZE"), agentruntime.DefaultSkillSandboxTmpfsSize),
		SkillSandboxPrepullImages:                FirstNonEmpty(os.Getenv("AGENT_API_SKILL_SANDBOX_PREPULL_IMAGES"), "python:3.12-slim,node:22-alpine"),
		SkillSandboxWarmPoolSize:                 EnvInt("AGENT_API_SKILL_SANDBOX_WARM_POOL_SIZE", 1),
	}
}

func defaultMemoryPolicyReloadInterval(path string) time.Duration {
	if strings.TrimSpace(path) == "" {
		return 0
	}
	return 30 * time.Second
}

// BindFlags binds CLI flags onto cfg. Defaults should already be loaded from Default().
func BindFlags(command *cobra.Command, cfg *Config) {
	flags := command.Flags()
	flags.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP server address")
	flags.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directory for user-scoped sessions and memory")
	flags.StringVar(&cfg.StoreBackend, "store-backend", cfg.StoreBackend, "storage backend: file, object, or sql")
	flags.StringVar(&cfg.ObjectBaseURL, "object-base-url", cfg.ObjectBaseURL, "HTTP object store base URL for store-backend=object")
	flags.StringVar(&cfg.ObjectToken, "object-token", cfg.ObjectToken, "bearer token for HTTP object store")
	flags.DurationVar(&cfg.ObjectTimeout, "object-timeout", cfg.ObjectTimeout, "HTTP object store request timeout")
	flags.StringVar(&cfg.ArtifactStore, "artifact-store", cfg.ArtifactStore, "artifact object store: file or s3")
	flags.StringVar(&cfg.ArtifactS3Endpoint, "artifact-s3-endpoint", cfg.ArtifactS3Endpoint, "S3/R2-compatible endpoint for artifacts")
	flags.StringVar(&cfg.ArtifactS3AccessKey, "artifact-s3-access-key", cfg.ArtifactS3AccessKey, "S3/R2 access key for artifacts")
	flags.StringVar(&cfg.ArtifactS3SecretKey, "artifact-s3-secret-key", cfg.ArtifactS3SecretKey, "S3/R2 secret key for artifacts")
	flags.StringVar(&cfg.ArtifactS3Bucket, "artifact-s3-bucket", cfg.ArtifactS3Bucket, "S3/R2 bucket for artifacts")
	flags.StringVar(&cfg.ArtifactS3Prefix, "artifact-s3-prefix", cfg.ArtifactS3Prefix, "S3/R2 key prefix for artifacts")
	flags.BoolVar(&cfg.ArtifactS3SSL, "artifact-s3-ssl", cfg.ArtifactS3SSL, "use HTTPS for S3/R2 artifacts")
	flags.Int64Var(&cfg.AssetMaxBytes, "asset-max-bytes", cfg.AssetMaxBytes, "max bytes for attachments and generated artifacts")
	flags.StringVar(&cfg.SQLDriver, "sql-driver", cfg.SQLDriver, "database/sql driver name for store-backend=sql")
	flags.StringVar(&cfg.SQLDSN, "sql-dsn", cfg.SQLDSN, "database/sql DSN for store-backend=sql")
	flags.StringVar(&cfg.SQLDialect, "sql-dialect", cfg.SQLDialect, "SQL dialect: question or postgres")
	flags.IntVar(&cfg.SQLMaxOpen, "sql-max-open-conns", cfg.SQLMaxOpen, "max open SQL connections")
	flags.IntVar(&cfg.SQLMaxIdle, "sql-max-idle-conns", cfg.SQLMaxIdle, "max idle SQL connections")
	flags.DurationVar(&cfg.SQLConnMaxLifetime, "sql-conn-max-lifetime", cfg.SQLConnMaxLifetime, "max SQL connection lifetime")
	flags.StringVar(&cfg.MessageSearchBackend, "message-search-backend", cfg.MessageSearchBackend, "message search backend: sql, elasticsearch, opensearch, semantic, or hybrid")
	flags.StringVar(&cfg.MessageSearchEndpoint, "message-search-endpoint", cfg.MessageSearchEndpoint, "Elasticsearch/OpenSearch endpoint for message search")
	flags.StringVar(&cfg.MessageSearchIndex, "message-search-index", cfg.MessageSearchIndex, "Elasticsearch/OpenSearch index for message search")
	flags.StringVar(&cfg.MessageSearchAPIKey, "message-search-api-key", cfg.MessageSearchAPIKey, "Elasticsearch/OpenSearch API key for message search")
	flags.StringVar(&cfg.MessageSearchUsername, "message-search-username", cfg.MessageSearchUsername, "Elasticsearch/OpenSearch username for message search")
	flags.StringVar(&cfg.MessageSearchPassword, "message-search-password", cfg.MessageSearchPassword, "Elasticsearch/OpenSearch password for message search")
	flags.DurationVar(&cfg.MessageSearchTimeout, "message-search-timeout", cfg.MessageSearchTimeout, "message search backend request timeout")
	flags.BoolVar(&cfg.MessageSearchIndexManagementEnabled, "message-search-index-management-enabled", cfg.MessageSearchIndexManagementEnabled, "enable Elasticsearch message index lifecycle/template maintenance")
	flags.StringVar(&cfg.MessageSearchIndexLifecyclePolicy, "message-search-index-lifecycle-policy", cfg.MessageSearchIndexLifecyclePolicy, "Elasticsearch ILM policy name for message search indices")
	flags.StringVar(&cfg.MessageSearchIndexTemplate, "message-search-index-template", cfg.MessageSearchIndexTemplate, "Elasticsearch index template name for message search indices")
	flags.StringVar(&cfg.MessageSearchIndexWriteAlias, "message-search-index-write-alias", cfg.MessageSearchIndexWriteAlias, "Elasticsearch write alias for rollover-backed message search indices")
	flags.StringVar(&cfg.MessageSearchIndexAnalyzer, "message-search-index-analyzer", cfg.MessageSearchIndexAnalyzer, "Elasticsearch analyzer for indexed message text")
	flags.StringVar(&cfg.MessageSearchIndexSearchAnalyzer, "message-search-index-search-analyzer", cfg.MessageSearchIndexSearchAnalyzer, "Elasticsearch search analyzer for message text queries")
	flags.DurationVar(&cfg.MessageSearchIndexDowngradeAfter, "message-search-index-downgrade-after", cfg.MessageSearchIndexDowngradeAfter, "age after which message search indices are downgraded to read-only")
	flags.DurationVar(&cfg.MessageSearchIndexCloseAfter, "message-search-index-close-after", cfg.MessageSearchIndexCloseAfter, "age after which message search indices are closed")
	flags.DurationVar(&cfg.MessageSearchIndexMaintenanceInterval, "message-search-index-maintenance-interval", cfg.MessageSearchIndexMaintenanceInterval, "Elasticsearch message index maintenance interval")
	flags.IntVar(&cfg.MessageSearchIndexMaintenanceBatchLimit, "message-search-index-maintenance-batch-limit", cfg.MessageSearchIndexMaintenanceBatchLimit, "max Elasticsearch message indices to downgrade or close per maintenance pass")
	flags.StringVar(&cfg.MessageSearchQdrantEndpoint, "message-search-qdrant-endpoint", cfg.MessageSearchQdrantEndpoint, "Qdrant endpoint for semantic message search")
	flags.StringVar(&cfg.MessageSearchQdrantCollection, "message-search-qdrant-collection", cfg.MessageSearchQdrantCollection, "Qdrant collection for semantic message search")
	flags.StringVar(&cfg.MessageSearchQdrantAPIKey, "message-search-qdrant-api-key", cfg.MessageSearchQdrantAPIKey, "Qdrant API key for semantic message search")
	flags.Float64Var(&cfg.MessageSearchQdrantScoreThreshold, "message-search-qdrant-score-threshold", cfg.MessageSearchQdrantScoreThreshold, "minimum Qdrant semantic search score; 0 disables")
	flags.StringVar(&cfg.MessageSearchEmbeddingProvider, "message-search-embedding-provider", cfg.MessageSearchEmbeddingProvider, "embedding provider for semantic message search: openai, nvidia, or vertex")
	flags.StringVar(&cfg.MessageSearchEmbeddingEndpoint, "message-search-embedding-endpoint", cfg.MessageSearchEmbeddingEndpoint, "embedding endpoint for semantic message search; OpenAI-compatible, NVIDIA NIM, or Vertex AI base URL")
	flags.StringVar(&cfg.MessageSearchEmbeddingAPIKey, "message-search-embedding-api-key", cfg.MessageSearchEmbeddingAPIKey, "embedding API key for OpenAI-compatible semantic message search")
	flags.StringVar(&cfg.MessageSearchEmbeddingAccessToken, "message-search-embedding-token", cfg.MessageSearchEmbeddingAccessToken, "OAuth access token for Vertex AI semantic message search; service account env or gcloud are used when empty")
	flags.StringVar(&cfg.MessageSearchEmbeddingModel, "message-search-embedding-model", cfg.MessageSearchEmbeddingModel, "embedding model for semantic message search")
	flags.IntVar(&cfg.MessageSearchEmbeddingDimensions, "message-search-embedding-dimensions", cfg.MessageSearchEmbeddingDimensions, "embedding vector dimensions; 0 uses provider default")
	flags.StringVar(&cfg.MessageSearchEmbeddingProjectID, "message-search-embedding-project-id", cfg.MessageSearchEmbeddingProjectID, "Google Cloud project ID for Vertex AI embeddings")
	flags.StringVar(&cfg.MessageSearchEmbeddingLocation, "message-search-embedding-location", cfg.MessageSearchEmbeddingLocation, "Vertex AI location for embeddings")
	flags.StringVar(&cfg.MessageSearchEmbeddingTaskType, "message-search-embedding-task-type", cfg.MessageSearchEmbeddingTaskType, "Vertex AI embedding task_type for search queries")
	flags.StringVar(&cfg.MessageSearchEmbeddingIndexTaskType, "message-search-embedding-index-task-type", cfg.MessageSearchEmbeddingIndexTaskType, "Vertex AI embedding task_type for indexed message documents")
	flags.BoolVar(&cfg.MessageSearchEmbeddingAutoTruncate, "message-search-embedding-auto-truncate", cfg.MessageSearchEmbeddingAutoTruncate, "allow Vertex AI embedding input auto truncation")
	flags.IntVar(&cfg.MessageSearchRRFK, "message-search-rrf-k", cfg.MessageSearchRRFK, "RRF k constant for hybrid message search ranking")
	flags.BoolVar(&cfg.MessageSearchQueryRewriteEnabled, "message-search-query-rewrite-enabled", cfg.MessageSearchQueryRewriteEnabled, "enable deterministic query rewrite variants for message search recall")
	flags.BoolVar(&cfg.MessageSearchDynamicTopKEnabled, "message-search-dynamic-topk-enabled", cfg.MessageSearchDynamicTopKEnabled, "enable adaptive recall window sizing for message search")
	flags.IntVar(&cfg.MessageSearchMinRecallWindow, "message-search-min-recall-window", cfg.MessageSearchMinRecallWindow, "minimum candidate recall window for adaptive message search")
	flags.IntVar(&cfg.MessageSearchMaxRecallWindow, "message-search-max-recall-window", cfg.MessageSearchMaxRecallWindow, "maximum candidate recall window for adaptive message search")
	flags.BoolVar(&cfg.MessageSearchMultiTurnEnabled, "message-search-multi-turn-enabled", cfg.MessageSearchMultiTurnEnabled, "enable extra rewritten retrieval passes when initial message search confidence is low")
	flags.BoolVar(&cfg.MessageSearchRerankEnabled, "message-search-rerank-enabled", cfg.MessageSearchRerankEnabled, "enable lightweight local reranking for message search candidates")
	flags.IntVar(&cfg.MessageSearchRerankCandidateLimit, "message-search-rerank-candidate-limit", cfg.MessageSearchRerankCandidateLimit, "max message search candidates reranked locally")
	flags.Float64Var(&cfg.MessageSearchLowConfidenceScore, "message-search-low-confidence-score", cfg.MessageSearchLowConfidenceScore, "score threshold that triggers rewritten message search retrieval")
	flags.BoolVar(&cfg.MemoryVectorEnabled, "memory-vector-enabled", cfg.MemoryVectorEnabled, "enable Qdrant vector indexing and retrieval for saved memory when embeddings are configured")
	flags.StringVar(&cfg.MemoryVectorQdrantEndpoint, "memory-vector-qdrant-endpoint", cfg.MemoryVectorQdrantEndpoint, "Qdrant endpoint for saved memory vector retrieval")
	flags.StringVar(&cfg.MemoryVectorQdrantCollection, "memory-vector-qdrant-collection", cfg.MemoryVectorQdrantCollection, "Qdrant collection for saved memory vectors")
	flags.StringVar(&cfg.MemoryVectorEpisodeQdrantCollection, "episodic-memory-qdrant-collection", cfg.MemoryVectorEpisodeQdrantCollection, "Qdrant collection for episodic memory vectors")
	flags.StringVar(&cfg.MemoryVectorQdrantAPIKey, "memory-vector-qdrant-api-key", cfg.MemoryVectorQdrantAPIKey, "Qdrant API key for saved memory vectors")
	flags.Float64Var(&cfg.MemoryVectorQdrantScoreThreshold, "memory-vector-qdrant-score-threshold", cfg.MemoryVectorQdrantScoreThreshold, "minimum Qdrant saved memory vector search score; 0 disables")
	flags.StringVar(&cfg.MemoryVectorEmbeddingProvider, "memory-vector-embedding-provider", cfg.MemoryVectorEmbeddingProvider, "embedding provider for saved memory vector retrieval: openai, nvidia, or vertex")
	flags.StringVar(&cfg.MemoryVectorEmbeddingEndpoint, "memory-vector-embedding-endpoint", cfg.MemoryVectorEmbeddingEndpoint, "embedding endpoint for saved memory vector retrieval; OpenAI-compatible, NVIDIA NIM, or Vertex AI base URL")
	flags.StringVar(&cfg.MemoryVectorEmbeddingAPIKey, "memory-vector-embedding-api-key", cfg.MemoryVectorEmbeddingAPIKey, "embedding API key for OpenAI-compatible saved memory vector retrieval")
	flags.StringVar(&cfg.MemoryVectorEmbeddingAccessToken, "memory-vector-embedding-token", cfg.MemoryVectorEmbeddingAccessToken, "OAuth access token for Vertex AI saved memory vector retrieval; service account env or gcloud are used when empty")
	flags.StringVar(&cfg.MemoryVectorEmbeddingModel, "memory-vector-embedding-model", cfg.MemoryVectorEmbeddingModel, "embedding model for saved memory vector retrieval")
	flags.IntVar(&cfg.MemoryVectorEmbeddingDimensions, "memory-vector-embedding-dimensions", cfg.MemoryVectorEmbeddingDimensions, "embedding vector dimensions for saved memory; 0 uses provider default")
	flags.StringVar(&cfg.MemoryVectorEmbeddingProjectID, "memory-vector-embedding-project-id", cfg.MemoryVectorEmbeddingProjectID, "Google Cloud project ID for Vertex AI saved memory embeddings")
	flags.StringVar(&cfg.MemoryVectorEmbeddingLocation, "memory-vector-embedding-location", cfg.MemoryVectorEmbeddingLocation, "Vertex AI location for saved memory embeddings")
	flags.StringVar(&cfg.MemoryVectorEmbeddingTaskType, "memory-vector-embedding-task-type", cfg.MemoryVectorEmbeddingTaskType, "Vertex AI embedding task_type for saved memory retrieval queries")
	flags.StringVar(&cfg.MemoryVectorEmbeddingIndexTaskType, "memory-vector-embedding-index-task-type", cfg.MemoryVectorEmbeddingIndexTaskType, "Vertex AI embedding task_type for indexed saved memory documents")
	flags.BoolVar(&cfg.MemoryVectorEmbeddingAutoTruncate, "memory-vector-embedding-auto-truncate", cfg.MemoryVectorEmbeddingAutoTruncate, "allow Vertex AI saved memory embedding input auto truncation")
	flags.IntVar(&cfg.MemoryVectorRRFK, "memory-vector-rrf-k", cfg.MemoryVectorRRFK, "RRF k constant for saved memory hybrid retrieval")
	flags.BoolVar(&cfg.MemoryVectorRerankEnabled, "memory-vector-rerank-enabled", cfg.MemoryVectorRerankEnabled, "enable NVIDIA reranking for L2 episodic memory vector candidates")
	flags.StringVar(&cfg.MemoryVectorRerankEndpoint, "memory-vector-rerank-endpoint", cfg.MemoryVectorRerankEndpoint, "NVIDIA reranking endpoint base URL or /v1/ranking URL")
	flags.StringVar(&cfg.MemoryVectorRerankAPIKey, "memory-vector-rerank-api-key", cfg.MemoryVectorRerankAPIKey, "NVIDIA reranking API key")
	flags.StringVar(&cfg.MemoryVectorRerankModel, "memory-vector-rerank-model", cfg.MemoryVectorRerankModel, "NVIDIA reranking model for L2 episodic memory")
	flags.IntVar(&cfg.MemoryVectorRerankCandidateLimit, "memory-vector-rerank-candidate-limit", cfg.MemoryVectorRerankCandidateLimit, "number of L2 memory vector candidates to rerank")
	flags.IntVar(&cfg.MemoryVectorRerankResultLimit, "memory-vector-rerank-result-limit", cfg.MemoryVectorRerankResultLimit, "default number of L2 reranked memories to return when no search limit is requested")
	flags.DurationVar(&cfg.MemoryVectorRerankTimeout, "memory-vector-rerank-timeout", cfg.MemoryVectorRerankTimeout, "NVIDIA reranking request timeout")
	flags.StringVar(&cfg.MemoryVectorRerankTruncate, "memory-vector-rerank-truncate", cfg.MemoryVectorRerankTruncate, "NVIDIA reranking truncate policy: END or NONE")
	flags.BoolVar(&cfg.MemoryRecallEnabled, "memory-recall-enabled", cfg.MemoryRecallEnabled, "enable per-turn memory recall")
	flags.BoolVar(&cfg.MemoryRecallConditionalEnabled, "memory-recall-conditional-enabled", cfg.MemoryRecallConditionalEnabled, "skip per-turn memory recall for low-information messages")
	flags.BoolVar(&cfg.MemoryRecallAsyncEnabled, "memory-recall-async-enabled", cfg.MemoryRecallAsyncEnabled, "run per-turn memory recall with a bounded async timeout")
	flags.DurationVar(&cfg.MemoryRecallTimeout, "memory-recall-timeout", cfg.MemoryRecallTimeout, "maximum time to wait for per-turn memory recall before continuing without memory")
	flags.IntVar(&cfg.MemoryRecallMinQueryRunes, "memory-recall-min-query-runes", cfg.MemoryRecallMinQueryRunes, "minimum CJK query length that can trigger conditional memory recall")
	flags.IntVar(&cfg.MemoryRecallRecentContextMessages, "memory-recall-recent-context-messages", cfg.MemoryRecallRecentContextMessages, "number of recent messages used to enrich memory recall queries")
	flags.IntVar(&cfg.MemoryRecallRecentContextMaxRunes, "memory-recall-recent-context-runes", cfg.MemoryRecallRecentContextMaxRunes, "maximum runes of recent context used to enrich memory recall queries")
	flags.IntVar(&cfg.MemoryRecallForceInterval, "memory-recall-force-interval", cfg.MemoryRecallForceInterval, "force memory recall every N user turns; 0 disables interval forcing")
	flags.IntVar(&cfg.MemoryRecallComplexTokenThreshold, "memory-recall-complex-token-threshold", cfg.MemoryRecallComplexTokenThreshold, "estimated token count above which memory recall is forced")
	flags.BoolVar(&cfg.MemoryRecallEmbeddingEnabled, "memory-recall-embedding-enabled", cfg.MemoryRecallEmbeddingEnabled, "enable embedding-drift memory recall trigger")
	flags.Float64Var(&cfg.MemoryRecallEmbeddingSimilarityThreshold, "memory-recall-embedding-similarity-threshold", cfg.MemoryRecallEmbeddingSimilarityThreshold, "average recent-message cosine similarity below which memory recall triggers")
	flags.IntVar(&cfg.MemoryRecallEmbeddingWindow, "memory-recall-embedding-window", cfg.MemoryRecallEmbeddingWindow, "number of recent messages compared by the embedding-drift trigger")
	flags.BoolVar(&cfg.MemoryRecallIntentClassifierEnabled, "memory-recall-intent-classifier-enabled", cfg.MemoryRecallIntentClassifierEnabled, "enable L3 embedding-based zero-shot memory recall intent classifier")
	flags.Float64Var(&cfg.MemoryRecallIntentClassifierThreshold, "memory-recall-intent-classifier-threshold", cfg.MemoryRecallIntentClassifierThreshold, "minimum classifier similarity score that can trigger L3 memory recall")
	flags.IntVar(&cfg.MemoryRecallIntentClassifierContextTurns, "memory-recall-intent-classifier-context-turns", cfg.MemoryRecallIntentClassifierContextTurns, "number of recent messages included in the L3 intent classifier context")
	flags.BoolVar(&cfg.MemoryRecallQueryRewriteEnabled, "memory-recall-query-rewrite-enabled", cfg.MemoryRecallQueryRewriteEnabled, "enable deterministic query rewrite for per-turn memory recall")
	flags.BoolVar(&cfg.MemoryRecallLLMTriggerEnabled, "memory-recall-llm-trigger-enabled", cfg.MemoryRecallLLMTriggerEnabled, "enable low-cost sidecar LLM memory recall trigger fallback")
	flags.DurationVar(&cfg.MemoryRecallLLMTriggerTimeout, "memory-recall-llm-trigger-timeout", cfg.MemoryRecallLLMTriggerTimeout, "maximum time to wait for sidecar LLM memory recall trigger")
	flags.StringVar(&cfg.MemoryPolicyPath, "memory-policy-path", cfg.MemoryPolicyPath, "JSON/YAML memory policy file for extraction, safety, conflict, recall, and episode rules")
	flags.StringVar(&cfg.MemoryPolicyVersion, "memory-policy-version", cfg.MemoryPolicyVersion, "expected memory policy version; also labels the built-in default policy when no file is set")
	flags.DurationVar(&cfg.MemoryPolicyReloadInterval, "memory-policy-reload-interval", cfg.MemoryPolicyReloadInterval, "poll interval for hot-reloading the memory policy file; 0 disables reload")
	flags.BoolVar(&cfg.MemoryPolicyStrictEval, "memory-policy-strict-eval", cfg.MemoryPolicyStrictEval, "run built-in memory policy smoke eval at startup")
	flags.BoolVar(&cfg.EpisodicMemoryEnabled, "episodic-memory-enabled", cfg.EpisodicMemoryEnabled, "enable L2 episodic memory capture and recall")
	flags.BoolVar(&cfg.EpisodicMemoryCaptureEnabled, "episodic-memory-capture-enabled", cfg.EpisodicMemoryCaptureEnabled, "enable after-turn L2 episodic memory capture")
	flags.BoolVar(&cfg.EpisodicMemoryContextEnabled, "episodic-memory-context-enabled", cfg.EpisodicMemoryContextEnabled, "enable query-aware L2 episodic memory context injection")
	flags.IntVar(&cfg.EpisodicMemoryMinMessages, "episodic-memory-min-messages", cfg.EpisodicMemoryMinMessages, "minimum visible user/assistant messages before capturing an episode")
	flags.IntVar(&cfg.EpisodicMemoryMaxMessages, "episodic-memory-max-messages", cfg.EpisodicMemoryMaxMessages, "maximum recent visible messages included in an episode summary")
	flags.IntVar(&cfg.EpisodicMemoryInjectLimit, "episodic-memory-inject-limit", cfg.EpisodicMemoryInjectLimit, "maximum L2 episodic memory abstracts injected per turn")
	flags.DurationVar(&cfg.EpisodicMemoryTTL, "episodic-memory-ttl", cfg.EpisodicMemoryTTL, "default TTL for captured L2 episodic memories")
	flags.DurationVar(&cfg.EpisodicMemorySummarizeTimeout, "episodic-memory-summarize-timeout", cfg.EpisodicMemorySummarizeTimeout, "timeout for LLM episodic memory summarization")
	flags.StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "default working directory")
	flags.StringVar(&cfg.UserWorkspaceRoot, "user-workspace-root", cfg.UserWorkspaceRoot, "root directory for per-user sandboxed workspaces")
	flags.BoolVar(&cfg.AllowCustomWorkingDir, "allow-custom-working-dir", cfg.AllowCustomWorkingDir, "allow request-provided working_dir when no user workspace root is configured")
	flags.StringVar(&cfg.Timezone, "timezone", cfg.Timezone, "IANA timezone used for current date/time context; empty uses server local time")
	flags.StringVar(&cfg.Locale, "locale", cfg.Locale, "BCP 47 locale tag used for locale context; empty lets the model infer from the latest user message")
	flags.StringVar(&cfg.LLMProvider, "llm-provider", cfg.LLMProvider, "LLM provider: anthropic, openai, deepseek, nvidia, qwen, gemini, vertex, shortapi, custom, or simple")
	flags.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "LLM API key; env fallback depends on -llm-provider")
	flags.StringVar(&cfg.APIToken, "api-token", cfg.APIToken, "LLM bearer/OAuth token; env fallback depends on -llm-provider")
	flags.StringVar(&cfg.APIBaseURL, "api-base-url", cfg.APIBaseURL, "LLM API base URL; use with openai-compatible custom providers")
	flags.StringVar(&cfg.Model, "model", cfg.Model, "model to use; provider default if empty")
	flags.StringVar(&cfg.LLMFallbacks, "llm-fallbacks", cfg.LLMFallbacks, "comma-separated fallback LLM specs provider[:model], credentials resolved from env")
	flags.StringVar(&cfg.LLMModelRoutes, "llm-model-routes", cfg.LLMModelRoutes, "comma-separated model routes: default=model,chat=model,skill:skill-name=model")
	flags.IntVar(&cfg.LLMMaxAttempts, "llm-max-attempts", cfg.LLMMaxAttempts, "max LLM attempts across retries and fallbacks")
	flags.DurationVar(&cfg.LLMRetryBackoff, "llm-retry-backoff", cfg.LLMRetryBackoff, "base backoff between retry rounds")
	flags.DurationVar(&cfg.LLMChatTimeout, "llm-chat-timeout", cfg.LLMChatTimeout, "timeout for one chat LLM provider call")
	flags.DurationVar(&cfg.LLMSkillTimeout, "llm-skill-timeout", cfg.LLMSkillTimeout, "timeout for one skill/workflow LLM provider call")
	flags.IntVar(&cfg.LLMDailyTokenQuota, "llm-daily-token-quota", cfg.LLMDailyTokenQuota, "daily successful LLM token quota per user; 0 disables")
	flags.IntVar(&cfg.LLMDailyRequestQuota, "llm-daily-request-quota", cfg.LLMDailyRequestQuota, "daily successful LLM request quota per user; 0 disables")
	flags.Float64Var(&cfg.LLMDailyCostQuotaUSD, "llm-daily-cost-quota-usd", cfg.LLMDailyCostQuotaUSD, "daily estimated LLM cost quota per user in USD; 0 disables")
	flags.Float64Var(&cfg.LLMInputCostPerMillion, "llm-input-cost-per-million", cfg.LLMInputCostPerMillion, "estimated input token cost per 1M tokens")
	flags.Float64Var(&cfg.LLMOutputCostPerMillion, "llm-output-cost-per-million", cfg.LLMOutputCostPerMillion, "estimated output token cost per 1M tokens")
	flags.IntVar(&cfg.LLMFailureThreshold, "llm-failure-threshold", cfg.LLMFailureThreshold, "consecutive retryable failures before temporarily disabling a backend")
	flags.DurationVar(&cfg.LLMCircuitCooldown, "llm-circuit-cooldown", cfg.LLMCircuitCooldown, "duration to skip a backend after circuit breaker opens")
	flags.BoolVar(&cfg.LiveEnabled, "live-enabled", cfg.LiveEnabled, "enable live voice websocket mode")
	flags.StringVar(&cfg.LiveProvider, "live-provider", cfg.LiveProvider, "live provider: vertex or xai")
	flags.StringVar(&cfg.LiveModel, "live-model", cfg.LiveModel, "live voice model")
	flags.StringVar(&cfg.LiveVertexProjectID, "live-vertex-project-id", cfg.LiveVertexProjectID, "Google Cloud project ID for Vertex Live")
	flags.StringVar(&cfg.LiveVertexLocation, "live-vertex-location", cfg.LiveVertexLocation, "Vertex location for Gemini Live")
	flags.StringVar(&cfg.LiveVertexBaseURL, "live-vertex-base-url", cfg.LiveVertexBaseURL, "optional Vertex Live websocket/API base URL")
	flags.StringVar(&cfg.LiveVertexAPIVersion, "live-vertex-api-version", cfg.LiveVertexAPIVersion, "Vertex Live websocket API version")
	flags.StringVar(&cfg.LiveXAIAPIKey, "live-xai-api-key", cfg.LiveXAIAPIKey, "xAI API key for live voice; normally set XAI_API_KEY")
	flags.StringVar(&cfg.LiveXAIBaseURL, "live-xai-base-url", cfg.LiveXAIBaseURL, "xAI realtime websocket base URL")
	flags.StringVar(&cfg.LiveInputAudioMIME, "live-input-audio-mime-type", cfg.LiveInputAudioMIME, "input audio MIME type sent to live voice provider")
	flags.StringVar(&cfg.LiveOutputAudioMIME, "live-output-audio-mime-type", cfg.LiveOutputAudioMIME, "fallback output audio MIME type for live voice provider")
	flags.StringVar(&cfg.LiveVoiceName, "live-voice-name", cfg.LiveVoiceName, "live voice name; xAI example ara, Vertex examples Puck, Charon, Kore, Fenrir, Aoede, or Zephyr")
	flags.StringVar(&cfg.LiveLanguageCode, "live-language-code", cfg.LiveLanguageCode, "optional BCP-47 Live response language code, for example zh-CN or en-US")
	flags.BoolVar(&cfg.LiveInputTranscription, "live-input-transcription-enabled", cfg.LiveInputTranscription, "request input audio transcription from live voice provider")
	flags.BoolVar(&cfg.LiveOutputTranscription, "live-output-transcription-enabled", cfg.LiveOutputTranscription, "request output audio transcription from live voice provider")
	flags.StringVar(&cfg.LiveVADStartSensitivity, "live-vad-start-sensitivity", cfg.LiveVADStartSensitivity, "Gemini Live VAD speech start sensitivity")
	flags.StringVar(&cfg.LiveVADEndSensitivity, "live-vad-end-sensitivity", cfg.LiveVADEndSensitivity, "Gemini Live VAD speech end sensitivity")
	flags.Float64Var(&cfg.LiveVADThreshold, "live-vad-threshold", cfg.LiveVADThreshold, "xAI server VAD threshold")
	flags.DurationVar(&cfg.LiveVADPrefixPadding, "live-vad-prefix-padding", cfg.LiveVADPrefixPadding, "Live VAD detected speech duration before confirming speech start")
	flags.DurationVar(&cfg.LiveVADSilenceDuration, "live-vad-silence-duration", cfg.LiveVADSilenceDuration, "Live VAD silence duration before confirming speech end")
	flags.DurationVar(&cfg.LiveSessionTimeout, "live-session-timeout", cfg.LiveSessionTimeout, "max duration for one live voice websocket session")
	flags.StringVar(&cfg.LiveSetupPromptCacheBackend, "live-setup-prompt-cache-backend", cfg.LiveSetupPromptCacheBackend, "live setup prompt cache backend: memory, redis, or none")
	flags.StringVar(&cfg.LiveSetupPromptCacheRedisURL, "live-setup-prompt-cache-redis-url", cfg.LiveSetupPromptCacheRedisURL, "Redis URL for live setup prompt cache")
	flags.DurationVar(&cfg.LiveSetupPromptCacheTTL, "live-setup-prompt-cache-ttl", cfg.LiveSetupPromptCacheTTL, "live setup prompt cache TTL")
	flags.StringVar(&cfg.AuthMode, "auth-mode", cfg.AuthMode, "auth mode: auto, jwt, cookie, trusted-header, header, none")
	flags.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "optional bearer token required for API requests")
	flags.StringVar(&cfg.UserHeader, "user-header", cfg.UserHeader, "header containing authenticated consumer user ID")
	flags.StringVar(&cfg.JWTSecret, "jwt-secret", cfg.JWTSecret, "HS256 JWT secret")
	flags.StringVar(&cfg.JWTIssuer, "jwt-issuer", cfg.JWTIssuer, "expected JWT issuer")
	flags.StringVar(&cfg.JWTAudience, "jwt-audience", cfg.JWTAudience, "expected JWT audience")
	flags.StringVar(&cfg.JWTUserClaim, "jwt-user-claim", cfg.JWTUserClaim, "JWT claim containing consumer user ID")
	flags.BoolVar(&cfg.EnableUserSystem, "enable-user-system", cfg.EnableUserSystem, "enable built-in consumer user registration/login APIs")
	flags.DurationVar(&cfg.AuthAccessTTL, "auth-access-ttl", cfg.AuthAccessTTL, "access token TTL for built-in user system")
	flags.DurationVar(&cfg.AuthRefreshTTL, "auth-refresh-ttl", cfg.AuthRefreshTTL, "refresh token TTL for built-in user system")
	flags.BoolVar(&cfg.EmailVerificationRequired, "email-verification-required", cfg.EmailVerificationRequired, "require email verification before built-in users can log in")
	flags.DurationVar(&cfg.EmailVerificationTTL, "email-verification-ttl", cfg.EmailVerificationTTL, "email verification token TTL")
	flags.StringVar(&cfg.EmailProvider, "email-provider", cfg.EmailProvider, "email provider for auth emails: resend or empty")
	flags.StringVar(&cfg.EmailFrom, "email-from", cfg.EmailFrom, "from address for auth emails")
	flags.StringVar(&cfg.EmailPublicBaseURL, "email-public-base-url", cfg.EmailPublicBaseURL, "public base URL used in auth email links")
	flags.StringVar(&cfg.ResendAPIKey, "resend-api-key", cfg.ResendAPIKey, "Resend API key for auth emails")
	flags.StringVar(&cfg.ResendBaseURL, "resend-base-url", cfg.ResendBaseURL, "optional Resend API base URL")
	flags.StringVar(&cfg.SessionCookieName, "session-cookie-name", cfg.SessionCookieName, "signed JWT session cookie name")
	flags.StringVar(&cfg.SessionCookieSecret, "session-cookie-secret", cfg.SessionCookieSecret, "HS256 secret for session cookie JWT")
	flags.StringVar(&cfg.SessionCookieDomain, "session-cookie-domain", cfg.SessionCookieDomain, "optional domain for session and CSRF cookies")
	flags.BoolVar(&cfg.SessionCookieSecure, "session-cookie-secure", cfg.SessionCookieSecure, "set Secure on session and CSRF cookies")
	flags.StringVar(&cfg.SessionCookieSameSite, "session-cookie-samesite", cfg.SessionCookieSameSite, "cookie SameSite policy: lax, strict, none")
	flags.BoolVar(&cfg.CSRFEnabled, "csrf-enabled", cfg.CSRFEnabled, "enable double-submit CSRF protection for session-cookie requests")
	flags.StringVar(&cfg.CSRFCookieName, "csrf-cookie-name", cfg.CSRFCookieName, "CSRF token cookie name")
	flags.StringVar(&cfg.CSRFHeaderName, "csrf-header-name", cfg.CSRFHeaderName, "CSRF request header name")
	flags.StringVar(&cfg.CORSAllowedOrigins, "cors-allowed-origins", cfg.CORSAllowedOrigins, "comma-separated browser origins allowed for CORS")
	flags.BoolVar(&cfg.CORSAllowCredentials, "cors-allow-credentials", cfg.CORSAllowCredentials, "allow credentials for CORS allowlisted origins")
	flags.StringVar(&cfg.AdminToken, "admin-token", cfg.AdminToken, "shared token required for admin APIs")
	flags.BoolVar(&cfg.EvalDailyEnabled, "eval-daily-enabled", cfg.EvalDailyEnabled, "enable daily incremental agent evaluation at UTC+8 05:00")
	flags.IntVar(&cfg.EvalDailyHour, "eval-daily-hour", cfg.EvalDailyHour, "daily evaluation local hour in UTC+8")
	flags.IntVar(&cfg.EvalDailyMinute, "eval-daily-minute", cfg.EvalDailyMinute, "daily evaluation local minute in UTC+8")
	flags.StringVar(&cfg.EvalDailyUserIDs, "eval-daily-user-ids", cfg.EvalDailyUserIDs, "comma-separated user IDs for daily evaluation; empty uses active built-in users when available")
	flags.IntVar(&cfg.EvalDailyBatchLimit, "eval-daily-batch-limit", cfg.EvalDailyBatchLimit, "max users processed per daily evaluation pass")
	flags.DurationVar(&cfg.EvalDailyTimeout, "eval-daily-timeout", cfg.EvalDailyTimeout, "timeout for one daily evaluation pass")
	flags.BoolVar(&cfg.EvalJudgeEnabled, "eval-judge-enabled", cfg.EvalJudgeEnabled, "enable LLM-as-Judge for golden set evaluation runs")
	flags.StringVar(&cfg.EvalJudgeModel, "eval-judge-model", cfg.EvalJudgeModel, "model used by LLM-as-Judge; also supports AGENT_API_LLM_MODEL_ROUTES judge=<model>")
	flags.StringVar(&cfg.EvalJudgePromptVersion, "eval-judge-prompt-version", cfg.EvalJudgePromptVersion, "prompt version label recorded by LLM-as-Judge evaluation results")
	flags.StringVar(&cfg.TrustedUserHeader, "trusted-user-header", cfg.TrustedUserHeader, "trusted gateway user ID header")
	flags.StringVar(&cfg.TrustedSecretHeader, "trusted-secret-header", cfg.TrustedSecretHeader, "header required for trusted-header auth")
	flags.StringVar(&cfg.TrustedSecret, "trusted-secret", cfg.TrustedSecret, "secret value required for trusted-header auth")
	flags.BoolVar(&cfg.AllowDangerousTools, "allow-dangerous-tools", cfg.AllowDangerousTools, "enable write/edit/bash tools and write/execute permissions")
	flags.StringVar(&cfg.NetworkAllowlist, "network-allowlist", cfg.NetworkAllowlist, "comma-separated domains allowed for WebFetch/WebSearch; empty disables app-level web allowlist")
	flags.StringVar(&cfg.PluginDir, "plugin-dir", cfg.PluginDir, "directory containing plugin manifests")
	flags.StringVar(&cfg.SkillDirs, "skill-dirs", cfg.SkillDirs, "comma-separated directories containing skill-name/SKILL.md folders")
	flags.StringVar(&cfg.MCPServersJSON, "mcp-servers", cfg.MCPServersJSON, "JSON array or object describing MCP servers")
	flags.StringVar(&cfg.RateLimitBackend, "rate-limit-backend", cfg.RateLimitBackend, "rate limit backend: memory, redis, gateway, none")
	flags.IntVar(&cfg.RateLimit, "rate-limit", cfg.RateLimit, "max requests per user per minute")
	flags.StringVar(&cfg.OperationRateLimits, "operation-rate-limits", cfg.OperationRateLimits, "comma-separated operation rate limits such as chat_message=60/m,job_create=20/m,data_export=5/h")
	flags.StringVar(&cfg.CacheBackend, "cache-backend", cfg.CacheBackend, "default cache backend: memory, redis, or none")
	flags.StringVar(&cfg.CacheRedisURL, "cache-redis-url", cfg.CacheRedisURL, "default Redis URL for cache-backed services")
	flags.StringVar(&cfg.CachePrefix, "cache-prefix", cfg.CachePrefix, "default Redis key prefix for the unified cache store")
	flags.DurationVar(&cfg.CacheDefaultTTL, "cache-default-ttl", cfg.CacheDefaultTTL, "default TTL for unified cache entries")
	flags.BoolVar(&cfg.CacheFailOpen, "cache-fail-open", cfg.CacheFailOpen, "treat cache backend errors as misses for typed cache callers")
	flags.StringVar(&cfg.RedisURL, "redis-url", cfg.RedisURL, "Redis URL for distributed rate limiting")
	flags.BoolVar(&cfg.RedisFailOpen, "redis-fail-open", cfg.RedisFailOpen, "allow requests when Redis rate limit is unavailable")
	flags.StringVar(&cfg.MessageContextCacheBackend, "message-context-cache-backend", cfg.MessageContextCacheBackend, "message context cache backend: memory, redis, or none")
	flags.StringVar(&cfg.MessageContextCacheRedisURL, "message-context-cache-redis-url", cfg.MessageContextCacheRedisURL, "Redis URL for message context cache")
	flags.DurationVar(&cfg.MessageContextCacheTTL, "message-context-cache-ttl", cfg.MessageContextCacheTTL, "message context cache TTL")
	flags.StringVar(&cfg.SessionListCacheBackend, "session-list-cache-backend", cfg.SessionListCacheBackend, "session list cache backend: redis or none")
	flags.StringVar(&cfg.SessionListCacheRedisURL, "session-list-cache-redis-url", cfg.SessionListCacheRedisURL, "Redis URL for session list zset pagination cache")
	flags.DurationVar(&cfg.SessionListCacheTTL, "session-list-cache-ttl", cfg.SessionListCacheTTL, "session list cache TTL")
	flags.StringVar(&cfg.MessageSequenceBackend, "message-sequence-backend", cfg.MessageSequenceBackend, "message seq_no allocator backend: redis or sql")
	flags.StringVar(&cfg.MessageSequenceRedisURL, "message-sequence-redis-url", cfg.MessageSequenceRedisURL, "Redis URL for message seq_no allocator")
	flags.StringVar(&cfg.MessageEventsBackend, "message-events-backend", cfg.MessageEventsBackend, "message event backend: local, kafka, dual, or none")
	flags.StringVar(&cfg.MessageEventsKafkaBrokers, "message-events-kafka-brokers", cfg.MessageEventsKafkaBrokers, "comma-separated Kafka brokers for message events")
	flags.StringVar(&cfg.MessageEventsKafkaTopic, "message-events-kafka-topic", cfg.MessageEventsKafkaTopic, "Kafka topic for message events")
	flags.StringVar(&cfg.MessageEventsKafkaClientID, "message-events-kafka-client-id", cfg.MessageEventsKafkaClientID, "Kafka client ID for message events")
	flags.BoolVar(&cfg.MessageEventsKafkaConsumerEnabled, "message-events-kafka-consumer-enabled", cfg.MessageEventsKafkaConsumerEnabled, "consume Kafka message events with the built-in worker")
	flags.StringVar(&cfg.MessageEventsKafkaConsumerGroup, "message-events-kafka-consumer-group", cfg.MessageEventsKafkaConsumerGroup, "Kafka consumer group for the built-in message event worker")
	flags.StringVar(&cfg.MessageEventsKafkaDLQTopic, "message-events-kafka-dlq-topic", cfg.MessageEventsKafkaDLQTopic, "optional Kafka dead-letter topic for failed message events")
	flags.IntVar(&cfg.MessageEventsKafkaRetryAttempts, "message-events-kafka-retry-attempts", cfg.MessageEventsKafkaRetryAttempts, "Kafka message event consumer retry attempts")
	flags.DurationVar(&cfg.MessageEventsKafkaRetryBackoff, "message-events-kafka-retry-backoff", cfg.MessageEventsKafkaRetryBackoff, "Kafka message event consumer retry backoff")
	flags.DurationVar(&cfg.MessageEventsKafkaProcessTimeout, "message-events-kafka-process-timeout", cfg.MessageEventsKafkaProcessTimeout, "Kafka message event processing timeout")
	flags.StringVar(&cfg.MessageEventsProcessedLockBackend, "message-events-processed-lock-backend", cfg.MessageEventsProcessedLockBackend, "message event processed lock backend: redis or none")
	flags.StringVar(&cfg.MessageEventsProcessedLockRedisURL, "message-events-processed-lock-redis-url", cfg.MessageEventsProcessedLockRedisURL, "Redis URL for Kafka message event processed locks")
	flags.DurationVar(&cfg.MessageEventsProcessedLockTTL, "message-events-processed-lock-ttl", cfg.MessageEventsProcessedLockTTL, "message event processed lock TTL")
	flags.StringVar(&cfg.JobQueueRedisURL, "job-queue-redis-url", cfg.JobQueueRedisURL, "Redis URL for durable job execution queue")
	flags.StringVar(&cfg.JobQueueStream, "job-queue-stream", cfg.JobQueueStream, "Redis stream name for durable job execution queue")
	flags.StringVar(&cfg.JobQueueConsumerGroup, "job-queue-consumer-group", cfg.JobQueueConsumerGroup, "Redis stream consumer group for job workers")
	flags.StringVar(&cfg.JobQueueConsumer, "job-queue-consumer", cfg.JobQueueConsumer, "Redis stream consumer name; default is hostname-pid")
	flags.DurationVar(&cfg.JobQueueBlockTimeout, "job-queue-block-timeout", cfg.JobQueueBlockTimeout, "Redis job queue blocking read timeout")
	flags.DurationVar(&cfg.JobQueueClaimIdle, "job-queue-claim-idle", cfg.JobQueueClaimIdle, "pending job idle age before another worker can claim it")
	flags.DurationVar(&cfg.JobQueueLockTTL, "job-queue-lock-ttl", cfg.JobQueueLockTTL, "Redis job execution lock TTL")
	flags.BoolVar(&cfg.JobWorkerEnabled, "job-worker-enabled", cfg.JobWorkerEnabled, "run the built-in durable job worker")
	flags.BoolVar(&cfg.JobEventStreamEnabled, "job-event-stream-enabled", cfg.JobEventStreamEnabled, "buffer job events in Redis Streams for resumable SSE subscribers")
	flags.StringVar(&cfg.JobEventStreamPrefix, "job-event-stream-prefix", cfg.JobEventStreamPrefix, "Redis key prefix for resumable job event streams")
	flags.DurationVar(&cfg.JobEventStreamTTL, "job-event-stream-ttl", cfg.JobEventStreamTTL, "TTL for per-job Redis event streams")
	flags.IntVar(&cfg.JobEventStreamMaxLen, "job-event-stream-max-len", cfg.JobEventStreamMaxLen, "approximate max entries retained per job event stream")
	flags.BoolVar(&cfg.ChatEventStreamEnabled, "chat-event-stream-enabled", cfg.ChatEventStreamEnabled, "buffer normal chat run events in Redis Streams for resumable SSE subscribers")
	flags.StringVar(&cfg.ChatEventStreamPrefix, "chat-event-stream-prefix", cfg.ChatEventStreamPrefix, "Redis key prefix for resumable chat run event streams")
	flags.DurationVar(&cfg.ChatEventStreamTTL, "chat-event-stream-ttl", cfg.ChatEventStreamTTL, "TTL for per-chat-run Redis event streams")
	flags.IntVar(&cfg.ChatEventStreamMaxLen, "chat-event-stream-max-len", cfg.ChatEventStreamMaxLen, "approximate max entries retained per chat run event stream")
	flags.DurationVar(&cfg.ChatEventStreamBlock, "chat-event-stream-block", cfg.ChatEventStreamBlock, "Redis blocking read duration for chat run event streams")
	flags.BoolVar(&cfg.JobEventFanoutEnabled, "job-event-fanout-enabled", cfg.JobEventFanoutEnabled, "broadcast job events through Redis pub/sub for multi-instance realtime streams")
	flags.StringVar(&cfg.JobEventFanoutChannel, "job-event-fanout-channel", cfg.JobEventFanoutChannel, "Redis pub/sub channel for multi-instance job event fanout")
	flags.StringVar(&cfg.JobEventFanoutOrigin, "job-event-fanout-origin", cfg.JobEventFanoutOrigin, "job event fanout origin id; default is hostname-pid-random")
	flags.StringVar(&cfg.WebPushVAPIDPublicKey, "web-push-vapid-public-key", cfg.WebPushVAPIDPublicKey, "VAPID public key for browser push notifications")
	flags.StringVar(&cfg.WebPushVAPIDPrivateKey, "web-push-vapid-private-key", cfg.WebPushVAPIDPrivateKey, "VAPID private key for browser push notifications")
	flags.StringVar(&cfg.WebPushVAPIDSubject, "web-push-vapid-subject", cfg.WebPushVAPIDSubject, "VAPID subscriber subject, usually mailto:admin@example.com or app URL")
	flags.IntVar(&cfg.WebPushTTLSeconds, "web-push-ttl-seconds", cfg.WebPushTTLSeconds, "browser push notification TTL in seconds")
	flags.BoolVar(&cfg.MessageAttachmentWorkerEnabled, "message-attachment-worker-enabled", cfg.MessageAttachmentWorkerEnabled, "enable async message attachment processing worker")
	flags.IntVar(&cfg.MessageAttachmentWorkerBatchSize, "message-attachment-worker-batch-size", cfg.MessageAttachmentWorkerBatchSize, "message attachment worker batch size")
	flags.DurationVar(&cfg.MessageAttachmentWorkerPollInterval, "message-attachment-worker-poll-interval", cfg.MessageAttachmentWorkerPollInterval, "message attachment worker poll interval")
	flags.DurationVar(&cfg.MessageAttachmentWorkerProcessTimeout, "message-attachment-worker-process-timeout", cfg.MessageAttachmentWorkerProcessTimeout, "message attachment worker per-attachment timeout")
	flags.IntVar(&cfg.MessageAttachmentThumbnailMaxDimension, "message-attachment-thumbnail-max-dimension", cfg.MessageAttachmentThumbnailMaxDimension, "max width or height for generated attachment thumbnails")
	flags.BoolVar(&cfg.MessageArchiveWorkerEnabled, "message-archive-worker-enabled", cfg.MessageArchiveWorkerEnabled, "enable async archive of old SQL messages into the artifact object store")
	flags.DurationVar(&cfg.MessageArchiveAfter, "message-archive-after", cfg.MessageArchiveAfter, "archive SQL message payloads older than this duration")
	flags.IntVar(&cfg.MessageArchiveWorkerBatchSize, "message-archive-worker-batch-size", cfg.MessageArchiveWorkerBatchSize, "message archive worker batch size")
	flags.DurationVar(&cfg.MessageArchiveWorkerPollInterval, "message-archive-worker-poll-interval", cfg.MessageArchiveWorkerPollInterval, "message archive worker poll interval")
	flags.DurationVar(&cfg.MessageArchiveWorkerProcessTimeout, "message-archive-worker-process-timeout", cfg.MessageArchiveWorkerProcessTimeout, "message archive worker per-message timeout")
	flags.StringVar(&cfg.MessageArchivePrefix, "message-archive-prefix", cfg.MessageArchivePrefix, "object-store prefix for archived message payloads")
	flags.BoolVar(&cfg.MessageArchiveClearPGPayload, "message-archive-clear-pg-payload", cfg.MessageArchiveClearPGPayload, "clear large SQL message payload fields after archive upload succeeds")
	flags.IntVar(&cfg.RetentionDays, "retention-days", cfg.RetentionDays, "delete sessions and memory older than this many days on startup; 0 disables")
	flags.DurationVar(&cfg.LocalArtifactStagingRetention, "local-artifact-staging-retention", cfg.LocalArtifactStagingRetention, "delete local generated artifact staging files older than this after object-store upload; 0 disables")
	flags.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout, "max time to drain HTTP requests and active agent work after SIGINT/SIGTERM")
	flags.DurationVar(&cfg.RequestTimeout, "request-timeout", cfg.RequestTimeout, "max duration for non-upgrade HTTP requests; 0 disables")
	flags.DurationVar(&cfg.TurnTimeout, "turn-timeout", cfg.TurnTimeout, "max duration for one agent turn")
	flags.BoolVar(&cfg.DeepAgentV2Enabled, "deep-agent-v2-enabled", cfg.DeepAgentV2Enabled, "enable DeepAgent v2 route metadata and executor adapter behavior")
	flags.BoolVar(&cfg.DeepAgentV2ShadowRoute, "deep-agent-v2-shadow-route", cfg.DeepAgentV2ShadowRoute, "record DeepAgent legacy/new route diffs without changing execution")
	flags.BoolVar(&cfg.DeepResearchOrchestratorWorkerEnabled, "deep-research-orchestrator-worker-enabled", cfg.DeepResearchOrchestratorWorkerEnabled, "enable orchestrator-worker execution for plan_execute deep research jobs")
	flags.StringVar(&cfg.DeepResearchWorkerBackend, "deep-research-worker-backend", cfg.DeepResearchWorkerBackend, "deep research worker backend: inline or harness_agent")
	flags.IntVar(&cfg.DeepResearchMaxWorkers, "deep-research-max-workers", cfg.DeepResearchMaxWorkers, "maximum deep research worker nodes per run")
	flags.IntVar(&cfg.DeepResearchMaxConcurrency, "deep-research-max-concurrency", cfg.DeepResearchMaxConcurrency, "maximum concurrent deep research workers")
	flags.DurationVar(&cfg.DeepResearchWorkerTimeout, "deep-research-worker-timeout", cfg.DeepResearchWorkerTimeout, "timeout per deep research worker")
	flags.DurationVar(&cfg.DeepResearchTotalTimeout, "deep-research-total-timeout", cfg.DeepResearchTotalTimeout, "total timeout for a deep research run")
	flags.IntVar(&cfg.DeepResearchMaxRetries, "deep-research-max-retries", cfg.DeepResearchMaxRetries, "maximum retries per failed deep research worker")
	flags.BoolVar(&cfg.DeepResearchReplanEnabled, "deep-research-replan-enabled", cfg.DeepResearchReplanEnabled, "allow the Deep Research orchestrator to revise the remaining task graph from execution evidence")
	flags.IntVar(&cfg.DeepResearchMaxReplans, "deep-research-max-replans", cfg.DeepResearchMaxReplans, "maximum execution-time Deep Research replan attempts")
	flags.IntVar(&cfg.DeepResearchReplanEveryBatches, "deep-research-replan-every-batches", cfg.DeepResearchReplanEveryBatches, "successful worker batches between adaptive Deep Research replan checkpoints")
	flags.BoolVar(&cfg.DeepResearchFallbackLegacy, "deep-research-fallback-legacy", cfg.DeepResearchFallbackLegacy, "fallback to legacy DeepAgent when orchestrator-worker planning fails before workers start")
	flags.BoolVar(&cfg.DeepResearchRequireSources, "deep-research-require-sources", cfg.DeepResearchRequireSources, "require sourced evidence for deep research aggregation")
	flags.IntVar(&cfg.DeepResearchMinSuccessfulWorkers, "deep-research-min-successful-workers", cfg.DeepResearchMinSuccessfulWorkers, "minimum successful workers required before aggregation")
	flags.BoolVar(&cfg.LoopAutomationEnabled, "loop-automation-enabled", cfg.LoopAutomationEnabled, "enable automatic loop discovery triggers")
	flags.BoolVar(&cfg.LoopScheduleTriggersEnabled, "loop-schedule-triggers-enabled", cfg.LoopScheduleTriggersEnabled, "enable scheduled loop discovery triggers")
	flags.BoolVar(&cfg.LoopWebhookTriggersEnabled, "loop-webhook-triggers-enabled", cfg.LoopWebhookTriggersEnabled, "enable webhook loop discovery triggers")
	flags.BoolVar(&cfg.LoopMonitorTriggersEnabled, "loop-monitor-triggers-enabled", cfg.LoopMonitorTriggersEnabled, "enable monitor loop discovery triggers")
	flags.BoolVar(&cfg.LoopEvalRepairTriggersEnabled, "loop-eval-repair-triggers-enabled", cfg.LoopEvalRepairTriggersEnabled, "enable evaluation failure repair loop triggers")
	flags.BoolVar(&cfg.LoopConnectorTriggersEnabled, "loop-connector-triggers-enabled", cfg.LoopConnectorTriggersEnabled, "enable connector event loop discovery triggers")
	flags.DurationVar(&cfg.LoopTriggerTTL, "loop-trigger-ttl", cfg.LoopTriggerTTL, "retention TTL for loop trigger dedupe ledger rows")
	flags.DurationVar(&cfg.SkillShellTimeout, "skill-shell-timeout", cfg.SkillShellTimeout, "max duration for skill frontmatter shell commands")
	flags.StringVar(&cfg.SkillShellRunner, "skill-shell-runner", cfg.SkillShellRunner, "skill shell runner: local or docker")
	flags.StringVar(&cfg.SkillSandboxImage, "skill-sandbox-image", cfg.SkillSandboxImage, "Docker image for skill shell runner=docker")
	flags.StringVar(&cfg.SkillSandboxNetwork, "skill-sandbox-network", cfg.SkillSandboxNetwork, "Docker network mode for skill shell runner=docker; use none by default or bridge when a skill needs egress")
	flags.StringVar(&cfg.SkillSandboxMemory, "skill-sandbox-memory", cfg.SkillSandboxMemory, "Docker memory limit for skill shell runner=docker")
	flags.StringVar(&cfg.SkillSandboxCPUs, "skill-sandbox-cpus", cfg.SkillSandboxCPUs, "Docker CPU quota for skill shell runner=docker")
	flags.IntVar(&cfg.SkillSandboxPidsLimit, "skill-sandbox-pids-limit", cfg.SkillSandboxPidsLimit, "Docker pids limit for skill shell runner=docker")
	flags.StringVar(&cfg.SkillSandboxTmpfsSize, "skill-sandbox-tmpfs-size", cfg.SkillSandboxTmpfsSize, "Docker /tmp tmpfs size for skill shell runner=docker")
	flags.StringVar(&cfg.SkillSandboxPrepullImages, "skill-sandbox-prepull-images", cfg.SkillSandboxPrepullImages, "comma-separated Docker images to pre-pull for skill shell runner=docker")
	flags.IntVar(&cfg.SkillSandboxWarmPoolSize, "skill-sandbox-warm-pool-size", cfg.SkillSandboxWarmPoolSize, "warm Docker containers per sandbox image for runner=docker when agentapi runs in Docker")
}

// Validate checks only cross-cutting startup configuration invariants.
// Deeper dependency-specific validation stays near the constructors that own those dependencies.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("config is required")
	}
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("addr is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data-dir is required")
	}
	if strings.TrimSpace(c.MCPServersJSON) != "" {
		servers, err := parseMCPServersJSON(c.MCPServersJSON)
		if err != nil {
			return err
		}
		c.MCPServers = servers
	} else if c.MCPServers == nil {
		c.MCPServers = []appconfig.MCPServerConfig{}
	}
	for i, server := range c.MCPServers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("mcp-servers[%d].name is required", i)
		}
	}
	return nil
}

func parseMCPServersJSON(raw string) ([]appconfig.MCPServerConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []appconfig.MCPServerConfig{}, nil
	}
	var list []appconfig.MCPServerConfig
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return normalizeMCPServers(list), nil
	}
	var single appconfig.MCPServerConfig
	if err := json.Unmarshal([]byte(raw), &single); err == nil {
		return normalizeMCPServers([]appconfig.MCPServerConfig{single}), nil
	}
	return nil, fmt.Errorf("mcp-servers must be a JSON array or object")
}

func normalizeMCPServers(servers []appconfig.MCPServerConfig) []appconfig.MCPServerConfig {
	if len(servers) == 0 {
		return []appconfig.MCPServerConfig{}
	}
	return append([]appconfig.MCPServerConfig(nil), servers...)
}

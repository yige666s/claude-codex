package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/engine"
	mcpcore "claude-codex/internal/harness/mcp"
	providerbackend "claude-codex/internal/harness/provider"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	skilltool "claude-codex/internal/harness/tools/skill"
	publictypes "claude-codex/internal/public/types"
)

const (
	memoryInjectedKey           = "agentruntime.memory_context_injected"
	consumerSecurityInjectedKey = "agentruntime.consumer_security_context_injected"
	workspaceContextAckContent  = "Understood. I have the workspace context."
	liveSkillSelectionTimeout   = 8 * time.Second
	liveWebResearchTimeout      = 75 * time.Second
	afterTurnMemoryTimeout      = 30 * time.Second
)

type hiddenUserMessageContextKey struct{}

type liveHarnessToolRunner interface {
	Runner
	Descriptors() []toolkit.Descriptor
	ExecuteTool(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error)
}

var liveHarnessToolAllowlist = map[string]struct{}{
	"WebSearch": {},
	"WebFetch":  {},
}

const liveWebResearchFunctionName = "web_research"

const consumerSecuritySystemContext = `<consumer-security>
You are serving a consumer web user. Do not expose internal server tools, tool names, file paths, workspace paths, shell commands, environment variables, credentials, stack traces, or raw provider errors.

Never claim that you can read local files, list project files, search server file contents, create arbitrary files, edit files, run shell commands, or inspect the server filesystem for the user. These are internal infrastructure capabilities, not user-facing product features.

If the user asks for local filesystem access, source-code search, arbitrary file creation/editing, shell execution, secrets, env vars, or server paths, politely refuse and offer safe alternatives: ask them to upload the file, use a published user-facing skill, or generate an artifact only through an approved skill flow.

Only describe published product skills and user-visible artifact/attachment flows. Do not mention hidden tools or implementation details.
</consumer-security>`

var ErrSessionNotRunning = errors.New("session is not running")
var ErrRuntimeShuttingDown = errors.New("runtime is shutting down")

type Runtime struct {
	config            RuntimeConfig
	sessions          SessionStore
	messageWriter     *MessageWriteService
	sessionLoader     *SessionLoadService
	contextCompactor  *ContextCompactionService
	messageSearch     *MessageSearchService
	messageCache      SessionContextCache
	messagePublisher  MessageEventPublisher
	live              *VertexLiveService
	vectorIndexer     *AsyncMessageVectorIndexPublisher
	localVectorIndex  bool
	memory            MemoryService
	episodeMemory     MemoryEpisodeService
	memoryExtract     MemoryExtractor
	memoryAbstract    MemoryAbstractor
	memoryOrganizer   MemoryOrganizer
	memoryRecall      *MemoryRecallDecider
	episodeSummarizer MemoryEpisodeSummarizer
	artifacts         *ArtifactService
	assetInsights     AssetInsightStore
	jobs              JobStore
	loopGoals         LoopGoalStore
	loopTriggers      LoopTriggerStore
	jobQueue          JobQueue
	jobEventFanout    JobEventPublisher
	jobEvents         JobEventBus
	skills            SkillCatalog
	skillExecutions   SkillExecutionStore
	workflowStore     WorkflowStore
	deepAgentEvidence DeepAgentEvidenceRepository
	toolCallLedger    ToolCallLedgerStore
	promptStore       PromptStore
	promptResolver    PromptResolver
	connectors        ConnectorStore
	connectorTokens   ConnectorTokenVault
	mcpConnectors     MCPConnectorStore
	mcpHost           mcpcore.Host
	engineFactory     EngineFactory
	riskScanner       RiskScanner
	riskRecorder      func(context.Context, RiskEvent)
	triggerQuotaCheck func(context.Context, LoopTriggerRequest) error
	loopTriggerPolicy LoopTriggerPolicy
	logger            *slog.Logger
	clock             Clock

	mu                    sync.Mutex
	wg                    sync.WaitGroup
	running               map[string]context.CancelFunc
	runningJobTurns       map[string]bool
	runningJobs           map[string]context.CancelFunc
	connectorRefreshLocks map[string]*sync.Mutex
	hiddenJobUserMessages map[string]bool
	loopTriggersByJob     map[string]LoopTriggerRecord
	shuttingDown          bool
}

func (r *Runtime) SetArtifactService(artifacts *ArtifactService) {
	r.artifacts = artifacts
}

func (r *Runtime) SetJobStore(jobs JobStore) {
	r.jobs = jobs
}

func (r *Runtime) SetLoopGoalStore(store LoopGoalStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryLoopGoalStore()
	}
	r.loopGoals = store
}

func (r *Runtime) SetLoopTriggerStore(store LoopTriggerStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryLoopTriggerStore()
	}
	r.loopTriggers = store
}

func (r *Runtime) SetLoopTriggerQuotaChecker(check func(context.Context, LoopTriggerRequest) error) {
	if r == nil {
		return
	}
	r.triggerQuotaCheck = check
}

func (r *Runtime) LoopTriggerLedgerStats(ctx context.Context, since time.Time) (LoopTriggerLedgerStats, error) {
	if r == nil || r.loopTriggers == nil {
		return LoopTriggerLedgerStats{}, nil
	}
	return r.loopTriggers.LoopTriggerLedgerStats(ctx, since)
}

func (r *Runtime) SetJobQueue(queue JobQueue) {
	r.jobQueue = queue
}

func (r *Runtime) SetJobEventFanout(fanout JobEventPublisher) {
	r.jobEventFanout = fanout
}

func (r *Runtime) SetSkillExecutionStore(store SkillExecutionStore) {
	r.skillExecutions = store
}

func (r *Runtime) SetWorkflowStore(store WorkflowStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	r.workflowStore = store
	if memoryWorkflow, ok := r.memory.(*MemoryWorkflowService); ok {
		r.memory = NewMemoryWorkflowService(memoryWorkflow.base, store, ContextWorkflowEventSink{})
	}
	if r.messageSearch != nil {
		r.messageSearch.SetWorkflowStore(store, ContextWorkflowEventSink{})
	}
}

func (r *Runtime) SetDeepAgentEvidenceRepository(repo DeepAgentEvidenceRepository) {
	if r == nil {
		return
	}
	r.deepAgentEvidence = repo
}

func (r *Runtime) SetToolCallLedgerStore(store ToolCallLedgerStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryToolCallLedgerStore()
	}
	r.toolCallLedger = store
}

func (r *Runtime) SetPromptStore(store PromptStore) {
	if r == nil {
		return
	}
	r.promptStore = store
	r.promptResolver = NewCachedPromptResolver(store, nil, r.config.CacheStore, r.config.CacheDefaultTTL, r.config.CacheFailOpen, r.config.CacheMetrics)
}

func (r *Runtime) SetRiskScanner(scanner RiskScanner) {
	r.riskScanner = scanner
}

func (r *Runtime) SetRiskRecorder(recorder func(context.Context, RiskEvent)) {
	r.riskRecorder = recorder
}

func (r *Runtime) MaxAssetBytes() int64 {
	if r == nil || r.artifacts == nil {
		return DefaultMaxAssetBytes
	}
	return r.artifacts.MaxBytes()
}

func NewRuntime(config RuntimeConfig, sessions SessionStore, memory MemoryService, skills SkillCatalog, engineFactory EngineFactory) *Runtime {
	if config.TurnTimeout <= 0 {
		config.TurnTimeout = 2 * time.Minute
	}
	if config.SkillShellTimeout <= 0 {
		config.SkillShellTimeout = 90 * time.Second
	}
	config.MessageSearch.CacheStore = config.CacheStore
	config.MessageSearch.CacheMetrics = config.CacheMetrics
	config.MessageSearch.CacheDefaultTTL = config.CacheDefaultTTL
	config.MessageSearch.CacheFailOpen = config.CacheFailOpen
	config.MemoryVector.CacheStore = config.CacheStore
	config.MemoryVector.CacheMetrics = config.CacheMetrics
	config.MemoryVector.CacheDefaultTTL = config.CacheDefaultTTL
	config.MemoryVector.CacheFailOpen = config.CacheFailOpen
	config.SkillShellSandbox = config.SkillShellSandbox.normalized()
	config.MemoryRecall = normalizeMemoryRecallConfig(config.MemoryRecall)
	config.EpisodicMemory = normalizeEpisodicMemoryConfig(config.EpisodicMemory)
	logger := componentLogger(config.Logger, "runtime")
	runtime := &Runtime{
		config:                config,
		sessions:              sessions,
		memory:                memory,
		episodeMemory:         memoryEpisodeServiceFrom(memory),
		memoryExtract:         NewRuleMemoryExtractor(),
		memoryAbstract:        NewRuleMemoryAbstractor(),
		memoryOrganizer:       NewRuleMemoryOrganizer(),
		memoryRecall:          NewMemoryRecallDecider(config.MemoryRecall, memoryRecallEmbedderFromConfig(config), componentLogger(logger, "memory_recall"), engineFactory),
		episodeSummarizer:     RuleMemoryEpisodeSummarizer{},
		skills:                skills,
		loopGoals:             NewMemoryLoopGoalStore(),
		loopTriggers:          NewMemoryLoopTriggerStore(),
		workflowStore:         NewMemoryWorkflowStore(),
		toolCallLedger:        NewMemoryToolCallLedgerStore(),
		connectors:            NewMemoryConnectorStore(),
		connectorTokens:       NewMemoryConnectorTokenVault(),
		mcpConnectors:         NewMemoryMCPConnectorStore(),
		mcpHost:               mcpcore.NewRuntimeHost(nil),
		promptResolver:        NewPromptResolver(nil, nil),
		engineFactory:         engineFactory,
		logger:                logger,
		clock:                 systemClock{},
		running:               make(map[string]context.CancelFunc),
		runningJobTurns:       make(map[string]bool),
		runningJobs:           make(map[string]context.CancelFunc),
		connectorRefreshLocks: make(map[string]*sync.Mutex),
		hiddenJobUserMessages: make(map[string]bool),
		loopTriggersByJob:     make(map[string]LoopTriggerRecord),
		jobEvents:             NewLocalJobEventBus(128),
		localVectorIndex:      true,
	}
	if memory != nil {
		memory = NewMemoryVectorService(memory, config.MemoryVector, componentLogger(logger, "memory_vector"))
		runtime.memory = NewMemoryWorkflowService(memory, runtime.workflowStore, ContextWorkflowEventSink{})
		runtime.episodeMemory = memoryEpisodeServiceFrom(runtime.memory)
	}
	if !config.EpisodicMemory.Enabled {
		runtime.episodeMemory = nil
		runtime.episodeSummarizer = nil
	}
	if _, ok := sessions.(MessageRepository); ok {
		if metaStore, ok := sessions.(MessageEmbeddingMetaStore); ok && messageVectorIndexingEnabled(config.MessageSearch) {
			indexer := NewQdrantMessageVectorIndexer(config.MessageSearch, metaStore)
			runtime.vectorIndexer = NewAsyncMessageVectorIndexPublisherWithLogger(indexer, defaultMessageVectorIndexWorkers, defaultMessageVectorIndexQueueSize, componentLogger(logger, "message_vector_index"))
		}
		runtime.SetMessageContextCache(NewMemorySessionContextCache())
	}
	if searchStore, ok := sessions.(MessageSearchStore); ok {
		runtime.messageSearch = NewMessageSearchService(config.MessageSearch, searchStore)
	}
	if config.Live.Enabled {
		runtime.live = NewVertexLiveService(config.Live, runtime, nil)
	}
	return runtime
}

func (r *Runtime) connectorRefreshLock(key string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.connectorRefreshLocks == nil {
		r.connectorRefreshLocks = make(map[string]*sync.Mutex)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "default"
	}
	lock := r.connectorRefreshLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		r.connectorRefreshLocks[key] = lock
	}
	return lock
}

func (r *Runtime) SetLogger(logger *slog.Logger) {
	if r == nil {
		return
	}
	r.logger = componentLogger(logger, "runtime")
}

func (r *Runtime) SetMessageWriteService(service *MessageWriteService) {
	r.messageWriter = service
}

func (r *Runtime) SetMessageEventPublisher(publisher MessageEventPublisher) {
	if r == nil {
		return
	}
	r.messagePublisher = publisher
	r.configureMessageServices()
}

func (r *Runtime) SetLocalMessageVectorIndexing(enabled bool) {
	if r == nil {
		return
	}
	r.localVectorIndex = enabled
	r.configureMessageServices()
}

func (r *Runtime) SetMessageContextCache(cache SessionContextCache) {
	if r == nil {
		return
	}
	r.messageCache = cache
	r.configureMessageServices()
}

func (r *Runtime) configureMessageServices() {
	if r == nil {
		return
	}
	repo, ok := r.sessions.(MessageRepository)
	if !ok || repo == nil {
		return
	}
	cache := r.messageCache
	if cache == nil {
		cache = NoopSessionContextCache{}
	}
	var publisher MessageEventPublisher = NoopMessageEventPublisher{}
	if r.messagePublisher != nil {
		publisher = CompositeMessageEventPublisher{publisher, r.messagePublisher}
	}
	if r.localVectorIndex && r.vectorIndexer != nil {
		publisher = CompositeMessageEventPublisher{publisher, r.vectorIndexer}
	}
	r.messageWriter = NewMessageWriteService(repo, cache, publisher)
	r.sessionLoader = NewSessionLoadService(repo, cache)
	if marker, ok := r.sessions.(MessageContextMarker); ok && marker != nil && r.engineFactory != nil {
		r.contextCompactor = NewContextCompactionService(
			r.sessionLoader,
			r.messageWriter,
			marker,
			LLMSummaryGenerator{RunnerFactory: r.engineFactory},
		)
		return
	}
	r.contextCompactor = nil
}

func (r *Runtime) SetSessionLoadService(service *SessionLoadService) {
	r.sessionLoader = service
}

func (r *Runtime) SetContextCompactionService(service *ContextCompactionService) {
	r.contextCompactor = service
}

func (r *Runtime) SetMessageSearchService(service *MessageSearchService) {
	r.messageSearch = service
}

func (r *Runtime) SetLiveService(service *VertexLiveService) {
	r.live = service
}

func (r *Runtime) SetLiveSetupPromptCache(cache LiveSetupPromptCache) {
	if r == nil || r.live == nil {
		return
	}
	r.live.SetSetupPromptCache(cache)
}

func (r *Runtime) WriteMessage(ctx context.Context, req MessageWriteRequest) (state.Message, error) {
	if r == nil || r.messageWriter == nil {
		return state.Message{}, fmt.Errorf("message write service is not configured")
	}
	message, err := r.messageWriter.Write(ctx, req)
	if err != nil {
		return state.Message{}, err
	}
	if r.messageSearch != nil {
		_ = r.messageSearch.InvalidateUserCache(ctx, req.UserID)
	}
	return message, nil
}

func (r *Runtime) LoadSessionContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	if r == nil || r.sessionLoader == nil {
		return nil, fmt.Errorf("session load service is not configured")
	}
	return r.sessionLoader.LoadContext(ctx, userID, sessionID, opts)
}

func (r *Runtime) CompactSessionContext(ctx context.Context, userID, sessionID string, opts ContextCompactionOptions) (ContextCompactionResult, error) {
	if r == nil || r.contextCompactor == nil {
		return ContextCompactionResult{}, fmt.Errorf("context compaction service is not configured")
	}
	return r.contextCompactor.Compact(ctx, userID, sessionID, opts)
}

func (r *Runtime) Live(ctx context.Context, req LiveRequest, input LiveClientStream, sink EventSink) error {
	if r == nil || r.live == nil {
		return fmt.Errorf("live mode is not configured")
	}
	return r.live.Run(ctx, req, input, sink)
}

func (r *Runtime) ListPendingMessageAttachments(ctx context.Context, userID string, limit int) ([]state.MessageAttachment, error) {
	if r == nil {
		return []state.MessageAttachment{}, nil
	}
	store, ok := r.sessions.(MessageAttachmentProcessorStore)
	if !ok || store == nil {
		return []state.MessageAttachment{}, nil
	}
	return store.ListPendingMessageAttachments(ctx, userID, limit)
}

func (r *Runtime) UpdateMessageAttachmentProcessing(ctx context.Context, userID, messageID, attachmentID string, status int, thumbnailKey, extractedTextKey string) error {
	if r == nil {
		return fmt.Errorf("message attachment processor store is not configured")
	}
	store, ok := r.sessions.(MessageAttachmentProcessorStore)
	if !ok || store == nil {
		return fmt.Errorf("message attachment processor store is not configured")
	}
	return store.UpdateMessageAttachmentProcessing(ctx, userID, messageID, attachmentID, status, thumbnailKey, extractedTextKey)
}

func (r *Runtime) SetMemoryExtractor(extractor MemoryExtractor) {
	if extractor != nil {
		r.memoryExtract = extractor
	}
}

func (r *Runtime) SetMemoryAbstractor(abstractor MemoryAbstractor) {
	if abstractor != nil {
		r.memoryAbstract = abstractor
	}
}

func (r *Runtime) SetMemoryOrganizer(organizer MemoryOrganizer) {
	if organizer != nil {
		r.memoryOrganizer = organizer
	}
}

func (r *Runtime) SetMemoryEpisodeService(service MemoryEpisodeService) {
	if service != nil {
		r.episodeMemory = service
	}
}

func (r *Runtime) SetMemoryEpisodeSummarizer(summarizer MemoryEpisodeSummarizer) {
	if summarizer != nil {
		r.episodeSummarizer = summarizer
	}
}

func (r *Runtime) CreateSession(ctx context.Context, userID, workingDir string) (*state.Session, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if r.config.UserWorkspaceRoot != "" {
		workingDir = r.userWorkspace(userID)
		if err := os.MkdirAll(workingDir, 0o755); err != nil {
			return nil, err
		}
	} else if strings.TrimSpace(workingDir) == "" || !r.config.AllowCustomWorkingDir {
		workingDir = r.config.DefaultWorkingDir
	}
	workingDir = filepath.Clean(workingDir)
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	return r.sessions.Create(ctx, userID, workingDir)
}

func (r *Runtime) ListSessions(ctx context.Context, userID string) ([]*state.Session, error) {
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	sessions, err := r.sessions.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if r.hideInternalTranscriptMessages(session) {
			if err := r.sessions.Save(ctx, userID, session); err != nil {
				return nil, err
			}
		}
	}
	return sessions, nil
}

func (r *Runtime) ListSessionsPage(ctx context.Context, userID string, limit, offset int) ([]*state.Session, error) {
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	if limit <= 0 && offset <= 0 {
		return r.ListSessions(ctx, userID)
	}
	if pager, ok := r.sessions.(interface {
		ListPage(context.Context, string, int, int) ([]*state.Session, error)
	}); ok && pager != nil {
		sessions, err := pager.ListPage(ctx, userID, limit, offset)
		if err != nil {
			return nil, err
		}
		for _, session := range sessions {
			if r.hideInternalTranscriptMessages(session) {
				if err := r.sessions.Save(ctx, userID, session); err != nil {
					return nil, err
				}
			}
		}
		return sessions, nil
	}
	sessions, err := r.ListSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(sessions) {
		return []*state.Session{}, nil
	}
	if limit <= 0 {
		return sessions[offset:], nil
	}
	end := offset + limit
	if end > len(sessions) {
		end = len(sessions)
	}
	return sessions[offset:end], nil
}

func (r *Runtime) GetSession(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	if r.sessions == nil {
		return nil, fmt.Errorf("session store is required")
	}
	session, err := r.sessions.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if r.hideInternalTranscriptMessages(session) {
		if err := r.sessions.Save(ctx, userID, session); err != nil {
			return nil, err
		}
	}
	return session, nil
}

func (r *Runtime) hideInternalTranscriptMessages(session *state.Session) bool {
	changed := hideWorkspaceContextAck(session)
	if r.hideSyntheticRoutedSkillMessages(session) {
		changed = true
	}
	return changed
}

func hideWorkspaceContextAck(session *state.Session) bool {
	if session == nil {
		return false
	}
	changed := false
	for i := range session.Messages {
		if session.Messages[i].Role == "assistant" && session.Messages[i].Content == workspaceContextAckContent && !session.Messages[i].Hidden {
			session.Messages[i].Hidden = true
			changed = true
		}
	}
	return changed
}

func (r *Runtime) hideSyntheticRoutedSkillMessages(session *state.Session) bool {
	if session == nil || r == nil || r.skills == nil {
		return false
	}
	changed := false
	var previousVisible *state.Message
	for i := range session.Messages {
		message := &session.Messages[i]
		if !message.Hidden && message.Role == "user" && isRunAsJobSlashMessage(r.skills, message.Content) && previousVisible != nil && previousVisible.Role == "user" && !isSlashCommand(previousVisible.Content) {
			message.Hidden = true
			changed = true
		}
		if message.Hidden || (message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" {
			continue
		}
		previousVisible = message
	}
	return changed
}

func isRunAsJobSlashMessage(catalog SkillCatalog, content string) bool {
	if catalog == nil || !isSlashCommand(content) {
		return false
	}
	parts := strings.SplitN(strings.TrimSpace(content), " ", 2)
	name := strings.TrimPrefix(parts[0], "/")
	if name == "" {
		return false
	}
	skill, ok := catalog.GetSkill(name)
	return ok && skill != nil && skill.RunAsJob
}

func isSlashCommand(content string) bool {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "/") {
		return false
	}
	if len(content) == 1 {
		return false
	}
	for _, r := range content[1:] {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return true
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func (r *Runtime) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if r.sessions == nil {
		return fmt.Errorf("session store is required")
	}
	if err := r.sessions.Delete(ctx, userID, sessionID); err != nil {
		return err
	}
	if r.messageCache != nil {
		_ = r.messageCache.InvalidateContext(ctx, userID, sessionID)
	}
	return nil
}

func (r *Runtime) DeleteSessionMemory(ctx context.Context, userID, sessionID string) error {
	if r.memory == nil {
		return nil
	}
	return r.memory.DeleteSession(ctx, userID, sessionID)
}

func (r *Runtime) DeleteUserMemory(ctx context.Context, userID string) error {
	if r.memory == nil {
		return nil
	}
	if service, ok := r.memory.(SavedMemoryDeletionService); ok {
		return service.DeleteSavedMemory(ctx, userID)
	}
	return r.memory.DeleteUser(ctx, userID)
}

func (r *Runtime) ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	if r.memory == nil {
		return []MemoryItem{}, nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return []MemoryItem{}, nil
	}
	items, err := service.ListMemoryItems(ctx, userID, filter)
	if err != nil {
		return nil, err
	}
	if filter.Namespace != MemoryNamespacePersonalization {
		items = excludeManagedPersonalizationMemory(items)
	}
	return items, nil
}

func (r *Runtime) ListMemoryEpisodes(ctx context.Context, userID string, filter MemoryEpisodeFilter) ([]MemoryEpisode, error) {
	if r == nil || r.episodeMemory == nil {
		return []MemoryEpisode{}, nil
	}
	return r.episodeMemory.ListMemoryEpisodes(ctx, userID, filter)
}

func (r *Runtime) GetMemoryEpisode(ctx context.Context, userID, episodeID string) (MemoryEpisode, error) {
	if r == nil || r.episodeMemory == nil {
		return MemoryEpisode{}, fmt.Errorf("memory episodes are not configured")
	}
	return r.episodeMemory.GetMemoryEpisode(ctx, userID, episodeID)
}

func (r *Runtime) ExpandMemoryEpisode(ctx context.Context, userID, episodeID string) (MemoryEpisode, error) {
	if _, err := r.GetMemoryEpisode(ctx, userID, episodeID); err != nil {
		return MemoryEpisode{}, err
	}
	if err := r.RecordMemoryEpisodeUse(ctx, userID, episodeID); err != nil {
		return MemoryEpisode{}, err
	}
	return r.GetMemoryEpisode(ctx, userID, episodeID)
}

func (r *Runtime) SearchMemoryEpisodes(ctx context.Context, userID, query string, opts MemoryEpisodeSearchOptions) ([]MemoryEpisodeSearchResult, error) {
	if r == nil || r.episodeMemory == nil {
		return []MemoryEpisodeSearchResult{}, nil
	}
	return r.episodeMemory.SearchMemoryEpisodes(ctx, userID, query, opts)
}

func (r *Runtime) RecordMemoryEpisodeUse(ctx context.Context, userID, episodeID string) error {
	if r == nil || r.episodeMemory == nil {
		return fmt.Errorf("memory episodes are not configured")
	}
	return r.episodeMemory.RecordMemoryEpisodeUse(ctx, userID, episodeID)
}

func (r *Runtime) DeleteMemoryEpisode(ctx context.Context, userID, episodeID string) error {
	if r == nil || r.episodeMemory == nil {
		return nil
	}
	return r.episodeMemory.DeleteMemoryEpisode(ctx, userID, episodeID)
}

func (r *Runtime) HideMemoryEpisode(ctx context.Context, userID, episodeID string) (MemoryEpisode, error) {
	if r == nil || r.episodeMemory == nil {
		return MemoryEpisode{}, fmt.Errorf("memory episodes are not configured")
	}
	episode, err := r.episodeMemory.GetMemoryEpisode(ctx, userID, episodeID)
	if err != nil {
		return MemoryEpisode{}, err
	}
	episode.Status = MemoryEpisodeStatusArchived
	episode.UpdatedAt = time.Now().UTC()
	return r.episodeMemory.UpdateMemoryEpisode(ctx, userID, episode)
}

func (r *Runtime) RestoreMemoryEpisode(ctx context.Context, userID, episodeID string) (MemoryEpisode, error) {
	if r == nil || r.episodeMemory == nil {
		return MemoryEpisode{}, fmt.Errorf("memory episodes are not configured")
	}
	episode, err := r.episodeMemory.GetMemoryEpisode(ctx, userID, episodeID)
	if err != nil {
		return MemoryEpisode{}, err
	}
	episode.Status = MemoryEpisodeStatusActive
	episode.UpdatedAt = time.Now().UTC()
	return r.episodeMemory.UpdateMemoryEpisode(ctx, userID, episode)
}

func (r *Runtime) PromoteMemoryEpisodes(ctx context.Context, userID string, episodeIDs []string, limit int) ([]MemoryItem, error) {
	if r == nil || r.episodeMemory == nil {
		return nil, fmt.Errorf("memory episodes are not configured")
	}
	service, err := r.memoryItemService()
	if err != nil {
		return nil, err
	}
	episodes, err := r.memoryEpisodesForPromotion(ctx, userID, episodeIDs, limit)
	if err != nil {
		return nil, err
	}
	promoter := RuleMemoryEpisodePromoter{
		Extractor: r.memoryExtract,
		Items:     service,
	}
	items, err := promoter.PromoteEpisodes(ctx, userID, episodes)
	if err != nil {
		return items, err
	}
	promotedEpisodeIDs := promotedEpisodeIDsFromItems(items)
	if len(promotedEpisodeIDs) > 0 {
		if err := r.episodeMemory.MarkMemoryEpisodesPromoted(ctx, userID, promotedEpisodeIDs); err != nil {
			return items, err
		}
	}
	return items, nil
}

func (r *Runtime) memoryEpisodesForPromotion(ctx context.Context, userID string, episodeIDs []string, limit int) ([]MemoryEpisode, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	episodeIDs = normalizeMemoryIDs(episodeIDs)
	if len(episodeIDs) == 0 {
		return r.episodeMemory.ListUnpromotedMemoryEpisodes(ctx, userID, limit)
	}
	episodes := make([]MemoryEpisode, 0, len(episodeIDs))
	for _, episodeID := range episodeIDs {
		episode, err := r.episodeMemory.GetMemoryEpisode(ctx, userID, episodeID)
		if err != nil {
			return nil, err
		}
		if episode.PromotedAt != nil {
			continue
		}
		episodes = append(episodes, episode)
	}
	return episodes, nil
}

func promotedEpisodeIDsFromItems(items []MemoryItem) []string {
	ids := []string{}
	for _, item := range items {
		for _, ref := range item.SourceRefs {
			if ref.Kind == "episode" && strings.TrimSpace(ref.ID) != "" {
				ids = append(ids, ref.ID)
			}
		}
	}
	return normalizeMemoryIDs(ids)
}

func (r *Runtime) GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error) {
	if r.memory == nil {
		return defaultMemorySettings(), nil
	}
	service, ok := r.memory.(MemorySettingsService)
	if !ok {
		return defaultMemorySettings(), nil
	}
	return service.GetMemorySettings(ctx, userID)
}

func (r *Runtime) UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	if r.memory == nil {
		return MemorySettings{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemorySettingsService)
	if !ok {
		return MemorySettings{}, fmt.Errorf("memory settings are not supported")
	}
	settings.UpdatedAt = time.Now().UTC()
	return service.UpdateMemorySettings(ctx, userID, settings)
}

func (r *Runtime) GetPersonalizationSettings(ctx context.Context, userID string) (PersonalizationSettings, error) {
	if r.memory == nil {
		return defaultPersonalizationSettings(), nil
	}
	service, ok := r.memory.(PersonalizationSettingsService)
	if !ok {
		return defaultPersonalizationSettings(), nil
	}
	return service.GetPersonalizationSettings(ctx, userID)
}

func (r *Runtime) UpdatePersonalizationSettings(ctx context.Context, userID string, settings PersonalizationSettings) (PersonalizationSettings, error) {
	if r.memory == nil {
		return PersonalizationSettings{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(PersonalizationSettingsService)
	if !ok {
		return PersonalizationSettings{}, fmt.Errorf("personalization settings are not supported")
	}
	current, err := service.GetPersonalizationSettings(ctx, userID)
	if err != nil {
		return PersonalizationSettings{}, err
	}
	settings.UpdatedAt = time.Now().UTC()
	settings.Version = current.Version + 1
	if settings.Version <= 1 {
		settings.Version = 2
	}
	updated, err := service.UpdatePersonalizationSettings(ctx, userID, settings)
	if err != nil {
		return PersonalizationSettings{}, err
	}
	if err := r.syncPersonalizationMemory(ctx, userID, updated); err != nil {
		return PersonalizationSettings{}, err
	}
	return updated, nil
}

func (r *Runtime) DeletePersonalizationSettings(ctx context.Context, userID string) (PersonalizationSettings, error) {
	if r.memory == nil {
		return defaultPersonalizationSettings(), nil
	}
	service, ok := r.memory.(PersonalizationSettingsService)
	if !ok {
		return defaultPersonalizationSettings(), nil
	}
	if err := service.DeletePersonalizationSettings(ctx, userID); err != nil {
		return PersonalizationSettings{}, err
	}
	if err := r.archivePersonalizationMemory(ctx, userID); err != nil {
		return PersonalizationSettings{}, err
	}
	return defaultPersonalizationSettings(), nil
}

func (r *Runtime) syncPersonalizationMemory(ctx context.Context, userID string, settings PersonalizationSettings) error {
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return err
	}
	desired := personalizationMemoryItems(userID, settings, now)
	desiredIDs := make(map[string]bool, len(desired))
	for _, item := range desired {
		desiredIDs[item.ID] = true
		if current, ok := findMemoryItemByID(existing, item.ID); ok {
			item.CreatedAt = current.CreatedAt
			item.AccessCount = current.AccessCount
			item.LastInjectedAt = current.LastInjectedAt
		}
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return err
		}
	}
	for _, item := range existing {
		if !isManagedPersonalizationMemory(item) || desiredIDs[item.ID] || item.Status != MemoryStatusActive {
			continue
		}
		item.Status = MemoryStatusArchived
		item.UpdatedAt = now
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["maintenance_action"] = "personalization_sync_archive"
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return err
		}
	}
	for _, current := range existing {
		if current.Status != MemoryStatusActive || isManagedPersonalizationMemory(current) || current.Source == MemorySourceUserEdit {
			continue
		}
		for _, explicit := range desired {
			if !memoryConflictCandidate(explicit, current) {
				continue
			}
			current.Status = MemoryStatusArchived
			current.SupersededByID = explicit.ID
			current.ConflictIDs = normalizeMemoryIDs(append(current.ConflictIDs, explicit.ID))
			current.UpdatedAt = now
			if current.Metadata == nil {
				current.Metadata = map[string]any{}
			}
			current.Metadata["conflict_strategy"] = "explicit_personalization"
			current.Metadata["maintenance_action"] = "archive_conflicting_implicit_memory"
			if _, err := service.UpdateMemoryItem(ctx, userID, current); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func (r *Runtime) archivePersonalizationMemory(ctx context.Context, userID string) error {
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Namespace: MemoryNamespacePersonalization})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, item := range items {
		if !isManagedPersonalizationMemory(item) || item.Status != MemoryStatusActive {
			continue
		}
		item.Status = MemoryStatusArchived
		item.UpdatedAt = now
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["maintenance_action"] = "personalization_reset_archive"
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return err
		}
	}
	return nil
}

func findMemoryItemByID(items []MemoryItem, itemID string) (MemoryItem, bool) {
	for _, item := range items {
		if item.ID == itemID {
			return item, true
		}
	}
	return MemoryItem{}, false
}

func (r *Runtime) GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error) {
	if r.memory == nil {
		return MemoryItem{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return MemoryItem{}, fmt.Errorf("memory item operations are not supported")
	}
	return service.GetMemoryItem(ctx, userID, itemID)
}

func (r *Runtime) UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	if r.memory == nil {
		return MemoryItem{}, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return MemoryItem{}, fmt.Errorf("memory item operations are not supported")
	}
	item.UpdatedAt = time.Now().UTC()
	return service.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) ApplyMemoryFeedback(ctx context.Context, userID, itemID, feedbackType string) (MemoryItem, error) {
	item, err := r.GetMemoryItem(ctx, userID, itemID)
	if err != nil {
		return MemoryItem{}, err
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	switch strings.ToLower(strings.TrimSpace(feedbackType)) {
	case "important":
		item.Weight = clamp01(item.Weight + 0.15)
		item.Metadata["feedback"] = "important"
	case "incorrect":
		item.Weight = clamp01(item.Weight - 0.25)
		item.Status = MemoryStatusArchived
		item.Metadata["feedback"] = "incorrect"
	case "not_relevant":
		item.Weight = clamp01(item.Weight - 0.10)
		item.Metadata["feedback"] = "not_relevant"
	default:
		return MemoryItem{}, fmt.Errorf("unsupported memory feedback type")
	}
	item.UpdatedAt = time.Now().UTC()
	return r.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) ResolveMemoryConflict(ctx context.Context, userID, itemID, action string) (MemoryItem, error) {
	item, err := r.GetMemoryItem(ctx, userID, itemID)
	if err != nil {
		return MemoryItem{}, err
	}
	if item.Status != MemoryStatusPendingConfirm && item.Status != MemoryStatusConflicted {
		return MemoryItem{}, fmt.Errorf("memory item is not pending confirmation")
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	now := time.Now().UTC()
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "accept":
		item.Status = MemoryStatusActive
		item.Metadata["conflict_resolution"] = "accepted"
		for _, conflictID := range item.ConflictIDs {
			conflict, err := r.GetMemoryItem(ctx, userID, conflictID)
			if err != nil {
				continue
			}
			conflict.Status = MemoryStatusArchived
			conflict.SupersededByID = item.ID
			conflict.UpdatedAt = now
			if conflict.Metadata == nil {
				conflict.Metadata = map[string]any{}
			}
			conflict.Metadata["conflict_resolution"] = "superseded_by_user_confirmed_memory"
			if _, err := r.UpdateMemoryItem(ctx, userID, conflict); err != nil {
				return MemoryItem{}, err
			}
		}
	case "reject":
		item.Status = MemoryStatusArchived
		item.Metadata["conflict_resolution"] = "rejected"
	case "keep_both":
		item.Status = MemoryStatusActive
		item.Metadata["conflict_resolution"] = "kept_both"
	default:
		return MemoryItem{}, fmt.Errorf("unsupported memory conflict action")
	}
	item.UpdatedAt = now
	return r.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) ReviewDeepAgentLearningCandidate(ctx context.Context, userID, candidateID, action, reviewerID, reason string) (MemoryItem, error) {
	itemID := deepAgentLearningMemoryItemID(candidateID)
	item, err := r.GetMemoryItem(ctx, userID, itemID)
	if err != nil {
		item, err = r.GetMemoryItem(ctx, userID, strings.TrimSpace(candidateID))
		if err != nil {
			return MemoryItem{}, err
		}
	}
	if item.Metadata == nil || item.Metadata["deep_agent_learning"] != true {
		return MemoryItem{}, fmt.Errorf("memory item is not a deep agent learning candidate")
	}
	now := time.Now().UTC()
	action = strings.ToLower(strings.TrimSpace(action))
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	switch action {
	case "accept":
		item.Status = MemoryStatusActive
		item.Metadata["review_status"] = deepAgentLearningStatusAccepted
		item.Metadata["user_confirmed"] = true
		item.Metadata["user_confirmation_required"] = false
	case "reject":
		item.Status = MemoryStatusArchived
		item.Metadata["review_status"] = deepAgentLearningStatusRejected
		item.Metadata["user_confirmed"] = false
	case "rollback":
		item.Status = MemoryStatusDeleted
		item.Metadata["review_status"] = deepAgentLearningStatusRollback
		item.Metadata["user_confirmed"] = false
	default:
		return MemoryItem{}, fmt.Errorf("unsupported deep agent learning review action")
	}
	item.Metadata["review_action"] = action
	item.Metadata["reviewed_by"] = strings.TrimSpace(reviewerID)
	item.Metadata["reviewed_at"] = now.Format(time.RFC3339Nano)
	if strings.TrimSpace(reason) != "" {
		item.Metadata["review_reason"] = strings.TrimSpace(reason)
	}
	item.UpdatedAt = now
	return r.UpdateMemoryItem(ctx, userID, item)
}

func (r *Runtime) RebuildMemoryAbstractions(ctx context.Context, userID string) ([]MemoryItem, error) {
	if r.memory == nil || r.memoryAbstract == nil {
		return []MemoryItem{}, nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil, fmt.Errorf("memory item operations are not supported")
	}
	existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	existing = excludeManagedPersonalizationMemory(existing)
	now := time.Now().UTC()
	abstracts, err := r.memoryAbstract.Build(ctx, userID, existing, now)
	if err != nil {
		return nil, err
	}
	builtHashes := map[string]bool{}
	updated := make([]MemoryItem, 0, len(abstracts))
	for _, item := range abstracts {
		item.UserID = userID
		item.SessionID = ""
		item.Source = MemorySourceSystem
		item.Status = MemoryStatusActive
		item = upsertMemoryItem(existing, item)
		item.Metadata["dirty"] = false
		saved, err := service.UpdateMemoryItem(ctx, userID, item)
		if err != nil {
			return nil, err
		}
		builtHashes[saved.RawHash] = true
		updated = append(updated, saved)
		existing = append(existing, saved)
	}
	for _, item := range existing {
		item = normalizeMemoryItem(item)
		if item.Source != MemorySourceSystem || (item.Level != MemoryLevelConcept && item.Level != MemoryLevelProfile) || builtHashes[item.RawHash] {
			continue
		}
		item.Status = MemoryStatusArchived
		item.UpdatedAt = now
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["archived_reason"] = "abstraction_rebuild_no_source_atoms"
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func (r *Runtime) ScoreMemoryQuality(ctx context.Context, userID string) ([]MemoryItem, error) {
	service, err := r.memoryItemService()
	if err != nil {
		return nil, err
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	items = excludeManagedPersonalizationMemory(items)
	now := time.Now().UTC()
	updated := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		scored := scoreMemoryQuality(item, items, now)
		if _, err := service.UpdateMemoryItem(ctx, userID, scored); err != nil {
			return nil, err
		}
		updated = append(updated, scored)
	}
	return updated, nil
}

func (r *Runtime) PlanMemoryMaintenance(ctx context.Context, userID string) ([]MemoryMaintenanceAction, error) {
	if r.memoryOrganizer == nil {
		return []MemoryMaintenanceAction{}, nil
	}
	service, err := r.memoryItemService()
	if err != nil {
		return nil, err
	}
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return nil, err
	}
	items = excludeManagedPersonalizationMemory(items)
	now := time.Now().UTC()
	scored := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		scored = append(scored, scoreMemoryQuality(item, items, now))
	}
	actions, err := r.memoryOrganizer.Plan(ctx, userID, scored, now)
	if err != nil {
		return nil, err
	}
	episodeActions, err := r.planEpisodePromotionMaintenance(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	actions = append(actions, episodeActions...)
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Confidence == actions[j].Confidence {
			return actions[i].Type < actions[j].Type
		}
		return actions[i].Confidence > actions[j].Confidence
	})
	return actions, nil
}

func (r *Runtime) planEpisodePromotionMaintenance(ctx context.Context, userID string, now time.Time) ([]MemoryMaintenanceAction, error) {
	if r == nil || r.episodeMemory == nil {
		return nil, nil
	}
	episodes, err := r.episodeMemory.ListUnpromotedMemoryEpisodes(ctx, userID, 5)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	var confidence float64
	for _, episode := range episodes {
		if episode.Status != MemoryEpisodeStatusActive {
			continue
		}
		score := clamp01(0.45*episode.Weight + 0.25*episode.Confidence + 0.20*episode.RecallScore)
		if episode.UseCount > 0 {
			score += 0.08
		}
		if episode.RecallCount > 0 {
			score += 0.04
		}
		score = clamp01(score)
		if score < 0.62 {
			continue
		}
		ids = append(ids, episode.ID)
		if score > confidence {
			confidence = score
		}
	}
	ids = normalizeMemoryIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	action := MemoryMaintenanceAction{
		UserID:     userID,
		Type:       "promote_episodes",
		MemoryIDs:  ids,
		Reason:     "Unpromoted episodic memories look stable enough to extract durable L3 memory candidates.",
		Confidence: confidence,
		Status:     MemoryMaintenancePending,
		CreatedAt:  now,
	}
	action.ID = memoryMaintenanceActionID(action.Type, action.MemoryIDs)
	return []MemoryMaintenanceAction{action}, nil
}

func (r *Runtime) RunMemoryMaintenance(ctx context.Context, userID string) (MemoryMaintenanceRunReport, error) {
	actions, err := r.PlanMemoryMaintenance(ctx, userID)
	if err != nil {
		return MemoryMaintenanceRunReport{}, err
	}
	report := MemoryMaintenanceRunReport{
		Actions: []MemoryMaintenanceAction{},
		Applied: []MemoryMaintenanceAction{},
		Planned: actions,
	}
	for _, action := range actions {
		if !memoryMaintenanceAutoApplyable(action) {
			report.Actions = append(report.Actions, action)
			continue
		}
		applied, err := r.applyMemoryMaintenanceAction(ctx, userID, action)
		if err != nil {
			return report, err
		}
		report.Applied = append(report.Applied, applied)
	}
	return report, nil
}

func (r *Runtime) ApplyMemoryMaintenance(ctx context.Context, userID, actionID string) (MemoryMaintenanceAction, error) {
	actions, err := r.PlanMemoryMaintenance(ctx, userID)
	if err != nil {
		return MemoryMaintenanceAction{}, err
	}
	var action MemoryMaintenanceAction
	for _, candidate := range actions {
		if memoryMaintenanceActionMatches(candidate, actionID) {
			action = candidate
			break
		}
	}
	if action.ID == "" {
		return MemoryMaintenanceAction{}, fmt.Errorf("memory maintenance action not found")
	}
	return r.applyMemoryMaintenanceAction(ctx, userID, action)
}

func (r *Runtime) applyMemoryMaintenanceAction(ctx context.Context, userID string, action MemoryMaintenanceAction) (MemoryMaintenanceAction, error) {
	service, err := r.memoryItemService()
	if err != nil {
		return MemoryMaintenanceAction{}, err
	}
	switch action.Type {
	case "archive_low_quality":
		for _, memoryID := range action.MemoryIDs {
			item, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			item.Status = MemoryStatusArchived
			if item.Metadata == nil {
				item.Metadata = map[string]any{}
			}
			item.Metadata["maintenance_action"] = action.Type
			if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
	case "merge_duplicates":
		if len(action.MemoryIDs) < 2 {
			break
		}
		winner, err := service.GetMemoryItem(ctx, userID, action.MemoryIDs[0])
		if err != nil {
			return MemoryMaintenanceAction{}, err
		}
		if winner.Metadata == nil {
			winner.Metadata = map[string]any{}
		}
		for _, memoryID := range action.MemoryIDs[1:] {
			loser, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			loser.Status = MemoryStatusArchived
			loser.SupersededByID = winner.ID
			if loser.Metadata == nil {
				loser.Metadata = map[string]any{}
			}
			loser.Metadata["maintenance_action"] = action.Type
			winner.RelatedIDs = append(winner.RelatedIDs, loser.ID)
			if _, err := service.UpdateMemoryItem(ctx, userID, loser); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
		winner.RelatedIDs = normalizeMemoryIDs(winner.RelatedIDs)
		winner.Metadata["maintenance_action"] = action.Type
		if _, err := service.UpdateMemoryItem(ctx, userID, winner); err != nil {
			return MemoryMaintenanceAction{}, err
		}
	case "rebuild_concept", "refresh_profile":
		if _, err := r.RebuildMemoryAbstractions(ctx, userID); err != nil {
			return MemoryMaintenanceAction{}, err
		}
	case "confirm_conflict":
		// User-facing action; applying only leaves the item pending and records that it was surfaced.
		for _, memoryID := range action.MemoryIDs {
			item, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			if item.Metadata == nil {
				item.Metadata = map[string]any{}
			}
			item.Metadata["maintenance_action"] = "confirm_conflict_surfaced"
			if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
	case "reduce_weight":
		for _, memoryID := range action.MemoryIDs {
			item, err := service.GetMemoryItem(ctx, userID, memoryID)
			if err != nil {
				continue
			}
			item.Weight = clamp01(item.Weight - 0.10)
			if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
				return MemoryMaintenanceAction{}, err
			}
		}
	case "promote_episodes":
		if _, err := r.PromoteMemoryEpisodes(ctx, userID, action.MemoryIDs, len(action.MemoryIDs)); err != nil {
			return MemoryMaintenanceAction{}, err
		}
	default:
		return MemoryMaintenanceAction{}, fmt.Errorf("unsupported memory maintenance action")
	}
	action.Status = MemoryMaintenanceApplied
	return action, nil
}

func memoryMaintenanceAutoApplyable(action MemoryMaintenanceAction) bool {
	if action.Status != "" && action.Status != MemoryMaintenancePending {
		return false
	}
	switch action.Type {
	case "merge_duplicates":
		return action.Confidence >= 0.90 && len(action.MemoryIDs) > 1
	case "rebuild_concept", "refresh_profile":
		return action.Confidence >= 0.80
	case "reduce_weight":
		return action.Confidence >= 0.85 && len(action.MemoryIDs) > 0
	case "archive_low_quality":
		return action.Confidence >= 0.95 && len(action.MemoryIDs) > 0
	default:
		return false
	}
}

func (r *Runtime) DismissMemoryMaintenance(ctx context.Context, userID, actionID string) (MemoryMaintenanceAction, error) {
	actions, err := r.PlanMemoryMaintenance(ctx, userID)
	if err != nil {
		return MemoryMaintenanceAction{}, err
	}
	for _, action := range actions {
		if memoryMaintenanceActionMatches(action, actionID) {
			action.Status = MemoryMaintenanceDismissed
			return action, nil
		}
	}
	return MemoryMaintenanceAction{}, fmt.Errorf("memory maintenance action not found")
}

func (r *Runtime) memoryItemService() (MemoryItemService, error) {
	if r.memory == nil {
		return nil, fmt.Errorf("memory is not configured")
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil, fmt.Errorf("memory item operations are not supported")
	}
	return service, nil
}

func (r *Runtime) DeleteMemoryItem(ctx context.Context, userID, itemID string) error {
	if r.memory == nil {
		return nil
	}
	service, ok := r.memory.(MemoryItemService)
	if !ok {
		return nil
	}
	return service.DeleteMemoryItem(ctx, userID, itemID)
}

func (r *Runtime) ExportUserData(ctx context.Context, user *UserProfile) (*UserDataExport, error) {
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return nil, fmt.Errorf("user is required")
	}
	sessions, err := r.ListSessions(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := &UserDataExport{
		ExportedAt: time.Now().UTC(),
		User:       user,
		Sessions:   sessions,
		Memory: MemoryExport{
			Sessions: make(map[string]string),
		},
	}
	if messages, ok := r.sessions.(interface {
		ListMessages(context.Context, string, string) ([]state.Message, error)
	}); ok {
		out.Messages = make(map[string][]state.Message, len(sessions))
		for _, session := range sessions {
			items, err := messages.ListMessages(ctx, user.ID, session.ID)
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				items = session.Messages
			}
			out.Messages[session.ID] = items
		}
	}
	if r.artifacts != nil {
		attachments, err := r.artifacts.List(ctx, user.ID, "", AssetKindAttachment)
		if err != nil {
			return nil, err
		}
		artifacts, err := r.artifacts.List(ctx, user.ID, "", AssetKindArtifact)
		if err != nil {
			return nil, err
		}
		out.Attachments = attachments
		out.Artifacts = artifacts
	}
	if r.jobs != nil {
		jobs, err := r.jobs.ListJobs(ctx, user.ID, "")
		if err != nil {
			return nil, err
		}
		out.Jobs = jobs
		out.JobEvents = make(map[string][]*JobEvent, len(jobs))
		for _, job := range jobs {
			events, err := r.jobs.ListJobEvents(ctx, user.ID, job.ID, "", 0)
			if err != nil {
				return nil, err
			}
			out.JobEvents[job.ID] = events
		}
	}
	if r.episodeMemory != nil {
		episodes, err := r.episodeMemory.ListMemoryEpisodes(ctx, user.ID, MemoryEpisodeFilter{})
		if err != nil {
			return nil, err
		}
		out.Memory.Episodes = episodes
	}
	if r.memory == nil {
		return out, nil
	}
	personalization, err := r.GetPersonalizationSettings(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out.Personalization = personalization
	if service, ok := r.memory.(MemoryItemService); ok {
		items, err := service.ListMemoryItems(ctx, user.ID, MemoryItemFilter{Status: ""})
		if err != nil {
			return nil, err
		}
		out.Memory.Items = items
	}
	userMemory, err := r.memory.LoadUserMemory(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out.Memory.User = userMemory
	for _, session := range sessions {
		content, err := r.memory.LoadSessionMemory(ctx, user.ID, session.ID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(content) != "" {
			out.Memory.Sessions[session.ID] = content
		}
	}
	return out, nil
}

func (r *Runtime) DeleteUserData(ctx context.Context, userID string) error {
	for _, session := range r.runningSessionIDs(userID) {
		r.Cancel(userID, session)
	}
	deletedMessages, err := r.loadUserMessagesForIndexDelete(ctx, userID)
	if err != nil {
		return err
	}
	if r.memory != nil {
		if err := r.memory.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.sessions != nil {
		if err := r.sessions.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.artifacts != nil {
		if err := r.artifacts.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.jobs != nil {
		if err := r.jobs.DeleteUser(ctx, userID); err != nil {
			return err
		}
	}
	if r.config.UserWorkspaceRoot != "" {
		if err := os.RemoveAll(r.userWorkspace(userID)); err != nil {
			return err
		}
	}
	r.publishDeletedMessageEvents(ctx, userID, deletedMessages)
	return nil
}

func (r *Runtime) PruneBefore(ctx context.Context, cutoff time.Time) (map[string]int, error) {
	out := make(map[string]int)
	if r.memory != nil {
		n, err := r.memory.PruneBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["memories"] = n
	}
	if r.sessions != nil {
		n, err := r.sessions.PruneBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["sessions"] = n
	}
	if r.artifacts != nil {
		n, err := r.artifacts.PruneDeletedBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["artifacts"] = n
	}
	if r.jobs != nil {
		n, err := r.jobs.PruneBefore(ctx, cutoff)
		if err != nil {
			return out, err
		}
		out["jobs"] = n
	}
	return out, nil
}

func (r *Runtime) CreateArtifact(ctx context.Context, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	return r.createAsset(ctx, AssetKindArtifact, userID, sessionID, filename, contentType, data)
}

func (r *Runtime) CreateAttachment(ctx context.Context, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	return r.createAsset(ctx, AssetKindAttachment, userID, sessionID, filename, contentType, data)
}

func (r *Runtime) CreatePresignedAttachmentUpload(ctx context.Context, userID, sessionID, filename, contentType string, sizeBytes int64, ttl time.Duration) (*PresignedAttachmentUpload, error) {
	if r.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	if strings.TrimSpace(sessionID) != "" {
		if _, err := r.GetSession(ctx, userID, sessionID); err != nil {
			return nil, err
		}
	}
	return r.artifacts.PresignAttachmentUpload(ctx, userID, sessionID, filename, contentType, sizeBytes, ttl)
}

func (r *Runtime) ConfirmAttachmentUpload(ctx context.Context, userID, sessionID, attachmentID, filename, contentType string, sizeBytes int64) (*Artifact, error) {
	if r.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	if strings.TrimSpace(sessionID) != "" {
		if _, err := r.GetSession(ctx, userID, sessionID); err != nil {
			return nil, err
		}
	}
	return r.artifacts.ConfirmAttachmentUpload(ctx, userID, sessionID, attachmentID, filename, contentType, sizeBytes)
}

func (r *Runtime) createAsset(ctx context.Context, kind, userID, sessionID, filename, contentType string, data []byte) (*Artifact, error) {
	if r.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	if strings.TrimSpace(sessionID) != "" {
		if _, err := r.GetSession(ctx, userID, sessionID); err != nil {
			return nil, err
		}
	}
	asset, err := r.artifacts.Create(ctx, kind, userID, sessionID, filename, contentType, data)
	if err != nil {
		return nil, err
	}
	r.scanCreatedAsset(ctx, asset, data)
	r.enqueueAssetInsight(asset, data)
	return asset, nil
}

func (r *Runtime) scanCreatedAsset(ctx context.Context, asset *Artifact, data []byte) {
	if r == nil || r.riskScanner == nil || r.riskRecorder == nil || asset == nil {
		return
	}
	findings := r.riskScanner.ScanRisk(ctx, RiskScanTarget{
		Kind:        asset.Kind,
		UserID:      asset.UserID,
		SessionID:   asset.SessionID,
		JobID:       asset.JobID,
		AssetID:     asset.ID,
		Filename:    asset.Filename,
		ContentType: asset.ContentType,
		Data:        data,
	})
	for _, finding := range findings {
		metadata := map[string]any{
			"category":     finding.Category,
			"snippet":      finding.Snippet,
			"target":       asset.Kind,
			"filename":     asset.Filename,
			"content_type": asset.ContentType,
			"size_bytes":   asset.SizeBytes,
		}
		for key, value := range finding.Metadata {
			metadata[key] = value
		}
		r.riskRecorder(ctx, RiskEvent{
			UserID:     asset.UserID,
			SessionID:  asset.SessionID,
			JobID:      asset.JobID,
			AssetID:    asset.ID,
			Operation:  riskOperationForScanTarget(asset.Kind),
			Reason:     finding.Reason,
			RiskLevel:  finding.RiskLevel,
			ScoreDelta: finding.ScoreDelta,
			Metadata:   metadata,
		})
	}
}

func (r *Runtime) recordExecutionDenialRisk(ctx context.Context, userID, sessionID, skillName string, err error, metadata map[string]any) {
	if r == nil || r.riskRecorder == nil {
		return
	}
	finding, ok := executionDenialFinding(err)
	if !ok {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["category"] = finding.Category
	metadata["snippet"] = finding.Snippet
	if strings.TrimSpace(skillName) != "" {
		metadata["skill_name"] = skillName
	}
	r.riskRecorder(ctx, RiskEvent{
		UserID:     userID,
		SessionID:  sessionID,
		JobID:      jobIDFromContext(ctx),
		RequestID:  requestIDFromContext(ctx),
		Operation:  "execution_denied",
		Reason:     finding.Reason,
		RiskLevel:  finding.RiskLevel,
		ScoreDelta: finding.ScoreDelta,
		Metadata:   metadata,
	})
}

func (r *Runtime) ListArtifacts(ctx context.Context, userID, sessionID string) ([]*Artifact, error) {
	return r.listAssets(ctx, AssetKindArtifact, userID, sessionID)
}

func (r *Runtime) ListAttachments(ctx context.Context, userID, sessionID string) ([]*Artifact, error) {
	return r.listAssets(ctx, AssetKindAttachment, userID, sessionID)
}

func (r *Runtime) listAssets(ctx context.Context, kind, userID, sessionID string) ([]*Artifact, error) {
	if r.artifacts == nil {
		return []*Artifact{}, nil
	}
	return r.artifacts.List(ctx, userID, sessionID, kind)
}

func (r *Runtime) GetArtifact(ctx context.Context, userID, artifactID string) (*Artifact, []byte, error) {
	return r.getAsset(ctx, AssetKindArtifact, userID, artifactID)
}

func (r *Runtime) GetAttachment(ctx context.Context, userID, attachmentID string) (*Artifact, []byte, error) {
	return r.getAsset(ctx, AssetKindAttachment, userID, attachmentID)
}

func (r *Runtime) GetAttachmentMetadata(ctx context.Context, userID, attachmentID string) (*Artifact, error) {
	if r.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	return r.artifacts.GetMetadata(ctx, userID, attachmentID, AssetKindAttachment)
}

func (r *Runtime) getAsset(ctx context.Context, kind, userID, assetID string) (*Artifact, []byte, error) {
	if r.artifacts == nil {
		return nil, nil, fmt.Errorf("artifact service is not configured")
	}
	return r.artifacts.Get(ctx, userID, assetID, kind)
}

func (r *Runtime) DeleteArtifact(ctx context.Context, userID, artifactID string) error {
	return r.deleteAsset(ctx, AssetKindArtifact, userID, artifactID)
}

func (r *Runtime) DeleteAttachment(ctx context.Context, userID, attachmentID string) error {
	return r.deleteAsset(ctx, AssetKindAttachment, userID, attachmentID)
}

func (r *Runtime) deleteAsset(ctx context.Context, kind, userID, assetID string) error {
	if r.artifacts == nil {
		return nil
	}
	return r.artifacts.Delete(ctx, userID, assetID, kind)
}

func (r *Runtime) Chat(ctx context.Context, req ChatRequest, sink EventSink) error {
	if strings.TrimSpace(req.UserID) == "" {
		return fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(req.Content) == "" && len(req.AttachmentIDs) == 0 && len(req.AttachmentURLs) == 0 {
		return fmt.Errorf("content or attachment is required")
	}
	if sink == nil {
		return fmt.Errorf("event sink is required")
	}
	req.ConnectorContext = r.resolveConnectorContext(ctx, req)
	session, err := r.GetSession(ctx, req.UserID, req.SessionID)
	if err != nil {
		return err
	}
	startMessageCount := len(session.Messages)
	if err := r.injectSessionRuntimeContexts(ctx, req.UserID, session); err != nil {
		return err
	}

	turnCtx, cancel := context.WithTimeout(ctx, r.config.TurnTimeout)
	turnKey := sessionKey(req.UserID, session.ID)
	if err := r.start(turnKey, cancel, jobIDFromContext(ctx) != ""); err != nil {
		cancel()
		return err
	}
	turnFinished := false
	finishTurn := func() {
		if turnFinished {
			return
		}
		r.finish(turnKey)
		turnFinished = true
	}
	defer finishTurn()

	if err := sink.Send(ctx, Event{Type: "start", SessionID: session.ID}); err != nil {
		return err
	}
	displayContent := req.Content
	if strings.TrimSpace(displayContent) == "" {
		displayContent = "Please analyze the attached file(s)."
	}
	if !hideUserMessageFromContext(ctx) {
		if err := sink.Send(ctx, Event{Type: "message", SessionID: session.ID, Role: "user", Content: displayContent}); err != nil {
			return err
		}
	}

	result, err := r.executeAgenticTaskWorkflow(turnCtx, req, session, func(token string) {
		_ = sink.Send(ctx, Event{Type: "delta", SessionID: session.ID, Role: "assistant", Content: token})
	})
	if err != nil {
		r.appendFailedTurn(session, displayContent, err)
		if saveErr := r.persistChatSession(ctx, req.UserID, session, startMessageCount); saveErr != nil {
			_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, Error: err.Error()})
			return errors.Join(err, saveErr)
		}
		_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, Error: err.Error()})
		return err
	}
	session = result.Session
	if session == nil {
		return fmt.Errorf("runner returned no session")
	}
	r.sanitizeSessionAttachmentBlocks(session)
	if err := r.persistChatSession(ctx, req.UserID, session, startMessageCount); err != nil {
		return err
	}
	if result.Job != nil {
		finishTurn()
		r.markJobUserMessageHidden(result.Job.ID)
		if err := r.StartJob(ctx, result.Job); err != nil {
			_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, JobID: result.Job.ID, Error: err.Error()})
			return err
		}
		if err := sink.Send(ctx, Event{Type: "job", SessionID: session.ID, JobID: result.Job.ID, Job: result.Job, JobReason: result.JobReason}); err != nil {
			return err
		}
		return nil
	}
	finishTurn()
	if err := sink.Send(ctx, Event{Type: "message", SessionID: session.ID, Role: "assistant", Content: result.Output}); err != nil {
		return err
	}
	if err := sink.Send(ctx, Event{Type: "done", SessionID: session.ID}); err != nil {
		return err
	}
	r.scheduleAfterTurnMemory(ctx, req.UserID, session)
	return nil
}

func (r *Runtime) LiveSystemInstruction(ctx context.Context, userID, sessionID string) string {
	if r == nil {
		return ""
	}
	var parts []string
	session, err := r.GetSession(ctx, userID, sessionID)
	if err != nil || session == nil {
		parts = append(parts, r.baseLiveRuntimeContextParts()...)
		return strings.Join(parts, "\n\n")
	}
	parts = append(parts, r.baseLiveRuntimeContextParts()...)
	parts = append(parts, "Live voice language policy: preserve the user's spoken language; if the utterance is ambiguous, prefer Chinese for this product unless recent conversation context is clearly in another language. Treat short repeated fillers, obvious ASR noise, and accidental wake words as non-actionable. Never trigger artifact-producing skills from vague live speech; require an explicit slash command or a clear confirmation-quality request.")
	parts = append(parts, "Session history policy: you may receive prior conversation history and saved memory at session start. If the startup prompt asks for a greeting, reply only with that brief greeting. Do NOT proactively answer, summarize, continue, or take action on prior history, saved memory, tools, or other sessions until the user makes a new spoken request. Use prior context only after it is relevant to the user's new request.")
	runner, _, _ := r.liveHarnessToolRunner(ctx, userID, sessionID)
	if runner != nil && liveRunnerHasWebTools(runner) {
		parts = append(parts, "Web research: you have a `web_research` function available. Use it — do not answer from memory — whenever the user asks for current events, recent news, live data, or any externally verifiable fact. Call the function immediately; do not narrate that you are about to search.")
	}
	if skillContext := r.liveSkillContext(); strings.TrimSpace(skillContext) != "" {
		parts = append(parts, "<skills>\n"+skillContext+"\n</skills>")
	}
	personalization, personalizationErr := r.GetPersonalizationSettings(ctx, userID)
	if personalizationErr == nil {
		if content := formatPersonalizationContext(personalization); strings.TrimSpace(content) != "" {
			parts = append(parts, "<personalization>\n"+content+"\n</personalization>")
		}
		if personalization.FeatureFlags.UseBrowserMemory {
			if browserMemory, err := r.browserMemoryContext(ctx, userID, session); err == nil && strings.TrimSpace(browserMemory) != "" {
				parts = append(parts, browserMemory)
			}
		}
	}
	if r.memory != nil {
		if personalizationErr == nil && !personalization.FeatureFlags.UseSavedMemory {
			// Explicit personalization disables saved-memory reference for this user.
		} else if memory, err := r.memory.LoadContext(ctx, userID, session); err == nil && strings.TrimSpace(memory) != "" {
			parts = append(parts, memory)
		}
	}
	content := strings.Join(parts, "\n\n")
	if resolution, err := r.promptResolver.Resolve(ctx, PromptResolveRequest{PromptID: PromptIDLiveSetup, UserID: userID, SessionID: sessionID, RuntimeMode: "live"}); err == nil {
		if rendered, err := RenderPrompt(resolution, map[string]any{"content": content}); err == nil {
			return rendered.Content
		}
	}
	return content
}

func (r *Runtime) LiveInitialHistory(ctx context.Context, userID, sessionID string) ([]state.Message, error) {
	if r == nil {
		return nil, nil
	}
	opts := SessionLoadOptions{
		MaxMessages:   defaultLiveInitialHistoryMaxMessages,
		MaxTokens:     defaultLiveInitialHistoryMaxTokens,
		LoadStrategy:  SessionLoadStrategySlidingWindow,
		IncludeSystem: true,
	}
	var messages []state.Message
	if r.sessionLoader != nil {
		loaded, err := r.sessionLoader.LoadContext(ctx, userID, sessionID, opts)
		if err != nil {
			return nil, err
		}
		messages = loaded
	} else {
		session, err := r.GetSession(ctx, userID, sessionID)
		if err != nil {
			return nil, err
		}
		if session != nil {
			messages = applySessionTokenBudget(applySlidingWindowBudget(session.Messages, opts), opts)
		}
	}
	out := make([]state.Message, 0, len(messages))
	for _, message := range messages {
		if message.Hidden {
			continue
		}
		if message.Status != 0 && message.Status != state.MessageStatusNormal {
			continue
		}
		if message.Role != state.MessageRoleUser && message.Role != state.MessageRoleAssistant && !isSystemContextMessage(message) {
			continue
		}
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		out = append(out, message)
	}
	return out, nil
}

func (r *Runtime) LiveToolFunctionDeclarations(ctx context.Context, userID, sessionID string) ([]map[string]any, error) {
	if r == nil {
		return nil, nil
	}
	var declarations []map[string]any
	runner, _, err := r.liveHarnessToolRunner(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if runner != nil {
		if liveRunnerHasWebTools(runner) {
			declarations = append(declarations, liveWebResearchFunctionDeclaration())
		}
	}
	if r.liveHasUserInvocableSkills() {
		declarations = append(declarations, liveRunSkillFunctionDeclaration())
	}
	return declarations, nil
}

func (r *Runtime) ExecuteLiveToolFunctionCall(ctx context.Context, userID, sessionID, callID, toolName string, args json.RawMessage, displayText string, sink EventSink) (bool, string, error) {
	toolName = strings.TrimSpace(toolName)
	if r == nil || toolName == "" {
		return false, "", nil
	}
	if liveSkillFunctionName(toolName) {
		skillName, skillArgs := liveSkillFunctionArgs(toolName, args)
		return r.ExecuteLiveSkillFunctionCall(ctx, userID, sessionID, skillName, skillArgs, displayText, sink)
	}
	if strings.EqualFold(toolName, liveWebResearchFunctionName) {
		return r.executeLiveWebResearchFunctionCall(ctx, userID, sessionID, callID, args, displayText, sink)
	}
	if !liveHarnessToolAllowed(toolName) {
		return false, "", nil
	}

	runner, session, err := r.liveHarnessToolRunner(ctx, userID, sessionID)
	if err != nil {
		return true, "", err
	}
	if runner == nil || session == nil {
		return true, "", fmt.Errorf("live tool runner is not configured")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	preparedArgs, fallbackLevel, err := prepareLiveHarnessToolInput(ctx, runner, toolName, args, displayText)
	if err != nil {
		return true, "", err
	}
	args = preparedArgs
	if sink != nil && fallbackLevel != "" {
		_ = sink.Send(ctx, Event{Type: "live_tool_fallback", SessionID: session.ID, Role: state.MessageRoleTool, Content: fallbackLevel, Data: liveJSON(map[string]any{"tool": toolName, "fallback": fallbackLevel})})
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		callID = fmt.Sprintf("live-call-%d", time.Now().UnixNano())
	}
	startMessageCount := len(session.Messages)
	if sink != nil {
		_ = sink.Send(ctx, Event{Type: "live_tool_start", SessionID: session.ID, Role: state.MessageRoleTool, Content: toolName, Data: liveJSON(map[string]any{"tool": toolName, "call_id": callID})})
	}

	callCtx, cancel := context.WithTimeout(ctx, r.config.TurnTimeout)
	defer cancel()
	callCtx = WithLLMScope(callCtx, LLMScope{
		UserID:    userID,
		SessionID: session.ID,
		RequestID: requestIDFromContext(ctx),
	})
	result, err := runner.ExecuteTool(callCtx, toolName, args)
	output := result.Output
	if err != nil {
		output = formatLiveToolExecutionError(toolName, err)
	}
	session.AddAssistantMessageWithTools("", []state.ToolCall{{
		ID:    callID,
		Name:  toolName,
		Input: args,
	}})
	session.AddToolResult(callID, toolName, args, output)
	if persistErr := r.persistChatSession(ctx, userID, session, startMessageCount); persistErr != nil {
		if err != nil {
			return true, output, errors.Join(err, persistErr)
		}
		return true, output, persistErr
	}
	if sink != nil {
		eventType := "live_tool_result"
		if err != nil {
			eventType = "live_tool_error"
		}
		_ = sink.Send(ctx, Event{Type: eventType, SessionID: session.ID, Role: state.MessageRoleTool, Content: output, Data: liveJSON(map[string]any{"tool": toolName, "call_id": callID})})
	}
	if err != nil {
		return true, output, err
	}
	return true, output, nil
}

func (r *Runtime) executeLiveWebResearchFunctionCall(ctx context.Context, userID, sessionID, callID string, args json.RawMessage, displayText string, sink EventSink) (bool, string, error) {
	runner, session, err := r.liveHarnessToolRunner(ctx, userID, sessionID)
	if err != nil {
		return true, "", err
	}
	if runner == nil || session == nil {
		return true, "", fmt.Errorf("live web research runner is not configured")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	initialSchema := liveWebResearchStructuredSchema(false)
	if result := ValidateStructuredJSON(args, initialSchema); !result.Valid() {
		emitStructuredOutputValidationFailure(ctx, initialSchema, liveWebResearchFunctionName, result)
		repaired, repairErr := repairStructuredJSONWithRunner(ctx, runner, initialSchema, string(args), result.Error(), "Latest live utterance: "+strings.TrimSpace(displayText))
		if repairErr != nil {
			emitStructuredOutputFallbackEvent(ctx, initialSchema, liveWebResearchFunctionName, structuredFallbackStopAutoExecute, repairErr)
			return true, "", fmt.Errorf("web_research input invalid after %s: %w; repair failed: %v", structuredFallbackRepairRetry, result.Error(), repairErr)
		}
		args = repaired
	}
	input := liveWebResearchArgs(args)
	if strings.TrimSpace(input.Query) == "" {
		input.Query = strings.TrimSpace(displayText)
	}
	normalizedInput, err := json.Marshal(input)
	if err != nil {
		return true, "", err
	}
	requiredSchema := liveWebResearchStructuredSchema(true)
	if result := ValidateStructuredJSON(normalizedInput, requiredSchema); !result.Valid() {
		emitStructuredOutputValidationFailure(ctx, requiredSchema, liveWebResearchFunctionName, result)
		return true, "", result.Error()
	}
	if strings.TrimSpace(input.Query) == "" {
		emitStructuredOutputFallbackEvent(ctx, requiredSchema, liveWebResearchFunctionName, structuredFallbackUserClarification, fmt.Errorf("web_research requires a query"))
		return true, "", fmt.Errorf("web_research requires a query; fallback=%s", structuredFallbackUserClarification)
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		callID = fmt.Sprintf("live-web-research-%d", time.Now().UnixNano())
	}
	startMessageCount := len(session.Messages)
	if sink != nil {
		_ = sink.Send(ctx, Event{Type: "live_tool_start", SessionID: session.ID, Role: state.MessageRoleTool, Content: "Web research", Data: liveJSON(map[string]any{"tool": liveWebResearchFunctionName, "call_id": callID, "query": input.Query})})
	}

	timeout := liveWebResearchTimeout
	if r.config.TurnTimeout > timeout {
		timeout = r.config.TurnTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	callCtx = WithLLMScope(callCtx, LLMScope{
		UserID:    userID,
		SessionID: session.ID,
		RequestID: requestIDFromContext(ctx),
	})
	researchSession := state.NewSession(session.WorkingDir)
	result, err := runner.RunGeneratedPrompt(callCtx, researchSession, liveWebResearchPrompt(input, displayText))
	output := strings.TrimSpace(result.Output)
	if output == "" {
		output = strings.TrimSpace(lastAssistantContent(researchSession))
	}
	if err != nil {
		output = formatLiveToolExecutionError(liveWebResearchFunctionName, err)
	}
	if output == "" {
		output = "No reliable web research result was produced."
	}

	session.AddAssistantMessageWithTools("", []state.ToolCall{{
		ID:    callID,
		Name:  liveWebResearchFunctionName,
		Input: json.RawMessage(normalizedInput),
	}})
	session.AddToolResult(callID, liveWebResearchFunctionName, json.RawMessage(normalizedInput), output)
	if persistErr := r.persistChatSession(ctx, userID, session, startMessageCount); persistErr != nil {
		if err != nil {
			return true, output, errors.Join(err, persistErr)
		}
		return true, output, persistErr
	}
	if sink != nil {
		eventType := "live_tool_result"
		if err != nil {
			eventType = "live_tool_error"
		}
		_ = sink.Send(ctx, Event{Type: eventType, SessionID: session.ID, Role: state.MessageRoleTool, Content: output, Data: liveJSON(map[string]any{"tool": liveWebResearchFunctionName, "call_id": callID, "query": input.Query})})
	}
	if err != nil {
		return true, output, err
	}
	return true, output, nil
}

func (r *Runtime) liveHarnessToolRunner(ctx context.Context, userID, sessionID string) (liveHarnessToolRunner, *state.Session, error) {
	if r == nil || r.engineFactory == nil {
		return nil, nil, nil
	}
	session, err := r.GetSession(ctx, userID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, nil
	}
	runner := r.runnerForScope(Scope{UserID: userID, SessionID: session.ID, WorkingDir: session.WorkingDir})
	toolRunner, ok := runner.(liveHarnessToolRunner)
	if !ok {
		return nil, session, nil
	}
	return toolRunner, session, nil
}

func liveHarnessToolAllowed(name string) bool {
	_, ok := liveHarnessToolAllowlist[strings.TrimSpace(name)]
	return ok
}

func liveRunnerHasWebTools(runner liveHarnessToolRunner) bool {
	if runner == nil {
		return false
	}
	hasSearch := false
	for _, descriptor := range runner.Descriptors() {
		if strings.EqualFold(strings.TrimSpace(descriptor.Name), "WebSearch") {
			hasSearch = true
			break
		}
	}
	return hasSearch
}

func (r *Runtime) liveHasUserInvocableSkills() bool {
	return r != nil && len(r.ListSkills()) > 0
}

func liveFunctionDeclarationFromDescriptor(descriptor toolkit.Descriptor) (map[string]any, error) {
	var parameters map[string]any
	if len(descriptor.InputSchema) > 0 {
		if err := json.Unmarshal(descriptor.InputSchema, &parameters); err != nil {
			return nil, fmt.Errorf("decode %s input schema: %w", descriptor.Name, err)
		}
	}
	if len(parameters) == 0 {
		parameters = map[string]any{"type": "OBJECT"}
	}
	parameters = liveNormalizeFunctionSchema(parameters).(map[string]any)
	return map[string]any{
		"name":        descriptor.Name,
		"description": liveHarnessToolDescription(descriptor),
		"parameters":  parameters,
	}, nil
}

func liveWebResearchFunctionDeclaration() map[string]any {
	return map[string]any{
		"name":        liveWebResearchFunctionName,
		"description": "Run a backend web research pass for the current Live voice turn. Use this instead of answering from memory when the user asks for current, recent, exact, numeric, sourced, or externally verifiable information. Especially use it for multi-step searches, comparisons, market/news/model/product lookups, date ranges, rankings, and requests that explicitly say to search the web. Do not speak a factual answer before this function returns.",
		"parameters": map[string]any{
			"type": "OBJECT",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "STRING",
					"description": "A complete standalone search/research question including the entity, date range, metric, geography, and required output.",
				},
				"requirements": map[string]any{
					"type":        "STRING",
					"description": "Specific constraints for the answer, such as exact numbers, sources, date range, comparison criteria, or preferred language.",
				},
				"reason": map[string]any{
					"type":        "STRING",
					"description": "A short reason why web research is required.",
				},
			},
			"required": []string{"query"},
		},
	}
}

func liveWebResearchStructuredSchema(requireQuery bool) StructuredSchema {
	required := []string{}
	if requireQuery {
		required = []string{"query"}
	}
	return StructuredSchema{
		Name:    liveWebResearchFunctionName,
		Version: "v1",
		Schema: map[string]any{
			"type":                 "object",
			"required":             required,
			"additionalProperties": false,
			"properties": map[string]any{
				"query":        map[string]any{"type": "string"},
				"requirements": map[string]any{"type": "string"},
				"reason":       map[string]any{"type": "string"},
			},
		},
	}
}

func prepareLiveHarnessToolInput(ctx context.Context, runner liveHarnessToolRunner, toolName string, args json.RawMessage, displayText string) (json.RawMessage, string, error) {
	schema, err := liveHarnessToolInputStructuredSchema(runner.Descriptors(), toolName)
	if err != nil || len(schema.Schema) == 0 {
		return args, "", err
	}
	result := ValidateStructuredJSON(args, schema)
	if result.Valid() {
		return args, "", nil
	}
	emitStructuredOutputValidationFailure(ctx, schema, toolName, result)
	repaired, repairErr := repairStructuredJSONWithRunner(ctx, runner, schema, string(args), result.Error(), liveToolRepairContext(toolName, displayText))
	if repairErr == nil {
		return repaired, structuredFallbackRepairRetry, nil
	}
	if fallbackArgs, ok := deterministicLiveHarnessToolInput(toolName, args, displayText); ok {
		if fallbackResult := ValidateStructuredJSON(fallbackArgs, schema); fallbackResult.Valid() {
			emitStructuredOutputFallbackEvent(ctx, schema, toolName, structuredFallbackDeterministic, nil)
			return fallbackArgs, structuredFallbackDeterministic, nil
		}
	}
	emitStructuredOutputFallbackEvent(ctx, schema, toolName, structuredFallbackStopAutoExecute, repairErr)
	return args, structuredFallbackStopAutoExecute, fmt.Errorf("live tool input invalid after %s: %w; repair failed: %v", structuredFallbackRepairRetry, result.Error(), repairErr)
}

func liveHarnessToolInputStructuredSchema(descriptors []toolkit.Descriptor, toolName string) (StructuredSchema, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return StructuredSchema{}, fmt.Errorf("live tool name is required")
	}
	for _, descriptor := range descriptors {
		if !strings.EqualFold(strings.TrimSpace(descriptor.Name), toolName) {
			continue
		}
		if len(descriptor.InputSchema) == 0 {
			return StructuredSchema{Name: descriptor.Name, Version: "v1"}, nil
		}
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
			return StructuredSchema{}, fmt.Errorf("decode %s input schema: %w", descriptor.Name, err)
		}
		return StructuredSchema{Name: descriptor.Name, Version: "v1", Schema: schema}, nil
	}
	return StructuredSchema{}, fmt.Errorf("live tool descriptor not found: %s", toolName)
}

func deterministicLiveHarnessToolInput(toolName string, args json.RawMessage, displayText string) (json.RawMessage, bool) {
	if !strings.EqualFold(strings.TrimSpace(toolName), "WebSearch") || strings.TrimSpace(displayText) == "" {
		return nil, false
	}
	var payload map[string]any
	if len(args) > 0 {
		_ = json.Unmarshal(args, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["query"] = strings.TrimSpace(displayText)
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(data), true
}

func liveToolRepairContext(toolName, displayText string) string {
	var builder strings.Builder
	if strings.TrimSpace(toolName) != "" {
		fmt.Fprintf(&builder, "Tool: %s\n", strings.TrimSpace(toolName))
	}
	if strings.TrimSpace(displayText) != "" {
		fmt.Fprintf(&builder, "Latest live utterance: %s\n", strings.TrimSpace(displayText))
	}
	return strings.TrimSpace(builder.String())
}

func liveHarnessToolDescription(descriptor toolkit.Descriptor) string {
	description := strings.TrimSpace(descriptor.Description)
	switch strings.TrimSpace(descriptor.Name) {
	case "WebSearch":
		return strings.TrimSpace(description + " Use this only when the user asks for current, recent, public, or externally verifiable information that is not already in the conversation.")
	case "WebFetch":
		return strings.TrimSpace(description + " Use this only when the user provides a specific URL or when a fetched page is needed after search.")
	default:
		return description
	}
}

type liveWebResearchInput struct {
	Query        string `json:"query"`
	Requirements string `json:"requirements,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func liveWebResearchArgs(raw json.RawMessage) liveWebResearchInput {
	var input liveWebResearchInput
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &input)
	}
	input.Query = strings.TrimSpace(input.Query)
	input.Requirements = strings.TrimSpace(input.Requirements)
	input.Reason = strings.TrimSpace(input.Reason)
	return input
}

func liveWebResearchPrompt(input liveWebResearchInput, displayText string) string {
	var builder strings.Builder
	builder.WriteString("You are executing a backend web research subtask for a Live voice conversation.\n")
	builder.WriteString("Use WebSearch first. Use WebFetch for the most relevant sources when snippets are insufficient. Do not ask follow-up questions.\n")
	builder.WriteString("Return a complete answer in the user's language, with concrete numbers, dates, and source URLs when available. If reliable data cannot be found, say what is missing instead of guessing.\n")
	builder.WriteString("Keep the answer concise enough for Live mode, but do not stop mid-sentence.\n\n")
	fmt.Fprintf(&builder, "Current date: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&builder, "Research question: %s\n", input.Query)
	if input.Requirements != "" {
		fmt.Fprintf(&builder, "Requirements: %s\n", input.Requirements)
	}
	if input.Reason != "" {
		fmt.Fprintf(&builder, "Why web research was requested: %s\n", input.Reason)
	}
	if strings.TrimSpace(displayText) != "" && strings.TrimSpace(displayText) != input.Query {
		fmt.Fprintf(&builder, "Latest live utterance/context: %s\n", strings.TrimSpace(displayText))
	}
	return builder.String()
}

func lastAssistantContent(session *state.Session) string {
	if session == nil {
		return ""
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		message := session.Messages[i]
		if message.Role == state.MessageRoleAssistant && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}

func liveNormalizeFunctionSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if strings.EqualFold(key, "type") {
				if text, ok := item.(string); ok {
					out[key] = strings.ToUpper(strings.TrimSpace(text))
					continue
				}
			}
			out[key] = liveNormalizeFunctionSchema(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = liveNormalizeFunctionSchema(item)
		}
		return out
	default:
		return value
	}
}

func liveSkillFunctionArgs(toolName string, raw json.RawMessage) (string, string) {
	var payload map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &payload)
	}
	skillName := firstStringValue(payload, "skill", "skill_name", "skillName", "name")
	args := firstStringValue(payload, "args", "arguments", "prompt", "request")
	if strings.EqualFold(strings.TrimSpace(toolName), "Skill") {
		skillName = firstNonEmptyString(skillName, firstStringValue(payload, "commandName", "command_name"))
	}
	return strings.TrimPrefix(strings.TrimSpace(skillName), "/"), strings.TrimSpace(args)
}

func firstStringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if values == nil {
			return ""
		}
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case fmt.Stringer:
			if strings.TrimSpace(typed.String()) != "" {
				return strings.TrimSpace(typed.String())
			}
		}
	}
	return ""
}

func formatLiveToolExecutionError(toolName string, err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("tool_error: %s failed: %s", strings.TrimSpace(toolName), err.Error())
}

func (r *Runtime) liveSkillContext() string {
	if r == nil {
		return ""
	}
	items := r.ListSkills()
	if len(items) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(formatSkillList(items))
	out.WriteString("\n\nLive mode has access to a `run_skill` function for published skills. Call `run_skill` only when the current user turn is an explicit slash command or a clear, unambiguous request for one listed skill. Artifact-producing work, including image generation, must be performed by `run_skill` backend skill/job events. Do not say you are generating an artifact, ask the user to wait for generation, or claim that a skill has run unless you have called `run_skill` or explicit skill/job results are present in the conversation.")
	return out.String()
}

func (r *Runtime) DetectLiveSkillCommand(ctx context.Context, userID, sessionID, text string) bool {
	_, ok := r.liveExplicitSkillCommand(text)
	return ok
}

func (r *Runtime) ExecuteLiveSkillCommand(ctx context.Context, userID, sessionID, text string, sink EventSink) (bool, error) {
	command, ok := r.liveExplicitSkillCommand(text)
	if !ok {
		return false, nil
	}
	handled, _, err := r.executeLiveSkillCommand(ctx, userID, sessionID, text, command, sink)
	return handled, err
}

func (r *Runtime) ExecuteLiveSkillFunctionCall(ctx context.Context, userID, sessionID, skillName, args, displayText string, sink EventSink) (bool, string, error) {
	if r == nil || r.skills == nil {
		return false, "", nil
	}
	skillName = strings.TrimPrefix(strings.TrimSpace(skillName), "/")
	if skillName == "" {
		return false, "", nil
	}
	skill, ok := r.skills.GetSkill(skillName)
	if !ok || skill == nil || !skill.UserInvocable || skill.IsHidden {
		return false, "", nil
	}
	args = strings.TrimSpace(args)
	command := "/" + skill.Name
	if args != "" {
		command += " " + args
	}
	if strings.TrimSpace(displayText) == "" {
		displayText = command
	}
	return r.executeLiveSkillCommand(ctx, userID, sessionID, displayText, command, sink)
}

func (r *Runtime) executeLiveSkillCommand(ctx context.Context, userID, sessionID, displayText, command string, sink EventSink) (bool, string, error) {
	if sink == nil {
		return true, "", fmt.Errorf("event sink is required")
	}
	session, err := r.GetSession(ctx, userID, sessionID)
	if err != nil {
		return true, "", err
	}
	ensureConsumerSecurityContext(session)
	if err := r.injectPersonalization(ctx, userID, session); err != nil {
		return true, "", err
	}
	startMessageCount := len(session.Messages)
	displayText = strings.TrimSpace(displayText)
	if displayText == "" {
		displayText = command
	}
	session.AddUserMessage(displayText)
	if err := sink.Send(ctx, Event{Type: "message", SessionID: session.ID, Role: state.MessageRoleUser, Content: displayText}); err != nil {
		return true, "", err
	}
	if err := sink.Send(ctx, Event{Type: "live_skill_start", SessionID: session.ID, Role: state.MessageRoleTool, Content: command, Data: liveJSON(map[string]any{"command": command})}); err != nil {
		return true, "", err
	}

	req := ChatRequest{UserID: userID, SessionID: session.ID, Content: command}
	if decision := r.RouteChat(req); decision.RunAsJob {
		if err := r.persistChatSession(ctx, userID, session, startMessageCount); err != nil {
			return true, "", err
		}
		job, err := r.CreateJob(ctx, req, firstNonEmptyString(decision.JobType, JobTypeSkill))
		if err != nil {
			return true, "", err
		}
		r.markJobUserMessageHidden(job.ID)
		if err := r.StartJob(ctx, job); err != nil {
			return true, "", err
		}
		if err := sink.Send(ctx, Event{Type: "job", SessionID: session.ID, JobID: job.ID, Job: job, JobReason: decision.Reason}); err != nil {
			return true, "", err
		}
		output := "Skill job started."
		if err := sink.Send(ctx, Event{Type: "live_skill_result", SessionID: session.ID, Role: state.MessageRoleTool, Content: output, Data: liveJSON(map[string]any{"command": command, "job_id": job.ID})}); err != nil {
			return true, "", err
		}
		return true, fmt.Sprintf("%s job_id=%s command=%s", output, job.ID, command), nil
	}
	if err := r.injectTurnMemoryContexts(ctx, userID, session, displayText); err != nil {
		return true, "", err
	}

	turnCtx, cancel := context.WithTimeout(ctx, r.config.TurnTimeout)
	turnKey := sessionKey(userID, session.ID)
	if err := r.start(turnKey, cancel, jobIDFromContext(ctx) != ""); err != nil {
		cancel()
		return true, "", err
	}
	turnFinished := false
	finishTurn := func() {
		if turnFinished {
			return
		}
		r.finish(turnKey)
		turnFinished = true
	}
	defer finishTurn()
	result, err := r.runSkillCommand(withHiddenUserMessage(turnCtx), req, userID, session, command, func(token string) {
		_ = sink.Send(ctx, Event{Type: "delta", SessionID: session.ID, Role: state.MessageRoleAssistant, Content: token})
	})
	if err != nil {
		stripTransientRuntimeContexts(session)
		r.appendFailedTurn(session, displayText, err)
		if saveErr := r.persistChatSession(ctx, userID, session, startMessageCount); saveErr != nil {
			_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, Error: err.Error()})
			return true, "", errors.Join(err, saveErr)
		}
		_ = sink.Send(ctx, Event{Type: "error", SessionID: session.ID, Error: err.Error()})
		return true, "", err
	}
	if result.Session == nil {
		return true, "", fmt.Errorf("skill runner returned no session")
	}
	session = result.Session
	r.sanitizeSessionAttachmentBlocks(session)
	if err := r.persistChatSession(ctx, userID, session, startMessageCount); err != nil {
		return true, "", err
	}
	if r.memory != nil {
		if err := r.afterTurnMemory(ctx, userID, session); err != nil {
			return true, "", err
		}
	}
	if err := sink.Send(ctx, Event{Type: "live_skill_result", SessionID: session.ID, Role: state.MessageRoleTool, Content: result.Output, Data: liveJSON(map[string]any{"command": command})}); err != nil {
		return true, "", err
	}
	if err := sink.Send(ctx, Event{Type: "message", SessionID: session.ID, Role: state.MessageRoleAssistant, Content: result.Output, Data: liveJSON(map[string]any{"source": "live_skill", "command": command})}); err != nil {
		return true, "", err
	}
	if err := sink.Send(ctx, Event{Type: "done", SessionID: session.ID}); err != nil {
		return true, "", err
	}
	return true, result.Output, nil
}

func (r *Runtime) liveExplicitSkillCommand(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || r == nil {
		return "", false
	}
	if strings.HasPrefix(text, "/") {
		parts := strings.SplitN(text, " ", 2)
		name := strings.TrimPrefix(strings.TrimSpace(parts[0]), "/")
		if name == "skills" {
			return "/skills", true
		}
		if _, ok := r.skillForPrompt(text); ok {
			return text, true
		}
		return "", false
	}
	return "", false
}

type liveSkillSelection struct {
	Action     string  `json:"action"`
	Skill      string  `json:"skill"`
	Args       string  `json:"args"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func liveSkillSelectionStructuredSchema() StructuredSchema {
	return StructuredSchema{
		Name:    "live_skill_selection",
		Version: "v1",
		Schema: map[string]any{
			"type":                 "object",
			"required":             []string{"action", "skill", "args", "confidence", "reason"},
			"additionalProperties": false,
			"properties": map[string]any{
				"action":     map[string]any{"type": "string", "enum": []any{"skill_call", "none"}},
				"skill":      map[string]any{"type": "string"},
				"args":       map[string]any{"type": "string"},
				"confidence": map[string]any{"type": "number"},
				"reason":     map[string]any{"type": "string"},
			},
		},
	}
}

func (r *Runtime) selectLiveSkillCommand(ctx context.Context, userID, sessionID, text string) (string, bool) {
	text = strings.TrimSpace(text)
	if r == nil || text == "" || r.engineFactory == nil {
		return "", false
	}
	items := r.ListSkills()
	if len(items) == 0 {
		return "", false
	}
	session, err := r.GetSession(ctx, userID, sessionID)
	if err != nil || session == nil {
		return "", false
	}
	callCtx, cancel := context.WithTimeout(ctx, liveSkillSelectionTimeout)
	defer cancel()
	callCtx = WithLLMScope(callCtx, LLMScope{
		UserID:    userID,
		SessionID: sessionID,
		RequestID: requestIDFromContext(ctx),
	})
	runner := r.runnerForScope(Scope{UserID: userID, SessionID: sessionID, WorkingDir: session.WorkingDir})
	result, err := runner.RunGeneratedPrompt(callCtx, state.NewSession(""), liveSkillSelectionPrompt(text, liveSkillSelectionRecentContext(session, 8), items))
	if err != nil {
		return "", false
	}
	selection, parseErr := parseLiveSkillSelectionWithError(result.Output)
	if parseErr != nil {
		schema := liveSkillSelectionStructuredSchema()
		emitStructuredOutputValidationFailure(callCtx, schema, "live_skill_selection", ExtractAndValidateStructuredObject(result.Output, schema))
		repaired, repairErr := repairStructuredJSONWithRunner(callCtx, runner, schema, result.Output, parseErr, "Latest live utterance: "+text)
		if repairErr != nil {
			emitStructuredOutputFallbackEvent(callCtx, schema, "live_skill_selection", structuredFallbackStopAutoExecute, repairErr)
			return "", false
		}
		selection, parseErr = parseLiveSkillSelectionWithError(string(repaired))
	}
	if parseErr != nil || !strings.EqualFold(strings.TrimSpace(selection.Action), "skill_call") {
		return "", false
	}
	if selection.Confidence > 0 && selection.Confidence < 0.55 {
		return "", false
	}
	skill, ok := r.skills.GetSkill(strings.TrimSpace(selection.Skill))
	if !ok || skill == nil || !skill.UserInvocable || skill.IsHidden {
		return "", false
	}
	args := strings.TrimSpace(selection.Args)
	if args == "" {
		args = text
	}
	return "/" + skill.Name + " " + args, true
}

func liveSkillSelectionPrompt(userText, recentContext string, items []*skills.SkillDefinition) string {
	var catalog strings.Builder
	for _, skill := range items {
		if skill == nil || !skill.UserInvocable || skill.IsHidden {
			continue
		}
		catalog.WriteString("- name: ")
		catalog.WriteString(skill.Name)
		if strings.TrimSpace(skill.DisplayName) != "" {
			catalog.WriteString("\n  display_name: ")
			catalog.WriteString(skill.DisplayName)
		}
		if len(skill.Aliases) > 0 {
			catalog.WriteString("\n  aliases: ")
			catalog.WriteString(strings.Join(skill.Aliases, ", "))
		}
		if strings.TrimSpace(skill.Description) != "" {
			catalog.WriteString("\n  description: ")
			catalog.WriteString(skill.Description)
		}
		if strings.TrimSpace(skill.WhenToUse) != "" {
			catalog.WriteString("\n  when_to_use: ")
			catalog.WriteString(skill.WhenToUse)
		}
		if strings.TrimSpace(skill.ArgumentHint) != "" {
			catalog.WriteString("\n  args_hint: ")
			catalog.WriteString(skill.ArgumentHint)
		}
		if skill.RunAsJob || skill.ExecutionContext == skills.ContextFork {
			catalog.WriteString("\n  run_mode: job")
		}
		if skillProducesArtifacts(skill) {
			catalog.WriteString("\n  produces_artifacts: true")
		}
		catalog.WriteString("\n")
	}
	if strings.TrimSpace(recentContext) == "" {
		recentContext = "(none)"
	}
	return fmt.Sprintf(`You are a strict router for a live voice Agent product.

Decide whether the user's latest utterance should be executed by exactly one published skill. Use the recent conversation only to resolve short follow-ups like "continue", "you decide", "that one", or "yes"; the latest utterance remains the trigger.

Return ONLY one JSON object, no markdown:
{"action":"skill_call","skill":"<skill_name>","args":"<natural language arguments>","confidence":0.0,"reason":"short reason"}

If no skill should run, return:
{"action":"none","skill":"","args":"","confidence":0.0,"reason":"short reason"}

Rules:
- Select a skill only when the user is asking the system to create, transform, analyze, fetch, generate, or process something that clearly matches a skill.
- If the user asks to create or generate an image, picture, drawing, visual, file, or other artifact, select the best matching artifact/image skill when one is available.
- If the latest utterance is a confirmation or continuation of a recent artifact/image request, select the matching skill and preserve the concrete request from context in args.
- Do not select a skill for greetings, small talk, status questions, explanations about available skills, or ambiguous requests.
- Use only skill names from the catalog.
- Preserve the user's concrete request in args, without adding unsupported requirements.

Available skills:
%s

Recent conversation:
%s

User utterance:
%q
`, catalog.String(), recentContext, userText)
}

func liveSkillSelectionRecentContext(session *state.Session, maxMessages int) string {
	if session == nil || maxMessages <= 0 {
		return ""
	}
	type line struct {
		role    string
		content string
	}
	lines := make([]line, 0, maxMessages)
	for i := len(session.Messages) - 1; i >= 0 && len(lines) < maxMessages; i-- {
		message := session.Messages[i]
		if message.Hidden || (message.Role != state.MessageRoleUser && message.Role != state.MessageRoleAssistant) {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		lines = append(lines, line{role: message.Role, content: content})
	}
	if len(lines) == 0 {
		return ""
	}
	var out strings.Builder
	for i := len(lines) - 1; i >= 0; i-- {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(lines[i].role)
		out.WriteString(": ")
		out.WriteString(lines[i].content)
	}
	return out.String()
}

func parseLiveSkillSelection(output string) (liveSkillSelection, bool) {
	selection, err := parseLiveSkillSelectionWithError(output)
	return selection, err == nil
}

func parseLiveSkillSelectionWithError(output string) (liveSkillSelection, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return liveSkillSelection{}, fmt.Errorf("live skill selection output is empty")
	}
	result := ExtractAndValidateStructuredObject(output, liveSkillSelectionStructuredSchema())
	if !result.Valid() {
		return liveSkillSelection{}, result.Error()
	}
	data, err := json.Marshal(result.Value)
	if err != nil {
		return liveSkillSelection{}, err
	}
	var selection liveSkillSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return liveSkillSelection{}, err
	}
	return selection, nil
}

func liveSkillLabels(skill *skills.SkillDefinition) []string {
	seen := map[string]bool{}
	var labels []string
	for _, label := range append([]string{skill.Name, skill.DisplayName}, skill.Aliases...) {
		label = strings.TrimSpace(label)
		key := strings.ToLower(label)
		if label == "" || seen[key] {
			continue
		}
		seen[key] = true
		labels = append(labels, label)
	}
	return labels
}

func liveSkillArgsForLabel(text, label string) (string, bool) {
	lowerText := strings.ToLower(text)
	lowerLabel := strings.ToLower(strings.TrimSpace(label))
	if lowerLabel == "" {
		return "", false
	}
	patterns := []string{
		"使用 " + lowerLabel + " 技能",
		"使用" + lowerLabel + "技能",
		"调用 " + lowerLabel + " 技能",
		"调用" + lowerLabel + "技能",
		"用 " + lowerLabel + " 技能",
		"用" + lowerLabel + "技能",
		"use " + lowerLabel + " skill",
		"call " + lowerLabel + " skill",
	}
	for _, pattern := range patterns {
		idx := strings.Index(lowerText, pattern)
		if idx < 0 {
			continue
		}
		return trimLiveSkillArgs(text[idx+len(pattern):]), true
	}
	return "", false
}

func trimLiveSkillArgs(text string) string {
	text = strings.TrimSpace(strings.Trim(text, " \t\r\n:：,，.。;；"))
	for _, prefix := range []string{"帮我", "帮忙", "来", "请", "please"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	return strings.TrimSpace(strings.Trim(text, " \t\r\n:：,，.。;；"))
}

func (r *Runtime) RecordLiveTurn(ctx context.Context, userID, sessionID, userText, assistantText, model string) error {
	if r == nil {
		return fmt.Errorf("runtime is not configured")
	}
	session, err := r.GetSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	startMessageCount := len(session.Messages)
	now := time.Now().UTC()
	if strings.TrimSpace(userText) != "" {
		session.Messages = append(session.Messages, state.Message{
			Role:        state.MessageRoleUser,
			ContentType: state.MessageContentTypeText,
			Content:     strings.TrimSpace(userText),
			Status:      state.MessageStatusNormal,
			ModelID:     model,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if strings.TrimSpace(assistantText) != "" {
		session.Messages = append(session.Messages, state.Message{
			Role:        state.MessageRoleAssistant,
			ContentType: state.MessageContentTypeText,
			Content:     strings.TrimSpace(assistantText),
			Status:      state.MessageStatusNormal,
			ModelID:     model,
			CreatedAt:   now.Add(time.Millisecond),
			UpdatedAt:   now.Add(time.Millisecond),
		})
	}
	if len(session.Messages) == startMessageCount {
		return nil
	}
	if err := r.persistChatSession(ctx, userID, session, startMessageCount); err != nil {
		return err
	}
	if r.memory != nil {
		if err := r.afterTurnMemory(ctx, userID, session); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) persistChatSession(ctx context.Context, userID string, session *state.Session, startMessageCount int) error {
	if r == nil || session == nil {
		return fmt.Errorf("session is required")
	}
	if r.messageWriter == nil || startMessageCount < 0 || startMessageCount > len(session.Messages) {
		if err := r.sessions.Save(ctx, userID, session); err != nil {
			return err
		}
		r.publishSavedTurnMessageEvents(ctx, userID, session, startMessageCount)
		return nil
	}
	if startMessageCount < len(session.Messages) {
		created, err := r.messageWriter.WriteMany(ctx, userID, session.ID, session.Messages[startMessageCount:])
		if err != nil {
			return err
		}
		copy(session.Messages[startMessageCount:], created)
		if r.messageSearch != nil {
			_ = r.messageSearch.InvalidateUserCache(ctx, userID)
		}
	}
	if metadataStore, ok := r.sessions.(SessionMetadataStore); ok && metadataStore != nil {
		return metadataStore.SaveSessionMetadata(ctx, userID, session)
	}
	return r.sessions.Save(ctx, userID, session)
}

func (r *Runtime) publishSavedTurnMessageEvents(ctx context.Context, userID string, session *state.Session, startMessageCount int) {
	if r == nil || session == nil || startMessageCount < 0 {
		return
	}
	if r.messagePublisher == nil && (!r.localVectorIndex || r.vectorIndexer == nil) {
		return
	}
	messages := session.Messages
	if startMessageCount >= len(messages) {
		saved, err := r.sessions.Get(ctx, userID, session.ID)
		if err != nil {
			logError(ctx, r.logger, "load saved messages for event publishing failed", err, contextLogAttrs(ctx, userID, session.ID, "")...)
			return
		}
		if saved != nil {
			messages = saved.Messages
		}
	}
	if startMessageCount >= len(messages) {
		return
	}
	for _, message := range messages[startMessageCount:] {
		if strings.TrimSpace(message.ID) == "" {
			continue
		}
		event := MessageEvent{
			Type:      MessageEventCreated,
			UserID:    userID,
			SessionID: session.ID,
			Message:   message,
			CreatedAt: time.Now().UTC(),
		}
		if r.messagePublisher != nil {
			if err := r.messagePublisher.PublishMessageEvent(ctx, event); err != nil {
				attrs := contextLogAttrs(ctx, event.UserID, event.SessionID, "")
				attrs = append(attrs, slog.String("message_id", event.Message.ID))
				logError(ctx, r.logger, "publish message event failed", err, attrs...)
			}
		}
		if r.localVectorIndex && r.vectorIndexer != nil {
			_ = r.vectorIndexer.PublishMessageEvent(ctx, event)
		}
	}
}

type messageListStore interface {
	ListMessages(ctx context.Context, userID, sessionID string) ([]state.Message, error)
}

func (r *Runtime) loadMessagesForIndexDelete(ctx context.Context, userID, sessionID string) ([]state.Message, error) {
	store, ok := r.sessions.(messageListStore)
	if !ok || store == nil {
		return nil, nil
	}
	messages, err := store.ListMessages(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	return cloneStateMessages(messages), nil
}

func (r *Runtime) loadUserMessagesForIndexDelete(ctx context.Context, userID string) ([]state.Message, error) {
	if r.sessions == nil {
		return nil, nil
	}
	sessions, err := r.sessions.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]state.Message, 0)
	for _, session := range sessions {
		if session == nil || strings.TrimSpace(session.ID) == "" {
			continue
		}
		messages, err := r.loadMessagesForIndexDelete(ctx, userID, session.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, messages...)
	}
	return out, nil
}

func (r *Runtime) publishDeletedMessageEvents(ctx context.Context, userID string, messages []state.Message) {
	if r == nil || len(messages) == 0 {
		return
	}
	now := time.Now().UTC()
	for _, message := range messages {
		if strings.TrimSpace(message.ID) == "" {
			continue
		}
		message.UserID = firstNonEmptyString(message.UserID, userID)
		message.Status = state.MessageStatusDeleted
		message.UpdatedAt = now
		event := MessageEvent{
			Type:      MessageEventDeleted,
			UserID:    message.UserID,
			SessionID: message.SessionID,
			Message:   message,
			CreatedAt: now,
		}
		if r.messagePublisher != nil {
			if err := r.messagePublisher.PublishMessageEvent(ctx, event); err != nil {
				attrs := contextLogAttrs(ctx, event.UserID, event.SessionID, "")
				attrs = append(attrs, slog.String("message_id", message.ID))
				logError(ctx, r.logger, "publish deleted message event failed", err, attrs...)
			}
		}
		if r.localVectorIndex && r.vectorIndexer != nil {
			_ = r.vectorIndexer.PublishMessageEvent(ctx, event)
		}
	}
}

func (r *Runtime) afterTurnMemory(ctx context.Context, userID string, session *state.Session) error {
	if r == nil || session == nil {
		return nil
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return err
	}
	if !settings.CaptureEnabled {
		return nil
	}
	if r.memory != nil {
		service, ok := r.memory.(MemoryItemService)
		if !ok || r.memoryExtract == nil {
			if err := r.memory.AfterTurn(ctx, userID, session); err != nil {
				return err
			}
		} else {
			candidates, err := r.memoryExtract.Extract(ctx, MemoryExtractionInput{
				UserID:    userID,
				SessionID: session.ID,
				Messages:  session.Messages,
				Now:       time.Now().UTC(),
			})
			if err != nil {
				if err := r.memory.AfterTurn(ctx, userID, session); err != nil {
					return err
				}
			} else {
				items := evaluateMemoryCandidates(userID, session.ID, candidates)
				if len(items) > 0 {
					existing, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{})
					if err != nil {
						return err
					}
					for _, candidate := range items {
						var conflictUpdates []MemoryItem
						candidate, conflictUpdates = applyMemoryConflictResolution(existing, candidate)
						for _, update := range conflictUpdates {
							if _, err := service.UpdateMemoryItem(ctx, userID, update); err != nil {
								return err
							}
							existing = append(existing, update)
						}
						item := upsertMemoryItem(existing, candidate)
						if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
							return err
						}
						existing = append(existing, item)
					}
					if err := r.markMemoryAbstractionsDirty(ctx, userID, service); err != nil {
						return err
					}
				}
			}
		}
	}
	return r.captureEpisodeAfterTurn(ctx, userID, session)
}

func (r *Runtime) captureEpisodeAfterTurn(ctx context.Context, userID string, session *state.Session) error {
	if r == nil || r.episodeMemory == nil || r.episodeSummarizer == nil || session == nil {
		return nil
	}
	config := normalizeEpisodicMemoryConfig(r.config.EpisodicMemory)
	if !config.Enabled || !config.CaptureEnabled {
		return nil
	}
	episode, ok, err := buildMemoryEpisodeFromSessionWithConfig(ctx, userID, session, r.episodeSummarizer, time.Now().UTC(), config)
	if err != nil || !ok {
		return err
	}
	_, err = r.episodeMemory.UpsertMemoryEpisode(ctx, userID, episode)
	return err
}

func (r *Runtime) recordEpisodeRecall(ctx context.Context, userID string, results []MemoryEpisodeSearchResult) {
	if r == nil || r.episodeMemory == nil {
		return
	}
	for _, result := range results {
		if strings.TrimSpace(result.Episode.ID) == "" {
			continue
		}
		if err := r.episodeMemory.RecordMemoryEpisodeRecall(ctx, userID, result.Episode.ID, result.Score); err != nil {
			logError(ctx, r.logger, "memory episode recall record failed", err, contextLogAttrs(ctx, userID, result.Episode.SessionID, result.Episode.ID)...)
		}
	}
}

func (r *Runtime) injectTurnEpisodeContexts(ctx context.Context, userID string, session *state.Session, query string) error {
	if r == nil || r.episodeMemory == nil || session == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	content, results, err := r.searchTurnEpisodeContexts(ctx, userID, query)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	session.AddSystemContext(episodicMemoryContextMarker + "\n" + content + "\n</episodic-memory>")
	r.recordEpisodeRecall(ctx, userID, results)
	return nil
}

func (r *Runtime) searchTurnEpisodeContexts(ctx context.Context, userID, query string) (string, []MemoryEpisodeSearchResult, error) {
	if r == nil || r.episodeMemory == nil || strings.TrimSpace(query) == "" {
		return "", nil, nil
	}
	config := normalizeEpisodicMemoryConfig(r.config.EpisodicMemory)
	if !config.Enabled || !config.ContextEnabled {
		return "", nil, nil
	}
	results, err := r.episodeMemory.SearchMemoryEpisodes(ctx, userID, query, MemoryEpisodeSearchOptions{
		Limit:    config.InjectLimit,
		MinScore: 0.12,
	})
	if err != nil {
		return "", nil, err
	}
	return formatEpisodeContextForPrompt(results), results, nil
}

func (r *Runtime) shouldScheduleAfterTurnMemory(session *state.Session) bool {
	if r == nil || session == nil {
		return false
	}
	return r.memory != nil || r.episodeMemory != nil
}

func (r *Runtime) scheduleAfterTurnMemory(ctx context.Context, userID string, session *state.Session) {
	if !r.shouldScheduleAfterTurnMemory(session) {
		return
	}
	sessionCopy := cloneSessionForBackgroundMemory(session)
	logCtx := context.WithoutCancel(ctx)
	go func() {
		runCtx, cancel := context.WithTimeout(logCtx, afterTurnMemoryTimeout)
		defer cancel()
		if err := r.afterTurnMemory(runCtx, userID, sessionCopy); err != nil {
			logError(runCtx, r.logger, "after-turn memory failed", err, contextLogAttrs(logCtx, userID, sessionCopy.ID, "")...)
		}
	}()
}

func cloneSessionForBackgroundMemory(session *state.Session) *state.Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.Tags = append([]string(nil), session.Tags...)
	clone.Metadata = cloneStringMap(session.Metadata)
	clone.Messages = cloneStateMessages(session.Messages)
	return &clone
}

func (r *Runtime) markMemoryAbstractionsDirty(ctx context.Context, userID string, service MemoryItemService) error {
	items, err := service.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: MemoryStatusActive})
	if err != nil {
		return err
	}
	for _, item := range items {
		item = normalizeMemoryItem(item)
		if item.Source != MemorySourceSystem || (item.Level != MemoryLevelConcept && item.Level != MemoryLevelProfile) {
			continue
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		item.Metadata["dirty"] = true
		item.UpdatedAt = time.Now().UTC()
		if _, err := service.UpdateMemoryItem(ctx, userID, item); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) appendFailedTurn(session *state.Session, userContent string, runErr error) {
	if session == nil || runErr == nil {
		return
	}
	ensureVisibleUserMessage(session, userContent)
	session.AddAssistantMessage("Request failed: " + runErr.Error())
}

func ensureVisibleUserMessage(session *state.Session, content string) {
	if session == nil || strings.TrimSpace(content) == "" {
		return
	}
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Hidden {
			continue
		}
		if msg.Role == "user" && msg.Content == content {
			return
		}
		if msg.Role == "user" || msg.Role == "assistant" {
			break
		}
	}
	session.AddUserMessage(content)
}

func webSearchPrompt(content string) string {
	return "请使用网页搜索查找最新资料，并基于可靠来源回答：" + strings.TrimSpace(content)
}

func normalizeWebSearchTurnMessages(session *state.Session, startIndex int, visibleContent, internalContent string, visibleBlocks []publictypes.ContentBlock) {
	normalizeInternalUserPromptTurnMessages(session, startIndex, visibleContent, internalContent, visibleBlocks)
}

func normalizeInternalUserPromptTurnMessages(session *state.Session, startIndex int, visibleContent, internalContent string, visibleBlocks []publictypes.ContentBlock) {
	if session == nil {
		return
	}
	visibleContent = strings.TrimSpace(visibleContent)
	internalContent = strings.TrimSpace(internalContent)
	if visibleContent == "" || internalContent == "" || visibleContent == internalContent {
		return
	}
	if startIndex < 0 {
		startIndex = 0
	}
	if startIndex > len(session.Messages) {
		startIndex = len(session.Messages)
	}
	visibleFound := false
	for i := startIndex; i < len(session.Messages); i++ {
		message := &session.Messages[i]
		if message.Role != state.MessageRoleUser {
			continue
		}
		if strings.TrimSpace(message.Content) == internalContent || strings.TrimSpace(promptContentText(message.ContentBlocks)) == internalContent {
			message.Hidden = true
			continue
		}
		if !message.Hidden && strings.TrimSpace(message.Content) == visibleContent {
			visibleFound = true
		}
	}
	if visibleFound {
		return
	}
	message := visibleUserTranscriptMessage(visibleContent, visibleBlocks)
	session.Messages = append(session.Messages, state.Message{})
	copy(session.Messages[startIndex+1:], session.Messages[startIndex:])
	session.Messages[startIndex] = message
}

func visibleUserTranscriptMessage(content string, blocks []publictypes.ContentBlock) state.Message {
	now := time.Now().UTC()
	message := state.Message{
		Role:          state.MessageRoleUser,
		ContentType:   state.MessageContentTypeText,
		Content:       content,
		Status:        state.MessageStatusNormal,
		IsContextUsed: true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if len(blocks) > 0 {
		message.ContentType = state.MessageContentTypeMultipart
		message.ContentParts = append([]publictypes.ContentBlock(nil), blocks...)
		message.ContentBlocks = append([]publictypes.ContentBlock(nil), blocks...)
	}
	return message
}

func (r *Runtime) CreateJob(ctx context.Context, req ChatRequest, jobType string) (*Job, error) {
	if r.jobs == nil {
		return nil, fmt.Errorf("job store is not configured")
	}
	if strings.TrimSpace(req.UserID) == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	if strings.TrimSpace(req.Content) == "" && len(req.AttachmentIDs) == 0 && len(req.AttachmentURLs) == 0 {
		return nil, fmt.Errorf("content or attachment is required")
	}
	req.ConnectorContext = r.resolveConnectorContext(ctx, req)
	if _, err := r.GetSession(ctx, req.UserID, req.SessionID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	normalizedJobType := firstNonEmptyString(jobType, JobTypeChat)
	loopGoalID := strings.TrimSpace(req.LoopGoalID)
	if normalizedJobType == JobTypeDeepAgent && r.loopGoals != nil {
		goalText := strings.TrimSpace(req.Content)
		if goalText == "" {
			goalText = "Please analyze the attached file(s)."
		}
		if loopGoalID == "" {
			goal := normalizeLoopGoal(&LoopGoal{
				UserID:      req.UserID,
				SessionID:   req.SessionID,
				Objective:   goalText,
				TaskType:    "deep_agent",
				Deliverable: "answer",
				Budget:      loopBudgetFromDeepAgentPolicy(defaultDeepAgentJobPolicy()),
				Trigger: LoopTrigger{
					Type:   LoopTriggerManual,
					Source: "agent_mode:plan_execute",
				},
				Status:    LoopGoalStatusPending,
				CreatedAt: now,
				UpdatedAt: now,
			})
			if err := r.loopGoals.UpsertLoopGoal(ctx, goal); err != nil {
				return nil, err
			}
			loopGoalID = goal.ID
		} else if _, err := r.loopGoals.GetLoopGoal(ctx, req.UserID, loopGoalID); err != nil {
			return nil, err
		}
	}
	job := &Job{
		ID:               NewJobID(),
		UserID:           req.UserID,
		SessionID:        req.SessionID,
		LoopGoalID:       loopGoalID,
		Type:             normalizedJobType,
		Status:           JobStatusQueued,
		Content:          req.Content,
		AttachmentIDs:    append([]string(nil), req.AttachmentIDs...),
		AttachmentURLs:   append([]ChatAttachmentURL(nil), req.AttachmentURLs...),
		ConnectorContext: normalizeConnectorScopes(req.ConnectorContext),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := r.jobs.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	if normalizedJobType == JobTypeDeepAgent && r.loopGoals != nil && loopGoalID != "" {
		if err := r.loopGoals.UpdateLoopGoalRun(ctx, req.UserID, loopGoalID, job.ID, "", LoopGoalStatusPending, now); err != nil {
			return nil, err
		}
	}
	return job, nil
}

func defaultDeepAgentJobPolicy() DeepAgentPolicy {
	return DeepAgentPolicy{
		MaxSteps:        6,
		MaxActions:      12,
		MaxDuration:     10 * time.Minute,
		StepTimeout:     5 * time.Minute,
		NoProgressLimit: 2,
	}
}

func (r *Runtime) StartJob(ctx context.Context, job *Job) error {
	if r.jobs == nil {
		return fmt.Errorf("job store is not configured")
	}
	if job == nil {
		return fmt.Errorf("job is required")
	}
	if _, err := r.jobs.GetJob(ctx, job.UserID, job.ID); err != nil {
		return err
	}
	hideUserMessage := r.consumeJobUserMessageHidden(job.ID)
	if r.jobQueue != nil {
		return r.jobQueue.EnqueueJob(ctx, JobQueueItem{
			JobID:           job.ID,
			UserID:          job.UserID,
			RequestID:       requestIDFromContext(ctx),
			HideUserMessage: hideUserMessage,
		})
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	workerCtx = withRequestID(workerCtx, requestIDFromContext(ctx))
	workerCtx = WithJobID(workerCtx, job.ID)
	if hideUserMessage {
		workerCtx = withHiddenUserMessage(workerCtx)
	}
	if err := r.startJob(job.ID, cancel); err != nil {
		cancel()
		return err
	}
	go r.runJob(workerCtx, job)
	return nil
}

func (r *Runtime) RunQueuedJob(ctx context.Context, item JobQueueItem) error {
	if r.jobs == nil {
		return fmt.Errorf("job store is not configured")
	}
	jobID := strings.TrimSpace(item.JobID)
	userID := strings.TrimSpace(item.UserID)
	if jobID == "" || userID == "" {
		return fmt.Errorf("job id and user id are required")
	}
	job, err := r.jobs.GetJob(ctx, userID, jobID)
	if err != nil {
		return err
	}
	if isTerminalJobStatus(job.Status) {
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	workerCtx = withRequestID(workerCtx, item.RequestID)
	workerCtx = WithJobID(workerCtx, job.ID)
	if item.HideUserMessage {
		workerCtx = withHiddenUserMessage(workerCtx)
	}
	if err := r.startJob(job.ID, cancel); err != nil {
		cancel()
		return err
	}
	return r.runJob(workerCtx, job)
}

func withHiddenUserMessage(ctx context.Context) context.Context {
	return context.WithValue(ctx, hiddenUserMessageContextKey{}, true)
}

func hideUserMessageFromContext(ctx context.Context) bool {
	hidden, _ := ctx.Value(hiddenUserMessageContextKey{}).(bool)
	return hidden
}

func (r *Runtime) markJobUserMessageHidden(jobID string) {
	jobID = strings.TrimSpace(jobID)
	if r == nil || jobID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hiddenJobUserMessages == nil {
		r.hiddenJobUserMessages = make(map[string]bool)
	}
	r.hiddenJobUserMessages[jobID] = true
}

func (r *Runtime) consumeJobUserMessageHidden(jobID string) bool {
	jobID = strings.TrimSpace(jobID)
	if r == nil || jobID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hiddenJobUserMessages[jobID] {
		return false
	}
	delete(r.hiddenJobUserMessages, jobID)
	return true
}

func (r *Runtime) runJob(ctx context.Context, job *Job) error {
	defer r.finishJob(job.ID)
	now := time.Now().UTC()
	if err := r.jobs.UpdateJobStatus(ctx, job.UserID, job.ID, JobStatusRunning, "", now); err != nil {
		return err
	}
	sink := &jobEventSink{store: r.jobs, bus: r.jobEvents, fanout: r.jobEventFanout, job: job, logger: componentLogger(r.logger, "job_event_fanout")}
	ctx = withJobEventEmitter(ctx, sink.Send)
	var err error
	switch job.Type {
	case JobTypeDeepAgent:
		err = r.runDeepAgentJob(ctx, job, sink)
	default:
		err = r.Chat(ctx, ChatRequest{UserID: job.UserID, SessionID: job.SessionID, Content: job.Content, AttachmentIDs: job.AttachmentIDs, AttachmentURLs: job.AttachmentURLs, ConnectorContext: job.ConnectorContext}, sink)
	}
	finishedAt := time.Now().UTC()
	if current, loadErr := r.jobs.GetJob(context.Background(), job.UserID, job.ID); loadErr == nil && current.Status == JobStatusCancelled {
		return nil
	}
	switch {
	case err == nil:
		return r.jobs.UpdateJobStatus(context.Background(), job.UserID, job.ID, JobStatusSucceeded, "", finishedAt)
	case errors.Is(err, context.Canceled) || errors.Is(err, ErrRuntimeShuttingDown):
		updateErr := r.jobs.UpdateJobStatus(context.Background(), job.UserID, job.ID, JobStatusCancelled, err.Error(), finishedAt)
		sendErr := sink.Send(context.Background(), Event{Type: "cancelled", SessionID: job.SessionID, JobID: job.ID})
		return errors.Join(updateErr, sendErr)
	default:
		updateErr := r.jobs.UpdateJobStatus(context.Background(), job.UserID, job.ID, JobStatusFailed, err.Error(), finishedAt)
		if !strings.HasPrefix(strings.TrimSpace(job.Content), "/") {
			r.recordExecutionDenialRisk(ctx, job.UserID, job.SessionID, "", err, map[string]any{"phase": "job", "job_type": job.Type})
		}
		return updateErr
	}
}

func (r *Runtime) runDeepAgentJob(ctx context.Context, job *Job, sink EventSink) error {
	if r == nil {
		return fmt.Errorf("runtime is not configured")
	}
	if job == nil {
		return fmt.Errorf("job is required")
	}
	if sink == nil {
		return fmt.Errorf("event sink is required")
	}
	goal := strings.TrimSpace(job.Content)
	if goal == "" {
		goal = "Please analyze the attached file(s)."
	}
	loopGoal := r.loadJobLoopGoal(ctx, job)
	if loopGoal != nil && strings.TrimSpace(loopGoal.Objective) != "" {
		goal = strings.TrimSpace(loopGoal.Objective)
	}
	session, err := r.GetSession(ctx, job.UserID, job.SessionID)
	if err != nil {
		_ = sink.Send(ctx, Event{Type: "error", SessionID: job.SessionID, JobID: job.ID, Error: err.Error()})
		return err
	}
	turnCtx, cancel := context.WithCancel(ctx)
	turnKey := sessionKey(job.UserID, session.ID)
	if err := r.start(turnKey, cancel, true); err != nil {
		cancel()
		_ = sink.Send(ctx, Event{Type: "error", SessionID: job.SessionID, JobID: job.ID, Error: err.Error()})
		return err
	}
	defer cancel()
	defer r.finish(turnKey)
	startMessageCount := len(session.Messages)
	ensureVisibleUserMessage(session, goal)
	if err := r.persistChatSession(ctx, job.UserID, session, startMessageCount); err != nil {
		_ = sink.Send(ctx, Event{Type: "error", SessionID: job.SessionID, JobID: job.ID, Error: err.Error()})
		return err
	}
	_ = sink.Send(ctx, Event{Type: "deep_agent_started", SessionID: job.SessionID, JobID: job.ID, Role: "workflow", Content: "Plan-and-execute task started"})
	policy := defaultDeepAgentJobPolicy()
	if loopGoal != nil {
		policy = deepAgentPolicyFromLoopBudget(loopGoal.Budget, policy)
		_ = r.loopGoals.UpdateLoopGoalRun(ctx, job.UserID, loopGoal.ID, job.ID, "", LoopGoalStatusRunning, time.Now().UTC())
	}
	state := map[string]any{
		"attachment_ids":    append([]string(nil), job.AttachmentIDs...),
		"attachment_urls":   append([]ChatAttachmentURL(nil), job.AttachmentURLs...),
		"connector_context": normalizeConnectorScopes(job.ConnectorContext),
	}
	if loopGoal != nil {
		state["loop_goal_id"] = loopGoal.ID
		state["loop_goal"] = loopGoal
		state["loop_goal_rubric"] = loopGoal.Rubric
		state["loop_goal_trigger"] = loopGoal.Trigger
		state["loop_goal_budget"] = loopGoal.Budget
	}
	if trigger, ok := r.loopTriggerForJob(job.ID); ok {
		state["loop_trigger"] = trigger
		state["loop_trigger_type"] = trigger.Type
		state["loop_trigger_source"] = trigger.Source
		state["loop_trigger_dedupe_key"] = trigger.DedupeKey
		_ = sink.Send(ctx, Event{Type: "loop_trigger_started", SessionID: job.SessionID, JobID: job.ID, Role: "workflow", Content: trigger.Type, Data: deepAgentEventData(map[string]any{"loop_trigger": trigger})})
	}
	result, err := r.ExecuteDeepAgentTask(turnCtx, DeepAgentTaskRequest{
		UserID:           job.UserID,
		SessionID:        job.SessionID,
		JobID:            job.ID,
		LoopGoalID:       job.LoopGoalID,
		Goal:             goal,
		Policy:           policy,
		Rubric:           deepAgentRubricFromLoopRubric(loopGoalRubric(loopGoal)),
		LoopGoal:         loopGoal,
		State:            state,
		ConnectorContext: normalizeConnectorScopes(job.ConnectorContext),
	}, nil, nil, nil)
	r.updateLoopGoalFromDeepAgentResult(ctx, job, loopGoal, result, err)
	if err != nil {
		messageErr := r.appendDeepAgentResultMessage(ctx, job.UserID, job.SessionID, result, err)
		_ = sink.Send(ctx, Event{Type: "error", SessionID: job.SessionID, JobID: job.ID, Error: err.Error()})
		if messageErr != nil {
			return errors.Join(err, messageErr)
		}
		return err
	}
	if err := r.appendDeepAgentResultMessage(ctx, job.UserID, job.SessionID, result, nil); err != nil {
		_ = sink.Send(ctx, Event{Type: "error", SessionID: job.SessionID, JobID: job.ID, Error: err.Error()})
		return err
	}
	_ = sink.Send(ctx, Event{Type: "deep_agent_completed", SessionID: job.SessionID, JobID: job.ID, Role: "workflow", Content: "Plan-and-execute task completed"})
	return sink.Send(ctx, Event{Type: "done", SessionID: job.SessionID, JobID: job.ID})
}

func (r *Runtime) loadJobLoopGoal(ctx context.Context, job *Job) *LoopGoal {
	if r == nil || r.loopGoals == nil || job == nil || strings.TrimSpace(job.LoopGoalID) == "" {
		return nil
	}
	goal, err := r.loopGoals.GetLoopGoal(ctx, job.UserID, job.LoopGoalID)
	if err != nil {
		return nil
	}
	return goal
}

func (r *Runtime) updateLoopGoalFromDeepAgentResult(ctx context.Context, job *Job, goal *LoopGoal, result *DeepAgentTaskResult, runErr error) {
	if r == nil || r.loopGoals == nil || job == nil || goal == nil {
		return
	}
	status := LoopGoalStatusSucceeded
	if result != nil && result.State != nil && strings.TrimSpace(result.State.Status) != "" {
		status = loopGoalStatusFromDeepAgent(result.State.Status)
	}
	if runErr != nil && status == LoopGoalStatusSucceeded {
		status = LoopGoalStatusFailed
	}
	runID := ""
	if result != nil && result.Run != nil {
		runID = result.Run.ID
	}
	_ = r.loopGoals.UpdateLoopGoalRun(ctx, job.UserID, goal.ID, job.ID, runID, status, time.Now().UTC())
}

func loopGoalRubric(goal *LoopGoal) LoopRubric {
	if goal == nil {
		return LoopRubric{}
	}
	return goal.Rubric
}

func deepAgentRubricFromLoopRubric(rubric LoopRubric) DeepAgentRubric {
	return DeepAgentRubric{
		AcceptanceCriteria: append([]string(nil), rubric.AcceptanceCriteria...),
		RequiredEvidence:   append([]string(nil), rubric.RequiredEvidence...),
		RequiredArtifacts:  append([]string(nil), rubric.RequiredArtifacts...),
		ForbiddenActions:   append([]string(nil), rubric.ForbiddenActions...),
		QualityBar:         rubric.QualityBar,
	}
}

func (r *Runtime) appendDeepAgentResultMessage(ctx context.Context, userID, sessionID string, result *DeepAgentTaskResult, runErr error) error {
	if r == nil {
		return fmt.Errorf("runtime is not configured")
	}
	session, err := r.GetSession(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	startMessageCount := len(session.Messages)
	session.AddAssistantMessage(formatDeepAgentResultMessage(result, runErr))
	r.sanitizeSessionAttachmentBlocks(session)
	if err := r.persistChatSession(ctx, userID, session, startMessageCount); err != nil {
		return err
	}
	return r.afterTurnMemory(ctx, userID, session)
}

func formatDeepAgentResultMessage(result *DeepAgentTaskResult, runErr error) string {
	var b strings.Builder
	var state *DeepAgentState
	if result != nil {
		state = result.State
	}
	if runErr != nil {
		b.WriteString("计划执行暂时中止。")
		b.WriteString("\n\n原因：")
		if state != nil && strings.TrimSpace(state.Blocker) != "" {
			b.WriteString(strings.TrimSpace(state.Blocker))
		} else {
			b.WriteString(runErr.Error())
		}
	} else {
		b.WriteString("计划执行完成。")
	}
	if state != nil {
		if len(state.Plan.Steps) > 0 {
			b.WriteString("\n\n计划：")
			for i, step := range state.Plan.Steps {
				title := strings.TrimSpace(firstNonEmptyString(step.Title, step.ID))
				if title == "" {
					title = fmt.Sprintf("Step %d", i+1)
				}
				status := strings.TrimSpace(step.Status)
				if status == "" {
					status = DeepAgentStepStatusPending
				}
				b.WriteString(fmt.Sprintf("\n%d. %s（%s）", i+1, title, status))
			}
		}
		if len(state.CompletedSteps) > 0 {
			b.WriteString("\n\n已完成步骤：")
			for _, stepID := range state.CompletedSteps {
				if strings.TrimSpace(stepID) == "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(strings.TrimSpace(stepID))
			}
		}
		if len(state.FailedSteps) > 0 {
			b.WriteString("\n\n失败步骤：")
			for _, stepID := range state.FailedSteps {
				if strings.TrimSpace(stepID) == "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(strings.TrimSpace(stepID))
			}
		}
		if strings.TrimSpace(state.Blocker) != "" && runErr == nil {
			b.WriteString("\n\n阻塞原因：")
			b.WriteString(strings.TrimSpace(state.Blocker))
		}
		if refs := deepAgentStateCurrentArtifactRefs(state); len(refs) > 0 {
			b.WriteString("\n\nArtifacts：")
			for _, ref := range refs {
				label := firstNonEmptyString(ref.Filename, ref.ID)
				if strings.TrimSpace(label) == "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(strings.TrimSpace(label))
				if strings.TrimSpace(ref.ID) != "" && ref.ID != label {
					b.WriteString("（")
					b.WriteString(strings.TrimSpace(ref.ID))
					b.WriteString("）")
				}
			}
		}
		finalEvidence := deepAgentFinalAnswerEvidenceForSummary(state)
		if len(finalEvidence.Sources) > 0 {
			b.WriteString("\n\nSources：")
			for _, source := range finalEvidence.Sources {
				label := firstNonEmptyString(source.Title, source.URL, source.Snippet)
				if strings.TrimSpace(label) == "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(strings.TrimSpace(label))
				if strings.TrimSpace(source.URL) != "" && source.URL != label {
					b.WriteString("（")
					b.WriteString(strings.TrimSpace(source.URL))
					b.WriteString("）")
				}
			}
		}
		if len(finalEvidence.Tests) > 0 {
			b.WriteString("\n\nTest results：")
			for idx, test := range finalEvidence.Tests {
				label := deepAgentResultTestLabel(state, test, idx)
				status := deepAgentResultTestStatus(test)
				b.WriteString("\n- ")
				b.WriteString(label)
				if strings.TrimSpace(status) != "" {
					b.WriteString("：")
					b.WriteString(status)
				}
			}
		}
		if len(finalEvidence.KnownGaps) > 0 {
			b.WriteString("\n\nKnown gaps：")
			for _, gap := range finalEvidence.KnownGaps {
				if strings.TrimSpace(gap) == "" {
					continue
				}
				b.WriteString("\n- ")
				b.WriteString(strings.TrimSpace(gap))
			}
		}
		b.WriteString(fmt.Sprintf("\n\n动作次数：%d", state.ActionCount))
	}
	if result != nil && result.Run != nil && strings.TrimSpace(result.Run.ID) != "" {
		b.WriteString("\n\nWorkflow Run：")
		b.WriteString(result.Run.ID)
	}
	return b.String()
}

func deepAgentResultTestLabel(state *DeepAgentState, test map[string]any, idx int) string {
	if command := strings.TrimSpace(deepAgentWorkflowString(test, "command")); command != "" {
		return command
	}
	stepID := strings.TrimSpace(deepAgentWorkflowString(test, "step_id"))
	if state != nil && idx >= 0 && idx < len(state.ActionHistory) {
		action := state.ActionHistory[idx]
		actionStepID := strings.TrimSpace(action.StepID)
		if stepID == "" || actionStepID == "" || stepID == actionStepID {
			parts := []string{fmt.Sprintf("action-%d", idx+1)}
			if actionStepID != "" {
				parts = append(parts, actionStepID)
			} else if stepID != "" {
				parts = append(parts, stepID)
			}
			if tool := strings.TrimSpace(action.Tool); tool != "" {
				parts = append(parts, tool)
			}
			return strings.Join(parts, " · ")
		}
	}
	return firstNonEmptyString(stepID, "test")
}

func deepAgentResultTestStatus(test map[string]any) string {
	status := strings.TrimSpace(deepAgentWorkflowString(test, "status"))
	if status != "" {
		return status
	}
	if test != nil && test["exit_code"] != nil {
		return fmt.Sprint(test["exit_code"])
	}
	return ""
}

func (r *Runtime) GetJob(ctx context.Context, userID, jobID string) (*Job, error) {
	if r.jobs == nil {
		return nil, fmt.Errorf("job store is not configured")
	}
	return r.jobs.GetJob(ctx, userID, jobID)
}

func (r *Runtime) ListJobs(ctx context.Context, userID, sessionID string) ([]*Job, error) {
	if r.jobs == nil {
		return []*Job{}, nil
	}
	return r.jobs.ListJobs(ctx, userID, sessionID)
}

func (r *Runtime) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	if r != nil && r.messageSearch != nil {
		return r.messageSearch.SearchMessages(ctx, userID, query, limit, offset)
	}
	if r == nil {
		return []MessageSearchResult{}, nil
	}
	store, ok := r.sessions.(MessageSearchStore)
	if !ok || store == nil {
		return []MessageSearchResult{}, nil
	}
	return store.SearchMessages(ctx, userID, query, limit, offset)
}

func (r *Runtime) ListSkillExecutions(ctx context.Context, filter SkillExecutionFilter) ([]SkillExecutionRecord, error) {
	if r == nil || r.skillExecutions == nil {
		return []SkillExecutionRecord{}, nil
	}
	return r.skillExecutions.ListSkillExecutions(ctx, filter)
}

func (r *Runtime) SummarizeSkillExecutions(ctx context.Context, filter SkillExecutionFilter) (SkillExecutionSummary, error) {
	if r == nil || r.skillExecutions == nil {
		return SkillExecutionSummary{SkillName: strings.TrimSpace(filter.SkillName)}, nil
	}
	return r.skillExecutions.SummarizeSkillExecutions(ctx, filter)
}

func (r *Runtime) ListWorkflowRuns(ctx context.Context, filter WorkflowRunFilter) ([]*WorkflowRun, error) {
	if r == nil || r.workflowStore == nil {
		return []*WorkflowRun{}, nil
	}
	return r.workflowStore.ListWorkflowRuns(ctx, filter)
}

func (r *Runtime) GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	if r == nil || r.workflowStore == nil {
		return nil, fmt.Errorf("workflow store is not configured")
	}
	return r.workflowStore.GetWorkflowRun(ctx, runID)
}

func (r *Runtime) ListWorkflowSteps(ctx context.Context, runID string) ([]*WorkflowStepRun, error) {
	if r == nil || r.workflowStore == nil {
		return []*WorkflowStepRun{}, nil
	}
	return r.workflowStore.ListWorkflowStepRuns(ctx, runID)
}

func (r *Runtime) ListToolCalls(ctx context.Context, filter ToolCallLedgerFilter) ([]engine.ToolLedgerEntry, error) {
	if r == nil || r.toolCallLedger == nil {
		return []engine.ToolLedgerEntry{}, nil
	}
	return r.toolCallLedger.ListToolCalls(ctx, filter)
}

type WorkflowRecoveryResult struct {
	RunID  string `json:"run_id"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (r *Runtime) ResumeWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	return r.ResumeWorkflowRunWithRequest(ctx, DeepAgentResumeRequest{RunID: runID})
}

func (r *Runtime) ResumeWorkflowRunWithRequest(ctx context.Context, req DeepAgentResumeRequest) (*WorkflowRun, error) {
	if r == nil || r.workflowStore == nil {
		return nil, fmt.Errorf("workflow store is not configured")
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		return nil, fmt.Errorf("workflow run id is required")
	}
	run, err := r.workflowStore.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	switch run.Name {
	case deepAgentTaskWorkflowName:
		req.RunID = run.ID
		result, err := r.ResumeDeepAgentTask(ctx, req, nil, nil, nil)
		if result != nil && result.Run != nil {
			return result.Run, err
		}
		return run, err
	default:
		return run, fmt.Errorf("workflow resume is unsupported for %s", run.Name)
	}
}

func (r *Runtime) CancelWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	if r == nil || r.workflowStore == nil {
		return nil, fmt.Errorf("workflow store is not configured")
	}
	run, err := r.workflowStore.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == WorkflowStatusSucceeded || run.Status == WorkflowStatusCancelled {
		return run, nil
	}
	now := time.Now().UTC()
	run.Status = WorkflowStatusCancelled
	run.Error = "cancelled by admin"
	run.UpdatedAt = now
	run.FinishedAt = &now
	if err := r.workflowStore.UpdateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (r *Runtime) RecoverStaleWorkflowRuns(ctx context.Context, userID string, limit int) ([]WorkflowRecoveryResult, error) {
	if r == nil || r.workflowStore == nil {
		return []WorkflowRecoveryResult{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	candidates := []*WorkflowRun{}
	for _, status := range []string{WorkflowStatusRunning, WorkflowStatusFailed} {
		runs, err := r.workflowStore.ListWorkflowRuns(ctx, WorkflowRunFilter{UserID: userID, Status: status, Limit: limit})
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run != nil && run.Recoverable {
				candidates = append(candidates, run)
			}
			if len(candidates) >= limit {
				break
			}
		}
		if len(candidates) >= limit {
			break
		}
	}
	results := make([]WorkflowRecoveryResult, 0, len(candidates))
	for _, run := range candidates {
		result := WorkflowRecoveryResult{RunID: run.ID, Name: run.Name, Status: "skipped"}
		resumed, err := r.ResumeWorkflowRun(ctx, run.ID)
		if err != nil {
			result.Error = err.Error()
		} else if resumed != nil {
			result.Status = resumed.Status
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *Runtime) ExecuteDeepAgentTask(ctx context.Context, req DeepAgentTaskRequest, planner DeepAgentPlanner, executor DeepAgentExecutor, verifier DeepAgentVerifier) (*DeepAgentTaskResult, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	store := r.workflowStore
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	if planner == nil {
		planner = NewRuntimeDeepAgentPlanner(r)
	}
	if executor == nil {
		executor = NewRuntimeDeepAgentExecutor(r)
	}
	controller := NewDeepAgentController(store, ContextWorkflowEventSink{}, planner, executor, verifier)
	controller.SetContextLoader(r)
	controller.SetEvidenceRepository(r.deepAgentEvidence)
	controller.SetRiskGate(NewRuntimeDeepAgentRiskGate(r))
	controller.SetLearningSink(NewRuntimeDeepAgentLearningSink(r))
	return controller.Execute(ctx, req)
}

func (r *Runtime) ResumeDeepAgentTask(ctx context.Context, req DeepAgentResumeRequest, planner DeepAgentPlanner, executor DeepAgentExecutor, verifier DeepAgentVerifier) (*DeepAgentTaskResult, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	store := r.workflowStore
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	if planner == nil {
		planner = NewRuntimeDeepAgentPlanner(r)
	}
	if executor == nil {
		executor = NewRuntimeDeepAgentExecutor(r)
	}
	controller := NewDeepAgentController(store, ContextWorkflowEventSink{}, planner, executor, verifier)
	controller.SetContextLoader(r)
	controller.SetEvidenceRepository(r.deepAgentEvidence)
	controller.SetRiskGate(NewRuntimeDeepAgentRiskGate(r))
	controller.SetLearningSink(NewRuntimeDeepAgentLearningSink(r))
	return controller.Resume(ctx, req)
}

func (r *Runtime) ListJobEvents(ctx context.Context, userID, jobID, afterID string, limit int) ([]*JobEvent, error) {
	if r.jobs == nil {
		return []*JobEvent{}, nil
	}
	return r.jobs.ListJobEvents(ctx, userID, jobID, afterID, limit)
}

func (r *Runtime) CancelJob(ctx context.Context, userID, jobID string) error {
	if r.jobs == nil {
		return fmt.Errorf("job store is not configured")
	}
	job, err := r.jobs.GetJob(ctx, userID, jobID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	cancel, ok := r.runningJobs[jobID]
	r.mu.Unlock()
	if ok {
		cancel()
		return nil
	}
	if isTerminalJobStatus(job.Status) {
		return nil
	}
	now := time.Now().UTC()
	if err := r.jobs.UpdateJobStatus(ctx, userID, jobID, JobStatusCancelled, "cancelled before execution", now); err != nil {
		return err
	}
	return (&jobEventSink{store: r.jobs, bus: r.jobEvents, fanout: r.jobEventFanout, job: job, logger: componentLogger(r.logger, "job_event_fanout")}).Send(ctx, Event{Type: "cancelled", SessionID: job.SessionID, JobID: job.ID})
}

func (r *Runtime) Cancel(userID, sessionID string) bool {
	key := sessionKey(userID, sessionID)
	r.mu.Lock()
	cancel, ok := r.running[key]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.shuttingDown = true
	cancels := make([]context.CancelFunc, 0, len(r.running)+len(r.runningJobs))
	for key, cancel := range r.running {
		if r.jobQueue != nil && r.runningJobTurns[key] {
			continue
		}
		cancels = append(cancels, cancel)
	}
	if r.jobQueue == nil {
		for _, cancel := range r.runningJobs {
			cancels = append(cancels, cancel)
		}
	}
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if r.vectorIndexer != nil {
		_ = r.vectorIndexer.Close(ctx)
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) ListSkills() []*skills.SkillDefinition {
	if r.skills == nil {
		return nil
	}
	return r.skills.ListUserInvocableSkills()
}

func (r *Runtime) RouteChat(req ChatRequest) JobRoutingDecision {
	content := strings.TrimSpace(req.Content)
	if content == "" || r.jobs == nil {
		return JobRoutingDecision{}
	}
	if strings.EqualFold(strings.TrimSpace(req.AgentMode), AgentModePlanExecute) {
		return JobRoutingDecision{RunAsJob: true, JobType: JobTypeDeepAgent, Reason: "user selected plan-and-execute mode"}
	}
	if strings.HasPrefix(content, "/") {
		skill, ok := r.skillForPrompt(content)
		if !ok {
			return JobRoutingDecision{}
		}
		if skill.RunAsJob || skill.ExecutionContext == skills.ContextFork {
			return JobRoutingDecision{RunAsJob: true, JobType: JobTypeSkill, Reason: "skill metadata requests durable job execution"}
		}
		return JobRoutingDecision{}
	}
	return JobRoutingDecision{}
}

func (r *Runtime) skillForPrompt(content string) (*skills.SkillDefinition, bool) {
	content = strings.TrimSpace(content)
	if content == "" || r.skills == nil {
		return nil, false
	}
	if strings.HasPrefix(content, "/") {
		parts := strings.SplitN(content, " ", 2)
		name := strings.TrimPrefix(parts[0], "/")
		if name == "" || name == "skills" {
			return nil, false
		}
		skill, ok := r.skills.GetSkill(name)
		if !ok || !skill.UserInvocable {
			return nil, false
		}
		return skill, true
	}
	return r.skills.MatchUserInvocableSkill(content)
}

func (r *Runtime) injectMemory(ctx context.Context, userID string, session *state.Session) error {
	if r.memory == nil || session == nil {
		return nil
	}
	personalization, err := r.GetPersonalizationSettings(ctx, userID)
	if err != nil {
		return err
	}
	if !personalization.FeatureFlags.UseSavedMemory {
		return nil
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return err
	}
	if !settings.ContextEnabled {
		return nil
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if session.Metadata[memoryInjectedKey] == "true" {
		return nil
	}
	content, err := r.memory.LoadContext(ctx, userID, session)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	session.AddSystemContext("<memory>\n" + content + "\n</memory>")
	session.Metadata[memoryInjectedKey] = "true"
	return nil
}

func (r *Runtime) injectTurnMemoryContexts(ctx context.Context, userID string, session *state.Session, query string) error {
	if r == nil || session == nil {
		return nil
	}
	personalization, err := r.GetPersonalizationSettings(ctx, userID)
	if err != nil {
		return err
	}
	if !personalization.FeatureFlags.UseSavedMemory {
		return nil
	}
	settings, err := r.GetMemorySettings(ctx, userID)
	if err != nil {
		return err
	}
	if !settings.ContextEnabled {
		return nil
	}
	recall := r.memoryRecall
	if recall == nil {
		recall = NewMemoryRecallDecider(r.config.MemoryRecall, nil, componentLogger(r.logger, "memory_recall"), r.engineFactory)
	}
	decision := recall.Decide(ctx, MemoryRecallInput{
		UserID:          userID,
		Session:         session,
		Message:         query,
		Personalization: personalization,
	})
	if !decision.Should {
		return nil
	}
	result, err := r.loadTurnMemoryContexts(ctx, userID, session, firstNonEmptyString(decision.Query, query), recall.config)
	if err != nil {
		return err
	}
	if strings.TrimSpace(result.MemoryContent) != "" {
		session.AddSystemContext(memoryContextMarker + "\n" + result.MemoryContent + "\n</memory>")
	}
	if strings.TrimSpace(result.EpisodeContent) != "" {
		session.AddSystemContext(episodicMemoryContextMarker + "\n" + result.EpisodeContent + "\n</episodic-memory>")
		r.recordEpisodeRecall(ctx, userID, result.EpisodeResults)
	}
	return nil
}

type turnMemoryContextResult struct {
	MemoryContent  string
	EpisodeContent string
	EpisodeResults []MemoryEpisodeSearchResult
}

func (r *Runtime) loadTurnMemoryContexts(ctx context.Context, userID string, session *state.Session, query string, config MemoryRecallConfig) (turnMemoryContextResult, error) {
	config = normalizeMemoryRecallConfig(config)
	if !config.AsyncEnabled {
		return r.loadTurnMemoryContextsSync(ctx, userID, session, query, config)
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultMemoryRecallTimeout
	}
	recallCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct {
		result turnMemoryContextResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := r.loadTurnMemoryContextsSync(recallCtx, userID, session, query, config)
		done <- outcome{result: result, err: err}
	}()
	select {
	case got := <-done:
		return got.result, got.err
	case <-recallCtx.Done():
		if errors.Is(recallCtx.Err(), context.DeadlineExceeded) {
			if r.logger != nil {
				r.logger.LogAttrs(ctx, slog.LevelDebug, "memory recall timed out", contextLogAttrs(ctx, userID, session.ID, "")...)
			}
			return turnMemoryContextResult{}, nil
		}
		return turnMemoryContextResult{}, recallCtx.Err()
	}
}

func (r *Runtime) loadTurnMemoryContextsSync(ctx context.Context, userID string, session *state.Session, query string, config MemoryRecallConfig) (turnMemoryContextResult, error) {
	recallQuery := strings.TrimSpace(query)
	querySession := runtimeContextQuerySession(session, recallQuery)
	var result turnMemoryContextResult
	if r.memory != nil {
		content, err := r.memory.LoadContext(ctx, userID, querySession)
		if err != nil {
			return result, err
		}
		result.MemoryContent = content
	}
	episodeContent, episodeResults, err := r.searchTurnEpisodeContexts(ctx, userID, recallQuery)
	if err != nil {
		return result, err
	}
	result.EpisodeContent = episodeContent
	result.EpisodeResults = episodeResults
	return result, nil
}

func runtimeContextQuerySession(session *state.Session, query string) *state.Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.Messages = append([]state.Message(nil), session.Messages...)
	if session.Metadata != nil {
		clone.Metadata = make(map[string]string, len(session.Metadata))
		for key, value := range session.Metadata {
			clone.Metadata[key] = value
		}
	}
	if strings.TrimSpace(query) != "" {
		clone.AddUserMessage(query)
	}
	return &clone
}

func (r *Runtime) injectPersonalization(ctx context.Context, userID string, session *state.Session) error {
	if session == nil {
		return nil
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if session.Metadata[personalizationInjectedKey] == "true" {
		return nil
	}
	settings, err := r.GetPersonalizationSettings(ctx, userID)
	if err != nil {
		return err
	}
	content := formatPersonalizationContext(settings)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	session.AddSystemContext("<personalization>\n" + content + "\n</personalization>")
	session.Metadata[personalizationInjectedKey] = "true"
	return nil
}

func (r *Runtime) run(ctx context.Context, req ChatRequest, session *state.Session, onToken func(string)) (runnerResult, error) {
	userID := req.UserID
	content := req.Content
	if strings.TrimSpace(content) == "" {
		content = "Please analyze the attached file(s)."
	}
	ctx = WithLLMScope(ctx, LLMScope{
		UserID:    userID,
		SessionID: session.ID,
		JobID:     jobIDFromContext(ctx),
		RequestID: requestIDFromContext(ctx),
	})
	ensureConsumerSecurityContext(session)
	if strings.HasPrefix(strings.TrimSpace(content), "/") {
		return r.runSkillCommand(ctx, req, userID, session, content, onToken)
	}
	if req.ThinkingMode {
		ctx = providerbackend.WithThinkingConfig(ctx, &providerbackend.ThinkingConfig{
			Enabled:      true,
			BudgetTokens: -1,
			Level:        "HIGH",
		})
	}
	llmContent := content
	webSearchMode := strings.EqualFold(strings.TrimSpace(req.AgentMode), AgentModeWebSearch)
	if webSearchMode {
		llmContent = webSearchPrompt(content)
	}
	prompt, err := r.chatPrompt(ctx, req, llmContent)
	if err != nil {
		return runnerResult{}, err
	}
	visiblePrompt, err := r.visibleChatPrompt(ctx, req, content)
	if err != nil {
		return runnerResult{}, err
	}
	llmSession, err := r.materializedSessionForLLM(ctx, userID, session)
	if err != nil {
		return runnerResult{}, err
	}
	if err := r.injectTurnMemoryContexts(ctx, userID, llmSession, content); err != nil {
		return runnerResult{}, err
	}
	r.injectTransientRuntimeContexts(llmSession)
	llmPrompt, err := r.materializeContentBlocks(ctx, userID, prompt)
	if err != nil {
		return runnerResult{}, err
	}
	runner := r.runnerForScope(Scope{
		UserID:           userID,
		SessionID:        session.ID,
		WorkingDir:       session.WorkingDir,
		Prompt:           content,
		ConnectorContext: req.ConnectorContext,
	})
	startedAt := time.Now().UTC()
	startMessageCount := len(llmSession.Messages)
	result, err := runWithTokenStreamContent(ctx, runner, llmSession, llmPrompt, false, onToken)
	if result.Session != nil && (webSearchMode || len(normalizeConnectorScopes(req.ConnectorContext)) > 0) {
		normalizeInternalUserPromptTurnMessages(result.Session, startMessageCount, content, promptContentText(prompt), visiblePrompt)
	}
	if errors.Is(err, skilltool.ErrRunAsJobRequired) {
		selection, ok := selectedRunAsJobSkill(result.Session, startMessageCount)
		if !ok {
			stripTransientRuntimeContexts(result.Session)
			return runnerResult{Output: result.Output, Session: result.Session}, err
		}
		job, jobErr := r.createSelectedSkillJob(ctx, req, session.ID, selection)
		if jobErr != nil {
			stripTransientRuntimeContexts(result.Session)
			return runnerResult{Output: result.Output, Session: result.Session}, jobErr
		}
		stripTransientRuntimeContexts(result.Session)
		return runnerResult{
			Session:   result.Session,
			Job:       job,
			JobReason: "skill metadata requests durable job execution",
		}, nil
	}
	r.recordInlineSkillExecutions(ctx, userID, result.Session, startMessageCount, startedAt, err)
	stripTransientRuntimeContexts(result.Session)
	return runnerResult{Output: result.Output, Session: result.Session}, err
}

func ensureConsumerSecurityContext(session *state.Session) {
	if session == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if session.Metadata[consumerSecurityInjectedKey] == "true" {
		return
	}
	session.AddSystemContext(consumerSecuritySystemContext)
	session.Metadata[consumerSecurityInjectedKey] = "true"
}

const vertexInlineAttachmentLimitBytes = 20 << 20
const textAttachmentPromptLimitBytes = 1 << 20
const signedAttachmentURLTTL = 15 * time.Minute

func (r *Runtime) chatPrompt(ctx context.Context, req ChatRequest, content string) ([]publictypes.ContentBlock, error) {
	blocks := []publictypes.ContentBlock{{Type: "text", Text: content}}
	if connectorPrompt := r.connectorContextPrompt(ctx, req); connectorPrompt != "" {
		blocks = append(blocks, publictypes.ContentBlock{Type: "text", Text: connectorPrompt})
	}
	attachmentNames := make([]string, 0, len(req.AttachmentIDs)+len(req.AttachmentURLs))
	for _, id := range req.AttachmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		artifact, err := r.GetAttachmentMetadata(ctx, req.UserID, id)
		if err != nil {
			return nil, fmt.Errorf("load attachment %s: %w", id, err)
		}
		if artifact.SessionID != "" && artifact.SessionID != req.SessionID {
			return nil, fmt.Errorf("attachment %s does not belong to this session", id)
		}
		attachmentNames = append(attachmentNames, artifact.Filename)
		if isTextAttachment(artifact.Filename, artifact.ContentType) {
			_, data, err := r.GetAttachment(ctx, req.UserID, id)
			if err != nil {
				return nil, fmt.Errorf("load attachment %s: %w", id, err)
			}
			if len(data) > textAttachmentPromptLimitBytes {
				return nil, fmt.Errorf("text attachment %s exceeds prompt inline limit of %d bytes", artifact.Filename, textAttachmentPromptLimitBytes)
			}
			blocks = append(blocks, publictypes.ContentBlock{
				Type: "text",
				Text: formatTextAttachmentPrompt(artifact.Filename, artifact.ContentType, data),
			})
			continue
		}
		blocks = append(blocks, attachmentReferenceBlock(artifact))
	}
	for _, item := range req.AttachmentURLs {
		fileURL := strings.TrimSpace(item.URL)
		if fileURL == "" {
			continue
		}
		parsedURL, err := url.Parse(fileURL)
		if err != nil {
			return nil, fmt.Errorf("invalid attachment URL %q: %w", fileURL, err)
		}
		if parsedURL.Scheme == "" {
			return nil, fmt.Errorf("invalid attachment URL %q: scheme is required", fileURL)
		}
		contentType := strings.TrimSpace(item.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(parsedURL.Path)))
		}
		if contentType == "" {
			return nil, fmt.Errorf("content_type is required for attachment URL %q", fileURL)
		}
		attachmentNames = append(attachmentNames, firstNonEmptyString(item.Filename, filepath.Base(parsedURL.Path), fileURL))
		blocks = append(blocks, publictypes.ContentBlock{
			Type: attachmentBlockType(contentType),
			Source: map[string]interface{}{
				"type":       "url",
				"media_type": normalizedContentType(contentType),
				"file_uri":   fileURL,
			},
		})
	}
	if len(attachmentNames) > 0 {
		blocks[0].Text = content + "\n\nAttached files: " + strings.Join(attachmentNames, ", ")
	}
	return blocks, nil
}

func (r *Runtime) visibleChatPrompt(ctx context.Context, req ChatRequest, content string) ([]publictypes.ContentBlock, error) {
	visibleReq := req
	visibleReq.ConnectorContext = nil
	return r.chatPrompt(ctx, visibleReq, content)
}

func attachmentReferenceBlock(artifact *Artifact) publictypes.ContentBlock {
	source := map[string]interface{}{
		"type":          "attachment_ref",
		"attachment_id": artifact.ID,
		"media_type":    normalizedContentType(artifact.ContentType),
		"filename":      artifact.Filename,
	}
	return publictypes.ContentBlock{
		Type:   attachmentBlockType(artifact.ContentType),
		Source: source,
	}
}

func (r *Runtime) materializedSessionForLLM(ctx context.Context, userID string, session *state.Session) (*state.Session, error) {
	if session == nil {
		return nil, nil
	}
	clone := *session
	clone.Messages = append([]state.Message(nil), session.Messages...)
	if session.Tags != nil {
		clone.Tags = append([]string(nil), session.Tags...)
	}
	if session.Metadata != nil {
		clone.Metadata = make(map[string]string, len(session.Metadata))
		for key, value := range session.Metadata {
			clone.Metadata[key] = value
		}
	}
	for i := range clone.Messages {
		if len(clone.Messages[i].ContentBlocks) == 0 {
			continue
		}
		blocks, err := r.materializeContentBlocks(ctx, userID, clone.Messages[i].ContentBlocks)
		if err != nil {
			return nil, err
		}
		clone.Messages[i].ContentBlocks = blocks
	}
	if personalization, err := r.GetPersonalizationSettings(ctx, userID); err == nil && !personalization.FeatureFlags.UseChatHistory {
		clone.Messages = personalizationContextMessagesOnly(clone.Messages)
	}
	return &clone, nil
}

func personalizationContextMessagesOnly(messages []state.Message) []state.Message {
	out := make([]state.Message, 0, len(messages))
	for _, message := range messages {
		if !message.Hidden {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if strings.Contains(content, "<consumer-security>") ||
			strings.Contains(content, "<personalization>") ||
			strings.Contains(content, "<browser-memory>") ||
			strings.Contains(content, "<memory>") {
			out = append(out, message)
		}
	}
	return out
}

func (r *Runtime) materializeContentBlocks(ctx context.Context, userID string, blocks []publictypes.ContentBlock) ([]publictypes.ContentBlock, error) {
	if len(blocks) == 0 {
		return blocks, nil
	}
	out := make([]publictypes.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if sourceString(block.Source, "type") != "attachment_ref" {
			out = append(out, block)
			continue
		}
		id := sourceString(block.Source, "attachment_id", "id")
		if id == "" {
			return nil, fmt.Errorf("attachment reference is missing attachment_id")
		}
		artifact, err := r.GetAttachmentMetadata(ctx, userID, id)
		if err != nil {
			return nil, fmt.Errorf("load attachment %s: %w", id, err)
		}
		materialized, err := r.materializedAttachmentBlock(ctx, userID, artifact)
		if err != nil {
			return nil, err
		}
		out = append(out, materialized)
	}
	return out, nil
}

func (r *Runtime) materializedAttachmentBlock(ctx context.Context, userID string, artifact *Artifact) (publictypes.ContentBlock, error) {
	if block, ok, err := r.presignedAttachmentBlock(ctx, artifact); ok && err == nil {
		return block, nil
	} else if ok && err != nil && artifact.SizeBytes > vertexInlineAttachmentLimitBytes {
		return publictypes.ContentBlock{}, fmt.Errorf("presign attachment %s: %w", artifact.Filename, err)
	}
	_, data, err := r.GetAttachment(ctx, userID, artifact.ID)
	if err != nil {
		return publictypes.ContentBlock{}, fmt.Errorf("load attachment %s: %w", artifact.ID, err)
	}
	if int64(len(data)) > vertexInlineAttachmentLimitBytes {
		return publictypes.ContentBlock{}, fmt.Errorf("attachment %s exceeds Vertex inlineData limit of %d bytes", artifact.Filename, vertexInlineAttachmentLimitBytes)
	}
	return publictypes.ContentBlock{
		Type: attachmentBlockType(artifact.ContentType),
		Source: map[string]interface{}{
			"type":          "base64",
			"attachment_id": artifact.ID,
			"media_type":    normalizedContentType(artifact.ContentType),
			"filename":      artifact.Filename,
			"data":          base64.StdEncoding.EncodeToString(data),
		},
	}, nil
}

func (r *Runtime) sanitizeSessionAttachmentBlocks(session *state.Session) {
	if session == nil {
		return
	}
	for i := range session.Messages {
		if len(session.Messages[i].ContentParts) > 0 {
			session.Messages[i].ContentParts = sanitizeAttachmentContentBlocks(session.Messages[i].ContentParts)
			session.Messages[i].ContentBlocks = session.Messages[i].ContentParts
			continue
		}
		if len(session.Messages[i].ContentBlocks) == 0 {
			continue
		}
		session.Messages[i].ContentBlocks = sanitizeAttachmentContentBlocks(session.Messages[i].ContentBlocks)
		session.Messages[i].ContentParts = session.Messages[i].ContentBlocks
	}
}

func sanitizeAttachmentContentBlocks(blocks []publictypes.ContentBlock) []publictypes.ContentBlock {
	out := make([]publictypes.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if id := sourceString(block.Source, "attachment_id", "id"); id != "" {
			mediaType := sourceString(block.Source, "media_type", "mime_type", "mimeType")
			filename := sourceString(block.Source, "filename", "name")
			source := map[string]interface{}{
				"type":          "attachment_ref",
				"attachment_id": id,
			}
			if mediaType != "" {
				source["media_type"] = normalizedContentType(mediaType)
			}
			if filename != "" {
				source["filename"] = filename
			}
			block.Source = source
		} else if sourceString(block.Source, "data", "base64", "file_uri", "fileUri", "url") != "" {
			block.Source = nil
		}
		out = append(out, block)
	}
	return out
}

func sourceString(source map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if source == nil {
			return ""
		}
		if value, ok := source[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *Runtime) presignedAttachmentBlock(ctx context.Context, artifact *Artifact) (publictypes.ContentBlock, bool, error) {
	if r == nil || r.artifacts == nil || artifact == nil {
		return publictypes.ContentBlock{}, false, nil
	}
	if isImageContentType(artifact.ContentType) {
		return publictypes.ContentBlock{}, false, nil
	}
	fileURL, ok, err := r.artifacts.PresignGet(ctx, artifact.ObjectKey, signedAttachmentURLTTL)
	if !ok || err != nil {
		return publictypes.ContentBlock{}, ok, err
	}
	return publictypes.ContentBlock{
		Type: attachmentBlockType(artifact.ContentType),
		Source: map[string]interface{}{
			"type":          "url",
			"attachment_id": artifact.ID,
			"media_type":    normalizedContentType(artifact.ContentType),
			"filename":      artifact.Filename,
			"file_uri":      fileURL,
		},
	}, true, nil
}

func formatTextAttachmentPrompt(filename, contentType string, data []byte) string {
	if strings.TrimSpace(contentType) == "" {
		contentType = "text/plain"
	}
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	return fmt.Sprintf("\n\nAttached text file: %s\nContent-Type: %s\n\n```text\n%s\n```", filename, contentType, text)
}

func isTextAttachment(filename, contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if strings.HasPrefix(contentType, "text/") {
		return true
	}
	switch contentType {
	case "application/json", "application/ld+json", "application/xml", "application/yaml", "application/x-yaml", "application/toml":
		return true
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	switch ext {
	case "txt", "md", "markdown", "csv", "tsv", "json", "jsonl", "log", "yaml", "yml", "xml", "html", "htm", "css", "js", "jsx", "ts", "tsx", "go", "py", "java", "c", "cpp", "h", "sh", "sql", "toml", "ini", "env":
		return true
	default:
		return false
	}
}

func attachmentBlockType(contentType string) string {
	if isImageContentType(contentType) {
		return "image"
	}
	return "file"
}

func isImageContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/")
}

func (r *Runtime) runSkillCommand(ctx context.Context, req ChatRequest, userID string, session *state.Session, content string, onToken func(string)) (runnerResult, error) {
	parts := strings.SplitN(strings.TrimSpace(content), " ", 2)
	name := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	if name == "skills" {
		return runnerResult{Output: formatSkillList(r.ListSkills()), Session: session}, nil
	}
	if r.skills == nil {
		return runnerResult{}, fmt.Errorf("skills are not configured")
	}
	skill, ok := r.skills.GetSkill(name)
	if !ok {
		return runnerResult{}, fmt.Errorf("unknown skill: /%s", name)
	}
	if !skill.UserInvocable {
		return runnerResult{}, fmt.Errorf("skill /%s is not user-invocable", name)
	}
	if len(req.AttachmentIDs) > 0 {
		attachmentContext, err := r.textAttachmentContext(ctx, req)
		if err != nil {
			return runnerResult{}, err
		}
		if attachmentContext != "" {
			args = strings.TrimSpace(args + "\n\n" + attachmentContext)
		}
	}
	if hideUserMessageFromContext(ctx) {
		session.AddHiddenUserMessage(content)
	} else {
		session.AddUserMessage(content)
	}
	return r.runSkill(ctx, userID, session, skill, args, onToken)
}

func (r *Runtime) textAttachmentContext(ctx context.Context, req ChatRequest) (string, error) {
	var parts []string
	for _, id := range req.AttachmentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		artifact, data, err := r.GetAttachment(ctx, req.UserID, id)
		if err != nil {
			return "", fmt.Errorf("load attachment %s: %w", id, err)
		}
		if artifact.SessionID != "" && artifact.SessionID != req.SessionID {
			return "", fmt.Errorf("attachment %s does not belong to this session", id)
		}
		if !isTextAttachment(artifact.Filename, artifact.ContentType) {
			continue
		}
		if len(data) > textAttachmentPromptLimitBytes {
			return "", fmt.Errorf("text attachment %s exceeds prompt inline limit of %d bytes", artifact.Filename, textAttachmentPromptLimitBytes)
		}
		parts = append(parts, formatTextAttachmentPrompt(artifact.Filename, artifact.ContentType, data))
	}
	return strings.Join(parts, "\n"), nil
}

func (r *Runtime) runSkill(ctx context.Context, userID string, session *state.Session, skill *skills.SkillDefinition, args string, onToken func(string)) (runnerResult, error) {
	return r.executeSkillWorkflow(ctx, userID, session, skill, args, onToken)
}

func (r *Runtime) runSkillDirect(ctx context.Context, userID string, session *state.Session, skill *skills.SkillDefinition, args string, onToken func(string)) (runnerResult, error) {
	startedAt := time.Now().UTC()
	status := SkillExecutionStatusFailed
	errText := ""
	var policy SkillRuntimePolicy
	inputSummary := summarizeSkillInput(args)
	execDiagnostics := skillExecutionDiagnostics{}
	defer func() {
		if r == nil || r.skillExecutions == nil || skill == nil {
			return
		}
		completedAt := time.Now().UTC()
		record := SkillExecutionRecord{
			SkillName:      skill.Name,
			UserID:         userID,
			SessionID:      session.ID,
			JobID:          jobIDFromContext(ctx),
			RequestID:      requestIDFromContext(ctx),
			Status:         status,
			Error:          errText,
			ErrorKind:      execDiagnostics.ErrorKind,
			Provider:       execDiagnostics.Provider,
			Model:          execDiagnostics.Model,
			InputSummary:   inputSummary,
			ArtifactCount:  execDiagnostics.ArtifactCount,
			DurationMS:     completedAt.Sub(startedAt).Milliseconds(),
			DiagnosticJSON: execDiagnostics.JSON,
			StartedAt:      startedAt,
			CompletedAt:    completedAt,
			Metadata: map[string]any{
				"args_length":       len(args),
				"allowed_tools":     policy.AllowedTools,
				"allowed_env":       policy.AllowedEnv,
				"network_allowlist": policy.NetworkAllowlist,
				"artifact_types":    policy.ArtifactTypes,
				"execution_context": string(skill.ExecutionContext),
				"run_as_job":        skill.RunAsJob,
			},
		}
		if err := r.skillExecutions.RecordSkillExecution(context.Background(), record); err != nil {
			logError(ctx, r.logger, "record skill execution failed", err,
				slog.String("request_id", record.RequestID),
				slog.String("user_id", userID),
				slog.String("session_id", session.ID),
				slog.String("job_id", record.JobID),
				slog.String("skill", skill.Name),
			)
		}
	}()
	ctx = WithLLMScope(ctx, LLMScope{
		UserID:    userID,
		SessionID: session.ID,
		SkillName: skill.Name,
		JobID:     jobIDFromContext(ctx),
		RequestID: requestIDFromContext(ctx),
	})
	workspace := r.sandboxedWorkingDir(userID, session.WorkingDir)
	if workspace == "" {
		workspace = filepath.Clean(r.config.DefaultWorkingDir)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		errText = err.Error()
		return runnerResult{}, err
	}
	startMessageCount := len(session.Messages)
	generatedArtifactsBefore := snapshotGeneratedSkillArtifactFiles(workspace)
	skillDir := workspace
	if strings.TrimSpace(skill.SkillRoot) != "" {
		skillDir = skill.SkillRoot
	}
	policy = r.skillRuntimePolicy(skill)
	shellTimeout := r.config.SkillShellTimeout
	if policy.ShellTimeout > 0 {
		shellTimeout = policy.ShellTimeout
	}
	blocks, err := skill.GetPrompt(args, &skills.SkillContext{
		SessionID:    session.ID,
		WorkingDir:   skillDir,
		Environment:  r.skillShellEnvironment(workspace, policy.AllowedEnv),
		ShellRuntime: r.skillShellRuntime(workspace, skillDir, skill, policy),
		ShellTimeout: shellTimeout,
	})
	if err != nil {
		errText = err.Error()
		r.recordExecutionDenialRisk(ctx, userID, session.ID, skill.Name, err, map[string]any{
			"phase":             "skill_prompt",
			"allowed_tools":     policy.AllowedTools,
			"allowed_env":       policy.AllowedEnv,
			"network_allowlist": policy.NetworkAllowlist,
			"sandbox_network":   policy.Sandbox.Network,
		})
		return runnerResult{}, err
	}
	var prompt strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			prompt.WriteString(block.Text)
		}
	}
	generated := skills.WrapGeneratedSkillPrompt(skill.Name, args, prompt.String())
	sandbox := applySkillSandboxPolicy(r.config.SkillShellSandbox, policy.Sandbox)
	runner := r.runnerForScope(Scope{
		UserID:            userID,
		SessionID:         session.ID,
		WorkingDir:        workspace,
		SkillName:         skill.Name,
		SkillRoot:         skillDir,
		SkillScoped:       true,
		SkillShell:        skill.Shell,
		SkillShellEnv:     r.skillShellEnvironment(workspace, policy.AllowedEnv),
		SkillShellSandbox: sandbox,
		AllowedTools:      policy.AllowedTools,
		AllowedEnv:        policy.AllowedEnv,
		NetworkAllowlist:  policy.NetworkAllowlist,
		ArtifactTypes:     policy.ArtifactTypes,
	})
	result, err := runWithTokenStream(ctx, runner, session, generated, true, onToken)
	if err != nil {
		errText = err.Error()
		execDiagnostics = collectSkillExecutionDiagnostics(result.Session, startMessageCount)
		r.recordExecutionDenialRisk(ctx, userID, session.ID, skill.Name, err, map[string]any{
			"phase":             "skill_runner",
			"allowed_tools":     policy.AllowedTools,
			"network_allowlist": policy.NetworkAllowlist,
		})
		return runnerResult{Output: result.Output, Session: result.Session}, err
	}
	if result.Session == nil {
		result.Session = session
	}
	execDiagnostics = collectSkillExecutionDiagnostics(result.Session, startMessageCount)
	if execDiagnostics.SkillError != "" || execDiagnostics.ErrorKind != "" {
		status = SkillExecutionStatusFailed
		errText = firstNonEmpty(execDiagnostics.SkillError, execDiagnostics.ErrorKind)
		return runnerResult{Output: result.Output, Session: result.Session}, nil
	}
	if skillProducesArtifacts(skill) && execDiagnostics.ArtifactCount == 0 {
		registered, registerErr := r.registerGeneratedSkillArtifacts(ctx, userID, session.ID, workspace, policy.ArtifactTypes, generatedArtifactsBefore, result.Session)
		if registerErr != nil {
			status = SkillExecutionStatusFailed
			errText = registerErr.Error()
			if execDiagnostics.JSON == nil {
				execDiagnostics.JSON = map[string]any{}
			}
			for key, value := range generatedSkillArtifactDiagnostics(workspace, generatedArtifactsBefore) {
				execDiagnostics.JSON[key] = value
			}
			return runnerResult{Output: result.Output, Session: result.Session}, registerErr
		}
		if registered > 0 {
			execDiagnostics = collectSkillExecutionDiagnostics(result.Session, startMessageCount)
			if execDiagnostics.ArtifactCount == 0 {
				execDiagnostics.ArtifactCount = registered
			}
		}
	}
	if skillProducesArtifacts(skill) && execDiagnostics.ArtifactCount == 0 {
		status = SkillExecutionStatusFailed
		execDiagnostics.ErrorKind = "missing_artifact"
		if execDiagnostics.JSON == nil {
			execDiagnostics.JSON = map[string]any{}
		}
		execDiagnostics.JSON["error_kind"] = execDiagnostics.ErrorKind
		execDiagnostics.JSON["expected_artifact"] = true
		for key, value := range generatedSkillArtifactDiagnostics(workspace, generatedArtifactsBefore) {
			execDiagnostics.JSON[key] = value
		}
		errText = "skill completed without creating the expected artifact"
		return runnerResult{Output: result.Output, Session: result.Session}, errors.New(errText)
	}
	status = SkillExecutionStatusSucceeded
	return runnerResult{Output: result.Output, Session: result.Session}, err
}

func snapshotGeneratedSkillArtifactFiles(workspace string) map[string]struct{} {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, generatedArtifactStagingDir)
	out := map[string]struct{}{}
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		if rel, relErr := filepath.Rel(dir, path); relErr == nil {
			out[filepath.Clean(rel)] = struct{}{}
		}
		return nil
	})
	return out
}

func (r *Runtime) registerGeneratedSkillArtifacts(ctx context.Context, userID, sessionID, workspace string, allowedTypes []string, before map[string]struct{}, session *state.Session) (int64, error) {
	if r == nil || r.artifacts == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(workspace) == "" {
		return 0, nil
	}
	dir := filepath.Join(workspace, generatedArtifactStagingDir)
	var count int64
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if _, seen := before[rel]; seen {
			return nil
		}
		filename := filepath.Base(path)
		contentType := generatedSkillArtifactContentType(filename)
		if len(cleanStringSlice(allowedTypes)) > 0 && !artifactContentTypeAllowed(contentType, allowedTypes) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		artifact, err := r.CreateArtifact(ctx, userID, sessionID, filename, contentType, data)
		if err != nil {
			return err
		}
		count++
		recordArtifactToolResult(session, fmt.Sprintf("skill-generated-artifact-%d", count), artifact, "skill_generated_file")
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return count, err
}

func generatedSkillArtifactDiagnostics(workspace string, before map[string]struct{}) map[string]any {
	workspace = strings.TrimSpace(workspace)
	out := map[string]any{
		"generated_artifacts_before":    len(before),
		"generated_artifacts_total":     0,
		"generated_artifacts_new":       0,
		"generated_artifacts_new_files": []string{},
	}
	if workspace == "" {
		out["generated_artifacts_dir_exists"] = false
		return out
	}
	dir := filepath.Join(workspace, generatedArtifactStagingDir)
	out["generated_artifacts_dir"] = dir
	info, err := os.Stat(dir)
	if err != nil {
		out["generated_artifacts_dir_exists"] = false
		if !os.IsNotExist(err) {
			out["generated_artifacts_scan_error"] = err.Error()
		}
		return out
	}
	if !info.IsDir() {
		out["generated_artifacts_dir_exists"] = false
		out["generated_artifacts_scan_error"] = "path is not a directory"
		return out
	}
	out["generated_artifacts_dir_exists"] = true
	var total int
	var newFiles []string
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry == nil || entry.IsDir() {
			return nil
		}
		total++
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if _, seen := before[rel]; !seen {
			newFiles = append(newFiles, rel)
		}
		return nil
	})
	if err != nil {
		out["generated_artifacts_scan_error"] = err.Error()
	}
	sort.Strings(newFiles)
	out["generated_artifacts_total"] = total
	out["generated_artifacts_new"] = len(newFiles)
	if len(newFiles) > 10 {
		out["generated_artifacts_new_files"] = newFiles[:10]
		out["generated_artifacts_new_files_truncated"] = true
	} else {
		out["generated_artifacts_new_files"] = newFiles
	}
	return out
}

func generatedSkillArtifactContentType(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	}
}

func recordArtifactToolResult(session *state.Session, callID string, artifact *Artifact, source string) {
	if session == nil || artifact == nil {
		return
	}
	input, _ := json.Marshal(map[string]any{
		"filename":     artifact.Filename,
		"content_type": artifact.ContentType,
		"source":       source,
	})
	output, _ := json.Marshal(artifactToolOutput{
		ID:                   artifact.ID,
		Kind:                 artifact.Kind,
		Filename:             artifact.Filename,
		ContentType:          artifact.ContentType,
		SizeBytes:            artifact.SizeBytes,
		DownloadPath:         "/v1/artifacts/" + artifact.ID,
		AssistantInstruction: "Use this metadata as internal context only. Tell the user the generated artifact is ready in the Artifacts panel. Do not paste the artifact body/content into chat. Do not expose raw JSON, artifact IDs, object paths, or download paths unless the user explicitly asks for technical details.",
	})
	callID = strings.TrimSpace(callID)
	if callID == "" {
		callID = "artifact-" + artifact.ID
	}
	session.AddToolResult(callID, ArtifactToolName, json.RawMessage(input), string(output))
}

type skillExecutionDiagnostics struct {
	SkillError    string
	ErrorKind     string
	Provider      string
	Model         string
	ArtifactCount int64
	JSON          map[string]any
}

type inlineSkillInvocation struct {
	Name string
	Args string
}

type runAsJobSkillSelection struct {
	Name string
	Args string
}

func skillCommandContent(selection runAsJobSkillSelection) string {
	name := strings.TrimPrefix(strings.TrimSpace(selection.Name), "/")
	if name == "" {
		return strings.TrimSpace(selection.Args)
	}
	content := "/" + name
	if strings.TrimSpace(selection.Args) != "" {
		content += " " + strings.TrimSpace(selection.Args)
	}
	return content
}

func selectedRunAsJobSkill(session *state.Session, startIndex int) (runAsJobSkillSelection, bool) {
	if session == nil {
		return runAsJobSkillSelection{}, false
	}
	if startIndex < 0 {
		startIndex = 0
	}
	for _, message := range session.Messages[startIndex:] {
		if message.Role != "tool" || message.ToolName != skilltool.ToolName || !skilltool.IsRunAsJobMarker(message.ToolOutput) {
			continue
		}
		raw := strings.TrimPrefix(strings.TrimSpace(message.ToolOutput), skilltool.RunAsJobMarkerPrefix)
		var payload struct {
			Skill    string `json:"skill"`
			Args     string `json:"args"`
			RunAsJob bool   `json:"run_as_job"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return runAsJobSkillSelection{}, false
		}
		name := strings.TrimPrefix(strings.TrimSpace(payload.Skill), "/")
		if name == "" || !payload.RunAsJob {
			return runAsJobSkillSelection{}, false
		}
		return runAsJobSkillSelection{Name: name, Args: strings.TrimSpace(payload.Args)}, true
	}
	return runAsJobSkillSelection{}, false
}

func (r *Runtime) createSelectedSkillJob(ctx context.Context, req ChatRequest, sessionID string, selection runAsJobSkillSelection) (*Job, error) {
	if r == nil || r.jobs == nil {
		return nil, fmt.Errorf("job store is not configured")
	}
	if r.skills == nil {
		return nil, fmt.Errorf("skills are not configured")
	}
	skill, ok := r.skills.GetSkill(selection.Name)
	if !ok {
		return nil, fmt.Errorf("unknown skill: /%s", selection.Name)
	}
	if !skill.UserInvocable {
		return nil, fmt.Errorf("skill /%s is not user-invocable", selection.Name)
	}
	if !skill.RunAsJob {
		return nil, fmt.Errorf("skill /%s is not configured for job execution", selection.Name)
	}
	jobReq := ChatRequest{
		UserID:         req.UserID,
		SessionID:      sessionID,
		Content:        skillCommandContent(selection),
		AttachmentIDs:  append([]string(nil), req.AttachmentIDs...),
		AttachmentURLs: append([]ChatAttachmentURL(nil), req.AttachmentURLs...),
	}
	return r.CreateJob(ctx, jobReq, JobTypeSkill)
}

func (r *Runtime) recordInlineSkillExecutions(ctx context.Context, userID string, session *state.Session, startIndex int, startedAt time.Time, runErr error) {
	if r == nil || r.skillExecutions == nil || session == nil {
		return
	}
	invocations := inlineSkillInvocations(session, startIndex)
	if len(invocations) == 0 {
		return
	}
	diagnostics := collectSkillExecutionDiagnostics(session, startIndex)
	completedAt := time.Now().UTC()
	for _, invocation := range invocations {
		if strings.TrimSpace(invocation.Name) == "" {
			continue
		}
		status := SkillExecutionStatusSucceeded
		errText := ""
		if runErr != nil {
			status = SkillExecutionStatusFailed
			errText = runErr.Error()
		}
		if diagnostics.SkillError != "" || diagnostics.ErrorKind != "" {
			status = SkillExecutionStatusFailed
			errText = firstNonEmpty(diagnostics.SkillError, diagnostics.ErrorKind, errText)
		}
		record := SkillExecutionRecord{
			SkillName:      invocation.Name,
			UserID:         userID,
			SessionID:      session.ID,
			JobID:          jobIDFromContext(ctx),
			RequestID:      requestIDFromContext(ctx),
			Status:         status,
			Error:          errText,
			ErrorKind:      diagnostics.ErrorKind,
			Provider:       diagnostics.Provider,
			Model:          diagnostics.Model,
			InputSummary:   summarizeSkillInput(invocation.Args),
			ArtifactCount:  diagnostics.ArtifactCount,
			DurationMS:     completedAt.Sub(startedAt).Milliseconds(),
			DiagnosticJSON: diagnostics.JSON,
			StartedAt:      startedAt,
			CompletedAt:    completedAt,
			Metadata: map[string]any{
				"execution_path": "llm_skill_tool",
				"args_length":    len(invocation.Args),
			},
		}
		if r.skills != nil {
			skill, ok := r.skills.GetSkill(invocation.Name)
			if !ok {
				if err := r.skillExecutions.RecordSkillExecution(context.Background(), record); err != nil {
					logError(ctx, r.logger, "record LLM-selected skill execution failed", err,
						slog.String("request_id", record.RequestID),
						slog.String("user_id", userID),
						slog.String("session_id", session.ID),
						slog.String("job_id", record.JobID),
						slog.String("skill", invocation.Name),
					)
				}
				continue
			}
			policy := r.skillRuntimePolicy(skill)
			record.Metadata["allowed_tools"] = policy.AllowedTools
			record.Metadata["allowed_env"] = policy.AllowedEnv
			record.Metadata["network_allowlist"] = policy.NetworkAllowlist
			record.Metadata["artifact_types"] = policy.ArtifactTypes
			record.Metadata["execution_context"] = string(skill.ExecutionContext)
			record.Metadata["run_as_job"] = skill.RunAsJob
		}
		if err := r.skillExecutions.RecordSkillExecution(context.Background(), record); err != nil {
			logError(ctx, r.logger, "record LLM-selected skill execution failed", err,
				slog.String("request_id", record.RequestID),
				slog.String("user_id", userID),
				slog.String("session_id", session.ID),
				slog.String("job_id", record.JobID),
				slog.String("skill", invocation.Name),
			)
		}
	}
}

func inlineSkillInvocations(session *state.Session, startIndex int) []inlineSkillInvocation {
	if session == nil || startIndex >= len(session.Messages) {
		return nil
	}
	if startIndex < 0 {
		startIndex = 0
	}
	out := make([]inlineSkillInvocation, 0, 1)
	for _, message := range session.Messages[startIndex:] {
		if !strings.EqualFold(message.ToolName, "Skill") {
			continue
		}
		var input struct {
			Skill string `json:"skill"`
			Args  string `json:"args"`
		}
		if len(message.ToolInput) > 0 {
			_ = json.Unmarshal(message.ToolInput, &input)
		}
		name := strings.TrimPrefix(strings.TrimSpace(input.Skill), "/")
		if name == "" {
			name = skillNameFromGeneratedPrompt(message.ToolOutput)
		}
		if name == "" {
			continue
		}
		out = append(out, inlineSkillInvocation{Name: name, Args: input.Args})
	}
	return out
}

func skillNameFromGeneratedPrompt(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<command-name>/") && strings.HasSuffix(line, "</command-name>") {
			line = strings.TrimPrefix(line, "<command-name>/")
			line = strings.TrimSuffix(line, "</command-name>")
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func collectSkillExecutionDiagnostics(session *state.Session, startIndex int) skillExecutionDiagnostics {
	out := skillExecutionDiagnostics{JSON: map[string]any{}}
	if session == nil {
		return out
	}
	if startIndex < 0 || startIndex > len(session.Messages) {
		startIndex = 0
	}
	var logs []map[string]any
	for _, message := range session.Messages[startIndex:] {
		if strings.EqualFold(message.ToolName, ArtifactToolName) {
			out.ArtifactCount++
		}
		if strings.TrimSpace(message.ToolOutput) != "" {
			collectSkillDiagnosticLines(&out, message.ToolOutput, &logs)
		}
		if message.Role == state.MessageRoleAssistant && strings.TrimSpace(message.Content) != "" {
			collectSkillDiagnosticLines(&out, message.Content, &logs)
		}
	}
	if out.Provider == "" && out.Model != "" {
		out.Provider = "vertex"
	}
	if len(logs) > 0 {
		out.JSON["logs"] = logs
	}
	if out.SkillError != "" {
		out.JSON["skill_error"] = out.SkillError
	}
	if out.ErrorKind != "" {
		out.JSON["error_kind"] = out.ErrorKind
	}
	return out
}

func collectSkillDiagnosticLines(out *skillExecutionDiagnostics, text string, logs *[]map[string]any) {
	if out == nil || strings.TrimSpace(text) == "" {
		return
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "skill_error:"):
			out.SkillError = strings.TrimSpace(strings.TrimPrefix(line, "skill_error:"))
		case strings.HasPrefix(line, "error_kind:"):
			out.ErrorKind = strings.TrimSpace(strings.TrimPrefix(line, "error_kind:"))
		case strings.HasPrefix(line, "model:"):
			out.Model = strings.TrimSpace(strings.TrimPrefix(line, "model:"))
		case strings.HasPrefix(line, "skill_log:"):
			var entry map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "skill_log:"))), &entry); err != nil {
				continue
			}
			if logs != nil {
				*logs = append(*logs, entry)
			}
			if value := stringFromMap(entry, "provider"); value != "" {
				out.Provider = value
			}
			if value := stringFromMap(entry, "model"); value != "" {
				out.Model = value
			}
			if value := stringFromMap(entry, "kind"); value != "" {
				out.ErrorKind = value
			}
			if value := stringFromMap(entry, "error_kind"); value != "" {
				out.ErrorKind = value
			}
		}
	}
}

func stringFromMap(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func summarizeSkillInput(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	return truncateSkillExecutionString(args, 512)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *Runtime) skillShellEnvironment(workspace string, allowedEnv []string) map[string]string {
	env := map[string]string{
		"AGENT_WORKSPACE_DIR": workspace,
	}
	allowed := make(map[string]struct{}, len(allowedEnv))
	for _, key := range allowedEnv {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		allowed[key] = struct{}{}
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	if declaresVertexAccessToken(allowed) {
		if token, err := skillShellVertexAccessToken(); err == nil && token != "" {
			for _, key := range []string{"VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN"} {
				if _, ok := allowed[key]; ok {
					env[key] = token
				}
			}
		}
	}
	return env
}

var skillShellVertexAccessToken = func() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func declaresVertexAccessToken(allowed map[string]struct{}) bool {
	for _, key := range []string{"VERTEX_ACCESS_TOKEN", "GOOGLE_OAUTH_ACCESS_TOKEN", "GOOGLE_ACCESS_TOKEN"} {
		if _, ok := allowed[key]; ok {
			return true
		}
	}
	return false
}

func (r *Runtime) skillShellRuntime(workspace, skillRoot string, skill *skills.SkillDefinition, policy SkillRuntimePolicy) skills.PromptShellRuntime {
	if r == nil || skill == nil {
		return nil
	}
	sandbox := applySkillSandboxPolicy(r.config.SkillShellSandbox, policy.Sandbox)
	if !sandbox.dockerEnabled() {
		return nil
	}
	return NewDockerSkillShellRuntime(sandbox, skill.Shell, workspace, skillRoot, r.skillShellEnvironment(workspace, policy.AllowedEnv), policy.AllowedTools)
}

func runWithTokenStream(ctx context.Context, runner Runner, session *state.Session, prompt string, generated bool, onToken func(string)) (engine.Result, error) {
	if streaming, ok := runner.(StreamingRunner); ok {
		if generated {
			return streaming.RunGeneratedPromptStream(ctx, session, prompt, onToken)
		}
		return streaming.RunStream(ctx, session, prompt, onToken)
	}
	if generated {
		return runner.RunGeneratedPrompt(ctx, session, prompt)
	}
	return runner.Run(ctx, session, prompt)
}

func runWithTokenStreamContent(ctx context.Context, runner Runner, session *state.Session, prompt []publictypes.ContentBlock, generated bool, onToken func(string)) (engine.Result, error) {
	if len(prompt) == 1 && prompt[0].Type == "text" {
		return runWithTokenStream(ctx, runner, session, prompt[0].Text, generated, onToken)
	}
	if generated {
		return runWithTokenStream(ctx, runner, session, promptContentText(prompt), generated, onToken)
	}
	if streamingContentRunner, ok := runner.(StreamingContentRunner); ok {
		return streamingContentRunner.RunContentStream(ctx, session, prompt, onToken)
	}
	if contentRunner, ok := runner.(ContentRunner); ok {
		return contentRunner.RunContent(ctx, session, prompt)
	}
	return runWithTokenStream(ctx, runner, session, promptContentText(prompt), generated, onToken)
}

func promptContentText(prompt []publictypes.ContentBlock) string {
	parts := make([]string, 0, len(prompt))
	for _, block := range prompt {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (r *Runtime) runnerForScope(scope Scope) Runner {
	if r.engineFactory == nil {
		fmt.Printf("ERROR: engineFactory is nil - cannot create runner\n")
		return nilRunner{}
	}

	fmt.Printf("DEBUG: runnerForScope - creating runner for UserID=%s, SessionID=%s\n",
		scope.UserID, scope.SessionID)

	scope.WorkingDir = r.sandboxedWorkingDir(scope.UserID, scope.WorkingDir)
	if scope.WorkingDir == "" {
		scope.WorkingDir = filepath.Clean(r.config.DefaultWorkingDir)
	}
	if scope.SkillRoot != "" {
		scope.SkillRoot = filepath.Clean(scope.SkillRoot)
	}
	if scope.Artifacts == nil && r.artifacts != nil && strings.TrimSpace(scope.UserID) != "" && strings.TrimSpace(scope.SessionID) != "" {
		scope.Artifacts = sessionArtifactWriter{
			runtime:   r,
			userID:    scope.UserID,
			sessionID: scope.SessionID,
		}
		scope.ArtifactMaxBytes = r.artifacts.MaxBytes()
	}
	scope.Artifacts = NewArtifactContentTypeWriter(scope.Artifacts, scope.ArtifactTypes)

	runner := r.engineFactory(scope)
	if runner == nil {
		fmt.Printf("ERROR: engineFactory returned nil runner\n")
	} else {
		fmt.Printf("DEBUG: runnerForScope - runner created successfully\n")
	}

	return runner
}

type sessionArtifactWriter struct {
	runtime   *Runtime
	userID    string
	sessionID string
}

func (w sessionArtifactWriter) Write(ctx context.Context, filename, contentType string, data []byte) (*Artifact, error) {
	if w.runtime == nil {
		return nil, fmt.Errorf("runtime is not configured")
	}
	return w.runtime.CreateArtifact(ctx, w.userID, w.sessionID, filename, contentType, data)
}

func (r *Runtime) sandboxedWorkingDir(userID, requested string) string {
	if r.config.UserWorkspaceRoot == "" {
		if strings.TrimSpace(requested) != "" && r.config.AllowCustomWorkingDir {
			return filepath.Clean(requested)
		}
		return filepath.Clean(r.config.DefaultWorkingDir)
	}
	userRoot := r.userWorkspace(userID)
	if strings.TrimSpace(requested) == "" {
		return userRoot
	}
	clean := filepath.Clean(requested)
	rel, err := filepath.Rel(userRoot, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return userRoot
	}
	return clean
}

func (r *Runtime) userWorkspace(userID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	return filepath.Join(filepath.Clean(r.config.UserWorkspaceRoot), hex.EncodeToString(sum[:])[:32])
}

func (r *Runtime) start(key string, cancel context.CancelFunc, jobScoped bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return ErrRuntimeShuttingDown
	}
	r.running[key] = cancel
	if jobScoped {
		r.runningJobTurns[key] = true
	}
	r.wg.Add(1)
	return nil
}

func (r *Runtime) finish(key string) {
	r.mu.Lock()
	delete(r.running, key)
	delete(r.runningJobTurns, key)
	r.mu.Unlock()
	r.wg.Done()
}

func (r *Runtime) startJob(jobID string, cancel context.CancelFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return ErrRuntimeShuttingDown
	}
	r.runningJobs[jobID] = cancel
	r.wg.Add(1)
	return nil
}

func (r *Runtime) finishJob(jobID string) {
	r.mu.Lock()
	delete(r.runningJobs, jobID)
	r.mu.Unlock()
	r.wg.Done()
}

func (r *Runtime) subscribeJobEvents(jobID string) (<-chan *JobEvent, func()) {
	if r == nil || r.jobEvents == nil {
		ch := make(chan *JobEvent)
		close(ch)
		return ch, func() {}
	}
	return r.jobEvents.SubscribeJobEvents(jobID)
}

func (r *Runtime) PublishRemoteJobEvent(event *JobEvent) {
	if r == nil || r.jobEvents == nil || event == nil {
		return
	}
	_ = r.jobEvents.PublishJobEvent(context.Background(), event)
}

func (r *Runtime) runningSessionIDs(userID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0)
	for key := range r.running {
		var parts []string
		if err := json.Unmarshal([]byte(key), &parts); err == nil && len(parts) == 2 && parts[0] == userID {
			out = append(out, parts[1])
		}
	}
	return out
}

type runnerResult struct {
	Output    string
	Session   *state.Session
	Job       *Job
	JobReason string
}

type jobEventSink struct {
	store  JobStore
	bus    JobEventPublisher
	fanout JobEventPublisher
	job    *Job
	logger *slog.Logger
}

func (s *jobEventSink) Send(ctx context.Context, event Event) error {
	if s == nil || s.store == nil || s.job == nil {
		return fmt.Errorf("job event sink is not configured")
	}
	if event.SessionID == "" {
		event.SessionID = s.job.SessionID
	}
	event.JobID = s.job.ID
	record := &JobEvent{
		ID:        NewJobEventID(),
		JobID:     s.job.ID,
		UserID:    s.job.UserID,
		SessionID: s.job.SessionID,
		Type:      event.Type,
		Event:     event,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.AddJobEvent(ctx, record); err != nil {
		return err
	}
	if s.bus != nil {
		_ = s.bus.PublishJobEvent(ctx, record)
	}
	if s.fanout != nil {
		if err := s.fanout.PublishJobEvent(ctx, record); err != nil {
			attrs := contextLogAttrs(ctx, record.UserID, record.SessionID, record.JobID)
			attrs = append(attrs, slog.String("event_id", record.ID))
			logError(ctx, s.logger, "publish job event fanout failed", err, attrs...)
		}
	}
	return nil
}

type nilRunner struct{}

func (nilRunner) Run(context.Context, *state.Session, string) (engine.Result, error) {
	return engine.Result{}, fmt.Errorf("engine factory is required")
}

func (nilRunner) RunGeneratedPrompt(context.Context, *state.Session, string) (engine.Result, error) {
	return engine.Result{}, fmt.Errorf("engine factory is required")
}

func formatSkillList(items []*skills.SkillDefinition) string {
	if len(items) == 0 {
		return "No skills available."
	}
	var out strings.Builder
	out.WriteString("# Available Skills\n\n")
	for _, skill := range items {
		out.WriteString(fmt.Sprintf("- `/%s`", skill.Name))
		if strings.TrimSpace(skill.Description) != "" {
			out.WriteString(": ")
			out.WriteString(skill.Description)
		}
		var hints []string
		if skill.RunAsJob || skill.ExecutionContext == skills.ContextFork {
			hints = append(hints, "run mode: job")
		}
		if skillProducesArtifacts(skill) {
			hints = append(hints, "produces artifacts")
		}
		if len(hints) > 0 {
			out.WriteString(" (")
			out.WriteString(strings.Join(hints, "; "))
			out.WriteString(")")
		}
		out.WriteString("\n")
	}
	return out.String()
}

func sessionKey(userID, sessionID string) string {
	key, _ := json.Marshal([]string{userID, sessionID})
	return string(key)
}

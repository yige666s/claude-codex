package agentruntime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

type MemoryRecallOrchestrator struct {
	memory        MemoryService
	items         MemoryItemService
	episodes      MemoryEpisodeService
	decider       *MemoryRecallDecider
	rewriter      MemoryQueryRewriter
	traceStore    MemoryRecallTraceStore
	recallConfig  MemoryRecallConfig
	episodeConfig EpisodicMemoryConfig
	logger        *slog.Logger
}

type MemoryRecallOrchestratorInput struct {
	UserID          string
	Session         *state.Session
	Query           string
	Personalization PersonalizationSettings
}

type MemoryRecallOrchestratorResult struct {
	Decision       MemoryRecallDecision
	MemoryContent  string
	EpisodeContent string
	EpisodeResults []MemoryEpisodeSearchResult
	Trace          MemoryRecallTrace
}

func NewMemoryRecallOrchestrator(memory MemoryService, episodes MemoryEpisodeService, decider *MemoryRecallDecider, rewriter MemoryQueryRewriter, traceStore MemoryRecallTraceStore, recallConfig MemoryRecallConfig, episodeConfig EpisodicMemoryConfig, logger *slog.Logger) *MemoryRecallOrchestrator {
	return &MemoryRecallOrchestrator{
		memory:        memory,
		items:         memoryItemServiceFrom(memory),
		episodes:      episodes,
		decider:       decider,
		rewriter:      rewriter,
		traceStore:    traceStore,
		recallConfig:  normalizeMemoryRecallConfig(recallConfig),
		episodeConfig: normalizeEpisodicMemoryConfig(episodeConfig),
		logger:        logger,
	}
}

func (o *MemoryRecallOrchestrator) Recall(ctx context.Context, input MemoryRecallOrchestratorInput) (result MemoryRecallOrchestratorResult, err error) {
	if o == nil || input.Session == nil {
		return result, nil
	}
	start := time.Now()
	decider := o.decider
	if decider == nil {
		decider = NewMemoryRecallDecider(o.recallConfig, nil, o.logger)
	}
	decision := decider.Decide(ctx, MemoryRecallInput{
		UserID:          input.UserID,
		Session:         input.Session,
		Message:         input.Query,
		Personalization: input.Personalization,
	})
	result.Decision = decision
	decisionQuery := firstNonEmptyString(decision.Query, input.Query)
	recallQuery := decisionQuery
	trace := MemoryRecallTrace{
		UserID:        input.UserID,
		SessionID:     input.Session.ID,
		TriggerReason: decision.Reason,
		OriginalQuery: input.Query,
		Query:         recallQuery,
		Metadata: map[string]any{
			"decision_should":       decision.Should,
			"memory_policy_version": memoryPolicyVersionFromProvider(decider),
		},
	}
	defer func() {
		trace.LatencyMS = time.Since(start).Milliseconds()
		trace.MemoryChars = len([]rune(strings.TrimSpace(result.MemoryContent)))
		trace.EpisodeChars = len([]rune(strings.TrimSpace(result.EpisodeContent)))
		trace.Injected = trace.MemoryChars > 0 || trace.EpisodeChars > 0
		trace.EpisodeIDs = memoryRecallEpisodeIDs(result.EpisodeResults)
		trace.SourceRefs = append(trace.SourceRefs, memoryRecallEpisodeSourceRefs(result.EpisodeResults)...)
		trace = normalizeMemoryRecallTrace(trace)
		result.Trace = trace
		o.recordTrace(ctx, trace)
	}()
	if !decision.Should {
		return result, nil
	}
	if o.rewriter != nil && o.recallConfig.QueryRewriteEnabled {
		rewriteStart := time.Now()
		got, err := o.rewriter.RewriteMemoryRecallQuery(ctx, MemoryQueryRewriteInput{
			UserID:          input.UserID,
			Session:         input.Session,
			OriginalQuery:   input.Query,
			DecisionQuery:   decisionQuery,
			Personalization: input.Personalization,
			Config:          o.recallConfig,
		})
		trace.Metadata["query_rewrite_ms"] = time.Since(rewriteStart).Milliseconds()
		if err != nil {
			trace.QueryRewriteDegraded = true
			trace.Metadata["query_rewrite_error"] = err.Error()
			if o.logger != nil {
				o.logger.LogAttrs(ctx, slog.LevelDebug, "memory recall query rewrite skipped", contextLogAttrs(ctx, input.UserID, input.Session.ID, "")...)
			}
		} else {
			if strings.TrimSpace(got.Query) != "" {
				recallQuery = strings.TrimSpace(got.Query)
				trace.Query = recallQuery
			}
			trace.QueryRewriteUsed = got.Used
			trace.QueryRewriteReason = got.Reason
			trace.QueryRewriteDegraded = got.Degraded
			if got.Used {
				trace.RewrittenQuery = recallQuery
			}
		}
	}
	loaded, err := o.load(ctx, input.UserID, input.Session, recallQuery)
	if err != nil {
		trace.Degraded = true
		trace.DegradedReason = err.Error()
		return result, err
	}
	result.MemoryContent = loaded.MemoryContent
	result.EpisodeContent = loaded.EpisodeContent
	result.EpisodeResults = loaded.EpisodeResults
	trace.MemoryItemIDs = loaded.MemoryItemIDs
	trace.SourceRefs = loaded.SourceRefs
	trace.Degraded = loaded.Degraded
	trace.DegradedReason = loaded.DegradedReason
	return result, nil
}

type memoryRecallLoadResult struct {
	MemoryContent  string
	EpisodeContent string
	EpisodeResults []MemoryEpisodeSearchResult
	MemoryItemIDs  []string
	SourceRefs     []MemorySourceRef
	Degraded       bool
	DegradedReason string
}

func (o *MemoryRecallOrchestrator) load(ctx context.Context, userID string, session *state.Session, query string) (memoryRecallLoadResult, error) {
	config := normalizeMemoryRecallConfig(o.recallConfig)
	if !config.AsyncEnabled {
		return o.loadSync(ctx, userID, session, query)
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultMemoryRecallTimeout
	}
	recallCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct {
		result memoryRecallLoadResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := o.loadSync(recallCtx, userID, session, query)
		done <- outcome{result: result, err: err}
	}()
	select {
	case got := <-done:
		return got.result, got.err
	case <-recallCtx.Done():
		if errors.Is(recallCtx.Err(), context.DeadlineExceeded) {
			if o.logger != nil {
				o.logger.LogAttrs(ctx, slog.LevelDebug, "memory recall timed out", contextLogAttrs(ctx, userID, session.ID, "")...)
			}
			return memoryRecallLoadResult{Degraded: true, DegradedReason: "timeout"}, nil
		}
		return memoryRecallLoadResult{}, recallCtx.Err()
	}
}

func (o *MemoryRecallOrchestrator) loadSync(ctx context.Context, userID string, session *state.Session, query string) (memoryRecallLoadResult, error) {
	recallQuery := strings.TrimSpace(query)
	querySession := runtimeContextQuerySession(session, recallQuery)
	var result memoryRecallLoadResult
	if o.memory != nil {
		content, err := o.memory.LoadContext(ctx, userID, querySession)
		if err != nil {
			return result, err
		}
		result.MemoryContent = content
	}
	auditItems := o.auditMemoryItems(ctx, userID, session, recallQuery)
	result.MemoryItemIDs = memoryRecallItemIDs(auditItems)
	result.SourceRefs = memoryRecallItemSourceRefs(auditItems)
	if o.episodes != nil && o.episodeConfig.Enabled && o.episodeConfig.ContextEnabled && recallQuery != "" {
		episodeResults, err := o.episodes.SearchMemoryEpisodes(ctx, userID, recallQuery, MemoryEpisodeSearchOptions{
			Limit:    o.episodeConfig.InjectLimit,
			MinScore: 0.12,
		})
		if err != nil {
			return result, err
		}
		result.EpisodeResults = episodeResults
		result.EpisodeContent = formatEpisodeContextForPrompt(episodeResults)
	}
	return result, nil
}

func (o *MemoryRecallOrchestrator) auditMemoryItems(ctx context.Context, userID string, session *state.Session, query string) []MemoryItem {
	if o == nil || o.items == nil {
		return nil
	}
	items, err := o.items.ListMemoryItems(ctx, userID, MemoryItemFilter{Status: MemoryStatusActive})
	if err != nil {
		if o.logger != nil {
			o.logger.LogAttrs(ctx, slog.LevelDebug, "memory recall item audit failed", contextLogAttrs(ctx, userID, sessionIDFromSession(session), "")...)
		}
		return nil
	}
	return selectMemoryItemsForSessionContext(items, query, sessionIDFromSession(session), 12)
}

func (o *MemoryRecallOrchestrator) recordTrace(ctx context.Context, trace MemoryRecallTrace) {
	if o == nil || o.traceStore == nil {
		return
	}
	if err := o.traceStore.RecordMemoryRecallTrace(ctx, trace); err != nil && o.logger != nil {
		o.logger.LogAttrs(ctx, slog.LevelDebug, "memory recall trace record failed", contextLogAttrs(ctx, trace.UserID, trace.SessionID, trace.ID)...)
	}
}

func sessionIDFromSession(session *state.Session) string {
	if session == nil {
		return ""
	}
	return session.ID
}

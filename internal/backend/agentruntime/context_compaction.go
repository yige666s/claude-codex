package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

const defaultCompactionMaxMessages = 1000

type ContextCompactionOptions struct {
	MaxMessages    int
	MaxTokens      int
	TargetTokens   int
	PreserveRecent int
	IncludeSystem  bool
}

func DefaultContextCompactionOptions() ContextCompactionOptions {
	return ContextCompactionOptions{
		MaxMessages:    defaultCompactionMaxMessages,
		MaxTokens:      128000,
		TargetTokens:   96000,
		PreserveRecent: 8,
		IncludeSystem:  true,
	}
}

type ContextCompactionResult struct {
	Compacted       bool
	SummaryMessage  *state.Message
	CompactedCount  int
	TokensBefore    int
	TokensAfter     int
	MarkedTruncated int
}

type SummaryGenerator interface {
	GenerateSummary(ctx context.Context, userID, sessionID string, messages []state.Message) (string, error)
}

type LLMSummaryGenerator struct {
	RunnerFactory        any
	ContextRunnerFactory ContextEngineFactory
	Scope                Scope
}

func (g LLMSummaryGenerator) GenerateSummary(ctx context.Context, userID, sessionID string, messages []state.Message) (string, error) {
	if g.ContextRunnerFactory == nil && g.RunnerFactory == nil {
		return "", fmt.Errorf("runner factory is required")
	}
	scope := g.Scope
	if scope.UserID == "" {
		scope.UserID = userID
	}
	if scope.SessionID == "" {
		scope.SessionID = sessionID
	}
	var (
		runner Runner
		err    error
	)
	if g.ContextRunnerFactory != nil {
		runner, err = g.ContextRunnerFactory(ctx, scope)
		if err != nil {
			return "", err
		}
	} else {
		switch factory := g.RunnerFactory.(type) {
		case nil:
		case EngineFactory:
			runner = factory(scope)
		case ContextEngineFactory:
			runner, err = factory(ctx, scope)
			if err != nil {
				return "", err
			}
		case func(scope Scope) Runner:
			runner = factory(scope)
		case func(context.Context, Scope) (Runner, error):
			runner, err = factory(ctx, scope)
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unsupported runner factory type %T", g.RunnerFactory)
		}
	}
	if runner == nil {
		return "", fmt.Errorf("summary runner is required")
	}
	prompt := buildContextSummaryPrompt(messages)
	result, err := runner.RunGeneratedPrompt(ctx, state.NewSession(scope.WorkingDir), prompt)
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(result.Output)
	if summary == "" && result.Session != nil {
		for i := len(result.Session.Messages) - 1; i >= 0; i-- {
			msg := result.Session.Messages[i]
			if msg.Role == state.MessageRoleAssistant && strings.TrimSpace(msg.Content) != "" {
				summary = strings.TrimSpace(msg.Content)
				break
			}
		}
	}
	if summary == "" {
		return "", fmt.Errorf("summary runner returned empty summary")
	}
	return summary, nil
}

type ContextCompactionService struct {
	loader    *SessionLoadService
	writer    *MessageWriteService
	marker    MessageContextMarker
	generator SummaryGenerator
	now       func() time.Time
}

func NewContextCompactionService(loader *SessionLoadService, writer *MessageWriteService, marker MessageContextMarker, generator SummaryGenerator) *ContextCompactionService {
	return &ContextCompactionService{
		loader:    loader,
		writer:    writer,
		marker:    marker,
		generator: generator,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *ContextCompactionService) Compact(ctx context.Context, userID, sessionID string, opts ContextCompactionOptions) (ContextCompactionResult, error) {
	if s == nil || s.loader == nil || s.writer == nil || s.marker == nil || s.generator == nil {
		return ContextCompactionResult{}, fmt.Errorf("context compaction service is not fully configured")
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" {
		return ContextCompactionResult{}, fmt.Errorf("user ID is required")
	}
	if sessionID == "" {
		return ContextCompactionResult{}, fmt.Errorf("session ID is required")
	}
	opts = normalizeContextCompactionOptions(opts)
	// Read one message beyond the configured count limit so message-count based
	// compaction can be detected instead of silently dropping older context from
	// the load window. The latest summary is loaded separately because it may be
	// older than that window and still needs to be folded into the replacement
	// summary.
	messages, err := s.loader.LoadContext(ctx, userID, sessionID, SessionLoadOptions{
		MaxMessages:   opts.MaxMessages + 1,
		MaxTokens:     1 << 30,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: opts.IncludeSystem,
	})
	if err != nil {
		return ContextCompactionResult{}, err
	}
	if opts.IncludeSystem {
		if summary, ok, summaryErr := s.loader.loadLatestSummary(ctx, userID, sessionID, messages); summaryErr != nil {
			return ContextCompactionResult{}, summaryErr
		} else if ok {
			messages = prependMessageIfMissing(summary, messages)
		}
	}
	tokensBefore := estimateMessagesTokens(messages)
	if tokensBefore <= opts.MaxTokens && activeOrdinaryMessageCount(messages) <= opts.MaxMessages {
		return ContextCompactionResult{TokensBefore: tokensBefore, TokensAfter: tokensBefore}, nil
	}
	overflow, preserved := splitMessagesForCompaction(messages, opts)
	if len(overflow) == 0 {
		return ContextCompactionResult{TokensBefore: tokensBefore, TokensAfter: tokensBefore}, nil
	}
	summary, err := s.generator.GenerateSummary(ctx, userID, sessionID, overflow)
	if err != nil {
		return ContextCompactionResult{}, err
	}
	ids := messageIDs(overflow)
	summaryMessage := state.Message{
		Role:          state.MessageRoleSystem,
		ContentType:   state.MessageContentTypeSummary,
		Content:       "[历史摘要] " + summary,
		Status:        state.MessageStatusNormal,
		IsContextUsed: true,
		CreatedAt:     s.now(),
	}
	if compactor, ok := any(s.writer.repo).(MessageContextCompactionWriter); ok {
		created, marked, err := compactor.WriteSummaryAndMarkMessagesContextUnused(ctx, userID, sessionID, summaryMessage, ids)
		if err != nil {
			return ContextCompactionResult{}, err
		}
		if err := s.writer.applyCreatedMessageSideEffects(ctx, created, true); err != nil {
			return ContextCompactionResult{}, err
		}
		tokensAfter := created.EstimateTokens() + estimateMessagesTokens(preserved)
		return ContextCompactionResult{
			Compacted:       true,
			SummaryMessage:  &created,
			CompactedCount:  len(overflow),
			TokensBefore:    tokensBefore,
			TokensAfter:     tokensAfter,
			MarkedTruncated: marked,
		}, nil
	}
	created, err := s.writer.persist(ctx, MessageWriteRequest{
		UserID:    userID,
		SessionID: sessionID,
		Message:   summaryMessage,
	})
	if err != nil {
		return ContextCompactionResult{}, err
	}
	marked, err := s.marker.MarkMessagesContextUnused(ctx, userID, sessionID, ids)
	if err != nil {
		return ContextCompactionResult{}, err
	}
	if err := s.writer.applyCreatedMessageSideEffects(ctx, created, true); err != nil {
		return ContextCompactionResult{}, err
	}
	tokensAfter := created.EstimateTokens() + estimateMessagesTokens(preserved)
	return ContextCompactionResult{
		Compacted:       true,
		SummaryMessage:  &created,
		CompactedCount:  len(overflow),
		TokensBefore:    tokensBefore,
		TokensAfter:     tokensAfter,
		MarkedTruncated: marked,
	}, nil
}

func normalizeContextCompactionOptions(opts ContextCompactionOptions) ContextCompactionOptions {
	defaults := DefaultContextCompactionOptions()
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = defaults.MaxMessages
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = defaults.MaxTokens
	}
	if opts.TargetTokens <= 0 || opts.TargetTokens >= opts.MaxTokens {
		opts.TargetTokens = defaults.TargetTokens
		if opts.TargetTokens >= opts.MaxTokens {
			opts.TargetTokens = opts.MaxTokens * 3 / 4
		}
	}
	if opts.PreserveRecent <= 0 {
		opts.PreserveRecent = defaults.PreserveRecent
	}
	if !opts.IncludeSystem {
		opts.IncludeSystem = defaults.IncludeSystem
	}
	return opts
}

func splitMessagesForCompaction(messages []state.Message, opts ContextCompactionOptions) ([]state.Message, []state.Message) {
	candidates := make([]state.Message, 0, len(messages))
	previousSummaries := make([]state.Message, 0, 1)
	for _, message := range messages {
		if message.Status != 0 && message.Status != state.MessageStatusNormal {
			continue
		}
		if !message.IsContextUsed {
			continue
		}
		if message.ContentType == state.MessageContentTypeSummary {
			previousSummaries = append(previousSummaries, message)
			continue
		}
		if message.Role == state.MessageRoleSystem {
			continue
		}
		candidates = append(candidates, message)
	}
	if len(candidates) <= opts.PreserveRecent {
		if len(previousSummaries) == 0 {
			return nil, candidates
		}
		return previousSummaries, candidates
	}
	preserved := append([]state.Message(nil), candidates[len(candidates)-opts.PreserveRecent:]...)
	overflow := append([]state.Message(nil), previousSummaries...)
	overflow = append(overflow, candidates[:len(candidates)-opts.PreserveRecent]...)
	for estimateMessagesTokens(preserved) > opts.TargetTokens && len(preserved) > 1 {
		overflow = append(overflow, preserved[0])
		preserved = preserved[1:]
	}
	return overflow, preserved
}

func activeOrdinaryMessageCount(messages []state.Message) int {
	count := 0
	for _, message := range messages {
		if message.Status != 0 && message.Status != state.MessageStatusNormal {
			continue
		}
		if !message.IsContextUsed || message.ContentType == state.MessageContentTypeSummary || message.Role == state.MessageRoleSystem {
			continue
		}
		count++
	}
	return count
}

func buildContextSummaryPrompt(messages []state.Message) string {
	var b strings.Builder
	b.WriteString("请将以下 Agent 多轮对话历史压缩成一段可继续推理使用的摘要。\n")
	b.WriteString("要求：保留用户目标、关键事实、已做决定、未完成事项、重要工具结果和后续约束；不要编造；用简洁中文输出。\n\n")
	for _, message := range messages {
		role := message.Role
		if role == "" {
			role = "message"
		}
		content := strings.TrimSpace(firstNonEmptyString(message.Content, message.ToolOutput))
		if content == "" && len(message.ToolInput) > 0 {
			content = string(message.ToolInput)
		}
		if content == "" {
			continue
		}
		b.WriteString("### ")
		b.WriteString(role)
		if message.ToolName != "" {
			b.WriteString(" / ")
			b.WriteString(message.ToolName)
		}
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

func estimateMessagesTokens(messages []state.Message) int {
	total := 0
	for i := range messages {
		total += messages[i].EstimateTokens()
	}
	return total
}

func messageIDs(messages []state.Message) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		if strings.TrimSpace(message.ID) != "" {
			ids = append(ids, message.ID)
		}
	}
	return ids
}

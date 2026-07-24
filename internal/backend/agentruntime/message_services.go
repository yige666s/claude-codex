package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

const (
	SessionLoadStrategyRecent        = "recent"
	SessionLoadStrategySummary       = "summary"
	SessionLoadStrategySlidingWindow = "sliding_window"

	MessageEventCreated = "message.created"
	MessageEventDeleted = "message.deleted"
)

type MessageRepository interface {
	AppendMessage(ctx context.Context, userID, sessionID string, message state.Message) (state.Message, error)
	LoadSessionMessages(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error)
}

type MessageSummaryRepository interface {
	LoadLatestSummaryMessage(ctx context.Context, userID, sessionID string) (state.Message, bool, error)
}

type MessageContextMarker interface {
	MarkMessagesContextUnused(ctx context.Context, userID, sessionID string, messageIDs []string) (int, error)
}

type MessageContextCompactionWriter interface {
	WriteSummaryAndMarkMessagesContextUnused(ctx context.Context, userID, sessionID string, summary state.Message, messageIDs []string) (state.Message, int, error)
}

type MessageAttachmentProcessorStore interface {
	ListPendingMessageAttachments(ctx context.Context, userID string, limit int) ([]state.MessageAttachment, error)
	UpdateMessageAttachmentProcessing(ctx context.Context, userID, messageID, attachmentID string, status int, thumbnailKey, extractedTextKey string) error
}

type MessageAttachmentProcessingQueue interface {
	ListPendingMessageAttachmentsForProcessing(ctx context.Context, limit int) ([]state.MessageAttachment, error)
	UpdateMessageAttachmentProcessing(ctx context.Context, userID, messageID, attachmentID string, status int, thumbnailKey, extractedTextKey string) error
}

type MessageFullTextBackfillStore interface {
	ListMessagesForFullTextBackfill(ctx context.Context, afterCreatedAt time.Time, afterMessageID string, limit int) ([]state.Message, error)
}

type MessageWriteRequest struct {
	UserID    string
	SessionID string
	Message   state.Message
}

type MessageEvent struct {
	Type      string        `json:"type"`
	UserID    string        `json:"user_id"`
	SessionID string        `json:"session_id"`
	Message   state.Message `json:"message"`
	CreatedAt time.Time     `json:"created_at"`
}

type MessageEventPublisher interface {
	PublishMessageEvent(ctx context.Context, event MessageEvent) error
}

type DurableMessageEventRepository interface {
	MessageEventOutboxEnabled() bool
}

type CompositeMessageEventPublisher []MessageEventPublisher

func (p CompositeMessageEventPublisher) PublishMessageEvent(ctx context.Context, event MessageEvent) error {
	for _, publisher := range p {
		if publisher == nil {
			continue
		}
		if err := publisher.PublishMessageEvent(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

type SessionContextCache interface {
	GetContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, bool, error)
	SetContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions, messages []state.Message) error
	InvalidateContext(ctx context.Context, userID, sessionID string) error
}

type SessionContextMessageAppender interface {
	AppendContextMessage(ctx context.Context, userID, sessionID string, message state.Message) error
}

type SessionContextWindowCache interface {
	ContextWindowSize() int
}

type MessageWriteService struct {
	repo      MessageRepository
	cache     SessionContextCache
	publisher MessageEventPublisher
	now       func() time.Time
}

func NewMessageWriteService(repo MessageRepository, cache SessionContextCache, publisher MessageEventPublisher) *MessageWriteService {
	return &MessageWriteService{
		repo:      repo,
		cache:     cache,
		publisher: publisher,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *MessageWriteService) Write(ctx context.Context, req MessageWriteRequest) (state.Message, error) {
	created, err := s.persist(ctx, req)
	if err != nil {
		return state.Message{}, err
	}
	if err := s.applyCreatedMessageSideEffects(ctx, created, false); err != nil {
		return state.Message{}, err
	}
	return created, nil
}

func (s *MessageWriteService) persist(ctx context.Context, req MessageWriteRequest) (state.Message, error) {
	if s == nil || s.repo == nil {
		return state.Message{}, fmt.Errorf("message repository is required")
	}
	userID := strings.TrimSpace(req.UserID)
	sessionID := strings.TrimSpace(req.SessionID)
	if userID == "" {
		return state.Message{}, fmt.Errorf("user ID is required")
	}
	if sessionID == "" {
		return state.Message{}, fmt.Errorf("session ID is required")
	}
	message := normalizeWriteMessage(req.Message, userID, sessionID, s.now())
	return s.repo.AppendMessage(ctx, userID, sessionID, message)
}

func (s *MessageWriteService) applyCreatedMessageSideEffects(ctx context.Context, created state.Message, invalidate bool) error {
	if s == nil {
		return fmt.Errorf("message writer is required")
	}
	userID := strings.TrimSpace(created.UserID)
	sessionID := strings.TrimSpace(created.SessionID)
	if s.cache != nil {
		if invalidate {
			if err := s.cache.InvalidateContext(ctx, userID, sessionID); err != nil {
				return err
			}
		} else if appender, ok := s.cache.(SessionContextMessageAppender); ok {
			if err := appender.AppendContextMessage(ctx, userID, sessionID, created); err != nil {
				return err
			}
		} else if err := s.cache.InvalidateContext(ctx, userID, sessionID); err != nil {
			return err
		}
	}
	durablePublisher := false
	if repo, ok := s.repo.(DurableMessageEventRepository); ok {
		durablePublisher = repo.MessageEventOutboxEnabled()
	}
	if s.publisher != nil && !durablePublisher {
		if err := s.publisher.PublishMessageEvent(ctx, MessageEvent{
			Type:      MessageEventCreated,
			UserID:    userID,
			SessionID: sessionID,
			Message:   created,
			CreatedAt: s.now(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *MessageWriteService) WriteMany(ctx context.Context, userID, sessionID string, messages []state.Message) ([]state.Message, error) {
	out := make([]state.Message, 0, len(messages))
	for _, message := range messages {
		created, err := s.Write(ctx, MessageWriteRequest{
			UserID:    userID,
			SessionID: sessionID,
			Message:   message,
		})
		if err != nil {
			return out, err
		}
		out = append(out, created)
	}
	return out, nil
}

func (s *MessageWriteService) WriteUserMessage(ctx context.Context, userID, sessionID, content string, hidden bool) (state.Message, error) {
	return s.Write(ctx, MessageWriteRequest{
		UserID:    userID,
		SessionID: sessionID,
		Message: state.Message{
			Role:        state.MessageRoleUser,
			ContentType: state.MessageContentTypeText,
			Content:     content,
			Hidden:      hidden,
		},
	})
}

func (s *MessageWriteService) WriteAssistantMessage(ctx context.Context, userID, sessionID, content string, toolCalls []state.ToolCall, hidden bool) (state.Message, error) {
	contentType := state.MessageContentTypeText
	if len(toolCalls) > 0 {
		contentType = state.MessageContentTypeToolCall
	}
	return s.Write(ctx, MessageWriteRequest{
		UserID:    userID,
		SessionID: sessionID,
		Message: state.Message{
			Role:        state.MessageRoleAssistant,
			ContentType: contentType,
			Content:     content,
			ToolCalls:   toolCalls,
			Hidden:      hidden,
		},
	})
}

func (s *MessageWriteService) WriteToolResult(ctx context.Context, userID, sessionID, callID, toolName string, input []byte, output string) (state.Message, error) {
	return s.Write(ctx, MessageWriteRequest{
		UserID:    userID,
		SessionID: sessionID,
		Message: state.Message{
			Role:        state.MessageRoleTool,
			ContentType: state.MessageContentTypeToolResult,
			ToolCallID:  callID,
			ToolName:    toolName,
			ToolInput:   input,
			ToolOutput:  output,
			Hidden:      true,
		},
	})
}

type SessionLoadOptions struct {
	MaxMessages   int
	MaxTokens     int
	LoadStrategy  string
	IncludeSystem bool
}

func DefaultSessionLoadOptions() SessionLoadOptions {
	return SessionLoadOptions{
		MaxMessages:   100,
		MaxTokens:     128000,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: true,
	}
}

type SessionLoadService struct {
	repo  MessageRepository
	cache SessionContextCache
}

func NewSessionLoadService(repo MessageRepository, cache SessionContextCache) *SessionLoadService {
	return &SessionLoadService{repo: repo, cache: cache}
}

func (s *SessionLoadService) LoadContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("message repository is required")
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" {
		return nil, fmt.Errorf("user ID is required")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}
	opts = normalizeSessionLoadOptions(opts)
	switch opts.LoadStrategy {
	case SessionLoadStrategySummary:
		return s.loadSummaryContext(ctx, userID, sessionID, opts)
	case SessionLoadStrategySlidingWindow:
		return s.loadSlidingWindowContext(ctx, userID, sessionID, opts)
	default:
		return s.loadRecentContext(ctx, userID, sessionID, opts)
	}
}

func (s *SessionLoadService) loadRecentContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	messages, err := s.loadWindowMessages(ctx, userID, sessionID, opts)
	if err != nil {
		return nil, err
	}
	return applySessionTokenBudget(applySessionMaxMessages(messages, opts), opts), nil
}

func (s *SessionLoadService) loadSummaryContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	recentOpts := opts
	recentOpts.IncludeSystem = false
	recent, err := s.loadWindowMessages(ctx, userID, sessionID, recentOpts)
	if err != nil {
		return nil, err
	}
	recent = applySessionMaxMessages(recent, recentOpts)
	if opts.IncludeSystem {
		if summary, ok, err := s.loadLatestSummary(ctx, userID, sessionID, recent); err != nil {
			return nil, err
		} else if ok {
			recent = prependMessageIfMissing(summary, recent)
		}
	}
	return applySessionTokenBudget(recent, opts), nil
}

func (s *SessionLoadService) loadSlidingWindowContext(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	windowOpts := opts
	if windowOpts.MaxMessages < 200 {
		windowOpts.MaxMessages = 200
	}
	messages, err := s.loadWindowMessages(ctx, userID, sessionID, windowOpts)
	if err != nil {
		return nil, err
	}
	selected := applySlidingWindowBudget(messages, opts)
	if opts.IncludeSystem && !containsSystemContextMessage(selected) {
		if summary, ok, err := s.loadLatestSummary(ctx, userID, sessionID, messages); err != nil {
			return nil, err
		} else if ok {
			selected = prependMessageIfMissing(summary, selected)
		}
	}
	return applySessionTokenBudget(selected, opts), nil
}

func (s *SessionLoadService) loadWindowMessages(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	if s.cache != nil {
		if cached, ok, err := s.cache.GetContext(ctx, userID, sessionID, opts); err != nil {
			return nil, err
		} else if ok {
			return cloneStateMessages(cached), nil
		}
	}
	loadOpts := opts
	if windowCache, ok := s.cache.(SessionContextWindowCache); ok {
		if windowSize := windowCache.ContextWindowSize(); windowSize > loadOpts.MaxMessages {
			loadOpts.MaxMessages = windowSize
		}
		loadOpts.IncludeSystem = true
	}
	messages, err := s.repo.LoadSessionMessages(ctx, userID, sessionID, loadOpts)
	if err != nil {
		return nil, err
	}
	windowMessages := cloneStateMessages(messages)
	if s.cache != nil {
		if err := s.cache.SetContext(ctx, userID, sessionID, loadOpts, windowMessages); err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func (s *SessionLoadService) loadLatestSummary(ctx context.Context, userID, sessionID string, fallback []state.Message) (state.Message, bool, error) {
	if repo, ok := s.repo.(MessageSummaryRepository); ok {
		return repo.LoadLatestSummaryMessage(ctx, userID, sessionID)
	}
	for i := len(fallback) - 1; i >= 0; i-- {
		if isSystemContextMessage(fallback[i]) {
			return fallback[i], true, nil
		}
	}
	return state.Message{}, false, nil
}

type NoopMessageEventPublisher struct{}

func (NoopMessageEventPublisher) PublishMessageEvent(context.Context, MessageEvent) error {
	return nil
}

type NoopSessionContextCache struct{}

func (NoopSessionContextCache) GetContext(context.Context, string, string, SessionLoadOptions) ([]state.Message, bool, error) {
	return nil, false, nil
}

func (NoopSessionContextCache) SetContext(context.Context, string, string, SessionLoadOptions, []state.Message) error {
	return nil
}

func (NoopSessionContextCache) InvalidateContext(context.Context, string, string) error {
	return nil
}

type MemorySessionContextCache struct {
	mu    sync.RWMutex
	items map[string][]state.Message
}

func NewMemorySessionContextCache() *MemorySessionContextCache {
	return &MemorySessionContextCache{items: map[string][]state.Message{}}
}

func (c *MemorySessionContextCache) GetContext(_ context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	messages, ok := c.items[sessionContextCacheKey(userID, sessionID, opts)]
	if !ok {
		return nil, false, nil
	}
	return cloneStateMessages(messages), true, nil
}

func (c *MemorySessionContextCache) SetContext(_ context.Context, userID, sessionID string, opts SessionLoadOptions, messages []state.Message) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[sessionContextCacheKey(userID, sessionID, opts)] = cloneStateMessages(messages)
	return nil
}

func (c *MemorySessionContextCache) InvalidateContext(_ context.Context, userID, sessionID string) error {
	if c == nil {
		return nil
	}
	prefix := strings.TrimSpace(userID) + ":" + strings.TrimSpace(sessionID) + ":"
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.items {
		if strings.HasPrefix(key, prefix) {
			delete(c.items, key)
		}
	}
	return nil
}

func normalizeWriteMessage(message state.Message, userID, sessionID string, now time.Time) state.Message {
	if strings.TrimSpace(message.ID) == "" {
		message.ID = uuid.NewString()
	}
	message.UserID = userID
	message.SessionID = sessionID
	if message.Status == 0 {
		message.Status = state.MessageStatusNormal
	}
	if message.ContentType == "" {
		message.ContentType = inferRuntimeMessageContentType(message)
	}
	if len(message.ContentParts) == 0 && len(message.ContentBlocks) > 0 {
		message.ContentParts = message.ContentBlocks
	}
	if len(message.ContentParts) > 0 {
		message.ContentParts = normalizeMessageContentParts(message.ContentParts)
		message.ContentBlocks = message.ContentParts
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = now
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.CreatedAt
	}
	if len(message.Attachments) == 0 {
		message.Attachments = messageAttachmentRefs(message)
	}
	if message.Status == state.MessageStatusNormal {
		message.IsContextUsed = true
	}
	if message.PromptTokens == 0 && message.CompletionTokens == 0 {
		tokens := message.EstimateTokens()
		if message.Role == state.MessageRoleAssistant {
			message.CompletionTokens = tokens
		} else {
			message.PromptTokens = tokens
		}
	}
	return message
}

func normalizeSessionLoadOptions(opts SessionLoadOptions) SessionLoadOptions {
	defaults := DefaultSessionLoadOptions()
	if opts == (SessionLoadOptions{}) {
		return defaults
	}
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = defaults.MaxMessages
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = defaults.MaxTokens
	}
	if strings.TrimSpace(opts.LoadStrategy) == "" {
		opts.LoadStrategy = defaults.LoadStrategy
	} else {
		opts.LoadStrategy = strings.ToLower(strings.TrimSpace(opts.LoadStrategy))
		switch opts.LoadStrategy {
		case SessionLoadStrategyRecent, SessionLoadStrategySummary, SessionLoadStrategySlidingWindow:
		default:
			opts.LoadStrategy = defaults.LoadStrategy
		}
	}
	return opts
}

func applySessionTokenBudget(messages []state.Message, opts SessionLoadOptions) []state.Message {
	if opts.MaxTokens <= 0 || len(messages) == 0 {
		return cloneStateMessages(messages)
	}
	systemMessages := make([]state.Message, 0)
	ordinary := make([]state.Message, 0, len(messages))
	for _, message := range messages {
		if isSystemContextMessage(message) {
			if opts.IncludeSystem {
				systemMessages = append(systemMessages, message)
			}
			continue
		}
		ordinary = append(ordinary, message)
	}

	selected := make([]state.Message, 0, len(messages))
	tokenCount := 0
	for _, message := range systemMessages {
		estimated := message.EstimateTokens()
		if tokenCount+estimated > opts.MaxTokens && len(selected) > 0 {
			continue
		}
		selected = append(selected, message)
		tokenCount += estimated
	}
	recent := make([]state.Message, 0, len(ordinary))
	for i := len(ordinary) - 1; i >= 0; i-- {
		message := ordinary[i]
		estimated := message.EstimateTokens()
		if tokenCount+estimated > opts.MaxTokens && len(recent) > 0 {
			break
		}
		recent = append(recent, message)
		tokenCount += estimated
	}
	for i := len(recent) - 1; i >= 0; i-- {
		selected = append(selected, recent[i])
	}
	return selected
}

func applySessionMaxMessages(messages []state.Message, opts SessionLoadOptions) []state.Message {
	if len(messages) == 0 {
		return nil
	}
	filtered := make([]state.Message, 0, len(messages))
	for _, message := range messages {
		if message.Status != 0 && message.Status != state.MessageStatusNormal {
			continue
		}
		if !message.IsContextUsed && message.ID != "" {
			continue
		}
		if isSystemContextMessage(message) && !opts.IncludeSystem {
			continue
		}
		filtered = append(filtered, message)
	}
	if opts.MaxMessages > 0 && len(filtered) > opts.MaxMessages {
		filtered = filtered[len(filtered)-opts.MaxMessages:]
	}
	return cloneStateMessages(filtered)
}

func applySlidingWindowBudget(messages []state.Message, opts SessionLoadOptions) []state.Message {
	if len(messages) == 0 {
		return nil
	}
	filtered := applySessionMaxMessages(messages, SessionLoadOptions{
		MaxMessages:   0,
		IncludeSystem: opts.IncludeSystem,
	})
	if len(filtered) == 0 {
		return nil
	}
	systemMessages := make([]state.Message, 0)
	ordinary := make([]state.Message, 0, len(filtered))
	for _, message := range filtered {
		if isSystemContextMessage(message) {
			if opts.IncludeSystem {
				systemMessages = append(systemMessages, message)
			}
			continue
		}
		ordinary = append(ordinary, message)
	}
	selected := make([]state.Message, 0, len(filtered))
	tokenCount := 0
	for i := len(ordinary) - 1; i >= 0; i-- {
		message := ordinary[i]
		estimated := message.EstimateTokens()
		if opts.MaxTokens > 0 && tokenCount+estimated > opts.MaxTokens && len(selected) > 0 {
			break
		}
		selected = append(selected, message)
		tokenCount += estimated
		if opts.MaxMessages > 0 && len(selected) >= opts.MaxMessages {
			break
		}
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	if opts.IncludeSystem && len(systemMessages) > 0 && (opts.MaxMessages <= 0 || len(selected) < opts.MaxMessages) {
		summary := systemMessages[len(systemMessages)-1]
		selected = prependMessageIfMissing(summary, selected)
	}
	return selected
}

func prependMessageIfMissing(message state.Message, messages []state.Message) []state.Message {
	if message.ID != "" {
		for _, existing := range messages {
			if existing.ID == message.ID {
				return messages
			}
		}
	}
	out := make([]state.Message, 0, len(messages)+1)
	out = append(out, message)
	out = append(out, messages...)
	return out
}

func containsSystemContextMessage(messages []state.Message) bool {
	for _, message := range messages {
		if isSystemContextMessage(message) {
			return true
		}
	}
	return false
}

func inferRuntimeMessageContentType(message state.Message) string {
	if len(message.ContentParts) > 0 || len(message.ContentBlocks) > 0 {
		return state.MessageContentTypeMultipart
	}
	if message.ContentType != "" {
		return message.ContentType
	}
	if message.Role == state.MessageRoleTool || message.ToolCallID != "" || message.ToolOutput != "" {
		return state.MessageContentTypeToolResult
	}
	if len(message.ToolCalls) > 0 {
		return state.MessageContentTypeToolCall
	}
	return state.MessageContentTypeText
}

func isSystemContextMessage(message state.Message) bool {
	return message.Role == state.MessageRoleSystem || message.ContentType == state.MessageContentTypeSummary
}

func cloneStateMessages(messages []state.Message) []state.Message {
	if messages == nil {
		return nil
	}
	out := make([]state.Message, len(messages))
	copy(out, messages)
	for i := range out {
		if messages[i].ContentParts != nil {
			out[i].ContentParts = append([]publictypes.ContentBlock(nil), messages[i].ContentParts...)
		}
		if messages[i].ContentBlocks != nil {
			out[i].ContentBlocks = append([]publictypes.ContentBlock(nil), messages[i].ContentBlocks...)
		}
		if messages[i].ToolCalls != nil {
			out[i].ToolCalls = append([]state.ToolCall(nil), messages[i].ToolCalls...)
		}
		if messages[i].Attachments != nil {
			out[i].Attachments = append([]state.MessageAttachment(nil), messages[i].Attachments...)
		}
		if messages[i].ArchivedAt != nil {
			archivedAt := *messages[i].ArchivedAt
			out[i].ArchivedAt = &archivedAt
		}
	}
	return out
}

func sessionContextCacheKey(userID, sessionID string, opts SessionLoadOptions) string {
	return fmt.Sprintf("%s:%s:%s",
		strings.TrimSpace(userID),
		strings.TrimSpace(sessionID),
		sessionContextOptionsCacheKey(opts),
	)
}

func sessionContextOptionsCacheKey(opts SessionLoadOptions) string {
	return fmt.Sprintf("%d:%d:%s:%t",
		opts.MaxMessages,
		opts.MaxTokens,
		strings.TrimSpace(opts.LoadStrategy),
		opts.IncludeSystem,
	)
}

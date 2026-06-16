package agentruntime

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"

	"claude-codex/internal/harness/state"
)

const (
	defaultMemoryRecallTimeout                 = 1200 * time.Millisecond
	defaultMemoryRecallMinQueryRunes           = 8
	defaultMemoryRecallRecentContextMessages   = 4
	defaultMemoryRecallRecentContextRunes      = 400
	defaultMemoryRecallForceInterval           = 10
	defaultMemoryRecallComplexTokenThreshold   = 200
	defaultMemoryRecallEmbeddingThreshold      = 0.75
	defaultMemoryRecallEmbeddingWindow         = 3
	defaultMemoryRecallIntentThreshold         = 0.6
	defaultMemoryRecallIntentContextTurns      = 4
	memoryRecallReasonDisabled                 = "disabled"
	memoryRecallReasonNoMessage                = "no_message"
	memoryRecallReasonForceRule                = "force_rule"
	memoryRecallReasonKeywordRule              = "keyword_rule"
	memoryRecallReasonEntityRule               = "ner_entity"
	memoryRecallReasonEmbeddingDrift           = "embedding_drift"
	memoryRecallReasonEmbeddingUnavailable     = "embedding_unavailable"
	memoryRecallReasonEmbeddingFallbackNoMatch = "embedding_fallback_no_match"
	memoryRecallReasonIntentClassifier         = "intent_classifier"
	memoryRecallReasonIntentUnavailable        = "intent_classifier_unavailable"
	memoryRecallReasonNoRecall                 = "no_recall_needed"
)

var defaultMemoryRecallPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)上次|之前|你还记得|我之前(说|提|讲|告诉你)|继续|接着|刚才|前面`),
	regexp.MustCompile(`(?i)那个(事|问题|方案|人)|还是.*那个|和之前一样`),
	regexp.MustCompile(`(?i)last time|as i mentioned|remember when|you said|we discussed|earlier|previously`),
}

var memoryRecallIntentLabels = []memoryRecallIntentLabel{
	{name: "new_topic", text: "用户提出了一个全新的问题或话题", recall: true},
	{name: "continuation", text: "用户在延续当前上下文窗口内的对话内容", recall: false},
	{name: "history_ref", text: "用户需要引用当前窗口之外的历史信息或过往记忆", recall: true},
	{name: "chit_chat", text: "用户在进行日常闲聊或简短确认", recall: false},
}

type memoryRecallIntentLabel struct {
	name   string
	text   string
	recall bool
}

type MemoryRecallDecision struct {
	Should bool
	Reason string
	Query  string
}

type MemoryRecallInput struct {
	Session         *state.Session
	Message         string
	Personalization PersonalizationSettings
}

type MemoryRecallDecider struct {
	config   MemoryRecallConfig
	embedder QueryEmbedder
	logger   *slog.Logger
}

func NewMemoryRecallDecider(config MemoryRecallConfig, embedder QueryEmbedder, logger *slog.Logger) *MemoryRecallDecider {
	config = normalizeMemoryRecallConfig(config)
	return &MemoryRecallDecider{config: config, embedder: embedder, logger: logger}
}

func memoryRecallEmbedderFromConfig(config RuntimeConfig) QueryEmbedder {
	recallConfig := normalizeMemoryRecallConfig(config.MemoryRecall)
	if !recallConfig.Enabled || (!recallConfig.EmbeddingEnabled && !recallConfig.IntentClassifierEnabled) {
		return nil
	}
	vectorConfig := normalizeMemoryVectorConfig(config.MemoryVector)
	embeddingConfig := memoryVectorMessageSearchConfig(vectorConfig, false)
	if !messageEmbeddingConfigured(embeddingConfig) {
		embeddingConfig = normalizeMessageSearchConfig(config.MessageSearch)
	}
	if !messageEmbeddingConfigured(embeddingConfig) {
		return nil
	}
	return NewMessageQueryEmbedder(embeddingConfig)
}

func defaultMemoryRecallConfig() MemoryRecallConfig {
	return MemoryRecallConfig{
		Configured:                   true,
		Enabled:                      true,
		ConditionalEnabled:           true,
		AsyncEnabled:                 true,
		Timeout:                      defaultMemoryRecallTimeout,
		MinQueryRunes:                defaultMemoryRecallMinQueryRunes,
		RecentContextMessages:        defaultMemoryRecallRecentContextMessages,
		RecentContextMaxRunes:        defaultMemoryRecallRecentContextRunes,
		ForceInterval:                defaultMemoryRecallForceInterval,
		ComplexTokenThreshold:        defaultMemoryRecallComplexTokenThreshold,
		EmbeddingEnabled:             true,
		EmbeddingSimilarityThreshold: defaultMemoryRecallEmbeddingThreshold,
		EmbeddingWindow:              defaultMemoryRecallEmbeddingWindow,
		IntentClassifierEnabled:      true,
		IntentClassifierThreshold:    defaultMemoryRecallIntentThreshold,
		IntentClassifierContextTurns: defaultMemoryRecallIntentContextTurns,
	}
}

func normalizeMemoryRecallConfig(config MemoryRecallConfig) MemoryRecallConfig {
	if !config.Configured {
		config = defaultMemoryRecallConfig()
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultMemoryRecallTimeout
	}
	if config.MinQueryRunes <= 0 {
		config.MinQueryRunes = defaultMemoryRecallMinQueryRunes
	}
	if config.RecentContextMessages <= 0 {
		config.RecentContextMessages = defaultMemoryRecallRecentContextMessages
	}
	if config.RecentContextMessages > 8 {
		config.RecentContextMessages = 8
	}
	if config.RecentContextMaxRunes <= 0 {
		config.RecentContextMaxRunes = defaultMemoryRecallRecentContextRunes
	}
	if config.RecentContextMaxRunes > 1200 {
		config.RecentContextMaxRunes = 1200
	}
	if config.ForceInterval < 0 {
		config.ForceInterval = 0
	}
	if config.ComplexTokenThreshold <= 0 {
		config.ComplexTokenThreshold = defaultMemoryRecallComplexTokenThreshold
	}
	if config.EmbeddingSimilarityThreshold <= 0 || config.EmbeddingSimilarityThreshold >= 1 {
		config.EmbeddingSimilarityThreshold = defaultMemoryRecallEmbeddingThreshold
	}
	if config.EmbeddingWindow <= 0 {
		config.EmbeddingWindow = defaultMemoryRecallEmbeddingWindow
	}
	if config.EmbeddingWindow > 8 {
		config.EmbeddingWindow = 8
	}
	if config.IntentClassifierThreshold <= 0 || config.IntentClassifierThreshold >= 1 {
		config.IntentClassifierThreshold = defaultMemoryRecallIntentThreshold
	}
	if config.IntentClassifierContextTurns <= 0 {
		config.IntentClassifierContextTurns = defaultMemoryRecallIntentContextTurns
	}
	if config.IntentClassifierContextTurns > 8 {
		config.IntentClassifierContextTurns = 8
	}
	return config
}

func (d *MemoryRecallDecider) Decide(ctx context.Context, input MemoryRecallInput) MemoryRecallDecision {
	config := normalizeMemoryRecallConfig(d.config)
	message := strings.TrimSpace(input.Message)
	query := buildMemoryRecallQuery(input.Session, message, config)
	if !config.Enabled {
		return MemoryRecallDecision{Reason: memoryRecallReasonDisabled}
	}
	if message == "" {
		return MemoryRecallDecision{Reason: memoryRecallReasonNoMessage}
	}
	if !config.ConditionalEnabled {
		return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonDisabled, Query: query}
	}
	if memoryRecallForceTrigger(input.Session, message, config) {
		return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonForceRule, Query: query}
	}
	if memoryRecallKeywordTrigger(message) {
		return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonKeywordRule, Query: query}
	}
	if memoryRecallEntityTrigger(message, memoryRecallUserEntities(input.Personalization)) {
		return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonEntityRule, Query: query}
	}
	if config.EmbeddingEnabled {
		should, reason, err := d.embeddingTrigger(ctx, input.Session, message, config)
		if err != nil {
			if d.logger != nil {
				d.logger.LogAttrs(ctx, slog.LevelWarn, "memory recall embedding trigger failed", slog.String("error", err.Error()))
			}
			if memoryRecallFallbackTrigger(message, config) {
				return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonEmbeddingUnavailable, Query: query}
			}
			return MemoryRecallDecision{Reason: memoryRecallReasonEmbeddingFallbackNoMatch, Query: query}
		}
		if should {
			return MemoryRecallDecision{Should: true, Reason: reason, Query: query}
		}
		if reason == memoryRecallReasonEmbeddingUnavailable && memoryRecallFallbackTrigger(message, config) {
			return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonEmbeddingUnavailable, Query: query}
		}
	}
	if config.IntentClassifierEnabled {
		should, reason, err := d.intentClassifierTrigger(ctx, input.Session, message, config)
		if err != nil {
			if d.logger != nil {
				d.logger.LogAttrs(ctx, slog.LevelWarn, "memory recall intent classifier failed", slog.String("error", err.Error()))
			}
			if memoryRecallFallbackTrigger(message, config) {
				return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonIntentUnavailable, Query: query}
			}
			return MemoryRecallDecision{Reason: memoryRecallReasonNoRecall, Query: query}
		}
		if should {
			return MemoryRecallDecision{Should: true, Reason: reason, Query: query}
		}
		if reason == memoryRecallReasonIntentUnavailable && memoryRecallFallbackTrigger(message, config) {
			return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonIntentUnavailable, Query: query}
		}
	}
	if !config.EmbeddingEnabled && !config.IntentClassifierEnabled && memoryRecallFallbackTrigger(message, config) {
		return MemoryRecallDecision{Should: true, Reason: memoryRecallReasonEmbeddingUnavailable, Query: query}
	}
	return MemoryRecallDecision{Reason: memoryRecallReasonNoRecall, Query: query}
}

func (d *MemoryRecallDecider) embeddingTrigger(ctx context.Context, session *state.Session, message string, config MemoryRecallConfig) (bool, string, error) {
	if d == nil || d.embedder == nil {
		return false, memoryRecallReasonEmbeddingUnavailable, nil
	}
	recent := recentMemoryRecallTexts(session, message, config)
	if len(recent) == 0 {
		return true, memoryRecallReasonEmbeddingDrift, nil
	}
	current, err := d.embedder.EmbedQuery(ctx, message)
	if err != nil {
		return false, "", err
	}
	if len(current) == 0 {
		return false, "", fmt.Errorf("memory recall embedding trigger received empty current embedding")
	}
	if len(recent) > config.EmbeddingWindow {
		recent = recent[len(recent)-config.EmbeddingWindow:]
	}
	var total float64
	var compared int
	for _, text := range recent {
		vector, err := d.embedder.EmbedQuery(ctx, text)
		if err != nil {
			return false, "", err
		}
		if len(vector) == 0 {
			continue
		}
		total += cosineSimilarityFloat32(current, vector)
		compared++
	}
	if compared == 0 {
		return true, memoryRecallReasonEmbeddingDrift, nil
	}
	avg := total / float64(compared)
	return avg < config.EmbeddingSimilarityThreshold, memoryRecallReasonEmbeddingDrift, nil
}

func (d *MemoryRecallDecider) intentClassifierTrigger(ctx context.Context, session *state.Session, message string, config MemoryRecallConfig) (bool, string, error) {
	if d == nil || d.embedder == nil {
		return false, memoryRecallReasonIntentUnavailable, nil
	}
	input := buildMemoryRecallIntentInput(session, message, config)
	if strings.TrimSpace(input) == "" {
		return false, memoryRecallReasonNoRecall, nil
	}
	inputEmbedding, err := d.embedder.EmbedQuery(ctx, input)
	if err != nil {
		return false, "", err
	}
	if len(inputEmbedding) == 0 {
		return false, "", fmt.Errorf("memory recall intent classifier received empty input embedding")
	}
	bestScore := math.Inf(-1)
	var best memoryRecallIntentLabel
	for _, label := range memoryRecallIntentLabels {
		labelEmbedding, err := d.embedder.EmbedQuery(ctx, label.text)
		if err != nil {
			return false, "", err
		}
		if len(labelEmbedding) == 0 {
			continue
		}
		score := cosineSimilarityFloat32(inputEmbedding, labelEmbedding)
		if score > bestScore {
			bestScore = score
			best = label
		}
	}
	if best.recall && bestScore >= config.IntentClassifierThreshold {
		return true, memoryRecallReasonIntentClassifier, nil
	}
	return false, memoryRecallReasonNoRecall, nil
}

func buildMemoryRecallQuery(session *state.Session, query string, config MemoryRecallConfig) string {
	config = normalizeMemoryRecallConfig(config)
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	contextText := recentMemoryRecallContext(session, query, config)
	if contextText == "" {
		return query
	}
	return "Context: " + tailClipRunes(contextText, config.RecentContextMaxRunes) + "\nQuery: " + query
}

func buildMemoryRecallIntentInput(session *state.Session, query string, config MemoryRecallConfig) string {
	config = normalizeMemoryRecallConfig(config)
	query = strings.TrimSpace(query)
	recentConfig := config
	recentConfig.RecentContextMessages = config.IntentClassifierContextTurns
	recent := recentMemoryRecallTexts(session, query, recentConfig)
	if len(recent) == 0 {
		return "user: " + query
	}
	return strings.Join(recent, "\n") + "\nuser: " + query
}

func recentMemoryRecallContext(session *state.Session, query string, config MemoryRecallConfig) string {
	return strings.Join(recentMemoryRecallTexts(session, query, config), "\n")
}

func recentMemoryRecallTexts(session *state.Session, query string, config MemoryRecallConfig) []string {
	if session == nil || config.RecentContextMessages <= 0 {
		return nil
	}
	query = strings.TrimSpace(query)
	lines := make([]string, 0, config.RecentContextMessages)
	skippedCurrentUser := false
	for i := len(session.Messages) - 1; i >= 0; i-- {
		msg := session.Messages[i]
		if msg.Hidden || (msg.Role != state.MessageRoleUser && msg.Role != state.MessageRoleAssistant) {
			continue
		}
		content := strings.TrimSpace(firstNonEmptyString(msg.Content, msg.ToolOutput))
		if content == "" {
			continue
		}
		if !skippedCurrentUser && msg.Role == state.MessageRoleUser && query != "" && content == query {
			skippedCurrentUser = true
			continue
		}
		lines = append(lines, msg.Role+": "+truncateEpisodeText(content, 220))
		if len(lines) >= config.RecentContextMessages {
			break
		}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

func memoryRecallForceTrigger(session *state.Session, message string, config MemoryRecallConfig) bool {
	turnIndex := memoryRecallTurnIndex(session, message)
	if turnIndex == 0 {
		return true
	}
	if config.ForceInterval > 0 && turnIndex%config.ForceInterval == 0 {
		return true
	}
	return estimateRecallTokenCount(message) > config.ComplexTokenThreshold
}

func memoryRecallTurnIndex(session *state.Session, message string) int {
	if session == nil {
		return 0
	}
	count := 0
	for _, msg := range session.Messages {
		if msg.Hidden || msg.Role != state.MessageRoleUser {
			continue
		}
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}
		count++
	}
	if count == 0 {
		return 0
	}
	return count - 1
}

func memoryRecallKeywordTrigger(message string) bool {
	message = strings.TrimSpace(message)
	for _, pattern := range defaultMemoryRecallPatterns {
		if pattern.MatchString(message) {
			return true
		}
	}
	return false
}

func memoryRecallEntityTrigger(message string, entities []string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" || len(entities) == 0 {
		return false
	}
	for _, entity := range entities {
		entity = strings.ToLower(strings.TrimSpace(entity))
		if entity == "" {
			continue
		}
		if strings.Contains(lower, entity) {
			return true
		}
	}
	return false
}

func memoryRecallUserEntities(settings PersonalizationSettings) []string {
	values := []string{settings.Profile.Nickname, settings.Profile.Occupation}
	values = append(values, splitEntityCandidates(settings.Profile.About)...)
	values = append(values, splitEntityCandidates(settings.CustomInstructions)...)
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if len([]rune(value)) < 2 || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		out = append(out, value)
		if len(out) >= 24 {
			break
		}
	}
	return out
}

func splitEntityCandidates(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，。；;、/|:：()（）[]【】", r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) >= 2 && !memoryRecallWeakToken(field) {
			out = append(out, field)
		}
	}
	return out
}

func memoryRecallFallbackTrigger(message string, config MemoryRecallConfig) bool {
	if memoryRecallKeywordTrigger(message) {
		return true
	}
	if estimateRecallTokenCount(message) > config.ComplexTokenThreshold {
		return true
	}
	if containsCJK(message) {
		return len([]rune(strings.TrimSpace(message))) >= config.MinQueryRunes
	}
	return len(queryTokens(message)) >= 3
}

func memoryRecallWeakToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "hi", "hello", "hey", "ok", "okay", "yes", "no", "thanks", "thank", "you", "好的", "谢谢", "可以", "用户", "默认", "回复":
		return true
	default:
		return false
	}
}

func estimateRecallTokenCount(message string) int {
	fields := strings.Fields(message)
	if len(fields) > 1 {
		return len(fields)
	}
	runes := len([]rune(message))
	if containsCJK(message) {
		return runes / 2
	}
	return runes / 4
}

func cosineSimilarityFloat32(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, normA, normB float64
	for idx := 0; idx < n; idx++ {
		av := float64(a[idx])
		bv := float64(b[idx])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func tailClipRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[len(runes)-maxRunes:])
}

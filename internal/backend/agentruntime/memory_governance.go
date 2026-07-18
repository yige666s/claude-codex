package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

const (
	defaultMemoryConfidence = 0.7
	defaultMemoryWeight     = 0.65
	memoryBM25K1            = 1.2
	memoryBM25B             = 0.75
)

type MemoryExtractionInput struct {
	UserID    string
	SessionID string
	Messages  []state.Message
	Now       time.Time
}

type MemoryExtractor interface {
	Extract(ctx context.Context, input MemoryExtractionInput) ([]MemoryCandidate, error)
}

type MemoryCandidate struct {
	Content     string            `json:"content"`
	Category    string            `json:"category"`
	Tags        []string          `json:"tags,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Source      string            `json:"source,omitempty"`
	SourceRefs  []MemorySourceRef `json:"source_refs,omitempty"`
	Visibility  string            `json:"visibility,omitempty"`
	Confidence  float64           `json:"confidence"`
	Importance  float64           `json:"importance"`
	Reason      string            `json:"reason,omitempty"`
	Sensitivity string            `json:"sensitivity,omitempty"`
	ExpiresHint string            `json:"expires_hint,omitempty"`
	Metadata    map[string]any    `json:"metadata,omitempty"`
}

type MemoryEvaluation struct {
	Accepted bool
	Item     MemoryItem
	Reason   string
}

type RuleMemoryExtractor struct {
	Policy   MemoryPolicy
	Provider MemoryPolicyProvider
}

func NewRuleMemoryExtractor() RuleMemoryExtractor {
	return NewRuleMemoryExtractorWithPolicy(DefaultMemoryPolicy())
}

func NewRuleMemoryExtractorWithPolicy(policy MemoryPolicy) RuleMemoryExtractor {
	return RuleMemoryExtractor{Policy: normalizeMemoryPolicy(policy)}
}

func NewRuleMemoryExtractorWithProvider(provider MemoryPolicyProvider) RuleMemoryExtractor {
	return RuleMemoryExtractor{Provider: provider}
}

func (e RuleMemoryExtractor) MemoryPolicy() MemoryPolicy {
	if e.Provider != nil {
		return e.Provider.MemoryPolicy()
	}
	return normalizeMemoryPolicy(e.Policy)
}

func (e RuleMemoryExtractor) Extract(_ context.Context, input MemoryExtractionInput) ([]MemoryCandidate, error) {
	policy := e.MemoryPolicy()
	userText := lastVisibleUserMessageFromMessages(input.Messages)
	if userText == "" || memoryOptOutRequestedWithPolicy(userText, policy) {
		return nil, nil
	}
	candidates := extractMemoryCandidatesWithPolicy(userText, policy)
	for i := range candidates {
		if candidates[i].Metadata == nil {
			candidates[i].Metadata = map[string]any{}
		}
		candidates[i].Metadata["extractor"] = "rule"
		candidates[i].Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
	}
	return candidates, nil
}

type HybridMemoryExtractor struct {
	Primary  MemoryExtractor
	Fallback MemoryExtractor
}

func NewHybridMemoryExtractor(primary, fallback MemoryExtractor) HybridMemoryExtractor {
	return HybridMemoryExtractor{Primary: primary, Fallback: fallback}
}

func (e HybridMemoryExtractor) Extract(ctx context.Context, input MemoryExtractionInput) ([]MemoryCandidate, error) {
	if e.Primary != nil {
		candidates, err := e.Primary.Extract(ctx, input)
		if len(candidates) > 0 {
			return candidates, err
		}
	}
	if e.Fallback != nil {
		return e.Fallback.Extract(ctx, input)
	}
	return nil, nil
}

type LLMMemoryExtractor struct {
	RunnerFactory  EngineFactory
	Timeout        time.Duration
	MaxAttempts    int
	PromptResolver PromptResolver
}

func NewLLMMemoryExtractor(factory EngineFactory) LLMMemoryExtractor {
	return LLMMemoryExtractor{RunnerFactory: factory, Timeout: 8 * time.Second, MaxAttempts: 2, PromptResolver: NewPromptResolver(nil, nil)}
}

func (e LLMMemoryExtractor) Extract(ctx context.Context, input MemoryExtractionInput) ([]MemoryCandidate, error) {
	if e.RunnerFactory == nil {
		return nil, fmt.Errorf("memory LLM extractor has no runner factory")
	}
	prompt := memoryExtractionPrompt(input)
	promptMeta := PromptMetadata{}
	if resolution, err := e.promptResolver().Resolve(ctx, PromptResolveRequest{PromptID: PromptIDMemoryExtract, UserID: input.UserID, SessionID: input.SessionID, RuntimeMode: "memory"}); err == nil {
		if rendered, err := RenderPrompt(resolution, memoryExtractionVariables(input)); err == nil {
			prompt = rendered.Content
			promptMeta = PromptMetadataFromRender(rendered)
		}
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if promptMeta.PromptID != "" {
		callCtx = WithPromptMetadata(callCtx, promptMeta)
	}
	runner := e.RunnerFactory(Scope{UserID: input.UserID, SessionID: input.SessionID})
	attempts := e.MaxAttempts
	if attempts <= 0 {
		attempts = 2
	}
	if attempts > 3 {
		attempts = 3
	}
	var lastOutput string
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptPrompt := prompt
		if attempt > 0 && strings.TrimSpace(lastOutput) != "" {
			attemptPrompt = memoryExtractionRepairPrompt(lastOutput, lastErr, input)
		}
		result, err := runner.RunGeneratedPrompt(callCtx, state.NewSession(""), attemptPrompt)
		if err != nil {
			lastErr = err
			continue
		}
		lastOutput = result.Output
		candidates, err := parseLLMMemoryCandidates(result.Output)
		if err != nil {
			lastErr = err
			continue
		}
		markLLMMemoryCandidates(candidates, attempt)
		return candidates, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func (e LLMMemoryExtractor) promptResolver() PromptResolver {
	if e.PromptResolver.Store != nil || len(e.PromptResolver.Fallbacks) > 0 {
		return e.PromptResolver
	}
	return NewPromptResolver(nil, nil)
}

type MemoryAbstractor interface {
	Build(ctx context.Context, userID string, items []MemoryItem, now time.Time) ([]MemoryItem, error)
}

type RuleMemoryAbstractor struct{}

func NewRuleMemoryAbstractor() RuleMemoryAbstractor {
	return RuleMemoryAbstractor{}
}

func (RuleMemoryAbstractor) Build(_ context.Context, userID string, items []MemoryItem, now time.Time) ([]MemoryItem, error) {
	type groupKey struct {
		namespace string
		category  string
	}
	byCategory := map[groupKey][]MemoryItem{}
	for _, item := range items {
		item = normalizeMemoryItem(item)
		if item.UserID != userID || item.Status != MemoryStatusActive || item.Level != MemoryLevelAtomic {
			continue
		}
		byCategory[groupKey{namespace: item.Namespace, category: item.Category}] = append(byCategory[groupKey{namespace: item.Namespace, category: item.Category}], item)
	}
	var abstracts []MemoryItem
	conceptLinesByNamespace := map[string][]string{}
	for key, group := range byCategory {
		if len(group) < 2 {
			continue
		}
		sortMemoryItems(group)
		related := make([]string, 0, len(group))
		parts := make([]string, 0, minInt(len(group), 5))
		for i, item := range group {
			related = append(related, item.ID)
			if i < 5 {
				parts = append(parts, strings.TrimSpace(item.Content))
			}
		}
		content := fmt.Sprintf("User %s summary: %s.", key.category, strings.Join(parts, "; "))
		concept := newConversationMemoryItem(userID, "", content)
		concept.Namespace = key.namespace
		concept.Level = MemoryLevelConcept
		concept.Category = key.category
		concept.Source = MemorySourceSystem
		concept.Tags = []string{"concept", key.category}
		concept.RelatedIDs = related
		concept.RawHash = memoryRawHash(MemoryCategoryFact, MemoryLevelConcept+":"+key.namespace+":"+key.category)
		concept.Confidence = averageMemoryConfidence(group)
		concept.Weight = computeMemoryWeight(key.category, 0.85, concept.Confidence, now, int64(len(group)))
		concept.CreatedAt = now
		concept.UpdatedAt = now
		concept.Metadata = map[string]any{
			"abstractor":    "rule",
			"atomic_count":  len(group),
			"dirty":         false,
			"source_level":  MemoryLevelAtomic,
			"summary_scope": key.category,
		}
		abstracts = append(abstracts, concept)
		conceptLinesByNamespace[key.namespace] = append(conceptLinesByNamespace[key.namespace], content)
	}
	for namespace, conceptLines := range conceptLinesByNamespace {
		if len(conceptLines) < 2 {
			continue
		}
		sort.Strings(conceptLines)
		content := "User memory profile: " + strings.Join(conceptLines, " ")
		profile := newConversationMemoryItem(userID, "", content)
		profile.Namespace = namespace
		profile.Level = MemoryLevelProfile
		profile.Category = MemoryCategoryFact
		profile.Source = MemorySourceSystem
		profile.Tags = []string{"profile"}
		profile.RawHash = memoryRawHash(MemoryCategoryFact, MemoryLevelProfile+":"+namespace+":user")
		profile.Confidence = defaultMemoryConfidence
		profile.Weight = computeMemoryWeight(profile.Category, 0.9, profile.Confidence, now, int64(len(conceptLines)))
		profile.CreatedAt = now
		profile.UpdatedAt = now
		profile.Metadata = map[string]any{
			"abstractor":    "rule",
			"concept_count": len(conceptLines),
			"dirty":         false,
			"source_level":  MemoryLevelConcept,
			"summary_scope": "profile",
		}
		abstracts = append(abstracts, profile)
	}
	return abstracts, nil
}

func extractMemoryItems(userID string, session *state.Session) []MemoryItem {
	if strings.TrimSpace(userID) == "" || session == nil || session.ID == "" {
		return nil
	}
	candidates, _ := NewRuleMemoryExtractor().Extract(context.Background(), MemoryExtractionInput{UserID: userID, SessionID: session.ID, Messages: session.Messages, Now: time.Now().UTC()})
	return evaluateMemoryCandidates(userID, session.ID, candidates)
}

func evaluateMemoryCandidates(userID, sessionID string, candidates []MemoryCandidate) []MemoryItem {
	return evaluateMemoryCandidatesWithPolicy(userID, sessionID, candidates, DefaultMemoryPolicy())
}

func evaluateMemoryCandidatesWithPolicy(userID, sessionID string, candidates []MemoryCandidate, policy MemoryPolicy) []MemoryItem {
	if len(candidates) == 0 {
		return nil
	}
	items := make([]MemoryItem, 0, len(candidates))
	for _, candidate := range candidates {
		evaluation := evaluateMemoryCandidateWithPolicy(userID, sessionID, candidate, policy)
		if !evaluation.Accepted {
			continue
		}
		items = append(items, evaluation.Item)
	}
	return items
}

func lastVisibleUserMessage(session *state.Session) string {
	if session == nil {
		return ""
	}
	return lastVisibleUserMessageFromMessages(session.Messages)
}

func lastVisibleUserMessageFromMessages(messages []state.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role == "user" && !msg.Hidden {
			return strings.TrimSpace(msg.Content)
		}
	}
	return ""
}

func extractMemoryCandidates(text string) []MemoryCandidate {
	return extractMemoryCandidatesWithPolicy(text, DefaultMemoryPolicy())
}

func extractMemoryCandidatesWithPolicy(text string, policy MemoryPolicy) []MemoryCandidate {
	policy = normalizeMemoryPolicy(policy)
	text = strings.TrimSpace(text)
	var candidates []MemoryCandidate
	for _, rule := range policy.Extraction.Rules {
		pattern, ok := compileMemoryPolicyPattern(rule.Pattern)
		if !ok {
			continue
		}
		content := firstMatch(pattern, text)
		if content == "" {
			continue
		}
		candidate, ok := memoryCandidateFromPolicyRule(text, content, rule, policy)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return dedupeMemoryCandidates(candidates)
}

func memoryCandidateFromPolicyRule(original, extracted string, rule MemoryExtractionRule, policy MemoryPolicy) (MemoryCandidate, bool) {
	kind := strings.ToLower(strings.TrimSpace(rule.Kind))
	candidate := MemoryCandidate{
		Tags:       normalizeMemoryTags(rule.Tags),
		Confidence: memoryPolicyRuleConfidence(rule),
		Importance: memoryPolicyRuleImportance(rule),
		Reason:     strings.TrimSpace(rule.Reason),
		Metadata: map[string]any{
			"extraction_rule_id":    strings.TrimSpace(rule.ID),
			"memory_policy_version": memoryPolicyVersion(policy),
		},
	}
	switch kind {
	case "explicit":
		candidate.Content = extracted
		candidate.Category = firstNonEmptyString(rule.Category, inferMemoryCategoryWithPolicy(extracted, policy))
		if len(candidate.Tags) == 0 {
			candidate.Tags = []string{"explicit"}
		}
		if candidate.Reason == "" {
			candidate.Reason = "explicit_memory_request"
		}
	case "preference":
		candidate.Content = preferenceMemoryContentWithPolicy(original, extracted, policy)
		candidate.Category = firstNonEmptyString(rule.Category, MemoryCategoryPreference)
		if len(candidate.Tags) == 0 {
			candidate.Tags = []string{"preference"}
		}
		if candidate.Reason == "" {
			candidate.Reason = "preference_pattern"
		}
	case "fact":
		candidate.Content = factMemoryContentWithPolicy(original, extracted, policy)
		candidate.Category = firstNonEmptyString(rule.Category, MemoryCategoryFact)
		if len(candidate.Tags) == 0 {
			candidate.Tags = []string{"fact"}
		}
		if candidate.Reason == "" {
			candidate.Reason = "fact_pattern"
		}
	default:
		return MemoryCandidate{}, false
	}
	return candidate, true
}

func memoryPolicyRuleConfidence(rule MemoryExtractionRule) float64 {
	if rule.Confidence > 0 {
		return rule.Confidence
	}
	switch strings.ToLower(strings.TrimSpace(rule.Kind)) {
	case "explicit":
		return 0.9
	case "preference":
		return 0.78
	case "fact":
		return 0.7
	default:
		return defaultMemoryConfidence
	}
}

func memoryPolicyRuleImportance(rule MemoryExtractionRule) float64 {
	if rule.Importance > 0 {
		return rule.Importance
	}
	switch strings.ToLower(strings.TrimSpace(rule.Kind)) {
	case "explicit":
		return 0.9
	case "preference":
		return 0.65
	case "fact":
		return 0.7
	default:
		return defaultMemoryWeight
	}
}

func memoryExtractionPrompt(input MemoryExtractionInput) string {
	return renderPromptContent(memoryExtractionPromptTemplate(), memoryExtractionVariables(input))
}

func memoryExtractionVariables(input MemoryExtractionInput) map[string]any {
	messages := recentVisibleConversation(input.Messages, 8)
	payload, _ := json.MarshalIndent(messages, "", "  ")
	return map[string]any{"conversation_json": string(payload)}
}

func memoryExtractionPromptTemplate() string {
	return PromptMemoryExtractionTemplate
}

func memoryExtractionRepairPrompt(output string, parseErr error, input MemoryExtractionInput) string {
	messages := recentVisibleConversation(input.Messages, 8)
	payload, _ := json.MarshalIndent(messages, "", "  ")
	output = truncateMemoryContent(output)
	errText := ""
	if parseErr != nil {
		errText = parseErr.Error()
	}
	return fmt.Sprintf(PromptMemoryExtractionRepairTemplate, errText, output, string(payload))
}

func recentVisibleConversation(messages []state.Message, limit int) []map[string]string {
	if limit <= 0 {
		limit = 8
	}
	start := 0
	if len(messages) > limit {
		start = len(messages) - limit
	}
	out := make([]map[string]string, 0, len(messages)-start)
	for _, msg := range messages[start:] {
		if msg.Hidden || (msg.Role != "user" && msg.Role != "assistant") {
			continue
		}
		row := map[string]string{
			"role":    msg.Role,
			"content": truncateForMemory(msg.Content),
		}
		if attachments := episodeAttachmentSummary([]state.Message{msg}); attachments != "" {
			row["attachments"] = truncateForMemory(attachments)
		}
		out = append(out, row)
	}
	return out
}

func parseLLMMemoryCandidates(output string) ([]MemoryCandidate, error) {
	output, err := normalizeLLMJSONOutput(output)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Memories []MemoryCandidate `json:"memories"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err == nil {
		return dedupeMemoryCandidates(payload.Memories), nil
	}
	var memories []MemoryCandidate
	if err := json.Unmarshal([]byte(output), &memories); err != nil {
		return nil, err
	}
	return dedupeMemoryCandidates(memories), nil
}

func normalizeLLMJSONOutput(output string) (string, error) {
	output = strings.TrimSpace(output)
	output = strings.TrimPrefix(output, "\ufeff")
	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("empty memory extraction output")
	}
	if strings.HasPrefix(output, "```") {
		if fenced := extractFencedBlock(output); fenced != "" {
			output = fenced
		}
	}
	output = strings.TrimSpace(output)
	if json.Valid([]byte(output)) {
		return output, nil
	}
	if extracted := extractFirstJSONValue(output); extracted != "" && json.Valid([]byte(extracted)) {
		return extracted, nil
	}
	return "", fmt.Errorf("memory extraction output is not valid JSON")
}

func extractFencedBlock(output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return ""
	}
	end := len(lines)
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			end = i
			break
		}
	}
	if end <= 1 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[1:end], "\n"))
}

func extractFirstJSONValue(output string) string {
	objectStart := strings.IndexRune(output, '{')
	arrayStart := strings.IndexRune(output, '[')
	switch {
	case objectStart < 0 && arrayStart < 0:
		return ""
	case arrayStart >= 0 && (objectStart < 0 || arrayStart < objectStart):
		return extractBalancedJSON(output, '[', ']')
	default:
		return extractBalancedJSON(output, '{', '}')
	}
}

func extractBalancedJSON(output string, open, close rune) string {
	start := strings.IndexRune(output, open)
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i, r := range output[start:] {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch r {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return strings.TrimSpace(output[start : start+i+len(string(r))])
			}
		}
	}
	return ""
}

func markLLMMemoryCandidates(candidates []MemoryCandidate, repairAttempt int) {
	for i := range candidates {
		if candidates[i].Metadata == nil {
			candidates[i].Metadata = map[string]any{}
		}
		candidates[i].Metadata["extractor"] = "llm"
		if repairAttempt > 0 {
			candidates[i].Metadata["extractor_repair_attempt"] = repairAttempt
		}
	}
}

func evaluateMemoryCandidate(userID, sessionID string, candidate MemoryCandidate) MemoryEvaluation {
	return evaluateMemoryCandidateWithPolicy(userID, sessionID, candidate, DefaultMemoryPolicy())
}

func evaluateMemoryCandidateWithPolicy(userID, sessionID string, candidate MemoryCandidate, policy MemoryPolicy) MemoryEvaluation {
	policy = normalizeMemoryPolicy(policy)
	candidate.Content = strings.TrimSpace(candidate.Content)
	if candidate.Content == "" {
		return MemoryEvaluation{Reason: "empty"}
	}
	if candidate.Confidence < policy.Extraction.MinConfidence {
		return MemoryEvaluation{Reason: "low_confidence"}
	}
	sensitivity := strings.ToLower(strings.TrimSpace(candidate.Sensitivity))
	if sensitivity == "secret" || sensitivity == "unsafe" || hasSecretMemoryWithPolicy(candidate.Content, policy) || hasPromptInjectionMemoryWithPolicy(candidate.Content, policy) {
		return MemoryEvaluation{Reason: "blocked_sensitive"}
	}
	content, metadata := sanitizeMemoryContentWithPolicy(candidate.Content, policy)
	content = truncateMemoryContentWithPolicy(content, policy)
	if content == "" || isWeakMemoryContentWithPolicy(content, policy) {
		return MemoryEvaluation{Reason: "weak_content"}
	}
	item := newConversationMemoryItem(userID, sessionID, content)
	if candidate.Namespace != "" {
		item.Namespace = normalizeMemoryNamespace(candidate.Namespace)
	}
	item.Category = normalizeMemoryCategory(candidate.Category)
	item.Tags = normalizeMemoryTags(candidate.Tags)
	if candidate.Source != "" {
		item.Source = normalizeMemorySource(candidate.Source)
	}
	item.SourceRefs = normalizeMemorySourceRefs(candidate.SourceRefs)
	if candidate.Visibility != "" {
		item.Visibility = normalizeMemoryVisibility(candidate.Visibility)
	}
	item.Confidence = clamp01(candidate.Confidence)
	item.Weight = computeMemoryWeight(item.Category, candidate.Importance, item.Confidence, item.UpdatedAt, item.AccessCount)
	// Bug 2 fix: 删除此处的 item.RawHash = memoryRawHash(...) 赋值。
	// normalizeMemoryItem 会在 upsertMemoryItem / 持久化前统一重算，
	// 保证 sanitize 后的 content 与 hash 严格对应。
	item.Metadata = metadata
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	if candidate.Metadata != nil {
		for key, value := range candidate.Metadata {
			item.Metadata[key] = value
		}
	}
	item.Metadata["reason"] = candidate.Reason
	if sensitivity != "" {
		item.Metadata["sensitivity"] = sensitivity
	}
	item.Metadata["security_filter_version"] = policy.Safety.Version
	item.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
	item.ExpiresAt = expiresAtFromHint(candidate.ExpiresHint, item.CreatedAt)
	return MemoryEvaluation{Accepted: true, Item: item}
}

func firstMatch(pattern *regexp.Regexp, text string) string {
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return cleanExtractedMemory(match[1])
}

func cleanExtractedMemory(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, " \t\r\n。.!?!?")
	return value
}

func preferenceMemoryContent(original, extracted string) string {
	return preferenceMemoryContentWithPolicy(original, extracted, DefaultMemoryPolicy())
}

func preferenceMemoryContentWithPolicy(original, extracted string, policy MemoryPolicy) string {
	original = strings.TrimSpace(original)
	if strings.Contains(original, extracted) && len([]rune(original)) <= normalizeMemoryPolicy(policy).Extraction.InlineOriginalMaxRunes {
		return cleanExtractedMemory(original)
	}
	return "User preference: " + extracted
}

func factMemoryContent(original, extracted string) string {
	return factMemoryContentWithPolicy(original, extracted, DefaultMemoryPolicy())
}

func factMemoryContentWithPolicy(original, extracted string, policy MemoryPolicy) string {
	policy = normalizeMemoryPolicy(policy)
	original = strings.TrimSpace(original)
	if strings.Contains(original, extracted) && len([]rune(original)) <= policy.Extraction.InlineOriginalMaxRunes && !isWeakMemoryContentWithPolicy(original, policy) {
		return cleanExtractedMemory(original)
	}
	return "User fact: " + extracted
}

func inferMemoryCategory(content string) string {
	return inferMemoryCategoryWithPolicy(content, DefaultMemoryPolicy())
}

func inferMemoryCategoryWithPolicy(content string, policy MemoryPolicy) string {
	content = strings.TrimSpace(content)
	lower := strings.ToLower(content)
	for _, hint := range normalizeMemoryPolicy(policy).Extraction.CategoryHints {
		for _, keyword := range hint.Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" {
				continue
			}
			if strings.Contains(lower, strings.ToLower(keyword)) || strings.Contains(content, keyword) {
				return normalizeMemoryCategory(hint.Category)
			}
		}
	}
	return MemoryCategoryFact
}

func dedupeMemoryCandidates(candidates []MemoryCandidate) []MemoryCandidate {
	seen := map[string]bool{}
	out := make([]MemoryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := memoryRawHash(candidate.Category, candidate.Content)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

func sanitizeMemoryContent(content string) (string, map[string]any) {
	return sanitizeMemoryContentWithPolicy(content, DefaultMemoryPolicy())
}

func sanitizeMemoryContentWithPolicy(content string, policy MemoryPolicy) (string, map[string]any) {
	policy = normalizeMemoryPolicy(policy)
	metadata := map[string]any{}
	content = strings.TrimSpace(content)
	var hits []string
	for _, rule := range policy.Safety.PIIRules {
		pattern, ok := compileMemoryPolicyPattern(rule.Pattern)
		if !ok {
			continue
		}
		name := strings.TrimSpace(rule.Name)
		if pattern.MatchString(content) {
			hits = append(hits, name)
			content = pattern.ReplaceAllString(content, "["+strings.ToUpper(name)+"_REDACTED]")
		}
	}
	if len(hits) > 0 {
		metadata["pii_redacted"] = hits
	}
	return content, metadata
}

func hasSecretMemory(content string) bool {
	return hasSecretMemoryWithPolicy(content, DefaultMemoryPolicy())
}

func hasSecretMemoryWithPolicy(content string, policy MemoryPolicy) bool {
	for _, rule := range normalizeMemoryPolicy(policy).Safety.PIIRules {
		pattern, ok := compileMemoryPolicyPattern(rule.Pattern)
		if !ok {
			continue
		}
		if strings.TrimSpace(rule.Name) == "secret" && pattern.MatchString(content) {
			return true
		}
	}
	return false
}

func hasPromptInjectionMemory(content string) bool {
	return hasPromptInjectionMemoryWithPolicy(content, DefaultMemoryPolicy())
}

func hasPromptInjectionMemoryWithPolicy(content string, policy MemoryPolicy) bool {
	lower := strings.ToLower(content)
	for _, phrase := range normalizeMemoryPolicy(policy).Safety.PromptInjectionPhrases {
		phrase = strings.TrimSpace(phrase)
		if phrase != "" && strings.Contains(lower, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

func memoryOptOutRequested(content string) bool {
	return memoryOptOutRequestedWithPolicy(content, DefaultMemoryPolicy())
}

func memoryOptOutRequestedWithPolicy(content string, policy MemoryPolicy) bool {
	lower := strings.ToLower(content)
	for _, phrase := range normalizeMemoryPolicy(policy).Safety.OptOutPhrases {
		phrase = strings.TrimSpace(phrase)
		if phrase != "" && strings.Contains(lower, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

func isWeakMemoryContent(content string) bool {
	return isWeakMemoryContentWithPolicy(content, DefaultMemoryPolicy())
}

func isWeakMemoryContentWithPolicy(content string, policy MemoryPolicy) bool {
	weakPolicy := normalizeMemoryPolicy(policy).Extraction.WeakContent
	words := strings.Fields(content)
	runes := len([]rune(content))
	if runes < weakPolicy.MinRunes || (len(words) < weakPolicy.MinNonCJKWords && !containsCJK(content)) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(content))
	for _, value := range weakPolicy.ExactPhrases {
		if lower == strings.ToLower(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func containsCJK(content string) bool {
	for _, r := range content {
		if (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf') {
			return true
		}
	}
	return false
}

func expiresAtFromHint(hint string, now time.Time) *time.Time {
	hint = strings.ToLower(strings.TrimSpace(hint))
	if hint == "" || hint == "never" || hint == "none" {
		return nil
	}
	var expires time.Time
	switch hint {
	case "day", "1d", "tomorrow":
		expires = now.Add(24 * time.Hour)
	case "week", "7d":
		expires = now.Add(7 * 24 * time.Hour)
	case "month", "30d":
		expires = now.Add(30 * 24 * time.Hour)
	default:
		if parsed, err := time.Parse(time.RFC3339, hint); err == nil {
			expires = parsed
		}
	}
	if expires.IsZero() {
		return nil
	}
	return &expires
}

func truncateMemoryContent(value string) string {
	return truncateMemoryContentWithPolicy(value, DefaultMemoryPolicy())
}

func truncateMemoryContentWithPolicy(value string, policy MemoryPolicy) string {
	value = strings.TrimSpace(value)
	maxRunes := normalizeMemoryPolicy(policy).Extraction.MaxContentRunes
	if len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func normalizeMemoryItem(item MemoryItem) MemoryItem {
	now := time.Now().UTC()
	item.UserID = strings.TrimSpace(item.UserID)
	item.SessionID = strings.TrimSpace(item.SessionID)
	item.Namespace = normalizeMemoryNamespace(item.Namespace)
	item.Kind = strings.TrimSpace(item.Kind)
	if item.Kind == "" {
		item.Kind = MemoryKindSession
	}
	item.Level = normalizeMemoryLevel(item.Level)
	item.Category = normalizeMemoryCategory(item.Category)
	item.Tags = normalizeMemoryTags(item.Tags)
	item.Source = normalizeMemorySource(item.Source)
	item.SourceRefs = normalizeMemorySourceRefs(item.SourceRefs)
	item.Visibility = normalizeMemoryVisibility(item.Visibility)
	item.Status = strings.TrimSpace(item.Status)
	if item.Status == "" {
		item.Status = MemoryStatusActive
	}
	item.Content = truncateMemoryContent(strings.TrimSpace(item.Content))

	// Bug 2 fix: 始终基于 normalize 后的 content 重算，不信任存储里的旧值，
	// 确保所有写入路径（evaluate、upsert、abstractor）hash 来源一致。
	item.RawHash = memoryRawHash(item.Category, item.Content)

	item.Confidence = clamp01(item.Confidence)
	if item.Confidence == 0 {
		item.Confidence = defaultMemoryConfidence
	}
	item.Weight = clamp01(item.Weight)
	if item.Weight == 0 {
		item.Weight = computeMemoryWeight(item.Category, defaultCategoryImportance(item.Category), item.Confidence, item.UpdatedAt, item.AccessCount)
	}
	item.ParentID = strings.TrimSpace(item.ParentID)
	item.RelatedIDs = normalizeMemoryIDs(item.RelatedIDs)
	item.ConflictIDs = normalizeMemoryIDs(item.ConflictIDs)
	item.SupersedesID = strings.TrimSpace(item.SupersedesID)
	item.SupersededByID = strings.TrimSpace(item.SupersededByID)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = item.CreatedAt
	}
	return item
}

func normalizeMemoryNamespace(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return MemoryNamespaceDefault
	}
	value = strings.ToLower(value)
	value = regexp.MustCompile(`[^a-z0-9_.:-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return MemoryNamespaceDefault
	}
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func normalizeMemoryLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MemoryLevelConcept:
		return MemoryLevelConcept
	case MemoryLevelProfile:
		return MemoryLevelProfile
	default:
		return MemoryLevelAtomic
	}
}

func normalizeMemoryCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MemoryCategoryPreference:
		return MemoryCategoryPreference
	case MemoryCategoryEvent:
		return MemoryCategoryEvent
	case MemoryCategorySkill:
		return MemoryCategorySkill
	default:
		return MemoryCategoryFact
	}
}

func normalizeMemorySource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MemorySourceAttachment:
		return MemorySourceAttachment
	case MemorySourceArtifact:
		return MemorySourceArtifact
	case MemorySourceVision:
		return MemorySourceVision
	case MemorySourceBrowser:
		return MemorySourceBrowser
	case MemorySourceUserEdit:
		return MemorySourceUserEdit
	case MemorySourceSystem:
		return MemorySourceSystem
	default:
		return MemorySourceConversation
	}
}

func normalizeMemoryVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MemoryVisibilityPrivate:
		return MemoryVisibilityPrivate
	case MemoryVisibilitySession:
		return MemoryVisibilitySession
	case MemoryVisibilityShared:
		return MemoryVisibilityShared
	case MemoryVisibilityUser:
		return MemoryVisibilityUser
	default:
		return MemoryVisibilityUser
	}
}

func normalizeMemoryTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func normalizeMemorySourceRefs(refs []MemorySourceRef) []MemorySourceRef {
	seen := map[string]bool{}
	out := make([]MemorySourceRef, 0, len(refs))
	for _, ref := range refs {
		ref.Kind = normalizeMemorySourceRefKind(ref.Kind)
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Filename = strings.TrimSpace(ref.Filename)
		ref.ContentType = strings.TrimSpace(ref.ContentType)
		ref.SessionID = strings.TrimSpace(ref.SessionID)
		ref.JobID = strings.TrimSpace(ref.JobID)
		ref.URI = strings.TrimSpace(ref.URI)
		if ref.ID == "" && ref.URI == "" {
			continue
		}
		key := ref.Kind + "\x00" + ref.ID + "\x00" + ref.URI
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			if out[i].ID == out[j].ID {
				return out[i].URI < out[j].URI
			}
			return out[i].ID < out[j].ID
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func normalizeMemorySourceRefKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case MemoryEpisodeSourceSession:
		return MemoryEpisodeSourceSession
	case MemoryEpisodeSourceJob:
		return MemoryEpisodeSourceJob
	case MemorySourceBrowser:
		return MemorySourceBrowser
	case "message":
		return "message"
	case "episode":
		return "episode"
	default:
		return normalizeAssetKind(kind)
	}
}

func normalizeMemoryIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func memoryRawHash(category, content string) string {
	content = strings.ToLower(strings.Join(strings.Fields(content), " "))
	category = normalizeMemoryCategory(category)
	if content == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(category + "\x00" + content))
	return hex.EncodeToString(sum[:])
}

func computeMemoryWeight(category string, importance, confidence float64, updatedAt time.Time, accessCount int64) float64 {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	daysSince := time.Since(updatedAt).Hours() / 24
	if daysSince < 0 {
		daysSince = 0
	}
	recency := math.Exp(-0.1 * daysSince)
	frequency := math.Log1p(float64(accessCount)) / 10
	score := 0.30*recency + 0.35*clamp01(importance) + 0.20*clamp01(frequency) + 0.15*clamp01(confidence)
	return clamp01(score)
}

func selectMemoryItemsForContext(items []MemoryItem, query string, limit int) []MemoryItem {
	return selectMemoryItemsForSessionContext(items, query, "", limit)
}

func selectMemoryItemsForSessionContext(items []MemoryItem, query, sessionID string, limit int) []MemoryItem {
	candidates := make([]MemoryItem, 0, len(items))
	for _, item := range items {
		item = normalizeMemoryItem(item)
		if item.Status != MemoryStatusActive || strings.TrimSpace(item.Content) == "" {
			continue
		}
		if isManagedPersonalizationMemory(item) {
			continue
		}
		if !memoryVisibleInSession(item, sessionID) {
			continue
		}
		candidates = append(candidates, item)
	}
	bm25Scores := memoryBM25ItemScores(query, candidates)
	for i := range candidates {
		relevance := 0.0
		if i < len(bm25Scores) {
			relevance = bm25Scores[i]
		}
		candidates[i].Weight = memoryContextScoreWithRelevance(candidates[i], relevance)
	}
	sortMemoryItems(candidates)
	return limitMemoryItems(candidates, limit)
}

func memoryVisibleInSession(item MemoryItem, sessionID string) bool {
	switch normalizeMemoryVisibility(item.Visibility) {
	case MemoryVisibilitySession:
		return strings.TrimSpace(sessionID) != "" && item.SessionID == strings.TrimSpace(sessionID)
	default:
		return true
	}
}

func memoryItemHasSourceRef(item MemoryItem, sourceKind, sourceID string) bool {
	sourceKind = strings.TrimSpace(sourceKind)
	sourceID = strings.TrimSpace(sourceID)
	if sourceKind != "" {
		sourceKind = normalizeMemorySourceRefKind(sourceKind)
	}
	for _, ref := range item.SourceRefs {
		if sourceKind != "" && normalizeMemorySourceRefKind(ref.Kind) != sourceKind {
			continue
		}
		if sourceID != "" && strings.TrimSpace(ref.ID) != sourceID {
			continue
		}
		return true
	}
	return false
}

func memoryContextScore(item MemoryItem, query string) float64 {
	return memoryContextScoreWithRelevance(item, memoryTextRelevance(query, item.Content+" "+strings.Join(item.Tags, " ")))
}

func memoryContextScoreWithRelevance(item MemoryItem, relevance float64) float64 {
	item = scoreMemoryQuality(item, nil, time.Now().UTC())
	base := computeMemoryWeight(item.Category, defaultCategoryImportance(item.Category), item.Confidence, item.UpdatedAt, item.AccessCount)
	quality := metadataFloat(item.Metadata, "quality_score", 0.65)
	feedback := memoryFeedbackScore(item)
	levelBoost := 0.0
	switch normalizeMemoryLevel(item.Level) {
	case MemoryLevelProfile:
		levelBoost = 0.08
	case MemoryLevelConcept:
		levelBoost = 0.12
	}
	sourceBoost := 0.0
	if item.Source == MemorySourceUserEdit {
		sourceBoost = 0.08
	}
	return clamp01(0.55*base + 0.22*relevance + 0.13*quality + 0.10*feedback + levelBoost + sourceBoost)
}

func scoreMemoryQuality(item MemoryItem, all []MemoryItem, now time.Time) MemoryItem {
	item = normalizeMemoryItem(item)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	daysSinceUpdate := now.Sub(item.UpdatedAt).Hours() / 24
	if daysSinceUpdate < 0 {
		daysSinceUpdate = 0
	}
	staleness := clamp01(daysSinceUpdate / 180)
	usage := clamp01(math.Log1p(float64(item.AccessCount)) / 5)
	if item.LastInjectedAt != nil {
		daysSinceInject := now.Sub(*item.LastInjectedAt).Hours() / 24
		if daysSinceInject < 30 {
			usage = clamp01(usage + 0.20)
		}
	}
	feedback := memoryFeedbackScore(item)
	conflictPenalty := 0.0
	if item.Status == MemoryStatusPendingConfirm || item.Status == MemoryStatusConflicted || len(item.ConflictIDs) > 0 {
		conflictPenalty = 0.25
	}
	if item.Status == MemoryStatusArchived || item.Status == MemoryStatusDeleted {
		conflictPenalty += 0.20
	}
	redundancy := memoryRedundancyScore(item, all)
	quality := clamp01(0.30*item.Confidence + 0.25*usage + 0.20*feedback + 0.15*(1-staleness) + 0.10*(1-redundancy) - conflictPenalty)
	reasons := []string{}
	if usage > 0.5 {
		reasons = append(reasons, "frequently_injected")
	}
	if feedback > 0.75 {
		reasons = append(reasons, "positive_feedback")
	}
	if staleness > 0.7 {
		reasons = append(reasons, "stale")
	}
	if redundancy > 0.6 {
		reasons = append(reasons, "possibly_redundant")
	}
	if conflictPenalty > 0 {
		reasons = append(reasons, "conflict_or_archived")
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	item.Metadata["quality_score"] = quality
	item.Metadata["staleness_score"] = staleness
	item.Metadata["redundancy_score"] = redundancy
	item.Metadata["usage_score"] = usage
	item.Metadata["feedback_score"] = feedback
	item.Metadata["quality_reasons"] = reasons
	return item
}

func memoryFeedbackScore(item MemoryItem) float64 {
	if item.Metadata == nil {
		return 0.5
	}
	switch strings.TrimSpace(fmt.Sprint(item.Metadata["feedback"])) {
	case "important":
		return 1
	case "incorrect":
		return 0
	case "not_relevant":
		return 0.25
	default:
		return 0.5
	}
}

func memoryRedundancyScore(item MemoryItem, all []MemoryItem) float64 {
	if len(all) == 0 {
		return 0
	}
	highest := 0.0
	for _, other := range all {
		other = normalizeMemoryItem(other)
		if other.ID == item.ID || other.Namespace != item.Namespace || other.Category != item.Category || other.Status == MemoryStatusDeleted {
			continue
		}
		highest = math.Max(highest, memoryTextRelevance(item.Content, other.Content))
	}
	return clamp01(highest)
}

func metadataFloat(metadata map[string]any, key string, fallback float64) float64 {
	if metadata == nil {
		return fallback
	}
	switch value := metadata[key].(type) {
	case float64:
		return clamp01(value)
	case float32:
		return clamp01(float64(value))
	case int:
		return clamp01(float64(value))
	case json.Number:
		parsed, err := value.Float64()
		if err == nil {
			return clamp01(parsed)
		}
	}
	return fallback
}

func memoryTextRelevance(query, content string) float64 {
	queryTokens := memoryTokens(query)
	if len(queryTokens) == 0 {
		return 0
	}
	contentTokens := memoryTokens(content)
	if len(contentTokens) == 0 {
		return 0
	}
	matched := 0
	for token := range queryTokens {
		if contentTokens[token] {
			matched++
		}
	}
	return clamp01(float64(matched) / math.Sqrt(float64(len(queryTokens)*len(contentTokens))))
}

type memoryConflictSlot struct {
	Name   string
	Value  string
	RuleID string
}

func memoryBM25ItemScores(query string, items []MemoryItem) []float64 {
	documents := make([]string, 0, len(items))
	for _, item := range items {
		item = normalizeMemoryItem(item)
		documents = append(documents, strings.TrimSpace(item.Content+" "+strings.Join(item.Tags, " ")))
	}
	return memoryBM25Scores(query, documents)
}

func memoryBM25Scores(query string, documents []string) []float64 {
	scores := make([]float64, len(documents))
	queryTerms := uniqueMemoryBM25Terms(memoryBM25Tokens(query))
	if len(queryTerms) == 0 || len(documents) == 0 {
		return scores
	}
	termDocumentFrequency := map[string]int{}
	documentTermFrequency := make([]map[string]int, len(documents))
	documentLengths := make([]int, len(documents))
	totalLength := 0
	nonEmptyDocuments := 0
	for i, document := range documents {
		tokens := memoryBM25Tokens(document)
		documentLengths[i] = len(tokens)
		if len(tokens) == 0 {
			documentTermFrequency[i] = map[string]int{}
			continue
		}
		nonEmptyDocuments++
		totalLength += len(tokens)
		termFrequency := map[string]int{}
		seen := map[string]bool{}
		for _, token := range tokens {
			termFrequency[token]++
			if !seen[token] {
				termDocumentFrequency[token]++
				seen[token] = true
			}
		}
		documentTermFrequency[i] = termFrequency
	}
	if nonEmptyDocuments == 0 || totalLength == 0 {
		return scores
	}
	averageDocumentLength := float64(totalLength) / float64(nonEmptyDocuments)
	maxScore := 0.0
	for i, termFrequency := range documentTermFrequency {
		if len(termFrequency) == 0 {
			continue
		}
		documentLength := float64(documentLengths[i])
		score := 0.0
		for _, term := range queryTerms {
			frequency := termFrequency[term]
			if frequency == 0 {
				continue
			}
			documentFrequency := termDocumentFrequency[term]
			if documentFrequency == 0 {
				continue
			}
			idf := math.Log(1 + (float64(nonEmptyDocuments-documentFrequency)+0.5)/(float64(documentFrequency)+0.5))
			denominator := float64(frequency) + memoryBM25K1*(1-memoryBM25B+memoryBM25B*documentLength/averageDocumentLength)
			if denominator <= 0 {
				continue
			}
			score += idf * (float64(frequency) * (memoryBM25K1 + 1)) / denominator
		}
		scores[i] = score
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore <= 0 {
		return scores
	}
	for i, score := range scores {
		scores[i] = clamp01(score / maxScore)
	}
	return scores
}

func uniqueMemoryBM25Terms(tokens []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func memoryBM25Tokens(value string) []string {
	value = strings.ToLower(value)
	tokens := []string{}
	ascii := make([]rune, 0, 16)
	cjk := make([]rune, 0, 16)
	flushASCII := func() {
		if len(ascii) >= 2 {
			token := string(ascii)
			if !memoryBM25StopWord(token) {
				tokens = append(tokens, token)
			}
		}
		ascii = ascii[:0]
	}
	flushCJK := func() {
		if len(cjk) == 2 {
			token := string(cjk)
			if !memoryBM25StopWord(token) {
				tokens = append(tokens, token)
			}
		} else if len(cjk) > 2 {
			for i := 0; i+1 < len(cjk); i++ {
				token := string(cjk[i : i+2])
				if !memoryBM25StopWord(token) {
					tokens = append(tokens, token)
				}
			}
		}
		cjk = cjk[:0]
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			flushCJK()
			ascii = append(ascii, r)
		case isMemoryBM25CJK(r):
			flushASCII()
			cjk = append(cjk, r)
		default:
			flushASCII()
			flushCJK()
		}
	}
	flushASCII()
	flushCJK()
	return tokens
}

func isMemoryBM25CJK(r rune) bool {
	return (r >= '\u4e00' && r <= '\u9fff') || (r >= '\u3400' && r <= '\u4dbf')
}

func memoryBM25StopWord(token string) bool {
	switch strings.TrimSpace(token) {
	case "", "the", "and", "for", "with", "this", "that", "you", "are", "was", "were", "一个", "这个", "那个", "一下", "我们", "你们", "他们":
		return true
	default:
		return false
	}
}

func memoryTokens(value string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range memoryBM25Tokens(value) {
		tokens[token] = true
	}
	return tokens
}

func defaultCategoryImportance(category string) float64 {
	switch normalizeMemoryCategory(category) {
	case MemoryCategoryEvent:
		return 0.8
	case MemoryCategoryFact:
		return 0.7
	case MemoryCategoryPreference:
		return 0.6
	case MemoryCategorySkill:
		return 0.5
	default:
		return defaultMemoryWeight
	}
}

func averageMemoryConfidence(items []MemoryItem) float64 {
	if len(items) == 0 {
		return defaultMemoryConfidence
	}
	var total float64
	for _, item := range items {
		total += clamp01(item.Confidence)
	}
	return clamp01(total / float64(len(items)))
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func upsertMemoryItem(existing []MemoryItem, candidate MemoryItem) MemoryItem {
	candidate = normalizeMemoryItem(candidate)
	for _, item := range existing {
		item = normalizeMemoryItem(item)
		if item.RawHash == "" || item.RawHash != candidate.RawHash || item.Namespace != candidate.Namespace {
			continue
		}
		candidate.ID = item.ID
		candidate.CreatedAt = item.CreatedAt
		candidate.AccessCount = item.AccessCount + 1
		candidate.Weight = computeMemoryWeight(candidate.Category, defaultCategoryImportance(candidate.Category), candidate.Confidence, candidate.UpdatedAt, candidate.AccessCount)
		return candidate
	}
	return candidate
}

func applyMemoryConflictResolution(existing []MemoryItem, candidate MemoryItem) (MemoryItem, []MemoryItem) {
	return applyMemoryConflictResolutionWithPolicy(existing, candidate, DefaultMemoryPolicy())
}

func applyMemoryConflictResolutionWithPolicy(existing []MemoryItem, candidate MemoryItem, policy MemoryPolicy) (MemoryItem, []MemoryItem) {
	policy = normalizeMemoryPolicy(policy)
	candidate = normalizeMemoryItem(candidate)
	if candidate.ID == "" {
		candidate.ID = newMemoryID()
	}
	var updates []MemoryItem
	for _, current := range existing {
		current = normalizeMemoryItem(current)
		if current.ID == candidate.ID || current.Status != MemoryStatusActive {
			continue
		}
		conflicts, conflictRuleID := memoryConflictCandidateWithPolicy(current, candidate, policy)
		if current.Namespace == MemoryNamespacePersonalization && current.Source == MemorySourceUserEdit && candidate.Source != MemorySourceUserEdit && conflicts {
			candidate.Status = MemoryStatusArchived
			candidate.SupersededByID = current.ID
			candidate.ConflictIDs = normalizeMemoryIDs(append(candidate.ConflictIDs, current.ID))
			candidate.Metadata["conflict_strategy"] = "explicit_personalization"
			candidate.Metadata["conflict_rule_id"] = conflictRuleID
			candidate.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
			return candidate, updates
		}
		if current.Namespace != candidate.Namespace || current.Level != MemoryLevelAtomic || current.Category != candidate.Category {
			continue
		}
		if !conflicts {
			continue
		}
		strategy := memoryConflictStrategyWithPolicy(current, candidate, policy)
		switch strategy {
		case "pending_confirm":
			candidate.Status = MemoryStatusPendingConfirm
			candidate.ConflictIDs = normalizeMemoryIDs(append(candidate.ConflictIDs, current.ID))
			candidate.Metadata["conflict_strategy"] = strategy
			candidate.Metadata["conflict_rule_id"] = conflictRuleID
			candidate.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
			continue
		case "candidate_loses":
			candidate.Status = MemoryStatusArchived
			candidate.SupersededByID = current.ID
			candidate.ConflictIDs = normalizeMemoryIDs(append(candidate.ConflictIDs, current.ID))
			candidate.Metadata["conflict_strategy"] = strategy
			candidate.Metadata["conflict_rule_id"] = conflictRuleID
			candidate.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
			continue
		default:
			current.Status = MemoryStatusArchived
			current.SupersededByID = candidate.ID
			current.ConflictIDs = normalizeMemoryIDs(append(current.ConflictIDs, candidate.ID))
			current.UpdatedAt = time.Now().UTC()
			if current.Metadata == nil {
				current.Metadata = map[string]any{}
			}
			current.Metadata["conflict_strategy"] = strategy
			current.Metadata["conflict_rule_id"] = conflictRuleID
			current.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
			candidate.SupersedesID = current.ID
			candidate.ConflictIDs = normalizeMemoryIDs(append(candidate.ConflictIDs, current.ID))
			candidate.Metadata["conflict_strategy"] = strategy
			candidate.Metadata["conflict_rule_id"] = conflictRuleID
			candidate.Metadata["memory_policy_version"] = memoryPolicyVersion(policy)
			updates = append(updates, current)
		}
	}
	return candidate, updates
}

func memoryConflictCandidate(a, b MemoryItem) bool {
	conflicts, _ := memoryConflictCandidateWithPolicy(a, b, DefaultMemoryPolicy())
	return conflicts
}

func memoryConflictCandidateWithPolicy(a, b MemoryItem, policy MemoryPolicy) (bool, string) {
	policy = normalizeMemoryPolicy(policy)
	if a.RawHash != "" && a.RawHash == b.RawHash {
		return false, ""
	}
	if aSlot, ok := memoryFactConflictSlotWithPolicy(a.Content, policy); ok {
		if bSlot, ok := memoryFactConflictSlotWithPolicy(b.Content, policy); ok && aSlot.Name == bSlot.Name {
			if memorySlotValuesSameWithPolicy(aSlot.Value, bSlot.Value, policy) {
				return false, aSlot.RuleID
			}
			return true, aSlot.RuleID
		}
	}
	overlap := memoryTextRelevance(a.Content, b.Content)
	if overlap < policy.Conflict.TextOverlapMin {
		return false, ""
	}
	if memoryContradictsWithPolicy(a.Content, b.Content, policy) {
		return true, "text_contradiction"
	}
	if overlap >= policy.Conflict.StrongOverlapThreshold {
		return true, "strong_text_overlap"
	}
	return false, ""
}

func memoryFactConflictSlot(content string) (memoryConflictSlot, bool) {
	return memoryFactConflictSlotWithPolicy(content, DefaultMemoryPolicy())
}

func memoryFactConflictSlotWithPolicy(content string, policy MemoryPolicy) (memoryConflictSlot, bool) {
	value := strings.ToLower(strings.TrimSpace(content))
	if value == "" {
		return memoryConflictSlot{}, false
	}
	for _, slot := range normalizeMemoryPolicy(policy).Conflict.Slots {
		for _, marker := range slot.Markers {
			marker = strings.ToLower(strings.TrimSpace(marker))
			if marker == "" {
				continue
			}
			if idx := strings.Index(value, marker); idx >= 0 {
				slotValue := normalizeMemorySlotValueWithPolicy(value[idx+len(marker):], policy)
				return memoryConflictSlot{Name: strings.TrimSpace(slot.Name), Value: slotValue, RuleID: strings.TrimSpace(slot.ID)}, true
			}
		}
	}
	return memoryConflictSlot{}, false
}

func memorySlotValuesSame(a, b string) bool {
	return memorySlotValuesSameWithPolicy(a, b, DefaultMemoryPolicy())
}

func memorySlotValuesSameWithPolicy(a, b string, policy MemoryPolicy) bool {
	a = normalizeMemorySlotValueWithPolicy(a, policy)
	b = normalizeMemorySlotValueWithPolicy(b, policy)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

func normalizeMemorySlotValue(value string) string {
	return normalizeMemorySlotValueWithPolicy(value, DefaultMemoryPolicy())
}

func normalizeMemorySlotValueWithPolicy(value string, policy MemoryPolicy) string {
	conflictPolicy := normalizeMemoryPolicy(policy).Conflict
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, " \t\r\n。.!?！？,，;；:：")
	for _, prefix := range conflictPolicy.SlotValuePrefixes {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	for _, suffix := range conflictPolicy.SlotValueSuffixes {
		value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
	}
	replacerArgs := make([]string, 0, len(conflictPolicy.SlotValueRemove)*2)
	for _, token := range conflictPolicy.SlotValueRemove {
		replacerArgs = append(replacerArgs, token, "")
	}
	if len(replacerArgs) == 0 {
		return value
	}
	replacer := strings.NewReplacer(replacerArgs...)
	return replacer.Replace(value)
}

func memoryConflictStrategy(existing, candidate MemoryItem) string {
	return memoryConflictStrategyWithPolicy(existing, candidate, DefaultMemoryPolicy())
}

func memoryConflictStrategyWithPolicy(existing, candidate MemoryItem, policy MemoryPolicy) string {
	if sourcePriority(candidate.Source) > sourcePriority(existing.Source) {
		return "source_priority"
	}
	if sourcePriority(candidate.Source) < sourcePriority(existing.Source) {
		return "candidate_loses"
	}
	if memoryTemporalReplacementWithPolicy(candidate.Content, policy) {
		return "temporal"
	}
	diff := math.Abs(candidate.Confidence - existing.Confidence)
	if diff < 0.05 {
		return "pending_confirm"
	}
	if candidate.Confidence > existing.Confidence {
		return "confidence"
	}
	return "candidate_loses"
}

func sourcePriority(source string) int {
	switch normalizeMemorySource(source) {
	case MemorySourceUserEdit:
		return 4
	case MemorySourceAttachment, MemorySourceArtifact, MemorySourceVision, MemorySourceBrowser:
		return 3
	case MemorySourceConversation:
		return 2
	default:
		return 1
	}
}

func memoryTemporalReplacement(content string) bool {
	return memoryTemporalReplacementWithPolicy(content, DefaultMemoryPolicy())
}

func memoryTemporalReplacementWithPolicy(content string, policy MemoryPolicy) bool {
	lower := strings.ToLower(content)
	for _, phrase := range normalizeMemoryPolicy(policy).Conflict.TemporalMarkers {
		needle := strings.ToLower(phrase)
		if strings.TrimSpace(needle) != "" && strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func memoryContradicts(a, b string) bool {
	return memoryContradictsWithPolicy(a, b, DefaultMemoryPolicy())
}

func memoryContradictsWithPolicy(a, b string, policy MemoryPolicy) bool {
	policy = normalizeMemoryPolicy(policy)
	aLower := strings.ToLower(a)
	bLower := strings.ToLower(b)
	aNeg := memoryHasNegationWithPolicy(aLower, policy)
	bNeg := memoryHasNegationWithPolicy(bLower, policy)
	if aNeg != bNeg && memoryTextRelevance(a, b) >= policy.Conflict.TextOverlapMin {
		return true
	}
	return memoryTemporalReplacementWithPolicy(bLower, policy) && memoryTextRelevance(a, b) >= policy.Conflict.TextOverlapMin
}

func memoryHasNegation(value string) bool {
	return memoryHasNegationWithPolicy(value, DefaultMemoryPolicy())
}

func memoryHasNegationWithPolicy(value string, policy MemoryPolicy) bool {
	lower := strings.ToLower(value)
	for _, phrase := range normalizeMemoryPolicy(policy).Conflict.NegationMarkers {
		needle := strings.ToLower(phrase)
		if strings.TrimSpace(needle) != "" && strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func recordMemoryInjection(item MemoryItem, sessionID, query string, now time.Time) MemoryItem {
	item = normalizeMemoryItem(item)
	item.AccessCount++
	item.LastInjectedAt = &now
	item.Weight = memoryContextScore(item, query)
	item.UpdatedAt = now
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	item.Metadata["last_injected_session_id"] = strings.TrimSpace(sessionID)
	item.Metadata["last_injected_query"] = truncateForMemory(query)
	item.Metadata["last_context_score"] = item.Weight
	return item
}

type MemoryOrganizer interface {
	Plan(ctx context.Context, userID string, items []MemoryItem, now time.Time) ([]MemoryMaintenanceAction, error)
}

type HybridMemoryOrganizer struct {
	Primary  MemoryOrganizer
	Fallback MemoryOrganizer
}

func NewHybridMemoryOrganizer(primary, fallback MemoryOrganizer) HybridMemoryOrganizer {
	return HybridMemoryOrganizer{Primary: primary, Fallback: fallback}
}

func (o HybridMemoryOrganizer) Plan(ctx context.Context, userID string, items []MemoryItem, now time.Time) ([]MemoryMaintenanceAction, error) {
	if o.Primary != nil {
		actions, err := o.Primary.Plan(ctx, userID, items, now)
		if err == nil && len(actions) > 0 {
			return actions, nil
		}
	}
	if o.Fallback != nil {
		return o.Fallback.Plan(ctx, userID, items, now)
	}
	return nil, nil
}

type LLMMemoryOrganizer struct {
	RunnerFactory EngineFactory
	Timeout       time.Duration
}

func NewLLMMemoryOrganizer(factory EngineFactory) LLMMemoryOrganizer {
	return LLMMemoryOrganizer{RunnerFactory: factory, Timeout: 8 * time.Second}
}

func (o LLMMemoryOrganizer) Plan(ctx context.Context, userID string, items []MemoryItem, now time.Time) ([]MemoryMaintenanceAction, error) {
	if o.RunnerFactory == nil {
		return nil, fmt.Errorf("memory LLM organizer has no runner factory")
	}
	payload := memoryOrganizerPayload(items)
	if len(payload) == 0 {
		return nil, nil
	}
	body, _ := json.MarshalIndent(payload, "", "  ")
	prompt := fmt.Sprintf(PromptMemoryOrganizerTemplate, string(body))
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := o.RunnerFactory(Scope{UserID: userID}).RunGeneratedPrompt(callCtx, state.NewSession(""), prompt)
	if err != nil {
		return nil, err
	}
	return parseLLMMemoryMaintenanceActions(result.Output, userID, items, now)
}

type RuleMemoryOrganizer struct{}

func NewRuleMemoryOrganizer() RuleMemoryOrganizer {
	return RuleMemoryOrganizer{}
}

func (RuleMemoryOrganizer) Plan(_ context.Context, userID string, items []MemoryItem, now time.Time) ([]MemoryMaintenanceAction, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	actions := []MemoryMaintenanceAction{}
	seen := map[string]bool{}
	add := func(action MemoryMaintenanceAction) {
		action.UserID = userID
		action.MemoryIDs = normalizeMemoryIDs(action.MemoryIDs)
		action.Confidence = clamp01(action.Confidence)
		if action.Status == "" {
			action.Status = MemoryMaintenancePending
		}
		if action.CreatedAt.IsZero() {
			action.CreatedAt = now
		}
		action.ID = memoryMaintenanceActionID(action.Type, action.MemoryIDs)
		if action.ID == "" || seen[action.ID] {
			return
		}
		seen[action.ID] = true
		actions = append(actions, action)
	}
	var dirtyAbstraction bool
	var profileCount int
	for _, item := range items {
		item = scoreMemoryQuality(item, items, now)
		quality := metadataFloat(item.Metadata, "quality_score", 0.5)
		staleness := metadataFloat(item.Metadata, "staleness_score", 0)
		redundancy := metadataFloat(item.Metadata, "redundancy_score", 0)
		if item.Status == MemoryStatusPendingConfirm || item.Status == MemoryStatusConflicted {
			add(MemoryMaintenanceAction{Type: "confirm_conflict", MemoryIDs: []string{item.ID}, Reason: "Memory conflict is waiting for user confirmation.", Confidence: 0.95})
		}
		if item.Status == MemoryStatusActive && item.Level == MemoryLevelAtomic && quality < 0.35 && staleness > 0.55 && item.AccessCount == 0 {
			add(MemoryMaintenanceAction{Type: "archive_low_quality", MemoryIDs: []string{item.ID}, Reason: "Memory has low quality, low usage, and is stale.", Confidence: 0.75})
		}
		if item.Status == MemoryStatusActive && item.Level == MemoryLevelAtomic && redundancy > 0.65 {
			duplicates := []string{item.ID}
			for _, other := range items {
				other = normalizeMemoryItem(other)
				if other.ID != item.ID && other.Namespace == item.Namespace && other.Category == item.Category && other.Status == MemoryStatusActive && memoryTextRelevance(item.Content, other.Content) > 0.65 {
					duplicates = append(duplicates, other.ID)
				}
			}
			if len(duplicates) > 1 {
				add(MemoryMaintenanceAction{Type: "merge_duplicates", MemoryIDs: duplicates, Reason: "Multiple memory items appear to describe the same user fact or preference.", Confidence: 0.70})
			}
		}
		if item.Source == MemorySourceSystem && (item.Level == MemoryLevelConcept || item.Level == MemoryLevelProfile) {
			if dirty, _ := item.Metadata["dirty"].(bool); dirty {
				dirtyAbstraction = true
			}
			if item.Level == MemoryLevelProfile {
				profileCount++
			}
		}
	}
	atomicByCategory := map[string]int{}
	for _, item := range items {
		item = normalizeMemoryItem(item)
		if item.Status == MemoryStatusActive && item.Level == MemoryLevelAtomic {
			atomicByCategory[item.Namespace+":"+item.Category]++
		}
	}
	for scope, count := range atomicByCategory {
		if count >= 2 && dirtyAbstraction {
			add(MemoryMaintenanceAction{Type: "rebuild_concept", MemoryIDs: nil, Reason: "Atomic memories changed; concept/profile summaries should be rebuilt for " + scope + ".", Confidence: 0.90})
			break
		}
	}
	if profileCount == 0 && len(atomicByCategory) >= 2 {
		add(MemoryMaintenanceAction{Type: "refresh_profile", MemoryIDs: nil, Reason: "Enough atomic memory exists to build a user profile summary.", Confidence: 0.80})
	}
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Confidence == actions[j].Confidence {
			return actions[i].Type < actions[j].Type
		}
		return actions[i].Confidence > actions[j].Confidence
	})
	return actions, nil
}

func memoryOrganizerPayload(items []MemoryItem) []map[string]any {
	out := make([]map[string]any, 0, minInt(len(items), 80))
	for _, item := range items {
		item = normalizeMemoryItem(item)
		if item.Status == MemoryStatusDeleted {
			continue
		}
		out = append(out, map[string]any{
			"id":           item.ID,
			"namespace":    item.Namespace,
			"level":        item.Level,
			"category":     item.Category,
			"status":       item.Status,
			"source":       item.Source,
			"content":      truncateForMemory(item.Content),
			"confidence":   item.Confidence,
			"weight":       item.Weight,
			"access_count": item.AccessCount,
			"conflict_ids": item.ConflictIDs,
			"quality":      metadataFloat(item.Metadata, "quality_score", 0.5),
			"dirty":        item.Metadata["dirty"],
			"source_refs":  item.SourceRefs,
		})
		if len(out) >= 80 {
			break
		}
	}
	return out
}

func parseLLMMemoryMaintenanceActions(output, userID string, items []MemoryItem, now time.Time) ([]MemoryMaintenanceAction, error) {
	output = strings.TrimSpace(output)
	output = strings.TrimPrefix(output, "```json")
	output = strings.TrimPrefix(output, "```")
	output = strings.TrimSuffix(output, "```")
	output = strings.TrimSpace(output)
	var payload struct {
		Actions []MemoryMaintenanceAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil, err
	}
	allowedIDs := map[string]bool{}
	for _, item := range items {
		allowedIDs[item.ID] = true
	}
	allowedTypes := map[string]bool{
		"archive_low_quality": true,
		"merge_duplicates":    true,
		"rebuild_concept":     true,
		"confirm_conflict":    true,
		"refresh_profile":     true,
		"reduce_weight":       true,
		"promote_episodes":    true,
	}
	var actions []MemoryMaintenanceAction
	for _, action := range payload.Actions {
		action.Type = strings.TrimSpace(action.Type)
		if !allowedTypes[action.Type] || action.Confidence < 0.6 {
			continue
		}
		validIDs := make([]string, 0, len(action.MemoryIDs))
		for _, id := range action.MemoryIDs {
			id = strings.TrimSpace(id)
			if allowedIDs[id] {
				validIDs = append(validIDs, id)
			}
		}
		if len(action.MemoryIDs) > 0 && len(validIDs) == 0 {
			continue
		}
		action.UserID = userID
		action.MemoryIDs = normalizeMemoryIDs(validIDs)
		action.Reason = truncateForMemory(action.Reason)
		if action.Reason == "" {
			action.Reason = "LLM organizer suggested this memory maintenance action."
		}
		action.Status = MemoryMaintenancePending
		action.CreatedAt = now
		action.ID = memoryMaintenanceActionID(action.Type, action.MemoryIDs)
		actions = append(actions, action)
	}
	return actions, nil
}

func memoryMaintenanceActionID(actionType string, memoryIDs []string) string {
	actionType = strings.TrimSpace(actionType)
	if actionType == "" {
		return ""
	}
	memoryIDs = normalizeMemoryIDs(memoryIDs)
	sum := sha256.Sum256([]byte(actionType + "\x00" + strings.Join(memoryIDs, ",")))
	return "mma_" + hex.EncodeToString(sum[:8])
}

func memoryMaintenanceActionMatches(action MemoryMaintenanceAction, actionID string) bool {
	return action.ID == strings.TrimSpace(actionID)
}

func applyMemoryLifecycle(item MemoryItem, now time.Time) (MemoryItem, bool) {
	item = normalizeMemoryItem(item)
	originalStatus := item.Status
	if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
		item.Status = MemoryStatusDeleted
	} else {
		age := now.Sub(item.UpdatedAt)
		switch {
		case item.Status == MemoryStatusActive && age > 90*24*time.Hour:
			item.Status = MemoryStatusDormant
		case item.Status == MemoryStatusDormant && age > 180*24*time.Hour:
			item.Status = MemoryStatusArchived
		}
	}
	if item.Status != originalStatus {
		item.UpdatedAt = now
		return item, true
	}
	return item, false
}

func formatMemoryItems(items []MemoryItem) string {
	if len(items) == 0 {
		return ""
	}
	ordered := append([]MemoryItem(nil), items...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Weight == ordered[j].Weight {
			return ordered[i].UpdatedAt.After(ordered[j].UpdatedAt)
		}
		return ordered[i].Weight > ordered[j].Weight
	})
	var builder strings.Builder
	for _, item := range ordered {
		item = normalizeMemoryItem(item)
		if item.Status != MemoryStatusActive || strings.TrimSpace(item.Content) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		scope := item.Category
		if item.Namespace != "" && item.Namespace != MemoryNamespaceDefault {
			scope = item.Namespace + "/" + scope
		}
		builder.WriteString(fmt.Sprintf("- [%s confidence=%.2f weight=%.2f] %s\n", scope, item.Confidence, item.Weight, item.Content))
	}
	return strings.TrimSpace(builder.String())
}

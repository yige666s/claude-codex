package agentruntime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultMemoryPolicyVersion = "memory-policy-default-v1"

type MemoryPolicy struct {
	Version    string                 `json:"version" yaml:"version"`
	Metadata   map[string]any         `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Extraction MemoryExtractionPolicy `json:"extraction" yaml:"extraction"`
	Safety     MemorySafetyPolicy     `json:"safety" yaml:"safety"`
	Conflict   MemoryConflictPolicy   `json:"conflict" yaml:"conflict"`
	Recall     MemoryRecallPolicy     `json:"recall" yaml:"recall"`
	Episode    MemoryEpisodePolicy    `json:"episode" yaml:"episode"`
}

type MemoryExtractionPolicy struct {
	Rules                  []MemoryExtractionRule  `json:"rules" yaml:"rules"`
	CategoryHints          []MemoryCategoryHint    `json:"category_hints" yaml:"category_hints"`
	WeakContent            MemoryWeakContentPolicy `json:"weak_content" yaml:"weak_content"`
	MinConfidence          float64                 `json:"min_confidence,omitempty" yaml:"min_confidence,omitempty"`
	InlineOriginalMaxRunes int                     `json:"inline_original_max_runes,omitempty" yaml:"inline_original_max_runes,omitempty"`
	MaxContentRunes        int                     `json:"max_content_runes,omitempty" yaml:"max_content_runes,omitempty"`
}

type MemoryExtractionRule struct {
	ID         string   `json:"id" yaml:"id"`
	Kind       string   `json:"kind" yaml:"kind"`
	Pattern    string   `json:"pattern" yaml:"pattern"`
	Category   string   `json:"category,omitempty" yaml:"category,omitempty"`
	Tags       []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	Confidence float64  `json:"confidence,omitempty" yaml:"confidence,omitempty"`
	Importance float64  `json:"importance,omitempty" yaml:"importance,omitempty"`
	Reason     string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type MemoryCategoryHint struct {
	Category string   `json:"category" yaml:"category"`
	Keywords []string `json:"keywords" yaml:"keywords"`
}

type MemoryWeakContentPolicy struct {
	MinRunes       int      `json:"min_runes,omitempty" yaml:"min_runes,omitempty"`
	MinNonCJKWords int      `json:"min_non_cjk_words,omitempty" yaml:"min_non_cjk_words,omitempty"`
	ExactPhrases   []string `json:"exact_phrases,omitempty" yaml:"exact_phrases,omitempty"`
}

type MemorySafetyPolicy struct {
	Version                string          `json:"version,omitempty" yaml:"version,omitempty"`
	PIIRules               []MemoryPIIRule `json:"pii_rules,omitempty" yaml:"pii_rules,omitempty"`
	PromptInjectionPhrases []string        `json:"prompt_injection_phrases,omitempty" yaml:"prompt_injection_phrases,omitempty"`
	OptOutPhrases          []string        `json:"opt_out_phrases,omitempty" yaml:"opt_out_phrases,omitempty"`
}

type MemoryPIIRule struct {
	Name    string `json:"name" yaml:"name"`
	Pattern string `json:"pattern" yaml:"pattern"`
}

type MemoryConflictPolicy struct {
	Slots                  []MemoryConflictSlotPolicy `json:"slots,omitempty" yaml:"slots,omitempty"`
	TemporalMarkers        []string                   `json:"temporal_markers,omitempty" yaml:"temporal_markers,omitempty"`
	NegationMarkers        []string                   `json:"negation_markers,omitempty" yaml:"negation_markers,omitempty"`
	SlotValuePrefixes      []string                   `json:"slot_value_prefixes,omitempty" yaml:"slot_value_prefixes,omitempty"`
	SlotValueSuffixes      []string                   `json:"slot_value_suffixes,omitempty" yaml:"slot_value_suffixes,omitempty"`
	SlotValueRemove        []string                   `json:"slot_value_remove,omitempty" yaml:"slot_value_remove,omitempty"`
	TextOverlapMin         float64                    `json:"text_overlap_min,omitempty" yaml:"text_overlap_min,omitempty"`
	StrongOverlapThreshold float64                    `json:"strong_overlap_threshold,omitempty" yaml:"strong_overlap_threshold,omitempty"`
}

type MemoryConflictSlotPolicy struct {
	ID      string   `json:"id" yaml:"id"`
	Name    string   `json:"name" yaml:"name"`
	Markers []string `json:"markers" yaml:"markers"`
}

type MemoryRecallPolicy struct {
	KeywordPatterns      []MemoryPattern          `json:"keyword_patterns,omitempty" yaml:"keyword_patterns,omitempty"`
	WeakTokens           []string                 `json:"weak_tokens,omitempty" yaml:"weak_tokens,omitempty"`
	QueryPreamblePhrases []string                 `json:"query_preamble_phrases,omitempty" yaml:"query_preamble_phrases,omitempty"`
	Expansions           []MemoryRecallExpansion  `json:"expansions,omitempty" yaml:"expansions,omitempty"`
	ProfileHints         MemoryRecallProfileHints `json:"profile_hints,omitempty" yaml:"profile_hints,omitempty"`
}

type MemoryPattern struct {
	ID      string `json:"id" yaml:"id"`
	Pattern string `json:"pattern" yaml:"pattern"`
}

type MemoryRecallExpansion struct {
	ID           string   `json:"id" yaml:"id"`
	Triggers     []string `json:"triggers" yaml:"triggers"`
	Expansion    string   `json:"expansion" yaml:"expansion"`
	HanExpansion string   `json:"han_expansion,omitempty" yaml:"han_expansion,omitempty"`
}

type MemoryRecallProfileHints struct {
	Nickname      string `json:"nickname,omitempty" yaml:"nickname,omitempty"`
	Occupation    string `json:"occupation,omitempty" yaml:"occupation,omitempty"`
	About         string `json:"about,omitempty" yaml:"about,omitempty"`
	ResponseStyle string `json:"response_style,omitempty" yaml:"response_style,omitempty"`
	Prefix        string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

type MemoryEpisodePolicy struct {
	CaptureSignals []string                `json:"capture_signals,omitempty" yaml:"capture_signals,omitempty"`
	LowInformation MemoryWeakContentPolicy `json:"low_information,omitempty" yaml:"low_information,omitempty"`
}

type MemoryPolicyProvider interface {
	MemoryPolicy() MemoryPolicy
}

type StaticMemoryPolicyProvider struct {
	Policy MemoryPolicy
}

func NewStaticMemoryPolicyProvider(policy MemoryPolicy) StaticMemoryPolicyProvider {
	return StaticMemoryPolicyProvider{Policy: normalizeMemoryPolicy(policy)}
}

func (p StaticMemoryPolicyProvider) MemoryPolicy() MemoryPolicy {
	return normalizeMemoryPolicy(p.Policy)
}

func DefaultMemoryPolicy() MemoryPolicy {
	return normalizeMemoryPolicy(defaultMemoryPolicy())
}

func LoadMemoryPolicyFile(path, expectedVersion string) (MemoryPolicy, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		policy := DefaultMemoryPolicy()
		if strings.TrimSpace(expectedVersion) != "" {
			policy.Version = strings.TrimSpace(expectedVersion)
		}
		return policy, nil
	}
	policy, _, err := loadMemoryPolicyFileWithFingerprint(path, expectedVersion)
	return policy, err
}

func loadMemoryPolicyFileWithFingerprint(path, expectedVersion string) (MemoryPolicy, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		policy := DefaultMemoryPolicy()
		if strings.TrimSpace(expectedVersion) != "" {
			policy.Version = strings.TrimSpace(expectedVersion)
		}
		return policy, "builtin:" + policy.Version, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return MemoryPolicy{}, "", err
	}
	policy, err := parseMemoryPolicyData(path, data, expectedVersion)
	if err != nil {
		return MemoryPolicy{}, "", err
	}
	sum := sha256.Sum256(data)
	return policy, fmt.Sprintf("%x", sum[:]), nil
}

func parseMemoryPolicyData(path string, data []byte, expectedVersion string) (MemoryPolicy, error) {
	var policy MemoryPolicy
	var err error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &policy)
	default:
		err = json.Unmarshal(data, &policy)
		if err != nil {
			err = yaml.Unmarshal(data, &policy)
		}
	}
	if err != nil {
		return MemoryPolicy{}, err
	}
	policy = normalizeMemoryPolicy(policy)
	if expected := strings.TrimSpace(expectedVersion); expected != "" && policy.Version != expected {
		return MemoryPolicy{}, fmt.Errorf("memory policy version %q does not match requested version %q", policy.Version, expected)
	}
	if err := ValidateMemoryPolicy(policy); err != nil {
		return MemoryPolicy{}, err
	}
	return policy, nil
}

func ValidateMemoryPolicy(policy MemoryPolicy) error {
	policy = normalizeMemoryPolicy(policy)
	for _, rule := range policy.Extraction.Rules {
		if strings.TrimSpace(rule.ID) == "" {
			return fmt.Errorf("memory extraction rule has empty id")
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("memory extraction rule %q: %w", rule.ID, err)
		}
		switch strings.ToLower(strings.TrimSpace(rule.Kind)) {
		case "explicit", "preference", "fact":
		default:
			return fmt.Errorf("memory extraction rule %q has unsupported kind %q", rule.ID, rule.Kind)
		}
	}
	for _, rule := range policy.Safety.PIIRules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("memory PII rule has empty name")
		}
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return fmt.Errorf("memory PII rule %q: %w", rule.Name, err)
		}
	}
	for _, pattern := range policy.Recall.KeywordPatterns {
		if strings.TrimSpace(pattern.ID) == "" {
			return fmt.Errorf("memory recall keyword pattern has empty id")
		}
		if _, err := regexp.Compile(pattern.Pattern); err != nil {
			return fmt.Errorf("memory recall keyword pattern %q: %w", pattern.ID, err)
		}
	}
	for _, slot := range policy.Conflict.Slots {
		if strings.TrimSpace(slot.ID) == "" || strings.TrimSpace(slot.Name) == "" {
			return fmt.Errorf("memory conflict slot has empty id or name")
		}
	}
	return nil
}

func normalizeMemoryPolicy(policy MemoryPolicy) MemoryPolicy {
	defaults := defaultMemoryPolicy()
	if strings.TrimSpace(policy.Version) == "" {
		policy.Version = defaults.Version
	}
	if len(policy.Extraction.Rules) == 0 {
		policy.Extraction.Rules = defaults.Extraction.Rules
	}
	if len(policy.Extraction.CategoryHints) == 0 {
		policy.Extraction.CategoryHints = defaults.Extraction.CategoryHints
	}
	if policy.Extraction.MinConfidence <= 0 {
		policy.Extraction.MinConfidence = defaults.Extraction.MinConfidence
	}
	if policy.Extraction.InlineOriginalMaxRunes <= 0 {
		policy.Extraction.InlineOriginalMaxRunes = defaults.Extraction.InlineOriginalMaxRunes
	}
	if policy.Extraction.MaxContentRunes <= 0 {
		policy.Extraction.MaxContentRunes = defaults.Extraction.MaxContentRunes
	}
	policy.Extraction.WeakContent = normalizeWeakContentPolicy(policy.Extraction.WeakContent, defaults.Extraction.WeakContent)
	if strings.TrimSpace(policy.Safety.Version) == "" {
		policy.Safety.Version = defaults.Safety.Version
	}
	if len(policy.Safety.PIIRules) == 0 {
		policy.Safety.PIIRules = defaults.Safety.PIIRules
	}
	if len(policy.Safety.PromptInjectionPhrases) == 0 {
		policy.Safety.PromptInjectionPhrases = defaults.Safety.PromptInjectionPhrases
	}
	if len(policy.Safety.OptOutPhrases) == 0 {
		policy.Safety.OptOutPhrases = defaults.Safety.OptOutPhrases
	}
	if len(policy.Conflict.Slots) == 0 {
		policy.Conflict.Slots = defaults.Conflict.Slots
	}
	if len(policy.Conflict.TemporalMarkers) == 0 {
		policy.Conflict.TemporalMarkers = defaults.Conflict.TemporalMarkers
	}
	if len(policy.Conflict.NegationMarkers) == 0 {
		policy.Conflict.NegationMarkers = defaults.Conflict.NegationMarkers
	}
	if len(policy.Conflict.SlotValuePrefixes) == 0 {
		policy.Conflict.SlotValuePrefixes = defaults.Conflict.SlotValuePrefixes
	}
	if len(policy.Conflict.SlotValueSuffixes) == 0 {
		policy.Conflict.SlotValueSuffixes = defaults.Conflict.SlotValueSuffixes
	}
	if len(policy.Conflict.SlotValueRemove) == 0 {
		policy.Conflict.SlotValueRemove = defaults.Conflict.SlotValueRemove
	}
	if policy.Conflict.TextOverlapMin <= 0 {
		policy.Conflict.TextOverlapMin = defaults.Conflict.TextOverlapMin
	}
	if policy.Conflict.StrongOverlapThreshold <= 0 {
		policy.Conflict.StrongOverlapThreshold = defaults.Conflict.StrongOverlapThreshold
	}
	if len(policy.Recall.KeywordPatterns) == 0 {
		policy.Recall.KeywordPatterns = defaults.Recall.KeywordPatterns
	}
	if len(policy.Recall.WeakTokens) == 0 {
		policy.Recall.WeakTokens = defaults.Recall.WeakTokens
	}
	if len(policy.Recall.QueryPreamblePhrases) == 0 {
		policy.Recall.QueryPreamblePhrases = defaults.Recall.QueryPreamblePhrases
	}
	if len(policy.Recall.Expansions) == 0 {
		policy.Recall.Expansions = defaults.Recall.Expansions
	}
	if strings.TrimSpace(policy.Recall.ProfileHints.Nickname) == "" {
		policy.Recall.ProfileHints.Nickname = defaults.Recall.ProfileHints.Nickname
	}
	if strings.TrimSpace(policy.Recall.ProfileHints.Occupation) == "" {
		policy.Recall.ProfileHints.Occupation = defaults.Recall.ProfileHints.Occupation
	}
	if strings.TrimSpace(policy.Recall.ProfileHints.About) == "" {
		policy.Recall.ProfileHints.About = defaults.Recall.ProfileHints.About
	}
	if strings.TrimSpace(policy.Recall.ProfileHints.ResponseStyle) == "" {
		policy.Recall.ProfileHints.ResponseStyle = defaults.Recall.ProfileHints.ResponseStyle
	}
	if strings.TrimSpace(policy.Recall.ProfileHints.Prefix) == "" {
		policy.Recall.ProfileHints.Prefix = defaults.Recall.ProfileHints.Prefix
	}
	if len(policy.Episode.CaptureSignals) == 0 {
		policy.Episode.CaptureSignals = defaults.Episode.CaptureSignals
	}
	policy.Episode.LowInformation = normalizeWeakContentPolicy(policy.Episode.LowInformation, defaults.Episode.LowInformation)
	return policy
}

func normalizeWeakContentPolicy(policy, defaults MemoryWeakContentPolicy) MemoryWeakContentPolicy {
	if policy.MinRunes <= 0 {
		policy.MinRunes = defaults.MinRunes
	}
	if policy.MinNonCJKWords <= 0 {
		policy.MinNonCJKWords = defaults.MinNonCJKWords
	}
	if len(policy.ExactPhrases) == 0 {
		policy.ExactPhrases = defaults.ExactPhrases
	}
	return policy
}

func compileMemoryPolicyPattern(pattern string) (*regexp.Regexp, bool) {
	compiled, err := regexp.Compile(strings.TrimSpace(pattern))
	if err != nil {
		return nil, false
	}
	return compiled, true
}

func memoryPolicyVersion(policy MemoryPolicy) string {
	return normalizeMemoryPolicy(policy).Version
}

func memoryPolicyVersionFromProvider(provider any) string {
	if policyProvider, ok := provider.(MemoryPolicyProvider); ok && policyProvider != nil {
		return memoryPolicyVersion(policyProvider.MemoryPolicy())
	}
	return memoryPolicyVersion(DefaultMemoryPolicy())
}

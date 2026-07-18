package agentruntime

import (
	"context"
	"strings"

	"claude-codex/internal/harness/state"
)

type MemoryQueryRewriter interface {
	RewriteMemoryRecallQuery(ctx context.Context, input MemoryQueryRewriteInput) (MemoryQueryRewriteResult, error)
}

type MemoryQueryRewriteInput struct {
	UserID          string
	Session         *state.Session
	OriginalQuery   string
	DecisionQuery   string
	Personalization PersonalizationSettings
	Config          MemoryRecallConfig
}

type MemoryQueryRewriteResult struct {
	Query    string
	Used     bool
	Reason   string
	Degraded bool
}

type DeterministicMemoryQueryRewriter struct {
	Policy   MemoryPolicy
	Provider MemoryPolicyProvider
}

func NewDeterministicMemoryQueryRewriter() DeterministicMemoryQueryRewriter {
	return NewDeterministicMemoryQueryRewriterWithPolicy(DefaultMemoryPolicy())
}

func NewDeterministicMemoryQueryRewriterWithPolicy(policy MemoryPolicy) DeterministicMemoryQueryRewriter {
	return DeterministicMemoryQueryRewriter{Policy: normalizeMemoryPolicy(policy)}
}

func NewDeterministicMemoryQueryRewriterWithProvider(provider MemoryPolicyProvider) DeterministicMemoryQueryRewriter {
	return DeterministicMemoryQueryRewriter{Provider: provider}
}

func (r DeterministicMemoryQueryRewriter) MemoryPolicy() MemoryPolicy {
	if r.Provider != nil {
		return r.Provider.MemoryPolicy()
	}
	return normalizeMemoryPolicy(r.Policy)
}

func (r DeterministicMemoryQueryRewriter) RewriteMemoryRecallQuery(_ context.Context, input MemoryQueryRewriteInput) (MemoryQueryRewriteResult, error) {
	policy := r.MemoryPolicy()
	base := strings.Join(strings.Fields(strings.TrimSpace(firstNonEmptyString(input.DecisionQuery, input.OriginalQuery))), " ")
	if base == "" {
		return MemoryQueryRewriteResult{}, nil
	}
	cleaned := stripMemoryRecallQueryPreambleWithPolicy(base, policy)
	variants := []string{cleaned}
	if cleaned != base {
		variants = append(variants, base)
	}
	variants = append(variants, memoryRecallSignalExpansionsWithPolicy(input, cleaned, policy)...)
	rewritten := strings.Join(compactMemoryRecallQueryParts(variants, 6), "\n")
	rewritten = tailClipRunes(rewritten, normalizeMemoryRecallConfig(input.Config).RecentContextMaxRunes*2)
	rewritten = strings.TrimSpace(rewritten)
	if rewritten == "" || strings.EqualFold(rewritten, base) {
		return MemoryQueryRewriteResult{Query: base, Reason: "deterministic_noop"}, nil
	}
	return MemoryQueryRewriteResult{
		Query:  rewritten,
		Used:   true,
		Reason: "deterministic_memory_signals",
	}, nil
}

func stripMemoryRecallQueryPreamble(query string) string {
	return stripMemoryRecallQueryPreambleWithPolicy(query, DefaultMemoryPolicy())
}

func stripMemoryRecallQueryPreambleWithPolicy(query string, policy MemoryPolicy) string {
	query = strings.TrimSpace(query)
	for _, phrase := range normalizeMemoryPolicy(policy).Recall.QueryPreamblePhrases {
		query = strings.ReplaceAll(query, phrase, " ")
	}
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

func memoryRecallSignalExpansions(input MemoryQueryRewriteInput, query string) []string {
	return memoryRecallSignalExpansionsWithPolicy(input, query, DefaultMemoryPolicy())
}

func memoryRecallSignalExpansionsWithPolicy(input MemoryQueryRewriteInput, query string, policy MemoryPolicy) []string {
	lower := strings.ToLower(query)
	var out []string
	for _, expansion := range normalizeMemoryPolicy(policy).Recall.Expansions {
		if !containsAnyString(lower, expansion.Triggers...) {
			continue
		}
		if strings.TrimSpace(expansion.Expansion) != "" {
			out = append(out, expansion.Expansion)
		}
		if memoryRecallQueryContainsHan(query) {
			if strings.TrimSpace(expansion.HanExpansion) != "" {
				out = append(out, expansion.HanExpansion)
			}
		}
	}
	if profileHints := memoryRecallProfileHintsWithPolicy(input.Personalization, policy); profileHints != "" {
		out = append(out, profileHints)
	}
	return out
}

func memoryRecallProfileHints(settings PersonalizationSettings) string {
	return memoryRecallProfileHintsWithPolicy(settings, DefaultMemoryPolicy())
}

func memoryRecallProfileHintsWithPolicy(settings PersonalizationSettings, policy MemoryPolicy) string {
	profileHints := normalizeMemoryPolicy(policy).Recall.ProfileHints
	var hints []string
	if strings.TrimSpace(settings.Profile.Nickname) != "" {
		hints = append(hints, profileHints.Nickname)
	}
	if strings.TrimSpace(settings.Profile.Occupation) != "" {
		hints = append(hints, profileHints.Occupation)
	}
	if strings.TrimSpace(settings.Profile.About) != "" {
		hints = append(hints, profileHints.About)
	}
	if strings.TrimSpace(settings.Style.Preset) != "" || strings.TrimSpace(settings.Style.Tone) != "" {
		hints = append(hints, profileHints.ResponseStyle)
	}
	if len(hints) == 0 {
		return ""
	}
	return profileHints.Prefix + strings.Join(compactMemoryRecallQueryParts(hints, len(hints)), ", ")
}

func compactMemoryRecallQueryParts(values []string, limit int) []string {
	if limit <= 0 {
		limit = len(values)
	}
	seen := map[string]bool{}
	out := make([]string, 0, min(limit, len(values)))
	for _, value := range values {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func containsAnyString(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func memoryRecallQueryContainsHan(value string) bool {
	for _, r := range value {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

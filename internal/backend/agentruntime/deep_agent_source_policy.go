package agentruntime

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	deepAgentSourcePolicyDefaultMaxSourcesPerBranch = 12
	deepAgentSourcePolicyDefaultMaxDuplicateDomains = 2
	deepAgentSourcePolicyDefaultMinScore            = 0.30
)

type deepAgentSourcePolicyDecision struct {
	URL    string  `json:"url,omitempty"`
	Title  string  `json:"title,omitempty"`
	Domain string  `json:"domain,omitempty"`
	Action string  `json:"action,omitempty"`
	Reason string  `json:"reason,omitempty"`
	Score  float64 `json:"score,omitempty"`
}

type deepAgentSourcePolicyReport struct {
	InputCount            int                             `json:"input_count"`
	OutputCount           int                             `json:"output_count"`
	FilteredCount         int                             `json:"filtered_count"`
	DuplicateURLCount     int                             `json:"duplicate_url_count"`
	DuplicateDomainCount  int                             `json:"duplicate_domain_count"`
	BlockedDomainCount    int                             `json:"blocked_domain_count"`
	LowScoreCount         int                             `json:"low_score_count"`
	TrimmedCount          int                             `json:"trimmed_count"`
	PrimarySourcePresent  bool                            `json:"primary_source_present"`
	PrimarySourceRequired bool                            `json:"primary_source_required"`
	RecencyRequirement    string                          `json:"recency_requirement,omitempty"`
	MinSourceScore        float64                         `json:"min_source_score,omitempty"`
	MaxSources            int                             `json:"max_sources,omitempty"`
	MaxDuplicateDomains   int                             `json:"max_duplicate_domains,omitempty"`
	Decisions             []deepAgentSourcePolicyDecision `json:"decisions,omitempty"`
}

type deepAgentScoredSourceRef struct {
	ref     DeepAgentSourceRef
	score   float64
	reasons []string
	index   int
	domain  string
	primary bool
}

func normalizeLoopContractSourcePolicy(policy LoopContractSourcePolicy, objective string) LoopContractSourcePolicy {
	if policy.QualityBar == "" {
		policy.QualityBar = "traceable and relevant evidence"
	}
	policy.PreferredDomains = normalizeDeepAgentDomainPatterns(append(policy.PreferredDomains, policy.PreferredSources...))
	policy.BlockedDomains = normalizeDeepAgentDomainPatterns(policy.BlockedDomains)
	if policy.MaxSourcesPerBranch <= 0 {
		policy.MaxSourcesPerBranch = deepAgentSourcePolicyDefaultMaxSourcesPerBranch
	}
	if policy.MaxDuplicateDomains <= 0 {
		policy.MaxDuplicateDomains = deepAgentSourcePolicyDefaultMaxDuplicateDomains
	}
	if policy.MinSourceScore <= 0 {
		policy.MinSourceScore = deepAgentSourcePolicyDefaultMinScore
	}
	if policy.MinSourceScore > 1 {
		policy.MinSourceScore = 1
	}
	if !policy.RequirePrimarySource && deepAgentSourcePolicyLooksProductResearch(objective) {
		policy.RequirePrimarySource = true
	}
	return policy
}

func deepAgentSourcePolicyLooksProductResearch(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return deepAgentContainsAny(lower,
		"product", "company", "pricing", "changelog", "competitor", "market", "vendor", "saas", "research",
		"产品", "公司", "定价", "竞品", "市场", "调研", "研究",
	)
}

func deepAgentSourcePolicyFromAction(action DeepAgentAction) LoopContractSourcePolicy {
	if action.Args == nil {
		return normalizeLoopContractSourcePolicy(LoopContractSourcePolicy{}, deepAgentActionString(action, "goal"))
	}
	policy := deepAgentSourcePolicyFromAny(action.Args["source_policy"])
	if policy.QualityBar == "" && action.Args["loop_contract"] != nil {
		contract := loopContractFromWorkflowValue(action.Args["loop_contract"])
		policy = contract.SourcePolicy
	}
	return normalizeLoopContractSourcePolicy(policy, deepAgentActionString(action, "goal"))
}

func deepAgentSourcePolicyFromAny(value any) LoopContractSourcePolicy {
	if value == nil {
		return LoopContractSourcePolicy{}
	}
	switch typed := value.(type) {
	case LoopContractSourcePolicy:
		return typed
	case map[string]any:
		policy := LoopContractSourcePolicy{
			RequiresSources:      deepAgentSourcePolicyBool(typed["requires_sources"]),
			MinSourceCount:       deepAgentSourcePolicyInt(typed["min_source_count"]),
			PreferredSources:     deepAgentStringSlice(typed["preferred_sources"]),
			PreferredDomains:     deepAgentStringSlice(typed["preferred_domains"]),
			BlockedDomains:       deepAgentStringSlice(typed["blocked_domains"]),
			MaxSourcesPerBranch:  deepAgentSourcePolicyInt(typed["max_sources_per_branch"]),
			MaxDuplicateDomains:  deepAgentSourcePolicyInt(typed["max_duplicate_domains"]),
			RequirePrimarySource: deepAgentSourcePolicyBool(typed["require_primary_source"]),
			RecencyRequirement:   strings.TrimSpace(fmt.Sprint(typed["recency_requirement"])),
			MinSourceScore:       deepAgentSourcePolicyFloat(typed["min_source_score"]),
			QualityBar:           strings.TrimSpace(fmt.Sprint(typed["quality_bar"])),
		}
		if policy.RecencyRequirement == "<nil>" {
			policy.RecencyRequirement = ""
		}
		if policy.QualityBar == "<nil>" {
			policy.QualityBar = ""
		}
		return policy
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return LoopContractSourcePolicy{}
		}
		var policy LoopContractSourcePolicy
		if err := json.Unmarshal(data, &policy); err != nil {
			return LoopContractSourcePolicy{}
		}
		return policy
	}
}

func deepAgentSourcePolicyPrompt(policy LoopContractSourcePolicy) string {
	policy = normalizeLoopContractSourcePolicy(policy, "")
	var b strings.Builder
	b.WriteString("Source policy:\n")
	b.WriteString(fmt.Sprintf("- Keep at most %d unique sources and no more than %d source(s) from the same domain.\n", policy.MaxSourcesPerBranch, policy.MaxDuplicateDomains))
	b.WriteString("- Deduplicate URLs before fetching; fetch only high-signal pages after search snippets identify likely value.\n")
	b.WriteString("- Prefer official/company pages, docs, pricing, changelog/blog, GitHub/release notes, credible databases/reviews, and high-signal user discussion.\n")
	b.WriteString("- Down-rank SEO listicles, sponsored pages, copied summaries, coupon pages, and generic content farms.\n")
	if len(policy.PreferredDomains) > 0 {
		b.WriteString(fmt.Sprintf("- Preferred domains: %s.\n", strings.Join(policy.PreferredDomains, ", ")))
	}
	if len(policy.BlockedDomains) > 0 {
		b.WriteString(fmt.Sprintf("- Blocked domains: %s.\n", strings.Join(policy.BlockedDomains, ", ")))
	}
	if policy.RequirePrimarySource {
		b.WriteString("- Include at least one official or primary source when one exists.\n")
	}
	if policy.RecencyRequirement != "" {
		b.WriteString(fmt.Sprintf("- Recency requirement: %s.\n", policy.RecencyRequirement))
	}
	return strings.TrimSpace(b.String())
}

func curateDeepAgentSourceRefs(refs []DeepAgentSourceRef, max int) []DeepAgentSourceRef {
	curated, _ := curateDeepAgentSourceRefsWithPolicy(refs, max, LoopContractSourcePolicy{})
	return curated
}

func curateDeepAgentSourceRefsWithPolicy(refs []DeepAgentSourceRef, max int, policy LoopContractSourcePolicy) ([]DeepAgentSourceRef, deepAgentSourcePolicyReport) {
	policy = normalizeLoopContractSourcePolicy(policy, "")
	effectiveMax := policy.MaxSourcesPerBranch
	if max > 0 && max < effectiveMax {
		effectiveMax = max
	}
	report := deepAgentSourcePolicyReport{
		InputCount:            len(refs),
		PrimarySourceRequired: policy.RequirePrimarySource,
		RecencyRequirement:    policy.RecencyRequirement,
		MinSourceScore:        policy.MinSourceScore,
		MaxSources:            effectiveMax,
		MaxDuplicateDomains:   policy.MaxDuplicateDomains,
	}
	if len(refs) == 0 {
		return nil, report
	}

	seenURL := map[string]struct{}{}
	scored := make([]deepAgentScoredSourceRef, 0, len(refs))
	for idx, ref := range refs {
		key := deepAgentSourceRefKey(ref)
		if key == "" {
			report.addDecision(ref, "", "filtered", "missing_url_or_title", 0)
			continue
		}
		if _, ok := seenURL[key]; ok {
			report.DuplicateURLCount++
			report.addDecision(ref, deepAgentSourceRefHost(ref), "filtered", "duplicate_url", 0)
			continue
		}
		seenURL[key] = struct{}{}

		domain := deepAgentSourceRefHost(ref)
		if domain != "" && deepAgentDomainMatches(domain, policy.BlockedDomains) {
			report.BlockedDomainCount++
			report.addDecision(ref, domain, "filtered", "blocked_domain", 0)
			continue
		}

		item := deepAgentScoreSourceRefWithPolicy(ref, policy)
		item.index = idx
		if item.score < policy.MinSourceScore {
			report.LowScoreCount++
			report.addDecision(ref, item.domain, "filtered", "below_min_source_score", item.score)
			continue
		}
		scored = append(scored, item)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index
		}
		return scored[i].score > scored[j].score
	})

	domainCounts := map[string]int{}
	selected := make([]deepAgentScoredSourceRef, 0, effectiveMax)
	for _, item := range scored {
		if effectiveMax > 0 && len(selected) >= effectiveMax {
			report.TrimmedCount++
			report.addDecision(item.ref, item.domain, "filtered", "max_sources_per_branch", item.score)
			continue
		}
		if item.domain != "" && policy.MaxDuplicateDomains > 0 && domainCounts[item.domain] >= policy.MaxDuplicateDomains {
			report.DuplicateDomainCount++
			report.addDecision(item.ref, item.domain, "filtered", "max_duplicate_domains", item.score)
			continue
		}
		selected = append(selected, item)
		if item.domain != "" {
			domainCounts[item.domain]++
		}
		if item.primary {
			report.PrimarySourcePresent = true
		}
		report.addDecision(item.ref, item.domain, "kept", "selected", item.score)
	}

	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].index < selected[j].index
	})
	out := make([]DeepAgentSourceRef, 0, len(selected))
	for _, item := range selected {
		ref := item.ref
		ref.Domain = item.domain
		ref.QualityScore = item.score
		ref.ScoreReasons = append([]string(nil), item.reasons...)
		if ref.SourceKind == "" {
			ref.SourceKind, _, _ = deepAgentClassifySourceQuality(ref)
		}
		out = append(out, ref)
	}
	report.OutputCount = len(out)
	report.FilteredCount = report.InputCount - report.OutputCount
	return out, report
}

func (report *deepAgentSourcePolicyReport) addDecision(ref DeepAgentSourceRef, domain, action, reason string, score float64) {
	if report == nil {
		return
	}
	if len(report.Decisions) >= 24 {
		return
	}
	report.Decisions = append(report.Decisions, deepAgentSourcePolicyDecision{
		URL:    ref.URL,
		Title:  ref.Title,
		Domain: domain,
		Action: action,
		Reason: reason,
		Score:  math.Round(score*100) / 100,
	})
}

func deepAgentScoreSourceRefWithPolicy(ref DeepAgentSourceRef, policy LoopContractSourcePolicy) deepAgentScoredSourceRef {
	domain := deepAgentSourceRefHost(ref)
	score := ref.QualityScore
	if score <= 0 {
		_, classifiedScore, _ := deepAgentClassifySourceQuality(ref)
		score = classifiedScore
	}
	reasons := []string{"base_quality"}
	text := strings.ToLower(strings.Join([]string{
		ref.URL,
		ref.Title,
		ref.Snippet,
		ref.Provider,
		ref.Quality,
		ref.SourceKind,
		domain,
	}, " "))
	primary := deepAgentSourceRefLooksPrimary(ref, domain, text)
	if primary {
		score += 0.18
		reasons = append(reasons, "primary_source")
	}
	if domain != "" && deepAgentDomainMatches(domain, policy.PreferredDomains) {
		score += 0.18
		reasons = append(reasons, "preferred_domain")
	}
	if strings.EqualFold(ref.Provider, "WebFetch") {
		score += 0.08
		reasons = append(reasons, "fetched_page")
	} else if strings.EqualFold(ref.Provider, "WebSearch") {
		score += 0.05
		reasons = append(reasons, "search_result")
	}
	if deepAgentContainsAny(text, "/docs", "/documentation", "/developer", "/api", "/pricing", "/changelog", "/release", "/blog", "/customers", "/case-studies") {
		score += 0.10
		reasons = append(reasons, "high_signal_path")
	}
	if deepAgentContainsAny(domain, "g2.com", "capterra.com", "trustpilot.com", "producthunt.com", "reddit.com", "news.ycombinator.com", "stackoverflow.com") {
		score += 0.04
		reasons = append(reasons, "useful_secondary_discussion")
	}
	if deepAgentContainsAny(domain, "medium.com", "substack.com", "quora.com", "pinterest.com", "facebook.com", "instagram.com", "tiktok.com", "youtube.com", "youtu.be") {
		score -= 0.22
		reasons = append(reasons, "low_signal_domain")
	}
	if deepAgentContainsAny(text, "sponsored", "advertorial", "listicle", "top 10", "best tools", "coupon", "promo code", "alternatives to") {
		score -= 0.18
		reasons = append(reasons, "seo_or_content_farm_signal")
	}
	if policy.RecencyRequirement != "" {
		recencyDelta, reason := deepAgentSourceRecencyScore(text, policy.RecencyRequirement)
		score += recencyDelta
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	if score < 0.01 {
		score = 0.01
	}
	if score > 1 {
		score = 1
	}
	return deepAgentScoredSourceRef{
		ref:     ref,
		score:   math.Round(score*100) / 100,
		reasons: reasons,
		domain:  domain,
		primary: primary,
	}
}

func deepAgentSourceRefScore(ref DeepAgentSourceRef) float64 {
	return deepAgentScoreSourceRefWithPolicy(ref, normalizeLoopContractSourcePolicy(LoopContractSourcePolicy{}, "")).score
}

func deepAgentSourceRefLooksPrimary(ref DeepAgentSourceRef, domain, text string) bool {
	if deepAgentContainsAny(strings.ToLower(ref.SourceKind), "primary", "official", "docs") ||
		deepAgentContainsAny(strings.ToLower(ref.Quality), "primary", "official") {
		return true
	}
	return strings.HasSuffix(domain, ".gov") ||
		strings.HasSuffix(domain, ".edu") ||
		deepAgentContainsAny(domain, "github.com", "gitlab.com") ||
		deepAgentContainsAny(text, "official", "docs.", "/docs", "developer.", "/developer", "/api", "/pricing", "/changelog", "release notes", "whitepaper", "官方")
}

func deepAgentSourceRecencyScore(text, requirement string) (float64, string) {
	if strings.TrimSpace(requirement) == "" {
		return 0, ""
	}
	currentYear := time.Now().UTC().Year()
	switch {
	case strings.Contains(text, strconv.Itoa(currentYear)) || strings.Contains(text, strconv.Itoa(currentYear-1)):
		return 0.06, "freshness_match"
	case strings.Contains(text, strconv.Itoa(currentYear-4)) || strings.Contains(text, strconv.Itoa(currentYear-5)):
		return -0.08, "stale_source_signal"
	default:
		return 0, "recency_unverified"
	}
}

func deepAgentSourceRefHost(ref DeepAgentSourceRef) string {
	raw := strings.TrimSpace(ref.URL)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	return strings.TrimPrefix(host, "www.")
}

func deepAgentDomainMatches(domain string, patterns []string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
	if domain == "" {
		return false
	}
	for _, pattern := range patterns {
		pattern = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(pattern)), "www.")
		if pattern == "" {
			continue
		}
		if domain == pattern || strings.HasSuffix(domain, "."+pattern) {
			return true
		}
	}
	return false
}

func normalizeDeepAgentDomainPatterns(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
			value = parsed.Hostname()
		}
		value = strings.TrimPrefix(strings.ToLower(value), "www.")
		value = strings.Trim(value, "/")
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func deepAgentSourcePolicyInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}

func deepAgentSourcePolicyFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		f, _ := typed.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return f
	default:
		return 0
	}
}

func deepAgentSourcePolicyBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

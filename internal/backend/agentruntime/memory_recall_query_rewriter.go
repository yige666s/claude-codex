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

type DeterministicMemoryQueryRewriter struct{}

func NewDeterministicMemoryQueryRewriter() DeterministicMemoryQueryRewriter {
	return DeterministicMemoryQueryRewriter{}
}

func (DeterministicMemoryQueryRewriter) RewriteMemoryRecallQuery(_ context.Context, input MemoryQueryRewriteInput) (MemoryQueryRewriteResult, error) {
	base := strings.Join(strings.Fields(strings.TrimSpace(firstNonEmptyString(input.DecisionQuery, input.OriginalQuery))), " ")
	if base == "" {
		return MemoryQueryRewriteResult{}, nil
	}
	cleaned := stripMemoryRecallQueryPreamble(base)
	variants := []string{cleaned}
	if cleaned != base {
		variants = append(variants, base)
	}
	variants = append(variants, memoryRecallSignalExpansions(input, cleaned)...)
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
	query = strings.TrimSpace(query)
	for _, phrase := range []string{"请问", "麻烦", "帮我", "帮忙", "查一下", "搜一下", "找一下", "我想知道", "你能不能", "can you", "please", "could you"} {
		query = strings.ReplaceAll(query, phrase, " ")
	}
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

func memoryRecallSignalExpansions(input MemoryQueryRewriteInput, query string) []string {
	lower := strings.ToLower(query)
	var out []string
	if containsAnyString(lower, "ootd", "穿搭", "打卡", "搭配", "衣服", "风格", "审美") {
		out = append(out, "user style preferences outfit preferences saved image references appearance constraints")
		if memoryRecallQueryContainsHan(query) {
			out = append(out, "用户穿搭偏好 风格偏好 保存的图片参考 外观约束")
		}
	}
	if containsAnyString(lower, "附近", "周围", "去哪", "哪里", "where", "nearby", "around", "location", "city", "城市", "住", "搬到") {
		out = append(out, "user profile location city typical places travel preferences")
		if memoryRecallQueryContainsHan(query) {
			out = append(out, "用户个人资料 当前位置 城市 常去地点 出行偏好")
		}
	}
	if containsAnyString(lower, "我是谁", "我的名字", "名字", "职业", "身份", "profile", "identity", "name", "occupation") {
		out = append(out, "user profile facts identity name occupation")
		if memoryRecallQueryContainsHan(query) {
			out = append(out, "用户资料 身份 名字 职业")
		}
	}
	if containsAnyString(lower, "喜欢", "偏好", "不要", "避免", "讨厌", "prefer", "preference", "avoid", "dislike", "boundary", "constraint") {
		out = append(out, "user preferences constraints boundaries likes dislikes")
		if memoryRecallQueryContainsHan(query) {
			out = append(out, "用户偏好 约束 边界 喜好 禁忌")
		}
	}
	if containsAnyString(lower, "上次", "之前", "刚才", "那个", "那张", "这张", "继续", "previous", "earlier", "last time", "that one") {
		out = append(out, "prior conversation choices saved assets episodic summaries")
		if memoryRecallQueryContainsHan(query) {
			out = append(out, "历史对话 之前选择 保存素材 情节摘要")
		}
	}
	if profileHints := memoryRecallProfileHints(input.Personalization); profileHints != "" {
		out = append(out, profileHints)
	}
	return out
}

func memoryRecallProfileHints(settings PersonalizationSettings) string {
	var hints []string
	if strings.TrimSpace(settings.Profile.Nickname) != "" {
		hints = append(hints, "known nickname")
	}
	if strings.TrimSpace(settings.Profile.Occupation) != "" {
		hints = append(hints, "known occupation")
	}
	if strings.TrimSpace(settings.Profile.About) != "" {
		hints = append(hints, "known profile about")
	}
	if strings.TrimSpace(settings.Style.Preset) != "" || strings.TrimSpace(settings.Style.Tone) != "" {
		hints = append(hints, "known response style preferences")
	}
	if len(hints) == 0 {
		return ""
	}
	return "personalization hints: " + strings.Join(hints, ", ")
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

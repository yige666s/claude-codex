package skills

import (
	"regexp"
	"strings"
)

var (
	triggerPattern = regexp.MustCompile(`(?is)triggers?\s+(?:on|include|includes):\s*(.+)$`)
	quotedPattern  = regexp.MustCompile(`"([^"]+)"|'([^']+)'|` + "`" + `([^` + "`" + `]+)` + "`")
)

func (m *SkillManager) MatchUserInvocableSkill(prompt string) (*SkillDefinition, bool) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, false
	}

	skills := m.ListUserInvocableSkills()
	var best *SkillDefinition
	bestScore := 0
	ambiguous := false
	for _, skill := range skills {
		score := scoreSkillPrompt(skill, prompt)
		switch {
		case score > bestScore:
			best = skill
			bestScore = score
			ambiguous = false
		case score > 0 && score == bestScore && best != nil && best.Name != skill.Name:
			ambiguous = true
		}
	}

	if best == nil || bestScore == 0 || ambiguous {
		return nil, false
	}
	return best, true
}

func scoreSkillPrompt(skill *SkillDefinition, prompt string) int {
	if skill == nil {
		return 0
	}
	normalizedPrompt := strings.ToLower(strings.TrimSpace(prompt))
	compactPrompt := normalizeMatchText(normalizedPrompt)
	best := 0

	for _, phrase := range skillMatchingPhrases(skill) {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if len(phrase) < 2 {
			continue
		}
		if strings.Contains(normalizedPrompt, phrase) || strings.Contains(compactPrompt, normalizeMatchText(phrase)) {
			score := 20 + len([]rune(phrase))
			if score > best {
				best = score
			}
		}
	}

	for _, trigger := range skillTriggerPhrases(skill.Description) {
		trigger = strings.ToLower(strings.TrimSpace(trigger))
		if len(trigger) < 2 {
			continue
		}
		if strings.Contains(normalizedPrompt, trigger) || strings.Contains(compactPrompt, normalizeMatchText(trigger)) {
			score := 100 + len([]rune(trigger))
			if score > best {
				best = score
			}
		}
	}

	return best
}

func skillMatchingPhrases(skill *SkillDefinition) []string {
	phrases := []string{skill.Name}
	if skill.DisplayName != "" {
		phrases = append(phrases, skill.DisplayName)
	}
	phrases = append(phrases, skill.Aliases...)
	return phrases
}

func skillTriggerPhrases(description string) []string {
	matches := triggerPattern.FindStringSubmatch(description)
	if len(matches) < 2 {
		return nil
	}
	triggerText := matches[1]
	seen := map[string]bool{}
	result := make([]string, 0)

	for _, match := range quotedPattern.FindAllStringSubmatch(triggerText, -1) {
		for i := 1; i < len(match); i++ {
			phrase := strings.TrimSpace(match[i])
			if phrase != "" && !seen[phrase] {
				seen[phrase] = true
				result = append(result, phrase)
			}
		}
	}

	splitter := func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == ';' || r == '；'
	}
	parts := strings.FieldsFunc(triggerText, splitter)
	for _, part := range parts {
		part = normalizeTriggerPhrase(part)
		if part != "" && !seen[part] {
			seen[part] = true
			result = append(result, part)
		}
	}
	return result
}

func normalizeTriggerPhrase(part string) string {
	part = strings.TrimSpace(part)
	part = strings.Trim(part, ".。")
	lower := strings.ToLower(part)
	prefixes := []string{
		"any mention of ",
		"requests to ",
		"also use when ",
		"use when ",
		"or requests to ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			part = strings.TrimSpace(part[len(prefix):])
			lower = strings.ToLower(part)
		}
	}
	return strings.Trim(part, `"'“”‘’ `)
}

func normalizeMatchText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		" ", "",
		"\t", "",
		"\n", "",
		"\r", "",
		"，", "",
		",", "",
		"。", "",
		".", "",
		"：", "",
		":", "",
		"；", "",
		";", "",
		"、", "",
		"一个", "",
		"一份", "",
		"一篇", "",
		"一下", "",
	)
	return replacer.Replace(value)
}

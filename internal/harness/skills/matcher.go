package skills

import (
	"regexp"
	"strings"
)

var triggerPattern = regexp.MustCompile(`(?i)triggers on:\s*(.+)$`)

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
	best := 0

	for _, phrase := range skillMatchingPhrases(skill) {
		phrase = strings.ToLower(strings.TrimSpace(phrase))
		if len(phrase) < 2 {
			continue
		}
		if strings.Contains(normalizedPrompt, phrase) {
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
		if strings.Contains(normalizedPrompt, trigger) {
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
	splitter := func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == ';'
	}
	parts := strings.FieldsFunc(matches[1], splitter)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

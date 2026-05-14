package messages

import (
	"fmt"
	"strings"
	"sync"

	"claude-codex/internal/harness/skills"
)

const (
	// SKILL_BUDGET_CONTEXT_PERCENT is the percentage of context window for skill listings
	SKILL_BUDGET_CONTEXT_PERCENT = 0.01
	// CHARS_PER_TOKEN is the approximate characters per token
	CHARS_PER_TOKEN = 4
	// DEFAULT_CHAR_BUDGET is the default character budget for skill listings
	DEFAULT_CHAR_BUDGET = 8000 // 1% of 200k × 4
	// MAX_LISTING_DESC_CHARS is the maximum characters for a skill description
	MAX_LISTING_DESC_CHARS = 250
)

// SkillListingManager manages skill listing attachments
type SkillListingManager struct {
	sentSkillNames map[string]bool
	mu             sync.RWMutex
}

// NewSkillListingManager creates a new skill listing manager
func NewSkillListingManager() *SkillListingManager {
	return &SkillListingManager{
		sentSkillNames: make(map[string]bool),
	}
}

// GetCharBudget calculates the character budget based on context window tokens
func GetCharBudget(contextWindowTokens int) int {
	if contextWindowTokens > 0 {
		return int(float64(contextWindowTokens) * CHARS_PER_TOKEN * SKILL_BUDGET_CONTEXT_PERCENT)
	}
	return DEFAULT_CHAR_BUDGET
}

// FormatSkillDescription formats a skill description with truncation
func FormatSkillDescription(skill *skills.SkillDefinition) string {
	desc := skill.Description
	if skill.WhenToUse != "" {
		desc = fmt.Sprintf("%s - %s", desc, skill.WhenToUse)
	}

	if len(desc) > MAX_LISTING_DESC_CHARS {
		return desc[:MAX_LISTING_DESC_CHARS-1] + "…"
	}
	return desc
}

// FormatSkillEntry formats a single skill entry
func FormatSkillEntry(skill *skills.SkillDefinition) string {
	return fmt.Sprintf("- %s: %s", skill.Name, FormatSkillDescription(skill))
}

// FormatFullSkillEntry formats a single skill entry without truncating the
// description. This is used when the runtime intentionally gives the model the
// complete skill catalog for LLM-driven skill selection.
func FormatFullSkillEntry(skill *skills.SkillDefinition) string {
	desc := skill.Description
	if skill.WhenToUse != "" {
		desc = fmt.Sprintf("%s - %s", desc, skill.WhenToUse)
	}
	var hints []string
	if skill.RunAsJob || skill.ExecutionContext == skills.ContextFork {
		hints = append(hints, "run mode: job")
	}
	if producesArtifacts(skill) {
		hints = append(hints, "produces artifacts")
	}
	if skill.ArgumentHint != "" {
		hints = append(hints, "args: "+skill.ArgumentHint)
	}
	if len(hints) == 0 {
		return fmt.Sprintf("- %s: %s", skill.Name, desc)
	}
	return fmt.Sprintf("- %s: %s (%s)", skill.Name, desc, strings.Join(hints, "; "))
}

func producesArtifacts(skill *skills.SkillDefinition) bool {
	if skill == nil {
		return false
	}
	if truthySkillMetadata(skill.Metadata["produces_artifacts"]) {
		return true
	}
	for _, key := range []string{"agentapi", "product", "runtime"} {
		nested, ok := skill.Metadata[key].(map[string]any)
		if !ok {
			if legacy, legacyOK := skill.Metadata[key].(map[interface{}]interface{}); legacyOK {
				nested = make(map[string]any, len(legacy))
				for k, v := range legacy {
					nested[fmt.Sprint(k)] = v
				}
				ok = true
			}
		}
		if ok && truthySkillMetadata(nested["produces_artifacts"]) {
			return true
		}
	}
	return false
}

func truthySkillMetadata(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y", "on":
			return true
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

// FormatAllSkillDescriptions formats every skill with its full description.
func FormatAllSkillDescriptions(skillList []*skills.SkillDefinition) string {
	if len(skillList) == 0 {
		return ""
	}
	entries := make([]string, 0, len(skillList))
	for _, skill := range skillList {
		entries = append(entries, FormatFullSkillEntry(skill))
	}
	return strings.Join(entries, "\n")
}

// FormatSkillsWithinBudget formats skills within the character budget
func FormatSkillsWithinBudget(skillList []*skills.SkillDefinition, contextWindowTokens int) string {
	if len(skillList) == 0 {
		return ""
	}

	budget := GetCharBudget(contextWindowTokens)

	// Separate bundled and non-bundled skills
	var bundledSkills []*skills.SkillDefinition
	var otherSkills []*skills.SkillDefinition

	for _, skill := range skillList {
		if skill.Source == skills.SourceBundled {
			bundledSkills = append(bundledSkills, skill)
		} else {
			otherSkills = append(otherSkills, skill)
		}
	}

	// Try full descriptions first
	var fullEntries []string
	for _, skill := range skillList {
		fullEntries = append(fullEntries, FormatSkillEntry(skill))
	}

	fullTotal := 0
	for _, entry := range fullEntries {
		fullTotal += len(entry)
	}
	fullTotal += len(fullEntries) - 1 // newlines

	if fullTotal <= budget {
		return strings.Join(fullEntries, "\n")
	}

	// Calculate space used by bundled skills (always preserved)
	bundledChars := 0
	for _, skill := range bundledSkills {
		bundledChars += len(FormatSkillEntry(skill)) + 1
	}

	remainingBudget := budget - bundledChars

	if len(otherSkills) == 0 {
		// Only bundled skills
		var entries []string
		for _, skill := range bundledSkills {
			entries = append(entries, FormatSkillEntry(skill))
		}
		return strings.Join(entries, "\n")
	}

	// Calculate max description length for non-bundled skills
	nameOverhead := 0
	for _, skill := range otherSkills {
		nameOverhead += len(skill.Name) + 4 // "- " + ": "
	}
	nameOverhead += len(otherSkills) - 1 // newlines

	availableForDescs := remainingBudget - nameOverhead
	maxDescLen := availableForDescs / len(otherSkills)

	const minDescLength = 20
	if maxDescLen < minDescLength {
		// Extreme case: non-bundled go names-only, bundled keep descriptions
		var entries []string
		for _, skill := range bundledSkills {
			entries = append(entries, FormatSkillEntry(skill))
		}
		for _, skill := range otherSkills {
			entries = append(entries, fmt.Sprintf("- %s", skill.Name))
		}
		return strings.Join(entries, "\n")
	}

	// Truncate non-bundled descriptions to fit within budget
	var entries []string
	for _, skill := range bundledSkills {
		entries = append(entries, FormatSkillEntry(skill))
	}
	for _, skill := range otherSkills {
		desc := FormatSkillDescription(skill)
		if len(desc) > maxDescLen {
			desc = desc[:maxDescLen-1] + "…"
		}
		entries = append(entries, fmt.Sprintf("- %s: %s", skill.Name, desc))
	}

	return strings.Join(entries, "\n")
}

// GetSkillListingAttachment creates a skill listing attachment for new skills
func (m *SkillListingManager) GetSkillListingAttachment(
	allSkills []*skills.SkillDefinition,
	contextWindowTokens int,
) *SkillListingAttachment {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find skills we haven't sent yet
	var newSkills []*skills.SkillDefinition
	for _, skill := range allSkills {
		if !m.sentSkillNames[skill.Name] {
			newSkills = append(newSkills, skill)
		}
	}

	if len(newSkills) == 0 {
		return nil
	}

	// Check if this is the initial batch
	isInitial := len(m.sentSkillNames) == 0

	// Mark as sent
	for _, skill := range newSkills {
		m.sentSkillNames[skill.Name] = true
	}

	// Format within budget
	content := FormatSkillsWithinBudget(newSkills, contextWindowTokens)

	return &SkillListingAttachment{
		Content:    content,
		SkillCount: len(newSkills),
		IsInitial:  isInitial,
	}
}

// SuppressNext marks all current skills as sent (for session resume)
func (m *SkillListingManager) SuppressNext(allSkills []*skills.SkillDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, skill := range allSkills {
		m.sentSkillNames[skill.Name] = true
	}
}

// Reset clears the sent skills tracking
func (m *SkillListingManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sentSkillNames = make(map[string]bool)
}

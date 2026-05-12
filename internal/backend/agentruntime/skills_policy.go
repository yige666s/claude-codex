package agentruntime

import (
	"strings"
	"sync"

	"claude-codex/internal/harness/skills"
)

type PublishedSkillCatalog struct {
	Base     SkillCatalog
	Allowed  map[string]bool
	AllowAll bool
}

func NewPublishedSkillCatalog(base SkillCatalog, names []string, allowAll bool) *PublishedSkillCatalog {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = true
		}
	}
	return &PublishedSkillCatalog{Base: base, Allowed: allowed, AllowAll: allowAll}
}

func (c *PublishedSkillCatalog) GetSkill(name string) (*skills.SkillDefinition, bool) {
	if c == nil || c.Base == nil {
		return nil, false
	}
	skill, ok := c.Base.GetSkill(name)
	if !ok || !c.isPublished(skill) {
		return nil, false
	}
	return skill, true
}

func (c *PublishedSkillCatalog) ListUserInvocableSkills() []*skills.SkillDefinition {
	if c == nil || c.Base == nil {
		return nil
	}
	base := c.Base.ListUserInvocableSkills()
	out := make([]*skills.SkillDefinition, 0, len(base))
	for _, skill := range base {
		if c.isPublished(skill) {
			out = append(out, skill)
		}
	}
	return out
}

func (c *PublishedSkillCatalog) MatchUserInvocableSkill(prompt string) (*skills.SkillDefinition, bool) {
	if c == nil || c.Base == nil {
		return nil, false
	}
	skill, ok := c.Base.MatchUserInvocableSkill(prompt)
	if !ok || !c.isPublished(skill) {
		return nil, false
	}
	return skill, true
}

func (c *PublishedSkillCatalog) isPublished(skill *skills.SkillDefinition) bool {
	if skill == nil {
		return false
	}
	if c.AllowAll {
		return true
	}
	if c.Allowed[skill.Name] {
		return true
	}
	for _, alias := range skill.Aliases {
		if c.Allowed[alias] {
			return true
		}
	}
	return false
}

type RegistrySkillCatalog struct {
	Base SkillCatalog

	mu      sync.RWMutex
	Records map[string]SkillRegistryRecord
}

func NewRegistrySkillCatalog(base SkillCatalog, records []SkillRegistryRecord) *RegistrySkillCatalog {
	byName := make(map[string]SkillRegistryRecord, len(records))
	for _, record := range records {
		record = normalizeSkillRegistryRecord(record)
		if record.Name != "" {
			byName[record.Name] = record
		}
	}
	return &RegistrySkillCatalog{Base: base, Records: byName}
}

func (c *RegistrySkillCatalog) SetRecords(records []SkillRegistryRecord) {
	if c == nil {
		return
	}
	byName := make(map[string]SkillRegistryRecord, len(records))
	for _, record := range records {
		record = normalizeSkillRegistryRecord(record)
		if record.Name != "" {
			byName[record.Name] = record
		}
	}
	c.mu.Lock()
	c.Records = byName
	c.mu.Unlock()
}

func (c *RegistrySkillCatalog) GetSkill(name string) (*skills.SkillDefinition, bool) {
	if c == nil || c.Base == nil {
		return nil, false
	}
	skill, ok := c.Base.GetSkill(name)
	if !ok || !c.isPublished(skill) {
		return nil, false
	}
	return skill, true
}

func (c *RegistrySkillCatalog) ListUserInvocableSkills() []*skills.SkillDefinition {
	if c == nil || c.Base == nil {
		return nil
	}
	base := c.Base.ListUserInvocableSkills()
	out := make([]*skills.SkillDefinition, 0, len(base))
	for _, skill := range base {
		if c.isPublished(skill) {
			out = append(out, skill)
		}
	}
	return out
}

func (c *RegistrySkillCatalog) MatchUserInvocableSkill(prompt string) (*skills.SkillDefinition, bool) {
	if c == nil || c.Base == nil {
		return nil, false
	}
	skill, ok := c.Base.MatchUserInvocableSkill(prompt)
	if !ok || !c.isPublished(skill) {
		return nil, false
	}
	return skill, true
}

func (c *RegistrySkillCatalog) SkillRecord(name string) (SkillRegistryRecord, bool) {
	if c == nil {
		return SkillRegistryRecord{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	record, ok := c.Records[strings.TrimSpace(name)]
	return record, ok
}

func (c *RegistrySkillCatalog) isPublished(skill *skills.SkillDefinition) bool {
	if skill == nil || !skill.UserInvocable || skill.IsHidden {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	record, ok := c.Records[skill.Name]
	if !ok {
		return false
	}
	return normalizeSkillStatus(record.Status) == SkillStatusPublished
}

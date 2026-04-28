package skills

import (
	"reflect"
	"sync"
	"time"
)

// SkillCache caches loaded skills to avoid redundant file reads
type SkillCache struct {
	mu     sync.RWMutex
	skills map[string]*SkillDefinition // fileIdentity -> skill
}

// NewSkillCache creates a new skill cache
func NewSkillCache() *SkillCache {
	return &SkillCache{
		skills: make(map[string]*SkillDefinition),
	}
}

// Get retrieves a skill from cache
func (c *SkillCache) Get(fileIdentity string) *SkillDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.skills[fileIdentity]
}

// Set stores a skill in cache
func (c *SkillCache) Set(fileIdentity string, skill *SkillDefinition) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.skills[fileIdentity] = skill
}

// Remove removes a skill from cache
func (c *SkillCache) Remove(fileIdentity string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.skills, fileIdentity)
}

// Clear clears all cached skills
func (c *SkillCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.skills = make(map[string]*SkillDefinition)
}

// Size returns the number of cached skills
func (c *SkillCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.skills)
}

// List returns all cached skills
func (c *SkillCache) List() []*SkillDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()

	skills := make([]*SkillDefinition, 0, len(c.skills))
	for _, skill := range c.skills {
		skills = append(skills, skill)
	}

	return skills
}

// SkillManager manages all skills (bundled + dynamic)
type SkillManager struct {
	mu                sync.RWMutex
	registry          *SkillRegistry
	loader            *SkillLoader
	dynamicSkills     map[string]*SkillDefinition // name -> skill
	conditionalSkills map[string]*SkillDefinition // name -> skill (not yet activated)
	listeners         []func()                    // Change listeners
}

// NewSkillManager creates a new skill manager
func NewSkillManager() *SkillManager {
	return &SkillManager{
		registry:          NewSkillRegistry(),
		loader:            NewSkillLoader(),
		dynamicSkills:     make(map[string]*SkillDefinition),
		conditionalSkills: make(map[string]*SkillDefinition),
		listeners:         make([]func(), 0),
	}
}

// LoadBundledSkills loads all bundled skills into the registry
func (m *SkillManager) LoadBundledSkills() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	bundled := GetBundledSkills()
	for _, skill := range bundled {
		if err := m.registry.Register(skill); err != nil {
			return err
		}
	}

	return nil
}

// LoadSkillsFromDirectory loads skills from a directory
func (m *SkillManager) LoadSkillsFromDirectory(dir string, source SkillSource) error {
	skills, err := m.loader.LoadSkillsFromDirectory(dir, source)
	if err != nil {
		return err
	}

	return m.registerLoadedSkills(skills)
}

// LoadCommandsFromDirectory loads legacy command-style skills from a commands directory.
func (m *SkillManager) LoadCommandsFromDirectory(dir string, source SkillSource) error {
	skills, err := m.loader.LoadCommandsFromDirectory(dir, source)
	if err != nil {
		return err
	}

	return m.registerLoadedSkills(skills)
}

func (m *SkillManager) RegisterLoadedSkills(skills []*SkillDefinition) error {
	return m.registerLoadedSkills(skills)
}

func (m *SkillManager) registerLoadedSkills(skills []*SkillDefinition) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, skill := range skills {
		// Check if skill has path conditions
		if len(skill.Paths) > 0 {
			// Store as conditional skill
			m.conditionalSkills[skill.Name] = skill
		} else {
			// Register immediately
			if err := m.registry.Register(skill); err != nil {
				// Skip duplicates
				continue
			}
			m.dynamicSkills[skill.Name] = skill
		}
	}

	// Notify listeners
	m.notifyListeners()

	return nil
}

// GetSkill retrieves a skill by name or alias
func (m *SkillManager) GetSkill(nameOrAlias string) (*SkillDefinition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.registry.Get(nameOrAlias)
}

// ListSkills returns all registered skills
func (m *SkillManager) ListSkills() []*SkillDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.registry.List()
}

// ListUserInvocableSkills returns skills that can be invoked by users
func (m *SkillManager) ListUserInvocableSkills() []*SkillDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.registry.ListUserInvocable()
}

// GetDynamicSkills returns all dynamically loaded skills
func (m *SkillManager) GetDynamicSkills() []*SkillDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skills := make([]*SkillDefinition, 0, len(m.dynamicSkills))
	for _, skill := range m.dynamicSkills {
		skills = append(skills, skill)
	}

	return skills
}

// GetConditionalSkillCount returns the number of conditional skills
func (m *SkillManager) GetConditionalSkillCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.conditionalSkills)
}

// ClearDynamicSkills removes all dynamically loaded skills
func (m *SkillManager) ClearDynamicSkills() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove from registry
	for name := range m.dynamicSkills {
		m.registry.Remove(name)
	}

	// Clear maps
	m.dynamicSkills = make(map[string]*SkillDefinition)
	m.conditionalSkills = make(map[string]*SkillDefinition)

	// Clear loader cache
	m.loader.ClearCache()

	// Notify listeners
	m.notifyListeners()
}

// OnSkillsChanged registers a listener for skill changes
func (m *SkillManager) OnSkillsChanged(callback func()) func() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.listeners = append(m.listeners, callback)

	// Return unsubscribe function
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		callbackPointer := reflect.ValueOf(callback).Pointer()
		for i, listener := range m.listeners {
			if reflect.ValueOf(listener).Pointer() == callbackPointer {
				m.listeners = append(m.listeners[:i], m.listeners[i+1:]...)
				break
			}
		}
	}
}

// notifyListeners notifies all change listeners (must be called with lock held)
func (m *SkillManager) notifyListeners() {
	for _, listener := range m.listeners {
		go listener()
	}
}

// EstimateSkillTokens estimates token count for a skill's frontmatter
func (m *SkillManager) EstimateSkillTokens(skill *SkillDefinition) int {
	text := skill.Name + " " + skill.Description
	if skill.WhenToUse != "" {
		text += " " + skill.WhenToUse
	}
	return EstimateTokenCount(text)
}

// SkillStats represents statistics about loaded skills
type SkillStats struct {
	TotalSkills       int
	BundledSkills     int
	DynamicSkills     int
	ConditionalSkills int
	UserInvocable     int
	CacheSize         int
	LastUpdated       time.Time
}

// GetStats returns statistics about loaded skills
func (m *SkillManager) GetStats() SkillStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bundledCount := 0
	userInvocableCount := 0

	for _, skill := range m.registry.List() {
		if skill.Source == SourceBundled {
			bundledCount++
		}
		if skill.UserInvocable {
			userInvocableCount++
		}
	}

	return SkillStats{
		TotalSkills:       m.registry.Count(),
		BundledSkills:     bundledCount,
		DynamicSkills:     len(m.dynamicSkills),
		ConditionalSkills: len(m.conditionalSkills),
		UserInvocable:     userInvocableCount,
		CacheSize:         m.loader.cache.Size(),
		LastUpdated:       time.Now(),
	}
}

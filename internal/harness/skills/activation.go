package skills

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ActivateConditionalSkillsForPaths activates conditional skills that match the given paths
func (m *SkillManager) ActivateConditionalSkillsForPaths(paths []string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	activated := 0

	for name, skill := range m.conditionalSkills {
		// Check if any of the skill's path patterns match any of the given paths
		if shouldActivateSkill(skill, paths) {
			// Register the skill
			if err := m.registry.Register(skill); err == nil {
				// Move from conditional to dynamic
				m.dynamicSkills[name] = skill
				delete(m.conditionalSkills, name)
				activated++
			}
		}
	}

	if activated > 0 {
		m.notifyListeners()
	}

	return activated
}

// shouldActivateSkill checks if a skill should be activated based on path patterns
func shouldActivateSkill(skill *SkillDefinition, paths []string) bool {
	if len(skill.Paths) == 0 {
		return false
	}

	// Check if any path matches any pattern
	for _, path := range paths {
		for _, pattern := range skill.Paths {
			if matchesPattern(path, pattern) {
				return true
			}
		}
	}

	return false
}

// matchesPattern checks if a path matches a glob pattern
func matchesPattern(path, pattern string) bool {
	// Normalize paths
	path = filepath.Clean(path)
	pattern = filepath.Clean(pattern)

	// Try exact match first
	if path == pattern {
		return true
	}

	// Try glob match
	matched, err := doublestar.Match(pattern, path)
	if err != nil {
		return false
	}

	if matched {
		return true
	}

	// Check if path is under pattern directory
	// e.g., pattern "src" should match "src/file.go"
	if strings.HasPrefix(path, pattern+string(filepath.Separator)) {
		return true
	}

	return false
}

// DiscoverSkillDirsForPaths discovers skill directories in the given paths
// This walks up the directory tree looking for .claude/skills directories
func DiscoverSkillDirsForPaths(paths []string) []string {
	seen := make(map[string]bool)
	var dirs []string

	for _, path := range paths {
		// Get absolute path
		absPath, err := filepath.Abs(path)
		if err != nil {
			continue
		}

		// If it's a file, use its directory
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}

		if !info.IsDir() {
			absPath = filepath.Dir(absPath)
		}

		// Walk up the directory tree
		current := absPath
		for {
			// Check for .claude/skills directory
			skillsDir := filepath.Join(current, ".claude", "skills")
			if _, err := os.Stat(skillsDir); err == nil {
				if !seen[skillsDir] {
					seen[skillsDir] = true
					dirs = append(dirs, skillsDir)
				}
			}

			// Move up one level
			parent := filepath.Dir(current)
			if parent == current {
				// Reached root
				break
			}
			current = parent
		}
	}

	return dirs
}

// AddSkillDirectories adds skills from multiple directories
func (m *SkillManager) AddSkillDirectories(dirs []string) error {
	for _, dir := range dirs {
		if err := m.LoadSkillsFromDirectory(dir, SourceFile); err != nil {
			return err
		}
	}
	return nil
}

// GetSkillsForPaths returns skills that are relevant for the given paths
// This includes both activated skills and skills that could be activated
func (m *SkillManager) GetSkillsForPaths(paths []string) []*SkillDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var relevant []*SkillDefinition

	// Add all non-conditional skills
	for _, skill := range m.dynamicSkills {
		if len(skill.Paths) == 0 {
			relevant = append(relevant, skill)
		}
	}

	// Add conditional skills that match
	for _, skill := range m.conditionalSkills {
		if shouldActivateSkill(skill, paths) {
			relevant = append(relevant, skill)
		}
	}

	return relevant
}

// DeactivateSkill removes a skill from the active registry
func (m *SkillManager) DeactivateSkill(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.dynamicSkills[name]
	if !ok {
		return false
	}

	// Remove from registry
	m.registry.Remove(name)

	// If it has path conditions, move back to conditional
	if len(skill.Paths) > 0 {
		m.conditionalSkills[name] = skill
	}

	delete(m.dynamicSkills, name)

	m.notifyListeners()
	return true
}

// ReloadSkillsForPaths reloads skills that are relevant for the given paths
func (m *SkillManager) ReloadSkillsForPaths(paths []string) error {
	// Discover skill directories
	dirs := DiscoverSkillDirsForPaths(paths)

	// Clear existing dynamic skills
	m.ClearDynamicSkills()

	// Load from discovered directories
	return m.AddSkillDirectories(dirs)
}

package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/websandbox"
)

// LoadedFrom indicates where a skill was loaded from
type LoadedFrom string

const (
	LoadedFromCommands LoadedFrom = "commands_DEPRECATED"
	LoadedFromSkills   LoadedFrom = "skills"
	LoadedFromPlugin   LoadedFrom = "plugin"
	LoadedFromManaged  LoadedFrom = "managed"
	LoadedFromBundled  LoadedFrom = "bundled"
	LoadedFromMCP      LoadedFrom = "mcp"
)

// SkillLoader loads skills from the filesystem
type SkillLoader struct {
	cache *SkillCache

	// Dynamic skill tracking
	dynamicSkillDirs               sync.Map // map[string]bool - tracks discovered skill directories
	dynamicSkills                  sync.Map // map[string]*SkillDefinition - dynamically loaded skills
	conditionalSkills              sync.Map // map[string]*SkillDefinition - skills with paths frontmatter
	activatedConditionalSkillNames sync.Map // map[string]bool - tracks which conditional skills were activated

	// Listeners for skill changes
	listeners []func()
	mu        sync.RWMutex
}

// NewSkillLoader creates a new skill loader
func NewSkillLoader() *SkillLoader {
	return &SkillLoader{
		cache:     NewSkillCache(),
		listeners: make([]func(), 0),
	}
}

// AddListener adds a listener that will be called when skills are loaded
func (l *SkillLoader) AddListener(listener func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.listeners = append(l.listeners, listener)
}

// notifyListeners notifies all listeners that skills have changed
func (l *SkillLoader) notifyListeners() {
	l.mu.RLock()
	listeners := make([]func(), len(l.listeners))
	copy(listeners, l.listeners)
	l.mu.RUnlock()

	for _, listener := range listeners {
		listener()
	}
}

// LoadSkillsFromDirectory loads all skills from a directory
// Only supports directory format: skill-name/SKILL.md (matching TypeScript behavior)
func (l *SkillLoader) LoadSkillsFromDirectory(dir string, source SkillSource) ([]*SkillDefinition, error) {
	// Check if directory exists
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Directory doesn't exist, return empty
		}
		return nil, fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	// Read directory entries
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var skills []*SkillDefinition
	for _, entry := range entries {
		// Only process directories (skill-name/)
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		skillDir := filepath.Join(dir, skillName)
		skillFile := filepath.Join(skillDir, "SKILL.md")

		// Check if SKILL.md exists
		if _, err := os.Stat(skillFile); err != nil {
			if os.IsNotExist(err) {
				// No SKILL.md in this directory, skip
				continue
			}
			// Other error, log but continue
			continue
		}

		// Load the skill
		skill, err := l.LoadSkillFromFile(skillFile, source)
		if err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "Warning: failed to load skill from %s: %v\n", skillFile, err)
			continue
		}

		if skill != nil {
			// Override skill name with directory name
			skill.Name = skillName

			// Check if skill has paths frontmatter (conditional skill)
			if len(skill.Paths) > 0 {
				l.conditionalSkills.Store(skillName, skill)
			}

			skills = append(skills, skill)
		}
	}

	return skills, nil
}

// LoadSkillFromFile loads a single skill from a file
func (l *SkillLoader) LoadSkillFromFile(path string, source SkillSource) (*SkillDefinition, error) {
	// Get file identity for deduplication
	identity, err := GetFileIdentity(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file identity: %w", err)
	}

	// Check cache
	if cached := l.cache.Get(identity); cached != nil {
		return cached, nil
	}

	// Read file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse file
	parsed, err := ParseSkillFile(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse skill file: %w", err)
	}

	// Extract skill name: use filename (minus extension) as the canonical name.
	// When skills live in subdirectories (e.g. skills/test-skill/SKILL.md),
	// the caller (LoadSkillsFromDir) overrides this with the directory name anyway.
	skillName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	// Build skill definition
	skill := l.buildSkillDefinition(skillName, path, parsed, source, identity)

	// Cache skill
	l.cache.Set(identity, skill)

	return skill, nil
}

// buildSkillDefinition builds a SkillDefinition from parsed data
func (l *SkillLoader) buildSkillDefinition(
	skillName string,
	filePath string,
	parsed *ParsedSkillFile,
	source SkillSource,
	identity string,
) *SkillDefinition {
	fm := parsed.Frontmatter

	// Get description
	description := CoerceDescriptionToString(fm.Description)
	hasUserDescription := description != ""
	if description == "" {
		description = ExtractDescriptionFromMarkdown(parsed.Content)
	}

	// Get display name
	displayName := ""
	if fm.Name != "" {
		displayName = fm.Name
	}

	// Parse fields
	allowedTools := ParseAllowedTools(fm.AllowedTools)
	argumentNames := ParseArgumentNames(fm.Arguments)
	paths := ParsePaths(fm.Paths)
	effort := ParseEffort(fm.Effort)
	shell := ParseShellFrontmatter(fm.Shell)
	allowedEnv, primaryEnv := ParseSkillMetadataEnv(fm.Metadata)

	// Determine execution context
	var execContext ExecutionContext
	if fm.Context == "fork" {
		execContext = ContextFork
	} else {
		execContext = ContextInline
	}

	// Build skill
	skill := &SkillDefinition{
		Name:                        skillName,
		DisplayName:                 displayName,
		Description:                 description,
		HasUserSpecifiedDescription: hasUserDescription,
		WhenToUse:                   fm.WhenToUse,
		ArgumentHint:                fm.ArgumentHint,
		AllowedTools:                allowedTools,
		Model:                       fm.Model,
		DisableModelInvocation:      ParseBooleanFrontmatter(fm.DisableModelInvocation),
		UserInvocable:               ParseBooleanFrontmatter(fm.UserInvocable),
		ExecutionContext:            execContext,
		Agent:                       fm.Agent,
		Effort:                      effort,
		Shell:                       shell,
		AllowedEnv:                  allowedEnv,
		PrimaryEnv:                  primaryEnv,
		Version:                     fm.Version,
		Source:                      source,
		LoadedFrom:                  string(LoadedFromSkills),
		Content:                     parsed.Content,
		ContentLength:               len(parsed.Content),
		ArgumentNames:               argumentNames,
		Paths:                       paths,
		Hooks:                       fm.Hooks,
		LoadedAt:                    time.Now(),
		FileIdentity:                identity,
		SkillRoot:                   filepath.Dir(filePath),
	}

	// Set default user invocable if not specified (matching TypeScript behavior)
	if fm.UserInvocable == nil {
		skill.UserInvocable = true
	}

	// Create prompt generator
	skill.GetPrompt = func(args string, ctx *SkillContext) ([]ContentBlock, error) {
		// Start with base directory prefix if available
		content := skill.Content
		if skill.SkillRoot != "" {
			content = fmt.Sprintf("Base directory for this skill: %s\n\n%s", skill.SkillRoot, content)
		}

		// Substitute arguments in content
		if len(skill.ArgumentNames) > 0 && args != "" {
			content = substituteArguments(content, skill.ArgumentNames, args)
		}

		// Replace ${CLAUDE_SKILL_DIR} with the skill's directory
		if skill.SkillRoot != "" {
			content = strings.ReplaceAll(content, "${CLAUDE_SKILL_DIR}", skill.SkillRoot)
		}

		// Replace ${CLAUDE_SESSION_ID} with session ID if available
		if ctx != nil && ctx.SessionID != "" {
			content = strings.ReplaceAll(content, "${CLAUDE_SESSION_ID}", ctx.SessionID)
		}

		if skill.LoadedFrom != string(LoadedFromMCP) {
			workingDir := skill.SkillRoot
			env := map[string]string{}
			if ctx != nil {
				if strings.TrimSpace(ctx.WorkingDir) != "" {
					workingDir = ctx.WorkingDir
				}
				env = ctx.Environment
			}
			var sandboxRuntime *websandbox.Runtime
			if ctx != nil {
				sandboxRuntime = ctx.WebSandbox
			}
			processed, err := ExecuteShellCommandsInPrompt(content, skill.Shell, workingDir, env, skill.AllowedTools, sandboxRuntime)
			if err != nil {
				return nil, err
			}
			content = processed
		}

		return []ContentBlock{{Type: "text", Text: content}}, nil
	}

	return skill
}

// substituteArguments substitutes named arguments in content
// Supports {{arg}} syntax for named arguments
func substituteArguments(content string, argNames []string, args string) string {
	// Split args by whitespace
	argValues := strings.Fields(args)

	result := content
	for i, name := range argNames {
		placeholder := fmt.Sprintf("{{%s}}", name)
		value := ""
		if i < len(argValues) {
			value = argValues[i]
		}
		result = strings.ReplaceAll(result, placeholder, value)
	}

	return result
}

// DiscoverSkillDirsForPaths discovers skill directories by walking up from file paths
// Only discovers directories below cwd (cwd-level skills are loaded at startup)
func (l *SkillLoader) DiscoverSkillDirsForPaths(filePaths []string, cwd string) ([]string, error) {
	resolvedCwd := strings.TrimSuffix(cwd, string(filepath.Separator))
	var newDirs []string

	for _, filePath := range filePaths {
		// Start from the file's parent directory
		currentDir := filepath.Dir(filePath)

		// Walk up to cwd but NOT including cwd itself
		for strings.HasPrefix(currentDir, resolvedCwd+string(filepath.Separator)) {
			skillDir := filepath.Join(currentDir, ".claude", "skills")

			// Skip if we've already checked this path
			if _, exists := l.dynamicSkillDirs.LoadOrStore(skillDir, true); !exists {
				// First time seeing this directory, check if it exists
				if info, err := os.Stat(skillDir); err == nil && info.IsDir() {
					// TODO: Check if directory is gitignored
					newDirs = append(newDirs, skillDir)
				}
			}

			// Move to parent
			parent := filepath.Dir(currentDir)
			if parent == currentDir {
				break // Reached root
			}
			currentDir = parent
		}
	}

	// Sort by path depth (deepest first)
	sortByDepth(newDirs)

	return newDirs, nil
}

// sortByDepth sorts paths by depth (deepest first)
func sortByDepth(paths []string) {
	// Simple bubble sort by separator count
	for i := range len(paths) {
		for j := i + 1; j < len(paths); j++ {
			depthI := strings.Count(paths[i], string(filepath.Separator))
			depthJ := strings.Count(paths[j], string(filepath.Separator))
			if depthJ > depthI {
				paths[i], paths[j] = paths[j], paths[i]
			}
		}
	}
}

// AddSkillDirectories loads skills from directories and adds them to dynamic skills
func (l *SkillLoader) AddSkillDirectories(dirs []string, source SkillSource) error {
	if len(dirs) == 0 {
		return nil
	}

	// Load skills from all directories
	var allSkills []*SkillDefinition
	for _, dir := range dirs {
		skills, err := l.LoadSkillsFromDirectory(dir, source)
		if err != nil {
			return fmt.Errorf("failed to load from %s: %w", dir, err)
		}
		allSkills = append(allSkills, skills...)
	}

	// Add to dynamic skills (deeper paths override shallower ones)
	for i := len(allSkills) - 1; i >= 0; i-- {
		skill := allSkills[i]
		if len(skill.Paths) > 0 {
			// Conditional skill - store separately
			l.conditionalSkills.Store(skill.Name, skill)
		} else {
			// Regular skill - add to dynamic skills
			l.dynamicSkills.Store(skill.Name, skill)
		}
	}

	// Notify listeners
	l.notifyListeners()

	return nil
}

// GetDynamicSkills returns all dynamically loaded skills
func (l *SkillLoader) GetDynamicSkills() []*SkillDefinition {
	var skills []*SkillDefinition
	l.dynamicSkills.Range(func(key, value any) bool {
		if skill, ok := value.(*SkillDefinition); ok {
			skills = append(skills, skill)
		}
		return true
	})
	return skills
}

// ActivateConditionalSkillsForPaths activates conditional skills matching file paths
func (l *SkillLoader) ActivateConditionalSkillsForPaths(filePaths []string, cwd string) []string {
	var activated []string

	l.conditionalSkills.Range(func(key, value any) bool {
		name := key.(string)
		skill := value.(*SkillDefinition)

		if len(skill.Paths) == 0 {
			return true
		}

		// Check if any file path matches the skill's path patterns
		for _, filePath := range filePaths {
			// Get relative path
			relPath, err := filepath.Rel(cwd, filePath)
			if err != nil || strings.HasPrefix(relPath, "..") {
				continue
			}

			// Check if path matches any pattern
			if matchesPathPattern(relPath, skill.Paths) {
				// Activate skill
				l.dynamicSkills.Store(name, skill)
				l.conditionalSkills.Delete(name)
				l.activatedConditionalSkillNames.Store(name, true)
				activated = append(activated, name)
				return false // Stop checking this skill
			}
		}

		return true
	})

	if len(activated) > 0 {
		l.notifyListeners()
	}

	return activated
}

// matchesPathPattern checks if a path matches any of the given patterns
// Uses simple glob-style matching
func matchesPathPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// Also check if path starts with pattern (for directory patterns)
		if strings.HasPrefix(path, strings.TrimSuffix(pattern, "/**")) {
			return true
		}
	}
	return false
}

// GetConditionalSkillCount returns the number of pending conditional skills
func (l *SkillLoader) GetConditionalSkillCount() int {
	count := 0
	l.conditionalSkills.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// ClearDynamicSkills clears all dynamic skill state
func (l *SkillLoader) ClearDynamicSkills() {
	l.dynamicSkillDirs = sync.Map{}
	l.dynamicSkills = sync.Map{}
	l.conditionalSkills = sync.Map{}
	l.activatedConditionalSkillNames = sync.Map{}
}

// ClearCache clears the skill cache
func (l *SkillLoader) ClearCache() {
	l.cache.Clear()
}

// ReloadSkill reloads a skill from disk
func (l *SkillLoader) ReloadSkill(path string, source SkillSource) (*SkillDefinition, error) {
	// Get file identity
	identity, err := GetFileIdentity(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file identity: %w", err)
	}

	// Remove from cache
	l.cache.Remove(identity)

	// Load fresh
	return l.LoadSkillFromFile(path, source)
}

package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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
		// Process directories and symlinked directories (skill-name/)
		if !isDirectoryEntry(dir, entry) {
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

// LoadCommandsFromDirectory loads legacy command-style skills from a commands directory.
// It supports both command.md files and command/SKILL.md directories. Directory
// SKILL.md files take precedence over sibling markdown files in that directory.
func (l *SkillLoader) LoadCommandsFromDirectory(dir string, source SkillSource) ([]*SkillDefinition, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat commands directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", dir)
	}

	commandDirs := make(map[string]struct{})
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "SKILL.md" {
			commandDirs[filepath.Dir(path)] = struct{}{}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to scan commands directory: %w", err)
	}

	var skills []*SkillDefinition
	for commandDir := range commandDirs {
		commandFile := filepath.Join(commandDir, "SKILL.md")
		relDir, err := filepath.Rel(dir, commandDir)
		if err != nil || relDir == "." || strings.HasPrefix(relDir, "..") {
			continue
		}
		skillName := commandNameFromRelativePath(relDir)
		skill, err := l.LoadSkillFromFile(commandFile, source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load command from %s: %v\n", commandFile, err)
			continue
		}
		l.configureLegacyCommandSkill(skill, skillName)
		skills = append(skills, skill)
	}

	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(entry.Name()) != ".md" || entry.Name() == "SKILL.md" {
			return nil
		}
		if isInsideCommandDir(path, commandDirs) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		namePath := strings.TrimSuffix(rel, filepath.Ext(rel))
		skillName := commandNameFromRelativePath(namePath)
		skill, err := l.LoadSkillFromFile(path, source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load command from %s: %v\n", path, err)
			return nil
		}
		l.configureLegacyCommandSkill(skill, skillName)
		skills = append(skills, skill)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to walk commands directory: %w", err)
	}

	return skills, nil
}

func (l *SkillLoader) configureLegacyCommandSkill(skill *SkillDefinition, name string) {
	if skill == nil {
		return
	}
	skill.Name = name
	skill.LoadedFrom = string(LoadedFromCommands)
	if len(skill.Paths) > 0 {
		l.conditionalSkills.Store(skill.Name, skill)
	}
}

func commandNameFromRelativePath(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "." {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, ":")
}

func isInsideCommandDir(path string, commandDirs map[string]struct{}) bool {
	for commandDir := range commandDirs {
		rel, err := filepath.Rel(commandDir, path)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
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
	model := strings.TrimSpace(fm.Model)
	if strings.EqualFold(model, "inherit") {
		model = ""
	}
	shell := ParseShellFrontmatter(fm.Shell)
	allowedEnv, primaryEnv := ParseSkillMetadataEnv(fm.Metadata)
	metadata := ParseSkillMetadata(fm.Metadata)
	runAsJob := ParseSkillMetadataRunAsJob(fm.Metadata)

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
		Model:                       model,
		DisableModelInvocation:      ParseBooleanFrontmatter(fm.DisableModelInvocation),
		UserInvocable:               ParseBooleanFrontmatter(fm.UserInvocable),
		ExecutionContext:            execContext,
		Agent:                       fm.Agent,
		Effort:                      effort,
		Shell:                       shell,
		AllowedEnv:                  allowedEnv,
		PrimaryEnv:                  primaryEnv,
		RunAsJob:                    runAsJob,
		Version:                     fm.Version,
		Metadata:                    metadata,
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
		if args != "" {
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
			var shellRuntime PromptShellRuntime
			if ctx != nil {
				shellRuntime = ctx.ShellRuntime
				if shellRuntime == nil {
					shellRuntime = ctx.WebSandbox
				}
			}
			var shellTimeout time.Duration
			if ctx != nil {
				shellTimeout = ctx.ShellTimeout
			}
			processed, err := ExecuteShellCommandsInPromptWithTimeout(content, skill.Shell, workingDir, env, skill.AllowedTools, shellRuntime, shellTimeout)
			if err != nil {
				return nil, err
			}
			content = processed
		}

		return []ContentBlock{{Type: "text", Text: content}}, nil
	}

	return skill
}

func isDirectoryEntry(parent string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, entry.Name()))
	return err == nil && info.IsDir()
}

// substituteArguments substitutes named arguments in content
// Supports Claude Code placeholders ($ARGUMENTS, $ARGUMENTS[n], $0, $name)
// and keeps the older {{arg}} form for compatibility with existing Go tests.
func substituteArguments(content string, argNames []string, args string) string {
	argValues := parsePromptArguments(args)
	argByName := make(map[string]string, len(argNames))
	for i, name := range argNames {
		if i < len(argValues) {
			argByName[name] = argValues[i]
		} else {
			argByName[name] = ""
		}
	}

	usedPlaceholder := false
	placeholderPattern := regexp.MustCompile(`\$ARGUMENTS\[\d+\]|\$ARGUMENTS|\$\d+|\$[A-Za-z_][A-Za-z0-9_]*`)
	result := placeholderPattern.ReplaceAllStringFunc(content, func(token string) string {
		switch {
		case token == "$ARGUMENTS":
			usedPlaceholder = true
			return args
		case strings.HasPrefix(token, "$ARGUMENTS["):
			indexText := strings.TrimSuffix(strings.TrimPrefix(token, "$ARGUMENTS["), "]")
			index, err := strconv.Atoi(indexText)
			if err != nil || index < 0 || index >= len(argValues) {
				usedPlaceholder = true
				return ""
			}
			usedPlaceholder = true
			return argValues[index]
		case len(token) > 1 && token[1] >= '0' && token[1] <= '9':
			index, err := strconv.Atoi(token[1:])
			if err != nil || index < 0 || index >= len(argValues) {
				usedPlaceholder = true
				return ""
			}
			usedPlaceholder = true
			return argValues[index]
		default:
			name := token[1:]
			if value, ok := argByName[name]; ok {
				usedPlaceholder = true
				return value
			}
			return token
		}
	})

	// Preserve the original Go-only placeholder format to avoid breaking local skills.
	for i, name := range argNames {
		placeholder := fmt.Sprintf("{{%s}}", name)
		if !strings.Contains(result, placeholder) {
			continue
		}
		usedPlaceholder = true
		value := ""
		if i < len(argValues) {
			value = argValues[i]
		}
		result = strings.ReplaceAll(result, placeholder, value)
	}

	if !usedPlaceholder && strings.TrimSpace(args) != "" {
		separator := "\n\n"
		if strings.HasSuffix(result, "\n") {
			separator = "\n"
		}
		result += separator + "ARGUMENTS: " + args
	}

	return result
}

func parsePromptArguments(args string) []string {
	var result []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		result = append(result, current.String())
		current.Reset()
	}

	for _, r := range args {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		case ' ', '\t', '\n', '\r':
			if !inSingle && !inDouble {
				flush()
				continue
			}
		}
		current.WriteRune(r)
	}
	if escaped {
		current.WriteRune('\\')
	}
	flush()
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

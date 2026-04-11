package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// BundledSkillRegistry manages built-in skills that ship with the CLI
type BundledSkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]*SkillDefinition
}

// Global bundled skills registry
var bundledRegistry = &BundledSkillRegistry{
	skills: make(map[string]*SkillDefinition),
}

// RegisterBundledSkill registers a built-in skill
// This should be called during application initialization
func RegisterBundledSkill(skill *SkillDefinition) error {
	if skill == nil {
		return fmt.Errorf("skill cannot be nil")
	}

	if skill.Name == "" {
		return ErrInvalidSkillName
	}

	// Set source
	skill.Source = SourceBundled
	skill.LoadedFrom = "bundled"

	// If skill has reference files, set up extraction
	if len(skill.Files) > 0 {
		skill.SkillRoot = GetBundledSkillExtractDir(skill.Name)

		// Wrap the prompt generator to extract files on first use
		originalGenerator := skill.GetPrompt
		if originalGenerator != nil {
			var extractOnce sync.Once
			var extractErr error

			skill.GetPrompt = func(args string, ctx *SkillContext) ([]ContentBlock, error) {
				// Extract files once
				extractOnce.Do(func() {
					extractErr = ExtractBundledSkillFiles(skill.Name, skill.Files)
				})

				if extractErr != nil {
					// Continue without base directory prefix if extraction failed
					return originalGenerator(args, ctx)
				}

				// Generate prompt
				blocks, err := originalGenerator(args, ctx)
				if err != nil {
					return nil, err
				}

				// Prepend base directory
				return prependBaseDir(blocks, skill.SkillRoot), nil
			}
		}
	}

	bundledRegistry.mu.Lock()
	defer bundledRegistry.mu.Unlock()

	// Check for conflicts
	if _, exists := bundledRegistry.skills[skill.Name]; exists {
		return fmt.Errorf("bundled skill '%s' already registered", skill.Name)
	}

	bundledRegistry.skills[skill.Name] = skill
	return nil
}

// GetBundledSkills returns all registered bundled skills
func GetBundledSkills() []*SkillDefinition {
	bundledRegistry.mu.RLock()
	defer bundledRegistry.mu.RUnlock()

	skills := make([]*SkillDefinition, 0, len(bundledRegistry.skills))
	for _, skill := range bundledRegistry.skills {
		// Return a copy to prevent external mutation
		skillCopy := *skill
		skills = append(skills, &skillCopy)
	}

	return skills
}

// GetBundledSkill returns a specific bundled skill by name
func GetBundledSkill(name string) (*SkillDefinition, bool) {
	bundledRegistry.mu.RLock()
	defer bundledRegistry.mu.RUnlock()

	skill, ok := bundledRegistry.skills[name]
	if !ok {
		return nil, false
	}

	// Return a copy
	skillCopy := *skill
	return &skillCopy, true
}

// ClearBundledSkills clears all bundled skills (for testing)
func ClearBundledSkills() {
	bundledRegistry.mu.Lock()
	defer bundledRegistry.mu.Unlock()

	bundledRegistry.skills = make(map[string]*SkillDefinition)
}

// GetBundledSkillExtractDir returns the extraction directory for a bundled skill
func GetBundledSkillExtractDir(skillName string) string {
	// Get bundled skills root (typically ~/.claude/bundled-skills/<nonce>)
	root := getBundledSkillsRoot()
	return filepath.Join(root, SanitizeSkillName(skillName))
}

// ExtractBundledSkillFiles extracts a bundled skill's reference files to disk
func ExtractBundledSkillFiles(skillName string, files map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	dir := GetBundledSkillExtractDir(skillName)

	// Write files safely
	if err := WriteSkillFiles(dir, files); err != nil {
		return fmt.Errorf("failed to extract bundled skill '%s': %w", skillName, err)
	}

	return nil
}

// getBundledSkillsRoot returns the root directory for bundled skill extraction
// This uses a per-process nonce for security
func getBundledSkillsRoot() string {
	// TODO: Implement proper bundled skills root with nonce
	// For now, use a simple path
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude", "bundled-skills")
}

// prependBaseDir prepends a base directory message to content blocks
func prependBaseDir(blocks []ContentBlock, baseDir string) []ContentBlock {
	prefix := fmt.Sprintf("Base directory for this skill: %s\n\n", baseDir)

	if len(blocks) == 0 {
		return []ContentBlock{{Type: "text", Text: prefix}}
	}

	// Prepend to first text block
	if blocks[0].Type == "text" {
		blocks[0].Text = prefix + blocks[0].Text
		return blocks
	}

	// Insert at beginning
	return append([]ContentBlock{{Type: "text", Text: prefix}}, blocks...)
}

// Helper to create a simple text-based skill
func NewSimpleSkill(name, description, content string) *SkillDefinition {
	return &SkillDefinition{
		Name:                        name,
		Description:                 description,
		HasUserSpecifiedDescription: true,
		Content:                     content,
		ContentLength:               len(content),
		UserInvocable:               true,
		Source:                      SourceBundled,
		LoadedFrom:                  "bundled",
		GetPrompt: func(args string, ctx *SkillContext) ([]ContentBlock, error) {
			text := content
			if args != "" {
				text = fmt.Sprintf("%s\n\nArguments: %s", content, args)
			}
			return []ContentBlock{{Type: "text", Text: text}}, nil
		},
	}
}

// Helper to create a skill with custom prompt generator
func NewCustomSkill(name, description string, generator PromptGenerator) *SkillDefinition {
	return &SkillDefinition{
		Name:                        name,
		Description:                 description,
		HasUserSpecifiedDescription: true,
		UserInvocable:               true,
		Source:                      SourceBundled,
		LoadedFrom:                  "bundled",
		GetPrompt:                   generator,
	}
}

package cli

import (
	"context"
	"fmt"
	"strings"

	"claude-codex/internal/harness/skills"
)

// SkillCommandRegistry wraps SkillManager to provide CommandRegistry interface
type SkillCommandRegistry struct {
	skillManager *skills.SkillManager
	workingDir   string
}

// NewSkillCommandRegistry creates a new skill command registry
func NewSkillCommandRegistry(skillManager *skills.SkillManager, workingDir string) *SkillCommandRegistry {
	return &SkillCommandRegistry{
		skillManager: skillManager,
		workingDir:   workingDir,
	}
}

// List returns all user-invocable skills as commands
func (r *SkillCommandRegistry) List() []*Command {
	userSkills := r.skillManager.ListUserInvocableSkills()
	commands := make([]*Command, 0, len(userSkills)+1)

	// Add the /skills command itself
	commands = append(commands, &Command{
		Name:        "/skills",
		Description: "List all available skills",
		Usage:       "",
	})

	for _, skill := range userSkills {
		cmd := &Command{
			Name:        "/" + skill.Name,
			Description: skill.Description,
			Usage:       skill.ArgumentHint,
		}
		commands = append(commands, cmd)
	}

	return commands
}

// Get retrieves a skill as a command
func (r *SkillCommandRegistry) Get(name string) (*Command, bool) {
	// Remove leading slash if present
	skillName := strings.TrimPrefix(name, "/")

	// Special command: /skills
	if skillName == "skills" {
		return &Command{
			Name:        "/skills",
			Description: "List all available skills",
			Usage:       "",
		}, true
	}

	skill, ok := r.skillManager.GetSkill(skillName)
	if !ok {
		return nil, false
	}

	if !skill.UserInvocable {
		return nil, false
	}

	return &Command{
		Name:        "/" + skill.Name,
		Description: skill.Description,
		Usage:       skill.ArgumentHint,
	}, true
}

// Execute runs a skill command
func (r *SkillCommandRegistry) Execute(ctx context.Context, name string, args []string, slash slashContext) error {
	// Remove leading slash if present
	skillName := strings.TrimPrefix(name, "/")

	// Special command: /skills - list all available skills
	if skillName == "skills" {
		return r.handleListSkills(slash)
	}

	// Get skill
	skill, ok := r.skillManager.GetSkill(skillName)
	if !ok {
		return fmt.Errorf("unknown skill: /%s. Use /skills to list available skills", skillName)
	}

	// Check if user can invoke this skill
	if !skill.UserInvocable {
		return fmt.Errorf("skill /%s is not user-invocable", skillName)
	}

	// Generate prompt from skill
	argsStr := strings.Join(args, " ")
	blocks, err := skill.GetPrompt(argsStr, &skills.SkillContext{
		SessionID:  "", // Will be set by engine
		WorkingDir: r.workingDir,
	})
	if err != nil {
		return fmt.Errorf("failed to generate skill prompt: %v", err)
	}

	// Convert blocks to text
	var promptText string
	for _, block := range blocks {
		if block.Type == "text" {
			promptText += block.Text
		}
	}
	promptText = skills.WrapGeneratedSkillPrompt(skill.Name, argsStr, promptText)

	// Return a special marker that tells TUI to send this as a prompt to AI
	// Format: __SKILL_PROMPT__<prompt>
	fmt.Fprintf(slash.streams.Out, "__SKILL_PROMPT__%s", promptText)
	return nil
}

func (r *SkillCommandRegistry) MatchNaturalPrompt(prompt string) (*skills.SkillDefinition, bool) {
	if r == nil || r.skillManager == nil {
		return nil, false
	}
	return r.skillManager.MatchUserInvocableSkill(prompt)
}

// handleListSkills lists all available skills
func (r *SkillCommandRegistry) handleListSkills(slash slashContext) error {
	userSkills := r.skillManager.ListUserInvocableSkills()

	if len(userSkills) == 0 {
		fmt.Fprintln(slash.streams.Out, "No skills available.")
		return nil
	}

	// Build skills list with better formatting
	var output strings.Builder
	output.WriteString("# Available Skills\n\n")

	// Group by source
	bundledSkills := make([]*skills.SkillDefinition, 0)
	fileSkills := make([]*skills.SkillDefinition, 0)
	otherSkills := make([]*skills.SkillDefinition, 0)

	for _, skill := range userSkills {
		switch skill.Source {
		case skills.SourceBundled:
			bundledSkills = append(bundledSkills, skill)
		case skills.SourceFile:
			fileSkills = append(fileSkills, skill)
		default:
			otherSkills = append(otherSkills, skill)
		}
	}

	// Display bundled skills
	if len(bundledSkills) > 0 {
		output.WriteString("## Built-in Skills\n\n")
		for _, skill := range bundledSkills {
			r.formatSkill(&output, skill)
		}
		output.WriteString("\n")
	}

	// Display file-based skills
	if len(fileSkills) > 0 {
		output.WriteString("## Custom Skills\n\n")
		for _, skill := range fileSkills {
			r.formatSkill(&output, skill)
		}
		output.WriteString("\n")
	}

	// Display other skills
	if len(otherSkills) > 0 {
		output.WriteString("## Other Skills\n\n")
		for _, skill := range otherSkills {
			r.formatSkill(&output, skill)
		}
		output.WriteString("\n")
	}

	// Summary
	stats := r.skillManager.GetStats()
	output.WriteString("---\n\n")
	output.WriteString(fmt.Sprintf("**Total:** %d skills | **Bundled:** %d | **Custom:** %d | **User-invocable:** %d\n\n",
		stats.TotalSkills, stats.BundledSkills, stats.DynamicSkills, stats.UserInvocable))
	output.WriteString("Type `/skillname` to use a skill, or `/skillname args` to pass arguments.\n")

	fmt.Fprint(slash.streams.Out, output.String())
	return nil
}

// formatSkill formats a single skill for display
func (r *SkillCommandRegistry) formatSkill(output *strings.Builder, skill *skills.SkillDefinition) {
	// Skill name
	output.WriteString(fmt.Sprintf("### `/%s`", skill.Name))
	if skill.DisplayName != "" && skill.DisplayName != skill.Name {
		output.WriteString(fmt.Sprintf(" - %s", skill.DisplayName))
	}
	output.WriteString("\n\n")

	// Description
	if skill.Description != "" {
		// Truncate long descriptions
		desc := skill.Description
		if len(desc) > 150 {
			desc = desc[:147] + "..."
		}
		output.WriteString(fmt.Sprintf("%s\n\n", desc))
	}

	// Usage hint
	if skill.ArgumentHint != "" {
		output.WriteString(fmt.Sprintf("**Usage:** `/%s %s`\n\n", skill.Name, skill.ArgumentHint))
	} else if len(skill.ArgumentNames) > 0 {
		output.WriteString(fmt.Sprintf("**Usage:** `/%s %s`\n\n", skill.Name, strings.Join(skill.ArgumentNames, " ")))
	}

	// Source info (only for file-based skills, show path)
	if skill.Source == skills.SourceFile && skill.LoadedFrom != "" {
		// Show relative path if possible
		relPath := skill.LoadedFrom
		if strings.HasPrefix(relPath, r.workingDir) {
			relPath = strings.TrimPrefix(relPath, r.workingDir)
			relPath = strings.TrimPrefix(relPath, "/")
		}
		output.WriteString(fmt.Sprintf("*Source: %s*\n\n", relPath))
	}
}

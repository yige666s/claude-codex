package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	toolkit "claude-codex/internal/harness/tools"
)

const ToolName = "Skill"

type Tool struct {
	skillManager *skills.SkillManager
}

type Input struct {
	Skill string `json:"skill"`
	Args  string `json:"args,omitempty"`
}

// Output represents the result of skill execution
type Output struct {
	Success      bool     `json:"success"`
	CommandName  string   `json:"commandName"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	Model        string   `json:"model,omitempty"`
	Status       string   `json:"status,omitempty"`  // "inline" or "forked"
	AgentID      string   `json:"agentId,omitempty"` // For forked skills
	Result       string   `json:"result,omitempty"`  // For forked skills
}

func NewTool(skillManager *skills.SkillManager) toolkit.Tool {
	return &Tool{
		skillManager: skillManager,
	}
}

func (t *Tool) Name() string {
	return ToolName
}

func (t *Tool) Description() string {
	// Note: Skill listing is now handled by the attachment mechanism
	// This description only provides basic usage instructions
	var desc strings.Builder
	desc.WriteString("Execute a skill within the main conversation\n\n")
	desc.WriteString("When users ask you to perform tasks, check if any of the available skills match. ")
	desc.WriteString("Skills provide specialized capabilities and domain knowledge.\n\n")
	desc.WriteString("When users reference a \"slash command\" or \"/<something>\" (e.g., \"/commit\", \"/review-pr\"), ")
	desc.WriteString("they are referring to a skill. Use this tool to invoke it.\n\n")
	desc.WriteString("How to invoke:\n")
	desc.WriteString("- Use this tool with the skill name and optional arguments\n")
	desc.WriteString("- Examples:\n")
	desc.WriteString("  - `skill: \"pdf\"` - invoke the pdf skill\n")
	desc.WriteString("  - `skill: \"commit\", args: \"-m 'Fix bug'\"` - invoke with arguments\n")
	desc.WriteString("  - `skill: \"review-pr\", args: \"123\"` - invoke with arguments\n")
	desc.WriteString("  - `skill: \"ms-office-suite:pdf\"` - invoke using fully qualified name\n\n")
	desc.WriteString("Important:\n")
	desc.WriteString("- Available skills are listed in system-reminder messages in the conversation\n")
	desc.WriteString("- When a skill matches the user's request, this is a BLOCKING REQUIREMENT: invoke the relevant Skill tool BEFORE generating any other response about the task\n")
	desc.WriteString("- NEVER mention a skill without actually calling this tool\n")
	desc.WriteString("- Do not invoke a skill that is already running\n")
	desc.WriteString("- Do not use this tool for built-in CLI commands (like /help, /clear, etc.)\n")
	desc.WriteString("- If you see a <command-name> tag in the current conversation turn, the skill has ALREADY been loaded - follow the instructions directly instead of calling this tool again\n")

	return desc.String()
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"skill": {
				"type": "string",
				"description": "The skill name. E.g. \"commit\", \"review-pr\", or \"pdf\""
			},
			"args": {
				"type": "string",
				"description": "Optional arguments for the skill"
			}
		},
		"required": ["skill"]
	}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelRead
}

func (t *Tool) IsConcurrencySafe() bool {
	return true
}

func (t *Tool) Execute(ctx context.Context, rawInput json.RawMessage) (toolkit.Result, error) {
	var input Input
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return toolkit.Result{}, fmt.Errorf("invalid input: %w", err)
	}

	// Normalize skill name (remove leading slash if present)
	skillName := strings.TrimPrefix(strings.TrimSpace(input.Skill), "/")

	// Get skill
	skill, ok := t.skillManager.GetSkill(skillName)
	if !ok {
		return toolkit.Result{}, fmt.Errorf("unknown skill: %s", skillName)
	}

	// Check if user can invoke this skill
	if !skill.UserInvocable {
		return toolkit.Result{}, fmt.Errorf("skill %s is not user-invocable", skillName)
	}

	// Check if skill should run in fork mode
	if skill.ExecutionContext == skills.ContextFork {
		// TODO: Implement forked skill execution
		// For now, return error indicating fork mode is not yet supported
		return toolkit.Result{}, fmt.Errorf("forked skill execution not yet implemented for skill: %s", skillName)
	}

	// Generate prompt from skill (inline mode)
	blocks, err := skill.GetPrompt(input.Args, &skills.SkillContext{
		SessionID:  "", // Will be set by engine
		WorkingDir: "",
	})
	if err != nil {
		return toolkit.Result{}, fmt.Errorf("failed to generate skill prompt: %w", err)
	}

	// Convert blocks to text using strings.Builder
	var promptBuilder strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			promptBuilder.WriteString(block.Text)
		}
	}

	promptText := promptBuilder.String()
	promptText = skills.WrapGeneratedSkillPrompt(skillName, input.Args, promptText)

	return toolkit.Result{
		Output: promptText,
	}, nil
}

package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	toolkit "claude-codex/internal/harness/tools"
	agenttool "claude-codex/internal/harness/tools/agent"
)

const ToolName = "Skill"
const RunAsJobMarkerPrefix = "agentapi_run_as_job:"

var ErrRunAsJobRequired = errors.New("skill requires job execution")

type Tool struct {
	skillManager  *skills.SkillManager
	defaultDir    string
	runSubagent   agenttool.Runner
	routeRunAsJob bool
}

type Options struct {
	DefaultDir    string
	RunSubagent   agenttool.Runner
	RouteRunAsJob bool
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

func NewToolWithRunner(skillManager *skills.SkillManager, defaultDir string, runSubagent agenttool.Runner) toolkit.Tool {
	return NewToolWithOptions(skillManager, Options{
		DefaultDir:  defaultDir,
		RunSubagent: runSubagent,
	})
}

func NewToolWithOptions(skillManager *skills.SkillManager, options Options) toolkit.Tool {
	return &Tool{
		skillManager:  skillManager,
		defaultDir:    options.DefaultDir,
		runSubagent:   options.RunSubagent,
		routeRunAsJob: options.RouteRunAsJob,
	}
}

func IsRunAsJobMarker(output string) bool {
	return strings.HasPrefix(strings.TrimSpace(output), RunAsJobMarkerPrefix)
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
	if t.routeRunAsJob && skill.RunAsJob {
		payload, err := json.Marshal(struct {
			Skill    string `json:"skill"`
			Args     string `json:"args,omitempty"`
			RunAsJob bool   `json:"run_as_job"`
		}{
			Skill:    skillName,
			Args:     input.Args,
			RunAsJob: true,
		})
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: RunAsJobMarkerPrefix + string(payload)}, nil
	}

	environment := map[string]string{}
	if strings.TrimSpace(t.defaultDir) != "" {
		environment["AGENT_WORKSPACE_DIR"] = t.defaultDir
	}
	blocks, err := skill.GetPrompt(input.Args, &skills.SkillContext{
		SessionID:   "", // Will be set by engine
		WorkingDir:  t.defaultDir,
		Environment: environment,
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

	if skill.ExecutionContext == skills.ContextFork {
		if t.runSubagent == nil {
			return toolkit.Result{}, fmt.Errorf("forked skill execution requires a configured subagent runner for skill: %s", skillName)
		}
		output, err := t.runSubagent(ctx, agenttool.Request{
			Prompt:       promptText,
			Description:  skill.Description,
			SubagentType: strings.TrimSpace(skill.Agent),
			Model:        strings.TrimSpace(skill.Model),
			WorkingDir:   t.defaultDir,
		})
		if err != nil {
			return toolkit.Result{}, err
		}
		return toolkit.Result{Output: output}, nil
	}

	return toolkit.Result{
		Output: promptText,
	}, nil
}

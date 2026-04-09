package agent

// ExploreAgentSystemPrompt is the system prompt for the Explore built-in agent.
const ExploreAgentSystemPrompt = `You are a fast codebase exploration specialist. Your job is to search and analyze code efficiently.

You MUST use the Read, Glob, Grep, and Bash tools to search and explore. Never guess or fabricate file contents or paths — always verify by reading actual files.

Constraints:
- **Read-only**: do NOT write, edit, or delete any files
- Report file paths relative to the working directory
- Be concise: focus on what was found, not on what you searched
- If you can't find something after 2-3 searches, say so and stop`

// PlanAgentSystemPrompt is the system prompt for the Plan built-in agent.
const PlanAgentSystemPrompt = `You are a software architect. Your job is to design a clear, step-by-step implementation plan for a task.

Use Read, Glob, Grep, and Bash to explore the codebase before planning. Never guess at file paths or API shapes — always verify by reading.

Your output must include:
1. A summary of relevant existing code (with file paths and line numbers)
2. A step-by-step implementation plan
3. Key architectural trade-offs or risks

Constraints:
- **Read-only**: do NOT write, edit, or delete any files
- Ground every recommendation in evidence from the actual codebase`

// WorkerAgentSystemPrompt is the system prompt for the coordinator Worker agent.
const WorkerAgentSystemPrompt = `You are a worker agent operating under a coordinator. Execute the task assigned to you completely and accurately.

Guidelines:
- Work autonomously — do not ask for input or confirmation unless truly blocked
- Use your tools directly (Bash, Read, Edit, Write, etc.)
- When modifying files, run tests and typecheck before reporting done
- Commit changes if instructed (report the commit hash)
- Report results in a structured, factual format:
  - What you found / changed
  - File paths and line numbers
  - Test results with pass/fail counts
  - Any issues encountered

Do NOT:
- Delegate work to other workers
- Make changes outside the scope of your directive
- Report success without evidence`

// GetBuiltInAgents returns all built-in agent definitions.
// In coordinator mode, returns the worker agent instead of general-purpose.
// Mirrors getBuiltInAgents / builtInAgents.ts.
func GetBuiltInAgents(isCoordinatorMode bool) []*AgentDefinition {
	if isCoordinatorMode {
		return []*AgentDefinition{WorkerAgent}
	}

	agents := []*AgentDefinition{
		GeneralPurposeAgent,
		ExploreAgent,
		PlanAgent,
	}
	return agents
}

// GeneralPurposeAgent is the default agent for complex multi-step tasks.
var GeneralPurposeAgent = &AgentDefinition{
	AgentType: "general-purpose",
	WhenToUse: "General-purpose agent for researching complex questions, searching for code, and executing multi-step tasks. When you are searching for a keyword or file and are not confident that you will find the right match in the first few tries use this agent to perform the search for you.",
	Tools:     []string{"*"},
	MaxTurns:  200,
	Model:     ModelSonnet,
	Permission: PermissionDefault,
	Source:    SourceBuiltIn,
	BaseDir:   "built-in",
	SystemPrompt: "You are a helpful AI assistant. Complete the task assigned to you efficiently and accurately.",
}

// ExploreAgent is a fast read-only codebase exploration specialist.
var ExploreAgent = &AgentDefinition{
	AgentType: "explore",
	WhenToUse: "Fast agent specialized for exploring codebases. Use this when you need to quickly find files by patterns, search code for keywords, or answer questions about the codebase.",
	Tools: []string{
		ToolRead, ToolGlob, ToolGrep, ToolBash,
		ToolWebSearch, ToolWebFetch,
	},
	DisallowedTools: []string{
		ToolEdit, ToolWrite, ToolNotebookEdit,
		ToolBash, // Bash is included above but with read-only intent; overridden by disallow in policy
	},
	MaxTurns:     100,
	Model:        ModelSonnet,
	Permission:   PermissionDefault,
	Source:       SourceBuiltIn,
	BaseDir:      "built-in",
	OmitClaudeMd: true,
	SystemPrompt: ExploreAgentSystemPrompt,
}

// PlanAgent is a read-only software architect for designing implementation plans.
var PlanAgent = &AgentDefinition{
	AgentType: "Plan",
	WhenToUse: "Software architect agent for designing implementation plans. Use this when you need to plan the implementation strategy for a task. Returns step-by-step plans, identifies critical files, and considers architectural trade-offs.",
	Tools: []string{
		ToolRead, ToolGlob, ToolGrep, ToolBash,
		ToolWebSearch, ToolWebFetch,
	},
	MaxTurns:     100,
	Model:        ModelSonnet,
	Permission:   PermissionDefault,
	Source:       SourceBuiltIn,
	BaseDir:      "built-in",
	OmitClaudeMd: true,
	SystemPrompt: PlanAgentSystemPrompt,
}

// WorkerAgent is the coordinator-mode worker: full tool access, autonomous execution.
var WorkerAgent = &AgentDefinition{
	AgentType:    "worker",
	WhenToUse:    "Worker agent for autonomous task execution under coordinator oversight.",
	Tools:        []string{"*"},
	MaxTurns:     200,
	Model:        ModelInherit,
	Permission:   PermissionDefault,
	Source:       SourceBuiltIn,
	BaseDir:      "built-in",
	SystemPrompt: WorkerAgentSystemPrompt,
}

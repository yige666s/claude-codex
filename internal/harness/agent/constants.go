package agent

// Tool name constants matching TypeScript constants/tools.ts
const (
	ToolAgent           = "Agent"
	ToolAskUserQuestion = "AskUserQuestion"
	ToolBash            = "Bash"
	ToolCronCreate      = "CronCreate"
	ToolCronDelete      = "CronDelete"
	ToolCronList        = "CronList"
	ToolEdit            = "Edit"
	ToolEnterPlanMode   = "EnterPlanMode"
	ToolEnterWorktree   = "EnterWorktree"
	ToolExitPlanMode    = "ExitPlanMode"
	ToolExitWorktree    = "ExitWorktree"
	ToolGlob            = "Glob"
	ToolGrep            = "Grep"
	ToolLSP             = "LSP"
	ToolMCP             = "MCP"
	ToolNotebookEdit    = "NotebookEdit"
	ToolREPL            = "REPLTool"
	ToolRead            = "Read"
	ToolSendMessage     = "SendMessage"
	ToolSkill           = "Skill"
	ToolSyntheticOutput = "SyntheticOutput"
	ToolTaskCreate      = "TaskCreate"
	ToolTaskGet         = "TaskGet"
	ToolTaskList        = "TaskList"
	ToolTaskOutput      = "TaskOutput"
	ToolTaskStop        = "TaskStop"
	ToolTaskUpdate      = "TaskUpdate"
	ToolTeamCreate      = "TeamCreate"
	ToolTeamDelete      = "TeamDelete"
	ToolToolSearch      = "ToolSearch"
	ToolWebFetch        = "WebFetch"
	ToolWebSearch       = "WebSearch"
	ToolWrite           = "Write"
)

// AllAgentDisallowedTools is the set of tools never available to any subagent.
// Mirrors ALL_AGENT_DISALLOWED_TOOLS in TypeScript.
var AllAgentDisallowedTools = map[string]bool{
	ToolTaskOutput:    true,
	ToolExitPlanMode:  true,
	ToolEnterPlanMode: true,
	ToolAgent:         true, // blocked to prevent runaway recursion
	ToolAskUserQuestion: true,
	ToolTaskStop:      true,
}

// CustomAgentDisallowedTools are additional restrictions for user-defined (non-built-in) agents.
// Mirrors CUSTOM_AGENT_DISALLOWED_TOOLS in TypeScript.
var CustomAgentDisallowedTools = map[string]bool{
	ToolTaskOutput:    true,
	ToolExitPlanMode:  true,
	ToolEnterPlanMode: true,
	ToolAgent:         true,
	ToolAskUserQuestion: true,
	ToolTaskStop:      true,
}

// AsyncAgentAllowedTools is the core set for ASYNC (background) agents.
// Mirrors ASYNC_AGENT_ALLOWED_TOOLS in TypeScript.
var AsyncAgentAllowedTools = map[string]bool{
	ToolRead:            true,
	ToolWebSearch:       true,
	ToolGrep:            true,
	ToolWebFetch:        true,
	ToolGlob:            true,
	ToolBash:            true,
	ToolREPL:            true,
	ToolEdit:            true,
	ToolWrite:           true,
	ToolNotebookEdit:    true,
	ToolSkill:           true,
	ToolSyntheticOutput: true,
	ToolToolSearch:      true,
	ToolEnterWorktree:   true,
	ToolExitWorktree:    true,
}

// InProcessTeammateAllowedTools are extra tools for in-process teammates.
// Mirrors IN_PROCESS_TEAMMATE_ALLOWED_TOOLS in TypeScript.
var InProcessTeammateAllowedTools = map[string]bool{
	ToolTaskCreate:  true,
	ToolTaskGet:     true,
	ToolTaskList:    true,
	ToolTaskUpdate:  true,
	ToolSendMessage: true,
	// Cron tools added when AGENT_TRIGGERS feature enabled:
	ToolCronCreate: true,
	ToolCronDelete: true,
	ToolCronList:   true,
}

// CoordinatorModeAllowedTools is what the COORDINATOR itself can use (not workers).
// Mirrors COORDINATOR_MODE_ALLOWED_TOOLS in TypeScript.
var CoordinatorModeAllowedTools = map[string]bool{
	ToolAgent:           true,
	ToolTaskStop:        true,
	ToolSendMessage:     true,
	ToolSyntheticOutput: true,
}

// InternalWorkerTools are tools that should NOT be exposed to workers
// (they belong to the coordinator or team management layer).
var InternalWorkerTools = map[string]bool{
	ToolTeamCreate:      true,
	ToolTeamDelete:      true,
	ToolSendMessage:     true,
	ToolSyntheticOutput: true,
}

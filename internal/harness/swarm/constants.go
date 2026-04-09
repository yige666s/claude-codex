package swarm

const (
	// TeamLeadName is the reserved name for the team leader.
	TeamLeadName = "team-lead"

	// SwarmSessionName is the tmux session name for swarm mode.
	SwarmSessionName = "claude-swarm"

	// SwarmViewWindowName is the tmux window name for the swarm view.
	SwarmViewWindowName = "swarm-view"

	// InProcessMarker is stored as tmuxPaneId for in-process teammates.
	InProcessMarker = "in-process"
)

// Environment variables used by the swarm system.
const (
	EnvTeammateCommand  = "CLAUDE_CODE_TEAMMATE_COMMAND"
	EnvTeammateColor    = "CLAUDE_CODE_AGENT_COLOR"
	EnvPlanModeRequired = "CLAUDE_CODE_PLAN_MODE_REQUIRED"
	EnvClaudeCode       = "CLAUDECODE"
	EnvAgentTeams       = "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"
)

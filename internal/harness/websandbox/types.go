package websandbox

import "time"

const (
	containerSkillRoot = "/workspace/skill"
	containerOutputDir = "/workspace/output"
	defaultImage       = "claude-codex-websandbox:latest"
)

type Scope struct {
	RootDir        string
	SessionID      string
	SkillName      string
	SkillScoped    bool
	AllowedTools   []string
	AllowedEnv     []string
	PrimaryEnv     string
	AllowedDomains []string
}

type RuntimeOptions struct {
	Image          string
	Timeout        time.Duration
	NetworkEnabled bool
	AutoBuildImage bool
	Runner         CommandRunner
	IdentityBroker *IdentityBroker
	AuditSink      AuditSink
	OutputBaseDir  string
}

type ActionType string

const (
	ActionExecuteScript ActionType = "execute_script"
	ActionListScripts   ActionType = "list_scripts"
	ActionFindScripts   ActionType = "find_scripts"
)

type Action struct {
	RawCommand string
	Type       ActionType
	Env        map[string]string
	Binary     string
	Args       []string
}

type Lease struct {
	ID        string
	AgentID   string
	TaskID    string
	Scopes    []string
	Env       map[string]string
	ExpiresAt time.Time
}

type AuditEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	Event     string            `json:"event"`
	SessionID string            `json:"session_id,omitempty"`
	SkillName string            `json:"skill_name,omitempty"`
	AgentID   string            `json:"agent_id,omitempty"`
	TaskID    string            `json:"task_id,omitempty"`
	Command   string            `json:"command,omitempty"`
	Action    string            `json:"action,omitempty"`
	Image     string            `json:"image,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Package swarm implements the in-process multi-agent swarm system.
// It mirrors the TypeScript backends/types.ts and related swarm utilities.
package swarm

import (
	"time"

	"claude-codex/internal/harness/permissions"
)

// BackendType identifies the execution backend for a teammate.
type BackendType string

const (
	BackendTypeInProcess BackendType = "in-process"
	BackendTypeTmux      BackendType = "tmux"
	BackendTypeITerm2    BackendType = "iterm2"
)

// AgentColorName is a terminal color name for a teammate.
type AgentColorName = string

// TeammateIdentity holds the identity fields for a teammate.
type TeammateIdentity struct {
	Name             string
	TeamName         string
	Color            AgentColorName
	PlanModeRequired bool
}

// AgentID is the formatted "name@team" identifier.
type AgentID string

// FormatAgentID creates an AgentID from name and team.
func FormatAgentID(name, teamName string) AgentID {
	return AgentID(name + "@" + teamName)
}

// ParseAgentID splits an AgentID into name and team parts.
func ParseAgentID(id AgentID) (name, teamName string) {
	s := string(id)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '@' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// TeammateSpawnConfig holds full spawn parameters for a teammate.
type TeammateSpawnConfig struct {
	TeammateIdentity
	Prompt                 string
	CWD                    string
	Model                  string
	SystemPrompt           string
	SystemPromptMode       string // "default" | "replace" | "append"
	WorktreePath           string
	ParentSessionID        string
	Permissions            []string
	AllowPermissionPrompts bool
}

// TeammateSpawnResult is returned after spawning a teammate.
type TeammateSpawnResult struct {
	Success bool
	AgentID AgentID
	Error   string
	TaskID  string // in-process only
}

// TeammateMessage is a message sent between teammates.
type TeammateMessage struct {
	Text      string
	From      string
	Color     string
	Timestamp time.Time
	Summary   string
}

// TeammateStatus tracks the running state of an in-process teammate.
type TeammateStatus string

const (
	StatusRunning   TeammateStatus = "running"
	StatusCompleted TeammateStatus = "completed"
	StatusFailed    TeammateStatus = "failed"
	StatusKilled    TeammateStatus = "killed"
)

// TeammateState is the runtime state of an in-process teammate.
type TeammateState struct {
	AgentID           AgentID
	TaskID            string
	Status            TeammateStatus
	IsIdle            bool
	ShutdownRequested bool
	PermissionMode    string
	// Cancel stops the teammate's execution goroutine.
	Cancel func()
}

// TeammateExecutor is the unified backend contract for spawning and managing teammates.
type TeammateExecutor interface {
	Type() BackendType
	IsAvailable() bool
	Spawn(cfg TeammateSpawnConfig) (TeammateSpawnResult, error)
	SendMessage(agentID AgentID, msg TeammateMessage) error
	Terminate(agentID AgentID, reason string) error
	Kill(agentID AgentID) error
	IsActive(agentID AgentID) bool
}

// TeamMember represents a member entry in the team config file.
type TeamMember struct {
	AgentID          string   `json:"agentId"`
	Name             string   `json:"name"`
	AgentType        string   `json:"agentType,omitempty"`
	Model            string   `json:"model,omitempty"`
	Prompt           string   `json:"prompt,omitempty"`
	Color            string   `json:"color,omitempty"`
	PlanModeRequired bool     `json:"planModeRequired,omitempty"`
	JoinedAt         int64    `json:"joinedAt"`
	TmuxPaneID       string   `json:"tmuxPaneId"`
	CWD              string   `json:"cwd"`
	WorktreePath     string   `json:"worktreePath,omitempty"`
	SessionID        string   `json:"sessionId,omitempty"`
	Subscriptions    []string `json:"subscriptions"`
	BackendType      string   `json:"backendType,omitempty"`
	IsActive         bool     `json:"isActive,omitempty"`
	Mode             string   `json:"mode,omitempty"`
}

// TeamAllowedPath is a path permission entry in the team config.
type TeamAllowedPath struct {
	Path     string `json:"path"`
	ToolName string `json:"toolName"`
	AddedBy  string `json:"addedBy"`
	AddedAt  int64  `json:"addedAt"`
}

// TeamFile is the on-disk coordination state for a team.
type TeamFile struct {
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	CreatedAt        int64             `json:"createdAt"`
	LeadAgentID      string            `json:"leadAgentId"`
	LeadSessionID    string            `json:"leadSessionId,omitempty"`
	HiddenPaneIDs    []string          `json:"hiddenPaneIds,omitempty"`
	TeamAllowedPaths []TeamAllowedPath `json:"teamAllowedPaths,omitempty"`
	Members          []TeamMember      `json:"members"`
}

// PermissionResolution is the leader's decision on a worker's permission request.
type PermissionResolution struct {
	Decision          string                         `json:"decision"`   // "approved" | "rejected"
	ResolvedBy        string                         `json:"resolvedBy"` // "worker" | "leader"
	Feedback          string                         `json:"feedback,omitempty"`
	UpdatedInput      any                            `json:"updatedInput,omitempty"`
	PermissionUpdates []permissions.PermissionUpdate `json:"permissionUpdates,omitempty"`
}

// SwarmPermissionRequest is a worker's request for the leader to approve a tool call.
type SwarmPermissionRequest struct {
	ID                    string                         `json:"id"`
	WorkerID              string                         `json:"workerId"`
	WorkerName            string                         `json:"workerName"`
	WorkerColor           string                         `json:"workerColor,omitempty"`
	TeamName              string                         `json:"teamName"`
	ToolName              string                         `json:"toolName"`
	ToolUseID             string                         `json:"toolUseId"`
	Description           string                         `json:"description"`
	Input                 map[string]any                 `json:"input"`
	PermissionSuggestions []any                          `json:"permissionSuggestions,omitempty"`
	Status                string                         `json:"status"` // "pending" | "approved" | "rejected"
	ResolvedBy            string                         `json:"resolvedBy,omitempty"`
	ResolvedAt            int64                          `json:"resolvedAt,omitempty"`
	Feedback              string                         `json:"feedback,omitempty"`
	UpdatedInput          map[string]any                 `json:"updatedInput,omitempty"`
	PermissionUpdates     []permissions.PermissionUpdate `json:"permissionUpdates,omitempty"`
	CreatedAt             int64                          `json:"createdAt"`
}

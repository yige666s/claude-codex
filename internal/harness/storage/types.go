package storage

import (
	"encoding/json"
)

// EntryType represents the type of entry in the transcript
type EntryType string

const (
	EntryTypeUser                    EntryType = "user"
	EntryTypeAssistant               EntryType = "assistant"
	EntryTypeAttachment              EntryType = "attachment"
	EntryTypeSystem                  EntryType = "system"
	EntryTypeTool                    EntryType = "tool"
	EntryTypeCustomTitle             EntryType = "custom-title"
	EntryTypeAITitle                 EntryType = "ai-title"
	EntryTypeLastPrompt              EntryType = "last-prompt"
	EntryTypeTag                     EntryType = "tag"
	EntryTypeAgentName               EntryType = "agent-name"
	EntryTypeAgentColor              EntryType = "agent-color"
	EntryTypeAgentSetting            EntryType = "agent-setting"
	EntryTypeMode                    EntryType = "mode"
	EntryTypeWorktreeState           EntryType = "worktree-state"
	EntryTypePRLink                  EntryType = "pr-link"
	EntryTypeFileHistorySnapshot     EntryType = "file-history-snapshot"
	EntryTypeAttributionSnapshot     EntryType = "attribution-snapshot"
	EntryTypeContentReplacement      EntryType = "content-replacement"
	EntryTypeContextCollapseCommit   EntryType = "context-collapse-commit"
	EntryTypeContextCollapseSnapshot EntryType = "context-collapse-snapshot"
	EntryTypeTaskSummary             EntryType = "task-summary"
)

// Entry is the base interface for all transcript entries
type Entry interface {
	GetType() EntryType
	GetSessionID() string
}

// BaseEntry contains common fields for all entries
type BaseEntry struct {
	Type      EntryType `json:"type"`
	SessionID string    `json:"sessionId,omitempty"`
	Timestamp string    `json:"timestamp,omitempty"`
}

func (e *BaseEntry) GetType() EntryType {
	return e.Type
}

func (e *BaseEntry) GetSessionID() string {
	return e.SessionID
}

// TranscriptMessage represents a message in the conversation
type TranscriptMessage struct {
	BaseEntry
	UUID       string          `json:"uuid"`
	ParentUUID string          `json:"parentUuid,omitempty"`
	Role       string          `json:"role,omitempty"`
	Content    string          `json:"content,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolInput  json.RawMessage `json:"toolInput,omitempty"`
	ToolOutput string          `json:"toolOutput,omitempty"`
	ToolCalls  []ToolCall      `json:"toolCalls,omitempty"`
	CWD        string          `json:"cwd,omitempty"`
	UserType   string          `json:"userType,omitempty"`
	Entrypoint string          `json:"entrypoint,omitempty"`
	Version    string          `json:"version,omitempty"`
	GitBranch  string          `json:"gitBranch,omitempty"`
	Slug       string          `json:"slug,omitempty"`
}

type ToolCall struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Input            json.RawMessage `json:"input"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
}

// MetadataEntry represents session metadata
type MetadataEntry struct {
	BaseEntry
	CustomTitle  string `json:"customTitle,omitempty"`
	AITitle      string `json:"aiTitle,omitempty"`
	LastPrompt   string `json:"lastPrompt,omitempty"`
	Tag          string `json:"tag,omitempty"`
	AgentName    string `json:"agentName,omitempty"`
	AgentColor   string `json:"agentColor,omitempty"`
	AgentSetting string `json:"agentSetting,omitempty"`
	Mode         string `json:"mode,omitempty"`
	TaskSummary  string `json:"summary,omitempty"`
}

// PRLinkEntry represents a link to a GitHub PR
type PRLinkEntry struct {
	BaseEntry
	PRNumber     int    `json:"prNumber"`
	PRUrl        string `json:"prUrl"`
	PRRepository string `json:"prRepository"`
}

// WorktreeStateEntry represents worktree session state
type WorktreeStateEntry struct {
	BaseEntry
	WorktreeSession *WorktreeSession `json:"worktreeSession"`
}

type WorktreeSession struct {
	OriginalCWD    string `json:"originalCwd"`
	WorktreePath   string `json:"worktreePath"`
	WorktreeBranch string `json:"worktreeBranch,omitempty"`
}

// FileHistorySnapshotEntry represents a file history snapshot
type FileHistorySnapshotEntry struct {
	BaseEntry
	MessageID        string                 `json:"messageId"`
	Snapshot         map[string]interface{} `json:"snapshot"`
	IsSnapshotUpdate bool                   `json:"isSnapshotUpdate"`
}

// ContentReplacementEntry represents content replacement decisions
type ContentReplacementEntry struct {
	BaseEntry
	MessageID   string `json:"messageId"`
	Decision    string `json:"decision"`
	Replacement string `json:"replacement,omitempty"`
}

// SessionMetadata holds cached session metadata
type SessionMetadata struct {
	CustomTitle   string
	AITitle       string
	LastPrompt    string
	Tag           string
	AgentName     string
	AgentColor    string
	AgentSetting  string
	Mode          string
	WorktreeState *WorktreeSession
	PRNumber      int
	PRUrl         string
	PRRepository  string
	TaskSummary   string
}

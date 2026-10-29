package bridge

import (
	"encoding/json"
	"time"

	toolkit "claude-codex/internal/harness/tools"
)

type Method string

const (
	MethodRunPrompt     Method = "run_prompt"
	MethodListTools     Method = "list_tools"
	MethodCreateSession Method = "create_session"
	MethodSessionPrompt Method = "session_prompt"
	MethodGetSession    Method = "get_session"
	MethodListSessions  Method = "list_sessions"
	MethodDeleteSession Method = "delete_session"
)

type Request struct {
	ID         int64           `json:"id"`
	Method     Method          `json:"method"`
	WorkingDir string          `json:"working_dir,omitempty"`
	Prompt     string          `json:"prompt,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Secret     string          `json:"secret,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type RunPromptResult struct {
	Output  string       `json:"output"`
	Session *SessionInfo `json:"session,omitempty"`
}

type ListToolsResult struct {
	Tools []toolkit.Descriptor `json:"tools"`
}

type SessionInfo struct {
	ID              string    `json:"id"`
	WorkingDir      string    `json:"working_dir,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
	MessageCount    int       `json:"message_count,omitempty"`
	LastUserMessage string    `json:"last_user_message,omitempty"`
	Archived        bool      `json:"archived,omitempty"`
}

type CreateSessionResult struct {
	Session SessionInfo `json:"session"`
}

type SessionPromptResult struct {
	Output  string      `json:"output"`
	Session SessionInfo `json:"session"`
}

type GetSessionResult struct {
	Session SessionInfo `json:"session"`
}

type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

type DeleteSessionResult struct {
	Deleted   bool   `json:"deleted"`
	SessionID string `json:"session_id"`
}

type runPromptParams struct {
	WorkingDir string `json:"working_dir,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
}

type listToolsParams struct {
	WorkingDir string `json:"working_dir,omitempty"`
}

type createSessionParams struct {
	WorkingDir string `json:"working_dir,omitempty"`
}

type sessionPromptParams struct {
	SessionID string `json:"session_id,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}

type getSessionParams struct {
	SessionID string `json:"session_id,omitempty"`
}

type listSessionsParams struct {
	WorkingDir string `json:"working_dir,omitempty"`
}

type deleteSessionParams struct {
	SessionID string `json:"session_id,omitempty"`
}

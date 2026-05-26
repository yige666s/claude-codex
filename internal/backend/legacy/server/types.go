package server

import (
	"time"
)

// ConnectResponse represents the response from creating a direct connect session
type ConnectResponse struct {
	SessionID string  `json:"session_id"`
	WsURL     string  `json:"ws_url"`
	WorkDir   *string `json:"work_dir,omitempty"`
}

// ServerConfig holds the configuration for the server
type ServerConfig struct {
	Port      int
	Host      string
	AuthToken string
	Unix      string
	// Idle timeout for detached sessions (ms). 0 = never expire.
	IdleTimeoutMs int
	// Maximum number of concurrent sessions.
	MaxSessions int
	// Default workspace directory for sessions that don't specify cwd.
	Workspace string
}

// SessionState represents the state of a session
type SessionState string

const (
	SessionStateStarting  SessionState = "starting"
	SessionStateRunning   SessionState = "running"
	SessionStateDetached  SessionState = "detached"
	SessionStateStopping  SessionState = "stopping"
	SessionStateStopped   SessionState = "stopped"
)

// SessionInfo holds information about a session
type SessionInfo struct {
	ID         string
	Status     SessionState
	CreatedAt  time.Time
	WorkDir    string
	SessionKey string
}

// SessionIndexEntry represents stable session metadata persisted to disk
// so sessions can be resumed across server restarts
type SessionIndexEntry struct {
	// Server-assigned session ID (matches the subprocess's claude session)
	SessionID string `json:"sessionId"`
	// The claude transcript session ID for --resume. Same as SessionID for direct sessions
	TranscriptSessionID string `json:"transcriptSessionId"`
	CWD                 string `json:"cwd"`
	PermissionMode      string `json:"permissionMode,omitempty"`
	CreatedAt           int64  `json:"createdAt"`
	LastActiveAt        int64  `json:"lastActiveAt"`
}

// SessionIndex maps stable session keys to session metadata
type SessionIndex map[string]SessionIndexEntry

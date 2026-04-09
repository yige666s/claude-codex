package coordinator

import "time"

// Mode represents the coordinator mode state
type Mode string

const (
	ModeNormal      Mode = "normal"
	ModeCoordinator Mode = "coordinator"
)

// Config holds coordinator configuration
type Config struct {
	Enabled       bool
	SimpleMode    bool // If true, workers only have Bash, Read, Edit tools
	ScratchpadDir string
	MCPClients    []MCPClient
}

// MCPClient represents an MCP server connection
type MCPClient struct {
	Name string
}

// WorkerContext contains context information for workers
type WorkerContext struct {
	AvailableTools []string
	MCPServers     []string
	ScratchpadDir  string
}

// SessionMode tracks the mode for session restoration
type SessionMode struct {
	Mode      Mode
	Timestamp time.Time
}

// CoordinatorState tracks the coordinator's operational state
type CoordinatorState struct {
	ActiveWorkers map[string]*WorkerInfo
	TaskQueue     []Task
}

// WorkerInfo tracks information about a spawned worker
type WorkerInfo struct {
	AgentID     string
	Description string
	Status      string
	StartTime   time.Time
	EndTime     *time.Time
}

// Task represents a queued coordinator task
type Task struct {
	ID          string
	Type        string // "spawn", "message", "stop"
	TargetAgent string
	Payload     interface{}
}

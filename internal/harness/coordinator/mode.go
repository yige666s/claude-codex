package coordinator

import (
	"os"
	"strings"
	"sync"
	"time"
)

const (
	EnvCoordinatorMode = "CLAUDE_CODE_COORDINATOR_MODE"
	EnvSimpleMode      = "CLAUDE_CODE_SIMPLE"
)

// Manager handles coordinator mode state and operations
type Manager struct {
	config Config
	state  CoordinatorState
	mu     sync.RWMutex
}

// NewManager creates a new coordinator manager
func NewManager(config Config) *Manager {
	return &Manager{
		config: config,
		state: CoordinatorState{
			ActiveWorkers: make(map[string]*WorkerInfo),
			TaskQueue:     []Task{},
		},
	}
}

func (m *Manager) teamStore() *TeamManager {
	root := strings.TrimSpace(m.config.ScratchpadDir)
	if root == "" {
		root = "."
	}
	return NewTeamManager(root)
}

// Create persists a team definition for coordinator-managed teams.
func (m *Manager) Create(name string) (Team, error) {
	return m.teamStore().Create(name)
}

// Delete removes a persisted team definition.
func (m *Manager) Delete(name string) (bool, error) {
	return m.teamStore().Delete(name)
}

// ListTeams returns persisted coordinator team definitions.
func (m *Manager) ListTeams() ([]Team, error) {
	return m.teamStore().List()
}

// RegisterWorker registers a new worker
func (m *Manager) RegisterWorker(agentID, description string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.ActiveWorkers[agentID] = &WorkerInfo{
		AgentID:     agentID,
		Description: description,
		Status:      "running",
		StartTime:   time.Now(),
	}
}

// UpdateWorkerStatus updates a worker's status
func (m *Manager) UpdateWorkerStatus(agentID, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if worker, ok := m.state.ActiveWorkers[agentID]; ok {
		worker.Status = status
		if status == "completed" || status == "failed" || status == "stopped" {
			now := time.Now()
			worker.EndTime = &now
		}
	}
}

// GetWorker retrieves worker information
func (m *Manager) GetWorker(agentID string) (*WorkerInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	worker, ok := m.state.ActiveWorkers[agentID]
	return worker, ok
}

// ListActiveWorkers returns all active workers
func (m *Manager) ListActiveWorkers() []*WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	workers := make([]*WorkerInfo, 0, len(m.state.ActiveWorkers))
	for _, worker := range m.state.ActiveWorkers {
		if worker.Status == "running" {
			workers = append(workers, worker)
		}
	}
	return workers
}

// RemoveWorker removes a worker from tracking
func (m *Manager) RemoveWorker(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.state.ActiveWorkers, agentID)
}

// GetConfig returns the coordinator configuration
func (m *Manager) GetConfig() Config {
	return m.config
}

// GetWorkerContext builds context for workers
func (m *Manager) GetWorkerContext(allTools []string) WorkerContext {
	workerTools := GetWorkerTools(m.config.SimpleMode, allTools)

	mcpServers := make([]string, len(m.config.MCPClients))
	for i, client := range m.config.MCPClients {
		mcpServers[i] = client.Name
	}

	return WorkerContext{
		AvailableTools: workerTools,
		MCPServers:     mcpServers,
		ScratchpadDir:  m.config.ScratchpadDir,
	}
}

// IsEnabled returns whether coordinator mode is enabled
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

// IsCoordinatorMode checks if coordinator mode is enabled
func IsCoordinatorMode() bool {
	return isEnvTruthy(os.Getenv(EnvCoordinatorMode))
}

// IsSimpleMode checks if simple mode is enabled (limited tools)
func IsSimpleMode() bool {
	return isEnvTruthy(os.Getenv(EnvSimpleMode))
}

// MatchSessionMode ensures the current mode matches the session's stored mode
// Returns a warning message if the mode was switched, or empty string if no switch needed
func MatchSessionMode(sessionMode Mode) string {
	if sessionMode == "" {
		return "" // No stored mode (old session)
	}

	currentIsCoordinator := IsCoordinatorMode()
	sessionIsCoordinator := sessionMode == ModeCoordinator

	if currentIsCoordinator == sessionIsCoordinator {
		return "" // Already matched
	}

	// Flip the env var
	if sessionIsCoordinator {
		os.Setenv(EnvCoordinatorMode, "1")
		return "Entered coordinator mode to match resumed session."
	} else {
		os.Unsetenv(EnvCoordinatorMode)
		return "Exited coordinator mode to match resumed session."
	}
}

// GetCurrentMode returns the current mode
func GetCurrentMode() Mode {
	if IsCoordinatorMode() {
		return ModeCoordinator
	}
	return ModeNormal
}

// isEnvTruthy checks if an environment variable is set to a truthy value
func isEnvTruthy(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "1" || value == "true" || value == "yes"
}

package agent

import (
	"context"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/harness/tools"
)

// Manager provides high-level agent management
type Manager struct {
	executor        *Executor
	progressTracker *ProgressTracker
	definitions     map[AgentType]*AgentDefinition
	toolRegistry    *tools.Registry
}

// NewManager creates a new agent manager
func NewManager(executor *Executor) *Manager {
	progressTracker := NewProgressTracker(executor)

	return &Manager{
		executor:        executor,
		progressTracker: progressTracker,
		definitions:     make(map[AgentType]*AgentDefinition),
		toolRegistry:    nil,
	}
}

// SetToolRegistry sets the tool registry for the manager
func (m *Manager) SetToolRegistry(registry *tools.Registry) {
	m.toolRegistry = registry
	m.executor.SetToolRegistry(registry)
}

// RegisterDefinition registers an agent definition
func (m *Manager) RegisterDefinition(def *AgentDefinition) error {
	if def.AgentType == "" {
		return fmt.Errorf("agent type cannot be empty")
	}

	m.definitions[def.AgentType] = def
	return nil
}

// GetDefinition returns an agent definition by type
func (m *Manager) GetDefinition(agentType AgentType) (*AgentDefinition, error) {
	def, ok := m.definitions[agentType]
	if !ok {
		return nil, fmt.Errorf("agent type not found: %s", agentType)
	}
	return def, nil
}

// ListDefinitions returns all registered agent definitions
func (m *Manager) ListDefinitions() []*AgentDefinition {
	defs := make([]*AgentDefinition, 0, len(m.definitions))
	for _, def := range m.definitions {
		defs = append(defs, def)
	}
	return defs
}

// RunAgent executes an agent with the given configuration
func (m *Manager) RunAgent(ctx context.Context, config AgentConfig) (*AgentResult, error) {
	// Validate definition
	if config.Definition == nil {
		return nil, fmt.Errorf("agent definition is required")
	}

	// Start progress tracking
	agentID := AgentID(generateAgentID())
	m.progressTracker.StartTracking(agentID)
	defer m.progressTracker.StopTracking(agentID)

	// Execute agent
	return m.executor.Execute(ctx, config)
}

// RunAgentByType executes an agent by type name
func (m *Manager) RunAgentByType(ctx context.Context, agentType AgentType, prompt string, parentModel string) (*AgentResult, error) {
	def, err := m.GetDefinition(agentType)
	if err != nil {
		return nil, err
	}

	config := AgentConfig{
		Definition:    def,
		ParentModel:   parentModel,
		InitialPrompt: prompt,
		WorkingDir:    ".",
	}

	return m.RunAgent(ctx, config)
}

// ForkAgent creates a forked agent that inherits parent context
func (m *Manager) ForkAgent(ctx context.Context, forkConfig ForkConfig) (*AgentResult, error) {
	// Build forked messages
	var assistantMessage Message
	if len(forkConfig.ParentMessages) > 0 {
		// Find last assistant message
		for i := len(forkConfig.ParentMessages) - 1; i >= 0; i-- {
			if forkConfig.ParentMessages[i].Role == "assistant" {
				assistantMessage = forkConfig.ParentMessages[i]
				break
			}
		}
	}

	forkedMessages := BuildForkedMessages(forkConfig.Directive, assistantMessage)

	// Create agent config
	config := AgentConfig{
		Definition:     ForkAgent,
		ParentID:       &forkConfig.ParentID,
		ParentModel:    forkConfig.ParentModel,
		WorkingDir:     forkConfig.WorkingDir,
		InitialPrompt:  "",
		IsFork:         true,
		InheritContext: true,
		ParentMessages: append(forkConfig.ParentMessages, forkedMessages...),
		SystemPrompt:   &forkConfig.SystemPrompt,
	}

	return m.RunAgent(ctx, config)
}

// GetAgentStatus returns the current status of an agent
func (m *Manager) GetAgentStatus(agentID AgentID) (*AgentInstance, error) {
	instance, ok := m.executor.GetInstance(agentID)
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	return instance, nil
}

// ListActiveAgents returns all currently running agents
func (m *Manager) ListActiveAgents() []*AgentInstance {
	return m.executor.ListInstances()
}

// AbortAgent stops a running agent
func (m *Manager) AbortAgent(agentID AgentID) error {
	return m.executor.Abort(agentID)
}

// GetProgressSummary returns the progress summary for an agent
func (m *Manager) GetProgressSummary(agentID AgentID) string {
	return m.progressTracker.GetSummary(agentID)
}

// AddProgressListener registers a progress listener
func (m *Manager) AddProgressListener(listener ProgressListener) {
	m.executor.AddProgressListener(listener)
}

// InitializeBuiltInAgents registers built-in agent definitions
func (m *Manager) InitializeBuiltInAgents() error {
	isCoordinator := false // Default; caller should set via env check if needed
	for _, def := range GetBuiltInAgents(isCoordinator) {
		if err := m.RegisterDefinition(def); err != nil {
			return fmt.Errorf("failed to register built-in agent %q: %w", def.AgentType, err)
		}
	}
	// Always register ForkAgent regardless of mode.
	if err := m.RegisterDefinition(ForkAgent); err != nil {
		return fmt.Errorf("failed to register fork agent: %w", err)
	}
	return nil
}

// InitializeBuiltInAgentsForMode registers built-in agents with coordinator-mode awareness.
func (m *Manager) InitializeBuiltInAgentsForMode(isCoordinatorMode bool) error {
	for _, def := range GetBuiltInAgents(isCoordinatorMode) {
		if err := m.RegisterDefinition(def); err != nil {
			return fmt.Errorf("failed to register built-in agent %q: %w", def.AgentType, err)
		}
	}
	if err := m.RegisterDefinition(ForkAgent); err != nil {
		return fmt.Errorf("failed to register fork agent: %w", err)
	}
	return nil
}

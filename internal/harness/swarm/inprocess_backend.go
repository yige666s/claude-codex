package swarm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentRunnerFunc is the function signature for running an in-process agent.
// It is injected at construction time to avoid import cycles with the query loop.
type AgentRunnerFunc func(ctx context.Context, cfg InProcessRunConfig) (<-chan string, error)

// InProcessRunConfig carries the parameters for starting an in-process agent.
type InProcessRunConfig struct {
	AgentID                AgentID
	Identity               TeammateIdentity
	Prompt                 string
	Model                  string
	SystemPrompt           string
	SystemPromptMode       string
	AllowedTools           []string
	AllowPermissionPrompts bool
	WorktreePath           string
	ParentSessionID        string
}

// InProcessBackend implements TeammateExecutor for in-process agents.
// Tmux and iTerm2 backends are not implemented (platform-specific).
type InProcessBackend struct {
	mu         sync.RWMutex
	agents     map[AgentID]*TeammateState
	runner     AgentRunnerFunc
	mailboxDir string // base dir for mailboxes (~/.claude/mailboxes)
}

// NewInProcessBackend creates a new InProcessBackend.
// runner is the function that actually executes an agent (injected to avoid cycles).
// mailboxDir is the base directory for agent mailboxes.
func NewInProcessBackend(runner AgentRunnerFunc, mailboxDir string) *InProcessBackend {
	return &InProcessBackend{
		agents:     make(map[AgentID]*TeammateState),
		runner:     runner,
		mailboxDir: mailboxDir,
	}
}

func (b *InProcessBackend) Type() BackendType { return BackendTypeInProcess }
func (b *InProcessBackend) IsAvailable() bool { return true }

// Spawn creates and starts a new in-process teammate.
func (b *InProcessBackend) Spawn(cfg TeammateSpawnConfig) (TeammateSpawnResult, error) {
	agentID := FormatAgentID(cfg.Name, cfg.TeamName)
	taskID := fmt.Sprintf("in_process_teammate_%s_%d", cfg.Name, time.Now().UnixNano())

	ctx, cancel := context.WithCancel(context.Background())

	state := &TeammateState{
		AgentID: agentID,
		TaskID:  taskID,
		Status:  StatusRunning,
		IsIdle:  false,
		Cancel:  cancel,
	}

	b.mu.Lock()
	b.agents[agentID] = state
	b.mu.Unlock()

	// Add to team file
	member := TeamMember{
		AgentID:       string(agentID),
		Name:          cfg.Name,
		Color:         cfg.Color,
		JoinedAt:      time.Now().UnixMilli(),
		TmuxPaneID:    InProcessMarker,
		CWD:           cfg.CWD,
		WorktreePath:  cfg.WorktreePath,
		Subscriptions: []string{},
		BackendType:   string(BackendTypeInProcess),
		IsActive:      true,
	}
	if cfg.TeamName != "" {
		_ = UpsertMember(cfg.TeamName, member) // best-effort
	}

	// Start the agent goroutine
	go func() {
		defer cancel()
		defer func() {
			b.mu.Lock()
			if s, ok := b.agents[agentID]; ok {
				if s.Status == StatusRunning {
					s.Status = StatusCompleted
				}
				s.IsIdle = true
			}
			b.mu.Unlock()
			if cfg.TeamName != "" {
				_, _ = RemoveMemberByAgentID(cfg.TeamName, string(agentID))
			}
		}()

		if b.runner == nil {
			return
		}

		runCfg := InProcessRunConfig{
			AgentID:                agentID,
			Identity:               cfg.TeammateIdentity,
			Prompt:                 cfg.Prompt,
			Model:                  cfg.Model,
			SystemPrompt:           cfg.SystemPrompt,
			SystemPromptMode:       cfg.SystemPromptMode,
			AllowedTools:           append([]string(nil), cfg.Permissions...),
			AllowPermissionPrompts: cfg.AllowPermissionPrompts,
			WorktreePath:           cfg.WorktreePath,
			ParentSessionID:        cfg.ParentSessionID,
		}

		stream, err := b.runner(ctx, runCfg)
		if err != nil {
			b.mu.Lock()
			if s, ok := b.agents[agentID]; ok {
				s.Status = StatusFailed
			}
			b.mu.Unlock()
			return
		}

		for range stream {
			// Drain the stream; messages are handled by the runner
		}
	}()

	return TeammateSpawnResult{
		Success: true,
		AgentID: agentID,
		TaskID:  taskID,
	}, nil
}

// SendMessage delivers a message to a teammate's mailbox.
func (b *InProcessBackend) SendMessage(agentID AgentID, msg TeammateMessage) error {
	return WriteToMailbox(b.mailboxDir, string(agentID), MailboxEntry{
		From:      msg.From,
		Text:      msg.Text,
		Color:     msg.Color,
		Timestamp: msg.Timestamp,
	})
}

// Terminate sends a shutdown request message to the agent.
func (b *InProcessBackend) Terminate(agentID AgentID, reason string) error {
	b.mu.RLock()
	state, ok := b.agents[agentID]
	b.mu.RUnlock()

	if !ok {
		return fmt.Errorf("Terminate: agent %s not found", agentID)
	}
	if state.ShutdownRequested {
		return nil // already shutting down
	}

	b.mu.Lock()
	state.ShutdownRequested = true
	b.mu.Unlock()

	// Deliver shutdown request via mailbox
	_, name := ParseAgentID(agentID)
	_ = name
	return WriteToMailbox(b.mailboxDir, string(agentID), MailboxEntry{
		From: TeamLeadName,
		Text: `{"type":"shutdown_request","reason":"` + reason + `"}`,
	})
}

// Kill immediately aborts the agent.
func (b *InProcessBackend) Kill(agentID AgentID) error {
	b.mu.Lock()
	state, ok := b.agents[agentID]
	if ok {
		state.Status = StatusKilled
		state.Cancel()
		delete(b.agents, agentID)
	}
	b.mu.Unlock()

	if !ok {
		return fmt.Errorf("Kill: agent %s not found", agentID)
	}
	return nil
}

// IsActive returns true if the agent is currently running.
func (b *InProcessBackend) IsActive(agentID AgentID) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	state, ok := b.agents[agentID]
	return ok && state.Status == StatusRunning
}

// ListAgents returns a snapshot of all tracked agents.
func (b *InProcessBackend) ListAgents() map[AgentID]TeammateStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[AgentID]TeammateStatus, len(b.agents))
	for id, s := range b.agents {
		out[id] = s.Status
	}
	return out
}

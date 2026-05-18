package cli

import (
	"context"
	"strings"
	"sync"

	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/state"
	coretasks "claude-codex/internal/harness/tasks"
)

type bridgeContinuationService struct {
	runner bridgeRunner
}

var (
	bridgeSessionLockMu sync.Mutex
	bridgeSessionLocks  = map[string]*sync.Mutex{}
)

func startBridgeContinuationForwarder(ctx context.Context, runner bridgeRunner, manager *coretasks.TaskManager) func() {
	if manager == nil {
		return func() {}
	}
	events, unsubscribe := manager.SubscribeTerminalEvents(32)
	done := make(chan struct{})
	service := &bridgeContinuationService{
		runner: runner,
	}
	var stopOnce sync.Once
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case taskID, ok := <-events:
				if !ok {
					return
				}
				notification, task, ok := coordinator.DrainTaskNotification(manager, taskID)
				if !ok {
					continue
				}
				go service.continueParent(ctx, task, notification)
			}
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(done)
		})
	}
}

func (s *bridgeContinuationService) continueParent(ctx context.Context, task coretasks.TaskState, notification string) {
	notification = strings.TrimSpace(notification)
	if notification == "" {
		return
	}
	sessionID := coordinator.TaskParentSessionID(task)
	workDir := coordinator.TaskWorkingDir(task)
	if strings.TrimSpace(workDir) == "" {
		workDir = s.runner.defaultWorkDir
	}

	unlock := lockBridgeSession(sessionID)
	defer unlock()

	session := (*state.Session)(nil)
	if sessionID != "" {
		if loaded, err := state.LoadSession(s.runner.home, sessionID); err == nil {
			session = loaded
		}
	}
	if session == nil {
		session = state.NewSession(workDir)
		sessionID = session.ID
	}
	if strings.TrimSpace(session.WorkingDir) != "" {
		workDir = session.WorkingDir
	}

	runner, err := s.runner.buildEngine(workDir)
	if err != nil {
		return
	}
	if _, err := runner.RunGeneratedPrompt(ctx, session, notification); err != nil {
		return
	}
	_, _ = session.Save(s.runner.home)
}

func lockBridgeSession(sessionID string) func() {
	if sessionID == "" {
		return func() {}
	}
	bridgeSessionLockMu.Lock()
	lock := bridgeSessionLocks[sessionID]
	if lock == nil {
		lock = &sync.Mutex{}
		bridgeSessionLocks[sessionID] = lock
	}
	bridgeSessionLockMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

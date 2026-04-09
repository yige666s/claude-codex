package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProgressTracker manages background progress summaries for agents
type ProgressTracker struct {
	executor *Executor
	mu       sync.RWMutex
	trackers map[AgentID]*agentTracker
}

// agentTracker tracks progress for a single agent
type agentTracker struct {
	agentID       AgentID
	startTime     time.Time
	lastSummary   time.Time
	summaryTicker *time.Ticker
	stopChan      chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(executor *Executor) *ProgressTracker {
	return &ProgressTracker{
		executor: executor,
		trackers: make(map[AgentID]*agentTracker),
	}
}

// StartTracking begins tracking progress for an agent
func (pt *ProgressTracker) StartTracking(agentID AgentID) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	// Don't track if already tracking
	if _, exists := pt.trackers[agentID]; exists {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	tracker := &agentTracker{
		agentID:       agentID,
		startTime:     time.Now(),
		lastSummary:   time.Now(),
		summaryTicker: time.NewTicker(30 * time.Second), // Summary every 30 seconds
		stopChan:      make(chan struct{}),
		ctx:           ctx,
		cancel:        cancel,
	}

	pt.trackers[agentID] = tracker

	// Start background goroutine
	go pt.trackProgress(tracker)
}

// StopTracking stops tracking progress for an agent
func (pt *ProgressTracker) StopTracking(agentID AgentID) {
	pt.mu.Lock()
	tracker, exists := pt.trackers[agentID]
	if !exists {
		pt.mu.Unlock()
		return
	}
	delete(pt.trackers, agentID)
	pt.mu.Unlock()

	// Stop the tracker
	tracker.cancel()
	tracker.summaryTicker.Stop()
	close(tracker.stopChan)
}

// trackProgress runs in background and generates periodic summaries
func (pt *ProgressTracker) trackProgress(tracker *agentTracker) {
	for {
		select {
		case <-tracker.ctx.Done():
			return
		case <-tracker.stopChan:
			return
		case <-tracker.summaryTicker.C:
			pt.generateSummary(tracker)
		}
	}
}

// generateSummary creates a progress summary for an agent
func (pt *ProgressTracker) generateSummary(tracker *agentTracker) {
	instance, ok := pt.executor.GetInstance(tracker.agentID)
	if !ok {
		return
	}

	elapsed := time.Since(tracker.startTime)
	summary := fmt.Sprintf(
		"Agent %s: Turn %d/%d, Running for %s",
		instance.Type,
		instance.TurnCount,
		instance.MaxTurns,
		formatDuration(elapsed),
	)

	// Notify progress
	pt.executor.notifyProgress(ProgressUpdate{
		AgentID:    tracker.agentID,
		TurnNumber: instance.TurnCount,
		Status:     instance.Status,
		Summary:    summary,
		Timestamp:  time.Now(),
	})

	tracker.lastSummary = time.Now()
}

// GetSummary returns the current summary for an agent
func (pt *ProgressTracker) GetSummary(agentID AgentID) string {
	pt.mu.RLock()
	tracker, exists := pt.trackers[agentID]
	pt.mu.RUnlock()

	if !exists {
		return ""
	}

	instance, ok := pt.executor.GetInstance(agentID)
	if !ok {
		return ""
	}

	elapsed := time.Since(tracker.startTime)
	return fmt.Sprintf(
		"Agent %s: Turn %d/%d, Running for %s",
		instance.Type,
		instance.TurnCount,
		instance.MaxTurns,
		formatDuration(elapsed),
	)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}

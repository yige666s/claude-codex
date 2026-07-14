package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const DefaultChatStreamEventLimit = 500
const DefaultChatStreamBlockRead = 10 * time.Second

type ChatStreamStore interface {
	CreateRun(ctx context.Context, userID, sessionID string) (*ChatRunSummary, error)
	CreateRunWithID(ctx context.Context, userID, sessionID, runID string) (*ChatRunSummary, error)
	Append(ctx context.Context, runID, userID, sessionID string, event Event) (*ChatStreamEvent, error)
	ListAfter(ctx context.Context, userID, runID, afterID string, limit int) ([]*ChatStreamEvent, bool, error)
	BlockRead(ctx context.Context, userID, runID, afterID string, limit int, block time.Duration) ([]*ChatStreamEvent, bool, error)
	LatestActiveForSession(ctx context.Context, userID, sessionID string) (*ChatRunSummary, error)
	MarkTerminal(ctx context.Context, runID string, terminalType string, errText string) error
}

type ChatStreamEvent struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	UserID    string    `json:"user_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Type      string    `json:"type"`
	Event     Event     `json:"event"`
	CreatedAt time.Time `json:"created_at"`
}

type MemoryChatStreamStore struct {
	mu      sync.Mutex
	runs    map[string]chatStreamRun
	events  map[string][]*ChatStreamEvent
	watches map[string]map[chan *ChatStreamEvent]struct{}
}

type chatStreamRun struct {
	UserID      string
	SessionID   string
	Status      string
	Terminal    bool
	LastEventID string
	UpdatedAt   time.Time
}

type ChatRunSummary struct {
	RunID       string    `json:"run_id"`
	SessionID   string    `json:"session_id,omitempty"`
	Status      string    `json:"status,omitempty"`
	Terminal    bool      `json:"terminal"`
	LastEventID string    `json:"last_event_id,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func NewMemoryChatStreamStore() *MemoryChatStreamStore {
	return &MemoryChatStreamStore{
		runs:    make(map[string]chatStreamRun),
		events:  make(map[string][]*ChatStreamEvent),
		watches: make(map[string]map[chan *ChatStreamEvent]struct{}),
	}
}

func NewChatRunID() string {
	return "run-" + newSortableID()
}

func (s *MemoryChatStreamStore) CreateRun(ctx context.Context, userID, sessionID string) (*ChatRunSummary, error) {
	return s.CreateRunWithID(ctx, userID, sessionID, NewChatRunID())
}

func (s *MemoryChatStreamStore) CreateRunWithID(ctx context.Context, userID, sessionID, runID string) (*ChatRunSummary, error) {
	if s == nil {
		return nil, fmt.Errorf("chat stream store is not configured")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		runID = NewChatRunID()
	}
	now := time.Now().UTC()
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	s.mu.Lock()
	s.runs[runID] = chatStreamRun{UserID: userID, SessionID: sessionID, Status: "running", UpdatedAt: now}
	s.mu.Unlock()
	return &ChatRunSummary{RunID: runID, SessionID: sessionID, Status: "running", UpdatedAt: now}, nil
}

func (s *MemoryChatStreamStore) Append(ctx context.Context, runID, userID, sessionID string, event Event) (*ChatStreamEvent, error) {
	if s == nil {
		return nil, fmt.Errorf("chat stream store is not configured")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("chat run id is required")
	}
	if event.SessionID == "" {
		event.SessionID = strings.TrimSpace(sessionID)
	}
	event.RunID = runID
	record := &ChatStreamEvent{
		ID:        NewJobEventID(),
		RunID:     runID,
		UserID:    strings.TrimSpace(userID),
		SessionID: strings.TrimSpace(sessionID),
		Type:      event.Type,
		Event:     event,
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	run, ok := s.runs[runID]
	if !ok {
		run = chatStreamRun{UserID: record.UserID, SessionID: record.SessionID}
	}
	if run.UserID == "" {
		run.UserID = record.UserID
	}
	if run.SessionID == "" {
		run.SessionID = record.SessionID
	}
	run.LastEventID = record.ID
	run.UpdatedAt = record.CreatedAt
	if chatStreamTerminal(record.Type) {
		run.Terminal = true
		run.Status = chatStreamTerminalStatus(record.Type)
	} else if run.Status == "" {
		run.Status = "running"
	}
	s.runs[runID] = run
	s.events[runID] = append(s.events[runID], record)
	watches := make([]chan *ChatStreamEvent, 0, len(s.watches[runID]))
	for ch := range s.watches[runID] {
		watches = append(watches, ch)
	}
	s.mu.Unlock()

	for _, ch := range watches {
		select {
		case ch <- record:
		default:
		}
	}
	return record, nil
}

func (s *MemoryChatStreamStore) ListAfter(ctx context.Context, userID, runID, afterID string, limit int) ([]*ChatStreamEvent, bool, error) {
	if s == nil {
		return nil, false, fmt.Errorf("chat stream store is not configured")
	}
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}
	if limit <= 0 {
		limit = DefaultChatStreamEventLimit
	}
	userID = strings.TrimSpace(userID)
	runID = strings.TrimSpace(runID)
	afterID = strings.TrimSpace(afterID)
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return nil, false, fmt.Errorf("chat run was not found")
	}
	if userID != "" && run.UserID != "" && run.UserID != userID {
		return nil, false, fmt.Errorf("chat run was not found")
	}
	events := s.events[runID]
	out := make([]*ChatStreamEvent, 0, minInt(limit, len(events)))
	for _, event := range events {
		if afterID != "" && event.ID <= afterID {
			continue
		}
		out = append(out, event)
		if len(out) >= limit {
			break
		}
	}
	return out, run.Terminal, nil
}

func (s *MemoryChatStreamStore) BlockRead(ctx context.Context, userID, runID, afterID string, limit int, block time.Duration) ([]*ChatStreamEvent, bool, error) {
	if s == nil {
		return nil, false, fmt.Errorf("chat stream store is not configured")
	}
	if block <= 0 {
		block = DefaultChatStreamBlockRead
	}
	events, terminal, err := s.ListAfter(ctx, userID, runID, afterID, limit)
	if err != nil || len(events) > 0 || terminal {
		return events, terminal, err
	}
	ch, cancel := s.Subscribe(runID)
	defer cancel()
	timer := time.NewTimer(block)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timer.C:
			events, terminal, err := s.ListAfter(ctx, userID, runID, afterID, limit)
			if err != nil {
				return nil, false, err
			}
			return events, terminal, nil
		case event, ok := <-ch:
			if !ok {
				return nil, false, nil
			}
			if afterID != "" && event.ID <= afterID {
				continue
			}
			events, terminal, err := s.ListAfter(ctx, userID, runID, afterID, limit)
			if err != nil {
				return nil, false, err
			}
			return events, terminal, nil
		}
	}
}

func (s *MemoryChatStreamStore) LatestActiveForSession(ctx context.Context, userID, sessionID string) (*ChatRunSummary, error) {
	if s == nil {
		return nil, fmt.Errorf("chat stream store is not configured")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *ChatRunSummary
	for runID, run := range s.runs {
		if run.Terminal || run.UserID != userID || run.SessionID != sessionID {
			continue
		}
		events := s.events[runID]
		summary := &ChatRunSummary{RunID: runID, SessionID: run.SessionID, Status: firstNonEmptyString(run.Status, "running"), Terminal: run.Terminal, LastEventID: run.LastEventID, UpdatedAt: run.UpdatedAt}
		if len(events) > 0 {
			last := events[len(events)-1]
			summary.LastEventID = last.ID
			summary.UpdatedAt = last.CreatedAt
		}
		if latest == nil || summary.UpdatedAt.After(latest.UpdatedAt) {
			latest = summary
		}
	}
	return latest, nil
}

func (s *MemoryChatStreamStore) MarkTerminal(ctx context.Context, runID string, terminalType string, _ string) error {
	if s == nil {
		return fmt.Errorf("chat stream store is not configured")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("chat run id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return fmt.Errorf("chat run was not found")
	}
	run.Terminal = true
	run.Status = chatStreamTerminalStatus(terminalType)
	run.UpdatedAt = time.Now().UTC()
	s.runs[runID] = run
	return nil
}

func (s *MemoryChatStreamStore) Subscribe(runID string) (<-chan *ChatStreamEvent, func()) {
	if s == nil {
		ch := make(chan *ChatStreamEvent)
		close(ch)
		return ch, func() {}
	}
	runID = strings.TrimSpace(runID)
	ch := make(chan *ChatStreamEvent, 64)
	s.mu.Lock()
	if s.watches[runID] == nil {
		s.watches[runID] = make(map[chan *ChatStreamEvent]struct{})
	}
	s.watches[runID][ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.watches[runID], ch)
		if len(s.watches[runID]) == 0 {
			delete(s.watches, runID)
		}
		s.mu.Unlock()
		close(ch)
	}
}

func chatStreamTerminal(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "done", "error", "cancelled":
		return true
	default:
		return false
	}
}

func chatStreamTerminalStatus(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "done":
		return "succeeded"
	case "cancelled":
		return "cancelled"
	case "error":
		return "failed"
	default:
		return "failed"
	}
}

type resumableChatSink struct {
	runID             string
	userID            string
	sessionID         string
	store             ChatStreamStore
	structuredOutputs StructuredOutputStore
	snapshots         ChatRunSnapshotStore
	reservations      ChatTurnReservationStore
	client            EventSink
	failed            bool
	terminal          bool
	eventCount        int
	structuredCount   int
	artifactCount     int
	finalContent      string
	finalMessageID    string
	lastEventID       string
	lastError         string
}

func (s *resumableChatSink) Send(ctx context.Context, event Event) error {
	if s == nil || s.store == nil {
		return fmt.Errorf("resumable chat sink is not configured")
	}
	record, err := s.store.Append(ctx, s.runID, s.userID, s.sessionID, event)
	if err != nil {
		return err
	}
	s.eventCount++
	s.lastEventID = record.ID
	if event.Type == "message" && event.Role == "assistant" {
		s.finalContent = event.Content
		s.finalMessageID = event.ID
	}
	if event.Type == "artifact" || event.Type == "artifact_created" || event.Type == "media.generated" {
		s.artifactCount++
	}
	if event.Type == "error" {
		s.lastError = event.Error
	}
	if output, ok := structuredOutputFromEvent(record.Event, s.userID, s.sessionID, s.runID); ok {
		s.structuredCount++
		if s.structuredOutputs != nil {
			if _, err := s.structuredOutputs.SaveStructuredOutput(ctx, output); err != nil {
				return err
			}
		}
	}
	if chatStreamTerminal(record.Type) {
		s.terminal = true
		s.saveTerminalSnapshot(ctx, record.Type)
	}
	if s.client == nil || s.failed {
		return nil
	}
	if sink, ok := s.client.(*sseEventSink); ok {
		if err := sink.send(ctx, record.ID, record.Event); err != nil {
			s.failed = true
		}
		return nil
	}
	if err := s.client.Send(ctx, record.Event); err != nil {
		s.failed = true
	}
	return nil
}

func (s *resumableChatSink) saveTerminalSnapshot(ctx context.Context, terminalType string) {
	if s == nil {
		return
	}
	status := chatStreamTerminalStatus(terminalType)
	if s.snapshots != nil {
		_ = s.snapshots.SaveChatRunSnapshot(ctx, ChatRunSnapshot{
			RunID:                 s.runID,
			UserID:                s.userID,
			SessionID:             s.sessionID,
			Status:                status,
			FinalMessageID:        s.finalMessageID,
			FinalContent:          s.finalContent,
			EventCount:            s.eventCount,
			StructuredOutputCount: s.structuredCount,
			ArtifactCount:         s.artifactCount,
			Error:                 s.lastError,
			LastEventID:           s.lastEventID,
			Payload:               json.RawMessage(`{}`),
		})
	}
	if s.reservations != nil {
		_ = s.reservations.UpdateChatTurnReservationStatus(ctx, s.userID, s.sessionID, s.runID, status)
	}
}

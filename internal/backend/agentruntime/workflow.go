package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	harnessengine "claude-codex/internal/harness/engine"
)

const (
	WorkflowStatusPending   = "pending"
	WorkflowStatusRunning   = "running"
	WorkflowStatusSucceeded = "succeeded"
	WorkflowStatusFailed    = "failed"
	WorkflowStatusCancelled = "cancelled"

	WorkflowStepStatusPending   = "pending"
	WorkflowStepStatusRunning   = "running"
	WorkflowStepStatusSucceeded = "succeeded"
	WorkflowStepStatusFailed    = "failed"
)

type WorkflowDefinition struct {
	Name    string                   `json:"name"`
	Version string                   `json:"version"`
	Steps   []WorkflowStepDefinition `json:"steps"`
}

type WorkflowStepDefinition struct {
	Name    string        `json:"name"`
	Handler string        `json:"handler,omitempty"`
	Timeout time.Duration `json:"timeout,omitempty"`
}

type WorkflowRun struct {
	ID             string         `json:"id"`
	UserID         string         `json:"user_id,omitempty"`
	SessionID      string         `json:"session_id,omitempty"`
	JobID          string         `json:"job_id,omitempty"`
	RequestID      string         `json:"request_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Status         string         `json:"status"`
	State          map[string]any `json:"state,omitempty"`
	Error          string         `json:"error,omitempty"`
	LeaseOwner     string         `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time     `json:"lease_expires_at,omitempty"`
	Recoverable    bool           `json:"recoverable,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
}

type WorkflowStepRun struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	StepIndex      int            `json:"step_index"`
	StepName       string         `json:"step_name"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Attempt        int            `json:"attempt,omitempty"`
	Status         string         `json:"status"`
	Input          map[string]any `json:"input,omitempty"`
	Output         map[string]any `json:"output,omitempty"`
	Error          string         `json:"error,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
}

type WorkflowStore interface {
	CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error
	UpdateWorkflowRun(ctx context.Context, run *WorkflowRun) error
	GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error)
	ListWorkflowRuns(ctx context.Context, filter WorkflowRunFilter) ([]*WorkflowRun, error)
	AddWorkflowStepRun(ctx context.Context, step *WorkflowStepRun) error
	UpdateWorkflowStepRun(ctx context.Context, step *WorkflowStepRun) error
	ListWorkflowStepRuns(ctx context.Context, runID string) ([]*WorkflowStepRun, error)
}

type WorkflowResumeStore interface {
	FindWorkflowRunByIdempotencyKey(ctx context.Context, userID, name, idempotencyKey string) (*WorkflowRun, error)
	GetWorkflowStepByIndex(ctx context.Context, runID string, stepIndex int) (*WorkflowStepRun, error)
}

type WorkflowRunFilter struct {
	UserID    string
	SessionID string
	JobID     string
	Name      string
	Status    string
	Limit     int
}

type WorkflowRequest struct {
	Definition     WorkflowDefinition
	UserID         string
	SessionID      string
	JobID          string
	RequestID      string
	IdempotencyKey string
	Recoverable    bool
	State          map[string]any
}

type WorkflowStepHandler func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error)

type WorkflowEngine struct {
	store    WorkflowStore
	handlers map[string]WorkflowStepHandler
	events   WorkflowEventSink
}

func NewWorkflowEngine(store WorkflowStore, events WorkflowEventSink) *WorkflowEngine {
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	if events == nil {
		events = NoopWorkflowEventSink{}
	}
	return &WorkflowEngine{store: store, handlers: make(map[string]WorkflowStepHandler), events: events}
}

func (e *WorkflowEngine) RegisterStepHandler(name string, handler WorkflowStepHandler) {
	if e == nil || name == "" || handler == nil {
		return
	}
	e.handlers[name] = handler
}

func (e *WorkflowEngine) Execute(ctx context.Context, req WorkflowRequest) (*WorkflowRun, error) {
	if e == nil {
		return nil, fmt.Errorf("workflow engine is not configured")
	}
	if req.Definition.Name == "" {
		return nil, fmt.Errorf("workflow definition name is required")
	}
	req = normalizeWorkflowRequest(ctx, req, false)
	now := time.Now().UTC()
	run := &WorkflowRun{
		ID:             NewWorkflowRunID(),
		UserID:         req.UserID,
		SessionID:      req.SessionID,
		JobID:          req.JobID,
		RequestID:      req.RequestID,
		IdempotencyKey: req.IdempotencyKey,
		Name:           req.Definition.Name,
		Version:        req.Definition.Version,
		Status:         WorkflowStatusRunning,
		State:          cloneWorkflowMap(req.State),
		Recoverable:    req.Recoverable,
		CreatedAt:      now,
		UpdatedAt:      now,
		StartedAt:      &now,
	}
	if err := e.store.CreateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Status: run.Status, Type: "workflow_run_started"})

	return e.executeRun(ctx, run, req.Definition, false)
}

func (e *WorkflowEngine) ExecuteOrResume(ctx context.Context, req WorkflowRequest) (*WorkflowRun, error) {
	if e == nil {
		return nil, fmt.Errorf("workflow engine is not configured")
	}
	if req.Definition.Name == "" {
		return nil, fmt.Errorf("workflow definition name is required")
	}
	req = normalizeWorkflowRequest(ctx, req, true)
	if req.IdempotencyKey != "" {
		if resumeStore, ok := e.store.(WorkflowResumeStore); ok {
			existing, err := resumeStore.FindWorkflowRunByIdempotencyKey(ctx, req.UserID, req.Definition.Name, req.IdempotencyKey)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				return e.resumeRun(ctx, existing, req.Definition)
			}
		}
	}
	return e.Execute(ctx, req)
}

func (e *WorkflowEngine) Resume(ctx context.Context, runID string, definition WorkflowDefinition) (*WorkflowRun, error) {
	if e == nil {
		return nil, fmt.Errorf("workflow engine is not configured")
	}
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("workflow run id is required")
	}
	if definition.Name == "" {
		return nil, fmt.Errorf("workflow definition name is required")
	}
	run, err := e.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Name != definition.Name {
		return nil, fmt.Errorf("workflow run %s is %s, not %s", run.ID, run.Name, definition.Name)
	}
	if definition.Version == "" {
		definition.Version = run.Version
	}
	return e.resumeRun(ctx, run, definition)
}

func (e *WorkflowEngine) resumeRun(ctx context.Context, run *WorkflowRun, definition WorkflowDefinition) (*WorkflowRun, error) {
	if run == nil {
		return nil, fmt.Errorf("workflow run is required")
	}
	if run.Status == WorkflowStatusSucceeded {
		return cloneWorkflowRun(run), nil
	}
	now := time.Now().UTC()
	run.Status = WorkflowStatusRunning
	run.Error = ""
	run.UpdatedAt = now
	run.FinishedAt = nil
	if run.StartedAt == nil {
		run.StartedAt = &now
	}
	if definition.Version == "" {
		definition.Version = firstNonEmptyString(run.Version, "v1")
	}
	run.Version = definition.Version
	if err := e.store.UpdateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Status: run.Status, Type: "workflow_run_resumed"})
	return e.executeRun(ctx, run, definition, true)
}

func (e *WorkflowEngine) executeRun(ctx context.Context, run *WorkflowRun, definition WorkflowDefinition, resume bool) (*WorkflowRun, error) {
	for stepIndex, stepDef := range definition.Steps {
		handlerName := firstNonEmptyString(stepDef.Handler, stepDef.Name)
		handler := e.handlers[handlerName]
		if handler == nil {
			return e.failRun(ctx, run, fmt.Errorf("workflow step handler not found: %s", handlerName))
		}
		if resume {
			step, output, reused, err := e.reuseCompletedStep(ctx, run, stepIndex, stepDef)
			if err != nil {
				return e.failRun(ctx, run, err)
			}
			if reused {
				mergeWorkflowState(run.State, output)
				run.UpdatedAt = time.Now().UTC()
				if err := e.store.UpdateWorkflowRun(ctx, run); err != nil {
					return nil, err
				}
				_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Step: cloneWorkflowStepRun(step), Status: WorkflowStepStatusSucceeded, Type: "workflow_step_reused"})
				continue
			}
		}
		step, output, err := e.executeStep(ctx, run, stepIndex, stepDef, handler, resume)
		if err != nil {
			_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Step: cloneWorkflowStepRun(step), Status: WorkflowStepStatusFailed, Type: "workflow_step_failed"})
			return e.failRun(ctx, run, err)
		}
		mergeWorkflowState(run.State, output)
		run.UpdatedAt = time.Now().UTC()
		if err := e.store.UpdateWorkflowRun(ctx, run); err != nil {
			return nil, err
		}
		_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Step: cloneWorkflowStepRun(step), Status: WorkflowStepStatusSucceeded, Type: "workflow_step_succeeded"})
	}

	finished := time.Now().UTC()
	run.Status = WorkflowStatusSucceeded
	run.UpdatedAt = finished
	run.FinishedAt = &finished
	if err := e.store.UpdateWorkflowRun(ctx, run); err != nil {
		return nil, err
	}
	_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Status: run.Status, Type: "workflow_run_succeeded"})
	return cloneWorkflowRun(run), nil
}

func (e *WorkflowEngine) Store() WorkflowStore {
	if e == nil {
		return nil
	}
	return e.store
}

func (e *WorkflowEngine) reuseCompletedStep(ctx context.Context, run *WorkflowRun, stepIndex int, stepDef WorkflowStepDefinition) (*WorkflowStepRun, map[string]any, bool, error) {
	resumeStore, ok := e.store.(WorkflowResumeStore)
	if !ok {
		return nil, nil, false, nil
	}
	step, err := resumeStore.GetWorkflowStepByIndex(ctx, run.ID, stepIndex)
	if err != nil {
		return step, nil, false, err
	}
	if step == nil {
		return nil, nil, false, nil
	}
	if step.StepName != stepDef.Name {
		return step, nil, false, fmt.Errorf("workflow step %d is %s, not %s", stepIndex, step.StepName, stepDef.Name)
	}
	if step.Status != WorkflowStepStatusSucceeded {
		return step, nil, false, nil
	}
	return step, cloneWorkflowMap(step.Output), true, nil
}

func (e *WorkflowEngine) executeStep(ctx context.Context, run *WorkflowRun, stepIndex int, stepDef WorkflowStepDefinition, handler WorkflowStepHandler, resume bool) (*WorkflowStepRun, map[string]any, error) {
	started := time.Now().UTC()
	isExistingStep := false
	step := &WorkflowStepRun{
		ID:             NewWorkflowStepRunID(),
		RunID:          run.ID,
		StepIndex:      stepIndex,
		StepName:       stepDef.Name,
		IdempotencyKey: workflowStepIdempotencyKey(run, stepIndex, stepDef.Name),
		Attempt:        1,
		Status:         WorkflowStepStatusRunning,
		Input:          cloneWorkflowMap(run.State),
		StartedAt:      started,
	}
	if resume {
		if resumeStore, ok := e.store.(WorkflowResumeStore); ok {
			existing, err := resumeStore.GetWorkflowStepByIndex(ctx, run.ID, stepIndex)
			if err != nil {
				return step, nil, err
			}
			if existing != nil {
				isExistingStep = true
				step = existing
				step.Status = WorkflowStepStatusRunning
				step.Input = cloneWorkflowMap(run.State)
				step.Output = nil
				step.Error = ""
				step.Attempt++
				if step.Attempt <= 0 {
					step.Attempt = 1
				}
				step.StartedAt = started
				step.FinishedAt = nil
				if step.IdempotencyKey == "" {
					step.IdempotencyKey = workflowStepIdempotencyKey(run, stepIndex, stepDef.Name)
				}
				if err := e.store.UpdateWorkflowStepRun(ctx, step); err != nil {
					return step, nil, err
				}
			}
		}
	}
	if step.ID == "" {
		step.ID = NewWorkflowStepRunID()
	}
	if !isExistingStep {
		if err := e.store.AddWorkflowStepRun(ctx, step); err != nil {
			return step, nil, err
		}
	}
	_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Step: cloneWorkflowStepRun(step), Status: step.Status, Type: "workflow_step_started"})

	stepCtx := ctx
	cancel := func() {}
	if stepDef.Timeout > 0 {
		stepCtx, cancel = context.WithTimeout(ctx, stepDef.Timeout)
	}
	stepCtx = harnessengine.WithToolExecutionScope(stepCtx, harnessengine.ToolExecutionScope{
		UserID:            run.UserID,
		SessionID:         run.SessionID,
		JobID:             run.JobID,
		RequestID:         run.RequestID,
		WorkflowRunID:     run.ID,
		WorkflowStepID:    step.ID,
		WorkflowStepIndex: step.StepIndex,
	})
	defer cancel()

	output, err := handler(stepCtx, run, cloneWorkflowMap(run.State))
	finished := time.Now().UTC()
	step.FinishedAt = &finished
	step.Output = cloneWorkflowMap(output)
	if err != nil {
		step.Status = WorkflowStepStatusFailed
		step.Error = err.Error()
		if updateErr := e.store.UpdateWorkflowStepRun(ctx, step); updateErr != nil {
			return step, output, updateErr
		}
		return step, output, err
	}
	step.Status = WorkflowStepStatusSucceeded
	if err := e.store.UpdateWorkflowStepRun(ctx, step); err != nil {
		return step, output, err
	}
	return step, output, nil
}

func (e *WorkflowEngine) failRun(ctx context.Context, run *WorkflowRun, err error) (*WorkflowRun, error) {
	finished := time.Now().UTC()
	run.Status = WorkflowStatusFailed
	run.Error = err.Error()
	run.UpdatedAt = finished
	run.FinishedAt = &finished
	if updateErr := e.store.UpdateWorkflowRun(ctx, run); updateErr != nil {
		return nil, updateErr
	}
	_ = e.events.EmitWorkflowEvent(ctx, WorkflowEvent{Run: cloneWorkflowRun(run), Status: run.Status, Type: "workflow_run_failed", Error: err.Error()})
	return cloneWorkflowRun(run), err
}

type MemoryWorkflowStore struct {
	mu       sync.Mutex
	runs     map[string]*WorkflowRun
	steps    map[string]*WorkflowStepRun
	stepList map[string][]string
}

func NewMemoryWorkflowStore() *MemoryWorkflowStore {
	return &MemoryWorkflowStore{
		runs:     make(map[string]*WorkflowRun),
		steps:    make(map[string]*WorkflowStepRun),
		stepList: make(map[string][]string),
	}
}

func (s *MemoryWorkflowStore) CreateWorkflowRun(_ context.Context, run *WorkflowRun) error {
	if s == nil || run == nil || run.ID == "" {
		return fmt.Errorf("workflow run is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.IdempotencyKey != "" {
		for _, existing := range s.runs {
			if existing.UserID == run.UserID && existing.Name == run.Name && existing.IdempotencyKey == run.IdempotencyKey {
				return fmt.Errorf("workflow run idempotency key already exists: %s", run.IdempotencyKey)
			}
		}
	}
	s.runs[run.ID] = cloneWorkflowRun(run)
	return nil
}

func (s *MemoryWorkflowStore) UpdateWorkflowRun(_ context.Context, run *WorkflowRun) error {
	if s == nil || run == nil || run.ID == "" {
		return fmt.Errorf("workflow run is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return fmt.Errorf("workflow run not found: %s", run.ID)
	}
	s.runs[run.ID] = cloneWorkflowRun(run)
	return nil
}

func (s *MemoryWorkflowStore) GetWorkflowRun(_ context.Context, runID string) (*WorkflowRun, error) {
	if s == nil || runID == "" {
		return nil, fmt.Errorf("workflow run id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	run := s.runs[runID]
	if run == nil {
		return nil, fmt.Errorf("workflow run not found: %s", runID)
	}
	return cloneWorkflowRun(run), nil
}

func (s *MemoryWorkflowStore) ListWorkflowRuns(_ context.Context, filter WorkflowRunFilter) ([]*WorkflowRun, error) {
	if s == nil {
		return []*WorkflowRun{}, nil
	}
	filter = normalizeWorkflowRunFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*WorkflowRun, 0, len(s.runs))
	for _, run := range s.runs {
		if !workflowRunMatchesFilter(run, filter) {
			continue
		}
		out = append(out, cloneWorkflowRun(run))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryWorkflowStore) AddWorkflowStepRun(_ context.Context, step *WorkflowStepRun) error {
	if s == nil || step == nil || step.ID == "" || step.RunID == "" {
		return fmt.Errorf("workflow step run is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.stepList[step.RunID] {
		existing := s.steps[id]
		if existing == nil {
			continue
		}
		if existing.StepIndex == step.StepIndex {
			return fmt.Errorf("workflow step index already exists: %s[%d]", step.RunID, step.StepIndex)
		}
		if step.IdempotencyKey != "" && existing.IdempotencyKey == step.IdempotencyKey {
			return fmt.Errorf("workflow step idempotency key already exists: %s", step.IdempotencyKey)
		}
	}
	s.steps[step.ID] = cloneWorkflowStepRun(step)
	s.stepList[step.RunID] = append(s.stepList[step.RunID], step.ID)
	return nil
}

func (s *MemoryWorkflowStore) UpdateWorkflowStepRun(_ context.Context, step *WorkflowStepRun) error {
	if s == nil || step == nil || step.ID == "" {
		return fmt.Errorf("workflow step run is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.steps[step.ID]; !ok {
		return fmt.Errorf("workflow step run not found: %s", step.ID)
	}
	s.steps[step.ID] = cloneWorkflowStepRun(step)
	return nil
}

func (s *MemoryWorkflowStore) ListWorkflowStepRuns(_ context.Context, runID string) ([]*WorkflowStepRun, error) {
	if s == nil || runID == "" {
		return []*WorkflowStepRun{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.stepList[runID]
	out := make([]*WorkflowStepRun, 0, len(ids))
	for _, id := range ids {
		if step := s.steps[id]; step != nil {
			out = append(out, cloneWorkflowStepRun(step))
		}
	}
	return out, nil
}

func (s *MemoryWorkflowStore) FindWorkflowRunByIdempotencyKey(_ context.Context, userID, name, idempotencyKey string) (*WorkflowRun, error) {
	if s == nil || strings.TrimSpace(idempotencyKey) == "" {
		return nil, nil
	}
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.UserID == userID && run.Name == name && run.IdempotencyKey == idempotencyKey {
			return cloneWorkflowRun(run), nil
		}
	}
	return nil, nil
}

func (s *MemoryWorkflowStore) GetWorkflowStepByIndex(_ context.Context, runID string, stepIndex int) (*WorkflowStepRun, error) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.stepList[runID] {
		step := s.steps[id]
		if step != nil && step.StepIndex == stepIndex {
			return cloneWorkflowStepRun(step), nil
		}
	}
	return nil, nil
}

type WorkflowEvent struct {
	Type   string           `json:"type"`
	Run    *WorkflowRun     `json:"run,omitempty"`
	Step   *WorkflowStepRun `json:"step,omitempty"`
	Status string           `json:"status,omitempty"`
	Error  string           `json:"error,omitempty"`
}

type WorkflowEventSink interface {
	EmitWorkflowEvent(ctx context.Context, event WorkflowEvent) error
}

type NoopWorkflowEventSink struct{}

func (NoopWorkflowEventSink) EmitWorkflowEvent(context.Context, WorkflowEvent) error { return nil }

type ContextWorkflowEventSink struct{}

func (ContextWorkflowEventSink) EmitWorkflowEvent(ctx context.Context, event WorkflowEvent) error {
	emit, _ := ctx.Value(jobEventEmitterContextKey{}).(jobEventEmitter)
	if emit == nil {
		return nil
	}
	payload := workflowEventPayload(event)
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return emit(ctx, Event{
		Type:      payload.Type,
		SessionID: payload.SessionID,
		JobID:     payload.JobID,
		Role:      "workflow",
		Content:   payload.Content,
		Error:     event.Error,
		Data:      data,
	})
}

type workflowEventPayloadRecord struct {
	Type         string         `json:"type"`
	WorkflowName string         `json:"workflow_name,omitempty"`
	Version      string         `json:"version,omitempty"`
	RunID        string         `json:"run_id,omitempty"`
	StepName     string         `json:"step_name,omitempty"`
	Status       string         `json:"status,omitempty"`
	UserID       string         `json:"user_id,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	JobID        string         `json:"job_id,omitempty"`
	Content      string         `json:"content,omitempty"`
	Error        string         `json:"error,omitempty"`
	Metrics      map[string]any `json:"metrics,omitempty"`
}

func workflowEventPayload(event WorkflowEvent) workflowEventPayloadRecord {
	record := workflowEventPayloadRecord{Type: event.Type, Status: event.Status, Error: event.Error}
	if record.Type == "" {
		record.Type = "workflow_event"
	}
	if event.Run != nil {
		record.WorkflowName = event.Run.Name
		record.Version = event.Run.Version
		record.RunID = event.Run.ID
		record.UserID = event.Run.UserID
		record.SessionID = event.Run.SessionID
		record.JobID = event.Run.JobID
	}
	if event.Step != nil {
		record.StepName = event.Step.StepName
		record.Status = event.Step.Status
		record.Error = firstNonEmptyString(record.Error, event.Step.Error)
		record.Metrics = workflowStepOutputMetrics(event.Step.Output)
	}
	if record.Content == "" {
		if record.StepName != "" {
			record.Content = record.WorkflowName + "." + record.StepName + " " + record.Status
		} else {
			record.Content = record.WorkflowName + " " + record.Status
		}
	}
	return record
}

func workflowStepOutputMetrics(output map[string]any) map[string]any {
	metrics := make(map[string]any)
	for _, key := range []string{
		"candidate_count",
		"result_count",
		"keyword_count",
		"semantic_count",
		"variant_count",
		"window",
		"expanded",
		"changed_count",
		"existing_count",
		"after_count",
		"artifact_count",
		"attachment_count",
		"content_length",
		"hidden_context_count",
		"message_count",
		"output_length",
		"job_started",
	} {
		if value, ok := output[key]; ok {
			metrics[key] = value
		}
	}
	if len(metrics) == 0 {
		return nil
	}
	return metrics
}

func NewWorkflowRunID() string {
	return "wfr-" + newSortableID()
}

func NewWorkflowStepRunID() string {
	return "wfs-" + newSortableID()
}

func mergeWorkflowState(state map[string]any, output map[string]any) {
	if state == nil || output == nil {
		return
	}
	for key, value := range output {
		state[key] = value
	}
}

func cloneWorkflowRun(run *WorkflowRun) *WorkflowRun {
	if run == nil {
		return nil
	}
	out := *run
	out.State = cloneWorkflowMap(run.State)
	return &out
}

func cloneWorkflowStepRun(step *WorkflowStepRun) *WorkflowStepRun {
	if step == nil {
		return nil
	}
	out := *step
	out.Input = cloneWorkflowMap(step.Input)
	out.Output = cloneWorkflowMap(step.Output)
	out.Metadata = cloneWorkflowMap(step.Metadata)
	return &out
}

func cloneWorkflowMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	data, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		out := make(map[string]any, len(in))
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func normalizeWorkflowRunFilter(filter WorkflowRunFilter) WorkflowRunFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.JobID = strings.TrimSpace(filter.JobID)
	filter.Name = strings.TrimSpace(filter.Name)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Limit < 0 {
		filter.Limit = 0
	}
	return filter
}

func workflowRunMatchesFilter(run *WorkflowRun, filter WorkflowRunFilter) bool {
	if run == nil {
		return false
	}
	if filter.UserID != "" && run.UserID != filter.UserID {
		return false
	}
	if filter.SessionID != "" && run.SessionID != filter.SessionID {
		return false
	}
	if filter.JobID != "" && run.JobID != filter.JobID {
		return false
	}
	if filter.Name != "" && run.Name != filter.Name {
		return false
	}
	if filter.Status != "" && run.Status != filter.Status {
		return false
	}
	return true
}

func normalizeWorkflowRequest(ctx context.Context, req WorkflowRequest, deriveIdempotencyKey bool) WorkflowRequest {
	req.UserID = strings.TrimSpace(req.UserID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.JobID = firstNonEmptyString(strings.TrimSpace(req.JobID), jobIDFromContext(ctx))
	req.RequestID = firstNonEmptyString(strings.TrimSpace(req.RequestID), workflowStateString(req.State, "request_id"))
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if deriveIdempotencyKey {
		req.IdempotencyKey = firstNonEmptyString(req.IdempotencyKey, req.RequestID)
	}
	if req.Definition.Version == "" {
		req.Definition.Version = "v1"
	}
	return req
}

func workflowStateString(state map[string]any, key string) string {
	if state == nil || key == "" {
		return ""
	}
	value, ok := state[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func workflowStepIdempotencyKey(run *WorkflowRun, stepIndex int, stepName string) string {
	base := ""
	if run != nil {
		base = firstNonEmptyString(run.IdempotencyKey, run.ID)
	}
	return fmt.Sprintf("%s:%d:%s", base, stepIndex, stepName)
}

func nextWorkflowStepIndex(steps []*WorkflowStepRun) int {
	next := 0
	for _, step := range steps {
		if step == nil {
			continue
		}
		if step.StepIndex >= next {
			next = step.StepIndex + 1
		}
	}
	return next
}

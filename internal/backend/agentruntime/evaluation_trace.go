package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

type EvaluationTraceSource interface {
	ListEvaluationTraces(ctx context.Context, scope EvaluationScope) ([]EvaluationTrace, error)
}

type EvaluationTrace struct {
	SubjectType     string
	SubjectID       string
	UserID          string
	SessionID       string
	JobID           string
	SkillName       string
	Provider        string
	Model           string
	Input           string
	Output          string
	Job             *Job
	Session         *state.Session
	Messages        []state.Message
	JobEvents       []*JobEvent
	SkillExecutions []SkillExecutionRecord
	LLMUsage        []LLMUsageRecord
	RiskEvents      []RiskEvent
	Artifacts       []*Artifact
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

type RuntimeEvaluationTraceSource struct {
	Runtime       *Runtime
	LLMUsage      LLMUsageAdminStore
	Risk          RiskStore
	MaxJobEvents  int
	MaxLLMRecords int
}

func (s RuntimeEvaluationTraceSource) ListEvaluationTraces(ctx context.Context, scope EvaluationScope) ([]EvaluationTrace, error) {
	scope = normalizeEvaluationScope(scope)
	if s.Runtime == nil {
		return nil, fmt.Errorf("runtime is required")
	}
	switch scope.SubjectType {
	case "", EvaluationSubjectJob:
		return s.listJobTraces(ctx, scope)
	case EvaluationSubjectSession:
		return s.listSessionTraces(ctx, scope)
	case EvaluationSubjectSkillExecution:
		return s.listSkillExecutionTraces(ctx, scope)
	default:
		return nil, fmt.Errorf("unsupported evaluation subject type %q", scope.SubjectType)
	}
}

func (s RuntimeEvaluationTraceSource) listJobTraces(ctx context.Context, scope EvaluationScope) ([]EvaluationTrace, error) {
	if strings.TrimSpace(scope.UserID) == "" {
		return nil, fmt.Errorf("user_id is required for runtime job evaluation")
	}
	jobs, err := s.Runtime.ListJobs(ctx, scope.UserID, scope.SessionID)
	if err != nil {
		return nil, err
	}
	out := make([]EvaluationTrace, 0, len(jobs))
	for _, job := range jobs {
		if job == nil || !jobMatchesEvaluationScope(job, scope) {
			continue
		}
		trace, err := s.traceForJob(ctx, scope, job)
		if err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	return out, nil
}

func (s RuntimeEvaluationTraceSource) traceForJob(ctx context.Context, scope EvaluationScope, job *Job) (EvaluationTrace, error) {
	var session *state.Session
	if strings.TrimSpace(job.SessionID) != "" {
		loaded, err := s.Runtime.GetSession(ctx, job.UserID, job.SessionID)
		if err == nil {
			session = loaded
		}
	}
	events, err := s.Runtime.ListJobEvents(ctx, job.UserID, job.ID, "", s.maxJobEvents())
	if err != nil {
		events = nil
	}
	skills, err := s.Runtime.ListSkillExecutions(ctx, SkillExecutionFilter{
		UserID:    job.UserID,
		SessionID: job.SessionID,
		JobID:     job.ID,
	})
	if err != nil {
		skills = nil
	}
	artifacts, _ := s.Runtime.ListArtifacts(ctx, job.UserID, job.SessionID)
	trace := EvaluationTrace{
		SubjectType:     EvaluationSubjectJob,
		SubjectID:       job.ID,
		UserID:          job.UserID,
		SessionID:       job.SessionID,
		JobID:           job.ID,
		Input:           job.Content,
		Job:             cloneJob(job),
		Session:         session,
		Messages:        sessionMessages(session),
		JobEvents:       cloneJobEvents(events),
		SkillExecutions: filterSkillExecutionsForEvaluation(skills, scope),
		LLMUsage:        s.llmUsageRecords(ctx, scope, job.UserID, job.SessionID),
		RiskEvents:      s.riskEvents(ctx, scope, job.UserID, job.SessionID, job.ID),
		Artifacts:       filterArtifactsForEvaluation(artifacts, job.ID),
		CreatedAt:       job.CreatedAt,
		CompletedAt:     job.FinishedAt,
	}
	trace.Output = latestAssistantOutput(trace.Messages, trace.JobEvents)
	trace.Provider, trace.Model = providerModelFromTrace(trace)
	return trace, nil
}

func (s RuntimeEvaluationTraceSource) listSessionTraces(ctx context.Context, scope EvaluationScope) ([]EvaluationTrace, error) {
	if strings.TrimSpace(scope.UserID) == "" {
		return nil, fmt.Errorf("user_id is required for runtime session evaluation")
	}
	var sessions []*state.Session
	if strings.TrimSpace(scope.SessionID) != "" {
		session, err := s.Runtime.GetSession(ctx, scope.UserID, scope.SessionID)
		if err != nil {
			return nil, err
		}
		sessions = []*state.Session{session}
	} else {
		loaded, err := s.Runtime.ListSessions(ctx, scope.UserID)
		if err != nil {
			return nil, err
		}
		sessions = loaded
	}
	out := make([]EvaluationTrace, 0, len(sessions))
	for _, session := range sessions {
		if session == nil || !sessionMatchesEvaluationScope(session, scope) {
			continue
		}
		jobs, _ := s.Runtime.ListJobs(ctx, scope.UserID, session.ID)
		artifacts, _ := s.Runtime.ListArtifacts(ctx, scope.UserID, session.ID)
		trace := EvaluationTrace{
			SubjectType: EvaluationSubjectSession,
			SubjectID:   session.ID,
			UserID:      scope.UserID,
			SessionID:   session.ID,
			Input:       firstUserInput(session.Messages),
			Output:      latestAssistantOutput(session.Messages, nil),
			Session:     cloneEvaluationSession(session),
			Messages:    sessionMessages(session),
			LLMUsage:    s.llmUsageRecords(ctx, scope, scope.UserID, session.ID),
			RiskEvents:  s.riskEvents(ctx, scope, scope.UserID, session.ID, ""),
			Artifacts:   filterArtifactsForEvaluation(artifacts, ""),
			CreatedAt:   session.StartedAt,
		}
		if len(jobs) > 0 {
			for _, job := range jobs {
				if job != nil {
					trace.JobEvents = append(trace.JobEvents, s.jobEvents(ctx, job.UserID, job.ID)...)
				}
			}
		}
		trace.Provider, trace.Model = providerModelFromTrace(trace)
		out = append(out, trace)
	}
	return out, nil
}

func (s RuntimeEvaluationTraceSource) listSkillExecutionTraces(ctx context.Context, scope EvaluationScope) ([]EvaluationTrace, error) {
	records, err := s.Runtime.ListSkillExecutions(ctx, SkillExecutionFilter{
		SkillName: scope.SkillName,
		UserID:    scope.UserID,
		SessionID: scope.SessionID,
		JobID:     scope.JobID,
	})
	if err != nil {
		return nil, err
	}
	records = filterSkillExecutionsForEvaluation(records, scope)
	out := make([]EvaluationTrace, 0, len(records))
	for _, record := range records {
		trace := EvaluationTrace{
			SubjectType:     EvaluationSubjectSkillExecution,
			SubjectID:       record.ID,
			UserID:          record.UserID,
			SessionID:       record.SessionID,
			JobID:           record.JobID,
			SkillName:       record.SkillName,
			Provider:        record.Provider,
			Model:           record.Model,
			Input:           record.InputSummary,
			Output:          record.Error,
			SkillExecutions: []SkillExecutionRecord{record},
			LLMUsage:        s.llmUsageRecords(ctx, scope, record.UserID, record.SessionID),
			RiskEvents:      s.riskEvents(ctx, scope, record.UserID, record.SessionID, record.JobID),
			CreatedAt:       record.StartedAt,
			CompletedAt:     cloneTimePtr(record.CompletedAt),
		}
		out = append(out, trace)
	}
	return out, nil
}

func (s RuntimeEvaluationTraceSource) jobEvents(ctx context.Context, userID, jobID string) []*JobEvent {
	events, err := s.Runtime.ListJobEvents(ctx, userID, jobID, "", s.maxJobEvents())
	if err != nil {
		return nil
	}
	return events
}

func (s RuntimeEvaluationTraceSource) llmUsageRecords(ctx context.Context, scope EvaluationScope, userID, sessionID string) []LLMUsageRecord {
	if s.LLMUsage == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	since := time.Time{}
	if scope.From != nil {
		since = *scope.From
	}
	limit := s.MaxLLMRecords
	if limit <= 0 {
		limit = 100
	}
	summary, err := s.LLMUsage.SummarizeLLMUsage(ctx, LLMUsageAdminFilter{
		UserID:        userID,
		Since:         since,
		Limit:         limit,
		PromptID:      scope.PromptID,
		PromptVersion: scope.PromptVersion,
		PromptHash:    scope.PromptHash,
		ExperimentID:  scope.ExperimentID,
		VariantID:     scope.VariantID,
	})
	if err != nil {
		return nil
	}
	out := make([]LLMUsageRecord, 0, len(summary.Recent))
	for _, record := range summary.Recent {
		if !llmRecordMatchesEvaluationScope(record, scope, userID, sessionID) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (s RuntimeEvaluationTraceSource) riskEvents(ctx context.Context, scope EvaluationScope, userID, sessionID, jobID string) []RiskEvent {
	if s.Risk == nil {
		return nil
	}
	since := time.Time{}
	if scope.From != nil {
		since = *scope.From
	}
	summary, err := s.Risk.ListRiskEvents(ctx, RiskEventFilter{
		UserID:    userID,
		SessionID: sessionID,
		Since:     since,
		Limit:     1000,
	})
	if err != nil {
		return nil
	}
	out := make([]RiskEvent, 0, len(summary.Events))
	for _, event := range summary.Events {
		if !riskEventMatchesEvaluationScope(event, scope, userID, sessionID, jobID) {
			continue
		}
		out = append(out, event)
	}
	return out
}

func (s RuntimeEvaluationTraceSource) maxJobEvents() int {
	if s.MaxJobEvents <= 0 {
		return 500
	}
	return s.MaxJobEvents
}

func jobMatchesEvaluationScope(job *Job, scope EvaluationScope) bool {
	if job == nil {
		return false
	}
	if scope.JobID != "" && job.ID != scope.JobID {
		return false
	}
	if scope.JobStatus != "" && job.Status != scope.JobStatus {
		return false
	}
	if scope.From != nil && job.UpdatedAt.Before(*scope.From) {
		return false
	}
	if scope.To != nil && !job.UpdatedAt.Before(*scope.To) {
		return false
	}
	return true
}

func sessionMatchesEvaluationScope(session *state.Session, scope EvaluationScope) bool {
	if session == nil {
		return false
	}
	if scope.SessionID != "" && session.ID != scope.SessionID {
		return false
	}
	if scope.From != nil && session.UpdatedAt.Before(*scope.From) {
		return false
	}
	if scope.To != nil && !session.UpdatedAt.Before(*scope.To) {
		return false
	}
	return true
}

func llmRecordMatchesEvaluationScope(record LLMUsageRecord, scope EvaluationScope, userID, sessionID string) bool {
	if userID != "" && record.UserID != userID {
		return false
	}
	if sessionID != "" && record.SessionID != sessionID {
		return false
	}
	if scope.Provider != "" && record.Provider != scope.Provider {
		return false
	}
	if scope.Model != "" && record.Model != scope.Model {
		return false
	}
	if scope.PromptID != "" && record.PromptID != scope.PromptID {
		return false
	}
	if scope.PromptVersion != "" && record.PromptVersion != scope.PromptVersion {
		return false
	}
	if scope.PromptHash != "" && record.PromptHash != scope.PromptHash {
		return false
	}
	if scope.ExperimentID != "" && record.ExperimentID != scope.ExperimentID {
		return false
	}
	if scope.VariantID != "" && record.VariantID != scope.VariantID {
		return false
	}
	if scope.From != nil && record.CreatedAt.Before(*scope.From) {
		return false
	}
	if scope.To != nil && !record.CreatedAt.Before(*scope.To) {
		return false
	}
	return true
}

func riskEventMatchesEvaluationScope(event RiskEvent, scope EvaluationScope, userID, sessionID, jobID string) bool {
	if userID != "" && event.UserID != userID {
		return false
	}
	if sessionID != "" && event.SessionID != sessionID {
		return false
	}
	if jobID != "" && event.JobID != jobID {
		return false
	}
	if scope.From != nil && event.CreatedAt.Before(*scope.From) {
		return false
	}
	if scope.To != nil && !event.CreatedAt.Before(*scope.To) {
		return false
	}
	return true
}

func filterSkillExecutionsForEvaluation(records []SkillExecutionRecord, scope EvaluationScope) []SkillExecutionRecord {
	out := make([]SkillExecutionRecord, 0, len(records))
	for _, record := range records {
		if scope.Provider != "" && record.Provider != scope.Provider {
			continue
		}
		if scope.Model != "" && record.Model != scope.Model {
			continue
		}
		if scope.From != nil && record.CompletedAt.Before(*scope.From) {
			continue
		}
		if scope.To != nil && !record.CompletedAt.Before(*scope.To) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func filterArtifactsForEvaluation(artifacts []*Artifact, jobID string) []*Artifact {
	out := make([]*Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil || artifact.Kind != AssetKindArtifact {
			continue
		}
		if strings.TrimSpace(jobID) != "" && strings.TrimSpace(artifact.JobID) != "" && artifact.JobID != jobID {
			continue
		}
		out = append(out, artifact)
	}
	return out
}

func sessionMessages(session *state.Session) []state.Message {
	if session == nil {
		return nil
	}
	return append([]state.Message(nil), session.Messages...)
}

func cloneEvaluationSession(session *state.Session) *state.Session {
	if session == nil {
		return nil
	}
	cloned := *session
	cloned.Messages = append([]state.Message(nil), session.Messages...)
	cloned.Tags = append([]string(nil), session.Tags...)
	if session.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(session.Metadata))
		for key, value := range session.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return &cloned
}

func cloneTimePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	cloned := value
	return &cloned
}

func cloneJobEvents(events []*JobEvent) []*JobEvent {
	out := make([]*JobEvent, 0, len(events))
	for _, event := range events {
		out = append(out, cloneJobEvent(event))
	}
	return out
}

func firstUserInput(messages []state.Message) string {
	for _, message := range messages {
		if message.Role == state.MessageRoleUser && !message.Hidden && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	return ""
}

func latestAssistantOutput(messages []state.Message, events []*JobEvent) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role == state.MessageRoleAssistant && !message.Hidden && strings.TrimSpace(message.Content) != "" {
			return message.Content
		}
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event != nil && event.Event.Role == state.MessageRoleAssistant && strings.TrimSpace(event.Event.Content) != "" {
			return event.Event.Content
		}
	}
	return ""
}

func providerModelFromTrace(trace EvaluationTrace) (string, string) {
	for _, record := range trace.LLMUsage {
		if record.Provider != "" || record.Model != "" {
			return record.Provider, record.Model
		}
	}
	for _, record := range trace.SkillExecutions {
		if record.Provider != "" || record.Model != "" {
			return record.Provider, record.Model
		}
	}
	return trace.Provider, trace.Model
}

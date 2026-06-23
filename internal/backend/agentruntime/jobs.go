package agentruntime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
)

type jobContextKey struct{}
type jobEventEmitterContextKey struct{}
type jobEventEmitter func(context.Context, Event) error

func WithJobID(ctx context.Context, jobID string) context.Context {
	if strings.TrimSpace(jobID) == "" {
		return ctx
	}
	return context.WithValue(ctx, jobContextKey{}, jobID)
}

func jobIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(jobContextKey{}).(string)
	return id
}

func withJobEventEmitter(ctx context.Context, emit jobEventEmitter) context.Context {
	if emit == nil {
		return ctx
	}
	return context.WithValue(ctx, jobEventEmitterContextKey{}, emit)
}

func emitJobEventFromContext(ctx context.Context, event Event) {
	emit, _ := ctx.Value(jobEventEmitterContextKey{}).(jobEventEmitter)
	if emit == nil {
		return
	}
	_ = emit(context.Background(), event)
}

func NewJobID() string {
	return "job-" + newSortableID()
}

func NewJobEventID() string {
	return "evt-" + newSortableID()
}

func newSortableID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(data[:])
}

type MemoryJobStore struct {
	mu     sync.Mutex
	jobs   map[string]*Job
	events map[string][]*JobEvent
}

func NewMemoryJobStore() *MemoryJobStore {
	return &MemoryJobStore{
		jobs:   make(map[string]*Job),
		events: make(map[string][]*JobEvent),
	}
}

func (s *MemoryJobStore) Init(context.Context) error { return nil }

func (s *MemoryJobStore) CreateJob(_ context.Context, job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

func (s *MemoryJobStore) GetJob(_ context.Context, userID, jobID string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok || job.UserID != userID {
		return nil, sql.ErrNoRows
	}
	return cloneJob(job), nil
}

func (s *MemoryJobStore) ListJobs(_ context.Context, userID, sessionID string) ([]*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Job, 0)
	for _, job := range s.jobs {
		if job.UserID != userID {
			continue
		}
		if strings.TrimSpace(sessionID) != "" && job.SessionID != sessionID {
			continue
		}
		out = append(out, cloneJob(job))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MemoryJobStore) UpdateJobStatus(_ context.Context, userID, jobID, status, errorText string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok || job.UserID != userID {
		return sql.ErrNoRows
	}
	job.Status = status
	job.Error = errorText
	job.UpdatedAt = at
	if status == JobStatusRunning && job.StartedAt == nil {
		job.StartedAt = &at
	}
	if isTerminalJobStatus(status) {
		job.FinishedAt = &at
	}
	return nil
}

func (s *MemoryJobStore) AddJobEvent(_ context.Context, event *JobEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[event.JobID] = append(s.events[event.JobID], cloneJobEvent(event))
	return nil
}

func (s *MemoryJobStore) ListJobEvents(_ context.Context, userID, jobID, afterID string, limit int) ([]*JobEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok || job.UserID != userID {
		return nil, sql.ErrNoRows
	}
	items := s.events[jobID]
	out := make([]*JobEvent, 0, len(items))
	afterSeen := afterID == ""
	for _, item := range items {
		if !afterSeen {
			if item.ID == afterID {
				afterSeen = true
			}
			continue
		}
		out = append(out, cloneJobEvent(item))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryJobStore) DeleteSession(_ context.Context, userID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if job.UserID == userID && job.SessionID == sessionID {
			delete(s.jobs, id)
			delete(s.events, id)
		}
	}
	return nil
}

func (s *MemoryJobStore) DeleteUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if job.UserID == userID {
			delete(s.jobs, id)
			delete(s.events, id)
		}
	}
	return nil
}

func (s *MemoryJobStore) PruneBefore(_ context.Context, cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, job := range s.jobs {
		if isTerminalJobStatus(job.Status) && job.UpdatedAt.Before(cutoff) {
			delete(s.jobs, id)
			delete(s.events, id)
			count++
		}
	}
	return count, nil
}

type SQLJobStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

func NewSQLJobStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLJobStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLJobStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLJobStore) Init(ctx context.Context) error {
	if err := requireSQLColumns(ctx, s.db, "agent_jobs",
		"job_id", "user_id", "session_id", "loop_goal_id", "type", "status", "content", "attachments", "error",
		"created_at", "updated_at", "started_at", "finished_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_job_events",
		"event_id", "job_id", "user_id", "session_id", "type", "payload", "created_at",
	)
}

func (s *SQLJobStore) CreateJob(ctx context.Context, job *Job) error {
	attachments, err := json.Marshal(jobAttachments{
		IDs:              job.AttachmentIDs,
		URLs:             job.AttachmentURLs,
		ConnectorContext: job.ConnectorContext,
	})
	if err != nil {
		return err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.InsertJob(ctx, dbsqlc.InsertJobParams{
			JobID:       job.ID,
			UserID:      job.UserID,
			SessionID:   job.SessionID,
			LoopGoalID:  job.LoopGoalID,
			Type:        job.Type,
			Status:      job.Status,
			Content:     sql.NullString{String: job.Content, Valid: true},
			Attachments: string(attachments),
			Error:       sql.NullString{String: job.Error, Valid: true},
			CreatedAt:   job.CreatedAt.UTC(),
			UpdatedAt:   job.UpdatedAt.UTC(),
			StartedAt:   sqlNullTime(job.StartedAt),
			FinishedAt:  sqlNullTime(job.FinishedAt),
		})
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_jobs (job_id, user_id, session_id, loop_goal_id, type, status, content, attachments, error, created_at, updated_at, started_at, finished_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		job.ID, job.UserID, job.SessionID, job.LoopGoalID, job.Type, job.Status, job.Content, string(attachments), job.Error,
		sqlTimeValue(job.CreatedAt, s.dialect), sqlTimeValue(job.UpdatedAt, s.dialect), nullableSQLTimeValue(job.StartedAt, s.dialect), nullableSQLTimeValue(job.FinishedAt, s.dialect))
	return err
}

func (s *SQLJobStore) GetJob(ctx context.Context, userID, jobID string) (*Job, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		job, err := s.queries.GetJob(ctx, dbsqlc.GetJobParams{UserID: userID, JobID: jobID})
		if err != nil {
			return nil, err
		}
		return jobFromSQLC(job), nil
	}
	return s.scanJob(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT job_id, user_id, session_id, loop_goal_id, type, status, content, attachments, error, created_at, updated_at, started_at, finished_at
FROM agent_jobs WHERE user_id = ? AND job_id = ?`), userID, jobID))
}

func (s *SQLJobStore) ListJobs(ctx context.Context, userID, sessionID string) ([]*Job, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListJobs(ctx, dbsqlc.ListJobsParams{UserID: userID, SessionID: strings.TrimSpace(sessionID)})
		if err != nil {
			return nil, err
		}
		return jobsFromSQLC(rows), nil
	}
	query := `SELECT job_id, user_id, session_id, loop_goal_id, type, status, content, attachments, error, created_at, updated_at, started_at, finished_at FROM agent_jobs WHERE user_id = ?`
	args := []any{userID}
	if strings.TrimSpace(sessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Job, 0)
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *SQLJobStore) UpdateJobStatus(ctx context.Context, userID, jobID, status, errorText string, at time.Time) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		var startedAt sql.NullTime
		if status == JobStatusRunning {
			startedAt = sqlNullTime(&at)
		}
		var finishedAt sql.NullTime
		if isTerminalJobStatus(status) {
			finishedAt = sqlNullTime(&at)
		}
		return s.queries.UpdateJobStatus(ctx, dbsqlc.UpdateJobStatusParams{
			Status:     status,
			Error:      sql.NullString{String: errorText, Valid: true},
			UpdatedAt:  at.UTC(),
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			UserID:     userID,
			JobID:      jobID,
		})
	}
	var startedAt any
	if status == JobStatusRunning {
		startedAt = sqlTimeValue(at, s.dialect)
	}
	var finishedAt any
	if isTerminalJobStatus(status) {
		finishedAt = sqlTimeValue(at, s.dialect)
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_jobs
SET status = ?, error = ?, updated_at = ?,
	started_at = COALESCE(started_at, ?),
	finished_at = COALESCE(?, finished_at)
WHERE user_id = ? AND job_id = ?`),
		status, errorText, sqlTimeValue(at, s.dialect), startedAt, finishedAt, userID, jobID)
	return err
}

func (s *SQLJobStore) AddJobEvent(ctx context.Context, event *JobEvent) error {
	payload, err := json.Marshal(event.Event)
	if err != nil {
		return err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.InsertJobEvent(ctx, dbsqlc.InsertJobEventParams{
			EventID:   event.ID,
			JobID:     event.JobID,
			UserID:    event.UserID,
			SessionID: event.SessionID,
			Type:      event.Type,
			Payload:   string(payload),
			CreatedAt: event.CreatedAt.UTC(),
		})
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_job_events (event_id, job_id, user_id, session_id, type, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`),
		event.ID, event.JobID, event.UserID, event.SessionID, event.Type, string(payload), sqlTimeValue(event.CreatedAt, s.dialect))
	return err
}

func (s *SQLJobStore) ListJobEvents(ctx context.Context, userID, jobID, afterID string, limit int) ([]*JobEvent, error) {
	if _, err := s.GetJob(ctx, userID, jobID); err != nil {
		return nil, err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		if limit < 0 {
			limit = 0
		}
		rows, err := s.queries.ListJobEvents(ctx, dbsqlc.ListJobEventsParams{
			UserID:     userID,
			JobID:      jobID,
			AfterID:    strings.TrimSpace(afterID),
			LimitCount: int32(limit),
		})
		if err != nil {
			return nil, err
		}
		return jobEventsFromSQLC(rows), nil
	}
	query := `SELECT event_id, job_id, user_id, session_id, type, payload, created_at FROM agent_job_events WHERE user_id = ? AND job_id = ?`
	args := []any{userID, jobID}
	if strings.TrimSpace(afterID) != "" {
		query += ` AND event_id > ?`
		args = append(args, afterID)
	}
	query += ` ORDER BY event_id ASC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*JobEvent, 0)
	for rows.Next() {
		event, err := scanJobEventRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *SQLJobStore) DeleteSession(ctx context.Context, userID, sessionID string) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.DeleteSessionJobs(ctx, dbsqlc.DeleteSessionJobsParams{UserID: userID, SessionID: sessionID})
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT job_id FROM agent_jobs WHERE user_id = ? AND session_id = ?`), userID, sessionID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_job_events WHERE user_id = ? AND job_id = ?`), userID, id); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_jobs WHERE user_id = ? AND session_id = ?`), userID, sessionID)
	return err
}

func (s *SQLJobStore) DeleteUser(ctx context.Context, userID string) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.DeleteUserJobs(ctx, userID)
	}
	if _, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_job_events WHERE user_id = ?`), userID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_jobs WHERE user_id = ?`), userID)
	return err
}

func (s *SQLJobStore) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.PruneTerminalJobsBefore(ctx, dbsqlc.PruneTerminalJobsBeforeParams{
			UpdatedAt: cutoff.UTC(),
			Status:    JobStatusSucceeded,
			Status_2:  JobStatusFailed,
			Status_3:  JobStatusCancelled,
		})
		return int(rows), err
	}
	cutoffValue := sqlTimeValue(cutoff, s.dialect)
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT job_id, user_id FROM agent_jobs WHERE updated_at < ? AND status IN (?, ?, ?)`), cutoffValue, JobStatusSucceeded, JobStatusFailed, JobStatusCancelled)
	if err != nil {
		return 0, err
	}
	type jobRef struct{ id, userID string }
	var refs []jobRef
	for rows.Next() {
		var ref jobRef
		if err := rows.Scan(&ref.id, &ref.userID); err != nil {
			rows.Close()
			return 0, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, ref := range refs {
		if _, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_job_events WHERE user_id = ? AND job_id = ?`), ref.userID, ref.id); err != nil {
			return 0, err
		}
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_jobs WHERE updated_at < ? AND status IN (?, ?, ?)`), cutoffValue, JobStatusSucceeded, JobStatusFailed, JobStatusCancelled)
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (s *SQLJobStore) scanJob(row *sql.Row) (*Job, error) {
	return scanJobRows(row)
}

type jobScanner interface {
	Scan(dest ...any) error
}

type jobAttachments struct {
	IDs              []string            `json:"ids,omitempty"`
	URLs             []ChatAttachmentURL `json:"urls,omitempty"`
	ConnectorContext []string            `json:"connector_context,omitempty"`
}

func scanJobRows(row jobScanner) (*Job, error) {
	var job Job
	var createdAt, updatedAt, startedAt, finishedAt any
	var attachments string
	if err := row.Scan(&job.ID, &job.UserID, &job.SessionID, &job.LoopGoalID, &job.Type, &job.Status, &job.Content, &attachments, &job.Error, &createdAt, &updatedAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if strings.TrimSpace(attachments) != "" {
		var parsed jobAttachments
		if err := json.Unmarshal([]byte(attachments), &parsed); err == nil {
			job.AttachmentIDs = parsed.IDs
			job.AttachmentURLs = parsed.URLs
			job.ConnectorContext = normalizeConnectorScopes(parsed.ConnectorContext)
		}
	}
	var err error
	if job.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if job.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return nil, err
	}
	if job.StartedAt, err = parseNullableSQLTime(startedAt); err != nil {
		return nil, err
	}
	if job.FinishedAt, err = parseNullableSQLTime(finishedAt); err != nil {
		return nil, err
	}
	return &job, nil
}

func jobFromSQLC(row dbsqlc.AgentJob) *Job {
	job := &Job{
		ID:         row.JobID,
		UserID:     row.UserID,
		SessionID:  row.SessionID,
		LoopGoalID: row.LoopGoalID,
		Type:       row.Type,
		Status:     row.Status,
		Content:    row.Content.String,
		Error:      row.Error.String,
		CreatedAt:  row.CreatedAt.UTC(),
		UpdatedAt:  row.UpdatedAt.UTC(),
		StartedAt:  timeFromNull(row.StartedAt),
		FinishedAt: timeFromNull(row.FinishedAt),
	}
	if strings.TrimSpace(row.Attachments) != "" {
		var parsed jobAttachments
		if err := json.Unmarshal([]byte(row.Attachments), &parsed); err == nil {
			job.AttachmentIDs = parsed.IDs
			job.AttachmentURLs = parsed.URLs
			job.ConnectorContext = normalizeConnectorScopes(parsed.ConnectorContext)
		}
	}
	return job
}

func jobsFromSQLC(rows []dbsqlc.AgentJob) []*Job {
	out := make([]*Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobFromSQLC(row))
	}
	return out
}

func scanJobEventRows(row jobScanner) (*JobEvent, error) {
	var event JobEvent
	var payload string
	var createdAt any
	if err := row.Scan(&event.ID, &event.JobID, &event.UserID, &event.SessionID, &event.Type, &payload, &createdAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(payload), &event.Event)
	var err error
	if event.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	return &event, nil
}

func jobEventFromSQLC(row dbsqlc.AgentJobEvent) *JobEvent {
	event := &JobEvent{
		ID:        row.EventID,
		JobID:     row.JobID,
		UserID:    row.UserID,
		SessionID: row.SessionID,
		Type:      row.Type,
		CreatedAt: row.CreatedAt.UTC(),
	}
	_ = json.Unmarshal([]byte(row.Payload), &event.Event)
	return event
}

func jobEventsFromSQLC(rows []dbsqlc.AgentJobEvent) []*JobEvent {
	out := make([]*JobEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobEventFromSQLC(row))
	}
	return out
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	clone := *job
	clone.AttachmentIDs = append([]string(nil), job.AttachmentIDs...)
	clone.AttachmentURLs = append([]ChatAttachmentURL(nil), job.AttachmentURLs...)
	if job.StartedAt != nil {
		startedAt := *job.StartedAt
		clone.StartedAt = &startedAt
	}
	if job.FinishedAt != nil {
		finishedAt := *job.FinishedAt
		clone.FinishedAt = &finishedAt
	}
	return &clone
}

func cloneJobEvent(event *JobEvent) *JobEvent {
	if event == nil {
		return nil
	}
	clone := *event
	if event.Event.Data != nil {
		clone.Event.Data = append([]byte(nil), event.Event.Data...)
	}
	return &clone
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case JobStatusSucceeded, JobStatusFailed, JobStatusCancelled:
		return true
	default:
		return false
	}
}

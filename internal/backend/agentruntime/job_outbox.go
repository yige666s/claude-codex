package agentruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultJobQueueOutboxBatch = 32
	defaultJobQueueOutboxPoll  = 250 * time.Millisecond
	defaultJobQueueOutboxLease = 30 * time.Second
)

type JobQueueOutboxItem struct {
	Item     JobQueueItem
	Attempts int
}

type JobQueueScheduleStore interface {
	ScheduleJob(ctx context.Context, userID, jobID string, item JobQueueItem, at time.Time) error
}

type JobQueueOutboxStore interface {
	JobQueueScheduleStore
	RecoverJobQueueOutbox(ctx context.Context, queuedBefore time.Time, limit int) (int, error)
	ClaimJobQueueOutbox(ctx context.Context, owner string, lease time.Duration, limit int) ([]JobQueueOutboxItem, error)
	MarkJobQueueOutboxPublished(ctx context.Context, jobID, owner string) error
	RetryJobQueueOutbox(ctx context.Context, jobID, owner, errorText string, retryAt time.Time) error
}

func (s *SQLJobStore) ScheduleJob(ctx context.Context, userID, jobID string, item JobQueueItem, at time.Time) error {
	if s == nil || s.db == nil || s.dialect != SQLDialectPostgres {
		return fmt.Errorf("durable job queue scheduling requires PostgreSQL")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
UPDATE agent_jobs
SET status = $3, updated_at = $4
WHERE user_id = $1 AND job_id = $2 AND status = $3
`, userID, jobID, JobStatusQueued, at.UTC())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("job %s is not schedulable", jobID)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO agent_job_queue_outbox (job_id, user_id, request_id, hide_user_message)
VALUES ($1, $2, $3, $4)
ON CONFLICT (job_id) DO UPDATE
SET request_id = CASE
		WHEN agent_job_queue_outbox.published_at IS NULL THEN EXCLUDED.request_id
		ELSE agent_job_queue_outbox.request_id
	END,
	hide_user_message = CASE
		WHEN agent_job_queue_outbox.published_at IS NULL THEN EXCLUDED.hide_user_message
		ELSE agent_job_queue_outbox.hide_user_message
	END,
	updated_at = now()
`, jobID, userID, item.RequestID, item.HideUserMessage); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLJobStore) RecoverJobQueueOutbox(ctx context.Context, queuedBefore time.Time, limit int) (int, error) {
	if s == nil || s.db == nil || s.dialect != SQLDialectPostgres {
		return 0, nil
	}
	if limit <= 0 {
		limit = defaultJobQueueOutboxBatch
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO agent_job_queue_outbox (job_id, user_id)
SELECT job_id, user_id
FROM agent_jobs
WHERE status = $1 AND created_at <= $2
ORDER BY created_at ASC
LIMIT $3
ON CONFLICT (job_id) DO NOTHING
`, JobStatusQueued, queuedBefore.UTC(), limit)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return int(rows), err
}

func (s *SQLJobStore) ClaimJobQueueOutbox(ctx context.Context, owner string, lease time.Duration, limit int) ([]JobQueueOutboxItem, error) {
	if s == nil || s.db == nil || s.dialect != SQLDialectPostgres {
		return nil, nil
	}
	if lease <= 0 {
		lease = defaultJobQueueOutboxLease
	}
	if limit <= 0 {
		limit = defaultJobQueueOutboxBatch
	}
	rows, err := s.db.QueryContext(ctx, `
WITH candidates AS (
	SELECT job_id
	FROM agent_job_queue_outbox
	WHERE published_at IS NULL
	  AND available_at <= now()
	  AND (claimed_until IS NULL OR claimed_until < now())
	ORDER BY created_at ASC
	FOR UPDATE SKIP LOCKED
	LIMIT $1
)
UPDATE agent_job_queue_outbox AS outbox
SET claimed_by = $2,
	claimed_until = now() + ($3 * interval '1 millisecond'),
	attempts = attempts + 1,
	updated_at = now()
FROM candidates
WHERE outbox.job_id = candidates.job_id
RETURNING outbox.job_id, outbox.user_id, outbox.request_id, outbox.hide_user_message, outbox.attempts
`, limit, owner, lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]JobQueueOutboxItem, 0, limit)
	for rows.Next() {
		var item JobQueueOutboxItem
		if err := rows.Scan(
			&item.Item.JobID,
			&item.Item.UserID,
			&item.Item.RequestID,
			&item.Item.HideUserMessage,
			&item.Attempts,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLJobStore) MarkJobQueueOutboxPublished(ctx context.Context, jobID, owner string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE agent_job_queue_outbox
SET published_at = now(), claimed_by = '', claimed_until = NULL, last_error = '', updated_at = now()
WHERE job_id = $1 AND published_at IS NULL AND claimed_by = $2
`, jobID, owner)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("job queue outbox claim lost for %s", jobID)
	}
	return nil
}

func (s *SQLJobStore) RetryJobQueueOutbox(ctx context.Context, jobID, owner, errorText string, retryAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE agent_job_queue_outbox
SET available_at = $3, claimed_by = '', claimed_until = NULL, last_error = $4, updated_at = now()
WHERE job_id = $1 AND published_at IS NULL AND claimed_by = $2
`, jobID, owner, retryAt.UTC(), errorText)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("job queue outbox claim lost for %s", jobID)
	}
	return nil
}

type JobQueueOutboxWorker struct {
	store  JobQueueOutboxStore
	queue  JobQueue
	owner  string
	batch  int
	poll   time.Duration
	lease  time.Duration
	logger *slog.Logger
}

func NewJobQueueOutboxWorker(store JobQueueOutboxStore, queue JobQueue, logger *slog.Logger) *JobQueueOutboxWorker {
	return &JobQueueOutboxWorker{
		store:  store,
		queue:  queue,
		owner:  "job-outbox-" + newSortableID(),
		batch:  defaultJobQueueOutboxBatch,
		poll:   defaultJobQueueOutboxPoll,
		lease:  defaultJobQueueOutboxLease,
		logger: componentLogger(logger, "job_queue_outbox"),
	}
}

func (w *JobQueueOutboxWorker) Run(ctx context.Context) error {
	if w == nil || w.store == nil || w.queue == nil {
		return fmt.Errorf("job queue outbox worker is not configured")
	}
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		processed, err := w.runOnce(ctx)
		if err != nil {
			logError(ctx, w.logger, "job queue outbox iteration failed", err)
		}
		if processed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *JobQueueOutboxWorker) runOnce(ctx context.Context) (int, error) {
	recovered, err := w.store.RecoverJobQueueOutbox(ctx, time.Now().UTC().Add(-5*time.Second), w.batch)
	if err != nil {
		return 0, err
	}
	items, err := w.store.ClaimJobQueueOutbox(ctx, w.owner, w.lease, w.batch)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		publishCtx, cancel := context.WithTimeout(ctx, w.lease)
		err := w.queue.EnqueueJob(publishCtx, item.Item)
		cancel()
		if err != nil {
			retryAt := time.Now().UTC().Add(jobQueueOutboxBackoff(item.Attempts))
			if retryErr := w.store.RetryJobQueueOutbox(ctx, item.Item.JobID, w.owner, err.Error(), retryAt); retryErr != nil {
				return len(items), fmt.Errorf("enqueue job: %w; schedule retry: %v", err, retryErr)
			}
			continue
		}
		if err := w.store.MarkJobQueueOutboxPublished(ctx, item.Item.JobID, w.owner); err != nil {
			return len(items), err
		}
	}
	return len(items) + recovered, nil
}

func jobQueueOutboxBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 8 {
		attempts = 8
	}
	return time.Duration(1<<(attempts-1)) * time.Second
}

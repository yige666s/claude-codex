package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultMessageEventOutboxBatch = 32
	defaultMessageEventOutboxPoll  = 250 * time.Millisecond
	defaultMessageEventOutboxLease = 30 * time.Second
)

type MessageEventOutboxItem struct {
	ID       string
	Event    MessageEvent
	Attempts int
}

type MessageEventOutboxStore interface {
	ClaimMessageEventOutbox(ctx context.Context, owner string, lease time.Duration, limit int) ([]MessageEventOutboxItem, error)
	MarkMessageEventOutboxPublished(ctx context.Context, id, owner string) error
	RetryMessageEventOutbox(ctx context.Context, id, owner, errorText string, retryAt time.Time) error
}

func (s *SQLSessionStore) MessageEventOutboxEnabled() bool {
	return s != nil && s.db != nil && s.dialect == SQLDialectPostgres
}

func enqueueSQLMessageEventTx(ctx context.Context, tx *sql.Tx, dialect SQLDialect, event MessageEvent) error {
	if tx == nil || dialect != SQLDialectPostgres {
		return nil
	}
	if event.Type == "" || event.Message.ID == "" {
		return fmt.Errorf("message event type and message id are required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = event.Message.CreatedAt
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO agent_message_event_outbox (
	event_id, message_id, user_id, session_id, event_type, payload
) VALUES ($1, $2, $3, $4, $5, $6::jsonb)
ON CONFLICT (event_id) DO NOTHING
`, messageEventOutboxID(event.Type, event.Message.ID), event.Message.ID, event.UserID, event.SessionID, event.Type, string(payload))
	return err
}

func messageEventOutboxID(eventType, messageID string) string {
	return eventType + ":" + messageID
}

func (s *SQLSessionStore) ClaimMessageEventOutbox(ctx context.Context, owner string, lease time.Duration, limit int) ([]MessageEventOutboxItem, error) {
	if !s.MessageEventOutboxEnabled() {
		return nil, nil
	}
	if lease <= 0 {
		lease = defaultMessageEventOutboxLease
	}
	if limit <= 0 {
		limit = defaultMessageEventOutboxBatch
	}
	rows, err := s.db.QueryContext(ctx, `
WITH candidates AS (
	SELECT event_id
	FROM agent_message_event_outbox
	WHERE published_at IS NULL
	  AND available_at <= now()
	  AND (claimed_until IS NULL OR claimed_until < now())
	ORDER BY created_at ASC
	FOR UPDATE SKIP LOCKED
	LIMIT $1
)
UPDATE agent_message_event_outbox AS outbox
SET claimed_by = $2,
	claimed_until = now() + ($3 * interval '1 millisecond'),
	attempts = attempts + 1,
	updated_at = now()
FROM candidates
WHERE outbox.event_id = candidates.event_id
RETURNING outbox.event_id, outbox.payload::text, outbox.attempts
`, limit, owner, lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MessageEventOutboxItem, 0, limit)
	for rows.Next() {
		var item MessageEventOutboxItem
		var payload string
		if err := rows.Scan(&item.ID, &payload, &item.Attempts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &item.Event); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLSessionStore) MarkMessageEventOutboxPublished(ctx context.Context, id, owner string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE agent_message_event_outbox
SET published_at = now(), claimed_by = '', claimed_until = NULL, last_error = '', updated_at = now()
WHERE event_id = $1 AND published_at IS NULL AND claimed_by = $2
`, id, owner)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("message event outbox claim lost for %s", id)
	}
	return nil
}

func (s *SQLSessionStore) RetryMessageEventOutbox(ctx context.Context, id, owner, errorText string, retryAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE agent_message_event_outbox
SET available_at = $3, claimed_by = '', claimed_until = NULL, last_error = $4, updated_at = now()
WHERE event_id = $1 AND published_at IS NULL AND claimed_by = $2
`, id, owner, retryAt.UTC(), errorText)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("message event outbox claim lost for %s", id)
	}
	return nil
}

type MessageEventOutboxWorker struct {
	store     MessageEventOutboxStore
	publisher MessageEventPublisher
	owner     string
	batch     int
	poll      time.Duration
	lease     time.Duration
	logger    *slog.Logger
}

func NewMessageEventOutboxWorker(store MessageEventOutboxStore, publisher MessageEventPublisher, logger *slog.Logger) *MessageEventOutboxWorker {
	return &MessageEventOutboxWorker{
		store:     store,
		publisher: publisher,
		owner:     "message-outbox-" + newSortableID(),
		batch:     defaultMessageEventOutboxBatch,
		poll:      defaultMessageEventOutboxPoll,
		lease:     defaultMessageEventOutboxLease,
		logger:    componentLogger(logger, "message_event_outbox"),
	}
}

func (w *MessageEventOutboxWorker) Run(ctx context.Context) error {
	if w == nil || w.store == nil || w.publisher == nil {
		return fmt.Errorf("message event outbox worker is not configured")
	}
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		processed, err := w.runOnce(ctx)
		if err != nil {
			logError(ctx, w.logger, "message event outbox iteration failed", err)
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

func (w *MessageEventOutboxWorker) runOnce(ctx context.Context) (int, error) {
	items, err := w.store.ClaimMessageEventOutbox(ctx, w.owner, w.lease, w.batch)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		publishCtx, cancel := context.WithTimeout(ctx, w.lease)
		err := w.publisher.PublishMessageEvent(publishCtx, item.Event)
		cancel()
		if err != nil {
			retryAt := time.Now().UTC().Add(messageEventOutboxBackoff(item.Attempts))
			if retryErr := w.store.RetryMessageEventOutbox(ctx, item.ID, w.owner, err.Error(), retryAt); retryErr != nil {
				return len(items), fmt.Errorf("publish message event: %w; schedule retry: %v", err, retryErr)
			}
			continue
		}
		if err := w.store.MarkMessageEventOutboxPublished(ctx, item.ID, w.owner); err != nil {
			return len(items), err
		}
	}
	return len(items), nil
}

func messageEventOutboxBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 8 {
		attempts = 8
	}
	return time.Duration(1<<(attempts-1)) * time.Second
}

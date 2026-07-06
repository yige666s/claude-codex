package agentruntime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	defaultMessageSearchBackfillBatchSize = 500
	defaultMessageSearchBackfillInterval  = 10 * time.Minute
)

type MessageFullTextBackfillWorker struct {
	store     MessageFullTextBackfillStore
	indexer   MessageFullTextIndexer
	batchSize int
	interval  time.Duration
	logger    *slog.Logger
}

func NewMessageFullTextBackfillWorker(store MessageFullTextBackfillStore, indexer MessageFullTextIndexer, batchSize int, interval time.Duration, logger *slog.Logger) *MessageFullTextBackfillWorker {
	if batchSize <= 0 || batchSize > 1000 {
		batchSize = defaultMessageSearchBackfillBatchSize
	}
	if interval <= 0 {
		interval = defaultMessageSearchBackfillInterval
	}
	return &MessageFullTextBackfillWorker{
		store:     store,
		indexer:   indexer,
		batchSize: batchSize,
		interval:  interval,
		logger:    componentLogger(logger, "message_fulltext_backfill"),
	}
}

func (w *MessageFullTextBackfillWorker) Run(ctx context.Context) error {
	if w == nil || w.store == nil || w.indexer == nil {
		return fmt.Errorf("message full-text backfill worker is not configured")
	}
	if _, err := w.BackfillOnce(ctx); err != nil && !errorsIsContextDone(ctx, err) {
		logError(ctx, w.logger, "message full-text backfill failed", err)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.BackfillOnce(ctx); err != nil && !errorsIsContextDone(ctx, err) {
				logError(ctx, w.logger, "message full-text backfill failed", err)
			}
		}
	}
}

func (w *MessageFullTextBackfillWorker) BackfillOnce(ctx context.Context) (int, error) {
	if w == nil || w.store == nil || w.indexer == nil {
		return 0, fmt.Errorf("message full-text backfill worker is not configured")
	}
	var afterCreatedAt time.Time
	afterMessageID := ""
	indexed := 0
	for {
		messages, err := w.store.ListMessagesForFullTextBackfill(ctx, afterCreatedAt, afterMessageID, w.batchSize)
		if err != nil {
			return indexed, err
		}
		if len(messages) == 0 {
			if indexed > 0 {
				w.logger.InfoContext(ctx, "message full-text backfill complete", slog.Int("indexed", indexed))
			}
			return indexed, nil
		}
		for _, message := range messages {
			if strings.TrimSpace(message.ID) == "" {
				continue
			}
			if err := w.indexer.IndexMessage(ctx, message); err != nil {
				return indexed, err
			}
			indexed++
			afterCreatedAt = message.CreatedAt.UTC()
			afterMessageID = message.ID
		}
		if len(messages) < w.batchSize {
			w.logger.InfoContext(ctx, "message full-text backfill complete", slog.Int("indexed", indexed))
			return indexed, nil
		}
	}
}

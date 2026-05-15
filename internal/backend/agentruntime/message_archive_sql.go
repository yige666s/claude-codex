package agentruntime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/state"
)

func (s *SQLSessionStore) ListMessagesForArchival(ctx context.Context, cutoff time.Time, limit int) ([]state.Message, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("sql session store is not configured")
	}
	if limit <= 0 || limit > 1000 {
		limit = defaultMessageArchiveBatchSize
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT `+sqlMessageColumns+`
FROM agent_messages
WHERE created_at < ?
  AND archive_uri = ''
ORDER BY created_at ASC
LIMIT ?`), sqlTimeValue(cutoff, s.dialect), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]state.Message, 0, limit)
	for rows.Next() {
		message, err := scanSQLMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.hydrateMessageAttachments(ctx, "", messages)
}

func (s *SQLSessionStore) MarkMessagesArchived(ctx context.Context, records []MessageArchiveRecord, clearPayload bool) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("sql session store is not configured")
	}
	if len(records) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, record := range records {
		if strings.TrimSpace(record.MessageID) == "" || strings.TrimSpace(record.ArchiveURI) == "" {
			continue
		}
		archivedAt := record.ArchivedAt
		if archivedAt.IsZero() {
			archivedAt = time.Now().UTC()
		}
		var result sql.Result
		if clearPayload {
			result, err = tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_messages
SET archive_uri = ?,
	archive_checksum = ?,
	archived_at = ?,
	content = ?,
	content_parts = ?,
	tool_input = ?,
	tool_output = ?,
	tool_calls = ?,
	updated_at = ?
WHERE message_id = ?
  AND archive_uri = ''`),
				record.ArchiveURI,
				record.ArchiveChecksum,
				sqlTimeValue(archivedAt, s.dialect),
				sanitizeSQLText(record.ContentPreview),
				"[]",
				"{}",
				sanitizeSQLText(record.ToolOutputPreview),
				"[]",
				sqlTimeValue(archivedAt, s.dialect),
				record.MessageID,
			)
		} else {
			result, err = tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_messages
SET archive_uri = ?,
	archive_checksum = ?,
	archived_at = ?,
	updated_at = ?
WHERE message_id = ?
  AND archive_uri = ''`),
				record.ArchiveURI,
				record.ArchiveChecksum,
				sqlTimeValue(archivedAt, s.dialect),
				sqlTimeValue(archivedAt, s.dialect),
				record.MessageID,
			)
		}
		if err != nil {
			_ = tx.Rollback()
			return updated, err
		}
		if affected, err := result.RowsAffected(); err == nil {
			updated += int(affected)
		}
	}
	if err := tx.Commit(); err != nil {
		return updated, err
	}
	return updated, nil
}

func (s *SQLSessionStore) hydrateSQLMessages(ctx context.Context, userID string, messages []state.Message) ([]state.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	hydrated, err := s.hydrateArchivedMessages(ctx, messages)
	if err != nil {
		return nil, err
	}
	return s.hydrateMessageAttachments(ctx, userID, hydrated)
}

func (s *SQLSessionStore) hydrateArchivedMessages(ctx context.Context, messages []state.Message) ([]state.Message, error) {
	if s == nil || s.messageArchive == nil || len(messages) == 0 {
		return messages, nil
	}
	for i := range messages {
		uri := strings.TrimSpace(messages[i].ArchiveURI)
		if uri == "" {
			continue
		}
		archived, err := s.messageArchive.ReadMessage(ctx, uri, messages[i].ArchiveChecksum)
		if err != nil {
			return nil, err
		}
		archived.ArchiveURI = messages[i].ArchiveURI
		archived.ArchiveChecksum = messages[i].ArchiveChecksum
		archived.ArchivedAt = messages[i].ArchivedAt
		archived.Status = messages[i].Status
		archived.IsContextUsed = messages[i].IsContextUsed
		archived.UpdatedAt = messages[i].UpdatedAt
		messages[i] = archived
	}
	return messages, nil
}

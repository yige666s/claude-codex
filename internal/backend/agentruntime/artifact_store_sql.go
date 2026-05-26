package agentruntime

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
)

type SQLArtifactStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

func NewSQLArtifactStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLArtifactStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLArtifactStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLArtifactStore) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, s.db, "agent_artifacts",
		"artifact_id",
		"kind",
		"user_id",
		"session_id",
		"job_id",
		"object_key",
		"filename",
		"content_type",
		"size_bytes",
		"created_at",
		"deleted_at",
	)
}

func (s *SQLArtifactStore) Create(ctx context.Context, artifact *Artifact) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.InsertArtifact(ctx, dbsqlc.InsertArtifactParams{
			ArtifactID:  artifact.ID,
			Kind:        normalizeAssetKind(artifact.Kind),
			UserID:      artifact.UserID,
			SessionID:   artifact.SessionID,
			JobID:       artifact.JobID,
			ObjectKey:   artifact.ObjectKey,
			Filename:    artifact.Filename,
			ContentType: artifact.ContentType,
			SizeBytes:   artifact.SizeBytes,
			CreatedAt:   artifact.CreatedAt.UTC(),
			DeletedAt:   sqlNullTime(artifact.DeletedAt),
		})
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_artifacts (artifact_id, kind, user_id, session_id, job_id, object_key, filename, content_type, size_bytes, created_at, deleted_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		artifact.ID,
		normalizeAssetKind(artifact.Kind),
		artifact.UserID,
		artifact.SessionID,
		artifact.JobID,
		artifact.ObjectKey,
		artifact.Filename,
		artifact.ContentType,
		artifact.SizeBytes,
		sqlTimeValue(artifact.CreatedAt, s.dialect),
		nullableSQLTimeValue(artifact.DeletedAt, s.dialect),
	)
	return err
}

func (s *SQLArtifactStore) Get(ctx context.Context, userID, artifactID, kind string) (*Artifact, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		artifact, err := s.queries.GetArtifact(ctx, dbsqlc.GetArtifactParams{
			UserID:     userID,
			ArtifactID: artifactID,
			Kind:       normalizeAssetKind(kind),
		})
		if err != nil {
			return nil, err
		}
		return artifactFromSQLC(artifact), nil
	}
	return s.scanArtifact(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT artifact_id, kind, user_id, session_id, job_id, object_key, filename, content_type, size_bytes, created_at, deleted_at
FROM agent_artifacts WHERE user_id = ? AND artifact_id = ? AND kind = ? AND deleted_at IS NULL`), userID, artifactID, normalizeAssetKind(kind)))
}

func (s *SQLArtifactStore) List(ctx context.Context, userID, sessionID, kind string) ([]*Artifact, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListArtifacts(ctx, dbsqlc.ListArtifactsParams{
			UserID:    userID,
			Kind:      normalizeAssetKind(kind),
			SessionID: sqlNullString(sessionID),
		})
		if err != nil {
			return nil, err
		}
		return artifactsFromSQLC(rows), nil
	}
	query := `SELECT artifact_id, kind, user_id, session_id, job_id, object_key, filename, content_type, size_bytes, created_at, deleted_at
	FROM agent_artifacts WHERE user_id = ? AND kind = ? AND deleted_at IS NULL`
	args := []any{userID, normalizeAssetKind(kind)}
	if strings.TrimSpace(sessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Artifact, 0)
	for rows.Next() {
		artifact, err := scanArtifactRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	return out, rows.Err()
}

func (s *SQLArtifactStore) ListUploadedArtifactsBefore(ctx context.Context, cutoff time.Time) ([]*Artifact, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListUploadedArtifactsBefore(ctx, dbsqlc.ListUploadedArtifactsBeforeParams{
			Kind:      AssetKindArtifact,
			CreatedAt: cutoff.UTC(),
		})
		if err != nil {
			return nil, err
		}
		return artifactsFromSQLC(rows), nil
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT artifact_id, kind, user_id, session_id, job_id, object_key, filename, content_type, size_bytes, created_at, deleted_at
FROM agent_artifacts
WHERE kind = ? AND deleted_at IS NULL AND object_key <> '' AND created_at < ?
ORDER BY created_at ASC`), AssetKindArtifact, sqlTimeValue(cutoff, s.dialect))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Artifact, 0)
	for rows.Next() {
		artifact, err := scanArtifactRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	return out, rows.Err()
}

func (s *SQLArtifactStore) MarkDeleted(ctx context.Context, userID, artifactID, kind string, at time.Time) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.MarkArtifactDeleted(ctx, dbsqlc.MarkArtifactDeletedParams{
			DeletedAt:  sqlNullTime(&at),
			UserID:     userID,
			ArtifactID: artifactID,
			Kind:       normalizeAssetKind(kind),
		})
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_artifacts SET deleted_at = ? WHERE user_id = ? AND artifact_id = ? AND kind = ? AND deleted_at IS NULL`), sqlTimeValue(at, s.dialect), userID, artifactID, normalizeAssetKind(kind))
	return err
}

func (s *SQLArtifactStore) DeleteSession(ctx context.Context, userID, sessionID string) ([]*Artifact, error) {
	artifacts, err := s.List(ctx, userID, sessionID, AssetKindArtifact)
	if err != nil {
		return nil, err
	}
	attachments, err := s.List(ctx, userID, sessionID, AssetKindAttachment)
	if err != nil {
		return nil, err
	}
	items := append(artifacts, attachments...)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		err = s.queries.MarkSessionArtifactsDeleted(ctx, dbsqlc.MarkSessionArtifactsDeletedParams{
			DeletedAt: sqlNullTime(timePtr(time.Now())),
			UserID:    userID,
			SessionID: sessionID,
		})
		return items, err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_artifacts SET deleted_at = ? WHERE user_id = ? AND session_id = ? AND deleted_at IS NULL`), sqlTimeValue(time.Now(), s.dialect), userID, sessionID)
	return items, err
}

func (s *SQLArtifactStore) DeleteUser(ctx context.Context, userID string) ([]*Artifact, error) {
	artifacts, err := s.List(ctx, userID, "", AssetKindArtifact)
	if err != nil {
		return nil, err
	}
	attachments, err := s.List(ctx, userID, "", AssetKindAttachment)
	if err != nil {
		return nil, err
	}
	items := append(artifacts, attachments...)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		err = s.queries.DeleteUserArtifacts(ctx, userID)
		return items, err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_artifacts WHERE user_id = ?`), userID)
	return items, err
}

func (s *SQLArtifactStore) PruneDeletedBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.PruneDeletedArtifactsBefore(ctx, sqlNullTime(&cutoff))
		return int(rows), err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_artifacts WHERE deleted_at IS NOT NULL AND deleted_at < ?`), sqlTimeValue(cutoff, s.dialect))
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *SQLArtifactStore) scanArtifact(row *sql.Row) (*Artifact, error) {
	var createdAt, deletedAt any
	var artifact Artifact
	err := row.Scan(&artifact.ID, &artifact.Kind, &artifact.UserID, &artifact.SessionID, &artifact.JobID, &artifact.ObjectKey, &artifact.Filename, &artifact.ContentType, &artifact.SizeBytes, &createdAt, &deletedAt)
	if err != nil {
		return nil, err
	}
	if artifact.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if artifact.DeletedAt, err = parseNullableSQLTime(deletedAt); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func artifactFromSQLC(row dbsqlc.AgentArtifact) *Artifact {
	return &Artifact{
		ID:          row.ArtifactID,
		Kind:        row.Kind,
		UserID:      row.UserID,
		SessionID:   row.SessionID,
		JobID:       row.JobID,
		ObjectKey:   row.ObjectKey,
		Filename:    row.Filename,
		ContentType: row.ContentType,
		SizeBytes:   row.SizeBytes,
		CreatedAt:   row.CreatedAt.UTC(),
		DeletedAt:   timeFromNull(row.DeletedAt),
	}
}

func artifactsFromSQLC(rows []dbsqlc.AgentArtifact) []*Artifact {
	out := make([]*Artifact, 0, len(rows))
	for _, row := range rows {
		out = append(out, artifactFromSQLC(row))
	}
	return out
}

func sqlNullTime(value *time.Time) sql.NullTime {
	if value == nil || value.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: value.UTC(), Valid: true}
}

func timeFromNull(value sql.NullTime) *time.Time {
	if !value.Valid || value.Time.IsZero() {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func timePtr(value time.Time) *time.Time {
	return &value
}

type artifactScanner interface {
	Scan(dest ...any) error
}

func scanArtifactRows(row artifactScanner) (*Artifact, error) {
	var createdAt, deletedAt any
	var artifact Artifact
	err := row.Scan(&artifact.ID, &artifact.Kind, &artifact.UserID, &artifact.SessionID, &artifact.JobID, &artifact.ObjectKey, &artifact.Filename, &artifact.ContentType, &artifact.SizeBytes, &createdAt, &deletedAt)
	if err != nil {
		return nil, err
	}
	if artifact.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if artifact.DeletedAt, err = parseNullableSQLTime(deletedAt); err != nil {
		return nil, err
	}
	return &artifact, nil
}

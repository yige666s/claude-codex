package agentruntime

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type SQLArtifactStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLArtifactStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLArtifactStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLArtifactStore{db: db, dialect: dialect}
}

func (s *SQLArtifactStore) Init(ctx context.Context) error {
	timeType := s.dialect.TimeType()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS agent_artifacts (
	artifact_id TEXT PRIMARY KEY,
	kind TEXT NOT NULL DEFAULT 'artifact',
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	object_key TEXT NOT NULL,
	filename TEXT NOT NULL,
	content_type TEXT NOT NULL,
	size_bytes BIGINT NOT NULL,
	created_at ` + timeType + ` NOT NULL,
	deleted_at ` + timeType + `
)`,
		`ALTER TABLE agent_artifacts ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'artifact'`,
		`ALTER TABLE agent_artifacts ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_agent_artifacts_user_created ON agent_artifacts (user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_artifacts_kind_user_created ON agent_artifacts (kind, user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_artifacts_session ON agent_artifacts (user_id, session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_artifacts_job ON agent_artifacts (user_id, job_id, created_at)`,
		`INSERT INTO agent_schema_migrations (version, applied_at)
VALUES (3, ` + s.dialect.Placeholder(1) + `)
ON CONFLICT(version) DO NOTHING`,
	} {
		if strings.Contains(stmt, s.dialect.Placeholder(1)) {
			if _, err := s.db.ExecContext(ctx, stmt, sqlTimeValue(time.Now(), s.dialect)); err != nil {
				return err
			}
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_artifacts", "created_at", "deleted_at")
}

func (s *SQLArtifactStore) Create(ctx context.Context, artifact *Artifact) error {
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
	return s.scanArtifact(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT artifact_id, kind, user_id, session_id, job_id, object_key, filename, content_type, size_bytes, created_at, deleted_at
FROM agent_artifacts WHERE user_id = ? AND artifact_id = ? AND kind = ? AND deleted_at IS NULL`), userID, artifactID, normalizeAssetKind(kind)))
}

func (s *SQLArtifactStore) List(ctx context.Context, userID, sessionID, kind string) ([]*Artifact, error) {
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

func (s *SQLArtifactStore) MarkDeleted(ctx context.Context, userID, artifactID, kind string, at time.Time) error {
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
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_artifacts WHERE user_id = ?`), userID)
	return items, err
}

func (s *SQLArtifactStore) PruneDeletedBefore(ctx context.Context, cutoff time.Time) (int, error) {
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

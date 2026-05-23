package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"claude-codex/internal/harness/state"
	"claude-codex/internal/public/fsutil"
)

const defaultProjectColor = "#159a85"

type FileProjectStore struct {
	root string
}

func NewFileProjectStore(root string) *FileProjectStore {
	return &FileProjectStore{root: root}
}

func (s *FileProjectStore) CreateProject(_ context.Context, project Project) (Project, error) {
	project = normalizeProject(project)
	if project.UserID == "" {
		return Project{}, fmt.Errorf("user ID is required")
	}
	if project.ID == "" {
		project.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now
	return project, s.write(project)
}

func (s *FileProjectStore) GetProject(_ context.Context, userID, projectID string) (Project, error) {
	path := s.projectPath(userID, projectID)
	data, err := os.ReadFile(path)
	if err != nil {
		return Project{}, err
	}
	var project Project
	if err := json.Unmarshal(data, &project); err != nil {
		return Project{}, err
	}
	if project.UserID != userID {
		return Project{}, os.ErrNotExist
	}
	return normalizeProject(project), nil
}

func (s *FileProjectStore) ListProjects(_ context.Context, userID string) ([]Project, error) {
	entries, err := os.ReadDir(s.projectsDir(userID))
	if err != nil {
		if os.IsNotExist(err) {
			return []Project{}, nil
		}
		return nil, err
	}
	out := make([]Project, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		project, err := s.GetProject(context.Background(), userID, strings.TrimSuffix(entry.Name(), ".json"))
		if err == nil {
			out = append(out, project)
		}
	}
	sortProjects(out)
	return out, nil
}

func (s *FileProjectStore) UpdateProject(ctx context.Context, userID, projectID string, update ProjectUpdate) (Project, error) {
	project, err := s.GetProject(ctx, userID, projectID)
	if err != nil {
		return Project{}, err
	}
	applyProjectUpdate(&project, update)
	project = normalizeProject(project)
	project.UpdatedAt = time.Now().UTC()
	return project, s.write(project)
}

func (s *FileProjectStore) DeleteProject(_ context.Context, userID, projectID string) error {
	err := os.Remove(s.projectPath(userID, projectID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *FileProjectStore) DeleteUserProjects(_ context.Context, userID string) error {
	err := os.RemoveAll(s.projectsDir(userID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *FileProjectStore) write(project Project) error {
	data, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.projectPath(project.UserID, project.ID), data, 0o644)
}

func (s *FileProjectStore) projectsDir(userID string) string {
	return filepath.Join(s.root, "users", userPathID(userID), "projects")
}

func (s *FileProjectStore) projectPath(userID, projectID string) string {
	return filepath.Join(s.projectsDir(userID), filepath.Base(projectID)+".json")
}

type SQLProjectStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLProjectStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLProjectStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLProjectStore{db: db, dialect: dialect}
}

func (s *SQLProjectStore) Init(ctx context.Context) error {
	timeType := s.dialect.TimeType()
	if err := RunSQLMigrations(ctx, s.db, s.dialect, []SQLMigration{
		{
			Version:    13,
			Statements: projectSchemaStatements(timeType, s.dialect),
		},
	}); err != nil {
		return err
	}
	return ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_projects", "created_at", "updated_at")
}

func projectSchemaStatements(timeType string, dialect SQLDialect) []string {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agent_projects (
	user_id TEXT NOT NULL,
	project_id TEXT NOT NULL,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	instructions TEXT NOT NULL DEFAULT '',
	color TEXT NOT NULL DEFAULT '',
	created_at ` + timeType + ` NOT NULL,
	updated_at ` + timeType + ` NOT NULL,
	PRIMARY KEY (user_id, project_id)
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_projects_user_updated ON agent_projects (user_id, updated_at)`,
	}
	if dialect == SQLDialectPostgres {
		statements = append(statements, `ALTER TABLE agent_sessions ADD COLUMN IF NOT EXISTS project_id TEXT NOT NULL DEFAULT ''`)
	}
	statements = append(statements, `CREATE INDEX IF NOT EXISTS idx_agent_sessions_user_project_updated ON agent_sessions (user_id, project_id, updated_at)`)
	return statements
}

func (s *SQLProjectStore) CreateProject(ctx context.Context, project Project) (Project, error) {
	project = normalizeProject(project)
	if project.UserID == "" {
		return Project{}, fmt.Errorf("user ID is required")
	}
	if project.ID == "" {
		project.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_projects (user_id, project_id, name, description, instructions, color, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		project.UserID,
		project.ID,
		project.Name,
		project.Description,
		project.Instructions,
		project.Color,
		sqlTimeValue(project.CreatedAt, s.dialect),
		sqlTimeValue(project.UpdatedAt, s.dialect),
	)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (s *SQLProjectStore) GetProject(ctx context.Context, userID, projectID string) (Project, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT p.user_id, p.project_id, p.name, p.description, p.instructions, p.color, p.created_at, p.updated_at,
	(SELECT COUNT(1) FROM agent_sessions sess WHERE sess.user_id = p.user_id AND sess.project_id = p.project_id AND sess.status <> ?) AS session_count
FROM agent_projects p
WHERE p.user_id = ? AND p.project_id = ?`), state.SessionStatusDeleted, userID, projectID)
	return scanProject(row)
}

func (s *SQLProjectStore) ListProjects(ctx context.Context, userID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT p.user_id, p.project_id, p.name, p.description, p.instructions, p.color, p.created_at, p.updated_at,
	(SELECT COUNT(1) FROM agent_sessions sess WHERE sess.user_id = p.user_id AND sess.project_id = p.project_id AND sess.status <> ?) AS session_count
FROM agent_projects p
WHERE p.user_id = ?
ORDER BY p.updated_at DESC`), state.SessionStatusDeleted, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLProjectStore) UpdateProject(ctx context.Context, userID, projectID string, update ProjectUpdate) (Project, error) {
	project, err := s.GetProject(ctx, userID, projectID)
	if err != nil {
		return Project{}, err
	}
	applyProjectUpdate(&project, update)
	project = normalizeProject(project)
	project.UpdatedAt = time.Now().UTC()
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_projects
SET name = ?, description = ?, instructions = ?, color = ?, updated_at = ?
WHERE user_id = ? AND project_id = ?`),
		project.Name,
		project.Description,
		project.Instructions,
		project.Color,
		sqlTimeValue(project.UpdatedAt, s.dialect),
		userID,
		projectID,
	)
	if err != nil {
		return Project{}, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return Project{}, sql.ErrNoRows
	}
	return s.GetProject(ctx, userID, projectID)
}

func (s *SQLProjectStore) DeleteProject(ctx context.Context, userID, projectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_sessions
SET project_id = '', updated_at = ?
WHERE user_id = ? AND project_id = ?`), sqlTimeValue(now, s.dialect), userID, projectID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
DELETE FROM agent_projects
WHERE user_id = ? AND project_id = ?`), userID, projectID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLProjectStore) DeleteUserProjects(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_projects WHERE user_id = ?`), userID)
	return err
}

func scanProject(scanner sqlScanner) (Project, error) {
	var project Project
	var createdRaw, updatedRaw any
	if err := scanner.Scan(
		&project.UserID,
		&project.ID,
		&project.Name,
		&project.Description,
		&project.Instructions,
		&project.Color,
		&createdRaw,
		&updatedRaw,
		&project.SessionCount,
	); err != nil {
		return Project{}, err
	}
	createdAt, err := parseSQLTime(createdRaw)
	if err != nil {
		return Project{}, err
	}
	updatedAt, err := parseSQLTime(updatedRaw)
	if err != nil {
		return Project{}, err
	}
	project.CreatedAt = createdAt
	project.UpdatedAt = updatedAt
	return normalizeProject(project), nil
}

func normalizeProject(project Project) Project {
	project.ID = strings.TrimSpace(project.ID)
	project.UserID = strings.TrimSpace(project.UserID)
	project.Name = truncateRunes(strings.TrimSpace(project.Name), 120)
	if project.Name == "" {
		project.Name = "Untitled project"
	}
	project.Description = truncateRunes(strings.TrimSpace(project.Description), 1000)
	project.Instructions = truncateRunes(strings.TrimSpace(project.Instructions), 8000)
	project.Color = normalizeProjectColor(project.Color)
	return project
}

func applyProjectUpdate(project *Project, update ProjectUpdate) {
	if project == nil {
		return
	}
	if update.Name != nil {
		project.Name = *update.Name
	}
	if update.Description != nil {
		project.Description = *update.Description
	}
	if update.Instructions != nil {
		project.Instructions = *update.Instructions
	}
	if update.Color != nil {
		project.Color = *update.Color
	}
}

func normalizeProjectColor(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultProjectColor
	}
	if len(value) == 7 && strings.HasPrefix(value, "#") {
		for _, r := range value[1:] {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				continue
			}
			return defaultProjectColor
		}
		return value
	}
	return defaultProjectColor
}

func sortProjects(projects []Project) {
	sort.SliceStable(projects, func(i, j int) bool {
		return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
	})
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func isProjectNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, os.ErrNotExist)
}

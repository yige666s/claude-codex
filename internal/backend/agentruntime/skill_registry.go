package agentruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/skills"
)

const (
	SkillStatusDraft       = "draft"
	SkillStatusPublished   = "published"
	SkillStatusUnpublished = "unpublished"
	SkillStatusDisabled    = "disabled"
	SkillStatusArchived    = "archived"
)

type SkillRegistryRecord struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name,omitempty"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Icon        string         `json:"icon,omitempty"`
	Status      string         `json:"status"`
	Version     string         `json:"version,omitempty"`
	Source      string         `json:"source,omitempty"`
	SkillRoot   string         `json:"skill_root,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	ContentHash string         `json:"content_hash,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	PublishedAt *time.Time     `json:"published_at,omitempty"`
}

type SkillVersionRecord struct {
	SkillName   string         `json:"skill_name"`
	Version     string         `json:"version,omitempty"`
	ContentHash string         `json:"content_hash,omitempty"`
	Changelog   string         `json:"changelog,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	PublishedAt *time.Time     `json:"published_at,omitempty"`
}

type SkillRegistryAdminStore interface {
	ListSkills(ctx context.Context) ([]SkillRegistryRecord, error)
	GetSkill(ctx context.Context, name string) (SkillRegistryRecord, error)
	UpdateSkill(ctx context.Context, record SkillRegistryRecord) (SkillRegistryRecord, error)
	SetSkillStatus(ctx context.Context, name string, status string) (SkillRegistryRecord, error)
	ListSkillVersions(ctx context.Context, name string) ([]SkillVersionRecord, error)
	RecordSkillVersion(ctx context.Context, record SkillRegistryRecord, changelog string) error
}

type SQLSkillRegistry struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLSkillRegistry(db *sql.DB) *SQLSkillRegistry {
	return NewSQLSkillRegistryWithDialect(db, SQLDialectQuestion)
}

func NewSQLSkillRegistryWithDialect(db *sql.DB, dialect SQLDialect) *SQLSkillRegistry {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLSkillRegistry{db: db, dialect: dialect}
}

func (s *SQLSkillRegistry) Init(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS agent_skills (
	name TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	icon TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'unpublished',
	version TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	skill_root TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	content_hash TEXT NOT NULL DEFAULT '',
	created_at ` + s.dialect.TimeType() + ` NOT NULL,
	updated_at ` + s.dialect.TimeType() + ` NOT NULL,
	published_at ` + s.dialect.TimeType() + `
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_skills_status ON agent_skills (status, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_skills_category ON agent_skills (category, status)`,
		`CREATE TABLE IF NOT EXISTS agent_skill_versions (
	skill_name TEXT NOT NULL,
	version TEXT NOT NULL DEFAULT '',
	content_hash TEXT NOT NULL DEFAULT '',
	changelog TEXT NOT NULL DEFAULT '',
	metadata TEXT NOT NULL DEFAULT '{}',
	created_at ` + s.dialect.TimeType() + ` NOT NULL,
	published_at ` + s.dialect.TimeType() + `,
	PRIMARY KEY (skill_name, version, content_hash)
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_skill_versions_skill_created ON agent_skill_versions (skill_name, created_at)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_skills", "created_at", "updated_at", "published_at")
}

func (s *SQLSkillRegistry) SyncLoadedSkills(ctx context.Context, loaded []*skills.SkillDefinition) error {
	existing, err := s.ListSkills(ctx)
	if err != nil {
		return err
	}
	byName := make(map[string]SkillRegistryRecord, len(existing))
	for _, record := range existing {
		byName[record.Name] = record
	}
	now := time.Now().UTC()
	for _, skill := range loaded {
		if skill == nil || strings.TrimSpace(skill.Name) == "" {
			continue
		}
		record := skillRegistryRecordFromDefinition(skill, now)
		if previous, ok := byName[record.Name]; ok {
			record.Status = normalizeSkillStatus(previous.Status)
			record.CreatedAt = previous.CreatedAt
			record.PublishedAt = previous.PublishedAt
			if record.Status == SkillStatusPublished && record.PublishedAt == nil {
				publishedAt := now
				record.PublishedAt = &publishedAt
			}
		} else if skillCodePublishesByDefault(skill) {
			record.Status = SkillStatusPublished
			publishedAt := now
			record.PublishedAt = &publishedAt
		}
		if err := s.UpsertSkill(ctx, record); err != nil {
			return err
		}
		if err := s.RecordSkillVersion(ctx, record, "Loaded skill definition"); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLSkillRegistry) ListSkills(ctx context.Context) ([]SkillRegistryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, display_name, description, category, icon, status, version, source, skill_root, metadata, content_hash, created_at, updated_at, published_at FROM agent_skills ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []SkillRegistryRecord
	for rows.Next() {
		record, err := scanSkillRegistryRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *SQLSkillRegistry) GetSkill(ctx context.Context, name string) (SkillRegistryRecord, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT name, display_name, description, category, icon, status, version, source, skill_root, metadata, content_hash, created_at, updated_at, published_at FROM agent_skills WHERE name = ?`), strings.TrimSpace(name))
	return scanSkillRegistryRecord(row)
}

func (s *SQLSkillRegistry) UpsertSkill(ctx context.Context, record SkillRegistryRecord) error {
	record = normalizeSkillRegistryRecord(record)
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_skills (name, display_name, description, category, icon, status, version, source, skill_root, metadata, content_hash, created_at, updated_at, published_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	display_name = excluded.display_name,
	description = excluded.description,
	category = excluded.category,
	icon = excluded.icon,
	version = excluded.version,
	source = excluded.source,
	skill_root = excluded.skill_root,
	metadata = excluded.metadata,
	content_hash = excluded.content_hash,
	updated_at = excluded.updated_at,
	published_at = CASE
		WHEN agent_skills.status = 'published' AND agent_skills.published_at IS NULL THEN excluded.published_at
		ELSE agent_skills.published_at
	END`),
		record.Name,
		record.DisplayName,
		record.Description,
		record.Category,
		record.Icon,
		record.Status,
		record.Version,
		record.Source,
		record.SkillRoot,
		string(metadata),
		record.ContentHash,
		sqlTimeValue(record.CreatedAt, s.dialect),
		sqlTimeValue(record.UpdatedAt, s.dialect),
		nullableSQLTimeValue(record.PublishedAt, s.dialect))
	return err
}

func (s *SQLSkillRegistry) UpdateSkill(ctx context.Context, record SkillRegistryRecord) (SkillRegistryRecord, error) {
	record = normalizeSkillRegistryRecord(record)
	record.UpdatedAt = time.Now().UTC()
	if record.Status == SkillStatusPublished && record.PublishedAt == nil {
		publishedAt := record.UpdatedAt
		record.PublishedAt = &publishedAt
	}
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return SkillRegistryRecord{}, err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_skills
SET display_name = ?,
	description = ?,
	category = ?,
	icon = ?,
	status = ?,
	version = ?,
	source = ?,
	skill_root = ?,
	metadata = ?,
	content_hash = ?,
	updated_at = ?,
	published_at = ?
WHERE name = ?`),
		record.DisplayName,
		record.Description,
		record.Category,
		record.Icon,
		record.Status,
		record.Version,
		record.Source,
		record.SkillRoot,
		string(metadata),
		record.ContentHash,
		sqlTimeValue(record.UpdatedAt, s.dialect),
		nullableSQLTimeValue(record.PublishedAt, s.dialect),
		record.Name)
	if err != nil {
		return SkillRegistryRecord{}, err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return SkillRegistryRecord{}, sql.ErrNoRows
	}
	return s.GetSkill(ctx, record.Name)
}

func (s *SQLSkillRegistry) SetSkillStatus(ctx context.Context, name string, status string) (SkillRegistryRecord, error) {
	record, err := s.GetSkill(ctx, name)
	if err != nil {
		return SkillRegistryRecord{}, err
	}
	record.Status = normalizeSkillStatus(status)
	return s.UpdateSkill(ctx, record)
}

func (s *SQLSkillRegistry) ListSkillVersions(ctx context.Context, name string) ([]SkillVersionRecord, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT skill_name, version, content_hash, changelog, metadata, created_at, published_at FROM agent_skill_versions WHERE skill_name = ? ORDER BY created_at DESC`), strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []SkillVersionRecord
	for rows.Next() {
		record, err := scanSkillVersionRecord(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, record)
	}
	return versions, rows.Err()
}

func (s *SQLSkillRegistry) RecordSkillVersion(ctx context.Context, record SkillRegistryRecord, changelog string) error {
	version := normalizeSkillVersionRecord(record, changelog)
	metadata, err := json.Marshal(version.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_skill_versions (skill_name, version, content_hash, changelog, metadata, created_at, published_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(skill_name, version, content_hash) DO UPDATE SET
	changelog = CASE
		WHEN excluded.changelog <> '' THEN excluded.changelog
		ELSE agent_skill_versions.changelog
	END,
	metadata = excluded.metadata,
	published_at = CASE
		WHEN agent_skill_versions.published_at IS NULL THEN excluded.published_at
		ELSE agent_skill_versions.published_at
	END`),
		version.SkillName,
		version.Version,
		version.ContentHash,
		version.Changelog,
		string(metadata),
		sqlTimeValue(version.CreatedAt, s.dialect),
		nullableSQLTimeValue(version.PublishedAt, s.dialect))
	return err
}

type skillRegistryScanner interface {
	Scan(dest ...any) error
}

func scanSkillVersionRecord(row skillRegistryScanner) (SkillVersionRecord, error) {
	var record SkillVersionRecord
	var metadata string
	var createdAt, publishedAt any
	if err := row.Scan(&record.SkillName, &record.Version, &record.ContentHash, &record.Changelog, &metadata, &createdAt, &publishedAt); err != nil {
		return SkillVersionRecord{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &record.Metadata)
	var err error
	if record.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return SkillVersionRecord{}, err
	}
	if record.PublishedAt, err = parseNullableSQLTime(publishedAt); err != nil {
		return SkillVersionRecord{}, err
	}
	return record, nil
}

func scanSkillRegistryRecord(row skillRegistryScanner) (SkillRegistryRecord, error) {
	var record SkillRegistryRecord
	var metadata string
	var createdAt, updatedAt, publishedAt any
	if err := row.Scan(&record.Name, &record.DisplayName, &record.Description, &record.Category, &record.Icon, &record.Status, &record.Version, &record.Source, &record.SkillRoot, &metadata, &record.ContentHash, &createdAt, &updatedAt, &publishedAt); err != nil {
		return SkillRegistryRecord{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &record.Metadata)
	var err error
	if record.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return SkillRegistryRecord{}, err
	}
	if record.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return SkillRegistryRecord{}, err
	}
	if record.PublishedAt, err = parseNullableSQLTime(publishedAt); err != nil {
		return SkillRegistryRecord{}, err
	}
	return normalizeSkillRegistryRecord(record), nil
}

func skillRegistryRecordFromDefinition(skill *skills.SkillDefinition, now time.Time) SkillRegistryRecord {
	metadata := copySkillMetadata(skill.Metadata)
	record := SkillRegistryRecord{
		Name:        strings.TrimSpace(skill.Name),
		DisplayName: strings.TrimSpace(skill.DisplayName),
		Description: strings.TrimSpace(skill.Description),
		Status:      SkillStatusUnpublished,
		Version:     firstNonEmptyString(strings.TrimSpace(skill.Version), skillMetadataString(metadata, "version")),
		Category:    skillMetadataString(metadata, "category"),
		Icon:        skillMetadataString(metadata, "icon"),
		Source:      string(skill.Source),
		SkillRoot:   strings.TrimSpace(skill.SkillRoot),
		CreatedAt:   now,
		UpdatedAt:   now,
		Metadata:    metadata,
	}
	record.Metadata["aliases"] = skill.Aliases
	record.Metadata["argument_hint"] = skill.ArgumentHint
	record.Metadata["allowed_tools"] = skill.AllowedTools
	record.Metadata["allowed_env"] = skill.AllowedEnv
	record.Metadata["run_as_job"] = skill.RunAsJob
	record.Metadata["execution_context"] = string(skill.ExecutionContext)
	record.Metadata["user_invocable"] = skill.UserInvocable
	record.Metadata["hidden"] = skill.IsHidden
	record.Metadata["loaded_from"] = skill.LoadedFrom
	record.Metadata["content_length"] = skill.ContentLength
	record.ContentHash = skillDefinitionHash(skill)
	return normalizeSkillRegistryRecord(record)
}

func copySkillMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(metadata)+10)
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func skillMetadataString(metadata map[string]any, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if value := skillMetadataValueString(metadata[key]); value != "" {
		return value
	}
	for _, section := range []string{"agentapi", "product", "marketplace", "registry"} {
		nested, ok := metadata[section].(map[string]any)
		if !ok {
			continue
		}
		if value := skillMetadataValueString(nested[key]); value != "" {
			return value
		}
	}
	return ""
}

func skillMetadataValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func normalizeSkillRegistryRecord(record SkillRegistryRecord) SkillRegistryRecord {
	record.Name = strings.TrimSpace(record.Name)
	record.DisplayName = strings.TrimSpace(record.DisplayName)
	record.Description = strings.TrimSpace(record.Description)
	record.Category = strings.TrimSpace(record.Category)
	record.Icon = strings.TrimSpace(record.Icon)
	record.Status = normalizeSkillStatus(record.Status)
	record.Version = strings.TrimSpace(record.Version)
	record.Source = strings.TrimSpace(record.Source)
	record.SkillRoot = strings.TrimSpace(record.SkillRoot)
	record.ContentHash = strings.TrimSpace(record.ContentHash)
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	return record
}

func normalizeSkillVersionRecord(record SkillRegistryRecord, changelog string) SkillVersionRecord {
	record = normalizeSkillRegistryRecord(record)
	now := time.Now().UTC()
	return SkillVersionRecord{
		SkillName:   record.Name,
		Version:     record.Version,
		ContentHash: record.ContentHash,
		Changelog:   strings.TrimSpace(changelog),
		Metadata: map[string]any{
			"display_name": record.DisplayName,
			"category":     record.Category,
			"icon":         record.Icon,
			"status":       record.Status,
			"source":       record.Source,
			"skill_root":   record.SkillRoot,
		},
		CreatedAt:   now,
		PublishedAt: record.PublishedAt,
	}
}

func normalizeSkillStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case SkillStatusDraft:
		return SkillStatusDraft
	case SkillStatusPublished:
		return SkillStatusPublished
	case SkillStatusUnpublished:
		return SkillStatusUnpublished
	case SkillStatusDisabled:
		return SkillStatusDisabled
	case SkillStatusArchived:
		return SkillStatusArchived
	default:
		return SkillStatusUnpublished
	}
}

func skillCodePublishesByDefault(skill *skills.SkillDefinition) bool {
	if skill == nil || !skill.UserInvocable || skill.IsHidden {
		return false
	}
	return true
}

func skillDefinitionHash(skill *skills.SkillDefinition) string {
	if skill == nil {
		return ""
	}
	payload := strings.Join([]string{
		skill.Name,
		skill.DisplayName,
		skill.Description,
		skill.ArgumentHint,
		skill.Version,
		skillMetadataHash(skill.Metadata),
		skill.SkillRoot,
		string(skill.Source),
		strings.Join(skill.Aliases, "\x00"),
		strings.Join(skill.AllowedTools, "\x00"),
		strings.Join(skill.AllowedEnv, "\x00"),
	}, "\x1f")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func skillMetadataHash(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(payload)
}

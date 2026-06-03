package agentruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	PromptStatusDraft         = "draft"
	PromptStatusReviewPending = "review_pending"
	PromptStatusPublished     = "published"
	PromptStatusArchived      = "archived"

	PromptExperimentStatusDraft     = "draft"
	PromptExperimentStatusRunning   = "running"
	PromptExperimentStatusPaused    = "paused"
	PromptExperimentStatusCompleted = "completed"

	PromptTrafficScopeUser    = "user"
	PromptTrafficScopeSession = "session"
	PromptTrafficScopeTenant  = "tenant"

	PromptIDLiveSetup     = "live_setup"
	PromptIDEvalJudge     = "eval_judge"
	PromptIDMemoryExtract = "memory_extract"
)

type promptMetadataContextKey struct{}

type PromptMetadata struct {
	PromptID      string `json:"prompt_id,omitempty"`
	PromptVersion string `json:"prompt_version,omitempty"`
	PromptHash    string `json:"prompt_hash,omitempty"`
	ExperimentID  string `json:"experiment_id,omitempty"`
	VariantID     string `json:"variant_id,omitempty"`
}

func WithPromptMetadata(ctx context.Context, metadata PromptMetadata) context.Context {
	metadata.PromptID = strings.TrimSpace(metadata.PromptID)
	metadata.PromptVersion = strings.TrimSpace(metadata.PromptVersion)
	metadata.PromptHash = strings.TrimSpace(metadata.PromptHash)
	metadata.ExperimentID = strings.TrimSpace(metadata.ExperimentID)
	metadata.VariantID = strings.TrimSpace(metadata.VariantID)
	if metadata.PromptID == "" && metadata.PromptVersion == "" && metadata.PromptHash == "" && metadata.ExperimentID == "" && metadata.VariantID == "" {
		return ctx
	}
	return context.WithValue(ctx, promptMetadataContextKey{}, metadata)
}

func promptMetadataFromContext(ctx context.Context) PromptMetadata {
	metadata, _ := ctx.Value(promptMetadataContextKey{}).(PromptMetadata)
	return metadata
}

type PromptTemplate struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Scope       string         `json:"scope,omitempty"`
	Owner       string         `json:"owner,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at,omitempty"`
}

type PromptVersion struct {
	PromptID        string         `json:"prompt_id"`
	Version         string         `json:"version"`
	Status          string         `json:"status"`
	Content         string         `json:"content"`
	VariablesSchema map[string]any `json:"variables_schema,omitempty"`
	RenderConfig    map[string]any `json:"render_config,omitempty"`
	ContentHash     string         `json:"content_hash"`
	BaseVersion     string         `json:"base_version,omitempty"`
	Changelog       string         `json:"changelog,omitempty"`
	CreatedBy       string         `json:"created_by,omitempty"`
	ReviewedBy      string         `json:"reviewed_by,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	PublishedAt     *time.Time     `json:"published_at,omitempty"`
}

type PromptExperiment struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	PromptID        string         `json:"prompt_id"`
	Status          string         `json:"status"`
	TrafficScope    string         `json:"traffic_scope"`
	Allocation      map[string]any `json:"allocation,omitempty"`
	Guardrails      map[string]any `json:"guardrails,omitempty"`
	WinnerVariantID string         `json:"winner_variant_id,omitempty"`
	CreatedBy       string         `json:"created_by,omitempty"`
	UpdatedBy       string         `json:"updated_by,omitempty"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	EndedAt         *time.Time     `json:"ended_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
}

type PromptExperimentVariant struct {
	ExperimentID  string         `json:"experiment_id"`
	VariantID     string         `json:"variant_id"`
	PromptVersion string         `json:"prompt_version"`
	Weight        int            `json:"weight"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty"`
}

type PromptListFilter struct {
	Scope  string
	Status string
	Query  string
	Limit  int
}

type PromptExperimentFilter struct {
	PromptID string
	Status   string
	Query    string
	Limit    int
}

type PromptStore interface {
	Init(ctx context.Context) error
	UpsertPrompt(ctx context.Context, prompt PromptTemplate) (PromptTemplate, error)
	GetPrompt(ctx context.Context, id string) (PromptTemplate, error)
	ListPrompts(ctx context.Context, filter PromptListFilter) ([]PromptTemplate, error)
	CreatePromptVersion(ctx context.Context, version PromptVersion) (PromptVersion, error)
	GetPromptVersion(ctx context.Context, promptID, version string) (PromptVersion, error)
	GetPublishedPromptVersion(ctx context.Context, promptID string) (PromptVersion, error)
	ListPromptVersions(ctx context.Context, promptID string) ([]PromptVersion, error)
	PublishPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error)
	RollbackPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error)
	UpsertPromptExperiment(ctx context.Context, experiment PromptExperiment, variants []PromptExperimentVariant) (PromptExperiment, error)
	GetPromptExperiment(ctx context.Context, id string) (PromptExperiment, []PromptExperimentVariant, error)
	ListPromptExperiments(ctx context.Context, filter PromptExperimentFilter) ([]PromptExperiment, error)
	UpdatePromptExperimentStatus(ctx context.Context, id, status, winnerVariantID, actor string) (PromptExperiment, error)
}

type MemoryPromptStore struct {
	mu          sync.Mutex
	prompts     map[string]PromptTemplate
	versions    map[string]PromptVersion
	experiments map[string]PromptExperiment
	variants    map[string][]PromptExperimentVariant
}

func NewMemoryPromptStore() *MemoryPromptStore {
	return &MemoryPromptStore{
		prompts:     make(map[string]PromptTemplate),
		versions:    make(map[string]PromptVersion),
		experiments: make(map[string]PromptExperiment),
		variants:    make(map[string][]PromptExperimentVariant),
	}
}

func (s *MemoryPromptStore) Init(context.Context) error { return nil }

func (s *MemoryPromptStore) UpsertPrompt(_ context.Context, prompt PromptTemplate) (PromptTemplate, error) {
	prompt = normalizePromptTemplate(prompt)
	if prompt.ID == "" {
		return PromptTemplate{}, fmt.Errorf("prompt id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.prompts[prompt.ID]; ok {
		if prompt.CreatedAt.IsZero() {
			prompt.CreatedAt = existing.CreatedAt
		}
	}
	prompt = normalizePromptTemplate(prompt)
	s.prompts[prompt.ID] = clonePromptTemplate(prompt)
	return clonePromptTemplate(prompt), nil
}

func (s *MemoryPromptStore) GetPrompt(_ context.Context, id string) (PromptTemplate, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	prompt, ok := s.prompts[id]
	if !ok {
		return PromptTemplate{}, sql.ErrNoRows
	}
	return clonePromptTemplate(prompt), nil
}

func (s *MemoryPromptStore) ListPrompts(_ context.Context, filter PromptListFilter) ([]PromptTemplate, error) {
	filter = normalizePromptListFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PromptTemplate, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		if !promptMatchesFilter(prompt, filter) {
			continue
		}
		if filter.Status != "" {
			if _, ok := s.publishedVersionLocked(prompt.ID); filter.Status == PromptStatusPublished && !ok {
				continue
			}
		}
		out = append(out, clonePromptTemplate(prompt))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryPromptStore) CreatePromptVersion(_ context.Context, version PromptVersion) (PromptVersion, error) {
	version = normalizePromptVersion(version)
	if version.PromptID == "" {
		return PromptVersion{}, fmt.Errorf("prompt id is required")
	}
	if version.Version == "" {
		return PromptVersion{}, fmt.Errorf("prompt version is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.prompts[version.PromptID]; !ok {
		s.prompts[version.PromptID] = normalizePromptTemplate(PromptTemplate{ID: version.PromptID, Name: version.PromptID})
	}
	key := promptVersionKey(version.PromptID, version.Version)
	if _, ok := s.versions[key]; ok {
		return PromptVersion{}, fmt.Errorf("prompt version already exists")
	}
	if version.Status == PromptStatusPublished {
		s.archivePublishedLocked(version.PromptID, timePtrValue(version.PublishedAt, version.CreatedAt))
	}
	s.versions[key] = clonePromptVersion(version)
	prompt := s.prompts[version.PromptID]
	prompt.UpdatedAt = time.Now().UTC()
	s.prompts[version.PromptID] = normalizePromptTemplate(prompt)
	return clonePromptVersion(version), nil
}

func (s *MemoryPromptStore) GetPromptVersion(_ context.Context, promptID, version string) (PromptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.versions[promptVersionKey(promptID, version)]
	if !ok {
		return PromptVersion{}, sql.ErrNoRows
	}
	return clonePromptVersion(item), nil
}

func (s *MemoryPromptStore) GetPublishedPromptVersion(_ context.Context, promptID string) (PromptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.publishedVersionLocked(promptID)
	if !ok {
		return PromptVersion{}, sql.ErrNoRows
	}
	return clonePromptVersion(item), nil
}

func (s *MemoryPromptStore) ListPromptVersions(_ context.Context, promptID string) ([]PromptVersion, error) {
	promptID = strings.TrimSpace(promptID)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PromptVersion, 0)
	for _, version := range s.versions {
		if version.PromptID == promptID {
			out = append(out, clonePromptVersion(version))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

func (s *MemoryPromptStore) PublishPromptVersion(_ context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.versions[promptVersionKey(promptID, version)]
	if !ok {
		return PromptVersion{}, sql.ErrNoRows
	}
	now := time.Now().UTC()
	s.archivePublishedLocked(item.PromptID, now)
	item.Status = PromptStatusPublished
	item.ReviewedBy = strings.TrimSpace(actor)
	item.Changelog = firstNonEmptyString(strings.TrimSpace(changelog), item.Changelog)
	item.PublishedAt = &now
	s.versions[promptVersionKey(item.PromptID, item.Version)] = normalizePromptVersion(item)
	return clonePromptVersion(item), nil
}

func (s *MemoryPromptStore) RollbackPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	return s.PublishPromptVersion(ctx, promptID, version, actor, firstNonEmptyString(changelog, "rollback to "+strings.TrimSpace(version)))
}

func (s *MemoryPromptStore) UpsertPromptExperiment(_ context.Context, experiment PromptExperiment, variants []PromptExperimentVariant) (PromptExperiment, error) {
	experiment = normalizePromptExperiment(experiment)
	if experiment.PromptID == "" {
		return PromptExperiment{}, fmt.Errorf("prompt id is required")
	}
	if len(variants) == 0 {
		return PromptExperiment{}, fmt.Errorf("at least one prompt experiment variant is required")
	}
	normalizedVariants := make([]PromptExperimentVariant, 0, len(variants))
	for _, variant := range variants {
		variant.ExperimentID = firstNonEmptyString(variant.ExperimentID, experiment.ID)
		variant = normalizePromptExperimentVariant(variant)
		if variant.ExperimentID != experiment.ID {
			return PromptExperiment{}, fmt.Errorf("variant experiment id mismatch")
		}
		if variant.PromptVersion == "" {
			return PromptExperiment{}, fmt.Errorf("variant prompt version is required")
		}
		normalizedVariants = append(normalizedVariants, variant)
	}
	if err := validatePromptExperimentWeights(normalizedVariants); err != nil {
		return PromptExperiment{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.prompts[experiment.PromptID]; !ok {
		s.prompts[experiment.PromptID] = normalizePromptTemplate(PromptTemplate{ID: experiment.PromptID, Name: experiment.PromptID})
	}
	if existing, ok := s.experiments[experiment.ID]; ok {
		if experiment.CreatedAt.IsZero() {
			experiment.CreatedAt = existing.CreatedAt
		}
		if experiment.CreatedBy == "" {
			experiment.CreatedBy = existing.CreatedBy
		}
	}
	experiment = normalizePromptExperiment(experiment)
	s.experiments[experiment.ID] = clonePromptExperiment(experiment)
	s.variants[experiment.ID] = clonePromptExperimentVariants(normalizedVariants)
	return clonePromptExperiment(experiment), nil
}

func (s *MemoryPromptStore) GetPromptExperiment(_ context.Context, id string) (PromptExperiment, []PromptExperimentVariant, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	experiment, ok := s.experiments[id]
	if !ok {
		return PromptExperiment{}, nil, sql.ErrNoRows
	}
	return clonePromptExperiment(experiment), clonePromptExperimentVariants(s.variants[id]), nil
}

func (s *MemoryPromptStore) ListPromptExperiments(_ context.Context, filter PromptExperimentFilter) ([]PromptExperiment, error) {
	filter = normalizePromptExperimentFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PromptExperiment, 0, len(s.experiments))
	for _, experiment := range s.experiments {
		if !promptExperimentMatchesFilter(experiment, filter) {
			continue
		}
		out = append(out, clonePromptExperiment(experiment))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryPromptStore) UpdatePromptExperimentStatus(_ context.Context, id, status, winnerVariantID, actor string) (PromptExperiment, error) {
	id = strings.TrimSpace(id)
	status = normalizePromptExperimentStatus(status)
	s.mu.Lock()
	defer s.mu.Unlock()
	experiment, ok := s.experiments[id]
	if !ok {
		return PromptExperiment{}, sql.ErrNoRows
	}
	now := time.Now().UTC()
	experiment.Status = status
	experiment.UpdatedBy = strings.TrimSpace(actor)
	experiment.UpdatedAt = now
	if status == PromptExperimentStatusRunning && experiment.StartedAt == nil {
		experiment.StartedAt = &now
	}
	if status == PromptExperimentStatusCompleted {
		experiment.WinnerVariantID = strings.TrimSpace(winnerVariantID)
		experiment.EndedAt = &now
	}
	s.experiments[id] = normalizePromptExperiment(experiment)
	return clonePromptExperiment(s.experiments[id]), nil
}

func (s *MemoryPromptStore) publishedVersionLocked(promptID string) (PromptVersion, bool) {
	promptID = strings.TrimSpace(promptID)
	var latest PromptVersion
	found := false
	for _, version := range s.versions {
		if version.PromptID != promptID || version.Status != PromptStatusPublished {
			continue
		}
		if !found || version.CreatedAt.After(latest.CreatedAt) {
			latest = version
			found = true
		}
	}
	return latest, found
}

func (s *MemoryPromptStore) archivePublishedLocked(promptID string, now time.Time) {
	for key, version := range s.versions {
		if version.PromptID == strings.TrimSpace(promptID) && version.Status == PromptStatusPublished {
			version.Status = PromptStatusArchived
			s.versions[key] = normalizePromptVersion(version)
		}
	}
}

type SQLPromptStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLPromptStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLPromptStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLPromptStore{db: db, dialect: dialect}
}

func (s *SQLPromptStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql prompt store is not configured")
	}
	if err := requireSQLColumns(ctx, s.db, "agent_prompt_templates",
		"id", "name", "description", "scope", "owner", "metadata", "created_at", "updated_at",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_prompt_versions",
		"prompt_id", "version", "status", "content", "variables_schema", "render_config",
		"content_hash", "base_version", "changelog", "created_by", "reviewed_by", "created_at", "published_at",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_prompt_experiments",
		"id", "name", "prompt_id", "status", "traffic_scope", "allocation", "guardrails",
		"winner_variant_id", "created_by", "updated_by", "started_at", "ended_at", "created_at", "updated_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_prompt_experiment_variants",
		"experiment_id", "variant_id", "prompt_version", "weight", "metadata", "created_at",
	)
}

func (s *SQLPromptStore) UpsertPrompt(ctx context.Context, prompt PromptTemplate) (PromptTemplate, error) {
	prompt = normalizePromptTemplate(prompt)
	metadata, err := json.Marshal(prompt.Metadata)
	if err != nil {
		return PromptTemplate{}, err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_prompt_templates (id, name, description, scope, owner, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	name = excluded.name,
	description = excluded.description,
	scope = excluded.scope,
	owner = excluded.owner,
	metadata = excluded.metadata,
	updated_at = excluded.updated_at`),
		prompt.ID, prompt.Name, prompt.Description, prompt.Scope, prompt.Owner, string(metadata),
		sqlTimeValue(prompt.CreatedAt, s.dialect), sqlTimeValue(prompt.UpdatedAt, s.dialect))
	if err != nil {
		return PromptTemplate{}, err
	}
	return s.GetPrompt(ctx, prompt.ID)
}

func (s *SQLPromptStore) GetPrompt(ctx context.Context, id string) (PromptTemplate, error) {
	return scanPromptTemplate(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, name, description, scope, owner, metadata, created_at, updated_at
FROM agent_prompt_templates WHERE id = ?`), strings.TrimSpace(id)))
}

func (s *SQLPromptStore) ListPrompts(ctx context.Context, filter PromptListFilter) ([]PromptTemplate, error) {
	filter = normalizePromptListFilter(filter)
	query := `SELECT id, name, description, scope, owner, metadata, created_at, updated_at FROM agent_prompt_templates`
	var where []string
	var args []any
	if filter.Scope != "" {
		where = append(where, "scope = ?")
		args = append(args, filter.Scope)
	}
	if filter.Query != "" {
		where = append(where, "(LOWER(id) LIKE ? OR LOWER(name) LIKE ? OR LOWER(description) LIKE ?)")
		pattern := "%" + strings.ToLower(filter.Query) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if filter.Status == PromptStatusPublished {
		where = append(where, "EXISTS (SELECT 1 FROM agent_prompt_versions v WHERE v.prompt_id = agent_prompt_templates.id AND v.status = ?)")
		args = append(args, PromptStatusPublished)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptTemplate{}
	for rows.Next() {
		item, err := scanPromptTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLPromptStore) CreatePromptVersion(ctx context.Context, version PromptVersion) (PromptVersion, error) {
	version = normalizePromptVersion(version)
	if _, err := s.GetPrompt(ctx, version.PromptID); err != nil {
		if !errorsIsSQLNoRows(err) {
			return PromptVersion{}, err
		}
		if _, err := s.UpsertPrompt(ctx, PromptTemplate{ID: version.PromptID, Name: version.PromptID}); err != nil {
			return PromptVersion{}, err
		}
	}
	vars, config, err := marshalPromptVersionJSON(version)
	if err != nil {
		return PromptVersion{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PromptVersion{}, err
	}
	defer tx.Rollback()
	if version.Status == PromptStatusPublished {
		if _, err := tx.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_prompt_versions SET status = ? WHERE prompt_id = ? AND status = ?`), PromptStatusArchived, version.PromptID, PromptStatusPublished); err != nil {
			return PromptVersion{}, err
		}
	}
	_, err = tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_prompt_versions (prompt_id, version, status, content, variables_schema, render_config, content_hash, base_version, changelog, created_by, reviewed_by, created_at, published_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		version.PromptID, version.Version, version.Status, version.Content, string(vars), string(config), version.ContentHash,
		version.BaseVersion, version.Changelog, version.CreatedBy, version.ReviewedBy, sqlTimeValue(version.CreatedAt, s.dialect), nullableSQLTimeValue(version.PublishedAt, s.dialect))
	if err != nil {
		return PromptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_prompt_templates SET updated_at = ? WHERE id = ?`), sqlTimeValue(time.Now().UTC(), s.dialect), version.PromptID); err != nil {
		return PromptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return PromptVersion{}, err
	}
	return s.GetPromptVersion(ctx, version.PromptID, version.Version)
}

func (s *SQLPromptStore) GetPromptVersion(ctx context.Context, promptID, version string) (PromptVersion, error) {
	return scanPromptVersion(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT prompt_id, version, status, content, variables_schema, render_config, content_hash, base_version, changelog, created_by, reviewed_by, created_at, published_at
FROM agent_prompt_versions WHERE prompt_id = ? AND version = ?`), strings.TrimSpace(promptID), strings.TrimSpace(version)))
}

func (s *SQLPromptStore) GetPublishedPromptVersion(ctx context.Context, promptID string) (PromptVersion, error) {
	return scanPromptVersion(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT prompt_id, version, status, content, variables_schema, render_config, content_hash, base_version, changelog, created_by, reviewed_by, created_at, published_at
FROM agent_prompt_versions WHERE prompt_id = ? AND status = ?
ORDER BY published_at DESC, created_at DESC LIMIT 1`), strings.TrimSpace(promptID), PromptStatusPublished))
}

func (s *SQLPromptStore) ListPromptVersions(ctx context.Context, promptID string) ([]PromptVersion, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT prompt_id, version, status, content, variables_schema, render_config, content_hash, base_version, changelog, created_by, reviewed_by, created_at, published_at
FROM agent_prompt_versions WHERE prompt_id = ? ORDER BY created_at DESC`), strings.TrimSpace(promptID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptVersion{}
	for rows.Next() {
		version, err := scanPromptVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

func (s *SQLPromptStore) PublishPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	return s.setPromptVersionPublished(ctx, promptID, version, actor, changelog)
}

func (s *SQLPromptStore) RollbackPromptVersion(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	return s.setPromptVersionPublished(ctx, promptID, version, actor, firstNonEmptyString(changelog, "rollback to "+strings.TrimSpace(version)))
}

func (s *SQLPromptStore) UpsertPromptExperiment(ctx context.Context, experiment PromptExperiment, variants []PromptExperimentVariant) (PromptExperiment, error) {
	experiment = normalizePromptExperiment(experiment)
	if len(variants) == 0 {
		return PromptExperiment{}, fmt.Errorf("at least one prompt experiment variant is required")
	}
	normalizedVariants := make([]PromptExperimentVariant, 0, len(variants))
	for _, variant := range variants {
		variant.ExperimentID = firstNonEmptyString(variant.ExperimentID, experiment.ID)
		variant = normalizePromptExperimentVariant(variant)
		normalizedVariants = append(normalizedVariants, variant)
	}
	if err := validatePromptExperimentWeights(normalizedVariants); err != nil {
		return PromptExperiment{}, err
	}
	allocation, guardrails, err := marshalPromptExperimentJSON(experiment)
	if err != nil {
		return PromptExperiment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PromptExperiment{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_prompt_experiments (id, name, prompt_id, status, traffic_scope, allocation, guardrails, winner_variant_id, created_by, updated_by, started_at, ended_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	name = excluded.name,
	prompt_id = excluded.prompt_id,
	status = excluded.status,
	traffic_scope = excluded.traffic_scope,
	allocation = excluded.allocation,
	guardrails = excluded.guardrails,
	winner_variant_id = excluded.winner_variant_id,
	updated_by = excluded.updated_by,
	started_at = excluded.started_at,
	ended_at = excluded.ended_at,
	updated_at = excluded.updated_at`),
		experiment.ID, experiment.Name, experiment.PromptID, experiment.Status, experiment.TrafficScope, string(allocation), string(guardrails),
		experiment.WinnerVariantID, experiment.CreatedBy, experiment.UpdatedBy, nullableSQLTimeValue(experiment.StartedAt, s.dialect), nullableSQLTimeValue(experiment.EndedAt, s.dialect),
		sqlTimeValue(experiment.CreatedAt, s.dialect), sqlTimeValue(experiment.UpdatedAt, s.dialect)); err != nil {
		return PromptExperiment{}, err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_prompt_experiment_variants WHERE experiment_id = ?`), experiment.ID); err != nil {
		return PromptExperiment{}, err
	}
	for _, variant := range normalizedVariants {
		metadata, err := json.Marshal(variant.Metadata)
		if err != nil {
			return PromptExperiment{}, err
		}
		if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_prompt_experiment_variants (experiment_id, variant_id, prompt_version, weight, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?)`),
			variant.ExperimentID, variant.VariantID, variant.PromptVersion, variant.Weight, string(metadata), sqlTimeValue(variant.CreatedAt, s.dialect)); err != nil {
			return PromptExperiment{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PromptExperiment{}, err
	}
	created, _, err := s.GetPromptExperiment(ctx, experiment.ID)
	return created, err
}

func (s *SQLPromptStore) GetPromptExperiment(ctx context.Context, id string) (PromptExperiment, []PromptExperimentVariant, error) {
	experiment, err := scanPromptExperiment(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, name, prompt_id, status, traffic_scope, allocation, guardrails, winner_variant_id, created_by, updated_by, started_at, ended_at, created_at, updated_at
FROM agent_prompt_experiments WHERE id = ?`), strings.TrimSpace(id)))
	if err != nil {
		return PromptExperiment{}, nil, err
	}
	variants, err := s.listPromptExperimentVariants(ctx, experiment.ID)
	if err != nil {
		return PromptExperiment{}, nil, err
	}
	return experiment, variants, nil
}

func (s *SQLPromptStore) ListPromptExperiments(ctx context.Context, filter PromptExperimentFilter) ([]PromptExperiment, error) {
	filter = normalizePromptExperimentFilter(filter)
	query := `SELECT id, name, prompt_id, status, traffic_scope, allocation, guardrails, winner_variant_id, created_by, updated_by, started_at, ended_at, created_at, updated_at FROM agent_prompt_experiments`
	var where []string
	var args []any
	if filter.PromptID != "" {
		where = append(where, "prompt_id = ?")
		args = append(args, filter.PromptID)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		where = append(where, "(LOWER(id) LIKE ? OR LOWER(name) LIKE ? OR LOWER(prompt_id) LIKE ?)")
		pattern := "%" + strings.ToLower(filter.Query) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptExperiment{}
	for rows.Next() {
		experiment, err := scanPromptExperiment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, experiment)
	}
	return out, rows.Err()
}

func (s *SQLPromptStore) UpdatePromptExperimentStatus(ctx context.Context, id, status, winnerVariantID, actor string) (PromptExperiment, error) {
	id = strings.TrimSpace(id)
	status = normalizePromptExperimentStatus(status)
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_prompt_experiments
SET status = ?,
	winner_variant_id = CASE WHEN ? = '' THEN winner_variant_id ELSE ? END,
	updated_by = ?,
	started_at = CASE WHEN ? = 'running' AND started_at IS NULL THEN ? ELSE started_at END,
	ended_at = CASE WHEN ? = 'completed' THEN ? ELSE ended_at END,
	updated_at = ?
WHERE id = ?`),
		status, strings.TrimSpace(winnerVariantID), strings.TrimSpace(winnerVariantID), strings.TrimSpace(actor),
		status, sqlTimeValue(now, s.dialect), status, sqlTimeValue(now, s.dialect), sqlTimeValue(now, s.dialect), id)
	if err != nil {
		return PromptExperiment{}, err
	}
	experiment, _, err := s.GetPromptExperiment(ctx, id)
	return experiment, err
}

func (s *SQLPromptStore) listPromptExperimentVariants(ctx context.Context, experimentID string) ([]PromptExperimentVariant, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT experiment_id, variant_id, prompt_version, weight, metadata, created_at
FROM agent_prompt_experiment_variants WHERE experiment_id = ? ORDER BY variant_id`), strings.TrimSpace(experimentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptExperimentVariant{}
	for rows.Next() {
		variant, err := scanPromptExperimentVariant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, variant)
	}
	return out, rows.Err()
}

func (s *SQLPromptStore) setPromptVersionPublished(ctx context.Context, promptID, version, actor, changelog string) (PromptVersion, error) {
	current, err := s.GetPromptVersion(ctx, promptID, version)
	if err != nil {
		return PromptVersion{}, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PromptVersion{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_prompt_versions SET status = ? WHERE prompt_id = ? AND status = ?`), PromptStatusArchived, current.PromptID, PromptStatusPublished); err != nil {
		return PromptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_prompt_versions
SET status = ?, reviewed_by = ?, changelog = CASE WHEN ? = '' THEN changelog ELSE ? END, published_at = ?
WHERE prompt_id = ? AND version = ?`),
		PromptStatusPublished, strings.TrimSpace(actor), strings.TrimSpace(changelog), strings.TrimSpace(changelog), sqlTimeValue(now, s.dialect), current.PromptID, current.Version); err != nil {
		return PromptVersion{}, err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_prompt_templates SET updated_at = ? WHERE id = ?`), sqlTimeValue(now, s.dialect), current.PromptID); err != nil {
		return PromptVersion{}, err
	}
	if err := tx.Commit(); err != nil {
		return PromptVersion{}, err
	}
	return s.GetPromptVersion(ctx, current.PromptID, current.Version)
}

type promptScanner interface {
	Scan(dest ...any) error
}

func scanPromptTemplate(row promptScanner) (PromptTemplate, error) {
	var prompt PromptTemplate
	var metadata string
	var createdAt, updatedAt any
	if err := row.Scan(&prompt.ID, &prompt.Name, &prompt.Description, &prompt.Scope, &prompt.Owner, &metadata, &createdAt, &updatedAt); err != nil {
		return PromptTemplate{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &prompt.Metadata)
	var err error
	if prompt.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return PromptTemplate{}, err
	}
	if prompt.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return PromptTemplate{}, err
	}
	return normalizePromptTemplate(prompt), nil
}

func scanPromptVersion(row promptScanner) (PromptVersion, error) {
	var version PromptVersion
	var variablesSchema, renderConfig string
	var createdAt, publishedAt any
	if err := row.Scan(&version.PromptID, &version.Version, &version.Status, &version.Content, &variablesSchema, &renderConfig, &version.ContentHash, &version.BaseVersion, &version.Changelog, &version.CreatedBy, &version.ReviewedBy, &createdAt, &publishedAt); err != nil {
		return PromptVersion{}, err
	}
	_ = json.Unmarshal([]byte(variablesSchema), &version.VariablesSchema)
	_ = json.Unmarshal([]byte(renderConfig), &version.RenderConfig)
	var err error
	if version.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return PromptVersion{}, err
	}
	if version.PublishedAt, err = parseNullableSQLTime(publishedAt); err != nil {
		return PromptVersion{}, err
	}
	return normalizePromptVersion(version), nil
}

func scanPromptExperiment(row promptScanner) (PromptExperiment, error) {
	var experiment PromptExperiment
	var allocation, guardrails string
	var startedAt, endedAt, createdAt, updatedAt any
	if err := row.Scan(&experiment.ID, &experiment.Name, &experiment.PromptID, &experiment.Status, &experiment.TrafficScope, &allocation, &guardrails, &experiment.WinnerVariantID, &experiment.CreatedBy, &experiment.UpdatedBy, &startedAt, &endedAt, &createdAt, &updatedAt); err != nil {
		return PromptExperiment{}, err
	}
	_ = json.Unmarshal([]byte(allocation), &experiment.Allocation)
	_ = json.Unmarshal([]byte(guardrails), &experiment.Guardrails)
	var err error
	if experiment.StartedAt, err = parseNullableSQLTime(startedAt); err != nil {
		return PromptExperiment{}, err
	}
	if experiment.EndedAt, err = parseNullableSQLTime(endedAt); err != nil {
		return PromptExperiment{}, err
	}
	if experiment.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return PromptExperiment{}, err
	}
	if experiment.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return PromptExperiment{}, err
	}
	return normalizePromptExperiment(experiment), nil
}

func scanPromptExperimentVariant(row promptScanner) (PromptExperimentVariant, error) {
	var variant PromptExperimentVariant
	var metadata string
	var createdAt any
	if err := row.Scan(&variant.ExperimentID, &variant.VariantID, &variant.PromptVersion, &variant.Weight, &metadata, &createdAt); err != nil {
		return PromptExperimentVariant{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &variant.Metadata)
	var err error
	if variant.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return PromptExperimentVariant{}, err
	}
	return normalizePromptExperimentVariant(variant), nil
}

func normalizePromptTemplate(prompt PromptTemplate) PromptTemplate {
	prompt.ID = strings.TrimSpace(prompt.ID)
	prompt.Name = truncateString(strings.TrimSpace(prompt.Name), 256)
	if prompt.Name == "" {
		prompt.Name = prompt.ID
	}
	prompt.Description = truncateString(strings.TrimSpace(prompt.Description), 4096)
	prompt.Scope = truncateString(strings.TrimSpace(prompt.Scope), 64)
	prompt.Owner = truncateString(strings.TrimSpace(prompt.Owner), 256)
	if prompt.Metadata == nil {
		prompt.Metadata = map[string]any{}
	}
	now := time.Now().UTC()
	if prompt.CreatedAt.IsZero() {
		prompt.CreatedAt = now
	} else {
		prompt.CreatedAt = prompt.CreatedAt.UTC()
	}
	if prompt.UpdatedAt.IsZero() {
		prompt.UpdatedAt = prompt.CreatedAt
	} else {
		prompt.UpdatedAt = prompt.UpdatedAt.UTC()
	}
	return prompt
}

func normalizePromptVersion(version PromptVersion) PromptVersion {
	version.PromptID = strings.TrimSpace(version.PromptID)
	version.Version = strings.TrimSpace(version.Version)
	if version.Version == "" {
		version.Version = "v1"
	}
	version.Status = normalizePromptStatus(version.Status)
	version.Content = strings.TrimSpace(version.Content)
	if version.VariablesSchema == nil {
		version.VariablesSchema = map[string]any{}
	}
	if version.RenderConfig == nil {
		version.RenderConfig = map[string]any{}
	}
	version.ContentHash = strings.TrimSpace(version.ContentHash)
	if version.ContentHash == "" {
		version.ContentHash = promptContentHash(version.Content, version.VariablesSchema, version.RenderConfig)
	}
	version.BaseVersion = strings.TrimSpace(version.BaseVersion)
	version.Changelog = truncateString(strings.TrimSpace(version.Changelog), 4096)
	version.CreatedBy = strings.TrimSpace(version.CreatedBy)
	version.ReviewedBy = strings.TrimSpace(version.ReviewedBy)
	if version.CreatedAt.IsZero() {
		version.CreatedAt = time.Now().UTC()
	} else {
		version.CreatedAt = version.CreatedAt.UTC()
	}
	if version.Status == PromptStatusPublished && version.PublishedAt == nil {
		value := version.CreatedAt
		version.PublishedAt = &value
	}
	if version.PublishedAt != nil {
		value := version.PublishedAt.UTC()
		version.PublishedAt = &value
	}
	return version
}

func normalizePromptExperiment(experiment PromptExperiment) PromptExperiment {
	experiment.ID = strings.TrimSpace(experiment.ID)
	if experiment.ID == "" {
		experiment.ID = newPromptExperimentID()
	}
	experiment.Name = truncateString(strings.TrimSpace(experiment.Name), 256)
	if experiment.Name == "" {
		experiment.Name = experiment.ID
	}
	experiment.PromptID = strings.TrimSpace(experiment.PromptID)
	experiment.Status = normalizePromptExperimentStatus(experiment.Status)
	experiment.TrafficScope = normalizePromptTrafficScope(experiment.TrafficScope)
	if experiment.Allocation == nil {
		experiment.Allocation = map[string]any{}
	}
	if experiment.Guardrails == nil {
		experiment.Guardrails = map[string]any{}
	}
	experiment.WinnerVariantID = strings.TrimSpace(experiment.WinnerVariantID)
	experiment.CreatedBy = strings.TrimSpace(experiment.CreatedBy)
	experiment.UpdatedBy = strings.TrimSpace(experiment.UpdatedBy)
	now := time.Now().UTC()
	if experiment.CreatedAt.IsZero() {
		experiment.CreatedAt = now
	} else {
		experiment.CreatedAt = experiment.CreatedAt.UTC()
	}
	if experiment.UpdatedAt.IsZero() {
		experiment.UpdatedAt = experiment.CreatedAt
	} else {
		experiment.UpdatedAt = experiment.UpdatedAt.UTC()
	}
	if experiment.StartedAt != nil {
		value := experiment.StartedAt.UTC()
		experiment.StartedAt = &value
	}
	if experiment.EndedAt != nil {
		value := experiment.EndedAt.UTC()
		experiment.EndedAt = &value
	}
	if experiment.Status == PromptExperimentStatusRunning && experiment.StartedAt == nil {
		value := experiment.UpdatedAt
		experiment.StartedAt = &value
	}
	if experiment.Status == PromptExperimentStatusCompleted && experiment.EndedAt == nil {
		value := experiment.UpdatedAt
		experiment.EndedAt = &value
	}
	return experiment
}

func normalizePromptExperimentVariant(variant PromptExperimentVariant) PromptExperimentVariant {
	variant.ExperimentID = strings.TrimSpace(variant.ExperimentID)
	variant.VariantID = strings.TrimSpace(variant.VariantID)
	if variant.VariantID == "" {
		variant.VariantID = "variant"
	}
	variant.PromptVersion = strings.TrimSpace(variant.PromptVersion)
	if variant.Weight < 0 {
		variant.Weight = 0
	}
	if variant.Metadata == nil {
		variant.Metadata = map[string]any{}
	}
	if variant.CreatedAt.IsZero() {
		variant.CreatedAt = time.Now().UTC()
	} else {
		variant.CreatedAt = variant.CreatedAt.UTC()
	}
	return variant
}

func normalizePromptExperimentStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case PromptExperimentStatusDraft, "":
		return PromptExperimentStatusDraft
	case PromptExperimentStatusRunning:
		return PromptExperimentStatusRunning
	case PromptExperimentStatusPaused:
		return PromptExperimentStatusPaused
	case PromptExperimentStatusCompleted:
		return PromptExperimentStatusCompleted
	default:
		return PromptExperimentStatusDraft
	}
}

func normalizeOptionalPromptExperimentStatus(status string) string {
	if strings.TrimSpace(status) == "" || strings.EqualFold(status, "all") {
		return ""
	}
	return normalizePromptExperimentStatus(status)
}

func normalizePromptTrafficScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case PromptTrafficScopeSession:
		return PromptTrafficScopeSession
	case PromptTrafficScopeTenant:
		return PromptTrafficScopeTenant
	case PromptTrafficScopeUser, "":
		return PromptTrafficScopeUser
	default:
		return PromptTrafficScopeUser
	}
}

func normalizePromptExperimentFilter(filter PromptExperimentFilter) PromptExperimentFilter {
	filter.PromptID = strings.TrimSpace(filter.PromptID)
	filter.Status = normalizeOptionalPromptExperimentStatus(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Limit = normalizeEvaluationLimit(filter.Limit)
	return filter
}

func promptExperimentMatchesFilter(experiment PromptExperiment, filter PromptExperimentFilter) bool {
	if filter.PromptID != "" && experiment.PromptID != filter.PromptID {
		return false
	}
	if filter.Status != "" && experiment.Status != filter.Status {
		return false
	}
	if filter.Query != "" && !containsLowerAny(filter.Query, experiment.ID, experiment.Name, experiment.PromptID) {
		return false
	}
	return true
}

func validatePromptExperimentWeights(variants []PromptExperimentVariant) error {
	seen := map[string]bool{}
	total := 0
	for _, variant := range variants {
		if variant.VariantID == "" {
			return fmt.Errorf("variant id is required")
		}
		if seen[variant.VariantID] {
			return fmt.Errorf("duplicate variant id: %s", variant.VariantID)
		}
		seen[variant.VariantID] = true
		total += variant.Weight
	}
	if total <= 0 {
		return fmt.Errorf("prompt experiment variant weights must be greater than zero")
	}
	return nil
}

func normalizePromptStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case PromptStatusDraft, "":
		return PromptStatusDraft
	case "review-pending", PromptStatusReviewPending:
		return PromptStatusReviewPending
	case PromptStatusPublished:
		return PromptStatusPublished
	case PromptStatusArchived:
		return PromptStatusArchived
	default:
		return PromptStatusDraft
	}
}

func normalizePromptListFilter(filter PromptListFilter) PromptListFilter {
	filter.Scope = strings.TrimSpace(filter.Scope)
	filter.Status = normalizeOptionalPromptStatus(filter.Status)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Limit = normalizeEvaluationLimit(filter.Limit)
	return filter
}

func normalizeOptionalPromptStatus(status string) string {
	if strings.TrimSpace(status) == "" || strings.EqualFold(status, "all") {
		return ""
	}
	return normalizePromptStatus(status)
}

func promptMatchesFilter(prompt PromptTemplate, filter PromptListFilter) bool {
	if filter.Scope != "" && prompt.Scope != filter.Scope {
		return false
	}
	if filter.Query != "" && !containsLowerAny(filter.Query, prompt.ID, prompt.Name, prompt.Description) {
		return false
	}
	return true
}

func promptContentHash(content string, variablesSchema, renderConfig map[string]any) string {
	payload := map[string]any{
		"content":          strings.TrimSpace(content),
		"variables_schema": variablesSchema,
		"render_config":    renderConfig,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func promptVersionKey(promptID, version string) string {
	return strings.TrimSpace(promptID) + "\x00" + strings.TrimSpace(version)
}

func newPromptExperimentID() string {
	return newEvaluationID("pexp")
}

func marshalPromptVersionJSON(version PromptVersion) ([]byte, []byte, error) {
	vars, err := json.Marshal(version.VariablesSchema)
	if err != nil {
		return nil, nil, err
	}
	config, err := json.Marshal(version.RenderConfig)
	if err != nil {
		return nil, nil, err
	}
	return vars, config, nil
}

func marshalPromptExperimentJSON(experiment PromptExperiment) ([]byte, []byte, error) {
	allocation, err := json.Marshal(experiment.Allocation)
	if err != nil {
		return nil, nil, err
	}
	guardrails, err := json.Marshal(experiment.Guardrails)
	if err != nil {
		return nil, nil, err
	}
	return allocation, guardrails, nil
}

func clonePromptTemplate(prompt PromptTemplate) PromptTemplate {
	prompt.Metadata = cloneEvaluationMap(prompt.Metadata)
	return prompt
}

func clonePromptVersion(version PromptVersion) PromptVersion {
	version.VariablesSchema = cloneEvaluationMap(version.VariablesSchema)
	version.RenderConfig = cloneEvaluationMap(version.RenderConfig)
	if version.PublishedAt != nil {
		value := *version.PublishedAt
		version.PublishedAt = &value
	}
	return version
}

func clonePromptExperiment(experiment PromptExperiment) PromptExperiment {
	experiment.Allocation = cloneEvaluationMap(experiment.Allocation)
	experiment.Guardrails = cloneEvaluationMap(experiment.Guardrails)
	if experiment.StartedAt != nil {
		value := *experiment.StartedAt
		experiment.StartedAt = &value
	}
	if experiment.EndedAt != nil {
		value := *experiment.EndedAt
		experiment.EndedAt = &value
	}
	return experiment
}

func clonePromptExperimentVariant(variant PromptExperimentVariant) PromptExperimentVariant {
	variant.Metadata = cloneEvaluationMap(variant.Metadata)
	return variant
}

func clonePromptExperimentVariants(variants []PromptExperimentVariant) []PromptExperimentVariant {
	out := make([]PromptExperimentVariant, 0, len(variants))
	for _, variant := range variants {
		out = append(out, clonePromptExperimentVariant(variant))
	}
	return out
}

func diffPromptVersions(from, to PromptVersion) []map[string]any {
	fields := []struct {
		name string
		a    any
		b    any
	}{
		{"status", from.Status, to.Status},
		{"version", from.Version, to.Version},
		{"content", from.Content, to.Content},
		{"variables_schema", stableJSONMap(from.VariablesSchema), stableJSONMap(to.VariablesSchema)},
		{"render_config", stableJSONMap(from.RenderConfig), stableJSONMap(to.RenderConfig)},
		{"content_hash", from.ContentHash, to.ContentHash},
		{"base_version", from.BaseVersion, to.BaseVersion},
		{"changelog", from.Changelog, to.Changelog},
	}
	out := make([]map[string]any, 0)
	for _, field := range fields {
		if fmt.Sprint(field.a) == fmt.Sprint(field.b) {
			continue
		}
		out = append(out, map[string]any{"field": field.name, "from": field.a, "to": field.b})
	}
	return out
}

type PromptResolver struct {
	Store     PromptStore
	Fallbacks map[string]PromptVersion
	Cache     *TypedCache[PromptResolution]
}

type PromptResolveRequest struct {
	PromptID      string
	ForcedVersion string
	UserID        string
	SessionID     string
	TenantID      string
	RuntimeMode   string
	Provider      string
	Model         string
}

type PromptResolution struct {
	PromptID   string                      `json:"prompt_id"`
	Version    PromptVersion               `json:"version"`
	Experiment *PromptExperiment           `json:"experiment,omitempty"`
	Variant    *PromptExperimentVariant    `json:"variant,omitempty"`
	Assignment *PromptExperimentAssignment `json:"assignment,omitempty"`
	Fallback   bool                        `json:"fallback,omitempty"`
}

type PromptExperimentAssignment struct {
	ExperimentID string `json:"experiment_id"`
	VariantID    string `json:"variant_id"`
	Bucket       int    `json:"bucket"`
	TrafficScope string `json:"traffic_scope"`
	ScopeID      string `json:"scope_id"`
}

func NewPromptResolver(store PromptStore, fallbacks map[string]PromptVersion) PromptResolver {
	out := make(map[string]PromptVersion, len(fallbacks)+len(defaultPromptFallbacks()))
	for key, value := range defaultPromptFallbacks() {
		out[key] = normalizePromptVersion(value)
	}
	for key, value := range fallbacks {
		out[strings.TrimSpace(key)] = normalizePromptVersion(value)
	}
	return PromptResolver{Store: store, Fallbacks: out}
}

func (r PromptResolver) Resolve(ctx context.Context, req PromptResolveRequest) (PromptResolution, error) {
	promptID := strings.TrimSpace(req.PromptID)
	if promptID == "" {
		return PromptResolution{}, fmt.Errorf("prompt id is required")
	}
	cacheKey := promptResolverCacheKey(promptID, req)
	if r.Cache != nil {
		if cached, ok, err := r.Cache.Get(ctx, cacheKey); err != nil {
			return PromptResolution{}, err
		} else if ok {
			return cached, nil
		}
	}
	resolution, err := r.resolveUncached(ctx, promptID, req)
	if err != nil {
		return PromptResolution{}, err
	}
	if r.Cache != nil {
		_ = r.Cache.Set(ctx, cacheKey, resolution)
	}
	return resolution, nil
}

func (r PromptResolver) resolveUncached(ctx context.Context, promptID string, req PromptResolveRequest) (PromptResolution, error) {
	if r.Store != nil {
		if strings.TrimSpace(req.ForcedVersion) != "" {
			version, err := r.Store.GetPromptVersion(ctx, promptID, req.ForcedVersion)
			if err == nil {
				return PromptResolution{PromptID: promptID, Version: version}, nil
			}
			if !errorsIsSQLNoRows(err) {
				return PromptResolution{}, err
			}
		} else {
			resolution, err := r.resolveExperimentResolution(ctx, promptID, req)
			if err == nil {
				return resolution, nil
			}
			if !errorsIsSQLNoRows(err) {
				return PromptResolution{}, err
			}
			version, err := r.Store.GetPublishedPromptVersion(ctx, promptID)
			if err == nil {
				return PromptResolution{PromptID: promptID, Version: version}, nil
			}
			if !errorsIsSQLNoRows(err) {
				return PromptResolution{}, err
			}
		}
	}
	if fallback, ok := r.Fallbacks[promptID]; ok {
		if strings.TrimSpace(req.ForcedVersion) != "" && fallback.Version != strings.TrimSpace(req.ForcedVersion) {
			return PromptResolution{}, sql.ErrNoRows
		}
		return PromptResolution{PromptID: promptID, Version: fallback, Fallback: true}, nil
	}
	return PromptResolution{}, sql.ErrNoRows
}

func promptResolverCacheKey(promptID string, req PromptResolveRequest) string {
	return BuildCacheKey(CacheKeyOptions{
		Namespace: promptResolverCacheNamespace,
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Version:   strings.TrimSpace(req.ForcedVersion),
		Parts: []string{
			"prompt=" + strings.TrimSpace(promptID),
			"tenant=" + strings.TrimSpace(req.TenantID),
			"mode=" + strings.TrimSpace(req.RuntimeMode),
			"provider=" + strings.TrimSpace(req.Provider),
			"model=" + strings.TrimSpace(req.Model),
		},
	})
}

func (r PromptResolver) resolveExperimentResolution(ctx context.Context, promptID string, req PromptResolveRequest) (PromptResolution, error) {
	if r.Store == nil {
		return PromptResolution{}, sql.ErrNoRows
	}
	experiments, err := r.Store.ListPromptExperiments(ctx, PromptExperimentFilter{PromptID: promptID, Status: PromptExperimentStatusRunning, Limit: 25})
	if err != nil {
		return PromptResolution{}, err
	}
	now := time.Now().UTC()
	for _, experiment := range experiments {
		if !promptExperimentActiveAt(experiment, now) {
			continue
		}
		_, variants, err := r.Store.GetPromptExperiment(ctx, experiment.ID)
		if err != nil {
			return PromptResolution{}, err
		}
		variant, assignment, ok := assignPromptExperimentVariant(experiment, variants, req)
		if !ok {
			continue
		}
		version, err := r.Store.GetPromptVersion(ctx, promptID, variant.PromptVersion)
		if err != nil {
			return PromptResolution{}, err
		}
		experimentCopy := clonePromptExperiment(experiment)
		variantCopy := clonePromptExperimentVariant(variant)
		return PromptResolution{
			PromptID:   promptID,
			Version:    version,
			Experiment: &experimentCopy,
			Variant:    &variantCopy,
			Assignment: &assignment,
		}, nil
	}
	return PromptResolution{}, sql.ErrNoRows
}

func promptExperimentActiveAt(experiment PromptExperiment, now time.Time) bool {
	experiment = normalizePromptExperiment(experiment)
	if experiment.Status != PromptExperimentStatusRunning {
		return false
	}
	if experiment.StartedAt != nil && now.Before(*experiment.StartedAt) {
		return false
	}
	if experiment.EndedAt != nil && now.After(*experiment.EndedAt) {
		return false
	}
	return true
}

func assignPromptExperimentVariant(experiment PromptExperiment, variants []PromptExperimentVariant, req PromptResolveRequest) (PromptExperimentVariant, PromptExperimentAssignment, bool) {
	scopeID := promptExperimentScopeID(experiment.TrafficScope, req)
	if scopeID == "" {
		return PromptExperimentVariant{}, PromptExperimentAssignment{}, false
	}
	total := 0
	for _, variant := range variants {
		if variant.Weight > 0 {
			total += variant.Weight
		}
	}
	if total <= 0 {
		return PromptExperimentVariant{}, PromptExperimentAssignment{}, false
	}
	bucket := promptExperimentBucket(experiment.ID, scopeID, total)
	cursor := 0
	for _, variant := range variants {
		if variant.Weight <= 0 {
			continue
		}
		cursor += variant.Weight
		if bucket < cursor {
			return variant, PromptExperimentAssignment{
				ExperimentID: experiment.ID,
				VariantID:    variant.VariantID,
				Bucket:       bucket,
				TrafficScope: experiment.TrafficScope,
				ScopeID:      scopeID,
			}, true
		}
	}
	return PromptExperimentVariant{}, PromptExperimentAssignment{}, false
}

func promptExperimentScopeID(scope string, req PromptResolveRequest) string {
	switch normalizePromptTrafficScope(scope) {
	case PromptTrafficScopeSession:
		return strings.TrimSpace(req.SessionID)
	case PromptTrafficScopeTenant:
		return strings.TrimSpace(req.TenantID)
	default:
		return strings.TrimSpace(req.UserID)
	}
}

func promptExperimentBucket(experimentID, scopeID string, modulo int) int {
	if modulo <= 0 {
		modulo = 100
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(experimentID) + "\x00" + strings.TrimSpace(scopeID)))
	value := int(sum[0])<<24 | int(sum[1])<<16 | int(sum[2])<<8 | int(sum[3])
	if value < 0 {
		value = -value
	}
	return value % modulo
}

type PromptRenderResult struct {
	PromptID         string         `json:"prompt_id"`
	PromptVersion    string         `json:"prompt_version"`
	PromptHash       string         `json:"prompt_hash"`
	Content          string         `json:"content"`
	RenderedPreview  string         `json:"rendered_preview,omitempty"`
	TokenEstimate    int            `json:"token_estimate,omitempty"`
	MissingVariables []string       `json:"missing_variables,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

func RenderPrompt(resolution PromptResolution, variables map[string]any) (PromptRenderResult, error) {
	version := normalizePromptVersion(resolution.Version)
	missing := missingPromptVariables(version.VariablesSchema, variables)
	if len(missing) > 0 {
		return PromptRenderResult{PromptID: version.PromptID, PromptVersion: version.Version, PromptHash: version.ContentHash, MissingVariables: missing}, fmt.Errorf("missing prompt variables: %s", strings.Join(missing, ", "))
	}
	content := renderPromptContent(version.Content, variables)
	return PromptRenderResult{
		PromptID:        firstNonEmptyString(version.PromptID, resolution.PromptID),
		PromptVersion:   version.Version,
		PromptHash:      version.ContentHash,
		Content:         content,
		RenderedPreview: truncateString(content, promptRenderPreviewLimit(version.RenderConfig)),
		TokenEstimate:   estimateTextTokens(content),
		Metadata:        promptRenderMetadata(resolution),
	}, nil
}

func PromptMetadataFromRender(rendered PromptRenderResult) PromptMetadata {
	metadata := PromptMetadata{
		PromptID:      rendered.PromptID,
		PromptVersion: rendered.PromptVersion,
		PromptHash:    rendered.PromptHash,
	}
	if rendered.Metadata != nil {
		metadata.ExperimentID = strings.TrimSpace(fmt.Sprint(rendered.Metadata["experiment_id"]))
		metadata.VariantID = strings.TrimSpace(fmt.Sprint(rendered.Metadata["variant_id"]))
	}
	return metadata
}

func promptRenderMetadata(resolution PromptResolution) map[string]any {
	metadata := map[string]any{"fallback": resolution.Fallback}
	if resolution.Assignment != nil {
		metadata["experiment_id"] = resolution.Assignment.ExperimentID
		metadata["variant_id"] = resolution.Assignment.VariantID
		metadata["bucket"] = resolution.Assignment.Bucket
		metadata["traffic_scope"] = resolution.Assignment.TrafficScope
	}
	return metadata
}

func missingPromptVariables(schema map[string]any, variables map[string]any) []string {
	required, ok := schema["required"].([]any)
	if !ok {
		return nil
	}
	var missing []string
	for _, item := range required {
		name := strings.TrimSpace(fmt.Sprint(item))
		if name == "" {
			continue
		}
		value, ok := variables[name]
		if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

var promptVariablePattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

func renderPromptContent(content string, variables map[string]any) string {
	return promptVariablePattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := promptVariablePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		value, ok := variables[parts[1]]
		if !ok {
			return ""
		}
		switch typed := value.(type) {
		case string:
			return typed
		case []byte:
			return string(typed)
		default:
			data, err := json.Marshal(typed)
			if err != nil {
				return fmt.Sprint(typed)
			}
			return string(data)
		}
	})
}

func promptRenderPreviewLimit(config map[string]any) int {
	if config != nil {
		if value, ok := config["preview_limit"].(float64); ok && value > 0 {
			return int(value)
		}
		if value, ok := config["preview_limit"].(int); ok && value > 0 {
			return value
		}
	}
	return 2000
}

func defaultPromptFallbacks() map[string]PromptVersion {
	return map[string]PromptVersion{
		PromptIDLiveSetup: {
			PromptID: PromptIDLiveSetup,
			Version:  "builtin-v1",
			Status:   PromptStatusPublished,
			Content:  "{{content}}",
		},
		PromptIDEvalJudge: {
			PromptID: PromptIDEvalJudge,
			Version:  DefaultGoldenJudgePromptVersion,
			Status:   PromptStatusPublished,
			Content:  goldenJudgeSystemPrompt(),
		},
		PromptIDMemoryExtract: {
			PromptID: PromptIDMemoryExtract,
			Version:  "builtin-v1",
			Status:   PromptStatusPublished,
			Content:  memoryExtractionPromptTemplate(),
			VariablesSchema: map[string]any{
				"required": []any{"conversation_json"},
			},
		},
	}
}

func errorsIsSQLNoRows(err error) bool {
	return err == nil || errors.Is(err, sql.ErrNoRows)
}

func timePtrValue(ptr *time.Time, fallback time.Time) time.Time {
	if ptr != nil {
		return ptr.UTC()
	}
	if fallback.IsZero() {
		return time.Now().UTC()
	}
	return fallback.UTC()
}

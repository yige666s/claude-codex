package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type GoldenSetFilter struct {
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

func (s *MemoryEvaluationStore) UpsertGoldenSet(_ context.Context, set GoldenSet) (GoldenSet, error) {
	if err := validateGoldenSetForStorage(set); err != nil {
		return GoldenSet{}, err
	}
	set = normalizeGoldenSet(set)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := goldenSetKey(set.ID, set.Version)
	if existing, ok := s.goldenSets[key]; ok {
		set.CreatedAt = existing.CreatedAt
	}
	s.goldenSets[key] = cloneGoldenSet(set)
	return cloneGoldenSet(set), nil
}

func (s *MemoryEvaluationStore) GetGoldenSet(_ context.Context, id string) (GoldenSet, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	return latestGoldenSetFromMap(s.goldenSets, id)
}

func (s *MemoryEvaluationStore) GetGoldenSetVersion(_ context.Context, id, version string) (GoldenSet, error) {
	id = strings.TrimSpace(id)
	version = strings.TrimSpace(version)
	if version == "" {
		return s.GetGoldenSet(context.Background(), id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.goldenSets[goldenSetKey(id, version)]
	if !ok {
		return GoldenSet{}, sql.ErrNoRows
	}
	return cloneGoldenSet(set), nil
}

func (s *MemoryEvaluationStore) ListGoldenSets(_ context.Context, filter GoldenSetFilter) ([]GoldenSet, error) {
	filter = normalizeGoldenSetFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GoldenSet, 0, len(s.goldenSets))
	for _, set := range s.goldenSets {
		if filter.ID != "" && set.ID != filter.ID {
			continue
		}
		if filter.Version != "" && set.Version != filter.Version {
			continue
		}
		out = append(out, cloneGoldenSet(set))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryEvaluationStore) DeleteGoldenSet(_ context.Context, id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for key, set := range s.goldenSets {
		if set.ID == id {
			delete(s.goldenSets, key)
			found = true
		}
	}
	if !found {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLEvaluationStore) UpsertGoldenSet(ctx context.Context, set GoldenSet) (GoldenSet, error) {
	if err := validateGoldenSetForStorage(set); err != nil {
		return GoldenSet{}, err
	}
	set = normalizeGoldenSet(set)
	metadata, err := json.Marshal(set.Metadata)
	if err != nil {
		return GoldenSet{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GoldenSet{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_eval_golden_sets
SET name = ?, description = ?, metadata = ?, created_at = ?, updated_at = ?
WHERE id = ? AND version = ?`),
		set.Name, set.Description, string(metadata), sqlTimeValue(set.CreatedAt, s.dialect), sqlTimeValue(set.UpdatedAt, s.dialect), set.ID, set.Version)
	if err != nil {
		return GoldenSet{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_eval_golden_sets (id, version, name, description, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`),
			set.ID, set.Version, set.Name, set.Description, string(metadata), sqlTimeValue(set.CreatedAt, s.dialect), sqlTimeValue(set.UpdatedAt, s.dialect)); err != nil {
			return GoldenSet{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_eval_golden_cases WHERE set_id = ? AND set_version = ?`), set.ID, set.Version); err != nil {
		return GoldenSet{}, err
	}
	for index, item := range set.Cases {
		if err := insertSQLGoldenCase(ctx, tx, s.dialect, set.ID, set.Version, index, item, set.CreatedAt, set.UpdatedAt); err != nil {
			return GoldenSet{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return GoldenSet{}, err
	}
	return set, nil
}

func (s *SQLEvaluationStore) GetGoldenSet(ctx context.Context, id string) (GoldenSet, error) {
	return s.GetGoldenSetVersion(ctx, id, "")
}

func (s *SQLEvaluationStore) GetGoldenSetVersion(ctx context.Context, id, version string) (GoldenSet, error) {
	id = strings.TrimSpace(id)
	version = strings.TrimSpace(version)
	if version == "" {
		set, err := scanGoldenSet(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, version, name, description, metadata, created_at, updated_at
FROM agent_eval_golden_sets WHERE id = ?
ORDER BY updated_at DESC
LIMIT 1`), id))
		if err != nil {
			return GoldenSet{}, err
		}
		cases, err := s.listGoldenCases(ctx, set.ID, set.Version)
		if err != nil {
			return GoldenSet{}, err
		}
		set.Cases = cases
		return normalizeGoldenSet(set), nil
	}
	set, err := scanGoldenSet(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, version, name, description, metadata, created_at, updated_at
FROM agent_eval_golden_sets WHERE id = ? AND version = ?`), id, version))
	if err != nil {
		return GoldenSet{}, err
	}
	cases, err := s.listGoldenCases(ctx, set.ID, set.Version)
	if err != nil {
		return GoldenSet{}, err
	}
	set.Cases = cases
	return normalizeGoldenSet(set), nil
}

func (s *SQLEvaluationStore) ListGoldenSets(ctx context.Context, filter GoldenSetFilter) ([]GoldenSet, error) {
	filter = normalizeGoldenSetFilter(filter)
	query := `SELECT id, version, name, description, metadata, created_at, updated_at FROM agent_eval_golden_sets`
	args := []any{}
	where := []string{}
	if filter.ID != "" {
		where = append(where, "id = ?")
		args = append(args, filter.ID)
	}
	if filter.Version != "" {
		where = append(where, "version = ?")
		args = append(args, filter.Version)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GoldenSet{}
	for rows.Next() {
		set, err := scanGoldenSet(rows)
		if err != nil {
			return nil, err
		}
		set.Cases, err = s.listGoldenCases(ctx, set.ID, set.Version)
		if err != nil {
			return nil, err
		}
		out = append(out, normalizeGoldenSet(set))
	}
	return out, rows.Err()
}

func (s *SQLEvaluationStore) DeleteGoldenSet(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_eval_golden_sets WHERE id = ?`), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertSQLGoldenCase(ctx context.Context, execer sqlExecer, dialect SQLDialect, setID, setVersion string, position int, item GoldenCase, createdAt, updatedAt time.Time) error {
	expectedFacts, goldEvidence, tags, metadata, err := marshalGoldenCaseJSON(item)
	if err != nil {
		return err
	}
	_, err = execer.ExecContext(ctx, dialect.Bind(`
INSERT INTO agent_eval_golden_cases (id, set_id, set_version, position, query, expected_answer, expected_facts, gold_evidence, tags, metadata, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		item.ID, setID, setVersion, position, item.Query, item.ExpectedAnswer, string(expectedFacts), string(goldEvidence), string(tags), string(metadata),
		sqlTimeValue(createdAt, dialect), sqlTimeValue(updatedAt, dialect))
	return err
}

func (s *SQLEvaluationStore) listGoldenCases(ctx context.Context, setID, setVersion string) ([]GoldenCase, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT id, query, expected_answer, expected_facts, gold_evidence, tags, metadata
FROM agent_eval_golden_cases
WHERE set_id = ? AND set_version = ?
ORDER BY position ASC, created_at ASC`), strings.TrimSpace(setID), strings.TrimSpace(setVersion))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GoldenCase{}
	for rows.Next() {
		item, err := scanGoldenCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, normalizeGoldenCase(item))
	}
	return out, rows.Err()
}

func marshalGoldenCaseJSON(item GoldenCase) ([]byte, []byte, []byte, []byte, error) {
	expectedFacts, err := json.Marshal(item.ExpectedFacts)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	goldEvidence, err := json.Marshal(item.GoldEvidence)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tags, err := json.Marshal(item.Tags)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return expectedFacts, goldEvidence, tags, metadata, nil
}

func scanGoldenSet(row evaluationScanner) (GoldenSet, error) {
	var set GoldenSet
	var metadata string
	var createdAt, updatedAt any
	if err := row.Scan(&set.ID, &set.Version, &set.Name, &set.Description, &metadata, &createdAt, &updatedAt); err != nil {
		return GoldenSet{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &set.Metadata)
	var err error
	if set.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return GoldenSet{}, err
	}
	if set.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return GoldenSet{}, err
	}
	return set, nil
}

func scanGoldenCase(row evaluationScanner) (GoldenCase, error) {
	var item GoldenCase
	var expectedFacts, goldEvidence, tags, metadata string
	if err := row.Scan(&item.ID, &item.Query, &item.ExpectedAnswer, &expectedFacts, &goldEvidence, &tags, &metadata); err != nil {
		return GoldenCase{}, err
	}
	_ = json.Unmarshal([]byte(expectedFacts), &item.ExpectedFacts)
	_ = json.Unmarshal([]byte(goldEvidence), &item.GoldEvidence)
	_ = json.Unmarshal([]byte(tags), &item.Tags)
	_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	return item, nil
}

func normalizeGoldenSetFilter(filter GoldenSetFilter) GoldenSetFilter {
	filter.ID = strings.TrimSpace(filter.ID)
	filter.Version = strings.TrimSpace(filter.Version)
	filter.Limit = normalizeEvaluationLimit(filter.Limit)
	return filter
}

func goldenSetKey(id, version string) string {
	return strings.TrimSpace(id) + "\x00" + strings.TrimSpace(version)
}

func latestGoldenSetFromMap(values map[string]GoldenSet, id string) (GoldenSet, error) {
	var latest GoldenSet
	found := false
	for _, set := range values {
		if set.ID != id {
			continue
		}
		if !found || set.UpdatedAt.After(latest.UpdatedAt) {
			latest = set
			found = true
		}
	}
	if !found {
		return GoldenSet{}, sql.ErrNoRows
	}
	return cloneGoldenSet(latest), nil
}

func cloneGoldenSet(set GoldenSet) GoldenSet {
	set.Metadata = cloneEvaluationMap(set.Metadata)
	set.Cases = cloneGoldenCases(set.Cases)
	return set
}

func cloneGoldenCases(cases []GoldenCase) []GoldenCase {
	out := make([]GoldenCase, len(cases))
	for i, item := range cases {
		item.ExpectedFacts = append([]string{}, item.ExpectedFacts...)
		item.GoldEvidence = cloneGoldenEvidence(item.GoldEvidence)
		item.Tags = append([]string{}, item.Tags...)
		item.Metadata = cloneEvaluationMap(item.Metadata)
		out[i] = item
	}
	if out == nil {
		return []GoldenCase{}
	}
	return out
}

func cloneGoldenEvidence(items []GoldenEvidence) []GoldenEvidence {
	out := make([]GoldenEvidence, len(items))
	for i, item := range items {
		item.Metadata = cloneEvaluationMap(item.Metadata)
		out[i] = item
	}
	if out == nil {
		return []GoldenEvidence{}
	}
	return out
}

func validateGoldenSetForStorage(set GoldenSet) error {
	if strings.TrimSpace(set.Name) == "" && strings.TrimSpace(set.ID) == "" {
		return fmt.Errorf("golden set name is required")
	}
	for _, item := range set.Cases {
		if strings.TrimSpace(item.Query) == "" {
			return fmt.Errorf("golden case query is required")
		}
	}
	return nil
}

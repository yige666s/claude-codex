package agentruntime

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/state"
	publictypes "claude-codex/internal/public/types"
)

const (
	AssetInsightStatusPending = "pending"
	AssetInsightStatusDone    = "done"
	AssetInsightStatusFailed  = "failed"
)

type AssetInsight struct {
	ID                string            `json:"id"`
	AssetID           string            `json:"asset_id"`
	Kind              string            `json:"kind"`
	UserID            string            `json:"user_id,omitempty"`
	SessionID         string            `json:"session_id,omitempty"`
	JobID             string            `json:"job_id,omitempty"`
	Filename          string            `json:"filename"`
	ContentType       string            `json:"content_type"`
	Status            string            `json:"status"`
	Summary           string            `json:"summary,omitempty"`
	OCRText           []string          `json:"ocr_text,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Entities          []map[string]any  `json:"entities,omitempty"`
	Relationships     []map[string]any  `json:"relationships,omitempty"`
	Style             map[string]any    `json:"style,omitempty"`
	CandidateMemories []MemoryCandidate `json:"candidate_memories,omitempty"`
	Extractor         string            `json:"extractor,omitempty"`
	Confidence        float64           `json:"confidence,omitempty"`
	Error             string            `json:"error,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	CompletedAt       *time.Time        `json:"completed_at,omitempty"`
}

type AssetInsightStore interface {
	Init(context.Context) error
	UpsertAssetInsight(context.Context, AssetInsight) (AssetInsight, error)
	GetAssetInsight(context.Context, string, string) (AssetInsight, error)
}

type MemoryAssetInsightStore struct {
	mu      sync.Mutex
	byAsset map[string]AssetInsight
}

func NewMemoryAssetInsightStore() *MemoryAssetInsightStore {
	return &MemoryAssetInsightStore{byAsset: map[string]AssetInsight{}}
}

func (s *MemoryAssetInsightStore) Init(context.Context) error {
	if s.byAsset == nil {
		s.byAsset = map[string]AssetInsight{}
	}
	return nil
}

func (s *MemoryAssetInsightStore) UpsertAssetInsight(_ context.Context, insight AssetInsight) (AssetInsight, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byAsset == nil {
		s.byAsset = map[string]AssetInsight{}
	}
	insight = normalizeAssetInsight(insight)
	if existing, ok := s.byAsset[insight.AssetID]; ok {
		insight.ID = existing.ID
		insight.CreatedAt = existing.CreatedAt
	}
	s.byAsset[insight.AssetID] = insight
	return insight, nil
}

func (s *MemoryAssetInsightStore) GetAssetInsight(_ context.Context, userID, assetID string) (AssetInsight, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	insight, ok := s.byAsset[strings.TrimSpace(assetID)]
	if !ok || (strings.TrimSpace(userID) != "" && insight.UserID != userID) {
		return AssetInsight{}, fmt.Errorf("asset insight not found")
	}
	return insight, nil
}

type SQLAssetInsightStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLAssetInsightStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLAssetInsightStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLAssetInsightStore{db: db, dialect: dialect}
}

func (s *SQLAssetInsightStore) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, s.db, "agent_asset_insights",
		"insight_id",
		"asset_id",
		"kind",
		"user_id",
		"session_id",
		"job_id",
		"filename",
		"content_type",
		"status",
		"summary",
		"ocr_text",
		"tags",
		"entities",
		"relationships",
		"style",
		"candidate_memories",
		"extractor",
		"confidence",
		"error",
		"created_at",
		"updated_at",
		"completed_at",
	)
}

func (s *SQLAssetInsightStore) UpsertAssetInsight(ctx context.Context, insight AssetInsight) (AssetInsight, error) {
	if s == nil || s.db == nil {
		return AssetInsight{}, fmt.Errorf("sql asset insight store is not configured")
	}
	insight = normalizeAssetInsight(insight)
	if existing, err := s.getAssetInsightByAssetID(ctx, insight.AssetID); err == nil {
		insight.ID = existing.ID
		insight.CreatedAt = existing.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return AssetInsight{}, err
	}
	ocrText, err := json.Marshal(insight.OCRText)
	if err != nil {
		return AssetInsight{}, err
	}
	tags, err := json.Marshal(insight.Tags)
	if err != nil {
		return AssetInsight{}, err
	}
	entities, err := json.Marshal(insight.Entities)
	if err != nil {
		return AssetInsight{}, err
	}
	relationships, err := json.Marshal(insight.Relationships)
	if err != nil {
		return AssetInsight{}, err
	}
	style, err := json.Marshal(insight.Style)
	if err != nil {
		return AssetInsight{}, err
	}
	candidateMemories, err := json.Marshal(insight.CandidateMemories)
	if err != nil {
		return AssetInsight{}, err
	}
	jsonValue := "?"
	if s.dialect == SQLDialectPostgres {
		jsonValue = "?::jsonb"
	}
	query := fmt.Sprintf(`
INSERT INTO agent_asset_insights (
	insight_id, asset_id, kind, user_id, session_id, job_id, filename, content_type, status,
	summary, ocr_text, tags, entities, relationships, style, candidate_memories,
	extractor, confidence, error, created_at, updated_at, completed_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, %[1]s, %[1]s, %[1]s, %[1]s, %[1]s, %[1]s, ?, ?, ?, ?, ?, ?)
ON CONFLICT (asset_id) DO UPDATE SET
	insight_id = excluded.insight_id,
	kind = excluded.kind,
	user_id = excluded.user_id,
	session_id = excluded.session_id,
	job_id = excluded.job_id,
	filename = excluded.filename,
	content_type = excluded.content_type,
	status = excluded.status,
	summary = excluded.summary,
	ocr_text = excluded.ocr_text,
	tags = excluded.tags,
	entities = excluded.entities,
	relationships = excluded.relationships,
	style = excluded.style,
	candidate_memories = excluded.candidate_memories,
	extractor = excluded.extractor,
	confidence = excluded.confidence,
	error = excluded.error,
	updated_at = excluded.updated_at,
	completed_at = excluded.completed_at`, jsonValue)
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(query),
		insight.ID,
		insight.AssetID,
		insight.Kind,
		insight.UserID,
		insight.SessionID,
		insight.JobID,
		insight.Filename,
		insight.ContentType,
		insight.Status,
		insight.Summary,
		string(ocrText),
		string(tags),
		string(entities),
		string(relationships),
		string(style),
		string(candidateMemories),
		insight.Extractor,
		insight.Confidence,
		insight.Error,
		sqlTimeValue(insight.CreatedAt, s.dialect),
		sqlTimeValue(insight.UpdatedAt, s.dialect),
		nullableSQLTimeValue(insight.CompletedAt, s.dialect),
	)
	if err != nil {
		return AssetInsight{}, err
	}
	return s.GetAssetInsight(ctx, insight.UserID, insight.AssetID)
}

func (s *SQLAssetInsightStore) GetAssetInsight(ctx context.Context, userID, assetID string) (AssetInsight, error) {
	if s == nil || s.db == nil {
		return AssetInsight{}, fmt.Errorf("sql asset insight store is not configured")
	}
	return scanAssetInsightRow(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT insight_id, asset_id, kind, user_id, session_id, job_id, filename, content_type, status,
	summary, ocr_text, tags, entities, relationships, style, candidate_memories,
	extractor, confidence, error, created_at, updated_at, completed_at
FROM agent_asset_insights
WHERE user_id = ? AND asset_id = ?`), strings.TrimSpace(userID), strings.TrimSpace(assetID)))
}

func (s *SQLAssetInsightStore) getAssetInsightByAssetID(ctx context.Context, assetID string) (AssetInsight, error) {
	return scanAssetInsightRow(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT insight_id, asset_id, kind, user_id, session_id, job_id, filename, content_type, status,
	summary, ocr_text, tags, entities, relationships, style, candidate_memories,
	extractor, confidence, error, created_at, updated_at, completed_at
FROM agent_asset_insights
WHERE asset_id = ?`), strings.TrimSpace(assetID)))
}

type assetInsightScanner interface {
	Scan(dest ...any) error
}

func scanAssetInsightRow(row assetInsightScanner) (AssetInsight, error) {
	var insight AssetInsight
	var ocrTextJSON, tagsJSON, entitiesJSON, relationshipsJSON, styleJSON, candidateMemoriesJSON string
	var createdAt, updatedAt, completedAt any
	if err := row.Scan(
		&insight.ID,
		&insight.AssetID,
		&insight.Kind,
		&insight.UserID,
		&insight.SessionID,
		&insight.JobID,
		&insight.Filename,
		&insight.ContentType,
		&insight.Status,
		&insight.Summary,
		&ocrTextJSON,
		&tagsJSON,
		&entitiesJSON,
		&relationshipsJSON,
		&styleJSON,
		&candidateMemoriesJSON,
		&insight.Extractor,
		&insight.Confidence,
		&insight.Error,
		&createdAt,
		&updatedAt,
		&completedAt,
	); err != nil {
		return AssetInsight{}, err
	}
	_ = json.Unmarshal([]byte(ocrTextJSON), &insight.OCRText)
	_ = json.Unmarshal([]byte(tagsJSON), &insight.Tags)
	_ = json.Unmarshal([]byte(entitiesJSON), &insight.Entities)
	_ = json.Unmarshal([]byte(relationshipsJSON), &insight.Relationships)
	_ = json.Unmarshal([]byte(styleJSON), &insight.Style)
	_ = json.Unmarshal([]byte(candidateMemoriesJSON), &insight.CandidateMemories)
	var err error
	if insight.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return AssetInsight{}, err
	}
	if insight.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return AssetInsight{}, err
	}
	if insight.CompletedAt, err = parseNullableSQLTime(completedAt); err != nil {
		return AssetInsight{}, err
	}
	return normalizeAssetInsight(insight), nil
}

func (r *Runtime) SetAssetInsightStore(store AssetInsightStore) {
	r.assetInsights = store
}

func (r *Runtime) AnalyzeAssetInsight(ctx context.Context, userID, kind, assetID string) (AssetInsight, error) {
	asset, data, err := r.getAsset(ctx, normalizeAssetKind(kind), userID, assetID)
	if err != nil {
		return AssetInsight{}, err
	}
	return r.processAssetInsight(ctx, asset, data)
}

func (r *Runtime) GetAssetInsight(ctx context.Context, userID, assetID string) (AssetInsight, error) {
	if r == nil || r.assetInsights == nil {
		return AssetInsight{}, fmt.Errorf("asset insight store is not configured")
	}
	return r.assetInsights.GetAssetInsight(ctx, userID, assetID)
}

func (r *Runtime) enqueueAssetInsight(asset *Artifact, data []byte) {
	if r == nil || r.assetInsights == nil || asset == nil || normalizeAssetKind(asset.Kind) != AssetKindArtifact || !isImageContentType(asset.ContentType) {
		return
	}
	assetCopy := *asset
	dataCopy := append([]byte(nil), data...)
	r.mu.Lock()
	if r.shuttingDown {
		r.mu.Unlock()
		return
	}
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if _, err := r.processAssetInsight(ctx, &assetCopy, dataCopy); err != nil {
			logError(ctx, r.logger, "asset insight processing failed", err, slog.String("asset_id", assetCopy.ID))
		}
	}()
}

func (r *Runtime) processAssetInsight(ctx context.Context, asset *Artifact, data []byte) (AssetInsight, error) {
	if r == nil || r.assetInsights == nil {
		return AssetInsight{}, fmt.Errorf("asset insight store is not configured")
	}
	now := time.Now().UTC()
	pending := assetInsightFromAsset(asset, now)
	pending.Status = AssetInsightStatusPending
	if _, err := r.assetInsights.UpsertAssetInsight(ctx, pending); err != nil {
		return AssetInsight{}, err
	}
	insight, err := r.buildAssetInsight(ctx, asset, data)
	insight.ID = pending.ID
	insight.AssetID = pending.AssetID
	insight.Kind = pending.Kind
	insight.UserID = pending.UserID
	insight.SessionID = pending.SessionID
	insight.JobID = pending.JobID
	insight.Filename = pending.Filename
	insight.ContentType = pending.ContentType
	insight.CreatedAt = pending.CreatedAt
	insight.UpdatedAt = time.Now().UTC()
	if err != nil {
		insight.Status = AssetInsightStatusFailed
		insight.Error = err.Error()
		_, saveErr := r.assetInsights.UpsertAssetInsight(ctx, insight)
		if saveErr != nil {
			return insight, saveErr
		}
		return insight, err
	}
	done := insight.UpdatedAt
	insight.CompletedAt = &done
	insight.Status = AssetInsightStatusDone
	return r.assetInsights.UpsertAssetInsight(ctx, insight)
}

func (r *Runtime) buildAssetInsight(ctx context.Context, asset *Artifact, data []byte) (AssetInsight, error) {
	if asset == nil {
		return AssetInsight{}, fmt.Errorf("asset is required")
	}
	contentType := normalizedContentType(firstNonEmptyString(asset.ContentType, mime.TypeByExtension(filepath.Ext(asset.Filename))))
	if isSVGContentType(contentType, asset.Filename) {
		return svgAssetInsight(asset, data), nil
	}
	if r.engineFactory == nil {
		return fallbackAssetInsight(asset), nil
	}
	output, err := r.runVisionAssetInsight(ctx, asset, contentType, data)
	if err != nil {
		fallback := fallbackAssetInsight(asset)
		fallback.Error = err.Error()
		return fallback, nil
	}
	return parseAssetInsightOutput(asset, output)
}

func (r *Runtime) runVisionAssetInsight(ctx context.Context, asset *Artifact, contentType string, data []byte) (string, error) {
	if int64(len(data)) > vertexInlineAttachmentLimitBytes {
		return "", fmt.Errorf("image exceeds inline vision limit of %d bytes", vertexInlineAttachmentLimitBytes)
	}
	blocks := []publictypes.ContentBlock{
		{
			Type: "text",
			Text: PromptVisionAssetInsight,
		},
		{
			Type: attachmentBlockType(contentType),
			Source: map[string]interface{}{
				"type":       "base64",
				"media_type": contentType,
				"data":       base64.StdEncoding.EncodeToString(data),
			},
		},
	}
	result, err := runWithTokenStreamContent(ctx, r.runnerForScope(Scope{UserID: asset.UserID, SessionID: asset.SessionID}), state.NewSession(""), blocks, false, nil)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func parseAssetInsightOutput(asset *Artifact, output string) (AssetInsight, error) {
	normalized, err := normalizeLLMJSONOutput(output)
	if err != nil {
		return AssetInsight{}, err
	}
	var payload struct {
		Summary                  string            `json:"summary"`
		OCRText                  []string          `json:"ocr_text"`
		VisualType               string            `json:"visual_type"`
		Tags                     []string          `json:"tags"`
		Entities                 []map[string]any  `json:"entities"`
		Relationships            []map[string]any  `json:"relationships"`
		Style                    map[string]any    `json:"style"`
		CandidateProjectMemories []MemoryCandidate `json:"candidate_project_memories"`
		CandidateUserMemories    []MemoryCandidate `json:"candidate_user_memories"`
		Confidence               float64           `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(normalized), &payload); err != nil {
		return AssetInsight{}, err
	}
	insight := fallbackAssetInsight(asset)
	insight.Summary = strings.TrimSpace(payload.Summary)
	insight.OCRText = cleanStringList(payload.OCRText, 40)
	insight.Tags = cleanStringList(append(payload.Tags, payload.VisualType), 40)
	insight.Entities = payload.Entities
	insight.Relationships = payload.Relationships
	insight.Style = payload.Style
	insight.CandidateMemories = normalizeAssetInsightCandidates(payload.CandidateProjectMemories, payload.CandidateUserMemories)
	insight.Extractor = "vision"
	insight.Confidence = clampFloat(payload.Confidence, 0, 1)
	if insight.Summary == "" {
		insight.Summary = fallbackAssetInsight(asset).Summary
	}
	return insight, nil
}

func svgAssetInsight(asset *Artifact, data []byte) AssetInsight {
	plain := svgPlainText(data)
	insight := fallbackAssetInsight(asset)
	insight.Extractor = "svg"
	insight.Tags = cleanStringList(append(insight.Tags, "svg", "diagram"), 40)
	if plain != "" {
		insight.Summary = fmt.Sprintf("Generated SVG artifact %q containing visible text: %s", asset.Filename, truncateOneLine(plain, 240))
		insight.OCRText = cleanStringList(strings.FieldsFunc(plain, func(r rune) bool { return r == '\n' || r == '\r' }), 80)
		insight.Confidence = 0.72
	}
	return insight
}

func fallbackAssetInsight(asset *Artifact) AssetInsight {
	now := time.Now().UTC()
	insight := assetInsightFromAsset(asset, now)
	insight.Status = AssetInsightStatusDone
	insight.Summary = fmt.Sprintf("Generated image artifact %q", asset.Filename)
	insight.Tags = cleanStringList([]string{"artifact", normalizeAssetKind(asset.Kind), "image", filepath.Ext(asset.Filename)}, 20)
	insight.Style = map[string]any{}
	insight.Extractor = "metadata"
	insight.Confidence = 0.45
	return insight
}

func assetInsightFromAsset(asset *Artifact, now time.Time) AssetInsight {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return AssetInsight{
		ID:          "insight-" + newSortableID(),
		AssetID:     strings.TrimSpace(asset.ID),
		Kind:        normalizeAssetKind(asset.Kind),
		UserID:      strings.TrimSpace(asset.UserID),
		SessionID:   strings.TrimSpace(asset.SessionID),
		JobID:       strings.TrimSpace(asset.JobID),
		Filename:    filepath.Base(strings.TrimSpace(asset.Filename)),
		ContentType: normalizedContentType(asset.ContentType),
		Status:      AssetInsightStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func normalizeAssetInsight(insight AssetInsight) AssetInsight {
	now := time.Now().UTC()
	if insight.ID == "" {
		insight.ID = "insight-" + newSortableID()
	}
	insight.AssetID = strings.TrimSpace(insight.AssetID)
	insight.Kind = normalizeAssetKind(insight.Kind)
	insight.Filename = filepath.Base(strings.TrimSpace(insight.Filename))
	insight.ContentType = normalizedContentType(insight.ContentType)
	if insight.Status == "" {
		insight.Status = AssetInsightStatusPending
	}
	if insight.CreatedAt.IsZero() {
		insight.CreatedAt = now
	}
	if insight.UpdatedAt.IsZero() {
		insight.UpdatedAt = now
	}
	insight.OCRText = cleanStringList(insight.OCRText, 120)
	insight.Tags = cleanStringList(insight.Tags, 80)
	if insight.Entities == nil {
		insight.Entities = []map[string]any{}
	}
	if insight.Relationships == nil {
		insight.Relationships = []map[string]any{}
	}
	if insight.Style == nil {
		insight.Style = map[string]any{}
	}
	if insight.CandidateMemories == nil {
		insight.CandidateMemories = []MemoryCandidate{}
	}
	insight.Confidence = clampFloat(insight.Confidence, 0, 1)
	return insight
}

func normalizeAssetInsightCandidates(project, user []MemoryCandidate) []MemoryCandidate {
	out := make([]MemoryCandidate, 0, len(project)+len(user))
	for _, candidate := range project {
		candidate.Metadata = cloneMemoryMetadata(candidate.Metadata)
		candidate.Metadata["promotion_scope"] = "project"
		out = append(out, candidate)
	}
	for _, candidate := range user {
		candidate.Metadata = cloneMemoryMetadata(candidate.Metadata)
		candidate.Metadata["promotion_scope"] = "user"
		out = append(out, candidate)
	}
	return dedupeMemoryCandidates(out)
}

func cloneMemoryMetadata(metadata map[string]any) map[string]any {
	out := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func cleanStringList(items []string, limit int) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimPrefix(item, "."))
		if item == "" || seen[strings.ToLower(item)] {
			continue
		}
		seen[strings.ToLower(item)] = true
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func isSVGContentType(contentType, filename string) bool {
	return strings.EqualFold(normalizedContentType(contentType), "image/svg+xml") || strings.EqualFold(strings.ToLower(filepath.Ext(filename)), ".svg")
}

var svgTagPattern = regexp.MustCompile(`<[^>]+>`)

func svgPlainText(data []byte) string {
	text := strings.ToValidUTF8(string(data), " ")
	text = strings.ReplaceAll(text, "</text>", "\n")
	text = strings.ReplaceAll(text, "</title>", "\n")
	text = strings.ReplaceAll(text, "</desc>", "\n")
	text = svgTagPattern.ReplaceAllString(text, " ")
	text = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func truncateOneLine(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

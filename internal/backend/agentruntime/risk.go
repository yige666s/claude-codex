package agentruntime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
)

const (
	RiskLevelLow    = "low"
	RiskLevelMedium = "medium"
	RiskLevelHigh   = "high"

	RiskOperationAuthLogin        = "auth_login"
	RiskOperationAuthRegister     = "auth_register"
	RiskOperationAuthRefresh      = "auth_refresh"
	RiskOperationChatMessage      = "chat_message"
	RiskOperationJobCreate        = "job_create"
	RiskOperationAttachmentUpload = "attachment_upload"
	RiskOperationAssetDownload    = "asset_download"
	RiskOperationMemoryExtract    = "memory_extract"
	RiskOperationDeepAgentAction  = "deep_agent_action"
	RiskOperationLoopWebhook      = "loop_webhook"
	RiskOperationDataExport       = "data_export"
	RiskOperationAccountDelete    = "account_delete"
	RiskOperationAdminAction      = "admin_action"

	RiskReviewStatusPending   = "pending"
	RiskReviewStatusInReview  = "in_review"
	RiskReviewStatusResolved  = "resolved"
	RiskReviewStatusDismissed = "dismissed"
)

type OperationLimit struct {
	Limit  int
	Window time.Duration
}

type OperationRateLimiter struct {
	mu           sync.Mutex
	limits       map[string]OperationLimit
	defaultLimit OperationLimit
	hits         map[string][]time.Time
}

func NewOperationRateLimiter(limits map[string]OperationLimit) *OperationRateLimiter {
	merged := DefaultOperationLimits()
	for operation, limit := range limits {
		operation = strings.TrimSpace(operation)
		if operation == "" || limit.Limit <= 0 || limit.Window <= 0 {
			continue
		}
		merged[operation] = limit
	}
	return &OperationRateLimiter{
		limits:       merged,
		defaultLimit: OperationLimit{Limit: 120, Window: time.Minute},
		hits:         make(map[string][]time.Time),
	}
}

func DefaultOperationLimits() map[string]OperationLimit {
	return map[string]OperationLimit{
		RiskOperationAuthLogin:        {Limit: 20, Window: time.Minute},
		RiskOperationAuthRegister:     {Limit: 10, Window: time.Hour},
		RiskOperationAuthRefresh:      {Limit: 60, Window: time.Minute},
		RiskOperationChatMessage:      {Limit: 60, Window: time.Minute},
		RiskOperationJobCreate:        {Limit: 20, Window: time.Minute},
		RiskOperationAttachmentUpload: {Limit: 30, Window: time.Minute},
		RiskOperationAssetDownload:    {Limit: 120, Window: time.Minute},
		RiskOperationMemoryExtract:    {Limit: 30, Window: time.Hour},
		RiskOperationLoopWebhook:      {Limit: 120, Window: time.Minute},
		RiskOperationDataExport:       {Limit: 5, Window: time.Hour},
		RiskOperationAccountDelete:    {Limit: 3, Window: time.Hour},
		RiskOperationAdminAction:      {Limit: 120, Window: time.Minute},
	}
}

func (l *OperationRateLimiter) Allow(operation, key string) bool {
	if l == nil {
		return true
	}
	operation = strings.TrimSpace(operation)
	key = strings.TrimSpace(key)
	if operation == "" || key == "" {
		return true
	}
	limit, ok := l.limits[operation]
	if !ok {
		limit = l.defaultLimit
	}
	if limit.Limit <= 0 || limit.Window <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-limit.Window)
	bucket := operation + ":" + key
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.hits[bucket]
	kept := hits[:0]
	for _, hit := range hits {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= limit.Limit {
		l.hits[bucket] = kept
		return false
	}
	kept = append(kept, now)
	l.hits[bucket] = kept
	return true
}

type RiskEvent struct {
	ID         string         `json:"id"`
	UserID     string         `json:"user_id,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
	JobID      string         `json:"job_id,omitempty"`
	AssetID    string         `json:"asset_id,omitempty"`
	RequestID  string         `json:"request_id,omitempty"`
	IPAddress  string         `json:"ip_address,omitempty"`
	Operation  string         `json:"operation"`
	Reason     string         `json:"reason"`
	RiskLevel  string         `json:"risk_level"`
	ScoreDelta int            `json:"score_delta"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type RiskScore struct {
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	UserID      string    `json:"user_id,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	IPAddress   string    `json:"ip_address,omitempty"`
	Score       int       `json:"score"`
	RiskLevel   string    `json:"risk_level"`
	EventCount  int       `json:"event_count"`
	LastEventAt time.Time `json:"last_event_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RiskEventFilter struct {
	UserID    string
	SessionID string
	IPAddress string
	Operation string
	RiskLevel string
	Query     string
	Since     time.Time
	Limit     int
}

type RiskSummary struct {
	Since       time.Time       `json:"since"`
	Total       int             `json:"total"`
	HighRisk    int             `json:"high_risk"`
	MediumRisk  int             `json:"medium_risk"`
	LowRisk     int             `json:"low_risk"`
	ByOperation []AuditLogGroup `json:"by_operation"`
	ByRisk      []AuditLogGroup `json:"by_risk"`
	Events      []RiskEvent     `json:"events"`
	Scores      []RiskScore     `json:"scores"`
}

type RiskReviewItem struct {
	ID          string         `json:"id"`
	RiskEventID string         `json:"risk_event_id"`
	UserID      string         `json:"user_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	JobID       string         `json:"job_id,omitempty"`
	AssetID     string         `json:"asset_id,omitempty"`
	RequestID   string         `json:"request_id,omitempty"`
	IPAddress   string         `json:"ip_address,omitempty"`
	Operation   string         `json:"operation"`
	Reason      string         `json:"reason"`
	RiskLevel   string         `json:"risk_level"`
	Priority    string         `json:"priority"`
	Status      string         `json:"status"`
	AssignedTo  string         `json:"assigned_to,omitempty"`
	Resolution  string         `json:"resolution,omitempty"`
	Note        string         `json:"note,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	ResolvedAt  *time.Time     `json:"resolved_at,omitempty"`
}

type RiskReviewFilter struct {
	UserID    string
	Status    string
	RiskLevel string
	Operation string
	Query     string
	Since     time.Time
	Limit     int
}

type RiskReviewUpdate struct {
	Status     string
	AssignedTo string
	Resolution string
	Note       string
	ActorID    string
}

type RiskReviewSummary struct {
	Since     time.Time        `json:"since"`
	Total     int              `json:"total"`
	Pending   int              `json:"pending"`
	InReview  int              `json:"in_review"`
	Resolved  int              `json:"resolved"`
	Dismissed int              `json:"dismissed"`
	ByStatus  []AuditLogGroup  `json:"by_status"`
	Items     []RiskReviewItem `json:"items"`
}

type RiskStore interface {
	Init(context.Context) error
	RecordRiskEvent(context.Context, RiskEvent) error
	ListRiskEvents(context.Context, RiskEventFilter) (RiskSummary, error)
}

type RiskReviewStore interface {
	ListRiskReviews(context.Context, RiskReviewFilter) (RiskReviewSummary, error)
	UpdateRiskReview(context.Context, string, RiskReviewUpdate) (RiskReviewItem, error)
}

type RiskScanTarget struct {
	Kind        string
	UserID      string
	SessionID   string
	JobID       string
	AssetID     string
	Filename    string
	ContentType string
	Content     string
	Data        []byte
}

type RiskFinding struct {
	Category   string         `json:"category"`
	Reason     string         `json:"reason"`
	RiskLevel  string         `json:"risk_level"`
	ScoreDelta int            `json:"score_delta"`
	Snippet    string         `json:"snippet,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type RiskScanner interface {
	ScanRisk(context.Context, RiskScanTarget) []RiskFinding
}

type BasicRiskScanner struct{}

func NewBasicRiskScanner() BasicRiskScanner {
	return BasicRiskScanner{}
}

func (BasicRiskScanner) ScanRisk(_ context.Context, target RiskScanTarget) []RiskFinding {
	findings := make([]RiskFinding, 0)
	text := target.Content
	if text == "" && len(target.Data) > 0 && looksTextual(target.ContentType, target.Filename, target.Data) {
		text = string(target.Data)
	}
	lower := strings.ToLower(text)
	for _, pattern := range []struct {
		needle   string
		category string
		reason   string
		level    string
		score    int
	}{
		{"ignore previous instructions", "prompt_injection", "prompt injection phrase", RiskLevelMedium, 10},
		{"disregard previous instructions", "prompt_injection", "prompt injection phrase", RiskLevelMedium, 10},
		{"reveal your system prompt", "prompt_injection", "system prompt exfiltration request", RiskLevelMedium, 10},
		{"print the developer message", "prompt_injection", "developer instruction exfiltration request", RiskLevelMedium, 10},
		{"curl ", "malware_command", "network shell command pattern", RiskLevelLow, 3},
		{"powershell -enc", "malware_command", "encoded powershell command", RiskLevelHigh, 25},
		{"rm -rf /", "destructive_command", "destructive shell command", RiskLevelHigh, 25},
		{"-----begin private key-----", "secret_exposure", "private key material", RiskLevelHigh, 25},
		{"aws_secret_access_key", "secret_exposure", "AWS secret access key marker", RiskLevelHigh, 25},
		{"sk_live_", "secret_exposure", "live secret key marker", RiskLevelHigh, 25},
		{"ghp_", "secret_exposure", "GitHub personal access token marker", RiskLevelMedium, 10},
	} {
		if strings.Contains(lower, pattern.needle) {
			findings = append(findings, RiskFinding{
				Category:   pattern.category,
				Reason:     pattern.reason,
				RiskLevel:  pattern.level,
				ScoreDelta: pattern.score,
				Snippet:    snippetAround(text, pattern.needle),
			})
		}
	}
	filename := strings.ToLower(strings.TrimSpace(target.Filename))
	contentType := strings.ToLower(strings.TrimSpace(target.ContentType))
	for _, ext := range []string{".exe", ".dll", ".dmg", ".pkg", ".msi", ".scr", ".bat", ".cmd", ".ps1", ".apk"} {
		if strings.HasSuffix(filename, ext) {
			findings = append(findings, RiskFinding{
				Category:   "suspicious_file",
				Reason:     "potentially executable upload or artifact",
				RiskLevel:  RiskLevelHigh,
				ScoreDelta: 20,
				Metadata:   map[string]any{"extension": ext},
			})
			break
		}
	}
	for _, marker := range []string{"application/x-msdownload", "application/x-dosexec", "application/vnd.microsoft.portable-executable"} {
		if strings.Contains(contentType, marker) {
			findings = append(findings, RiskFinding{
				Category:   "suspicious_file",
				Reason:     "executable content type",
				RiskLevel:  RiskLevelHigh,
				ScoreDelta: 20,
				Metadata:   map[string]any{"content_type": target.ContentType},
			})
			break
		}
	}
	return dedupeRiskFindings(findings)
}

type MemoryRiskStore struct {
	mu            sync.Mutex
	events        []RiskEvent
	scores        map[string]RiskScore
	reviews       []RiskReviewItem
	reviewByEvent map[string]bool
}

func NewMemoryRiskStore() *MemoryRiskStore {
	return &MemoryRiskStore{scores: make(map[string]RiskScore), reviewByEvent: make(map[string]bool)}
}

func (s *MemoryRiskStore) Init(context.Context) error { return nil }

func (s *MemoryRiskStore) RecordRiskEvent(_ context.Context, event RiskEvent) error {
	if s == nil {
		return nil
	}
	event = normalizeRiskEvent(event)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	s.applyScoreLocked("user", event.UserID, event)
	s.applyScoreLocked("session", event.SessionID, event)
	s.applyScoreLocked("ip", event.IPAddress, event)
	s.createReviewLocked(event)
	return nil
}

func (s *MemoryRiskStore) createReviewLocked(event RiskEvent) {
	if !riskEventNeedsReview(event) || s.reviewByEvent[event.ID] {
		return
	}
	now := time.Now().UTC()
	review := reviewItemFromRiskEvent(event, now)
	s.reviews = append(s.reviews, review)
	s.reviewByEvent[event.ID] = true
}

func (s *MemoryRiskStore) applyScoreLocked(subjectType, subjectID string, event RiskEvent) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return
	}
	key := subjectType + ":" + subjectID
	score := s.scores[key]
	score.SubjectType = subjectType
	score.SubjectID = subjectID
	score.Score += event.ScoreDelta
	score.EventCount++
	score.LastEventAt = event.CreatedAt
	score.UpdatedAt = time.Now().UTC()
	score.RiskLevel = riskLevelForScore(score.Score)
	if subjectType == "user" {
		score.UserID = subjectID
	} else if subjectType == "session" {
		score.SessionID = subjectID
	} else if subjectType == "ip" {
		score.IPAddress = subjectID
	}
	s.scores[key] = score
}

func (s *MemoryRiskStore) ListRiskEvents(_ context.Context, filter RiskEventFilter) (RiskSummary, error) {
	if s == nil {
		return RiskSummary{}, nil
	}
	filter = normalizeRiskEventFilter(filter)
	s.mu.Lock()
	events := append([]RiskEvent(nil), s.events...)
	scores := make([]RiskScore, 0, len(s.scores))
	for _, score := range s.scores {
		scores = append(scores, score)
	}
	s.mu.Unlock()
	sort.Slice(events, func(i, j int) bool { return events[i].CreatedAt.After(events[j].CreatedAt) })
	return summarizeRiskEvents(events, scores, filter), nil
}

func (s *MemoryRiskStore) ListRiskReviews(_ context.Context, filter RiskReviewFilter) (RiskReviewSummary, error) {
	if s == nil {
		return RiskReviewSummary{}, nil
	}
	filter = normalizeRiskReviewFilter(filter)
	s.mu.Lock()
	items := append([]RiskReviewItem(nil), s.reviews...)
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return summarizeRiskReviews(items, filter), nil
}

func (s *MemoryRiskStore) UpdateRiskReview(_ context.Context, id string, update RiskReviewUpdate) (RiskReviewItem, error) {
	if s == nil {
		return RiskReviewItem{}, fmt.Errorf("risk review store is not configured")
	}
	update = normalizeRiskReviewUpdate(update)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.reviews {
		if s.reviews[i].ID != id {
			continue
		}
		applyRiskReviewUpdate(&s.reviews[i], update)
		return s.reviews[i], nil
	}
	return RiskReviewItem{}, sql.ErrNoRows
}

type SQLRiskStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

func NewSQLRiskStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLRiskStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLRiskStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLRiskStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql risk store is not configured")
	}
	if err := requireSQLColumns(ctx, s.db, "agent_risk_events",
		"id", "user_id", "session_id", "job_id", "asset_id", "request_id", "ip_address",
		"operation", "reason", "risk_level", "score_delta", "metadata", "created_at",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_risk_scores",
		"subject_type", "subject_id", "score", "risk_level", "event_count", "last_event_at", "updated_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_risk_reviews",
		"id", "risk_event_id", "user_id", "session_id", "job_id", "asset_id", "request_id", "ip_address",
		"operation", "reason", "risk_level", "priority", "status", "assigned_to", "resolution", "note",
		"metadata", "created_at", "updated_at", "resolved_at",
	)
}

func (s *SQLRiskStore) RecordRiskEvent(ctx context.Context, event RiskEvent) error {
	if s == nil || s.db == nil {
		return nil
	}
	event = normalizeRiskEvent(event)
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		if err := s.queries.InsertRiskEvent(ctx, dbsqlc.InsertRiskEventParams{
			ID:         event.ID,
			UserID:     sqlNullString(event.UserID),
			SessionID:  sqlNullString(event.SessionID),
			JobID:      sqlNullString(event.JobID),
			AssetID:    sqlNullString(event.AssetID),
			RequestID:  sqlNullString(event.RequestID),
			IpAddress:  sqlNullString(event.IPAddress),
			Operation:  event.Operation,
			Reason:     event.Reason,
			RiskLevel:  event.RiskLevel,
			ScoreDelta: int32(event.ScoreDelta),
			Metadata:   string(metadata),
			CreatedAt:  event.CreatedAt.UTC(),
		}); err != nil {
			return err
		}
		if err := s.upsertScore(ctx, "user", event.UserID, event); err != nil {
			return err
		}
		if err := s.upsertScore(ctx, "session", event.SessionID, event); err != nil {
			return err
		}
		if err := s.upsertScore(ctx, "ip", event.IPAddress, event); err != nil {
			return err
		}
		return s.createReview(ctx, event)
	}
	if _, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_risk_events (id, user_id, session_id, job_id, asset_id, request_id, ip_address, operation, reason, risk_level, score_delta, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		event.ID,
		nullableString(event.UserID),
		nullableString(event.SessionID),
		nullableString(event.JobID),
		nullableString(event.AssetID),
		nullableString(event.RequestID),
		nullableString(event.IPAddress),
		event.Operation,
		event.Reason,
		event.RiskLevel,
		event.ScoreDelta,
		string(metadata),
		sqlTimeValue(event.CreatedAt, s.dialect),
	); err != nil {
		return err
	}
	if err := s.upsertScore(ctx, "user", event.UserID, event); err != nil {
		return err
	}
	if err := s.upsertScore(ctx, "session", event.SessionID, event); err != nil {
		return err
	}
	if err := s.upsertScore(ctx, "ip", event.IPAddress, event); err != nil {
		return err
	}
	return s.createReview(ctx, event)
}

func (s *SQLRiskStore) createReview(ctx context.Context, event RiskEvent) error {
	if !riskEventNeedsReview(event) {
		return nil
	}
	review := reviewItemFromRiskEvent(event, time.Now().UTC())
	metadata, err := json.Marshal(review.Metadata)
	if err != nil {
		return err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		err = s.queries.InsertRiskReview(ctx, dbsqlc.InsertRiskReviewParams{
			ID:          review.ID,
			RiskEventID: review.RiskEventID,
			UserID:      sqlNullString(review.UserID),
			SessionID:   sqlNullString(review.SessionID),
			JobID:       sqlNullString(review.JobID),
			AssetID:     sqlNullString(review.AssetID),
			RequestID:   sqlNullString(review.RequestID),
			IpAddress:   sqlNullString(review.IPAddress),
			Operation:   review.Operation,
			Reason:      review.Reason,
			RiskLevel:   review.RiskLevel,
			Priority:    review.Priority,
			Status:      review.Status,
			AssignedTo:  sqlNullString(review.AssignedTo),
			Resolution:  sqlNullString(review.Resolution),
			Note:        sqlNullString(review.Note),
			Metadata:    string(metadata),
			CreatedAt:   review.CreatedAt.UTC(),
			UpdatedAt:   review.UpdatedAt.UTC(),
			ResolvedAt:  sqlNullTime(review.ResolvedAt),
		})
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil
		}
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_risk_reviews (id, risk_event_id, user_id, session_id, job_id, asset_id, request_id, ip_address, operation, reason, risk_level, priority, status, assigned_to, resolution, note, metadata, created_at, updated_at, resolved_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		review.ID,
		review.RiskEventID,
		nullableString(review.UserID),
		nullableString(review.SessionID),
		nullableString(review.JobID),
		nullableString(review.AssetID),
		nullableString(review.RequestID),
		nullableString(review.IPAddress),
		review.Operation,
		review.Reason,
		review.RiskLevel,
		review.Priority,
		review.Status,
		nullableString(review.AssignedTo),
		nullableString(review.Resolution),
		nullableString(review.Note),
		string(metadata),
		sqlTimeValue(review.CreatedAt, s.dialect),
		sqlTimeValue(review.UpdatedAt, s.dialect),
		nullableSQLTimeValue(review.ResolvedAt, s.dialect),
	)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return nil
	}
	return err
}

func (s *SQLRiskStore) upsertScore(ctx context.Context, subjectType, subjectID string, event RiskEvent) error {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil
	}
	now := time.Now().UTC()
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		scoreRow, err := s.queries.GetRiskScore(ctx, dbsqlc.GetRiskScoreParams{SubjectType: subjectType, SubjectID: subjectID})
		switch {
		case err == nil:
			score := int(scoreRow.Score) + event.ScoreDelta
			count := int(scoreRow.EventCount) + 1
			return s.queries.UpdateRiskScore(ctx, dbsqlc.UpdateRiskScoreParams{
				Score:       int32(score),
				RiskLevel:   riskLevelForScore(score),
				EventCount:  int32(count),
				LastEventAt: event.CreatedAt.UTC(),
				UpdatedAt:   now,
				SubjectType: subjectType,
				SubjectID:   subjectID,
			})
		case err == sql.ErrNoRows:
			score := event.ScoreDelta
			return s.queries.InsertRiskScore(ctx, dbsqlc.InsertRiskScoreParams{
				SubjectType: subjectType,
				SubjectID:   subjectID,
				Score:       int32(score),
				RiskLevel:   riskLevelForScore(score),
				EventCount:  1,
				LastEventAt: event.CreatedAt.UTC(),
				UpdatedAt:   now,
			})
		default:
			return err
		}
	}
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT score, event_count FROM agent_risk_scores WHERE subject_type = ? AND subject_id = ?`), subjectType, subjectID)
	var score, count int
	switch err := row.Scan(&score, &count); {
	case err == nil:
		score += event.ScoreDelta
		count++
		_, err = s.db.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_risk_scores SET score = ?, risk_level = ?, event_count = ?, last_event_at = ?, updated_at = ? WHERE subject_type = ? AND subject_id = ?`),
			score, riskLevelForScore(score), count, sqlTimeValue(event.CreatedAt, s.dialect), sqlTimeValue(now, s.dialect), subjectType, subjectID)
		return err
	case err == sql.ErrNoRows:
		score = event.ScoreDelta
		_, err = s.db.ExecContext(ctx, s.dialect.Bind(`INSERT INTO agent_risk_scores (subject_type, subject_id, score, risk_level, event_count, last_event_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`),
			subjectType, subjectID, score, riskLevelForScore(score), 1, sqlTimeValue(event.CreatedAt, s.dialect), sqlTimeValue(now, s.dialect))
		return err
	default:
		return err
	}
}

func (s *SQLRiskStore) ListRiskEvents(ctx context.Context, filter RiskEventFilter) (RiskSummary, error) {
	if s == nil || s.db == nil {
		return RiskSummary{}, fmt.Errorf("sql risk store is not configured")
	}
	filter = normalizeRiskEventFilter(filter)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		eventRows, err := s.queries.ListRiskEvents(ctx, dbsqlc.ListRiskEventsParams{
			Since:     filter.Since.UTC(),
			UserID:    filter.UserID,
			SessionID: filter.SessionID,
			IpAddress: filter.IPAddress,
			Operation: filter.Operation,
			RiskLevel: filter.RiskLevel,
		})
		if err != nil {
			return RiskSummary{}, err
		}
		subjectType, subjectID := riskScoreSubjectFilter(filter)
		scoreRows, err := s.queries.ListRiskScores(ctx, dbsqlc.ListRiskScoresParams{SubjectType: subjectType, SubjectID: subjectID})
		if err != nil {
			return RiskSummary{}, err
		}
		return summarizeRiskEvents(riskEventsFromSQLC(eventRows), riskScoresFromSQLC(scoreRows), filter), nil
	}
	query := `SELECT id, user_id, session_id, job_id, asset_id, request_id, ip_address, operation, reason, risk_level, score_delta, metadata, created_at FROM agent_risk_events WHERE created_at >= ?`
	args := []any{sqlTimeValue(filter.Since, s.dialect)}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if filter.IPAddress != "" {
		query += ` AND ip_address = ?`
		args = append(args, filter.IPAddress)
	}
	if filter.Operation != "" && filter.Operation != "all" {
		query += ` AND operation = ?`
		args = append(args, filter.Operation)
	}
	if filter.RiskLevel != "" && filter.RiskLevel != "all" {
		query += ` AND risk_level = ?`
		args = append(args, filter.RiskLevel)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return RiskSummary{}, err
	}
	defer rows.Close()
	events := make([]RiskEvent, 0)
	for rows.Next() {
		event, err := scanRiskEvent(rows)
		if err != nil {
			return RiskSummary{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return RiskSummary{}, err
	}
	scoreQuery := `SELECT subject_type, subject_id, score, risk_level, event_count, last_event_at, updated_at FROM agent_risk_scores`
	scoreArgs := []any{}
	if filter.UserID != "" {
		scoreQuery += ` WHERE subject_type = ? AND subject_id = ?`
		scoreArgs = append(scoreArgs, "user", filter.UserID)
	} else if filter.SessionID != "" {
		scoreQuery += ` WHERE subject_type = ? AND subject_id = ?`
		scoreArgs = append(scoreArgs, "session", filter.SessionID)
	} else if filter.IPAddress != "" {
		scoreQuery += ` WHERE subject_type = ? AND subject_id = ?`
		scoreArgs = append(scoreArgs, "ip", filter.IPAddress)
	}
	scoreQuery += ` ORDER BY score DESC, updated_at DESC LIMIT 100`
	scoreRows, err := s.db.QueryContext(ctx, s.dialect.Bind(scoreQuery), scoreArgs...)
	if err != nil {
		return RiskSummary{}, err
	}
	defer scoreRows.Close()
	scores := make([]RiskScore, 0)
	for scoreRows.Next() {
		score, err := scanRiskScore(scoreRows)
		if err != nil {
			return RiskSummary{}, err
		}
		scores = append(scores, score)
	}
	if err := scoreRows.Err(); err != nil {
		return RiskSummary{}, err
	}
	return summarizeRiskEvents(events, scores, filter), nil
}

func (s *SQLRiskStore) ListRiskReviews(ctx context.Context, filter RiskReviewFilter) (RiskReviewSummary, error) {
	if s == nil || s.db == nil {
		return RiskReviewSummary{}, fmt.Errorf("sql risk store is not configured")
	}
	filter = normalizeRiskReviewFilter(filter)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListRiskReviews(ctx, dbsqlc.ListRiskReviewsParams{
			Since:     filter.Since.UTC(),
			UserID:    filter.UserID,
			Status:    filter.Status,
			RiskLevel: filter.RiskLevel,
			Operation: filter.Operation,
		})
		if err != nil {
			return RiskReviewSummary{}, err
		}
		return summarizeRiskReviews(riskReviewsFromSQLC(rows), filter), nil
	}
	query := `SELECT id, risk_event_id, user_id, session_id, job_id, asset_id, request_id, ip_address, operation, reason, risk_level, priority, status, assigned_to, resolution, note, metadata, created_at, updated_at, resolved_at FROM agent_risk_reviews WHERE created_at >= ?`
	args := []any{sqlTimeValue(filter.Since, s.dialect)}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.Status != "" && filter.Status != "all" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.RiskLevel != "" && filter.RiskLevel != "all" {
		query += ` AND risk_level = ?`
		args = append(args, filter.RiskLevel)
	}
	if filter.Operation != "" && filter.Operation != "all" {
		query += ` AND operation = ?`
		args = append(args, filter.Operation)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return RiskReviewSummary{}, err
	}
	defer rows.Close()
	items := make([]RiskReviewItem, 0)
	for rows.Next() {
		item, err := scanRiskReviewItem(rows)
		if err != nil {
			return RiskReviewSummary{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return RiskReviewSummary{}, err
	}
	return summarizeRiskReviews(items, filter), nil
}

func (s *SQLRiskStore) UpdateRiskReview(ctx context.Context, id string, update RiskReviewUpdate) (RiskReviewItem, error) {
	if s == nil || s.db == nil {
		return RiskReviewItem{}, fmt.Errorf("sql risk store is not configured")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return RiskReviewItem{}, fmt.Errorf("risk review id is required")
	}
	update = normalizeRiskReviewUpdate(update)
	now := time.Now().UTC()
	var resolvedAt any
	if riskReviewStatusTerminal(update.Status) {
		resolvedAt = sqlTimeValue(now, s.dialect)
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		var resolved sql.NullTime
		if riskReviewStatusTerminal(update.Status) {
			resolved = sqlNullTime(&now)
		}
		rows, err := s.queries.UpdateRiskReview(ctx, dbsqlc.UpdateRiskReviewParams{
			Status:     update.Status,
			AssignedTo: sqlNullString(update.AssignedTo),
			Resolution: sqlNullString(update.Resolution),
			Note:       sqlNullString(update.Note),
			UpdatedAt:  now,
			ResolvedAt: resolved,
			ID:         id,
		})
		if err != nil {
			return RiskReviewItem{}, err
		}
		if rows == 0 {
			return RiskReviewItem{}, sql.ErrNoRows
		}
		return s.scanRiskReview(ctx, id)
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_risk_reviews
SET status = ?, assigned_to = ?, resolution = ?, note = ?, updated_at = ?, resolved_at = ?
WHERE id = ?`),
		update.Status,
		nullableString(update.AssignedTo),
		nullableString(update.Resolution),
		nullableString(update.Note),
		sqlTimeValue(now, s.dialect),
		resolvedAt,
		id,
	)
	if err != nil {
		return RiskReviewItem{}, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return RiskReviewItem{}, sql.ErrNoRows
	}
	return s.scanRiskReview(ctx, id)
}

func (s *SQLRiskStore) scanRiskReview(ctx context.Context, id string) (RiskReviewItem, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.GetRiskReview(ctx, id)
		if err != nil {
			return RiskReviewItem{}, err
		}
		return riskReviewFromSQLC(row), nil
	}
	return scanRiskReviewItem(s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT id, risk_event_id, user_id, session_id, job_id, asset_id, request_id, ip_address, operation, reason, risk_level, priority, status, assigned_to, resolution, note, metadata, created_at, updated_at, resolved_at FROM agent_risk_reviews WHERE id = ?`), id))
}

type riskEventScanner interface {
	Scan(dest ...any) error
}

func scanRiskEvent(row riskEventScanner) (RiskEvent, error) {
	var event RiskEvent
	var createdAt any
	var metadata string
	var userID, sessionID, jobID, assetID, requestID, ipAddress sql.NullString
	if err := row.Scan(
		&event.ID,
		&userID,
		&sessionID,
		&jobID,
		&assetID,
		&requestID,
		&ipAddress,
		&event.Operation,
		&event.Reason,
		&event.RiskLevel,
		&event.ScoreDelta,
		&metadata,
		&createdAt,
	); err != nil {
		return RiskEvent{}, err
	}
	event.UserID = userID.String
	event.SessionID = sessionID.String
	event.JobID = jobID.String
	event.AssetID = assetID.String
	event.RequestID = requestID.String
	event.IPAddress = ipAddress.String
	if strings.TrimSpace(metadata) != "" {
		_ = json.Unmarshal([]byte(metadata), &event.Metadata)
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	parsed, err := parseSQLTime(createdAt)
	if err != nil {
		return RiskEvent{}, err
	}
	event.CreatedAt = parsed
	return event, nil
}

func scanRiskScore(row riskEventScanner) (RiskScore, error) {
	var score RiskScore
	var lastEventAt, updatedAt any
	if err := row.Scan(&score.SubjectType, &score.SubjectID, &score.Score, &score.RiskLevel, &score.EventCount, &lastEventAt, &updatedAt); err != nil {
		return RiskScore{}, err
	}
	if score.SubjectType == "user" {
		score.UserID = score.SubjectID
	} else if score.SubjectType == "session" {
		score.SessionID = score.SubjectID
	} else if score.SubjectType == "ip" {
		score.IPAddress = score.SubjectID
	}
	parsed, err := parseSQLTime(lastEventAt)
	if err != nil {
		return RiskScore{}, err
	}
	score.LastEventAt = parsed
	parsed, err = parseSQLTime(updatedAt)
	if err != nil {
		return RiskScore{}, err
	}
	score.UpdatedAt = parsed
	return score, nil
}

func scanRiskReviewItem(row riskEventScanner) (RiskReviewItem, error) {
	var item RiskReviewItem
	var createdAt, updatedAt, resolvedAt any
	var metadata string
	var userID, sessionID, jobID, assetID, requestID, ipAddress, assignedTo, resolution, note sql.NullString
	if err := row.Scan(
		&item.ID,
		&item.RiskEventID,
		&userID,
		&sessionID,
		&jobID,
		&assetID,
		&requestID,
		&ipAddress,
		&item.Operation,
		&item.Reason,
		&item.RiskLevel,
		&item.Priority,
		&item.Status,
		&assignedTo,
		&resolution,
		&note,
		&metadata,
		&createdAt,
		&updatedAt,
		&resolvedAt,
	); err != nil {
		return RiskReviewItem{}, err
	}
	item.UserID = userID.String
	item.SessionID = sessionID.String
	item.JobID = jobID.String
	item.AssetID = assetID.String
	item.RequestID = requestID.String
	item.IPAddress = ipAddress.String
	item.AssignedTo = assignedTo.String
	item.Resolution = resolution.String
	item.Note = note.String
	if strings.TrimSpace(metadata) != "" {
		_ = json.Unmarshal([]byte(metadata), &item.Metadata)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	parsed, err := parseSQLTime(createdAt)
	if err != nil {
		return RiskReviewItem{}, err
	}
	item.CreatedAt = parsed
	parsed, err = parseSQLTime(updatedAt)
	if err != nil {
		return RiskReviewItem{}, err
	}
	item.UpdatedAt = parsed
	if parsed, err := parseNullableSQLTime(resolvedAt); err != nil {
		return RiskReviewItem{}, err
	} else {
		item.ResolvedAt = parsed
	}
	return item, nil
}

func riskEventFromSQLC(row dbsqlc.AgentRiskEvent) RiskEvent {
	event := RiskEvent{
		ID:         row.ID,
		UserID:     row.UserID.String,
		SessionID:  row.SessionID.String,
		JobID:      row.JobID.String,
		AssetID:    row.AssetID.String,
		RequestID:  row.RequestID.String,
		IPAddress:  row.IpAddress.String,
		Operation:  row.Operation,
		Reason:     row.Reason,
		RiskLevel:  row.RiskLevel,
		ScoreDelta: int(row.ScoreDelta),
		CreatedAt:  row.CreatedAt.UTC(),
	}
	if strings.TrimSpace(row.Metadata) != "" {
		_ = json.Unmarshal([]byte(row.Metadata), &event.Metadata)
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	return event
}

func riskEventsFromSQLC(rows []dbsqlc.AgentRiskEvent) []RiskEvent {
	out := make([]RiskEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, riskEventFromSQLC(row))
	}
	return out
}

func riskScoreFromSQLC(row dbsqlc.AgentRiskScore) RiskScore {
	score := RiskScore{
		SubjectType: row.SubjectType,
		SubjectID:   row.SubjectID,
		Score:       int(row.Score),
		RiskLevel:   row.RiskLevel,
		EventCount:  int(row.EventCount),
		LastEventAt: row.LastEventAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
	if score.SubjectType == "user" {
		score.UserID = score.SubjectID
	} else if score.SubjectType == "session" {
		score.SessionID = score.SubjectID
	} else if score.SubjectType == "ip" {
		score.IPAddress = score.SubjectID
	}
	return score
}

func riskScoresFromSQLC(rows []dbsqlc.AgentRiskScore) []RiskScore {
	out := make([]RiskScore, 0, len(rows))
	for _, row := range rows {
		out = append(out, riskScoreFromSQLC(row))
	}
	return out
}

func riskReviewFromSQLC(row dbsqlc.AgentRiskReview) RiskReviewItem {
	item := RiskReviewItem{
		ID:          row.ID,
		RiskEventID: row.RiskEventID,
		UserID:      row.UserID.String,
		SessionID:   row.SessionID.String,
		JobID:       row.JobID.String,
		AssetID:     row.AssetID.String,
		RequestID:   row.RequestID.String,
		IPAddress:   row.IpAddress.String,
		Operation:   row.Operation,
		Reason:      row.Reason,
		RiskLevel:   row.RiskLevel,
		Priority:    row.Priority,
		Status:      row.Status,
		AssignedTo:  row.AssignedTo.String,
		Resolution:  row.Resolution.String,
		Note:        row.Note.String,
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
		ResolvedAt:  timeFromNull(row.ResolvedAt),
	}
	if strings.TrimSpace(row.Metadata) != "" {
		_ = json.Unmarshal([]byte(row.Metadata), &item.Metadata)
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item
}

func riskReviewsFromSQLC(rows []dbsqlc.AgentRiskReview) []RiskReviewItem {
	out := make([]RiskReviewItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, riskReviewFromSQLC(row))
	}
	return out
}

func riskScoreSubjectFilter(filter RiskEventFilter) (string, string) {
	switch {
	case filter.UserID != "":
		return "user", filter.UserID
	case filter.SessionID != "":
		return "session", filter.SessionID
	case filter.IPAddress != "":
		return "ip", filter.IPAddress
	default:
		return "", ""
	}
}

func normalizeRiskEvent(event RiskEvent) RiskEvent {
	event.ID = strings.TrimSpace(event.ID)
	if event.ID == "" {
		event.ID = newRiskEventID()
	}
	event.Operation = strings.TrimSpace(event.Operation)
	if event.Operation == "" {
		event.Operation = "unknown"
	}
	event.Reason = strings.TrimSpace(event.Reason)
	if event.Reason == "" {
		event.Reason = "policy_triggered"
	}
	event.RiskLevel = strings.ToLower(strings.TrimSpace(event.RiskLevel))
	if event.RiskLevel == "" {
		event.RiskLevel = riskLevelForScore(event.ScoreDelta)
	}
	if event.ScoreDelta <= 0 {
		event.ScoreDelta = riskScoreDelta(event.RiskLevel)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	return event
}

func normalizeRiskEventFilter(filter RiskEventFilter) RiskEventFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.IPAddress = strings.TrimSpace(filter.IPAddress)
	filter.Operation = strings.TrimSpace(filter.Operation)
	filter.RiskLevel = strings.ToLower(strings.TrimSpace(filter.RiskLevel))
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	if filter.Since.IsZero() {
		filter.Since = time.Now().UTC().Add(-24 * time.Hour)
	}
	if filter.Limit <= 0 || filter.Limit > 1000 {
		filter.Limit = 200
	}
	return filter
}

func normalizeRiskReviewFilter(filter RiskReviewFilter) RiskReviewFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.Status = normalizeRiskReviewStatus(filter.Status)
	filter.RiskLevel = strings.ToLower(strings.TrimSpace(filter.RiskLevel))
	filter.Operation = strings.TrimSpace(filter.Operation)
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	if filter.Since.IsZero() {
		filter.Since = time.Now().UTC().Add(-7 * 24 * time.Hour)
	}
	if filter.Limit <= 0 || filter.Limit > 1000 {
		filter.Limit = 200
	}
	return filter
}

func normalizeRiskReviewUpdate(update RiskReviewUpdate) RiskReviewUpdate {
	update.Status = normalizeRiskReviewStatus(update.Status)
	if update.Status == "" || update.Status == "all" {
		update.Status = RiskReviewStatusInReview
	}
	update.AssignedTo = strings.TrimSpace(update.AssignedTo)
	update.Resolution = strings.TrimSpace(update.Resolution)
	update.Note = strings.TrimSpace(update.Note)
	update.ActorID = strings.TrimSpace(update.ActorID)
	return update
}

func normalizeRiskReviewStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "all":
		return strings.ToLower(strings.TrimSpace(status))
	case RiskReviewStatusPending:
		return RiskReviewStatusPending
	case RiskReviewStatusInReview, "reviewing", "in-review":
		return RiskReviewStatusInReview
	case RiskReviewStatusResolved, "resolve":
		return RiskReviewStatusResolved
	case RiskReviewStatusDismissed, "dismiss":
		return RiskReviewStatusDismissed
	default:
		return RiskReviewStatusInReview
	}
}

func riskReviewStatusTerminal(status string) bool {
	return status == RiskReviewStatusResolved || status == RiskReviewStatusDismissed
}

func riskEventNeedsReview(event RiskEvent) bool {
	if event.RiskLevel == RiskLevelHigh {
		return true
	}
	category, _ := event.Metadata["category"].(string)
	category = strings.ToLower(strings.TrimSpace(category))
	return strings.Contains(category, "suspicious_file") || strings.Contains(category, "secret_exposure")
}

func reviewItemFromRiskEvent(event RiskEvent, now time.Time) RiskReviewItem {
	return RiskReviewItem{
		ID:          newRiskReviewID(),
		RiskEventID: event.ID,
		UserID:      event.UserID,
		SessionID:   event.SessionID,
		JobID:       event.JobID,
		AssetID:     event.AssetID,
		RequestID:   event.RequestID,
		IPAddress:   event.IPAddress,
		Operation:   event.Operation,
		Reason:      event.Reason,
		RiskLevel:   event.RiskLevel,
		Priority:    reviewPriorityForRiskEvent(event),
		Status:      RiskReviewStatusPending,
		Metadata:    copyAnyMap(event.Metadata),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func reviewPriorityForRiskEvent(event RiskEvent) string {
	if event.RiskLevel == RiskLevelHigh || event.ScoreDelta >= 20 {
		return RiskLevelHigh
	}
	if event.RiskLevel == RiskLevelMedium || event.ScoreDelta >= 10 {
		return RiskLevelMedium
	}
	return RiskLevelLow
}

func applyRiskReviewUpdate(item *RiskReviewItem, update RiskReviewUpdate) {
	now := time.Now().UTC()
	item.Status = update.Status
	item.AssignedTo = update.AssignedTo
	item.Resolution = update.Resolution
	item.Note = update.Note
	item.UpdatedAt = now
	if riskReviewStatusTerminal(update.Status) {
		item.ResolvedAt = &now
	} else {
		item.ResolvedAt = nil
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	if update.ActorID != "" {
		item.Metadata["last_actor_id"] = update.ActorID
	}
}

func summarizeRiskEvents(events []RiskEvent, scores []RiskScore, filter RiskEventFilter) RiskSummary {
	filter = normalizeRiskEventFilter(filter)
	summary := RiskSummary{
		Since:  filter.Since,
		Events: make([]RiskEvent, 0, minInt(len(events), filter.Limit)),
	}
	operationCounts := map[string]int{}
	riskCounts := map[string]int{}
	for _, event := range events {
		if event.CreatedAt.Before(filter.Since) {
			continue
		}
		if filter.UserID != "" && event.UserID != filter.UserID {
			continue
		}
		if filter.SessionID != "" && event.SessionID != filter.SessionID {
			continue
		}
		if filter.IPAddress != "" && event.IPAddress != filter.IPAddress {
			continue
		}
		if filter.Operation != "" && filter.Operation != "all" && event.Operation != filter.Operation {
			continue
		}
		if filter.RiskLevel != "" && filter.RiskLevel != "all" && event.RiskLevel != filter.RiskLevel {
			continue
		}
		if filter.Query != "" && !riskEventMatchesQuery(event, filter.Query) {
			continue
		}
		summary.Total++
		operationCounts[event.Operation]++
		riskCounts[event.RiskLevel]++
		switch event.RiskLevel {
		case RiskLevelHigh:
			summary.HighRisk++
		case RiskLevelMedium:
			summary.MediumRisk++
		default:
			summary.LowRisk++
		}
		if len(summary.Events) < filter.Limit {
			summary.Events = append(summary.Events, event)
		}
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Score == scores[j].Score {
			return scores[i].UpdatedAt.After(scores[j].UpdatedAt)
		}
		return scores[i].Score > scores[j].Score
	})
	for _, score := range scores {
		if filter.UserID != "" && !(score.SubjectType == "user" && score.SubjectID == filter.UserID) {
			continue
		}
		if filter.SessionID != "" && !(score.SubjectType == "session" && score.SubjectID == filter.SessionID) {
			continue
		}
		if filter.IPAddress != "" && !(score.SubjectType == "ip" && score.SubjectID == filter.IPAddress) {
			continue
		}
		summary.Scores = append(summary.Scores, score)
		if len(summary.Scores) >= 100 {
			break
		}
	}
	summary.ByOperation = auditGroups(operationCounts)
	summary.ByRisk = auditGroups(riskCounts)
	return summary
}

func summarizeRiskReviews(items []RiskReviewItem, filter RiskReviewFilter) RiskReviewSummary {
	filter = normalizeRiskReviewFilter(filter)
	summary := RiskReviewSummary{
		Since: filter.Since,
		Items: make([]RiskReviewItem, 0, minInt(len(items), filter.Limit)),
	}
	statusCounts := map[string]int{}
	for _, item := range items {
		if item.CreatedAt.Before(filter.Since) {
			continue
		}
		if filter.UserID != "" && item.UserID != filter.UserID {
			continue
		}
		if filter.Status != "" && filter.Status != "all" && item.Status != filter.Status {
			continue
		}
		if filter.RiskLevel != "" && filter.RiskLevel != "all" && item.RiskLevel != filter.RiskLevel {
			continue
		}
		if filter.Operation != "" && filter.Operation != "all" && item.Operation != filter.Operation {
			continue
		}
		if filter.Query != "" && !riskReviewMatchesQuery(item, filter.Query) {
			continue
		}
		summary.Total++
		statusCounts[item.Status]++
		switch item.Status {
		case RiskReviewStatusPending:
			summary.Pending++
		case RiskReviewStatusInReview:
			summary.InReview++
		case RiskReviewStatusResolved:
			summary.Resolved++
		case RiskReviewStatusDismissed:
			summary.Dismissed++
		}
		if len(summary.Items) < filter.Limit {
			summary.Items = append(summary.Items, item)
		}
	}
	summary.ByStatus = auditGroups(statusCounts)
	return summary
}

func riskReviewMatchesQuery(item RiskReviewItem, query string) bool {
	values := []string{item.ID, item.RiskEventID, item.UserID, item.SessionID, item.JobID, item.AssetID, item.RequestID, item.IPAddress, item.Operation, item.Reason, item.RiskLevel, item.Status, item.AssignedTo, item.Resolution, item.Note}
	if len(item.Metadata) > 0 {
		if data, err := json.Marshal(item.Metadata); err == nil {
			values = append(values, string(data))
		}
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func riskEventMatchesQuery(event RiskEvent, query string) bool {
	values := []string{event.ID, event.UserID, event.SessionID, event.JobID, event.AssetID, event.RequestID, event.IPAddress, event.Operation, event.Reason, event.RiskLevel}
	if len(event.Metadata) > 0 {
		if data, err := json.Marshal(event.Metadata); err == nil {
			values = append(values, string(data))
		}
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func looksTextual(contentType, filename string, data []byte) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") || strings.Contains(contentType, "yaml") || strings.Contains(contentType, "markdown") {
		return true
	}
	switch strings.ToLower(filepathExt(filename)) {
	case ".txt", ".md", ".json", ".yaml", ".yml", ".xml", ".csv", ".log", ".go", ".js", ".ts", ".tsx", ".py", ".sh", ".html", ".css":
		return true
	}
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	if len(sample) == 0 {
		return false
	}
	control := 0
	for _, b := range sample {
		if b == 0 {
			return false
		}
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			control++
		}
	}
	return control*20 < len(sample)
}

func filepathExt(filename string) string {
	idx := strings.LastIndex(filename, ".")
	if idx < 0 {
		return ""
	}
	return filename[idx:]
}

func snippetAround(text, needle string) string {
	if text == "" || needle == "" {
		return ""
	}
	lower := strings.ToLower(text)
	idx := strings.Index(lower, strings.ToLower(needle))
	if idx < 0 {
		return ""
	}
	start := idx - 40
	if start < 0 {
		start = 0
	}
	end := idx + len(needle) + 40
	if end > len(text) {
		end = len(text)
	}
	return strings.TrimSpace(text[start:end])
}

func dedupeRiskFindings(findings []RiskFinding) []RiskFinding {
	out := make([]RiskFinding, 0, len(findings))
	seen := map[string]bool{}
	for _, finding := range findings {
		finding.Category = strings.TrimSpace(finding.Category)
		finding.Reason = strings.TrimSpace(finding.Reason)
		finding.RiskLevel = strings.ToLower(strings.TrimSpace(finding.RiskLevel))
		if finding.RiskLevel == "" {
			finding.RiskLevel = riskLevelForScore(finding.ScoreDelta)
		}
		if finding.ScoreDelta <= 0 {
			finding.ScoreDelta = riskScoreDelta(finding.RiskLevel)
		}
		key := finding.Category + ":" + finding.Reason + ":" + finding.RiskLevel
		if finding.Category == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, finding)
	}
	return out
}

func riskLevelForScore(score int) string {
	switch {
	case score >= 50:
		return RiskLevelHigh
	case score >= 20:
		return RiskLevelMedium
	default:
		return RiskLevelLow
	}
}

func riskScoreDelta(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case RiskLevelHigh:
		return 25
	case RiskLevelMedium:
		return 10
	default:
		return 3
	}
}

func newRiskEventID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(data[:])
}

func newRiskReviewID() string {
	return "rrv-" + newRiskEventID()
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (s *Server) SetRiskStore(store RiskStore) {
	if s == nil {
		return
	}
	s.risk = store
}

func (s *Server) SetRiskScanner(scanner RiskScanner) {
	if s == nil {
		return
	}
	s.riskScanner = scanner
}

func (s *Server) SetOperationRateLimiter(limiter *OperationRateLimiter) {
	if s == nil {
		return
	}
	s.operationLimiter = limiter
}

func publicAuthRiskOperation(method, path string) string {
	if method != http.MethodPost {
		return ""
	}
	switch path {
	case "v1/auth/login":
		return RiskOperationAuthLogin
	case "v1/auth/register":
		return RiskOperationAuthRegister
	case "v1/auth/refresh":
		return RiskOperationAuthRefresh
	default:
		return ""
	}
}

func classifyRiskOperation(method, path string, parts []string) string {
	switch {
	case path == "v1/admin/users" || strings.HasPrefix(path, "v1/admin/users/") ||
		path == "v1/admin/ops" || strings.HasPrefix(path, "v1/admin/ops/") ||
		path == "v1/admin/skills" || strings.HasPrefix(path, "v1/admin/skills/"):
		return RiskOperationAdminAction
	case method == http.MethodPost && path == "v1/jobs":
		return RiskOperationJobCreate
	case method == http.MethodPost && (path == "v1/attachments" || path == "v1/attachments/presign" ||
		(len(parts) == 4 && parts[0] == "v1" && parts[1] == "attachments" && parts[3] == "confirm")):
		return RiskOperationAttachmentUpload
	case method == http.MethodPost && len(parts) == 4 && parts[0] == "v1" && parts[1] == "sessions" && parts[3] == "messages":
		return RiskOperationChatMessage
	case method == http.MethodGet && len(parts) == 3 && parts[0] == "v1" && (parts[1] == "attachments" || parts[1] == "artifacts"):
		return RiskOperationAssetDownload
	case method == http.MethodPost && len(parts) == 5 && parts[0] == "v1" && (parts[1] == "attachments" || parts[1] == "artifacts") && parts[3] == "memory" && parts[4] == "extract":
		return RiskOperationMemoryExtract
	case method == http.MethodGet && path == "v1/data/export":
		return RiskOperationDataExport
	case method == http.MethodDelete && path == "v1/account":
		return RiskOperationAccountDelete
	default:
		return ""
	}
}

func (s *Server) allowPublicOperation(w http.ResponseWriter, r *http.Request, operation string) bool {
	key := "ip:" + clientIP(r)
	if s == nil || s.operationLimiter == nil || s.operationLimiter.Allow(operation, key) {
		return true
	}
	if s.metrics != nil {
		s.metrics.IncRateLimited()
	}
	s.recordRiskEvent(r, RiskEvent{
		IPAddress:  clientIP(r),
		Operation:  operation,
		Reason:     "operation_rate_limit",
		RiskLevel:  RiskLevelMedium,
		ScoreDelta: 10,
		Metadata:   map[string]any{"key": key},
	})
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
	return false
}

func (s *Server) allowUserOperation(w http.ResponseWriter, r *http.Request, user User, operation string) bool {
	key := "user:" + user.ID
	if operation == RiskOperationAuthLogin || operation == RiskOperationAuthRegister || operation == RiskOperationAuthRefresh {
		key = "ip:" + clientIP(r)
	}
	if s == nil || s.operationLimiter == nil || s.operationLimiter.Allow(operation, key) {
		return true
	}
	if s.metrics != nil {
		s.metrics.IncRateLimited()
	}
	s.recordRiskEvent(r, RiskEvent{
		UserID:     user.ID,
		SessionID:  sessionIDFromRiskPath(strings.Trim(r.URL.Path, "/")),
		IPAddress:  clientIP(r),
		Operation:  operation,
		Reason:     "operation_rate_limit",
		RiskLevel:  RiskLevelMedium,
		ScoreDelta: 10,
		Metadata:   map[string]any{"key": key},
	})
	s.auditEvent(r, "risk_rate_limited", user, map[string]any{"operation": operation})
	writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limited"})
	return false
}

func sessionIDFromRiskPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "sessions" {
		return parts[2]
	}
	return ""
}

func (s *Server) recordRiskEvent(r *http.Request, event RiskEvent) {
	if s == nil || s.risk == nil {
		return
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
		event.RequestID = requestIDFromContext(r.Context())
		if event.IPAddress == "" {
			event.IPAddress = clientIP(r)
		}
	}
	if err := s.risk.RecordRiskEvent(ctx, event); err != nil {
		if s.metrics != nil {
			s.metrics.IncAuditError()
		}
		s.logEvent("risk_store_error", map[string]any{"operation": event.Operation, "error": err.Error(), "request_id": event.RequestID})
	}
}

func (s *Server) scanAndRecordRisk(r *http.Request, target RiskScanTarget) []RiskFinding {
	if s == nil || s.riskScanner == nil {
		return nil
	}
	findings := s.riskScanner.ScanRisk(r.Context(), target)
	for _, finding := range findings {
		metadata := map[string]any{
			"category": finding.Category,
			"snippet":  finding.Snippet,
			"target":   target.Kind,
		}
		for key, value := range finding.Metadata {
			metadata[key] = value
		}
		if target.Filename != "" {
			metadata["filename"] = target.Filename
		}
		if target.ContentType != "" {
			metadata["content_type"] = target.ContentType
		}
		if len(target.Data) > 0 {
			metadata["size_bytes"] = len(target.Data)
		}
		s.recordRiskEvent(r, RiskEvent{
			UserID:     target.UserID,
			SessionID:  target.SessionID,
			JobID:      target.JobID,
			AssetID:    target.AssetID,
			Operation:  riskOperationForScanTarget(target.Kind),
			Reason:     finding.Reason,
			RiskLevel:  finding.RiskLevel,
			ScoreDelta: finding.ScoreDelta,
			Metadata:   metadata,
		})
	}
	return findings
}

func riskOperationForScanTarget(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "prompt", "job_prompt":
		return "content_scan"
	case AssetKindAttachment:
		return "upload_scan"
	case AssetKindArtifact:
		return "artifact_scan"
	default:
		return "content_scan"
	}
}

func executionDenialFinding(err error) (RiskFinding, bool) {
	if err == nil {
		return RiskFinding{}, false
	}
	text := strings.TrimSpace(err.Error())
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "web sandbox denied command") || strings.Contains(lower, "docker skill shell runtime does not support") || strings.Contains(lower, "not declared for this skill"):
		return RiskFinding{Category: "sandbox_denied", Reason: "sandbox command denied", RiskLevel: RiskLevelMedium, ScoreDelta: 10, Snippet: trimRiskSnippet(text)}, true
	case strings.Contains(lower, "tool is not enabled for this runtime scope") || strings.Contains(lower, "write and execute tools are disabled by the product policy") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "blocked in plan mode") || strings.Contains(lower, "needs ") && strings.Contains(lower, " permission"):
		return RiskFinding{Category: "tool_denied", Reason: "tool permission denied", RiskLevel: RiskLevelMedium, ScoreDelta: 10, Snippet: trimRiskSnippet(text)}, true
	case strings.Contains(lower, "network is unreachable") || strings.Contains(lower, "temporary failure in name resolution") || strings.Contains(lower, "could not resolve host") || strings.Contains(lower, "name or service not known"):
		return RiskFinding{Category: "sandbox_egress_denied", Reason: "sandbox network egress denied or unavailable", RiskLevel: RiskLevelMedium, ScoreDelta: 10, Snippet: trimRiskSnippet(text)}, true
	case strings.Contains(lower, "docker skill shell command timed out") || strings.Contains(lower, "output exceeds max size") || strings.Contains(lower, "exceeds max size"):
		return RiskFinding{Category: "sandbox_limit", Reason: "sandbox resource limit exceeded", RiskLevel: RiskLevelLow, ScoreDelta: 5, Snippet: trimRiskSnippet(text)}, true
	default:
		return RiskFinding{}, false
	}
}

func trimRiskSnippet(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 240 {
		return text
	}
	return strings.TrimSpace(text[:240])
}

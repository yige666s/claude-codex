package agentruntime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type AuditRecord struct {
	ID        string         `json:"id"`
	Event     string         `json:"event"`
	UserID    string         `json:"user_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	AssetID   string         `json:"asset_id,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	IPAddress string         `json:"ip_address,omitempty"`
	UserAgent string         `json:"user_agent,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	RiskLevel string         `json:"risk_level,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type AuditLogger interface {
	Init(context.Context) error
	Record(context.Context, AuditRecord) error
}

type AuditLogFilter struct {
	UserID    string
	Event     string
	RiskLevel string
	Query     string
	Since     time.Time
	Limit     int
}

type AuditLogGroup struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type AuditLogSummary struct {
	Since      time.Time       `json:"since"`
	Total      int             `json:"total"`
	HighRisk   int             `json:"high_risk"`
	MediumRisk int             `json:"medium_risk"`
	LowRisk    int             `json:"low_risk"`
	ByEvent    []AuditLogGroup `json:"by_event"`
	ByRisk     []AuditLogGroup `json:"by_risk"`
	Records    []AuditRecord   `json:"records"`
}

type AuditLogStore interface {
	AuditLogger
	ListAuditRecords(context.Context, AuditLogFilter) (AuditLogSummary, error)
}

type MemoryAuditLogger struct {
	mu      sync.Mutex
	Records []AuditRecord
}

func NewMemoryAuditLogger() *MemoryAuditLogger {
	return &MemoryAuditLogger{}
}

func (l *MemoryAuditLogger) Init(context.Context) error { return nil }

func (l *MemoryAuditLogger) Record(_ context.Context, record AuditRecord) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Records = append(l.Records, record)
	return nil
}

func (l *MemoryAuditLogger) ListAuditRecords(_ context.Context, filter AuditLogFilter) (AuditLogSummary, error) {
	if l == nil {
		return AuditLogSummary{}, nil
	}
	filter = normalizeAuditLogFilter(filter)
	l.mu.Lock()
	records := append([]AuditRecord(nil), l.Records...)
	l.mu.Unlock()
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return summarizeAuditRecords(records, filter), nil
}

type SQLAuditLogger struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLAuditLoggerWithDialect(db *sql.DB, dialect SQLDialect) *SQLAuditLogger {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLAuditLogger{db: db, dialect: dialect}
}

func (l *SQLAuditLogger) Init(ctx context.Context) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("sql audit logger is not configured")
	}
	timeType := l.dialect.TimeType()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS agent_audit_logs (
	id TEXT PRIMARY KEY,
	event TEXT NOT NULL,
	user_id TEXT,
	session_id TEXT,
	job_id TEXT,
	asset_id TEXT,
	request_id TEXT,
	ip_address TEXT,
	user_agent TEXT,
	metadata TEXT NOT NULL,
	created_at ` + timeType + ` NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_audit_logs_user_created ON agent_audit_logs (user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_audit_logs_event_created ON agent_audit_logs (event, created_at)`,
	} {
		if _, err := l.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return ensureReadableTimeColumns(ctx, l.db, l.dialect, "agent_audit_logs", "created_at")
}

func (l *SQLAuditLogger) Record(ctx context.Context, record AuditRecord) error {
	if l == nil || l.db == nil {
		return nil
	}
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	_, err = l.db.ExecContext(ctx, l.dialect.Bind(`
INSERT INTO agent_audit_logs (id, event, user_id, session_id, job_id, asset_id, request_id, ip_address, user_agent, metadata, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.Event,
		nullableString(record.UserID),
		nullableString(record.SessionID),
		nullableString(record.JobID),
		nullableString(record.AssetID),
		nullableString(record.RequestID),
		nullableString(record.IPAddress),
		nullableString(record.UserAgent),
		string(metadata),
		sqlTimeValue(record.CreatedAt, l.dialect),
	)
	return err
}

func (l *SQLAuditLogger) ListAuditRecords(ctx context.Context, filter AuditLogFilter) (AuditLogSummary, error) {
	if l == nil || l.db == nil {
		return AuditLogSummary{}, fmt.Errorf("sql audit logger is not configured")
	}
	filter = normalizeAuditLogFilter(filter)
	query := `SELECT id, event, user_id, session_id, job_id, asset_id, request_id, ip_address, user_agent, metadata, created_at FROM agent_audit_logs WHERE created_at >= ?`
	args := []any{sqlTimeValue(filter.Since, l.dialect)}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.Event != "" && filter.Event != "all" {
		query += ` AND event = ?`
		args = append(args, filter.Event)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := l.db.QueryContext(ctx, l.dialect.Bind(query), args...)
	if err != nil {
		return AuditLogSummary{}, err
	}
	defer rows.Close()
	records := make([]AuditRecord, 0)
	for rows.Next() {
		record, err := scanAuditRecord(rows)
		if err != nil {
			return AuditLogSummary{}, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return AuditLogSummary{}, err
	}
	return summarizeAuditRecords(records, filter), nil
}

type auditRecordScanner interface {
	Scan(dest ...any) error
}

func scanAuditRecord(row auditRecordScanner) (AuditRecord, error) {
	var record AuditRecord
	var createdAt any
	var metadata string
	var userID, sessionID, jobID, assetID, requestID, ipAddress, userAgent sql.NullString
	if err := row.Scan(
		&record.ID,
		&record.Event,
		&userID,
		&sessionID,
		&jobID,
		&assetID,
		&requestID,
		&ipAddress,
		&userAgent,
		&metadata,
		&createdAt,
	); err != nil {
		return AuditRecord{}, err
	}
	record.UserID = userID.String
	record.SessionID = sessionID.String
	record.JobID = jobID.String
	record.AssetID = assetID.String
	record.RequestID = requestID.String
	record.IPAddress = ipAddress.String
	record.UserAgent = userAgent.String
	if strings.TrimSpace(metadata) != "" {
		_ = json.Unmarshal([]byte(metadata), &record.Metadata)
	}
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	parsed, err := parseSQLTime(createdAt)
	if err != nil {
		return AuditRecord{}, err
	}
	record.CreatedAt = parsed
	record.RiskLevel = auditRiskLevel(record.Event)
	return record, nil
}

func normalizeAuditLogFilter(filter AuditLogFilter) AuditLogFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.Event = strings.TrimSpace(filter.Event)
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

func summarizeAuditRecords(records []AuditRecord, filter AuditLogFilter) AuditLogSummary {
	filter = normalizeAuditLogFilter(filter)
	summary := AuditLogSummary{
		Since:   filter.Since,
		Records: make([]AuditRecord, 0, minInt(len(records), filter.Limit)),
	}
	eventCounts := map[string]int{}
	riskCounts := map[string]int{}
	for _, record := range records {
		record.RiskLevel = auditRiskLevel(record.Event)
		if record.CreatedAt.Before(filter.Since) {
			continue
		}
		if filter.UserID != "" && record.UserID != filter.UserID {
			continue
		}
		if filter.Event != "" && filter.Event != "all" && record.Event != filter.Event {
			continue
		}
		if filter.RiskLevel != "" && filter.RiskLevel != "all" && record.RiskLevel != filter.RiskLevel {
			continue
		}
		if filter.Query != "" && !auditRecordMatchesQuery(record, filter.Query) {
			continue
		}
		summary.Total++
		eventCounts[record.Event]++
		riskCounts[record.RiskLevel]++
		switch record.RiskLevel {
		case "high":
			summary.HighRisk++
		case "medium":
			summary.MediumRisk++
		default:
			summary.LowRisk++
		}
		if len(summary.Records) < filter.Limit {
			summary.Records = append(summary.Records, record)
		}
	}
	summary.ByEvent = auditGroups(eventCounts)
	summary.ByRisk = auditGroups(riskCounts)
	return summary
}

func auditGroups(counts map[string]int) []AuditLogGroup {
	groups := make([]AuditLogGroup, 0, len(counts))
	for key, count := range counts {
		groups = append(groups, AuditLogGroup{Key: key, Count: count})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count == groups[j].Count {
			return groups[i].Key < groups[j].Key
		}
		return groups[i].Count > groups[j].Count
	})
	return groups
}

func auditRecordMatchesQuery(record AuditRecord, query string) bool {
	values := []string{
		record.ID,
		record.Event,
		record.UserID,
		record.SessionID,
		record.JobID,
		record.AssetID,
		record.RequestID,
		record.IPAddress,
		record.UserAgent,
	}
	if len(record.Metadata) > 0 {
		if data, err := json.Marshal(record.Metadata); err == nil {
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

func auditRiskLevel(event string) string {
	event = strings.ToLower(strings.TrimSpace(event))
	switch event {
	case "account_delete", "memory_delete_user", "data_export", "user_ban", "user_disable", "skill_disable", "skill_policy_update", "admin_job_cancel":
		return "high"
	}
	if strings.Contains(event, "rate_limited") || strings.Contains(event, "auth_login_failed") {
		return "medium"
	}
	if strings.Contains(event, "delete") || strings.Contains(event, "disable") || strings.Contains(event, "ban") || strings.Contains(event, "policy") {
		return "high"
	}
	if strings.Contains(event, "cancel") || strings.Contains(event, "publish") || strings.Contains(event, "unpublish") || strings.Contains(event, "update") || strings.Contains(event, "memory_") || strings.Contains(event, "review") {
		return "medium"
	}
	return "low"
}

func (s *Server) SetAuditLogger(logger AuditLogger) {
	if s == nil {
		return
	}
	s.audit = logger
}

func (s *Server) auditEvent(r *http.Request, event string, user User, fields map[string]any) {
	if s == nil || s.audit == nil || r == nil || strings.TrimSpace(event) == "" {
		return
	}
	record := AuditRecord{
		ID:        newAuditID(),
		Event:     event,
		UserID:    user.ID,
		RequestID: requestIDFromContext(r.Context()),
		IPAddress: clientIP(r),
		UserAgent: r.UserAgent(),
		Metadata:  map[string]any{},
		CreatedAt: time.Now().UTC(),
	}
	for key, value := range fields {
		switch key {
		case "session_id":
			record.SessionID, _ = value.(string)
		case "job_id":
			record.JobID, _ = value.(string)
		case "asset_id", "attachment_id", "artifact_id":
			record.AssetID, _ = value.(string)
		default:
			record.Metadata[key] = value
		}
	}
	if err := s.audit.Record(r.Context(), record); err != nil {
		if s.metrics != nil {
			s.metrics.IncAuditError()
		}
		s.logEvent("audit_error", map[string]any{"event_name": event, "error": err.Error(), "request_id": record.RequestID})
	}
}

func newAuditID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(data[:])
}

func clientIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); value != "" {
		if first, _, ok := strings.Cut(value, ","); ok {
			return strings.TrimSpace(first)
		}
		return value
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

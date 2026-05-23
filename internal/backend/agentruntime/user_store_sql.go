package agentruntime

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type UserAccount struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	DisplayName     string     `json:"display_name"`
	PasswordHash    string     `json:"-"`
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
}

type RefreshTokenRecord struct {
	TokenHash string     `json:"-"`
	UserID    string     `json:"user_id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	UserAgent string     `json:"user_agent,omitempty"`
	IPAddress string     `json:"ip_address,omitempty"`
}

type EmailVerificationTokenRecord struct {
	TokenHash string     `json:"-"`
	UserID    string     `json:"user_id"`
	Email     string     `json:"email"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

type PasswordResetTokenRecord struct {
	TokenHash string     `json:"-"`
	UserID    string     `json:"user_id"`
	Email     string     `json:"email"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
}

type AdminUserRecord struct {
	ID                      string     `json:"id"`
	Email                   string     `json:"email"`
	DisplayName             string     `json:"display_name"`
	Status                  string     `json:"status"`
	EmailVerifiedAt         *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	LastLoginAt             *time.Time `json:"last_login_at,omitempty"`
	RefreshTokenCount       int        `json:"refresh_token_count"`
	ActiveRefreshTokenCount int        `json:"active_refresh_token_count"`
}

type AdminUserFilter struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

type UserStore interface {
	CreateUser(ctx context.Context, user *UserAccount) error
	GetUserByID(ctx context.Context, userID string) (*UserAccount, error)
	GetUserByEmail(ctx context.Context, email string) (*UserAccount, error)
	UpdateLastLogin(ctx context.Context, userID string, at time.Time) error
	CreateRefreshToken(ctx context.Context, token *RefreshTokenRecord) error
	GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshTokenRecord, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string, at time.Time) error
	RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time) error
	CreateEmailVerificationToken(ctx context.Context, token *EmailVerificationTokenRecord) error
	ConsumeEmailVerificationToken(ctx context.Context, tokenHash string, at time.Time) (*UserAccount, error)
	CreatePasswordResetToken(ctx context.Context, token *PasswordResetTokenRecord) error
	ConsumePasswordResetToken(ctx context.Context, tokenHash string, newPasswordHash string, at time.Time) (*UserAccount, error)
	DeleteUser(ctx context.Context, userID string) error
	PruneExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int, error)
}

type AdminUserStore interface {
	ListUsers(ctx context.Context, filter AdminUserFilter) ([]AdminUserRecord, error)
	GetAdminUser(ctx context.Context, userID string) (*AdminUserRecord, error)
	UpdateUserStatus(ctx context.Context, userID string, status string, at time.Time) (*AdminUserRecord, error)
}

type SQLUserStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLUserStore(db *sql.DB) *SQLUserStore {
	return NewSQLUserStoreWithDialect(db, SQLDialectQuestion)
}

func NewSQLUserStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLUserStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLUserStore{db: db, dialect: dialect}
}

func (s *SQLUserStore) Init(ctx context.Context) error {
	timeType := s.dialect.TimeType()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS agent_users (
	user_id TEXT PRIMARY KEY,
	email TEXT NOT NULL,
	email_normalized TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	display_name TEXT NOT NULL,
	status TEXT NOT NULL,
	email_verified_at ` + timeType + `,
	created_at ` + timeType + ` NOT NULL,
	updated_at ` + timeType + ` NOT NULL,
	last_login_at ` + timeType + `
)`,
		`ALTER TABLE agent_users ADD COLUMN IF NOT EXISTS email_verified_at ` + timeType,
		`CREATE INDEX IF NOT EXISTS idx_agent_users_status ON agent_users (status)`,
		`CREATE TABLE IF NOT EXISTS agent_refresh_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES agent_users(user_id) ON DELETE CASCADE,
	created_at ` + timeType + ` NOT NULL,
	expires_at ` + timeType + ` NOT NULL,
	revoked_at ` + timeType + `,
	user_agent TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT ''
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_refresh_tokens_user ON agent_refresh_tokens (user_id, expires_at)`,
		`CREATE TABLE IF NOT EXISTS agent_email_verification_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES agent_users(user_id) ON DELETE CASCADE,
	email TEXT NOT NULL,
	created_at ` + timeType + ` NOT NULL,
	expires_at ` + timeType + ` NOT NULL,
	used_at ` + timeType + `
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_email_verification_tokens_user ON agent_email_verification_tokens (user_id, expires_at)`,
		`CREATE TABLE IF NOT EXISTS agent_password_reset_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES agent_users(user_id) ON DELETE CASCADE,
	email TEXT NOT NULL,
	created_at ` + timeType + ` NOT NULL,
	expires_at ` + timeType + ` NOT NULL,
	used_at ` + timeType + `
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_password_reset_tokens_user ON agent_password_reset_tokens (user_id, expires_at)`,
		`INSERT INTO agent_schema_migrations (version, applied_at)
VALUES (2, ` + s.dialect.Placeholder(1) + `)
ON CONFLICT(version) DO NOTHING`,
	} {
		if strings.HasPrefix(stmt, "ALTER TABLE agent_users ADD COLUMN") && s.dialect != SQLDialectPostgres {
			continue
		}
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
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_users", "created_at", "updated_at", "last_login_at"); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_users", "email_verified_at"); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_refresh_tokens", "created_at", "expires_at", "revoked_at"); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_email_verification_tokens", "created_at", "expires_at", "used_at"); err != nil {
		return err
	}
	return ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_password_reset_tokens", "created_at", "expires_at", "used_at")
}

func (s *SQLUserStore) CreateUser(ctx context.Context, user *UserAccount) error {
	if user == nil {
		return fmt.Errorf("user is required")
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_users (user_id, email, email_normalized, password_hash, display_name, status, email_verified_at, created_at, updated_at, last_login_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		user.ID,
		user.Email,
		normalizeEmail(user.Email),
		user.PasswordHash,
		user.DisplayName,
		user.Status,
		nullableSQLTimeValue(user.EmailVerifiedAt, s.dialect),
		sqlTimeValue(user.CreatedAt, s.dialect),
		sqlTimeValue(user.UpdatedAt, s.dialect),
		nullableSQLTimeValue(user.LastLoginAt, s.dialect),
	)
	return err
}

func (s *SQLUserStore) GetUserByID(ctx context.Context, userID string) (*UserAccount, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, email, password_hash, display_name, status, email_verified_at, created_at, updated_at, last_login_at
FROM agent_users WHERE user_id = ?`), userID))
}

func (s *SQLUserStore) GetUserByEmail(ctx context.Context, email string) (*UserAccount, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, email, password_hash, display_name, status, email_verified_at, created_at, updated_at, last_login_at
FROM agent_users WHERE email_normalized = ?`), normalizeEmail(email)))
}

func (s *SQLUserStore) ListUsers(ctx context.Context, filter AdminUserFilter) ([]AdminUserRecord, error) {
	filter = normalizeAdminUserFilter(filter)
	now := time.Now().UTC()
	query := `
SELECT u.user_id, u.email, u.display_name, u.status, u.email_verified_at, u.created_at, u.updated_at, u.last_login_at,
	COUNT(rt.token_hash) AS refresh_token_count,
	COALESCE(SUM(CASE WHEN rt.revoked_at IS NULL AND rt.expires_at > ? THEN 1 ELSE 0 END), 0) AS active_refresh_token_count
FROM agent_users u
LEFT JOIN agent_refresh_tokens rt ON rt.user_id = u.user_id`
	args := []any{sqlTimeValue(now, s.dialect)}
	var where []string
	if filter.Status != "" {
		where = append(where, "u.status = ?")
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		where = append(where, "(LOWER(u.email) LIKE ? OR LOWER(u.display_name) LIKE ? OR LOWER(u.user_id) LIKE ?)")
		like := "%" + strings.ToLower(filter.Query) + "%"
		args = append(args, like, like, like)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " GROUP BY u.user_id, u.email, u.display_name, u.status, u.email_verified_at, u.created_at, u.updated_at, u.last_login_at ORDER BY u.created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []AdminUserRecord
	for rows.Next() {
		record, err := scanAdminUserRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if records == nil {
		records = []AdminUserRecord{}
	}
	return records, rows.Err()
}

func (s *SQLUserStore) GetAdminUser(ctx context.Context, userID string) (*AdminUserRecord, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT u.user_id, u.email, u.display_name, u.status, u.email_verified_at, u.created_at, u.updated_at, u.last_login_at,
	COUNT(rt.token_hash) AS refresh_token_count,
	COALESCE(SUM(CASE WHEN rt.revoked_at IS NULL AND rt.expires_at > ? THEN 1 ELSE 0 END), 0) AS active_refresh_token_count
FROM agent_users u
LEFT JOIN agent_refresh_tokens rt ON rt.user_id = u.user_id
WHERE u.user_id = ?
GROUP BY u.user_id, u.email, u.display_name, u.status, u.email_verified_at, u.created_at, u.updated_at, u.last_login_at`), sqlTimeValue(now, s.dialect), strings.TrimSpace(userID))
	record, err := scanAdminUserRecord(row)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *SQLUserStore) UpdateUserStatus(ctx context.Context, userID string, status string, at time.Time) (*AdminUserRecord, error) {
	status = normalizeUserStatus(status)
	if status == "" {
		return nil, fmt.Errorf("invalid user status")
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_users SET status = ?, updated_at = ? WHERE user_id = ?`), status, sqlTimeValue(at, s.dialect), userID)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return nil, sql.ErrNoRows
	}
	return s.GetAdminUser(ctx, userID)
}

func (s *SQLUserStore) UpdateLastLogin(ctx context.Context, userID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_users SET last_login_at = ?, updated_at = ? WHERE user_id = ?`),
		sqlTimeValue(at, s.dialect), sqlTimeValue(at, s.dialect), userID)
	return err
}

func (s *SQLUserStore) CreateRefreshToken(ctx context.Context, token *RefreshTokenRecord) error {
	if token == nil {
		return fmt.Errorf("refresh token is required")
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_refresh_tokens (token_hash, user_id, created_at, expires_at, revoked_at, user_agent, ip_address)
VALUES (?, ?, ?, ?, ?, ?, ?)`),
		token.TokenHash,
		token.UserID,
		sqlTimeValue(token.CreatedAt, s.dialect),
		sqlTimeValue(token.ExpiresAt, s.dialect),
		nullableSQLTimeValue(token.RevokedAt, s.dialect),
		token.UserAgent,
		token.IPAddress,
	)
	return err
}

func (s *SQLUserStore) GetRefreshToken(ctx context.Context, tokenHash string) (*RefreshTokenRecord, error) {
	var createdAt, expiresAt, revokedAt any
	var rec RefreshTokenRecord
	err := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT token_hash, user_id, created_at, expires_at, revoked_at, user_agent, ip_address
FROM agent_refresh_tokens WHERE token_hash = ?`), tokenHash).Scan(
		&rec.TokenHash,
		&rec.UserID,
		&createdAt,
		&expiresAt,
		&revokedAt,
		&rec.UserAgent,
		&rec.IPAddress,
	)
	if err != nil {
		return nil, err
	}
	if rec.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if rec.ExpiresAt, err = parseSQLTime(expiresAt); err != nil {
		return nil, err
	}
	if rec.RevokedAt, err = parseNullableSQLTime(revokedAt); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *SQLUserStore) RevokeRefreshToken(ctx context.Context, tokenHash string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`),
		sqlTimeValue(at, s.dialect), tokenHash)
	return err
}

func (s *SQLUserStore) RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`),
		sqlTimeValue(at, s.dialect), userID)
	return err
}

func (s *SQLUserStore) CreateEmailVerificationToken(ctx context.Context, token *EmailVerificationTokenRecord) error {
	if token == nil {
		return fmt.Errorf("email verification token is required")
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_email_verification_tokens (token_hash, user_id, email, created_at, expires_at, used_at)
VALUES (?, ?, ?, ?, ?, ?)`),
		token.TokenHash,
		token.UserID,
		token.Email,
		sqlTimeValue(token.CreatedAt, s.dialect),
		sqlTimeValue(token.ExpiresAt, s.dialect),
		nullableSQLTimeValue(token.UsedAt, s.dialect),
	)
	return err
}

func (s *SQLUserStore) ConsumeEmailVerificationToken(ctx context.Context, tokenHash string, at time.Time) (*UserAccount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var userID string
	var expiresAt any
	err = tx.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, expires_at
FROM agent_email_verification_tokens
WHERE token_hash = ? AND used_at IS NULL`), tokenHash).Scan(&userID, &expiresAt)
	if err != nil {
		return nil, err
	}
	parsedExpiresAt, err := parseSQLTime(expiresAt)
	if err != nil {
		return nil, err
	}
	if at.After(parsedExpiresAt) {
		return nil, fmt.Errorf("email verification token expired")
	}
	if _, err = tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_email_verification_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`), sqlTimeValue(at, s.dialect), tokenHash); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_users SET status = ?, email_verified_at = ?, updated_at = ? WHERE user_id = ?`),
		UserStatusActive, sqlTimeValue(at, s.dialect), sqlTimeValue(at, s.dialect), userID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetUserByID(ctx, userID)
}

func (s *SQLUserStore) CreatePasswordResetToken(ctx context.Context, token *PasswordResetTokenRecord) error {
	if strings.TrimSpace(token.TokenHash) == "" {
		return fmt.Errorf("password reset token is required")
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_password_reset_tokens (token_hash, user_id, email, created_at, expires_at, used_at)
VALUES (?, ?, ?, ?, ?, ?)`),
		token.TokenHash,
		token.UserID,
		token.Email,
		sqlTimeValue(token.CreatedAt, s.dialect),
		sqlTimeValue(token.ExpiresAt, s.dialect),
		nullableSQLTimeValue(token.UsedAt, s.dialect),
	)
	return err
}

func (s *SQLUserStore) ConsumePasswordResetToken(ctx context.Context, tokenHash string, newPasswordHash string, at time.Time) (*UserAccount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tokenHash = strings.TrimSpace(tokenHash)
	var userID string
	var expiresAt any
	var usedAt any
	row := tx.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, expires_at, used_at
FROM agent_password_reset_tokens
WHERE token_hash = ?`), tokenHash)
	if err := row.Scan(&userID, &expiresAt, &usedAt); err != nil {
		return nil, err
	}
	parsedExpiresAt, err := parseSQLTime(expiresAt)
	if err != nil {
		return nil, err
	}
	parsedUsedAt, err := parseNullableSQLTime(usedAt)
	if err != nil {
		return nil, err
	}
	if parsedUsedAt != nil || !at.Before(parsedExpiresAt) {
		return nil, fmt.Errorf("password reset token expired")
	}
	res, err := tx.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_password_reset_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`), sqlTimeValue(at, s.dialect), tokenHash)
	if err != nil {
		return nil, err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return nil, fmt.Errorf("password reset token expired")
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`UPDATE agent_users SET password_hash = ?, updated_at = ? WHERE user_id = ?`), newPasswordHash, sqlTimeValue(at, s.dialect), userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetUserByID(ctx, userID)
}

func (s *SQLUserStore) DeleteUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_users WHERE user_id = ?`), userID)
	return err
}

func (s *SQLUserStore) PruneExpiredRefreshTokens(ctx context.Context, cutoff time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_refresh_tokens WHERE expires_at < ? OR revoked_at < ?`), sqlTimeValue(cutoff, s.dialect), sqlTimeValue(cutoff, s.dialect))
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *SQLUserStore) scanUser(row *sql.Row) (*UserAccount, error) {
	var emailVerifiedAt, createdAt, updatedAt, lastLoginAt any
	var user UserAccount
	if err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.DisplayName,
		&user.Status,
		&emailVerifiedAt,
		&createdAt,
		&updatedAt,
		&lastLoginAt,
	); err != nil {
		return nil, err
	}
	var err error
	if user.EmailVerifiedAt, err = parseNullableSQLTime(emailVerifiedAt); err != nil {
		return nil, err
	}
	if user.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if user.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return nil, err
	}
	if user.LastLoginAt, err = parseNullableSQLTime(lastLoginAt); err != nil {
		return nil, err
	}
	return &user, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeAdminUserFilter(filter AdminUserFilter) AdminUserFilter {
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	filter.Status = normalizeOptionalUserStatus(filter.Status)
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func normalizeUserStatus(status string) string {
	status = normalizeOptionalUserStatus(status)
	if status == "" {
		return UserStatusActive
	}
	return status
}

func normalizeOptionalUserStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case UserStatusPendingVerification:
		return UserStatusPendingVerification
	case UserStatusActive:
		return UserStatusActive
	case UserStatusDisabled:
		return UserStatusDisabled
	case UserStatusBanned:
		return UserStatusBanned
	default:
		return ""
	}
}

func validUserStatus(status string) bool {
	return normalizeOptionalUserStatus(status) != ""
}

func scanAdminUserRecord(row skillRegistryScanner) (AdminUserRecord, error) {
	var record AdminUserRecord
	var emailVerifiedAt, createdAt, updatedAt, lastLoginAt any
	if err := row.Scan(
		&record.ID,
		&record.Email,
		&record.DisplayName,
		&record.Status,
		&emailVerifiedAt,
		&createdAt,
		&updatedAt,
		&lastLoginAt,
		&record.RefreshTokenCount,
		&record.ActiveRefreshTokenCount,
	); err != nil {
		return AdminUserRecord{}, err
	}
	var err error
	if record.EmailVerifiedAt, err = parseNullableSQLTime(emailVerifiedAt); err != nil {
		return AdminUserRecord{}, err
	}
	if record.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return AdminUserRecord{}, err
	}
	if record.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return AdminUserRecord{}, err
	}
	if record.LastLoginAt, err = parseNullableSQLTime(lastLoginAt); err != nil {
		return AdminUserRecord{}, err
	}
	return record, nil
}

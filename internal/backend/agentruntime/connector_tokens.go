package agentruntime

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type SQLConnectorTokenVault struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLConnectorTokenVaultWithDialect(db *sql.DB, dialect SQLDialect) *SQLConnectorTokenVault {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLConnectorTokenVault{db: db, dialect: dialect}
}

func (v *SQLConnectorTokenVault) Init(ctx context.Context) error {
	if v == nil || v.db == nil {
		return fmt.Errorf("connector token vault is not configured")
	}
	if len(connectorTokenVaultKey()) == 0 {
		return fmt.Errorf("AGENT_API_CONNECTOR_TOKEN_SECRET is required for the SQL connector token vault")
	}
	return requireSQLColumns(ctx, v.db, "agent_connector_tokens",
		"token_ref", "provider", "access_token_ciphertext", "refresh_token_ciphertext", "token_type",
		"scopes_json", "expires_at", "refresh_expires_at", "created_at", "updated_at",
	)
}

func (v *SQLConnectorTokenVault) PutToken(ctx context.Context, token ConnectorToken) error {
	if v == nil || v.db == nil {
		return fmt.Errorf("connector token vault is not configured")
	}
	token = normalizeConnectorToken(token)
	access, err := protectConnectorSecret(token.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := protectConnectorSecret(token.RefreshToken)
	if err != nil {
		return err
	}
	scopesJSON, _ := json.Marshal(token.Scopes)
	now := time.Now().UTC()
	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = now
	}
	if v.dialect == SQLDialectPostgres {
		query := `INSERT INTO agent_connector_tokens (
token_ref, provider, access_token_ciphertext, refresh_token_ciphertext, token_type,
scopes_json, expires_at, refresh_expires_at, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (token_ref) DO UPDATE SET
provider = EXCLUDED.provider,
access_token_ciphertext = EXCLUDED.access_token_ciphertext,
refresh_token_ciphertext = EXCLUDED.refresh_token_ciphertext,
token_type = EXCLUDED.token_type,
scopes_json = EXCLUDED.scopes_json,
expires_at = EXCLUDED.expires_at,
refresh_expires_at = EXCLUDED.refresh_expires_at,
updated_at = EXCLUDED.updated_at`
		_, err := v.db.ExecContext(ctx, query, token.Ref, token.Provider, access, refresh, token.TokenType, string(scopesJSON), token.ExpiresAt, token.RefreshExpiresAt, token.UpdatedAt, token.UpdatedAt)
		return err
	}
	query := `INSERT OR REPLACE INTO agent_connector_tokens (
token_ref, provider, access_token_ciphertext, refresh_token_ciphertext, token_type,
scopes_json, expires_at, refresh_expires_at, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?)`
	_, err = v.db.ExecContext(ctx, query, token.Ref, token.Provider, access, refresh, token.TokenType, string(scopesJSON), token.ExpiresAt, token.RefreshExpiresAt, token.UpdatedAt, token.UpdatedAt)
	return err
}

func (v *SQLConnectorTokenVault) GetToken(ctx context.Context, ref string) (*ConnectorToken, error) {
	if v == nil || v.db == nil {
		return nil, nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	query := v.dialect.Bind(`SELECT token_ref, provider, access_token_ciphertext, refresh_token_ciphertext, token_type, scopes_json, expires_at, refresh_expires_at, updated_at
FROM agent_connector_tokens
WHERE token_ref = ?
LIMIT 1`)
	var token ConnectorToken
	var accessCipher, refreshCipher, scopesRaw string
	var expiresAt, refreshExpiresAt sql.NullTime
	err := v.db.QueryRowContext(ctx, query, ref).Scan(&token.Ref, &token.Provider, &accessCipher, &refreshCipher, &token.TokenType, &scopesRaw, &expiresAt, &refreshExpiresAt, &token.UpdatedAt)
	if err != nil {
		if errorsIsSQLNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(scopesRaw), &token.Scopes); err != nil {
		token.Scopes = nil
	}
	access, err := unprotectConnectorSecret(accessCipher)
	if err != nil {
		return nil, err
	}
	refresh, err := unprotectConnectorSecret(refreshCipher)
	if err != nil {
		return nil, err
	}
	token.AccessToken = access
	token.RefreshToken = refresh
	token.ExpiresAt = nullTimePtr(expiresAt)
	token.RefreshExpiresAt = nullTimePtr(refreshExpiresAt)
	token = normalizeConnectorToken(token)
	return &token, nil
}

func (v *SQLConnectorTokenVault) DeleteToken(ctx context.Context, ref string) error {
	if v == nil || v.db == nil {
		return nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	query := v.dialect.Bind(`DELETE FROM agent_connector_tokens WHERE token_ref = ?`)
	_, err := v.db.ExecContext(ctx, query, ref)
	return err
}

func normalizeConnectorToken(token ConnectorToken) ConnectorToken {
	token.Ref = strings.TrimSpace(token.Ref)
	token.Provider = normalizeConnectorProviderID(token.Provider)
	token.Scopes = normalizeConnectorScopes(token.Scopes)
	if token.TokenType == "" {
		token.TokenType = "bearer"
	}
	if token.UpdatedAt.IsZero() {
		token.UpdatedAt = time.Now().UTC()
	}
	return token
}

func protectConnectorSecret(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	key := connectorTokenVaultKey()
	if len(key) == 0 {
		return "", fmt.Errorf("AGENT_API_CONNECTOR_TOKEN_SECRET is required to persist connector tokens")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plain), nil)
	payload := append(nonce, ciphertext...)
	return "aesgcm:" + base64.StdEncoding.EncodeToString(payload), nil
}

func unprotectConnectorSecret(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "plain:") {
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "plain:"))
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if !strings.HasPrefix(value, "aesgcm:") {
		return value, nil
	}
	key := connectorTokenVaultKey()
	if len(key) == 0 {
		return "", fmt.Errorf("connector token encryption key is required to decrypt persisted token")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "aesgcm:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("connector token ciphertext is malformed")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func connectorTokenVaultKey() []byte {
	secret := strings.TrimSpace(os.Getenv("AGENT_API_CONNECTOR_TOKEN_SECRET"))
	if secret == "" {
		return nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(secret); err == nil && len(decoded) == 32 {
		return decoded
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

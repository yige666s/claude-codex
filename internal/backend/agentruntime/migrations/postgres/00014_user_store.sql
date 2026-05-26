-- +goose Up
CREATE TABLE IF NOT EXISTS agent_users (
	user_id TEXT PRIMARY KEY,
	email TEXT NOT NULL,
	email_normalized TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	display_name TEXT NOT NULL,
	status TEXT NOT NULL,
	email_verified_at TIMESTAMPTZ,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_login_at TIMESTAMPTZ
);

ALTER TABLE agent_users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_agent_users_status ON agent_users (status);

CREATE TABLE IF NOT EXISTS agent_refresh_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES agent_users(user_id) ON DELETE CASCADE,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	revoked_at TIMESTAMPTZ,
	user_agent TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_refresh_tokens_user ON agent_refresh_tokens (user_id, expires_at);

CREATE TABLE IF NOT EXISTS agent_email_verification_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES agent_users(user_id) ON DELETE CASCADE,
	email TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_email_verification_tokens_user ON agent_email_verification_tokens (user_id, expires_at);

CREATE TABLE IF NOT EXISTS agent_password_reset_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES agent_users(user_id) ON DELETE CASCADE,
	email TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_password_reset_tokens_user ON agent_password_reset_tokens (user_id, expires_at);

-- +goose Down
SELECT 1;

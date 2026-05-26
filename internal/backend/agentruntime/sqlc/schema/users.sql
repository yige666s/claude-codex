CREATE TABLE agent_users (
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

CREATE TABLE agent_refresh_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	revoked_at TIMESTAMPTZ,
	user_agent TEXT NOT NULL DEFAULT '',
	ip_address TEXT NOT NULL DEFAULT ''
);

CREATE TABLE agent_email_verification_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	email TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ
);

CREATE TABLE agent_password_reset_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	email TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	expires_at TIMESTAMPTZ NOT NULL,
	used_at TIMESTAMPTZ
);

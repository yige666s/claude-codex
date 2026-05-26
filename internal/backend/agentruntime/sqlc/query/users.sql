-- name: InsertUser :exec
INSERT INTO agent_users (
	user_id,
	email,
	email_normalized,
	password_hash,
	display_name,
	status,
	email_verified_at,
	created_at,
	updated_at,
	last_login_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10
);

-- name: GetUserByID :one
SELECT
	user_id,
	email,
	email_normalized,
	password_hash,
	display_name,
	status,
	email_verified_at,
	created_at,
	updated_at,
	last_login_at
FROM agent_users
WHERE user_id = $1;

-- name: GetUserByEmail :one
SELECT
	user_id,
	email,
	email_normalized,
	password_hash,
	display_name,
	status,
	email_verified_at,
	created_at,
	updated_at,
	last_login_at
FROM agent_users
WHERE email_normalized = $1;

-- name: ListAdminUsers :many
SELECT
	u.user_id,
	u.email,
	u.display_name,
	u.status,
	u.email_verified_at,
	u.created_at,
	u.updated_at,
	u.last_login_at,
	COUNT(rt.token_hash)::bigint AS refresh_token_count,
	COALESCE(SUM(CASE WHEN rt.revoked_at IS NULL AND rt.expires_at > sqlc.arg('now_at') THEN 1 ELSE 0 END), 0)::bigint AS active_refresh_token_count
FROM agent_users u
LEFT JOIN agent_refresh_tokens rt ON rt.user_id = u.user_id
WHERE (sqlc.arg('status')::text = '' OR u.status = sqlc.arg('status')::text)
  AND (
	sqlc.arg('query')::text = ''
	OR LOWER(u.email) LIKE '%' || sqlc.arg('query')::text || '%'
	OR LOWER(u.display_name) LIKE '%' || sqlc.arg('query')::text || '%'
	OR LOWER(u.user_id) LIKE '%' || sqlc.arg('query')::text || '%'
  )
GROUP BY u.user_id, u.email, u.display_name, u.status, u.email_verified_at, u.created_at, u.updated_at, u.last_login_at
ORDER BY u.created_at DESC
LIMIT sqlc.arg('limit_count')::int
OFFSET sqlc.arg('offset_count')::int;

-- name: GetAdminUser :one
SELECT
	u.user_id,
	u.email,
	u.display_name,
	u.status,
	u.email_verified_at,
	u.created_at,
	u.updated_at,
	u.last_login_at,
	COUNT(rt.token_hash)::bigint AS refresh_token_count,
	COALESCE(SUM(CASE WHEN rt.revoked_at IS NULL AND rt.expires_at > sqlc.arg('now_at') THEN 1 ELSE 0 END), 0)::bigint AS active_refresh_token_count
FROM agent_users u
LEFT JOIN agent_refresh_tokens rt ON rt.user_id = u.user_id
WHERE u.user_id = sqlc.arg('user_id')
GROUP BY u.user_id, u.email, u.display_name, u.status, u.email_verified_at, u.created_at, u.updated_at, u.last_login_at;

-- name: UpdateUserStatus :execrows
UPDATE agent_users
SET status = $1,
	updated_at = $2
WHERE user_id = $3;

-- name: UpdateLastLogin :exec
UPDATE agent_users
SET last_login_at = $1,
	updated_at = $1
WHERE user_id = $2;

-- name: InsertRefreshToken :exec
INSERT INTO agent_refresh_tokens (
	token_hash,
	user_id,
	created_at,
	expires_at,
	revoked_at,
	user_agent,
	ip_address
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7
);

-- name: GetRefreshToken :one
SELECT
	token_hash,
	user_id,
	created_at,
	expires_at,
	revoked_at,
	user_agent,
	ip_address
FROM agent_refresh_tokens
WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE agent_refresh_tokens
SET revoked_at = $1
WHERE token_hash = $2
  AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokens :exec
UPDATE agent_refresh_tokens
SET revoked_at = $1
WHERE user_id = $2
  AND revoked_at IS NULL;

-- name: InsertEmailVerificationToken :exec
INSERT INTO agent_email_verification_tokens (
	token_hash,
	user_id,
	email,
	created_at,
	expires_at,
	used_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6
);

-- name: GetEmailVerificationTokenForConsume :one
SELECT
	user_id,
	expires_at
FROM agent_email_verification_tokens
WHERE token_hash = $1
  AND used_at IS NULL;

-- name: MarkEmailVerificationTokenUsed :exec
UPDATE agent_email_verification_tokens
SET used_at = $1
WHERE token_hash = $2
  AND used_at IS NULL;

-- name: MarkUserEmailVerified :exec
UPDATE agent_users
SET status = $1,
	email_verified_at = $2,
	updated_at = $2
WHERE user_id = $3;

-- name: InsertPasswordResetToken :exec
INSERT INTO agent_password_reset_tokens (
	token_hash,
	user_id,
	email,
	created_at,
	expires_at,
	used_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6
);

-- name: GetPasswordResetTokenForConsume :one
SELECT
	user_id,
	expires_at,
	used_at
FROM agent_password_reset_tokens
WHERE token_hash = $1;

-- name: MarkPasswordResetTokenUsed :execrows
UPDATE agent_password_reset_tokens
SET used_at = $1
WHERE token_hash = $2
  AND used_at IS NULL;

-- name: UpdateUserPassword :exec
UPDATE agent_users
SET password_hash = $1,
	updated_at = $2
WHERE user_id = $3;

-- name: DeleteUser :exec
DELETE FROM agent_users
WHERE user_id = $1;

-- name: PruneExpiredRefreshTokens :execrows
DELETE FROM agent_refresh_tokens
WHERE expires_at < $1
   OR revoked_at < $1;

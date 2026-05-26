-- name: DeleteMemoryForSession :exec
DELETE FROM agent_memory
WHERE user_id = $1
  AND session_id = $2;

-- name: DeleteMemoryForUser :exec
DELETE FROM agent_memory
WHERE user_id = $1;

-- name: DeleteMemorySettingsForUser :exec
DELETE FROM agent_memory_settings
WHERE user_id = $1;

-- name: GetMemorySettingsPayload :one
SELECT payload
FROM agent_memory_settings
WHERE user_id = $1;

-- name: UpsertMemorySettings :exec
INSERT INTO agent_memory_settings (user_id, payload, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT(user_id) DO UPDATE SET
	payload = excluded.payload,
	updated_at = excluded.updated_at;

-- name: GetPersonalizationSettingsPayload :one
SELECT payload
FROM agent_personalization_settings
WHERE user_id = $1;

-- name: UpsertPersonalizationSettings :exec
INSERT INTO agent_personalization_settings (user_id, payload, version, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT(user_id) DO UPDATE SET
	payload = excluded.payload,
	version = excluded.version,
	updated_at = excluded.updated_at;

-- name: DeletePersonalizationSettingsForUser :exec
DELETE FROM agent_personalization_settings
WHERE user_id = $1;

-- name: DeleteDeletedMemoryBefore :execrows
DELETE FROM agent_memory
WHERE status = $1
  AND updated_at < $2;

-- name: GetMemoryItem :one
SELECT
	id,
	user_id,
	session_id,
	namespace,
	kind,
	level,
	category,
	tags,
	source,
	source_refs,
	visibility,
	status,
	content,
	raw_hash,
	confidence,
	weight,
	access_count,
	parent_id,
	related_ids,
	conflict_ids,
	supersedes_id,
	superseded_by_id,
	last_injected_at,
	metadata,
	expires_at,
	created_at,
	updated_at
FROM agent_memory
WHERE user_id = $1
  AND id = $2;

-- name: ListMemoryItems :many
SELECT
	id,
	user_id,
	session_id,
	namespace,
	kind,
	level,
	category,
	tags,
	source,
	source_refs,
	visibility,
	status,
	content,
	raw_hash,
	confidence,
	weight,
	access_count,
	parent_id,
	related_ids,
	conflict_ids,
	supersedes_id,
	superseded_by_id,
	last_injected_at,
	metadata,
	expires_at,
	created_at,
	updated_at
FROM agent_memory
WHERE user_id = sqlc.arg('user_id')
  AND (sqlc.arg('session_id')::text = '' OR session_id = sqlc.arg('session_id')::text)
  AND (sqlc.arg('namespace')::text = '' OR namespace = sqlc.arg('namespace')::text)
  AND (sqlc.arg('kind')::text = '' OR kind = sqlc.arg('kind')::text)
  AND (sqlc.arg('level')::text = '' OR level = sqlc.arg('level')::text)
  AND (sqlc.arg('category')::text = '' OR category = sqlc.arg('category')::text)
  AND (sqlc.arg('visibility')::text = '' OR visibility = sqlc.arg('visibility')::text)
  AND (sqlc.arg('status')::text = '' OR status = sqlc.arg('status')::text)
  AND (sqlc.arg('query_pattern')::text = '' OR LOWER(content) LIKE sqlc.arg('query_pattern')::text)
  AND (sqlc.arg('source_kind_pattern')::text = '' OR source_refs LIKE sqlc.arg('source_kind_pattern')::text)
  AND (sqlc.arg('source_id_pattern')::text = '' OR source_refs LIKE sqlc.arg('source_id_pattern')::text)
ORDER BY weight DESC, updated_at DESC, id DESC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0);

-- name: ListAllMemoryItems :many
SELECT
	id,
	user_id,
	session_id,
	namespace,
	kind,
	level,
	category,
	tags,
	source,
	source_refs,
	visibility,
	status,
	content,
	raw_hash,
	confidence,
	weight,
	access_count,
	parent_id,
	related_ids,
	conflict_ids,
	supersedes_id,
	superseded_by_id,
	last_injected_at,
	metadata,
	expires_at,
	created_at,
	updated_at
FROM agent_memory;

-- name: UpsertMemoryItem :exec
INSERT INTO agent_memory (
	id,
	user_id,
	session_id,
	namespace,
	kind,
	level,
	category,
	tags,
	source,
	source_refs,
	visibility,
	status,
	content,
	raw_hash,
	confidence,
	weight,
	access_count,
	parent_id,
	related_ids,
	conflict_ids,
	supersedes_id,
	superseded_by_id,
	last_injected_at,
	metadata,
	expires_at,
	created_at,
	updated_at
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
	$10,
	$11,
	$12,
	$13,
	$14,
	$15,
	$16,
	$17,
	$18,
	$19,
	$20,
	$21,
	$22,
	$23,
	$24,
	$25,
	$26,
	$27
)
ON CONFLICT(id) DO UPDATE SET
	session_id = excluded.session_id,
	namespace = excluded.namespace,
	kind = excluded.kind,
	level = excluded.level,
	category = excluded.category,
	tags = excluded.tags,
	source = excluded.source,
	source_refs = excluded.source_refs,
	visibility = excluded.visibility,
	status = excluded.status,
	content = excluded.content,
	raw_hash = excluded.raw_hash,
	confidence = excluded.confidence,
	weight = excluded.weight,
	access_count = excluded.access_count,
	parent_id = excluded.parent_id,
	related_ids = excluded.related_ids,
	conflict_ids = excluded.conflict_ids,
	supersedes_id = excluded.supersedes_id,
	superseded_by_id = excluded.superseded_by_id,
	last_injected_at = excluded.last_injected_at,
	metadata = excluded.metadata,
	expires_at = excluded.expires_at,
	updated_at = excluded.updated_at;

-- name: DeleteMemoryItem :exec
DELETE FROM agent_memory
WHERE user_id = $1
  AND id = $2;

-- name: InsertArtifact :exec
INSERT INTO agent_artifacts (
	artifact_id,
	kind,
	user_id,
	session_id,
	job_id,
	object_key,
	filename,
	content_type,
	size_bytes,
	created_at,
	deleted_at
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
	$11
);

-- name: GetArtifact :one
SELECT
	artifact_id,
	kind,
	user_id,
	session_id,
	job_id,
	object_key,
	filename,
	content_type,
	size_bytes,
	created_at,
	deleted_at
FROM agent_artifacts
WHERE user_id = $1
  AND artifact_id = $2
  AND kind = $3
  AND deleted_at IS NULL;

-- name: ListArtifacts :many
SELECT
	artifact_id,
	kind,
	user_id,
	session_id,
	job_id,
	object_key,
	filename,
	content_type,
	size_bytes,
	created_at,
	deleted_at
FROM agent_artifacts
WHERE user_id = sqlc.arg(user_id)
  AND kind = sqlc.arg(kind)
  AND deleted_at IS NULL
  AND (sqlc.narg(session_id)::text IS NULL OR session_id = sqlc.narg(session_id)::text)
ORDER BY created_at DESC;

-- name: ListUploadedArtifactsBefore :many
SELECT
	artifact_id,
	kind,
	user_id,
	session_id,
	job_id,
	object_key,
	filename,
	content_type,
	size_bytes,
	created_at,
	deleted_at
FROM agent_artifacts
WHERE kind = $1
  AND deleted_at IS NULL
  AND object_key <> ''
  AND created_at < $2
ORDER BY created_at ASC;

-- name: MarkArtifactDeleted :exec
UPDATE agent_artifacts
SET deleted_at = $1
WHERE user_id = $2
  AND artifact_id = $3
  AND kind = $4
  AND deleted_at IS NULL;

-- name: MarkSessionArtifactsDeleted :exec
UPDATE agent_artifacts
SET deleted_at = $1
WHERE user_id = $2
  AND session_id = $3
  AND deleted_at IS NULL;

-- name: DeleteUserArtifacts :exec
DELETE FROM agent_artifacts
WHERE user_id = $1;

-- name: PruneDeletedArtifactsBefore :execrows
DELETE FROM agent_artifacts
WHERE deleted_at IS NOT NULL
  AND deleted_at < $1;

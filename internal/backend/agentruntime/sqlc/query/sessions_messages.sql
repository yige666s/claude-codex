-- name: GetSession :one
SELECT
	user_id,
	session_id,
	agent_id,
	title,
	status,
	message_count,
	total_tokens,
	working_dir,
	tags,
	description,
	parent_id,
	branch_point,
	metadata,
	archived,
	created_at,
	updated_at,
	last_message_at
FROM agent_sessions
WHERE user_id = $1
  AND session_id = $2
  AND status <> $3;

-- name: ListSessions :many
SELECT
	s.user_id,
	s.session_id,
	s.agent_id,
	COALESCE(NULLIF(s.title, ''), (
		SELECT m.content
		FROM agent_messages m
		WHERE m.user_id = s.user_id
		  AND m.session_id = s.session_id
		  AND m.status <> 2
		  AND m.hidden = 0
		  AND m.role = 'user'
		  AND TRIM(m.content) <> ''
		ORDER BY m.seq_no ASC
		LIMIT 1
	), '')::text AS title,
	s.status,
	s.message_count,
	s.total_tokens,
	s.working_dir,
	s.tags,
	s.description,
	s.parent_id,
	s.branch_point,
	s.metadata,
	s.archived,
	s.created_at,
	s.updated_at,
	s.last_message_at
FROM agent_sessions s
WHERE s.user_id = sqlc.arg('user_id')
  AND s.status <> sqlc.arg('deleted_status')
ORDER BY s.updated_at DESC
LIMIT NULLIF(sqlc.arg('limit_count')::int, 0)
OFFSET sqlc.arg('offset_count')::int;

-- name: UpsertSession :exec
INSERT INTO agent_sessions (
	user_id,
	session_id,
	agent_id,
	title,
	status,
	message_count,
	total_tokens,
	working_dir,
	tags,
	description,
	parent_id,
	branch_point,
	metadata,
	archived,
	created_at,
	updated_at,
	last_message_at
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
	$17
)
ON CONFLICT(user_id, session_id) DO UPDATE SET
	agent_id = excluded.agent_id,
	title = excluded.title,
	status = excluded.status,
	message_count = excluded.message_count,
	total_tokens = excluded.total_tokens,
	working_dir = excluded.working_dir,
	tags = excluded.tags,
	description = excluded.description,
	parent_id = excluded.parent_id,
	branch_point = excluded.branch_point,
	metadata = excluded.metadata,
	archived = excluded.archived,
	updated_at = excluded.updated_at,
	last_message_at = excluded.last_message_at;

-- name: SoftDeleteSessionMessages :exec
UPDATE agent_messages
SET status = $1,
	updated_at = $2
WHERE user_id = $3
  AND session_id = $4
  AND status <> $5;

-- name: SoftDeleteSession :exec
UPDATE agent_sessions
SET status = $1,
	archived = $2,
	updated_at = $3
WHERE user_id = $4
  AND session_id = $5
  AND status <> $6;

-- name: SoftDeleteUserMessages :exec
UPDATE agent_messages
SET status = $1,
	updated_at = $2
WHERE user_id = $3
  AND status <> $4;

-- name: SoftDeleteUserSessions :exec
UPDATE agent_sessions
SET status = $1,
	archived = $2,
	updated_at = $3
WHERE user_id = $4
  AND status <> $5;

-- name: ListSessionsBeforePrune :many
SELECT user_id, session_id
FROM agent_sessions
WHERE updated_at < $1
  AND status <> $2;

-- name: ListMessages :many
SELECT
	message_id,
	session_id,
	user_id,
	seq_no,
	parent_id,
	role,
	content_type,
	content,
	content_parts,
	tool_call_id,
	tool_name,
	tool_input,
	tool_output,
	tool_calls,
	prompt_tokens,
	completion_tokens,
	status,
	is_context_used,
	model_id,
	run_id,
	hidden,
	created_at,
	updated_at,
	archive_uri,
	archive_checksum,
	archived_at
FROM agent_messages
WHERE user_id = $1
  AND session_id = $2
  AND status <> $3
ORDER BY seq_no ASC;

-- name: GetSessionCreatedAt :one
SELECT created_at
FROM agent_sessions
WHERE user_id = $1
  AND session_id = $2
  AND status <> $3;

-- name: NextMessageSeq :one
SELECT (COALESCE(MAX(seq_no), 0) + 1)::bigint
FROM agent_messages
WHERE user_id = $1
  AND session_id = $2;

-- name: MaxMessageSeq :one
SELECT COALESCE(MAX(seq_no), 0)::bigint
FROM agent_messages
WHERE user_id = $1
  AND session_id = $2;

-- name: FindMessageByID :one
SELECT
	message_id,
	session_id,
	user_id,
	seq_no,
	parent_id,
	role,
	content_type,
	content,
	content_parts,
	tool_call_id,
	tool_name,
	tool_input,
	tool_output,
	tool_calls,
	prompt_tokens,
	completion_tokens,
	status,
	is_context_used,
	model_id,
	run_id,
	hidden,
	created_at,
	updated_at,
	archive_uri,
	archive_checksum,
	archived_at
FROM agent_messages
WHERE user_id = $1
  AND session_id = $2
  AND message_id = $3;

-- name: InsertMessage :exec
INSERT INTO agent_messages (
	message_id,
	session_id,
	user_id,
	seq_no,
	parent_id,
	role,
	content_type,
	content,
	content_parts,
	tool_call_id,
	tool_name,
	tool_input,
	tool_output,
	tool_calls,
	prompt_tokens,
	completion_tokens,
	status,
	is_context_used,
	model_id,
	run_id,
	hidden,
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
	$23
);

-- name: UpsertMessage :exec
INSERT INTO agent_messages (
	message_id,
	session_id,
	user_id,
	seq_no,
	parent_id,
	role,
	content_type,
	content,
	content_parts,
	tool_call_id,
	tool_name,
	tool_input,
	tool_output,
	tool_calls,
	prompt_tokens,
	completion_tokens,
	status,
	is_context_used,
	model_id,
	run_id,
	hidden,
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
	$23
)
ON CONFLICT(message_id) DO UPDATE SET
	session_id = excluded.session_id,
	user_id = excluded.user_id,
	seq_no = excluded.seq_no,
	parent_id = excluded.parent_id,
	role = excluded.role,
	content_type = excluded.content_type,
	content = excluded.content,
	content_parts = excluded.content_parts,
	tool_call_id = excluded.tool_call_id,
	tool_name = excluded.tool_name,
	tool_input = excluded.tool_input,
	tool_output = excluded.tool_output,
	tool_calls = excluded.tool_calls,
	prompt_tokens = excluded.prompt_tokens,
	completion_tokens = excluded.completion_tokens,
	status = excluded.status,
	is_context_used = excluded.is_context_used,
	model_id = excluded.model_id,
	run_id = excluded.run_id,
	hidden = excluded.hidden,
	updated_at = excluded.updated_at;

-- name: UpdateSessionAfterAppend :exec
UPDATE agent_sessions
SET message_count = (
		SELECT COUNT(*)
		FROM agent_messages
		WHERE agent_messages.user_id = $1
		  AND agent_messages.session_id = $2
		  AND agent_messages.status <> $3
	),
	total_tokens = agent_sessions.total_tokens + $4,
	title = CASE WHEN agent_sessions.title = '' AND sqlc.arg('title_candidate')::text <> '' THEN sqlc.arg('title_candidate')::text ELSE agent_sessions.title END,
	updated_at = sqlc.arg('updated_at'),
	last_message_at = sqlc.arg('last_message_at')
WHERE agent_sessions.user_id = sqlc.arg('target_user_id')
  AND agent_sessions.session_id = sqlc.arg('target_session_id');

-- name: LoadSessionMessages :many
SELECT
	message_id,
	session_id,
	user_id,
	seq_no,
	parent_id,
	role,
	content_type,
	content,
	content_parts,
	tool_call_id,
	tool_name,
	tool_input,
	tool_output,
	tool_calls,
	prompt_tokens,
	completion_tokens,
	status,
	is_context_used,
	model_id,
	run_id,
	hidden,
	created_at,
	updated_at,
	archive_uri,
	archive_checksum,
	archived_at
FROM agent_messages
WHERE user_id = sqlc.arg('user_id')
  AND session_id = sqlc.arg('session_id')
  AND status = sqlc.arg('normal_status')
  AND is_context_used = 1
  AND (
	sqlc.arg('include_system')::boolean
	OR (role <> sqlc.arg('system_role') AND content_type <> sqlc.arg('summary_content_type'))
  )
ORDER BY seq_no DESC
LIMIT sqlc.arg('max_messages')::int;

-- name: LoadLatestSummaryMessage :one
SELECT
	message_id,
	session_id,
	user_id,
	seq_no,
	parent_id,
	role,
	content_type,
	content,
	content_parts,
	tool_call_id,
	tool_name,
	tool_input,
	tool_output,
	tool_calls,
	prompt_tokens,
	completion_tokens,
	status,
	is_context_used,
	model_id,
	run_id,
	hidden,
	created_at,
	updated_at,
	archive_uri,
	archive_checksum,
	archived_at
FROM agent_messages
WHERE user_id = $1
  AND session_id = $2
  AND status = $3
  AND is_context_used = 1
  AND (role = $4 OR content_type = $5)
ORDER BY seq_no DESC
LIMIT 1;

-- name: SearchMessages :many
SELECT
	m.message_id,
	m.session_id,
	m.seq_no,
	m.role,
	m.content,
	m.tool_output,
	m.created_at,
	s.title,
	s.description
FROM agent_messages m
JOIN agent_sessions s ON s.user_id = m.user_id AND s.session_id = m.session_id
WHERE m.user_id = $1
  AND m.status = $2
  AND s.status <> $3
  AND m.hidden = 0
  AND m.role <> 'tool'
  AND (m.content ILIKE sqlc.arg('pattern')::text ESCAPE '\' OR m.tool_output ILIKE sqlc.arg('pattern')::text ESCAPE '\')
ORDER BY m.created_at DESC
LIMIT sqlc.arg('limit_count')::int OFFSET sqlc.arg('offset_count')::int;

-- name: UpsertMessageEmbeddingMeta :exec
INSERT INTO agent_message_embedding_meta (
	embedding_id,
	message_id,
	session_id,
	user_id,
	chunk_index,
	vector_id,
	model_version,
	created_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8
)
ON CONFLICT(embedding_id) DO UPDATE SET
	vector_id = excluded.vector_id,
	model_version = excluded.model_version,
	created_at = excluded.created_at;

-- name: UpsertMessageAttachment :exec
INSERT INTO agent_message_attachments (
	attachment_id,
	message_id,
	session_id,
	user_id,
	file_type,
	mime_type,
	file_name,
	file_size,
	storage_key,
	thumbnail_key,
	extracted_text_key,
	embedding_status,
	created_at
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
	$13
)
ON CONFLICT(message_id, attachment_id) DO UPDATE SET
	file_type = excluded.file_type,
	mime_type = excluded.mime_type,
	file_name = excluded.file_name,
	file_size = excluded.file_size,
	storage_key = excluded.storage_key,
	thumbnail_key = excluded.thumbnail_key,
	extracted_text_key = excluded.extracted_text_key,
	embedding_status = excluded.embedding_status;

-- name: ListPendingMessageAttachments :many
SELECT
	attachment_id,
	message_id,
	session_id,
	user_id,
	file_type,
	mime_type,
	file_name,
	file_size,
	storage_key,
	thumbnail_key,
	extracted_text_key,
	embedding_status,
	created_at
FROM agent_message_attachments
WHERE user_id = $1
  AND embedding_status = $2
ORDER BY created_at ASC
LIMIT $3;

-- name: ListPendingMessageAttachmentsForProcessing :many
SELECT
	attachment_id,
	message_id,
	session_id,
	user_id,
	file_type,
	mime_type,
	file_name,
	file_size,
	storage_key,
	thumbnail_key,
	extracted_text_key,
	embedding_status,
	created_at
FROM agent_message_attachments
WHERE embedding_status = $1
ORDER BY created_at ASC
LIMIT $2;

-- name: UpdateMessageAttachmentProcessing :exec
UPDATE agent_message_attachments
SET embedding_status = $1,
	thumbnail_key = $2,
	extracted_text_key = $3
WHERE user_id = $4
  AND message_id = $5
  AND attachment_id = $6;

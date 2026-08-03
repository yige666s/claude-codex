-- +goose Up
WITH ranked AS (
	SELECT user_id, session_id, idempotency_key,
		ROW_NUMBER() OVER (
			PARTITION BY user_id, session_id
			ORDER BY updated_at DESC, created_at DESC, idempotency_key DESC
		) AS position
	FROM agent_chat_turn_reservations
	WHERE status = 'reserved'
)
UPDATE agent_chat_turn_reservations AS reservation
SET status = 'expired', updated_at = now()
FROM ranked
WHERE reservation.user_id = ranked.user_id
	AND reservation.session_id = ranked.session_id
	AND reservation.idempotency_key = ranked.idempotency_key
	AND ranked.position > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_chat_turn_reservations_active_session
	ON agent_chat_turn_reservations (user_id, session_id)
	WHERE status = 'reserved';

-- +goose Down
DROP INDEX IF EXISTS uniq_agent_chat_turn_reservations_active_session;

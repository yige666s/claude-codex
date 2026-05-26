-- +goose Up
DROP TABLE IF EXISTS agent_message_embedding_meta_legacy_pre_message_module;
DROP TABLE IF EXISTS agent_messages_legacy_pre_message_module;
DROP TABLE IF EXISTS agent_sessions_legacy_pre_message_module;

DELETE FROM agent_message_attachments a WHERE NOT EXISTS (
	SELECT 1 FROM agent_messages m WHERE m.message_id = a.message_id
);

DELETE FROM agent_message_embedding_meta e WHERE NOT EXISTS (
	SELECT 1 FROM agent_messages m WHERE m.message_id = e.message_id
);

DELETE FROM agent_messages m WHERE NOT EXISTS (
	SELECT 1 FROM agent_sessions s WHERE s.user_id = m.user_id AND s.session_id = m.session_id
);

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_messages_session'
	) THEN
		ALTER TABLE agent_messages ADD CONSTRAINT fk_agent_messages_session FOREIGN KEY (user_id, session_id) REFERENCES agent_sessions(user_id, session_id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_message_attachments_message'
	) THEN
		ALTER TABLE agent_message_attachments ADD CONSTRAINT fk_agent_message_attachments_message FOREIGN KEY (message_id) REFERENCES agent_messages(message_id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'fk_agent_message_embedding_meta_message'
	) THEN
		ALTER TABLE agent_message_embedding_meta ADD CONSTRAINT fk_agent_message_embedding_meta_message FOREIGN KEY (message_id) REFERENCES agent_messages(message_id) ON DELETE CASCADE;
	END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
SELECT 1;

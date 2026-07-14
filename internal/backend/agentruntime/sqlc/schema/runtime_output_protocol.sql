CREATE TABLE agent_message_structured_outputs (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	run_id TEXT NOT NULL DEFAULT '',
	message_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT '',
	schema_version TEXT NOT NULL DEFAULT '',
	payload_json JSONB NOT NULL DEFAULT '{}',
	source TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_structured_outputs_session_time
	ON agent_message_structured_outputs (user_id, session_id, created_at);

CREATE INDEX idx_agent_structured_outputs_run
	ON agent_message_structured_outputs (user_id, run_id);

CREATE TABLE agent_chat_run_snapshots (
	run_id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	final_message_id TEXT NOT NULL DEFAULT '',
	final_content TEXT NOT NULL DEFAULT '',
	event_count INTEGER NOT NULL DEFAULT 0,
	structured_output_count INTEGER NOT NULL DEFAULT 0,
	artifact_count INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	last_event_id TEXT NOT NULL DEFAULT '',
	payload_json JSONB NOT NULL DEFAULT '{}',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_chat_run_snapshots_user_session
	ON agent_chat_run_snapshots (user_id, session_id, updated_at DESC);

CREATE TABLE agent_chat_turn_reservations (
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	idempotency_key TEXT NOT NULL DEFAULT '',
	run_id TEXT NOT NULL DEFAULT '',
	user_message_id TEXT NOT NULL DEFAULT '',
	assistant_message_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (user_id, session_id, idempotency_key)
);

CREATE UNIQUE INDEX idx_agent_chat_turn_reservations_run
	ON agent_chat_turn_reservations (user_id, run_id);

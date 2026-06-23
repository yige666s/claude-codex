-- +goose Up
CREATE TABLE IF NOT EXISTS agent_connector_connections (
  connection_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL,
  status TEXT NOT NULL,
  permission_policy TEXT NOT NULL DEFAULT 'read_only',
  scopes_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  token_ref TEXT NOT NULL DEFAULT '',
  external_account_id TEXT NOT NULL DEFAULT '',
  external_account_label TEXT NOT NULL DEFAULT '',
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  connected_at TIMESTAMPTZ,
  last_sync_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  disconnected_at TIMESTAMPTZ,
  UNIQUE (user_id, workspace_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_agent_connector_connections_user_updated
  ON agent_connector_connections (user_id, workspace_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS agent_connector_oauth_states (
  state TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  scopes_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  redirect_uri TEXT NOT NULL DEFAULT '',
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_connector_oauth_states_user_provider
  ON agent_connector_oauth_states (user_id, provider, expires_at DESC);

-- +goose Down
SELECT 1;

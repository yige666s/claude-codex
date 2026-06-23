CREATE TABLE agent_connector_tokens (
  token_ref TEXT PRIMARY KEY,
  provider TEXT NOT NULL,
  access_token_ciphertext TEXT NOT NULL DEFAULT '',
  refresh_token_ciphertext TEXT NOT NULL DEFAULT '',
  token_type TEXT NOT NULL DEFAULT 'bearer',
  scopes_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  expires_at TIMESTAMPTZ,
  refresh_expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agent_connector_tokens_provider_updated
  ON agent_connector_tokens (provider, updated_at DESC);

CREATE TABLE agent_mcp_servers (
  server_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL DEFAULT '',
  provider TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  transport TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL DEFAULT '',
  command_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  headers_ref TEXT NOT NULL DEFAULT '',
  oauth_token_ref TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'disconnected',
  last_discovered_at TIMESTAMPTZ,
  instructions TEXT NOT NULL DEFAULT '',
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, workspace_id, provider)
);

CREATE INDEX idx_agent_mcp_servers_user_updated
  ON agent_mcp_servers (user_id, workspace_id, updated_at DESC);

CREATE TABLE agent_mcp_tool_policies (
  policy_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  workspace_id TEXT NOT NULL DEFAULT '',
  server_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  permission_policy TEXT NOT NULL DEFAULT 'read_only',
  requires_review BOOLEAN NOT NULL DEFAULT false,
  side_effect_level TEXT NOT NULL DEFAULT 'read',
  allowed BOOLEAN NOT NULL DEFAULT true,
  metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, workspace_id, server_id, tool_name)
);

CREATE INDEX idx_agent_mcp_tool_policies_server_tool
  ON agent_mcp_tool_policies (server_id, tool_name);

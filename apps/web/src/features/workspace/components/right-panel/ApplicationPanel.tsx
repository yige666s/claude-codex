import { useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";
import type { ConnectorPolicy, ConnectorStatus } from "../../../../types";

const connectorPolicyOptions: Array<{ value: ConnectorPolicy; label: string }> = [
  { value: "read_only", label: "Read only" },
  { value: "draft_write", label: "Draft writes" },
  { value: "write_with_review", label: "Review writes" },
  { value: "disabled", label: "Disabled" }
];
const connectorToolPreviewLimit = 6;

type ApplicationPanelProps = {
  connectors: ConnectorStatus[];
  busyProvider: string;
  notice?: string;
  emptyLabel: string;
  onConnect: (provider: string) => void;
  onPolicyChange: (provider: string, policy: ConnectorPolicy) => void;
  onDisconnect: (provider: string) => void;
};

export function ApplicationPanel({
  connectors,
  busyProvider,
  notice,
  emptyLabel,
  onConnect,
  onPolicyChange,
  onDisconnect
}: ApplicationPanelProps) {
  if (!connectors.length) {
    return <div className="resource-empty">{emptyLabel}</div>;
  }

  return (
    <div className="application-panel">
      <div className="application-panel-summary">
        <strong>Connectors</strong>
        <span>{connectors.filter((item) => item.connection?.status === "connected").length} connected</span>
      </div>
      {notice && (
        <div className="connector-success-banner" role="status">
          <strong>{notice}</strong>
        </div>
      )}
      <div className="connector-list">
        {connectors.map((item) => (
          <ConnectorCard
            key={item.provider.id}
            item={item}
            busy={busyProvider === item.provider.id}
            onConnect={onConnect}
            onPolicyChange={onPolicyChange}
            onDisconnect={onDisconnect}
          />
        ))}
      </div>
    </div>
  );
}

function ConnectorCard({
  item,
  busy,
  onConnect,
  onPolicyChange,
  onDisconnect
}: {
  item: ConnectorStatus;
  busy: boolean;
  onConnect: (provider: string) => void;
  onPolicyChange: (provider: string, policy: ConnectorPolicy) => void;
  onDisconnect: (provider: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const connection = item.connection;
  const connected = connection?.status === "connected";

  return (
    <section className={`connector-card ${expanded ? "expanded" : ""}`}>
      <div className="connector-card-main">
        <div>
          <div className="connector-title-row">
            <strong>{item.provider.name}</strong>
            <span className={`connector-status ${connected ? "connected" : connection?.status === "disabled" ? "disabled" : ""}`}>
              {connected ? "Connected" : connection?.status === "disabled" ? "Disabled" : "Not connected"}
            </span>
            {!item.provider.configured && <span className="connector-status warning">OAuth not configured</span>}
          </div>
          <p>{item.provider.description}</p>
        </div>
        <div className="settings-action-group">
          {connected ? (
            <button className="settings-action danger-outline" type="button" disabled={busy} onClick={() => onDisconnect(item.provider.id)}>
              Disconnect
            </button>
          ) : (
            <button className="settings-action primary" type="button" disabled={busy} onClick={() => onConnect(item.provider.id)}>
              {busy ? "Connecting..." : "Connect"}
            </button>
          )}
          <button
            className="connector-details-toggle"
            type="button"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            {expanded ? "Hide details" : "Show details"}
          </button>
        </div>
      </div>
      {expanded && (
        <div className="connector-card-details">
          <div className="connector-meta-grid">
            <ConnectorFact label="Scope" value={connection?.scopes?.join(", ") || item.provider.scopes.join(", ")} />
            <ConnectorFact label="Last sync" value={formatConnectorTime(connection?.last_sync_at)} />
            <ConnectorFact label="Context" value={item.context.task_types.join(", ")} />
            <ConnectorFact label="Runtime" value={formatConnectorKind(item.provider.connection_kind, item.provider.supports_synced_index)} />
            <ConnectorFact label="MCP server" value={formatMCPServer(item.mcp_server)} />
            <ConnectorFact label="Discovery" value={formatConnectorTime(item.mcp_server?.last_discovered_at)} />
            <label className="connector-policy-field">
              <span>Policy</span>
              <select
                className="settings-select connector-policy-select"
                value={connection?.permission_policy || item.provider.default_policy}
                disabled={!connection || busy}
                onChange={(event) => onPolicyChange(item.provider.id, event.target.value as ConnectorPolicy)}
              >
                {connectorPolicyOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            </label>
          </div>
          <ConnectorMCPError server={item.mcp_server} />
          <ConnectorTools tools={item.mcp_tools || []} />
          <small className="connector-policy-note">{item.context.policy_hint}</small>
        </div>
      )}
    </section>
  );
}

function ConnectorMCPError({ server }: { server?: ConnectorStatus["mcp_server"] }) {
  const error = formatMCPDiscoveryError(server);
  if (!error) return null;
  const match = error.match(/https?:\/\/\S+/);
  const url = match?.[0]?.replace(/[")\]}.,]+$/, "");
  return (
    <div className="connector-error-note" role="status">
      <strong>MCP discovery error</strong>
      <span>{error}</span>
      {url && (
        <a href={url} target="_blank" rel="noreferrer">
          Open provider setup
        </a>
      )}
    </div>
  );
}

function ConnectorTools({ tools }: { tools: ConnectorStatus["mcp_tools"] }) {
  const [expanded, setExpanded] = useState(false);
  if (!tools?.length) return null;
  const visibleTools = expanded ? tools : tools.slice(0, connectorToolPreviewLimit);
  const hiddenCount = Math.max(0, tools.length - visibleTools.length);
  const readCount = tools.filter((tool) => tool.side_effect_level === "read").length;
  const reviewCount = tools.filter((tool) => tool.requires_review).length;
  const disabledCount = tools.filter((tool) => !tool.allowed).length;
  const summary = [
    `${tools.length} tools`,
    `${readCount} read`,
    reviewCount ? `${reviewCount} review` : "",
    disabledCount ? `${disabledCount} disabled` : ""
  ].filter(Boolean).join(" · ");
  return (
    <div className={`connector-tools ${expanded ? "expanded" : ""}`}>
      <div className="connector-tools-header">
        <div>
          <span>MCP tools</span>
          <strong>{summary}</strong>
        </div>
        {tools.length > connectorToolPreviewLimit && (
          <button
            className="connector-tools-toggle"
            type="button"
            aria-expanded={expanded}
            onClick={() => setExpanded((value) => !value)}
          >
            {expanded ? <ChevronUp size={16} /> : <ChevronDown size={16} />}
            {expanded ? "Show less" : `Show all ${tools.length}`}
          </button>
        )}
      </div>
      <div className="connector-tool-list" aria-label="MCP tools">
        {visibleTools.map((tool) => (
          <span className={`connector-tool-chip ${tool.allowed ? "" : "disabled"}`} key={tool.policy_id || tool.tool_name}>
            <strong>{tool.tool_name}</strong>
            <small>{formatToolPolicy(tool.permission_policy, tool.requires_review, tool.side_effect_level)}</small>
          </span>
        ))}
        {!expanded && hiddenCount > 0 && (
          <span className="connector-tool-chip more">
            <strong>+{hiddenCount} more</strong>
          </span>
        )}
      </div>
    </div>
  );
}

function ConnectorFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="connector-fact">
      <span>{label}</span>
      <strong>{value || "None"}</strong>
    </div>
  );
}

function formatConnectorTime(value?: string) {
  if (!value) return "Never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function formatMCPServer(server?: ConnectorStatus["mcp_server"]) {
  if (!server) return "None";
  const transport = server.transport || "mcp";
  return `${transport} · ${server.status}`;
}

function formatMCPDiscoveryError(server?: ConnectorStatus["mcp_server"]) {
  const raw = typeof server?.metadata?.last_discovery_error === "string" ? server.metadata.last_discovery_error : "";
  if (!raw || server?.status !== "error") return "";
  return raw
    .replace(/\\\//g, "/")
    .replace(/^mcp\s+\w+\s+tools\/list\s+failed\s+\(\d+\):\s*/i, "")
    .replace(/^mcp\s+\w+\s+initialize\s+failed\s+\(\d+\):\s*/i, "")
    .trim();
}

function formatToolPolicy(policy: ConnectorPolicy, review: boolean, sideEffect: string) {
  if (review) return `${policy} · review`;
  return `${policy} · ${sideEffect || "unknown"}`;
}

function formatConnectorKind(kind?: string, synced?: boolean) {
  if (synced) return "Synced index + MCP";
  if (kind === "mcp_builtin_adapter") return "Built-in MCP adapter";
  if (kind === "mcp_remote") return "Remote MCP";
  if (kind === "synced_index") return "Synced index";
  return "Connector";
}

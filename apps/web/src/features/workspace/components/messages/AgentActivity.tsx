import { AlertCircle, CheckCircle2, ChevronDown, Circle, Copy, Download, Filter, Loader2, Wrench } from "lucide-react";
import { useMemo, useState } from "react";
import type { AgentActivity as AgentActivityState, AgentActivityItem } from "../../../../types";

type AgentActivityProps = {
  activity: AgentActivityState;
};

type TraceFilter = "all" | "model" | "tool" | "artifact" | "browser" | "verifier" | "error";

const traceFilters: Array<{ id: TraceFilter; label: string }> = [
  { id: "all", label: "All" },
  { id: "model", label: "Model" },
  { id: "tool", label: "Tool" },
  { id: "artifact", label: "Artifact" },
  { id: "browser", label: "Browser" },
  { id: "verifier", label: "Verifier" },
  { id: "error", label: "Error" }
];

export function AgentActivity({ activity }: AgentActivityProps) {
  const [filter, setFilter] = useState<TraceFilter>("all");
  const latest = activity.items[activity.items.length - 1];
  const toolCount = activity.items.filter((item) => isToolActivity(item)).length;
  const summary = [
    activity.running ? "Running" : "Completed",
    `${activity.items.length} step${activity.items.length === 1 ? "" : "s"}`,
    toolCount ? `${toolCount} tool${toolCount === 1 ? "" : "s"}` : ""
  ].filter(Boolean).join(" · ");
  const filteredItems = useMemo(() => activity.items.filter((item) => itemMatchesFilter(item, filter)), [activity.items, filter]);

  return (
    <details
      key={`${activity.session_id}:${activity.running ? "running" : "complete"}`}
      className={`agent-activity ${activity.running ? "running" : "complete"}`}
      open={activity.running}
    >
      <summary>
        <span className="agent-activity-icon" aria-hidden="true">
          {activity.running ? <Loader2 size={16} /> : <CheckCircle2 size={16} />}
        </span>
        <span className="agent-activity-title">
          Agent trace
          <small>{latest?.title || summary}</small>
        </span>
        <span className="agent-activity-summary">{summary}</span>
        <ChevronDown className="agent-activity-chevron" size={16} aria-hidden="true" />
      </summary>
      <div className="agent-activity-toolbar" aria-label="Trace controls">
        <span><Filter size={13} aria-hidden="true" /> Filter</span>
        <div className="agent-activity-filters">
          {traceFilters.map((item) => (
            <button
              key={item.id}
              type="button"
              className={filter === item.id ? "active" : ""}
              onClick={() => setFilter(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <button type="button" onClick={() => copyTrace(activity)} title="Copy trace JSON">
          <Copy size={13} aria-hidden="true" />
          Copy
        </button>
        <button type="button" onClick={() => downloadTrace(activity)} title="Download trace JSON">
          <Download size={13} aria-hidden="true" />
          JSON
        </button>
      </div>
      <ol className="agent-activity-list" aria-label="Agent activity timeline">
        {filteredItems.map((item) => (
          <li key={item.id} className={`agent-activity-item ${displayStatus(item, activity.running)}`}>
            <span className="agent-activity-item-icon" aria-hidden="true">{iconForItem(item, activity.running)}</span>
            <details className="agent-activity-item-detail">
              <summary>
                <strong>{item.title}</strong>
                {item.detail && <small>{item.detail}</small>}
              </summary>
              <TraceItemDetails item={item} />
            </details>
          </li>
        ))}
        {!filteredItems.length && <li className="agent-activity-empty">No trace items match this filter.</li>}
      </ol>
    </details>
  );
}

function TraceItemDetails({ item }: { item: AgentActivityItem }) {
  const rows = traceRows(item);
  const links = evidenceLinks(item);
  return (
    <div className="agent-activity-trace-detail">
      {rows.length > 0 && (
        <dl>
          {rows.map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      )}
      {links.length > 0 && (
        <div className="agent-activity-evidence-links">
          <span>Evidence</span>
          {links.map((link) => (
            <a key={link.href} href={link.href} target="_blank" rel="noreferrer">{link.label}</a>
          ))}
        </div>
      )}
      {!!item.metadata && (
        <details className="agent-activity-raw">
          <summary>Raw event</summary>
          <pre>{JSON.stringify(item.metadata, null, 2)}</pre>
        </details>
      )}
    </div>
  );
}

function traceRows(item: AgentActivityItem): Array<[string, string]> {
  const meta = item.metadata || {};
  const route = recordValue(meta.route);
  const resultMetadata = recordValue(meta.result_metadata);
  const evidence = recordValue(meta.evidence);
  const diagnostics = recordValue(meta.diagnostics) || recordValue(resultMetadata?.diagnostic_details) || recordValue(evidence?.diagnostics);
  const rows: Array<[string, string | undefined]> = [
    ["Status", item.status],
    ["Step", stringValue(meta.step_id || meta.step_title)],
    ["Action", stringValue(meta.action_id || meta.action_hash)],
    ["Tool", stringValue(meta.tool || meta.tool_name || route?.mode)],
    ["Executor", stringValue(route?.executor)],
    ["Skill", stringValue(meta.skill_name || route?.skill_name)],
    ["Attempt", stringValue(meta.attempt || meta.retry_count || meta.attempt_strategy)],
    ["Duration", formatDuration(meta.duration_ms || resultMetadata?.duration_ms || diagnostics?.duration_ms)],
    ["Input summary", stringValue(meta.input_summary || meta.query || meta.prompt_preview || meta.content)],
    ["Output summary", stringValue(meta.output_summary || meta.summary || evidence?.summary || resultMetadata?.summary)],
    ["Evidence ID", stringValue(meta.evidence_id)],
    ["Tokens", stringValue(meta.token_estimate || meta.total_tokens || resultMetadata?.token_estimate)],
    ["Cost", formatCost(meta.cost || meta.cost_usd || meta.estimated_cost_usd || resultMetadata?.estimated_cost_usd)],
    ["Error", stringValue(meta.error)]
  ];
  return rows.filter((row): row is [string, string] => Boolean(row[1]));
}

function evidenceLinks(item: AgentActivityItem): Array<{ href: string; label: string }> {
  const meta = item.metadata || {};
  const sources = arrayValue(meta.sources);
  const links: Array<{ href: string; label: string }> = [];
  sources.forEach((source, index) => {
    const record = recordValue(source);
    const href = stringValue(record?.url || source);
    if (!/^https?:\/\//i.test(href)) return;
    links.push({ href, label: stringValue(record?.title) || `Source ${index + 1}` });
  });
  return links.slice(0, 6);
}

function itemMatchesFilter(item: AgentActivityItem, filter: TraceFilter): boolean {
  if (filter === "all") return true;
  if (filter === "error") return displayStatus(item, false) === "failed";
  const text = `${item.type} ${item.title} ${item.detail || ""} ${JSON.stringify(item.metadata || {})}`.toLowerCase();
  if (filter === "model") return /\bmodel\b|llm|prompt|token/.test(text);
  if (filter === "tool") return /tool|skill|mcp|connector|sandbox/.test(text);
  if (filter === "artifact") return /artifact|document|docx|image|file/.test(text);
  if (filter === "browser") return /browser|web|fetch|search|url|http/.test(text);
  if (filter === "verifier") return /verif|test|check|review/.test(text);
  return true;
}

function isToolActivity(item: AgentActivityItem): boolean {
  return itemMatchesFilter(item, "tool") || itemMatchesFilter(item, "artifact") || itemMatchesFilter(item, "browser");
}

function displayStatus(item: AgentActivityItem, activityRunning: boolean): AgentActivityItem["status"] {
  if (item.status === "failed") return "failed";
  if (!activityRunning && item.status === "running") return "succeeded";
  return item.status;
}

function iconForItem(item: AgentActivityItem, activityRunning: boolean) {
  const status = displayStatus(item, activityRunning);
  if (status === "failed") return <AlertCircle size={14} />;
  if (status === "succeeded") return <CheckCircle2 size={14} />;
  if (isToolActivity(item)) return <Wrench size={14} />;
  return <Circle size={10} />;
}

function copyTrace(activity: AgentActivityState) {
  const text = JSON.stringify(tracePayload(activity), null, 2);
  navigator.clipboard?.writeText(text).catch(() => {});
}

function downloadTrace(activity: AgentActivityState) {
  const blob = new Blob([JSON.stringify(tracePayload(activity), null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `agent-trace-${activity.job_id || activity.session_id}.json`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function tracePayload(activity: AgentActivityState) {
  return {
    session_id: activity.session_id,
    job_id: activity.job_id,
    running: activity.running,
    item_count: activity.items.length,
    items: activity.items
  };
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function stringValue(value: unknown): string {
  if (typeof value === "string") return value.trim();
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function formatDuration(value: unknown): string {
  const ms = numberValue(value);
  if (!Number.isFinite(ms) || ms <= 0) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} s`;
}

function formatCost(value: unknown): string {
  const cost = numberValue(value);
  if (!Number.isFinite(cost) || cost <= 0) return "";
  return `$${cost.toFixed(cost < 0.01 ? 4 : 2)}`;
}

function numberValue(value: unknown): number {
  if (typeof value === "number") return value;
  if (typeof value === "string" && value.trim() !== "") return Number(value);
  return Number.NaN;
}

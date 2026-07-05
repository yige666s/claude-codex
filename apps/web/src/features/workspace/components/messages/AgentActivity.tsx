import { useState } from "react";
import { CheckCircle2, ChevronDown, ChevronRight, CircleStop, Loader2, Search, Wrench, XCircle } from "lucide-react";
import type { AgentActivity as AgentActivityState, AgentActivityItem } from "../../../../types";

type AgentActivityProps = {
  activity: AgentActivityState;
};

export function AgentActivity({ activity }: AgentActivityProps) {
  const [collapsed, setCollapsed] = useState(false);
  const latest = activity.items[activity.items.length - 1];
  const thinkingItems = activity.items.filter((item) => activityChannel(item) === "thinking").slice(-8);
  const toolItems = activity.items.filter((item) => activityChannel(item) === "tool").slice(-6);
  const noticeItems = activity.items
    .filter((item) => {
      const channel = activityChannel(item);
      return ["notice", "checkpoint"].includes(channel) || ((item.status === "failed" || item.status === "cancelled") && channel !== "tool");
    })
    .slice(-3);
  const citationItems = activity.items.filter((item) => activityChannel(item) === "citation").slice(-4);
  const toolCount = toolItems.length;
  const terminalItem = latestTerminalActivityItem(activity.items);
  const terminalStatus = terminalItem ? terminalItem.status : activity.running ? "running" : latest?.status || "default";
  const stateLabel = terminalStatus === "running"
    ? "Running"
    : terminalStatus === "cancelled"
      ? "Cancelled"
      : terminalStatus === "failed"
        ? "Failed"
        : "Completed";
  const stateClass = terminalStatus === "running"
    ? "running"
    : terminalStatus === "cancelled"
      ? "cancelled"
      : terminalStatus === "failed"
        ? "failed"
        : "complete";
  const summary = [
    stateLabel,
    `${activity.items.length} step${activity.items.length === 1 ? "" : "s"}`,
    toolCount ? `${toolCount} tool${toolCount === 1 ? "" : "s"}` : ""
  ].filter(Boolean).join(" · ");
  const latestThinking = latestVisibleItem(thinkingItems) || latest;
  const detail = latestThinking?.detail || latestThinking?.title || summary;
  const primaryLabel = terminalStatus === "running" ? "Thinking" : stateLabel;
  const thinkingLabel = activity.running ? "Thinking" : "Thought process";
  const hasSections = thinkingItems.length > 0 || toolItems.length > 0 || noticeItems.length > 0 || citationItems.length > 0;

  return (
    <div
      key={`${activity.session_id}:${stateClass}`}
      className={`agent-activity agent-activity-compact ${stateClass} ${collapsed ? "collapsed" : ""}`}
      role="status"
      aria-live={activity.running ? "polite" : "off"}
    >
      <div className="agent-activity-compact-row">
        <span className="agent-activity-icon" aria-hidden="true">
          {terminalStatus === "running"
            ? <Loader2 size={16} />
            : terminalStatus === "cancelled"
              ? <CircleStop size={16} />
              : terminalStatus === "failed"
                ? <XCircle size={16} />
                : <CheckCircle2 size={16} />}
        </span>
        <span className="agent-activity-title">
          {primaryLabel}
          <small>{detail}</small>
        </span>
        <span className="agent-activity-summary">{summary}</span>
        {hasSections && (
          <button
            className="agent-activity-collapse"
            type="button"
            aria-expanded={!collapsed}
            aria-label={collapsed ? "Expand thinking trace" : "Collapse thinking trace"}
            title={collapsed ? "Expand thinking trace" : "Collapse thinking trace"}
            onClick={() => setCollapsed((value) => !value)}
          >
            <ChevronDown size={16} aria-hidden="true" />
          </button>
        )}
      </div>
      {hasSections && !collapsed && (
        <div className="agent-activity-sections">
          {noticeItems.length > 0 && (
            <div className="agent-notice-list" aria-label="Important agent updates">
              {noticeItems.map((item) => (
                <div key={item.id} className={`agent-notice-item ${item.status}`}>
                  <strong>{item.title}</strong>
                  {item.detail && <span>{item.detail}</span>}
                </div>
              ))}
            </div>
          )}
          {toolItems.length > 0 && (
            <div className="agent-tool-list" aria-label="Tool activity">
              {toolItems.map((item) => (
                <div key={item.id} className={`agent-tool-row ${item.status}`}>
                  <span className="agent-tool-icon" aria-hidden="true">
                    {toolLooksLikeSearch(item) ? <Search size={14} /> : <Wrench size={14} />}
                  </span>
                  <p>
                    <strong>{toolStatusLabel(item)}</strong>
                    {item.detail && <small>{item.detail}</small>}
                  </p>
                  {toolSourceCount(item) && <span className="agent-tool-count">{toolSourceCount(item)}</span>}
                </div>
              ))}
            </div>
          )}
          {thinkingItems.length > 0 && (
            <details className="agent-thinking-panel" aria-label="Thinking progress">
              <summary>
                <ChevronRight size={14} aria-hidden="true" />
                <span>{thinkingLabel}</span>
                <small>{thinkingItems.length} update{thinkingItems.length === 1 ? "" : "s"}</small>
              </summary>
              <div className="agent-thinking-stream">
                {thinkingItems.map((item) => (
                  <div key={item.id} className={`agent-thinking-item ${item.status || "running"}`}>
                    <span aria-hidden="true" />
                    <p>
                      <strong>{item.title}</strong>
                      {item.detail && <small>{item.detail}</small>}
                    </p>
                  </div>
                ))}
              </div>
            </details>
          )}
          {citationItems.length > 0 && (
            <div className="agent-citation-list" aria-label="Citations">
              {citationItems.map((item) => (
                <span key={item.id} className="agent-citation-chip">
                  {item.title}
                </span>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function activityChannel(item: AgentActivityItem): NonNullable<AgentActivityItem["channel"]> {
  if (item.channel) return item.channel;
  return isToolActivity(item) ? "tool" : "thinking";
}

function latestVisibleItem(items: AgentActivityItem[]): AgentActivityItem | undefined {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (item.status !== "succeeded" || item.detail || item.title) return item;
  }
  return undefined;
}

function latestTerminalActivityItem(items: AgentActivityItem[]): AgentActivityItem | undefined {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    const item = items[index];
    if (isTerminalActivityItem(item)) return item;
  }
  return undefined;
}

function isTerminalActivityItem(item: AgentActivityItem): boolean {
  if (item.type === "done" || item.type === "error" || item.type === "cancelled") return true;
  if (/_(succeeded|completed|failed|error|cancelled)$/.test(item.type)) return true;
  return item.channel === "notice" && (item.status === "failed" || item.status === "cancelled");
}

function isToolActivity(item: AgentActivityItem): boolean {
  const text = `${item.type} ${item.title} ${item.detail || ""} ${JSON.stringify(item.metadata || {})}`.toLowerCase();
  return /tool|skill|mcp|connector|sandbox|artifact|document|docx|image|file|browser|web|fetch|search|url|http/.test(text);
}

function toolLooksLikeSearch(item: AgentActivityItem): boolean {
  const text = `${item.type} ${item.title} ${item.detail || ""} ${JSON.stringify(item.metadata || {})}`.toLowerCase();
  return /search|web|fetch|url|http|source/.test(text);
}

function toolStatusLabel(item: AgentActivityItem): string {
  if (item.status === "failed") return item.title.startsWith("Tool failed") ? item.title : `Failed · ${item.title}`;
  if (item.status === "succeeded") return item.title.startsWith("Tool finished") || item.title.startsWith("Used") ? item.title : `Done · ${item.title}`;
  if (item.status === "cancelled") return `Cancelled · ${item.title}`;
  return item.title;
}

function toolSourceCount(item: AgentActivityItem): string {
  const sources = item.metadata?.sources;
  if (Array.isArray(sources)) return `${sources.length} source${sources.length === 1 ? "" : "s"}`;
  const resultCount = item.metadata?.result_count || item.metadata?.results_count || item.metadata?.source_count;
  if (typeof resultCount === "number") return `${resultCount} result${resultCount === 1 ? "" : "s"}`;
  if (typeof resultCount === "string" && resultCount.trim()) return resultCount.trim();
  return "";
}

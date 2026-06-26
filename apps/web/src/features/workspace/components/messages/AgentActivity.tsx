import { CheckCircle2, Loader2 } from "lucide-react";
import type { AgentActivity as AgentActivityState, AgentActivityItem } from "../../../../types";

type AgentActivityProps = {
  activity: AgentActivityState;
};

export function AgentActivity({ activity }: AgentActivityProps) {
  const latest = activity.items[activity.items.length - 1];
  const toolCount = activity.items.filter((item) => isToolActivity(item)).length;
  const summary = [
    activity.running ? "Running" : "Completed",
    `${activity.items.length} step${activity.items.length === 1 ? "" : "s"}`,
    toolCount ? `${toolCount} tool${toolCount === 1 ? "" : "s"}` : ""
  ].filter(Boolean).join(" · ");
  const detail = latest?.title || summary;

  return (
    <div
      key={`${activity.session_id}:${activity.running ? "running" : "complete"}`}
      className={`agent-activity agent-activity-compact ${activity.running ? "running" : "complete"}`}
      role="status"
      aria-live={activity.running ? "polite" : "off"}
    >
      <div className="agent-activity-compact-row">
        <span className="agent-activity-icon" aria-hidden="true">
          {activity.running ? <Loader2 size={16} /> : <CheckCircle2 size={16} />}
        </span>
        <span className="agent-activity-title">
          Agent trace
          <small>{detail}</small>
        </span>
        <span className="agent-activity-summary">{summary}</span>
      </div>
    </div>
  );
}

function isToolActivity(item: AgentActivityItem): boolean {
  const text = `${item.type} ${item.title} ${item.detail || ""} ${JSON.stringify(item.metadata || {})}`.toLowerCase();
  return /tool|skill|mcp|connector|sandbox|artifact|document|docx|image|file|browser|web|fetch|search|url|http/.test(text);
}

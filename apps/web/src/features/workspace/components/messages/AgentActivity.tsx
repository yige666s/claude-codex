import { AlertCircle, CheckCircle2, ChevronDown, Circle, Loader2, Wrench } from "lucide-react";
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
          Agent activity
          <small>{latest?.title || summary}</small>
        </span>
        <span className="agent-activity-summary">{summary}</span>
        <ChevronDown className="agent-activity-chevron" size={16} aria-hidden="true" />
      </summary>
      <ol className="agent-activity-list" aria-label="Agent activity timeline">
        {activity.items.map((item) => (
          <li key={item.id} className={`agent-activity-item ${displayStatus(item, activity.running)}`}>
            <span className="agent-activity-item-icon" aria-hidden="true">{iconForItem(item, activity.running)}</span>
            <span>
              <strong>{item.title}</strong>
              {item.detail && <small>{item.detail}</small>}
            </span>
          </li>
        ))}
      </ol>
    </details>
  );
}

function isToolActivity(item: AgentActivityItem): boolean {
  return /tool|skill|artifact|sandbox|web|search|fetch/i.test(`${item.type} ${item.title} ${item.detail || ""}`);
}

function displayStatus(item: AgentActivityItem, activityRunning: boolean): AgentActivityItem["status"] {
  if (item.status === "failed") return "failed";
  if (!activityRunning) return "succeeded";
  return item.status;
}

function iconForItem(item: AgentActivityItem, activityRunning: boolean) {
  const status = displayStatus(item, activityRunning);
  if (status === "failed") return <AlertCircle size={14} />;
  if (status === "succeeded") return <CheckCircle2 size={14} />;
  if (isToolActivity(item)) return <Wrench size={14} />;
  return <Circle size={10} />;
}

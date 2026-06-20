import { ChevronDown } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { MotionPanel } from "../../../../components/motion";
import type { Job, JobEvent } from "../../../../types";
import type { JobStreamStatus } from "../../workspaceTypes";

export const terminalJobs = new Set(["succeeded", "failed", "cancelled"]);

type JobPanelProps = {
  jobs: Job[];
  selectedJobId: string;
  jobEvents: JobEvent[];
  jobStreamNotice: string;
  jobStreamStatus: JobStreamStatus;
  emptyLabel: string;
  onToggleJob: (jobId: string) => void;
  onCancelJob: () => void;
  formatTime: (value?: string) => string;
};

export function JobPanel({
  jobs,
  selectedJobId,
  jobEvents,
  jobStreamNotice,
  jobStreamStatus,
  emptyLabel,
  onToggleJob,
  onCancelJob,
  formatTime
}: JobPanelProps) {
  if (!jobs.length) return <div className="empty-small">{emptyLabel}</div>;
  return (
    <div className="job-list">
      {jobs.map((job) => {
        const expanded = job.id === selectedJobId;
        return (
          <section key={job.id} className={`job-list-entry ${expanded ? "expanded" : ""}`}>
            <Button
              className={`job-summary ${expanded ? "active" : ""}`}
              onClick={() => onToggleJob(job.id)}
              aria-expanded={expanded}
            >
              <span>{job.content || job.id}</span>
              <small>{job.status} · {formatTime(job.updated_at)}</small>
              <ChevronDown size={16} aria-hidden="true" />
            </Button>
            {expanded && (
              <MotionPanel className="job-expanded">
                <div className="job-card">
                  <div className={`pill ${job.status}`}>{job.status}</div>
                  {jobStreamNotice && !terminalJobs.has(job.status) && (
                    <span className={`job-stream-state ${jobStreamStatus}`}>{jobStreamNotice}</span>
                  )}
                  <Button className="danger inline" disabled={terminalJobs.has(job.status)} onClick={onCancelJob}>Cancel job</Button>
                </div>
                <JobEventTimeline events={jobEvents} />
              </MotionPanel>
            )}
          </section>
        );
      })}
    </div>
  );
}

export function JobEventTimeline({ events }: { events: JobEvent[] }) {
  const groups = groupJobEvents(visibleJobEvents(events));
  if (!groups.length) return <div className="empty-small">No job events yet</div>;
  return (
    <div className="timeline">
      {groups.map((group) => (
        <section key={group.id} className="timeline-event-group">
          <div className="timeline-event-group-head">
            <h4>{group.label}</h4>
            <span>{group.description}</span>
          </div>
          {group.events.map((event) => (
            <JobEventDetail key={event.id} event={event} />
          ))}
        </section>
      ))}
    </div>
  );
}

function visibleJobEvents(events: JobEvent[]): JobEvent[] {
  return events.filter((event) => !(event.type === "delta" && event.event?.role === "assistant"));
}

type JobEventGroup = {
  id: string;
  label: string;
  description: string;
  events: JobEvent[];
};

function groupJobEvents(events: JobEvent[]): JobEventGroup[] {
  const order = ["run", "steps", "outputs"];
  const labels: Record<string, { label: string; description: string }> = {
    run: {
      label: "Run",
      description: "Workflow lifecycle, status changes, and blocking errors"
    },
    steps: {
      label: "Steps",
      description: "Planner steps, DeepAgent actions, and child jobs"
    },
    outputs: {
      label: "Outputs",
      description: "Artifacts, reports, and generated deliverables"
    }
  };
  const grouped = new Map<string, JobEvent[]>();
  for (const event of events) {
    const data = eventData(event);
    const group = eventGroupForType(stringValue(data?.event_group) || event.type);
    grouped.set(group, [...(grouped.get(group) || []), event]);
  }
  return order
    .filter((id) => (grouped.get(id) || []).length > 0)
    .map((id) => ({ id, label: labels[id]?.label || id, description: labels[id]?.description || "", events: grouped.get(id) || [] }));
}

function eventGroupForType(type: string): string {
  if (type === "artifact_output" || type === "deep_agent_artifact_output" || type.startsWith("artifact")) return "outputs";
  if (type === "workflow_step" || type === "deep_agent_action" || type === "child_skill_job") return "steps";
  if (type.startsWith("workflow_step") || type.startsWith("deep_agent_action") || type === "deep_agent_child_job") return "steps";
  return "run";
}

function JobEventDetail({ event }: { event: JobEvent }) {
  const data = eventData(event);
  const title = eventTitle(event, data);
  const subtitle = eventSubtitle(event, data);
  const route = recordValue(data?.route);
  const resultMetadata = recordValue(data?.result_metadata);
  const diagnostics = recordValue(data?.diagnostics) || recordValue(resultMetadata?.diagnostic_details);
  const evidence = recordValue(data?.evidence);
  const routeRows = compactRows([
    ["Mode", stringValue(route?.mode)],
    ["Executor", stringValue(route?.executor)],
    ["Version", stringValue(route?.version || data?.route_version)],
    ["Deliverable", stringValue(route?.deliverable_type || data?.deliverable_type)],
    ["Requires artifact", boolString(route?.requires_artifact)],
    ["Skill", stringValue(route?.skill_name)],
    ["Search scope", stringValue(route?.search_scope || data?.search_scope)],
    ["Allowed tools", displayValue(route?.allowed_tools || data?.allowed_tools)],
    ["Filename", stringValue(route?.filename_hint)],
    ["Shadow diff", displayValue(route?.shadow_diff || data?.route_shadow_diff)]
  ]);
  const workflowRows = compactRows([
    ["Workflow", stringValue(data?.workflow_name)],
    ["Run", stringValue(data?.run_id)],
    ["Step", stringValue(data?.step_name)],
    ["Status", stringValue(data?.status)]
  ]);
  const actionRows = compactRows([
    ["Plan step", stringValue(data?.step_id || data?.step_title)],
    ["Tool", stringValue(data?.tool)],
    ["Skill", stringValue(data?.skill_name)],
    ["Query", stringValue(data?.query)],
    ["Action", stringValue(data?.action_hash)],
    ["Prompt", stringValue(data?.prompt_preview)],
    ["Attempt", stringValue(data?.attempt_strategy)]
  ]);
  const resultRows = compactRows([
    ["Result", stringValue(data?.result_status)],
    ["Completed", boolString(data?.completed)],
    ["Artifacts", stringValue(resultMetadata?.artifact_count)],
    ["Child job", stringValue(resultMetadata?.job_id)],
    ["Tool valid", boolString(resultMetadata?.tool_result_valid)],
    ["Error class", stringValue(data?.error_class || resultMetadata?.error_class)],
    ["Error", event.event?.error || stringValue(data?.error)]
  ]);
  const evidenceRows = compactRows([
    ["Sources", displayValue(data?.sources)],
    ["Tool calls", displayValue(data?.tool_calls)],
    ["Artifacts", displayValue(data?.artifact_refs)],
    ["Child jobs", displayValue(data?.child_jobs)],
    ["Evidence summary", stringValue(evidence?.summary)]
  ]);
  const metricRows = rowsFromRecord(recordValue(data?.metrics));
  return (
    <details className={`timeline-row event-${eventStatus(event, data)}`}>
      <summary>
        <span>{title}</span>
        <p>{subtitle}</p>
      </summary>
      <div className="timeline-detail">
        {workflowRows.length > 0 && <DetailGroup title="Workflow" rows={workflowRows} />}
        {routeRows.length > 0 && <DetailGroup title="Route" rows={routeRows} />}
        {actionRows.length > 0 && <DetailGroup title="Action" rows={actionRows} />}
        {resultRows.length > 0 && <DetailGroup title="Result" rows={resultRows} />}
        {evidenceRows.length > 0 && <DetailGroup title="Evidence" rows={evidenceRows} />}
        {diagnostics && <DetailJSON title="Diagnostics" value={diagnostics} />}
        {metricRows.length > 0 && <DetailGroup title="Metrics" rows={metricRows} />}
        {data && (
          <details className="timeline-raw">
            <summary>Raw data</summary>
            <pre>{JSON.stringify(data, null, 2)}</pre>
          </details>
        )}
      </div>
    </details>
  );
}

function DetailJSON({ title, value }: { title: string; value: Record<string, unknown> }) {
  return (
    <details className="timeline-raw">
      <summary>{title}</summary>
      <pre>{JSON.stringify(value, null, 2)}</pre>
    </details>
  );
}

function DetailGroup({ title, rows }: { title: string; rows: Array<[string, string]> }) {
  return (
    <section className="timeline-detail-group">
      <h4>{title}</h4>
      <dl>
        {rows.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function eventData(event: JobEvent): Record<string, unknown> | null {
  return recordValue(event.event?.data) || null;
}

function eventTitle(event: JobEvent, data: Record<string, unknown> | null): string {
  if (event.type === "deep_agent_artifact_output") {
    const artifact = recordValue(data?.artifact);
    return ["artifact", stringValue(artifact?.filename || artifact?.id)].filter(Boolean).join(" · ");
  }
  if (event.type === "deep_agent_child_job") {
    const child = recordValue(data?.child_job);
    return ["child job", stringValue(child?.id), stringValue(child?.status)].filter(Boolean).join(" · ");
  }
  if (event.type.startsWith("deep_agent_action_")) {
    const step = stringValue(data?.step_id || data?.step_title);
    const tool = stringValue(data?.tool);
    const skill = stringValue(data?.skill_name);
    return [step || "deep_agent_action", tool, skill].filter(Boolean).join(" · ");
  }
  const workflow = stringValue(data?.workflow_name);
  const step = stringValue(data?.step_name);
  if (workflow && step) return `${workflow}.${step}`;
  return event.type;
}

function eventSubtitle(event: JobEvent, data: Record<string, unknown> | null): string {
  return event.event?.error || event.event?.content || event.event?.job_reason || stringValue(data?.status) || event.id;
}

function eventStatus(event: JobEvent, data: Record<string, unknown> | null): string {
  const status = stringValue(data?.result_status || data?.status).toLowerCase();
  if (status) return status;
  if (event.type.endsWith("_failed")) return "failed";
  if (event.type.endsWith("_succeeded")) return "succeeded";
  if (event.type.endsWith("_started")) return "running";
  return "default";
}

function rowsFromRecord(record?: Record<string, unknown>): Array<[string, string]> {
  if (!record) return [];
  return Object.entries(record)
    .map(([key, value]) => [key, displayValue(value)] as [string, string])
    .filter(([, value]) => value !== "");
}

function compactRows(rows: Array<[string, string | undefined]>): Array<[string, string]> {
  return rows.filter((row): row is [string, string] => Boolean(row[1]));
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function boolString(value: unknown): string {
  return typeof value === "boolean" ? String(value) : "";
}

function displayValue(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

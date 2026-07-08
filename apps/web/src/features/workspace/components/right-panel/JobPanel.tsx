import { ChevronDown, Copy, Download, Filter } from "lucide-react";
import { useMemo, useState } from "react";
import { userFacingErrorMessage } from "../../../../api/errorMessages";
import { Button } from "../../../../components/ui/button";
import { MotionPanel } from "../../../../components/motion";
import type { DeepAgentTraceSummary, GateDecision, Job, JobEvent, JobStatus, LoopContract, LoopHandoff, RuntimeEvent } from "../../../../types";
import type { JobStreamStatus } from "../../workspaceTypes";
import { buildParallelGroupsFromJobEvents, formatParallelDuration, type ParallelBranchBudget, type ParallelGroupTrace } from "../parallelTrace";

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
        const effectiveJob = expanded ? jobWithTerminalEvents(job, jobEvents) : job;
        return (
          <section key={effectiveJob.id} className={`job-list-entry ${expanded ? "expanded" : ""}`}>
            <Button
              className={`job-summary ${expanded ? "active" : ""}`}
              onClick={() => onToggleJob(effectiveJob.id)}
              aria-expanded={expanded}
            >
              <span>{effectiveJob.content || effectiveJob.id}</span>
              <small>{effectiveJob.status} · {formatTime(effectiveJob.updated_at)}</small>
              <ChevronDown size={16} aria-hidden="true" />
            </Button>
            {expanded && (
              <MotionPanel className="job-expanded">
                <div className="job-card">
                  <div className={`pill ${effectiveJob.status}`}>{effectiveJob.status}</div>
                  {jobStreamNotice && !terminalJobs.has(effectiveJob.status) && (
                    <span className={`job-stream-state ${jobStreamStatus}`}>{jobStreamNotice}</span>
                  )}
                  <Button className="danger inline" disabled={terminalJobs.has(effectiveJob.status)} onClick={onCancelJob}>Cancel job</Button>
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
  const [filter, setFilter] = useState<TraceFilter>("all");
  const visibleEvents = useMemo(() => visibleJobEvents(events), [events]);
  const filteredEvents = useMemo(() => visibleEvents.filter((event) => eventMatchesFilter(event, filter)), [visibleEvents, filter]);
  const parallelGroups = useMemo(() => buildParallelGroupsFromJobEvents(visibleEvents), [visibleEvents]);
  const loopContract = useMemo(() => loopContractFromEvents(visibleEvents), [visibleEvents]);
  const handoff = useMemo(() => loopHandoffFromEvents(visibleEvents), [visibleEvents]);
  const gateDecision = useMemo(() => latestBlockingGateDecision(visibleEvents), [visibleEvents]);
  const traceSummary = useMemo(() => traceSummaryFromEvents(visibleEvents), [visibleEvents]);
  const groups = groupJobEvents(filteredEvents);
  if (!visibleEvents.length) return <div className="empty-small">No job events yet</div>;
  return (
    <>
      <div className="agent-activity-toolbar timeline-toolbar" aria-label="Trace controls">
        <span className="agent-activity-filter-label"><Filter size={13} aria-hidden="true" /> Filter</span>
        <div className="agent-activity-filters" aria-label="Trace filters">
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
        <div className="agent-activity-actions" aria-label="Trace export actions">
          <button type="button" onClick={() => copyJobTrace(visibleEvents)} title="Copy trace JSON">
            <Copy size={13} aria-hidden="true" />
            Copy
          </button>
          <button type="button" onClick={() => downloadJobTrace(visibleEvents)} title="Download trace JSON">
            <Download size={13} aria-hidden="true" />
            JSON
          </button>
        </div>
      </div>
      {loopContract && <LoopContractOverview contract={loopContract} />}
      {traceSummary && <TraceSummaryCard summary={traceSummary} />}
      {handoff && <HandoffOverview handoff={handoff} />}
      {gateDecision && <GateDecisionNotice decision={gateDecision} />}
      {parallelGroups.length > 0 && <ParallelJobOverview groups={parallelGroups} />}
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
        {!groups.length && <div className="empty-small">No trace events match this filter</div>}
      </div>
    </>
  );
}

function LoopContractOverview({ contract }: { contract: LoopContract }) {
  const deliverable = contract.deliverable;
  const stopConditions = contract.stop_policy?.done_when?.filter(Boolean).slice(0, 2) || [];
  return (
    <section className="loop-contract-overview" aria-label="Loop contract summary">
      <header>
        <strong>Loop Contract</strong>
        <span>{contract.version || "loop-contract/v1"}</span>
      </header>
      <dl>
        <div>
          <dt>Objective</dt>
          <dd>{contract.objective || "Current task"}</dd>
        </div>
        <div>
          <dt>Task type</dt>
          <dd>{humanizeToken(contract.task_type || "general_task")}</dd>
        </div>
        <div>
          <dt>Deliverable</dt>
          <dd>{formatLoopDeliverable(deliverable)}</dd>
        </div>
        <div>
          <dt>Budget</dt>
          <dd>{formatLoopBudget(contract.budget)}</dd>
        </div>
        {stopConditions.length > 0 && (
          <div>
            <dt>Done when</dt>
            <dd>{stopConditions.join(" · ")}</dd>
          </div>
        )}
      </dl>
    </section>
  );
}

function HandoffOverview({ handoff }: { handoff: LoopHandoff }) {
  const workspace = handoff.workspace || {};
  const artifact = handoff.artifact || {};
  const connector = handoff.connector || {};
  const artifactLabel = artifact.final_artifact
    ? stringValue(artifact.final_artifact.filename || artifact.final_artifact.id)
    : artifact.source_artifacts?.length ? `${artifact.source_artifacts.length} artifact(s)` : "";
  const rows = compactRows([
    ["Type", humanizeToken(handoff.type || "handoff")],
    ["Resume point", handoff.resume_point],
    ["Summary", handoff.summary],
    ["Workspace", [workspace.repo, workspace.branch, workspace.worktree].filter(Boolean).join(" · ")],
    ["Changed files", previewInlineList(workspace.changed_files || [], 3, "; ")],
    ["Tests", previewInlineList(workspace.test_commands || [], 2, "; ")],
    ["Artifacts", artifactLabel],
    ["Connector", [connector.provider, connector.risk_level].filter(Boolean).join(" · ")],
    ["Review", handoff.review_state],
    ["Next", handoff.recommended_action]
  ]);
  return (
    <section className="loop-contract-overview handoff-overview" aria-label="Handoff summary">
      <header>
        <strong>Handoff</strong>
        <span>{handoff.resume_available ? "Resume available" : humanizeToken(handoff.review_state || "current state")}</span>
      </header>
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

function GateDecisionNotice({ decision }: { decision: GateDecision }) {
  return (
    <section className="gate-decision-notice" aria-label="Gate decision">
      <header>
        <strong>{humanizeToken(decision.gate || "gate")} gate</strong>
        <span>{gateRecoveryLabel(decision)}</span>
      </header>
      <p>{decision.block_reason || "Execution is waiting for a gate decision."}</p>
      {decision.repair_hint && <small>{decision.repair_hint}</small>}
    </section>
  );
}

function ParallelJobOverview({ groups }: { groups: ParallelGroupTrace[] }) {
  return (
    <section className="parallel-job-overview" aria-label="Parallel branch overview">
      <header>
        <strong>Parallel Branches</strong>
        <span>{groups.reduce((sum, group) => sum + group.branches.length, 0)} branches</span>
      </header>
      {groups.map((group) => (
        <details key={group.id} open>
          <summary>
            <span>{group.id}</span>
            <small>{group.succeededCount}/{group.branchCount || group.branches.length} succeeded</small>
          </summary>
          <ParallelQualityPanel group={group} />
          <div className="parallel-job-branches">
            {group.branches.map((branch) => (
              <details key={branch.id} className={`parallel-job-branch ${branch.status}`}>
                <summary>
                  <strong>{branch.title}</strong>
                  <span>{branch.status}</span>
                </summary>
                <dl>
                  <div>
                    <dt>Objective</dt>
                    <dd>{branch.objective || branch.id}</dd>
                  </div>
                  {(branch.kind || branch.coverageDimension) && (
                    <div>
                      <dt>Scope</dt>
                      <dd>{[branch.kind, branch.coverageDimension].filter(Boolean).join(" · ")}</dd>
                    </div>
                  )}
                  <div>
                    <dt>Evidence</dt>
                    <dd>{branch.sourceCount} sources · {branch.artifactCount} artifacts · {branch.toolCallCount} tools</dd>
                  </div>
                  {branch.budget && (
                    <div>
                      <dt>Budget</dt>
                      <dd>{formatBranchBudget(branch.budget)}</dd>
                    </div>
                  )}
                  {branch.durationMs && (
                    <div>
                      <dt>Duration</dt>
                      <dd>{formatParallelDuration(branch.durationMs)}</dd>
                    </div>
                  )}
                  {branch.recommendedNextAction && (
                    <div>
                      <dt>Next</dt>
                      <dd>{branch.recommendedNextAction}</dd>
                    </div>
                  )}
                  {branch.error && (
                    <div>
                      <dt>Error</dt>
                      <dd>{branch.error}</dd>
                    </div>
                  )}
                </dl>
                {branch.sources.length > 0 && <ParallelSourceChips sources={branch.sources} />}
              </details>
            ))}
          </div>
        </details>
      ))}
    </section>
  );
}

function ParallelSourceChips({ sources }: { sources: ParallelGroupTrace["sources"] }) {
  const [expanded, setExpanded] = useState(false);
  const visibleSources = expanded ? sources : sources.slice(0, 6);
  const hiddenCount = sources.length - visibleSources.length;
  return (
    <div className="parallel-source-chips">
      {visibleSources.map((source, index) => {
        const label = source.title || source.provider || source.domain || `Source ${index + 1}`;
        return source.url ? (
          <a key={source.url} href={source.url} target="_blank" rel="noreferrer" title={source.url}>{label}</a>
        ) : (
          <span key={`${label}-${index}`}>{label}</span>
        );
      })}
      {sources.length > 6 && (
        <button type="button" onClick={() => setExpanded((value) => !value)}>
          {expanded ? "Show fewer sources" : `Show ${hiddenCount} more sources`}
        </button>
      )}
    </div>
  );
}

function loopContractFromEvents(events: JobEvent[]): LoopContract | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const data = eventData(events[index]);
    const contract = recordValue(data?.loop_contract);
    if (contract && stringValue(contract.id)) return contract as LoopContract;
  }
  return null;
}

function loopHandoffFromEvents(events: JobEvent[]): LoopHandoff | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const data = eventData(events[index]);
    const handoff = recordValue(data?.handoff || data?.loop_handoff);
    if (handoff && (stringValue(handoff.type) || stringValue(handoff.summary) || stringValue(handoff.resume_point))) return handoff as LoopHandoff;
    const state = recordValue(data?.deep_agent_state);
    const stateHandoff = recordValue(state?.handoff);
    if (stateHandoff && (stringValue(stateHandoff.type) || stringValue(stateHandoff.summary) || stringValue(stateHandoff.resume_point))) return stateHandoff as LoopHandoff;
  }
  return null;
}

function traceSummaryFromEvents(events: JobEvent[]): DeepAgentTraceSummary | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const data = eventData(events[index]);
    const summary = recordValue(data?.trace_summary);
    if (summary && (stringValue(summary.root_cause) || stringValue(summary.final_status))) {
      return summary as DeepAgentTraceSummary;
    }
  }
  const gate = latestBlockingGateDecision(events);
  if (gate) {
    return {
      final_status: gate.requires_review ? "review_required" : "blocked",
      root_cause: gate.block_reason || "Execution is waiting on a gate decision.",
      category: gate.category,
      failed_phase: gate.gate,
      failed_gate: gate.gate,
      suggested_repair: gate.repair_hint || gateRecoveryLabel(gate),
      top_evidence: gate.evidence_refs
    };
  }
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    const data = eventData(event);
    if (event.type === "deep_agent_evaluator_verdict" && data?.passed === false) {
      return {
        final_status: "needs_repair",
        root_cause: stringValue(data.reason) || displayValue(data.failed_criteria) || "Final evaluator failed.",
        category: "verifier_failed",
        failed_phase: "verify",
        suggested_repair: displayValue(data.repair_plan) || "Address failed evaluator criteria and rerun final verification.",
        top_evidence: stringArrayValue(data.failed_criteria)
      };
    }
    if (event.event?.error || event.type.endsWith("_failed")) {
      return {
        final_status: "failed",
        root_cause: event.event?.error || event.event?.content || "Job event failed.",
        category: traceCategoryFromText(`${event.type} ${event.event?.error || ""} ${JSON.stringify(data || {})}`),
        failed_phase: eventGroupForType(event.type),
        failed_tool: stringValue(data?.tool || data?.tool_name || data?.provider),
        suggested_repair: "Inspect the top evidence and resume from the latest safe checkpoint.",
        top_evidence: [event.type, stringValue(data?.step_id || data?.action_id || data?.run_id)].filter(Boolean)
      };
    }
  }
  return null;
}

function TraceSummaryCard({ summary }: { summary: DeepAgentTraceSummary }) {
  const rows = compactRows([
    ["Status", humanizeToken(summary.final_status || "unknown")],
    ["Category", humanizeToken(summary.category || "")],
    ["Phase", humanizeToken(summary.failed_phase || "")],
    ["Gate", humanizeToken(summary.failed_gate || "")],
    ["Tool", summary.failed_tool],
    ["Repair", summary.suggested_repair],
    ["Evidence", previewInlineList(summary.top_evidence || [], 4, " · ")]
  ]);
  return (
    <section className="loop-contract-overview trace-summary-card" aria-label="Root cause summary">
      <header>
        <strong>Root Cause</strong>
        <span>{humanizeToken(summary.category || summary.final_status || "trace summary")}</span>
      </header>
      <p>{summary.root_cause || "No root cause available yet."}</p>
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
    </section>
  );
}

function formatBranchBudget(budget: ParallelBranchBudget): string {
  const parts = [
    budget.timeout_ms ? `${formatParallelDuration(budget.timeout_ms)} timeout` : "",
    budget.max_tool_calls ? `${budget.max_tool_calls} tools` : "",
    budget.max_sources ? `${budget.max_sources} sources` : "",
    budget.max_tokens ? `${budget.max_tokens} tokens` : ""
  ].filter(Boolean);
  return parts.length ? parts.join(" · ") : "Default";
}

function stringArrayValue(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => stringValue(item)).filter(Boolean);
}

function traceCategoryFromText(text: string): string {
  const lower = text.toLowerCase();
  if (/auth|oauth|permission|unauthorized|forbidden|reconnect/.test(lower)) return "auth";
  if (/timeout|deadline|timed out/.test(lower)) return "timeout";
  if (/budget|max actions|max duration|quota|rate limit/.test(lower)) return "budget";
  if (/source|citation|coverage|low score/.test(lower)) return "source_quality";
  if (/empty|model_empty|no output/.test(lower)) return "model_empty";
  if (/schema|json|invalid argument|tool_result/.test(lower)) return "tool_schema";
  if (/artifact|file missing|missing file/.test(lower)) return "artifact_missing";
  if (/connector|mcp|unavailable/.test(lower)) return "connector_unavailable";
  if (/verifier|verification|evaluator/.test(lower)) return "verifier_failed";
  return "unknown";
}

function latestBlockingGateDecision(events: JobEvent[]): GateDecision | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const data = eventData(events[index]);
    const decision = recordValue(data?.gate_decision);
    if (!decision) continue;
    if (decision.allow === false || decision.requires_review === true) return decision as GateDecision;
  }
  return null;
}

function gateRecoveryLabel(decision: GateDecision): string {
  if (decision.requires_review) {
    if (decision.category === "auth") return "Reconnect required";
    return "Review required";
  }
  switch (decision.category) {
    case "auth":
      return "Reconnect required";
    case "budget":
      return "Increase budget";
    case "source":
      return "Needs sources";
    case "artifact":
      return "Needs artifact";
    case "tool":
      return "Retry with another tool";
    default:
      return "Needs repair";
  }
}

function formatLoopDeliverable(deliverable?: LoopContract["deliverable"]): string {
  const parts = [
    humanizeToken(deliverable?.type || ""),
    deliverable?.format,
    deliverable?.filename_hint
  ].filter(Boolean);
  return parts.length ? parts.join(" · ") : "Answer";
}

function formatLoopBudget(budget?: LoopContract["budget"]): string {
  if (!budget) return "Default";
  const parts = [
    budget.max_steps ? `${budget.max_steps} steps` : "",
    budget.max_actions ? `${budget.max_actions} actions` : "",
    budget.max_duration_ms ? formatDuration(budget.max_duration_ms) : ""
  ].filter(Boolean);
  return parts.length ? parts.join(" · ") : "Default";
}

function humanizeToken(value?: string): string {
  return value ? value.replace(/[_-]+/g, " ") : "";
}

function ParallelQualityPanel({ group }: { group: ParallelGroupTrace }) {
  const coverage = typeof group.coverageScore === "number" ? Math.round(group.coverageScore * 100) : undefined;
  const hasDetails = group.missingCoverage.length > 0 || group.conflicts.length > 0 || group.uncertaintyNotes.length > 0;
  if (coverage === undefined && !hasDetails) return null;
  return (
    <div className="parallel-quality-panel">
      <div className="parallel-quality-strip">
        {coverage !== undefined && <span className="parallel-quality-pill">{coverage}% coverage</span>}
        <span className={`parallel-quality-pill ${group.missingCoverage.length ? "warning" : ""}`}>
          {group.missingCoverage.length ? `${group.missingCoverage.length} missing` : "complete"}
        </span>
        <span className={`parallel-quality-pill ${group.conflictCount ? "warning" : ""}`}>
          {group.conflictCount ? `${group.conflictCount} conflicts` : "no conflicts"}
        </span>
        {group.supplementalBranchCount ? <span className="parallel-quality-pill">{group.supplementalBranchCount} supplemental</span> : null}
      </div>
      {group.missingCoverage.length > 0 && (
        <p><strong>Missing coverage</strong> {group.missingCoverage.join(", ")}</p>
      )}
      {group.conflicts.length > 0 && (
        <div className="parallel-conflict-list">
          {group.conflicts.slice(0, 4).map((conflict) => (
            <p key={`${conflict.field}-${conflict.subject || "default"}`}>
              <strong>{conflict.field}{conflict.subject ? `/${conflict.subject}` : ""}</strong>
              <span>{previewInlineList(conflict.values, 4, " vs ")}</span>
            </p>
          ))}
          {group.conflicts.length > 4 && <p className="parallel-list-more">+{group.conflicts.length - 4} more conflicts</p>}
        </div>
      )}
      {group.uncertaintyNotes.length > 0 && (
        <p><strong>Uncertainty</strong> {previewInlineList(group.uncertaintyNotes, 2, " · ")}</p>
      )}
    </div>
  );
}

type TraceFilter = "all" | "model" | "tool" | "artifact" | "browser" | "verifier" | "parallel" | "error";

const traceFilters: Array<{ id: TraceFilter; label: string }> = [
  { id: "all", label: "All" },
  { id: "model", label: "Model" },
  { id: "tool", label: "Tool" },
  { id: "artifact", label: "Artifact" },
  { id: "browser", label: "Browser" },
  { id: "verifier", label: "Verifier" },
  { id: "parallel", label: "Parallel" },
  { id: "error", label: "Error" }
];

function visibleJobEvents(events: JobEvent[]): JobEvent[] {
  return events.filter((event) => !(event.type === "delta" && event.event?.role === "assistant"));
}

function eventMatchesFilter(event: JobEvent, filter: TraceFilter): boolean {
  if (filter === "all") return true;
  const data = eventData(event);
  if (filter === "error") return eventStatus(event, data) === "failed";
  const text = `${event.type} ${event.event?.content || ""} ${event.event?.error || ""} ${JSON.stringify(data || {})}`.toLowerCase();
  if (filter === "model") return /\bmodel\b|llm|prompt|token/.test(text);
  if (filter === "tool") return /tool|skill|mcp|connector|sandbox/.test(text);
  if (filter === "artifact") return /artifact|document|docx|image|file/.test(text);
  if (filter === "browser") return /browser|web|fetch|search|url|http/.test(text);
  if (filter === "verifier") return /verif|evaluat|test|check|review|gate/.test(text);
  if (filter === "parallel") return /parallel|branch|fan-out|fan out/.test(text);
  return true;
}

function copyJobTrace(events: JobEvent[]) {
  navigator.clipboard?.writeText(JSON.stringify(jobTracePayload(events), null, 2)).catch(() => {});
}

function downloadJobTrace(events: JobEvent[]) {
  const blob = new Blob([JSON.stringify(jobTracePayload(events), null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `job-trace-${events[0]?.job_id || "events"}.json`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function jobTracePayload(events: JobEvent[]) {
  return {
    job_id: events[0]?.job_id || "",
    event_count: events.length,
    trace_summary: traceSummaryFromEvents(events),
    events
  };
}

type JobEventGroup = {
  id: string;
  label: string;
  description: string;
  events: JobEvent[];
};

function groupJobEvents(events: JobEvent[]): JobEventGroup[] {
  const order = ["run", "parallel", "steps", "outputs"];
  const labels: Record<string, { label: string; description: string }> = {
    run: {
      label: "Run",
      description: "Workflow lifecycle, status changes, and blocking errors"
    },
    parallel: {
      label: "Parallel",
      description: "Branch fan-out, branch completion, and join synthesis"
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
  if (type === "gate" || type === "deep_agent_gate_decision") return "run";
  if (type === "artifact_output" || type === "deep_agent_artifact_output" || type.startsWith("artifact")) return "outputs";
  if (type.startsWith("deep_agent_parallel_") || type.startsWith("deep_research_") || type === "parallel_workflow") return "parallel";
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
  const gateDecision = recordValue(data?.gate_decision);
  const evidenceDiagnostics = recordValue(evidence?.diagnostics);
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
    ["Worker", stringValue(data?.worker_title || data?.worker_id)],
    ["Worker role", stringValue(data?.worker_role)],
    ["Agent run", stringValue(data?.agent_run_id)],
    ["Action ID", stringValue(data?.action_id || data?.action_hash)],
    ["Plan step", stringValue(data?.step_id || data?.step_title)],
    ["Tool", stringValue(data?.tool)],
    ["Skill", stringValue(data?.skill_name)],
    ["Query", stringValue(data?.query)],
    ["Input summary", stringValue(data?.input_summary || data?.prompt_preview || data?.query || event.event?.content)],
    ["Prompt", stringValue(data?.prompt_preview)],
    ["Attempt", stringValue(data?.attempt || data?.attempt_strategy)]
  ]);
  const resultRows = compactRows([
    ["Result", stringValue(data?.result_status)],
    ["Completed", boolString(data?.completed)],
    ["Output summary", stringValue(data?.output_summary || data?.summary || evidence?.summary || resultMetadata?.summary)],
    ["Artifacts", stringValue(resultMetadata?.artifact_count)],
    ["Child job", stringValue(resultMetadata?.job_id)],
    ["Tool valid", boolString(resultMetadata?.tool_result_valid)],
    ["Error class", stringValue(data?.error_class || resultMetadata?.error_class)],
    ["Error", displayError(event.event?.error || stringValue(data?.error))]
  ]);
  const evidenceRows = compactRows([
    ["Evidence ID", stringValue(data?.evidence_id || evidence?.action_id)],
    ["Sources", displaySourcesPreview(data?.sources)],
    ["Tool calls", displayListPreview(data?.tool_calls, "tool call")],
    ["Artifacts", displayListPreview(data?.artifact_refs, "artifact")],
    ["Child jobs", displayListPreview(data?.child_jobs, "child job")],
    ["Evidence summary", stringValue(evidence?.summary)]
  ]);
  const handoff = recordValue(data?.handoff || data?.loop_handoff);
  const handoffWorkspace = recordValue(handoff?.workspace);
  const handoffRows = compactRows([
    ["Type", humanizeToken(stringValue(handoff?.type))],
    ["Resume point", stringValue(handoff?.resume_point)],
    ["Summary", stringValue(handoff?.summary)],
    ["Changed files", displayValue(handoffWorkspace?.changed_files)],
    ["Rollback", stringValue(handoffWorkspace?.rollback_plan)],
    ["Next", stringValue(handoff?.recommended_action)]
  ]);
  const gateRows = compactRows([
    ["Gate", humanizeToken(stringValue(gateDecision?.gate || data?.gate))],
    ["Category", humanizeToken(stringValue(gateDecision?.category || data?.category))],
    ["Allowed", boolString(gateDecision?.allow ?? data?.allow)],
    ["Needs review", boolString(gateDecision?.requires_review ?? data?.requires_review)],
    ["Reason", stringValue(gateDecision?.block_reason || data?.block_reason)],
    ["Recovery", stringValue(gateDecision?.repair_hint || data?.repair_hint)],
    ["Evidence", displayValue(gateDecision?.evidence_refs || data?.evidence_refs)]
  ]);
  const evaluatorRows = compactRows([
    ["Verdict", humanizeToken(stringValue(data?.verdict))],
    ["Passed", boolString(data?.passed)],
    ["Failed criteria", displayValue(data?.failed_criteria)],
    ["Repair plan", displayValue(data?.repair_plan)],
    ["Source coverage", displayValue(data?.source_coverage)],
    ["Rubric coverage", displayValue(data?.rubric_coverage)]
  ]);
  const metricRows = compactRows([
    ["Duration", formatDuration(data?.duration_ms || resultMetadata?.duration_ms || evidenceDiagnostics?.duration_ms)],
    ["Tokens", stringValue(data?.token_estimate || resultMetadata?.token_estimate || evidenceDiagnostics?.token_estimate)],
    ["Cost", formatCost(data?.cost || data?.cost_usd || data?.estimated_cost_usd || resultMetadata?.estimated_cost_usd || evidenceDiagnostics?.estimated_cost_usd)]
  ]).concat(rowsFromRecord(recordValue(data?.metrics)));
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
        {gateRows.length > 0 && <DetailGroup title="Gate" rows={gateRows} />}
        {evaluatorRows.length > 0 && <DetailGroup title="Evaluator" rows={evaluatorRows} />}
        {handoffRows.length > 0 && <DetailGroup title="Handoff" rows={handoffRows} />}
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
  if (event.type === "deep_agent_loop_contract") return "Loop contract";
  if (event.type === "deep_agent_evaluator_verdict") return `Evaluator · ${humanizeToken(stringValue(data?.verdict) || "verdict")}`;
  if (event.type === "deep_agent_gate_decision") return `${humanizeToken(stringValue(data?.gate) || "gate")} gate`;
  if (event.type === "deep_agent_artifact_output") {
    const artifact = recordValue(data?.artifact);
    return ["artifact", stringValue(artifact?.filename || artifact?.id)].filter(Boolean).join(" · ");
  }
  if (event.type === "deep_agent_child_job") {
    const child = recordValue(data?.child_job);
    return ["child job", stringValue(child?.id), stringValue(child?.status)].filter(Boolean).join(" · ");
  }
  if (event.type.startsWith("deep_research_worker_")) {
    const worker = stringValue(data?.worker_title || data?.worker_id || data?.branch_title);
    const label = event.type.replace(/^deep_research_worker_/, "").replace(/_/g, " ");
    return ["deep research worker", label, worker].filter(Boolean).join(" · ");
  }
  if (event.type.startsWith("deep_research_")) {
    return ["deep research", event.type.replace(/^deep_research_/, "").replace(/_/g, " ")].join(" · ");
  }
  if (event.type.startsWith("deep_agent_parallel_")) {
    const branch = stringValue(data?.branch_title || data?.branch_id);
    const group = stringValue(data?.parallel_group_id);
    const label = event.type.replace(/^deep_agent_parallel_/, "").replace(/_/g, " ");
    return ["parallel", label, branch || group].filter(Boolean).join(" · ");
  }
  if (event.type === "deep_agent_connectors_planned") {
    return ["connectors planned", connectorNames(data)].filter(Boolean).join(" · ");
  }
  if (event.type.startsWith("connector_tool_call_") || event.type.startsWith("mcp_connector_tool_call_")) {
    return ["connector tool", stringValue(data?.tool_name || event.event?.content), stringValue(data?.provider)].filter(Boolean).join(" · ");
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
  if (stringValue(data?.title)) return stringValue(data?.title);
  return event.type;
}

function connectorNames(data: Record<string, unknown> | null): string {
  const raw = data?.connectors;
  if (!Array.isArray(raw)) return "";
  return raw.map((item) => {
    const record = recordValue(item);
    return stringValue(record?.provider || item);
  }).filter(Boolean).join(", ");
}

function eventSubtitle(event: JobEvent, data: Record<string, unknown> | null): string {
  if (event.type === "deep_agent_loop_contract") {
    const contract = recordValue(data?.loop_contract);
    const deliverable = recordValue(contract?.deliverable);
    return [
      humanizeToken(stringValue(contract?.task_type)),
      stringValue(deliverable?.format || deliverable?.type)
    ].filter(Boolean).join(" · ") || "Execution contract established";
  }
  if (event.type === "deep_agent_gate_decision") {
    return stringValue(data?.block_reason) || (data?.allow === true ? "Allowed" : "Blocked");
  }
  if (event.type === "deep_agent_evaluator_verdict") {
    const failed = data?.failed_criteria;
    return data?.passed === true ? "Final evaluator passed" : displayValue(failed) || stringValue(data?.repair_plan) || "Final evaluator requires repair";
  }
  return event.event?.error || stringValue(data?.detail) || event.event?.content || event.event?.job_reason || stringValue(data?.status) || event.id;
}

function eventStatus(event: JobEvent, data: Record<string, unknown> | null): string {
  if (event.type === "deep_agent_gate_decision" && data?.allow === false) return "failed";
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

function displayError(value: unknown): string {
  const message = stringValue(value);
  return message ? userFacingErrorMessage(message) : "";
}

function boolString(value: unknown): string {
  return typeof value === "boolean" ? String(value) : "";
}

function displayValue(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

function displaySourcesPreview(value: unknown): string {
  if (!Array.isArray(value)) return displayValue(value);
  const items = value.map((item, index) => {
    const record = recordValue(item);
    if (!record) return truncateDetailText(displayValue(item));
    const label = stringValue(record.title || record.url || record.provider) || `Source ${index + 1}`;
    const url = stringValue(record.url);
    return truncateDetailText(url && url !== label ? `${label} (${url})` : label);
  }).filter(Boolean);
  return previewItems(items, 6, "sources");
}

function displayListPreview(value: unknown, itemLabel: string): string {
  if (!Array.isArray(value)) return displayValue(value);
  const items = value.map((item, index) => {
    const record = recordValue(item);
    if (!record) return truncateDetailText(displayValue(item));
    const label = stringValue(record.name || record.title || record.filename || record.id || record.job_id) || `${itemLabel} ${index + 1}`;
    const status = stringValue(record.status || record.result_status);
    return truncateDetailText(status ? `${label} (${status})` : label);
  }).filter(Boolean);
  return previewItems(items, 6, `${itemLabel}s`);
}

function previewItems(items: string[], limit: number, label: string): string {
  if (!items.length) return "";
  const visible = items.slice(0, limit);
  const suffix = items.length > limit ? `; +${items.length - limit} more ${label}` : "";
  return `${visible.join("; ")}${suffix}`;
}

function previewInlineList(values: string[], limit: number, separator: string): string {
  const visible = values.map((item) => truncateDetailText(item, 64)).filter(Boolean).slice(0, limit);
  if (!visible.length) return "";
  const suffix = values.length > limit ? `${separator}+${values.length - limit} more` : "";
  return `${visible.join(separator)}${suffix}`;
}

function truncateDetailText(value: string, max = 160): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  if (normalized.length <= max) return normalized;
  return `${normalized.slice(0, Math.max(0, max - 1)).trimEnd()}…`;
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

function jobWithTerminalEvents(job: Job, events: JobEvent[]): Job {
  const terminal = latestTerminalJobEvent(events, job.id);
  if (!terminal || terminal.status === job.status) return job;
  return {
    ...job,
    status: terminal.status,
    error: job.error || terminal.event.error,
    updated_at: terminal.created_at || job.updated_at,
    finished_at: job.finished_at || terminal.created_at
  };
}

function latestTerminalJobEvent(events: JobEvent[], jobId?: string): { status: JobStatus; event: RuntimeEvent; created_at: string } | null {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (jobId && event.job_id !== jobId) continue;
    const status = terminalJobStatusFromRuntimeEvent(event.event);
    if (!status) continue;
    return { status, event: event.event, created_at: event.created_at };
  }
  return null;
}

function terminalJobStatusFromRuntimeEvent(event?: RuntimeEvent): JobStatus | "" {
  const jobStatus = event?.job?.status || "";
  if (terminalJobs.has(jobStatus)) return jobStatus as JobStatus;
  switch (event?.type) {
    case "done":
    case "workflow_run_succeeded":
      return "succeeded";
    case "error":
    case "workflow_run_failed":
      return "failed";
    case "cancelled":
    case "workflow_run_cancelled":
      return "cancelled";
    default:
      return "";
  }
}

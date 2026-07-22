import { describe, expect, it } from "vitest";
import {
  chatActivityTerminalStatus,
  chatRunStreamTerminalEvent,
  mergeFetchedJobsWithLocalTerminalState,
  restorableSessionJob,
  terminalJobStatusFromRuntimeEvent
} from "../features/workspace/AgentWorkspace";
import type { Job, JobEvent, RuntimeEvent } from "../types";

function buildJob(overrides: Partial<Job> = {}): Job {
  return {
    id: "job-1",
    session_id: "session-1",
    type: "deep_research",
    status: "running",
    content: "Investigate",
    created_at: "2026-07-22T10:00:00Z",
    updated_at: "2026-07-22T10:00:00Z",
    ...overrides
  };
}

function buildJobEvent(overrides: Partial<JobEvent> = {}): JobEvent {
  const event: RuntimeEvent = overrides.event || { type: "done", job: { status: "succeeded" } as Job };
  return {
    id: "evt-1",
    job_id: "job-1",
    type: event.type,
    event,
    created_at: "2026-07-22T10:05:00Z",
    ...overrides
  };
}

describe("AgentWorkspace deep research job handling", () => {
  it("does not treat workflow sub-run terminal events as job terminal", () => {
    expect(terminalJobStatusFromRuntimeEvent({ type: "workflow_run_succeeded" })).toBe("");
    expect(terminalJobStatusFromRuntimeEvent({ type: "workflow_run_failed" })).toBe("");
    expect(terminalJobStatusFromRuntimeEvent({ type: "workflow_run_cancelled" })).toBe("");
    expect(terminalJobStatusFromRuntimeEvent({ type: "done" })).toBe("succeeded");
  });

  it("keeps deep research worker failures and completion markers non-terminal for overall activity", () => {
    expect(chatActivityTerminalStatus({ type: "deep_research_worker_failed" })).toBe("");
    expect(chatActivityTerminalStatus({ type: "deep_research_completed" })).toBe("");
    expect(chatActivityTerminalStatus({ type: "deep_agent_completed" })).toBe("");
    expect(chatActivityTerminalStatus({ type: "deep_agent_failed" })).toBe("");
    expect(chatRunStreamTerminalEvent({ type: "job_handoff" })).toBe(true);
    expect(chatActivityTerminalStatus({ type: "job_handoff" })).toBe("");
  });

  it("restores the preferred active background job for the current session", () => {
    const jobs = [
      buildJob({ id: "job-older", session_id: "session-1", status: "running" }),
      buildJob({ id: "job-active", session_id: "session-1", status: "queued" }),
      buildJob({ id: "job-other", session_id: "session-2", status: "running" }),
      buildJob({ id: "job-done", session_id: "session-1", status: "succeeded" })
    ];

    expect(restorableSessionJob(jobs, "session-1", "job-active")?.id).toBe("job-active");
    expect(restorableSessionJob(jobs, "session-1", "missing")?.id).toBe("job-older");
    expect(restorableSessionJob(jobs, "session-3")).toBeNull();
  });

  it("preserves a locally confirmed terminal job over a stale refresh", () => {
    const current = [buildJob({ status: "succeeded", updated_at: "2026-07-22T10:10:00Z", finished_at: "2026-07-22T10:10:00Z" })];
    const fetched = [buildJob({ status: "running", updated_at: "2026-07-22T10:09:00Z" })];

    const merged = mergeFetchedJobsWithLocalTerminalState(fetched, current, []);

    expect(merged[0].status).toBe("succeeded");
    expect(merged[0].finished_at).toBe("2026-07-22T10:10:00Z");
  });

  it("applies an authoritative terminal job event during refresh reconciliation", () => {
    const fetched = [buildJob({ status: "running" })];
    const events = [buildJobEvent({
      event: { type: "done", job: buildJob({ status: "succeeded" }) },
      created_at: "2026-07-22T10:11:00Z"
    })];

    const merged = mergeFetchedJobsWithLocalTerminalState(fetched, [], events);

    expect(merged[0].status).toBe("succeeded");
    expect(merged[0].finished_at).toBe("2026-07-22T10:11:00Z");
  });
});

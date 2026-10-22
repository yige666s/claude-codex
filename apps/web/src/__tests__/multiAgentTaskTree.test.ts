import { describe, expect, it } from "vitest";
import type { JobEvent } from "../types";
import { multiAgentWorkers } from "../features/workspace/components/MultiAgentTaskTree";

const event = (id: string, type: string, data: unknown): JobEvent => ({
  id, job_id: "job-1", type, created_at: "2026-10-22T00:00:00Z",
  event: { type, data }
});

describe("multi-agent task tree", () => {
  it("tracks worker status from replayed events", () => {
    expect(multiAgentWorkers([
      event("1", "multi_agent_worker_started", { child_session_id: "child-1", parent_session_id: "parent", description: "Inspect API", status: "started" }),
      event("2", "multi_agent_worker_succeeded", JSON.stringify({ child_session_id: "child-1", parent_session_id: "parent", description: "Inspect API", status: "succeeded" })),
      event("3", "multi_agent_worker_failed", { child_session_id: "child-2", description: "Review tests" })
    ])).toEqual([
      { id: "child-1", parentSessionId: "parent", description: "Inspect API", status: "succeeded" },
      { id: "child-2", parentSessionId: "", description: "Review tests", status: "failed" }
    ]);
  });
});

import type { JobEvent } from "../../../types";

type WorkerStatus = "running" | "succeeded" | "failed";

export type MultiAgentWorker = {
  id: string;
  parentSessionId: string;
  description: string;
  status: WorkerStatus;
};

function eventPayload(event: JobEvent): Record<string, unknown> {
  const raw = event.event?.data;
  if (raw && typeof raw === "object" && !Array.isArray(raw)) return raw as Record<string, unknown>;
  if (typeof raw === "string") {
    try {
      const parsed: unknown = JSON.parse(raw);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return parsed as Record<string, unknown>;
    } catch { /* Ignore malformed historical events. */ }
  }
  return {};
}

export function multiAgentWorkers(events: JobEvent[]): MultiAgentWorker[] {
  const workers = new Map<string, MultiAgentWorker>();
  for (const event of events) {
    if (!event.type.startsWith("multi_agent_worker_")) continue;
    const status = event.type.slice("multi_agent_worker_".length);
    if (status !== "started" && status !== "succeeded" && status !== "failed") continue;
    const data = eventPayload(event);
    const id = typeof data.child_session_id === "string" ? data.child_session_id : "";
    if (!id) continue;
    const previous = workers.get(id);
    workers.set(id, {
      id,
      parentSessionId: typeof data.parent_session_id === "string" ? data.parent_session_id : previous?.parentSessionId || "",
      description: typeof data.description === "string" && data.description ? data.description : previous?.description || "Worker",
      status: status === "started" ? "running" : status
    });
  }
  return [...workers.values()];
}

export function MultiAgentTaskTree({ events }: { events: JobEvent[] }) {
  const workers = multiAgentWorkers(events);
  return (
    <section className="multi-agent-tree" aria-label="Multi-agent tasks">
      <header><strong>Agent tasks</strong><small>{workers.length} worker{workers.length === 1 ? "" : "s"}</small></header>
      {!workers.length && <p>Waiting for the coordinator to delegate tasks…</p>}
      <ul>
        {workers.map((worker) => (
          <li key={worker.id}>
            <span className={`multi-agent-worker-status ${worker.status}`}>{worker.status}</span>
            <span>{worker.description}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

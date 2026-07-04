import type { AgentActivityItem, JobEvent } from "../../../types";

export type ParallelBranchTrace = {
  id: string;
  title: string;
  objective?: string;
  kind?: string;
  coverageDimension?: string;
  budget?: ParallelBranchBudget;
  recommendedNextAction?: string;
  missingCoverage: string[];
  status: "queued" | "running" | "succeeded" | "failed" | "timed_out" | "cancelled" | "default";
  sourceCount: number;
  artifactCount: number;
  toolCallCount: number;
  durationMs?: number;
  error?: string;
  sources: ParallelSourceRef[];
  artifacts: ParallelArtifactRef[];
  toolCalls: ParallelToolCallRef[];
};

export type ParallelBranchBudget = {
  timeout_ms?: number;
  max_tool_calls?: number;
  max_sources?: number;
  max_tokens?: number;
};

export type ParallelGroupTrace = {
  id: string;
  status: ParallelBranchTrace["status"];
  joined?: boolean;
  joinedStatus?: ParallelBranchTrace["status"];
  branchCount: number;
  succeededCount: number;
  failedCount: number;
  minSuccessfulBranches?: number;
  coverageScore?: number;
  coverageRequired: string[];
  coverageCovered: string[];
  missingCoverage: string[];
  conflictCount: number;
  conflicts: ParallelConflictTrace[];
  uncertaintyNotes: string[];
  partialSynthesis?: boolean;
  supplementalBranchCount?: number;
  branches: ParallelBranchTrace[];
  sources: ParallelSourceRef[];
  artifacts: ParallelArtifactRef[];
};

export type ParallelConflictTrace = {
  field: string;
  subject?: string;
  kind?: string;
  values: string[];
  branches: string[];
  evidence: string[];
  confidence?: number;
  reason?: string;
};

export type ParallelSourceRef = {
  id?: string;
  title?: string;
  url?: string;
  provider?: string;
  snippet?: string;
  domain?: string;
  quality?: string;
  quality_score?: number;
  source_kind?: string;
  score_reasons?: string[];
};

export type ParallelArtifactRef = {
  id?: string;
  filename?: string;
  content_type?: string;
  job_id?: string;
};

export type ParallelToolCallRef = {
  id?: string;
  name?: string;
  status?: string;
};

type TraceEventLike = {
  type: string;
  data: Record<string, unknown>;
};

export function buildParallelGroupsFromActivity(items: AgentActivityItem[]): ParallelGroupTrace[] {
  return buildParallelGroups(items.map((item) => ({
    type: stringValue(item.metadata?.event_type) || item.type,
    data: item.metadata || {}
  })));
}

export function buildParallelGroupsFromJobEvents(events: JobEvent[]): ParallelGroupTrace[] {
  return buildParallelGroups(events.map((event) => ({
    type: event.type,
    data: recordValue(event.event?.data) || {}
  })));
}

export function formatParallelDuration(ms?: number): string {
  if (!Number.isFinite(ms || Number.NaN) || !ms || ms <= 0) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} s`;
}

function buildParallelGroups(events: TraceEventLike[]): ParallelGroupTrace[] {
  const groups = new Map<string, ParallelGroupTrace>();
  const ensureGroup = (id: string): ParallelGroupTrace => {
    const existing = groups.get(id);
    if (existing) return existing;
    const created: ParallelGroupTrace = {
      id,
      status: "running",
      branchCount: 0,
      succeededCount: 0,
      failedCount: 0,
      coverageRequired: [],
      coverageCovered: [],
      missingCoverage: [],
      conflictCount: 0,
      conflicts: [],
      uncertaintyNotes: [],
      branches: [],
      sources: [],
      artifacts: []
    };
    groups.set(id, created);
    return created;
  };

  for (const event of events) {
    if (!event.type.startsWith("deep_agent_parallel_")) {
      mergeBranchResultsFromRecord(groups, event.data);
      continue;
    }
    const groupID = stringValue(event.data.parallel_group_id) || "parallel";
    const group = ensureGroup(groupID);
    const branchID = stringValue(event.data.branch_id);
    mergeQualityFromRecord(group, event.data);
    if (event.type === "deep_agent_parallel_group_started") {
      group.branchCount = Math.max(group.branchCount, numberValue(event.data.branch_count) || 0);
      group.status = "running";
      continue;
    }
    if (event.type === "deep_agent_parallel_group_joined") {
      group.branchCount = Math.max(group.branchCount, numberValue(event.data.branch_count) || group.branches.length);
      group.succeededCount = numberValue(event.data.succeeded_branch_count) || group.succeededCount;
      group.failedCount = numberValue(event.data.failed_branch_count) || group.failedCount;
      group.minSuccessfulBranches = numberValue(event.data.min_successful_branches) || group.minSuccessfulBranches;
      group.joined = true;
      group.joinedStatus = statusValue(event.data.result_status) || group.joinedStatus;
      group.status = group.joinedStatus || (group.succeededCount >= Math.max(1, group.minSuccessfulBranches || 1) ? "succeeded" : "failed");
      mergeBranchResultsFromRecord(groups, event.data);
      continue;
    }
    if (!branchID) {
      mergeBranchResultsFromRecord(groups, event.data);
      continue;
    }
    const branch = ensureBranch(group, branchID, stringValue(event.data.branch_title));
    branch.objective = stringValue(event.data.objective) || branch.objective;
    branch.kind = stringValue(event.data.branch_kind) || branch.kind;
    branch.coverageDimension = stringValue(event.data.coverage_dimension) || branch.coverageDimension;
    branch.budget = budgetValue(event.data.branch_budget) || branch.budget;
    branch.sourceCount = numberValue(event.data.source_count) || branch.sourceCount;
    branch.artifactCount = numberValue(event.data.artifact_count) || branch.artifactCount;
    branch.toolCallCount = numberValue(event.data.tool_call_count) || branch.toolCallCount;
    branch.durationMs = numberValue(event.data.duration_ms) || branch.durationMs;
    branch.error = stringValue(event.data.error) || branch.error;
    branch.status = booleanValue(event.data.timed_out)
      ? "timed_out"
      : event.type.endsWith("_started")
      ? "running"
      : event.type.endsWith("_failed")
        ? "failed"
        : event.type.endsWith("_succeeded")
          ? "succeeded"
          : branch.status;
  }

  for (const group of groups.values()) {
    group.branches.sort((a, b) => a.id.localeCompare(b.id));
    group.branchCount = Math.max(group.branchCount, group.branches.length);
    group.succeededCount = group.branches.filter((branch) => branch.status === "succeeded").length || group.succeededCount;
    group.failedCount = group.branches.filter((branch) => branch.status === "failed").length || group.failedCount;
    group.sources = dedupeSources(group.branches.flatMap((branch) => branch.sources));
    group.artifacts = group.branches.flatMap((branch) => branch.artifacts);
    if (group.joinedStatus) {
      group.status = group.joinedStatus;
    } else if (group.status === "running" && group.branchCount > 0 && group.succeededCount + group.failedCount >= group.branchCount) {
      const minimum = Math.max(1, group.minSuccessfulBranches || group.branchCount);
      group.status = group.succeededCount >= minimum ? "succeeded" : "failed";
    }
  }
  return [...groups.values()].sort((a, b) => a.id.localeCompare(b.id));
}

function mergeBranchResultsFromRecord(groups: Map<string, ParallelGroupTrace>, record: Record<string, unknown>) {
  for (const results of branchResultArrays(record)) {
    const groupID = stringValue(record.parallel_group_id) || "parallel";
    const group = groups.get(groupID) || {
      id: groupID,
      status: "default" as const,
      branchCount: 0,
      succeededCount: 0,
      failedCount: 0,
      coverageRequired: [],
      coverageCovered: [],
      missingCoverage: [],
      conflictCount: 0,
      conflicts: [],
      uncertaintyNotes: [],
      branches: [],
      sources: [],
      artifacts: []
    };
    groups.set(groupID, group);
    mergeQualityFromRecord(group, record);
    for (const raw of results) {
      const result = recordValue(raw);
      if (!result) continue;
      const id = stringValue(result.id);
      if (!id) continue;
      const branch = ensureBranch(group, id, stringValue(result.title));
      branch.status = statusValue(result.status) || branch.status;
      branch.error = stringValue(result.error) || branch.error;
      const contribution = recordValue(result.contribution);
      const metadata = recordValue(result.metadata);
      branch.kind = stringValue(contribution?.kind) || stringValue(metadata?.branch_kind) || branch.kind;
      branch.coverageDimension = stringValue(contribution?.coverage_dimension) || stringValue(metadata?.coverage_dimension) || branch.coverageDimension;
      branch.budget = budgetValue(metadata?.branch_budget) || branch.budget;
      branch.recommendedNextAction = stringValue(contribution?.recommended_next_action) || branch.recommendedNextAction;
      branch.missingCoverage = mergeStringArrays(branch.missingCoverage, stringArray(contribution?.missing_coverage));
      if (booleanValue(metadata?.timed_out)) branch.status = "timed_out";
      branch.sources = sourceRefs(result.sources);
      branch.artifacts = artifactRefs(result.artifacts);
      branch.toolCalls = toolCallRefs(result.tool_calls);
      branch.sourceCount = Math.max(branch.sourceCount, branch.sources.length);
      branch.artifactCount = Math.max(branch.artifactCount, branch.artifacts.length);
      branch.toolCallCount = Math.max(branch.toolCallCount, branch.toolCalls.length);
      branch.durationMs = numberValue(metadata?.duration_ms) || branch.durationMs;
    }
  }
}

function mergeQualityFromRecord(group: ParallelGroupTrace, record: Record<string, unknown>) {
  const score = numberValue(record.coverage_score);
  if (score !== undefined) group.coverageScore = score;
  group.coverageRequired = mergeStringArrays(group.coverageRequired, stringArray(record.coverage_required));
  group.coverageCovered = mergeStringArrays(group.coverageCovered, stringArray(record.coverage_covered));
  group.missingCoverage = mergeStringArrays(group.missingCoverage, stringArray(record.missing_coverage));
  const conflicts = conflictRefs(record.parallel_conflicts);
  if (conflicts.length > 0) group.conflicts = conflicts;
  group.conflictCount = numberValue(record.conflict_count) || group.conflicts.length || group.conflictCount;
  group.uncertaintyNotes = mergeStringArrays(group.uncertaintyNotes, stringArray(record.uncertainty_notes));
  const partial = booleanValue(record.partial_synthesis);
  if (partial !== undefined) group.partialSynthesis = partial;
  group.supplementalBranchCount = numberValue(record.supplemental_branch_count) || group.supplementalBranchCount;
}

function branchResultArrays(record: Record<string, unknown>): unknown[][] {
  const candidates = [
    record.branch_results,
    recordValue(record.diagnostics)?.branch_results,
    recordValue(record.evidence)?.branch_results,
    recordValue(recordValue(record.evidence)?.diagnostics)?.branch_results,
    recordValue(record.result_metadata)?.branch_results,
    recordValue(recordValue(record.result_metadata)?.diagnostics)?.branch_results,
    recordValue(recordValue(record.result_metadata)?.step_evidence)?.branch_results,
    recordValue(recordValue(recordValue(record.result_metadata)?.step_evidence)?.diagnostics)?.branch_results,
    recordValue(record.step_evidence)?.branch_results,
    recordValue(recordValue(record.step_evidence)?.diagnostics)?.branch_results
  ];
  return candidates.filter(Array.isArray);
}

function ensureBranch(group: ParallelGroupTrace, id: string, title?: string): ParallelBranchTrace {
  const existing = group.branches.find((branch) => branch.id === id);
  if (existing) {
    if (title) existing.title = title;
    return existing;
  }
  const branch: ParallelBranchTrace = {
    id,
    title: title || id,
    status: "queued",
    sourceCount: 0,
    artifactCount: 0,
    toolCallCount: 0,
    missingCoverage: [],
    sources: [],
    artifacts: [],
    toolCalls: []
  };
  group.branches.push(branch);
  return branch;
}

function sourceRefs(value: unknown): ParallelSourceRef[] {
  if (!Array.isArray(value)) return [];
  return value.map((item): ParallelSourceRef | null => {
    const record = recordValue(item);
    if (!record) return null;
    return {
      id: stringValue(record.id),
      title: stringValue(record.title),
      url: stringValue(record.url),
      provider: stringValue(record.provider),
      snippet: stringValue(record.snippet)
    };
  }).filter((item): item is ParallelSourceRef => item !== null);
}

function artifactRefs(value: unknown): ParallelArtifactRef[] {
  if (!Array.isArray(value)) return [];
  return value.map((item): ParallelArtifactRef | null => {
    const record = recordValue(item);
    if (!record) return null;
    return {
      id: stringValue(record.id),
      filename: stringValue(record.filename),
      content_type: stringValue(record.content_type),
      job_id: stringValue(record.job_id)
    };
  }).filter((item): item is ParallelArtifactRef => item !== null);
}

function toolCallRefs(value: unknown): ParallelToolCallRef[] {
  if (!Array.isArray(value)) return [];
  return value.map((item): ParallelToolCallRef | null => {
    const record = recordValue(item);
    if (!record) return null;
    return {
      id: stringValue(record.id),
      name: stringValue(record.name),
      status: stringValue(record.status)
    };
  }).filter((item): item is ParallelToolCallRef => item !== null);
}

function conflictRefs(value: unknown): ParallelConflictTrace[] {
  if (!Array.isArray(value)) return [];
  return value.map((item): ParallelConflictTrace | null => {
    const record = recordValue(item);
    if (!record) return null;
    const field = stringValue(record.field);
    if (!field) return null;
    return {
      field,
      subject: stringValue(record.subject),
      kind: stringValue(record.kind),
      values: stringArray(record.values),
      branches: stringArray(record.branches),
      evidence: stringArray(record.evidence),
      confidence: numberValue(record.confidence),
      reason: stringValue(record.reason)
    };
  }).filter((item): item is ParallelConflictTrace => item !== null);
}

function dedupeSources(sources: ParallelSourceRef[]): ParallelSourceRef[] {
  const seen = new Set<string>();
  const out: ParallelSourceRef[] = [];
  for (const source of sources) {
    const key = (source.url || `${source.title || ""}|${source.provider || ""}`).toLowerCase();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push(source);
  }
  return out;
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map(stringValue).filter(Boolean);
}

function mergeStringArrays(existing: string[], incoming: string[]): string[] {
  if (incoming.length === 0) return existing;
  const seen = new Set(existing);
  const out = [...existing];
  for (const item of incoming) {
    if (seen.has(item)) continue;
    seen.add(item);
    out.push(item);
  }
  return out;
}

function statusValue(value: unknown): ParallelBranchTrace["status"] | "" {
  const status = stringValue(value).toLowerCase();
  if (status === "queued" || status === "running" || status === "succeeded" || status === "failed" || status === "timed_out" || status === "cancelled") {
    return status;
  }
  return "";
}

function budgetValue(value: unknown): ParallelBranchBudget | undefined {
  const record = recordValue(value);
  if (!record) return undefined;
  const budget: ParallelBranchBudget = {
    timeout_ms: numberValue(record.timeout_ms),
    max_tool_calls: numberValue(record.max_tool_calls),
    max_sources: numberValue(record.max_sources),
    max_tokens: numberValue(record.max_tokens)
  };
  return Object.values(budget).some((item) => item !== undefined) ? budget : undefined;
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  if (typeof value === "string") return value.trim();
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function numberValue(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
  }
  return undefined;
}

function booleanValue(value: unknown): boolean | undefined {
  if (typeof value === "boolean") return value;
  if (typeof value === "string") {
    if (value.toLowerCase() === "true") return true;
    if (value.toLowerCase() === "false") return false;
  }
  return undefined;
}

import { clearAuth, loadAuth, saveAuth } from "./authStore";
import type { AdminHealthStatus, AdminSkill, AdminUser, Asset, AuditLogSummary, AuthRegistrationPending, AuthSession, BrowserMemoryRequest, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunReport, EvaluationRunSummary, EvaluationScope, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, MemoryItem, MemoryMaintenanceAction, MemoryMaintenanceRunReport, MemorySettings, MessageSearchResult, PersonalizationSettings, ReadinessStatus, RiskReviewItem, RiskReviewSummary, RiskSummary, Session, Skill, SkillExecution, SkillExecutionSummary, SkillReviewResult, SkillVersion, UserProfile } from "../types";

const configuredAPIBaseURL = ((import.meta as ImportMeta & { env?: Record<string, string | undefined> }).env?.VITE_AGENT_API_BASE_URL || "").trim();

export class ApiError extends Error {
  status: number;
  code?: string;
  requestId?: string;

  constructor(message: string, status: number, code?: string, requestId?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

export class ApiClient {
  private auth: AuthSession | null = loadAuth();
  private onAuthChange: (session: AuthSession | null) => void;
  private refreshPromise: Promise<boolean> | null = null;
  private refreshTimer: number | null = null;
  private disposed = false;

  constructor(onAuthChange: (session: AuthSession | null) => void) {
    this.onAuthChange = onAuthChange;
  }

  session(): AuthSession | null {
    return this.auth;
  }

  start(): void {
    this.disposed = false;
    this.scheduleAccessRefresh();
  }

  dispose(): void {
    this.disposed = true;
    if (this.refreshTimer && typeof window !== "undefined") window.clearTimeout(this.refreshTimer);
    this.refreshTimer = null;
  }

  async login(email: string, password: string): Promise<AuthSession> {
    const result = await this.authRequest("/v1/auth/login", { email, password });
    if (!isAuthSession(result)) throw new ApiError("email verification is required", 202, "verification_required");
    return result;
  }

  async register(email: string, password: string, displayName: string): Promise<AuthSession | AuthRegistrationPending> {
    return this.authRequest("/v1/auth/register", { email, password, display_name: displayName });
  }

  async logout(): Promise<void> {
    const refreshToken = this.auth?.refresh_token || "";
    try {
      await this.fetchJSON("/v1/auth/logout", {
        method: "POST",
        body: JSON.stringify({ refresh_token: refreshToken })
      });
    } finally {
      this.setAuth(null);
    }
  }

  async me(): Promise<UserProfile> {
    const payload = await this.fetchJSON<{ user: UserProfile }>("/v1/auth/me");
    return payload.user;
  }

  async readiness(): Promise<ReadinessStatus> {
    const response = await fetch(this.apiURL(`/readyz?ts=${Date.now()}`), {
      cache: "no-store",
      credentials: "include"
    });
    const payload = await response.json().catch(() => null) as ReadinessStatus | null;
    if (payload) return payload;
    return { status: response.ok ? "ok" : "error", checks: [] };
  }

  async sessions(limit = 50, offset = 0): Promise<Session[]> {
    const params = new URLSearchParams({
      limit: String(limit),
      summary: "1"
    });
    if (offset > 0) params.set("offset", String(offset));
    return this.fetchJSON<Session[]>(`/v1/sessions?${params.toString()}`);
  }

  async createSession(): Promise<Session> {
    return this.fetchJSON<Session>("/v1/sessions", {
      method: "POST",
      body: JSON.stringify({ working_dir: "" })
    });
  }

  async getSession(id: string): Promise<Session> {
    return this.fetchJSON<Session>(`/v1/sessions/${encodeURIComponent(id)}`);
  }

  async deleteSession(id: string): Promise<void> {
    await this.fetchJSON(`/v1/sessions/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  async deleteSessionMemory(id: string): Promise<void> {
    await this.fetchJSON(`/v1/sessions/${encodeURIComponent(id)}/memory`, { method: "DELETE" });
  }

  async deleteAllMemory(): Promise<void> {
    await this.fetchJSON("/v1/memory", { method: "DELETE" });
  }

  async memorySettings(): Promise<MemorySettings> {
    return this.fetchJSON<MemorySettings>("/v1/memory/settings");
  }

  async updateMemorySettings(patch: Partial<Pick<MemorySettings, "enabled" | "capture_enabled" | "context_enabled">>): Promise<MemorySettings> {
    return this.fetchJSON<MemorySettings>("/v1/memory/settings", {
      method: "PATCH",
      body: JSON.stringify(patch)
    });
  }

  async personalization(): Promise<PersonalizationSettings> {
    return this.fetchJSON<PersonalizationSettings>("/v1/personalization");
  }

  async updatePersonalization(patch: Partial<Pick<PersonalizationSettings, "profile" | "style" | "traits" | "custom_instructions" | "feature_flags">>): Promise<PersonalizationSettings> {
    return this.fetchJSON<PersonalizationSettings>("/v1/personalization", {
      method: "PATCH",
      body: JSON.stringify(patch)
    });
  }

  async resetPersonalization(): Promise<PersonalizationSettings> {
    return this.fetchJSON<PersonalizationSettings>("/v1/personalization/reset", {
      method: "POST",
      body: JSON.stringify({})
    });
  }

  async createBrowserMemory(request: BrowserMemoryRequest): Promise<MemoryItem> {
    return this.fetchJSON<MemoryItem>("/v1/personalization/browser-memory", {
      method: "POST",
      body: JSON.stringify(request)
    });
  }

  async memoryItems(options: { sessionId?: string; status?: string; level?: string; namespace?: string; visibility?: string } = {}): Promise<MemoryItem[]> {
    const params = new URLSearchParams({ limit: "100" });
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.status) params.set("status", options.status);
    if (options.level) params.set("level", options.level);
    if (options.namespace) params.set("namespace", options.namespace);
    if (options.visibility) params.set("visibility", options.visibility);
    if (!options.status) params.set("status", "active");
    const payload = await this.fetchJSON<{ items: MemoryItem[] }>(`/v1/memory?${params.toString()}`);
    return payload.items || [];
  }

  async updateMemoryItem(id: string, patch: Partial<Pick<MemoryItem, "content" | "namespace" | "category" | "tags" | "visibility" | "status">>): Promise<MemoryItem> {
    return this.fetchJSON<MemoryItem>(`/v1/memory/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch)
    });
  }

  async memoryFeedback(id: string, type: "important" | "incorrect" | "not_relevant"): Promise<MemoryItem> {
    return this.fetchJSON<MemoryItem>(`/v1/memory/${encodeURIComponent(id)}/feedback`, {
      method: "POST",
      body: JSON.stringify({ type })
    });
  }

  async memoryResolve(id: string, action: "accept" | "reject" | "keep_both"): Promise<MemoryItem> {
    return this.fetchJSON<MemoryItem>(`/v1/memory/${encodeURIComponent(id)}/resolve`, {
      method: "POST",
      body: JSON.stringify({ action })
    });
  }

  async rebuildMemory(): Promise<MemoryItem[]> {
    const payload = await this.fetchJSON<{ items: MemoryItem[] }>("/v1/memory/rebuild", {
      method: "POST",
      body: JSON.stringify({})
    });
    return payload.items || [];
  }

  async scoreMemory(): Promise<MemoryItem[]> {
    const payload = await this.fetchJSON<{ items: MemoryItem[] }>("/v1/memory/score", {
      method: "POST",
      body: JSON.stringify({})
    });
    return payload.items || [];
  }

  async memoryMaintenance(): Promise<MemoryMaintenanceAction[]> {
    const payload = await this.fetchJSON<{ actions: MemoryMaintenanceAction[] }>("/v1/memory/maintenance");
    return payload.actions || [];
  }

  async runMemoryMaintenance(): Promise<MemoryMaintenanceRunReport> {
    const payload = await this.fetchJSON<Partial<MemoryMaintenanceRunReport>>("/v1/memory/maintenance/run", {
      method: "POST",
      body: JSON.stringify({})
    });
    return {
      actions: payload.actions || [],
      applied: payload.applied || [],
      planned: payload.planned || []
    };
  }

  async applyMemoryMaintenance(id: string): Promise<MemoryMaintenanceAction> {
    return this.fetchJSON<MemoryMaintenanceAction>(`/v1/memory/maintenance/${encodeURIComponent(id)}/apply`, {
      method: "POST",
      body: JSON.stringify({})
    });
  }

  async dismissMemoryMaintenance(id: string): Promise<MemoryMaintenanceAction> {
    return this.fetchJSON<MemoryMaintenanceAction>(`/v1/memory/maintenance/${encodeURIComponent(id)}/dismiss`, {
      method: "POST",
      body: JSON.stringify({})
    });
  }

  async deleteMemoryItem(id: string): Promise<void> {
    await this.fetchJSON(`/v1/memory/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  async exportData(): Promise<unknown> {
    return this.fetchJSON<unknown>("/v1/data/export");
  }

  async deleteAccount(): Promise<void> {
    const refreshToken = this.auth?.refresh_token || "";
    try {
      await this.fetchJSON("/v1/account", {
        method: "DELETE",
        body: JSON.stringify({ refresh_token: refreshToken })
      });
    } finally {
      this.setAuth(null);
    }
  }

  async cancelSession(id: string): Promise<void> {
    await this.fetchJSON(`/v1/sessions/${encodeURIComponent(id)}/cancel`, { method: "POST" });
  }

  async skills(): Promise<Skill[]> {
    const payload = await this.fetchJSON<{ skills: Skill[] }>("/v1/skills");
    return payload.skills || [];
  }

  async adminSkills(adminToken: string): Promise<AdminSkill[]> {
    const payload = await this.fetchJSON<{ skills: AdminSkill[] }>("/v1/admin/skills", {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.skills || [];
  }

  async updateAdminSkill(name: string, adminToken: string, patch: Partial<AdminSkill> & { changelog?: string }): Promise<AdminSkill> {
    const payload = await this.fetchJSON<{ skill: AdminSkill }>(`/v1/admin/skills/${encodeURIComponent(name)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(patch)
    });
    return payload.skill;
  }

  async setAdminSkillStatus(name: string, adminToken: string, action: "publish" | "unpublish" | "disable", changelog = ""): Promise<AdminSkill> {
    const payload = await this.fetchJSON<{ skill: AdminSkill }>(`/v1/admin/skills/${encodeURIComponent(name)}/${action}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(changelog ? { changelog } : {})
    });
    return payload.skill;
  }

  async adminSkillReview(name: string, adminToken: string): Promise<SkillReviewResult> {
    const payload = await this.fetchJSON<{ review: SkillReviewResult }>(`/v1/admin/skills/${encodeURIComponent(name)}/review`, {
      method: "POST",
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      ...payload.review,
      issues: payload.review?.issues || []
    };
  }

  async adminSkillVersions(name: string, adminToken: string): Promise<SkillVersion[]> {
    const payload = await this.fetchJSON<{ versions: SkillVersion[] }>(`/v1/admin/skills/${encodeURIComponent(name)}/versions`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.versions || [];
  }

  async adminSkillExecutions(name: string, adminToken: string, limit = 20): Promise<SkillExecution[]> {
    const payload = await this.fetchJSON<{ executions: SkillExecution[] }>(`/v1/admin/skills/${encodeURIComponent(name)}/executions?limit=${encodeURIComponent(String(limit))}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.executions || [];
  }

  async adminSkillAnalytics(name: string, adminToken: string): Promise<SkillExecutionSummary> {
    const payload = await this.fetchJSON<{ summary: SkillExecutionSummary }>(`/v1/admin/skills/${encodeURIComponent(name)}/analytics`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.summary;
  }

  async adminUsers(adminToken: string, options: { q?: string; status?: string; limit?: number; offset?: number } = {}): Promise<AdminUser[]> {
    const params = new URLSearchParams();
    if (options.q) params.set("q", options.q);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.limit) params.set("limit", String(options.limit));
    if (options.offset) params.set("offset", String(options.offset));
    const query = params.toString();
    const payload = await this.fetchJSON<{ users: AdminUser[] }>(`/v1/admin/users${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.users || [];
  }

  async adminUser(id: string, adminToken: string): Promise<AdminUser> {
    const payload = await this.fetchJSON<{ user: AdminUser }>(`/v1/admin/users/${encodeURIComponent(id)}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.user;
  }

  async updateAdminUserStatus(id: string, adminToken: string, status: "active" | "disabled" | "banned"): Promise<AdminUser> {
    const payload = await this.fetchJSON<{ user: AdminUser }>(`/v1/admin/users/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ status })
    });
    return payload.user;
  }

  async adminUserAction(id: string, adminToken: string, action: "disable" | "ban" | "reactivate"): Promise<AdminUser> {
    const payload = await this.fetchJSON<{ user: AdminUser }>(`/v1/admin/users/${encodeURIComponent(id)}/${action}`, {
      method: "POST",
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.user;
  }

  async adminOpsSessions(adminToken: string, userId: string, options: { q?: string; limit?: number } = {}): Promise<Session[]> {
    const params = new URLSearchParams({ user_id: userId });
    if (options.q) params.set("q", options.q);
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await this.fetchJSON<{ sessions: Session[] }>(`/v1/admin/ops/sessions?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.sessions || [];
  }

  async adminOpsJobs(adminToken: string, userId: string, options: { sessionId?: string; q?: string; status?: string; limit?: number } = {}): Promise<Job[]> {
    const params = new URLSearchParams({ user_id: userId });
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.q) params.set("q", options.q);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await this.fetchJSON<{ jobs: Job[] }>(`/v1/admin/ops/jobs?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.jobs || [];
  }

  async adminOpsJobEvents(adminToken: string, userId: string, jobId: string, limit = 500): Promise<JobEvent[]> {
    const params = new URLSearchParams({ user_id: userId, limit: String(limit) });
    const payload = await this.fetchJSON<{ events: JobEvent[] }>(`/v1/admin/ops/jobs/${encodeURIComponent(jobId)}/events?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.events || [];
  }

  async adminOpsCancelJob(adminToken: string, userId: string, jobId: string): Promise<void> {
    const params = new URLSearchParams({ user_id: userId });
    await this.fetchJSON(`/v1/admin/ops/jobs/${encodeURIComponent(jobId)}/cancel?${params.toString()}`, {
      method: "POST",
      headers: { "X-Admin-Token": adminToken }
    });
  }

  async adminOpsAssets(adminToken: string, userId: string, options: { sessionId?: string; jobId?: string; q?: string; kind?: string; limit?: number } = {}): Promise<Asset[]> {
    const params = new URLSearchParams({ user_id: userId });
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.jobId) params.set("job_id", options.jobId);
    if (options.q) params.set("q", options.q);
    if (options.kind && options.kind !== "all") params.set("kind", options.kind);
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await this.fetchJSON<{ assets: Asset[] }>(`/v1/admin/ops/assets?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.assets || [];
  }

  async adminOpsHealth(adminToken: string): Promise<AdminHealthStatus> {
    return this.fetchJSON<AdminHealthStatus>("/v1/admin/ops/health", {
      headers: { "X-Admin-Token": adminToken }
    });
  }

  async adminOpsLLMUsage(adminToken: string, options: { userId?: string; days?: number; limit?: number } = {}): Promise<LLMUsageAdminSummary> {
    const params = new URLSearchParams();
    if (options.userId) params.set("user_id", options.userId);
    if (options.days) params.set("days", String(options.days));
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ usage: LLMUsageAdminSummary }>(`/v1/admin/ops/llm-usage${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      ...payload.usage,
      by_provider: payload.usage?.by_provider || [],
      recent: payload.usage?.recent || []
    };
  }

  async adminOpsLLMConfig(adminToken: string): Promise<LLMGovernanceConfig> {
    const payload = await this.fetchJSON<{ config: LLMGovernanceConfig }>("/v1/admin/ops/llm-config", {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.config || {};
  }

  async updateAdminOpsLLMConfig(adminToken: string, patch: LLMGovernanceConfig): Promise<LLMGovernanceConfig> {
    const payload = await this.fetchJSON<{ config: LLMGovernanceConfig }>("/v1/admin/ops/llm-config", {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(patch)
    });
    return payload.config || {};
  }

  async adminOpsQuota(adminToken: string, userId: string, options: { days?: number; limit?: number } = {}): Promise<LLMQuotaAdminSummary> {
    const params = new URLSearchParams({ user_id: userId });
    if (options.days) params.set("days", String(options.days));
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await this.fetchJSON<{ quota: LLMQuotaAdminSummary }>(`/v1/admin/ops/quota?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      ...payload.quota,
      recent_adjustments: payload.quota?.recent_adjustments || []
    };
  }

  async adminOpsQuotaReset(adminToken: string, userId: string, reason: string): Promise<LLMQuotaAdminSummary> {
    const payload = await this.fetchJSON<{ quota: LLMQuotaAdminSummary }>("/v1/admin/ops/quota/reset", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ user_id: userId, reason })
    });
    return {
      ...payload.quota,
      recent_adjustments: payload.quota?.recent_adjustments || []
    };
  }

  async adminOpsQuotaRefund(adminToken: string, payload: { userId: string; requestRefund?: number; tokenRefund?: number; costRefundUSD?: number; reason?: string }): Promise<LLMQuotaAdminSummary> {
    const response = await this.fetchJSON<{ quota: LLMQuotaAdminSummary }>("/v1/admin/ops/quota/refund", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        user_id: payload.userId,
        request_refund: payload.requestRefund || 0,
        token_refund: payload.tokenRefund || 0,
        cost_refund_usd: payload.costRefundUSD || 0,
        reason: payload.reason || ""
      })
    });
    return {
      ...response.quota,
      recent_adjustments: response.quota?.recent_adjustments || []
    };
  }

  async adminOpsAudit(adminToken: string, options: { userId?: string; event?: string; risk?: string; q?: string; days?: number; limit?: number } = {}): Promise<AuditLogSummary> {
    const params = new URLSearchParams();
    if (options.userId) params.set("user_id", options.userId);
    if (options.event && options.event !== "all") params.set("event", options.event);
    if (options.risk && options.risk !== "all") params.set("risk", options.risk);
    if (options.q) params.set("q", options.q);
    if (options.days) params.set("days", String(options.days));
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ audit: AuditLogSummary }>(`/v1/admin/ops/audit${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      ...payload.audit,
      by_event: payload.audit?.by_event || [],
      by_risk: payload.audit?.by_risk || [],
      records: payload.audit?.records || []
    };
  }

  async adminOpsRisk(adminToken: string, options: { userId?: string; sessionId?: string; ipAddress?: string; operation?: string; risk?: string; q?: string; days?: number; limit?: number } = {}): Promise<RiskSummary> {
    const params = new URLSearchParams();
    if (options.userId) params.set("user_id", options.userId);
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.ipAddress) params.set("ip_address", options.ipAddress);
    if (options.operation && options.operation !== "all") params.set("operation", options.operation);
    if (options.risk && options.risk !== "all") params.set("risk", options.risk);
    if (options.q) params.set("q", options.q);
    if (options.days) params.set("days", String(options.days));
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ risk: RiskSummary }>(`/v1/admin/ops/risk${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      ...payload.risk,
      by_operation: payload.risk?.by_operation || [],
      by_risk: payload.risk?.by_risk || [],
      events: payload.risk?.events || [],
      scores: payload.risk?.scores || []
    };
  }

  async adminOpsRiskReviews(adminToken: string, options: { userId?: string; status?: string; operation?: string; risk?: string; q?: string; days?: number; limit?: number } = {}): Promise<RiskReviewSummary> {
    const params = new URLSearchParams();
    if (options.userId) params.set("user_id", options.userId);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.operation && options.operation !== "all") params.set("operation", options.operation);
    if (options.risk && options.risk !== "all") params.set("risk", options.risk);
    if (options.q) params.set("q", options.q);
    if (options.days) params.set("days", String(options.days));
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ reviews: RiskReviewSummary }>(`/v1/admin/ops/risk/reviews${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      ...payload.reviews,
      by_status: payload.reviews?.by_status || [],
      items: payload.reviews?.items || []
    };
  }

  async updateRiskReview(adminToken: string, id: string, payload: { status: string; assignedTo?: string; resolution?: string; note?: string }): Promise<RiskReviewItem> {
    const response = await this.fetchJSON<{ review: RiskReviewItem }>(`/v1/admin/ops/risk/reviews/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        status: payload.status,
        assigned_to: payload.assignedTo || "",
        resolution: payload.resolution || "",
        note: payload.note || ""
      })
    });
    return response.review;
  }

  async createEvaluationRun(adminToken: string, payload: { name?: string; trigger?: string; scope: EvaluationScope }): Promise<EvaluationRunReport> {
    const response = await this.fetchJSON<EvaluationRunReport>("/v1/admin/ops/eval/runs", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(payload)
    });
    return normalizeEvaluationReport(response);
  }

  async adminOpsEvaluationRuns(adminToken: string, options: { status?: string; trigger?: string; limit?: number } = {}): Promise<EvaluationRun[]> {
    const params = new URLSearchParams();
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.trigger) params.set("trigger", options.trigger);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ runs: EvaluationRun[] }>(`/v1/admin/ops/eval/runs${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.runs || [];
  }

  async adminOpsEvaluationRun(adminToken: string, id: string, limit = 500): Promise<EvaluationRunReport> {
    const params = new URLSearchParams({ limit: String(limit) });
    const payload = await this.fetchJSON<EvaluationRunReport>(`/v1/admin/ops/eval/runs/${encodeURIComponent(id)}?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return normalizeEvaluationReport(payload);
  }

  async adminOpsEvaluationResults(adminToken: string, options: { runId?: string; status?: string; subjectType?: string; userId?: string; sessionId?: string; jobId?: string; skillName?: string; provider?: string; model?: string; limit?: number } = {}): Promise<EvaluationResult[]> {
    const params = evaluationResultParams(options);
    const query = params.toString();
    const payload = await this.fetchJSON<{ results: EvaluationResult[] }>(`/v1/admin/ops/eval/results${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.results || [];
  }

  async adminOpsEvaluationReviews(adminToken: string, options: { resultId?: string; status?: string; limit?: number } = {}): Promise<EvaluationReview[]> {
    const params = new URLSearchParams();
    if (options.resultId) params.set("result_id", options.resultId);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ reviews: EvaluationReview[] }>(`/v1/admin/ops/eval/reviews${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.reviews || [];
  }

  async adminOpsEvaluationSummary(adminToken: string, options: { from?: string; to?: string; status?: string; trigger?: string; limit?: number } = {}): Promise<{ summary: EvaluationRunSummary; runs: EvaluationRun[] }> {
    const params = evaluationSummaryParams(options);
    const query = params.toString();
    const payload = await this.fetchJSON<{ summary: EvaluationRunSummary; runs: EvaluationRun[] }>(`/v1/admin/ops/eval/summary${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      summary: normalizeEvaluationSummary(payload.summary),
      runs: payload.runs || []
    };
  }

  async adminOpsEvaluationResultsCSV(adminToken: string, options: { runId?: string; status?: string; subjectType?: string; userId?: string; sessionId?: string; jobId?: string; skillName?: string; provider?: string; model?: string; limit?: number } = {}): Promise<string> {
    const params = evaluationResultParams(options);
    params.set("format", "csv");
    const query = params.toString();
    return this.fetchText(`/v1/admin/ops/eval/results?${query}`, {
      headers: { "X-Admin-Token": adminToken }
    });
  }

  async adminOpsEvaluationSummaryMarkdown(adminToken: string, options: { from?: string; to?: string; status?: string; trigger?: string; limit?: number } = {}): Promise<string> {
    const params = evaluationSummaryParams(options);
    params.set("format", "markdown");
    const query = params.toString();
    return this.fetchText(`/v1/admin/ops/eval/summary?${query}`, {
      headers: { "X-Admin-Token": adminToken }
    });
  }

  async updateEvaluationReview(adminToken: string, id: string, payload: { status: string; reviewer?: string; note?: string }): Promise<EvaluationReview> {
    const response = await this.fetchJSON<{ review: EvaluationReview }>(`/v1/admin/ops/eval/reviews/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        status: payload.status,
        reviewer: payload.reviewer || "",
        note: payload.note || ""
      })
    });
    return response.review;
  }

  async searchMessages(query: string, limit = 20, offset = 0): Promise<MessageSearchResult[]> {
    const params = new URLSearchParams({
      q: query,
      limit: String(limit),
      offset: String(offset)
    });
    const payload = await this.fetchJSON<{ items: MessageSearchResult[] }>(`/v1/search/messages?${params.toString()}`);
    return payload.items || [];
  }

  async jobs(sessionId?: string): Promise<Job[]> {
    const query = sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : "";
    const payload = await this.fetchJSON<{ jobs: Job[] }>(`/v1/jobs${query}`);
    return payload.jobs || [];
  }

  async jobEvents(jobId: string): Promise<JobEvent[]> {
    const payload = await this.fetchJSON<{ events: JobEvent[] }>(`/v1/jobs/${encodeURIComponent(jobId)}/events`);
    return payload.events || [];
  }

  async cancelJob(jobId: string): Promise<void> {
    await this.fetchJSON(`/v1/jobs/${encodeURIComponent(jobId)}/cancel`, { method: "POST" });
  }

  async attachments(sessionId?: string): Promise<Asset[]> {
    const query = sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : "";
    const payload = await this.fetchJSON<{ attachments: Asset[] }>(`/v1/attachments${query}`);
    return payload.attachments || [];
  }

  async artifacts(sessionId?: string): Promise<Asset[]> {
    const query = sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : "";
    const payload = await this.fetchJSON<{ artifacts: Asset[] }>(`/v1/artifacts${query}`);
    return payload.artifacts || [];
  }

  async uploadAttachment(file: File, sessionId?: string, onProgress?: (percent: number) => void): Promise<Asset> {
    const form = new FormData();
    form.append("file", file);
    if (sessionId) form.append("session_id", sessionId);
    if (onProgress) return this.uploadAttachmentXHR(form, onProgress);
    return this.fetchJSON<Asset>("/v1/attachments", { method: "POST", body: form });
  }

  async deleteAttachment(id: string): Promise<void> {
    await this.fetchJSON(`/v1/attachments/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  async deleteArtifact(id: string): Promise<void> {
    await this.fetchJSON(`/v1/artifacts/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  async extractAssetMemory(asset: Pick<Asset, "id" | "kind">, options: { namespace?: string; visibility?: string } = {}): Promise<MemoryItem[]> {
    const collection = asset.kind === "attachment" ? "attachments" : "artifacts";
    const payload = await this.fetchJSON<{ items: MemoryItem[] }>(`/v1/${collection}/${encodeURIComponent(asset.id)}/memory/extract`, {
      method: "POST",
      body: JSON.stringify(options)
    });
    return payload.items || [];
  }

  attachmentURL(id: string): string {
    return this.authURL(`/v1/attachments/${encodeURIComponent(id)}`);
  }

  artifactURL(id: string): string {
    return this.authURL(`/v1/artifacts/${encodeURIComponent(id)}`);
  }

  artifactPreviewURL(id: string): string {
    return this.authURL(`/v1/artifacts/${encodeURIComponent(id)}/preview`);
  }

  jobStreamURL(jobId: string, afterId?: string): string {
    const params = new URLSearchParams({ stream: "1" });
    if (this.auth?.access_token) params.set("token", this.auth.access_token);
    if (afterId) params.set("after_id", afterId);
    return this.apiURL(`/v1/jobs/${encodeURIComponent(jobId)}/events?${params.toString()}`);
  }

  liveSessionURL(sessionId: string): string {
    const params = new URLSearchParams();
    if (this.auth?.access_token) params.set("token", this.auth.access_token);
    const path = `/v1/sessions/${encodeURIComponent(sessionId)}/live/ws`;
    const url = this.apiURL(params.size ? `${path}?${params.toString()}` : path);
    return url.replace(/^http:/, "ws:").replace(/^https:/, "wss:");
  }

  async chatResponse(sessionId: string, content: string, attachmentIds: string[] = [], signal?: AbortSignal, retry = true): Promise<Response> {
    await this.ensureFreshAccess();
    const response = await fetch(this.apiURL(`/v1/sessions/${encodeURIComponent(sessionId)}/messages`), {
      method: "POST",
      credentials: "include",
      headers: this.headers({ "Content-Type": "application/json" }),
      signal,
      body: JSON.stringify({ content, attachment_ids: attachmentIds })
    });
    if (response.status === 401 && retry && await this.refresh({ clearOnFailure: true })) {
      return this.chatResponse(sessionId, content, attachmentIds, signal, false);
    }
    if (!response.ok) throw await toApiError(response);
    return response;
  }

  private async authRequest(path: string, body: Record<string, string>): Promise<AuthSession | AuthRegistrationPending> {
    const response = await fetch(this.apiURL(path), {
      method: "POST",
      credentials: "include",
      headers: this.headers({ "Content-Type": "application/json" }, false),
      body: JSON.stringify(body)
    });
    if (!response.ok) throw await toApiError(response);
    const payload = (await response.json()) as AuthSession | AuthRegistrationPending;
    if (isAuthSession(payload)) this.setAuth(payload);
    return payload;
  }

  private async fetchJSON<T>(path: string, options: RequestInit = {}, retry = true): Promise<T> {
    await this.ensureFreshAccess();
    const response = await fetch(this.apiURL(path), {
      ...options,
      credentials: "include",
      headers: this.headers(options.headers)
    });
    if (response.status === 401 && retry && this.auth?.refresh_token) {
      if (await this.refresh({ clearOnFailure: true })) return this.fetchJSON<T>(path, options, false);
    }
    if (!response.ok) throw await toApiError(response);
    if (response.status === 204) return undefined as T;
    return (await response.json()) as T;
  }

  private async fetchText(path: string, options: RequestInit = {}, retry = true): Promise<string> {
    await this.ensureFreshAccess();
    const response = await fetch(this.apiURL(path), {
      ...options,
      credentials: "include",
      headers: this.headers(options.headers)
    });
    if (response.status === 401 && retry && this.auth?.refresh_token) {
      if (await this.refresh({ clearOnFailure: true })) return this.fetchText(path, options, false);
    }
    if (!response.ok) throw await toApiError(response);
    return response.text();
  }

  private async ensureFreshAccess(): Promise<void> {
    if (!this.auth?.refresh_token) return;
    const expiresAt = new Date(this.auth.expires_at).getTime();
    if (Number.isFinite(expiresAt) && expiresAt - Date.now() > 60_000) return;
    await this.refresh({ clearOnFailure: false });
  }

  private async refresh({ clearOnFailure = false }: { clearOnFailure?: boolean } = {}): Promise<boolean> {
    if (!this.auth?.refresh_token) {
      if (clearOnFailure) this.setAuth(null);
      return false;
    }
    if (this.refreshPromise) return this.refreshPromise;
    const refreshToken = this.auth.refresh_token;
    this.refreshPromise = (async () => {
      const response = await fetch(this.apiURL("/v1/auth/refresh"), {
        method: "POST",
        credentials: "include",
        headers: this.headers({ "Content-Type": "application/json" }, false),
        body: JSON.stringify({ refresh_token: refreshToken })
      });
      if (response.ok) {
        this.setAuth((await response.json()) as AuthSession);
        return true;
      }
      if (this.auth?.refresh_token && this.auth.refresh_token !== refreshToken) return true;
      const stored = loadAuth();
      if (stored?.refresh_token && stored.refresh_token !== refreshToken) {
        this.setAuth(stored);
        return true;
      }
      if (clearOnFailure) this.setAuth(null);
      return false;
    })().finally(() => {
      this.refreshPromise = null;
    });
    return this.refreshPromise;
  }

  private scheduleAccessRefresh(): void {
    if (this.disposed || typeof window === "undefined") return;
    if (this.refreshTimer) window.clearTimeout(this.refreshTimer);
    this.refreshTimer = null;
    if (!this.auth?.refresh_token) return;
    const expiresAt = new Date(this.auth.expires_at).getTime();
    if (!Number.isFinite(expiresAt)) return;
    const delay = Math.max(5_000, expiresAt - Date.now() - 60_000);
    this.refreshTimer = window.setTimeout(() => {
      this.refresh({ clearOnFailure: false }).catch(() => {});
    }, delay);
  }

  private async uploadAttachmentXHR(form: FormData, onProgress: (percent: number) => void): Promise<Asset> {
    await this.ensureFreshAccess();
    return new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", this.apiURL("/v1/attachments"));
      xhr.withCredentials = true;
      if (this.auth?.access_token) xhr.setRequestHeader("Authorization", `Bearer ${this.auth.access_token}`);
      if (this.auth?.csrf_token) xhr.setRequestHeader("X-CSRF-Token", this.auth.csrf_token);
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable) onProgress(Math.round((event.loaded / event.total) * 100));
      };
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          onProgress(100);
          resolve(JSON.parse(xhr.responseText) as Asset);
          return;
        }
        try {
          const payload = JSON.parse(xhr.responseText || "{}") as { error?: string; message?: string };
          reject(new ApiError(payload.message || payload.error || xhr.statusText, xhr.status));
        } catch {
          reject(new ApiError(xhr.responseText || xhr.statusText, xhr.status));
        }
      };
      xhr.onerror = () => reject(new ApiError("upload failed", xhr.status || 0));
      xhr.send(form);
    });
  }

  private headers(init?: HeadersInit, includeAuth = true): Headers {
    const headers = new Headers(init);
    if (includeAuth && this.auth?.access_token) headers.set("Authorization", `Bearer ${this.auth.access_token}`);
    if (this.auth?.csrf_token) headers.set("X-CSRF-Token", this.auth.csrf_token);
    return headers;
  }

  private authURL(path: string): string {
    const url = this.apiURL(path);
    if (!this.auth?.access_token) return url;
    const sep = url.includes("?") ? "&" : "?";
    return `${url}${sep}token=${encodeURIComponent(this.auth.access_token)}`;
  }

  private apiURL(path: string): string {
    return joinAPIURL(configuredAPIBaseURL, path);
  }

  private setAuth(session: AuthSession | null): void {
    this.auth = session;
    if (session) saveAuth(session);
    else clearAuth();
    this.scheduleAccessRefresh();
    if (!this.disposed) this.onAuthChange(session);
  }
}

export function joinAPIURL(baseURL: string, path: string): string {
  const cleanPath = path.startsWith("/") ? path : `/${path}`;
  const cleanBase = baseURL.trim().replace(/\/+$/, "");
  if (!cleanBase) return cleanPath;
  return `${cleanBase}${cleanPath}`;
}

function normalizeEvaluationReport(report: EvaluationRunReport): EvaluationRunReport {
  return {
    ...report,
    results: report.results || [],
    reviews: report.reviews || [],
    summary: normalizeEvaluationSummary(report.summary)
  };
}

function normalizeEvaluationSummary(summary: EvaluationRunSummary | undefined): EvaluationRunSummary {
  return {
    run_id: summary?.run_id || "",
    total: summary?.total || 0,
    passed: summary?.passed || 0,
    failed: summary?.failed || 0,
    warning: summary?.warning || 0,
    pass_rate: summary?.pass_rate || 0,
    failure_rate: summary?.failure_rate || 0,
    warning_rate: summary?.warning_rate || 0,
    metrics: summary?.metrics || {}
  };
}

function evaluationResultParams(options: { runId?: string; status?: string; subjectType?: string; userId?: string; sessionId?: string; jobId?: string; skillName?: string; provider?: string; model?: string; limit?: number } = {}): URLSearchParams {
  const params = new URLSearchParams();
  if (options.runId) params.set("run_id", options.runId);
  if (options.status && options.status !== "all") params.set("status", options.status);
  if (options.subjectType && options.subjectType !== "all") params.set("subject_type", options.subjectType);
  if (options.userId) params.set("user_id", options.userId);
  if (options.sessionId) params.set("session_id", options.sessionId);
  if (options.jobId) params.set("job_id", options.jobId);
  if (options.skillName) params.set("skill_name", options.skillName);
  if (options.provider) params.set("provider", options.provider);
  if (options.model) params.set("model", options.model);
  if (options.limit) params.set("limit", String(options.limit));
  return params;
}

function evaluationSummaryParams(options: { from?: string; to?: string; status?: string; trigger?: string; limit?: number } = {}): URLSearchParams {
  const params = new URLSearchParams();
  if (options.from) params.set("from", options.from);
  if (options.to) params.set("to", options.to);
  if (options.status && options.status !== "all") params.set("status", options.status);
  if (options.trigger) params.set("trigger", options.trigger);
  if (options.limit) params.set("limit", String(options.limit));
  return params;
}

async function toApiError(response: Response): Promise<ApiError> {
  const requestId = response.headers.get("X-Request-ID") || undefined;
  const text = await response.text();
  try {
    const payload = JSON.parse(text || "{}") as { error?: string; message?: string; code?: string; request_id?: string };
    return new ApiError(payload.message || payload.error || response.statusText, response.status, payload.code, payload.request_id || requestId);
  } catch {
    return new ApiError(text || response.statusText, response.status, undefined, requestId);
  }
}

function isAuthSession(value: AuthSession | AuthRegistrationPending): value is AuthSession {
  return Boolean((value as AuthSession).access_token && (value as AuthSession).refresh_token);
}

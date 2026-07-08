import { clearAuth, loadAuth, saveAuth } from "./authStore";
import { userFacingErrorMessage } from "./errorMessages";
import type { AdminHealthStatus, AdminSkill, AdminUser, Asset, AuditLogSummary, AuthRegistrationPending, AuthSession, BrowserMemoryRequest, BrowserPushConfig, BrowserPushSubscriptionResponse, ChatRunSummary, ConnectorAuthStart, ConnectorConnection, ConnectorPolicy, ConnectorStatus, DeepAgentReplayReport, DeepAgentResumeRequest, DeepAgentWorkflowSummary, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunReport, EvaluationRunSummary, EvaluationScope, EvaluationThresholds, GoldenCandidate, GoldenCase, GoldenSet, GoldenTraceCaptureRequest, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, LoopDiscoveryEvent, LoopDiscoveryResult, LoopTriggerRecord, MemoryEvaluationRunResponse, MemoryItem, MemoryMaintenanceAction, MemoryMaintenanceRunReport, MemorySettings, MessageSearchResult, PersonalizationSettings, PromptDetail, PromptEnvironmentPin, PromptExperiment, PromptExperimentDetail, PromptExperimentVariant, PromptRenderResult, PromptTemplate, PromptVersion, PromptVersionDiff, RAGEvaluationRunResponse, ReadinessStatus, RiskReviewItem, RiskReviewSummary, RiskSummary, Session, Skill, SkillExecution, SkillExecutionSummary, SkillReviewResult, SkillVersion, TaskInboxResponse, UserProfile, WorkflowRun, WorkflowStepRun } from "../types";

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

  async requestPasswordReset(email: string): Promise<void> {
    const response = await fetch(this.apiURL("/v1/auth/password-reset/request"), {
      method: "POST",
      credentials: "include",
      headers: this.headers({ "Content-Type": "application/json" }, false),
      body: JSON.stringify({ email })
    });
    if (!response.ok) throw await toApiError(response);
  }

  async resetPassword(token: string, password: string): Promise<void> {
    const response = await fetch(this.apiURL("/v1/auth/password-reset/confirm"), {
      method: "POST",
      credentials: "include",
      headers: this.headers({ "Content-Type": "application/json" }, false),
      body: JSON.stringify({ token, password })
    });
    if (!response.ok) throw await toApiError(response);
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

  async activeChatRun(sessionId: string): Promise<ChatRunSummary | null> {
    const payload = await this.fetchJSON<{ run: ChatRunSummary | null }>(`/v1/sessions/${encodeURIComponent(sessionId)}/active-run`);
    return payload.run || null;
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

  async connectors(): Promise<ConnectorStatus[]> {
    const payload = await this.fetchJSON<{ connectors: ConnectorStatus[] }>("/v1/connectors");
    return payload.connectors || [];
  }

  async startConnectorAuth(provider: string, redirectUri?: string): Promise<ConnectorAuthStart> {
    const payload = await this.fetchJSON<{ auth: ConnectorAuthStart }>(`/v1/connectors/${encodeURIComponent(provider)}/connect`, {
      method: "POST",
      body: JSON.stringify({ redirect_uri: redirectUri || window.location.origin })
    });
    return payload.auth;
  }

  async completeConnectorAuth(provider: string, request: { state: string; code: string; external_account_label?: string; scopes?: string[] }): Promise<ConnectorConnection> {
    const payload = await this.fetchJSON<{ connection: ConnectorConnection }>(`/v1/connectors/${encodeURIComponent(provider)}/callback`, {
      method: "POST",
      body: JSON.stringify(request)
    });
    return payload.connection;
  }

  async updateConnectorPolicy(provider: string, policy: ConnectorPolicy): Promise<ConnectorConnection> {
    const payload = await this.fetchJSON<{ connection: ConnectorConnection }>(`/v1/connectors/${encodeURIComponent(provider)}/policy`, {
      method: "PATCH",
      body: JSON.stringify({ policy })
    });
    return payload.connection;
  }

  async disconnectConnector(provider: string): Promise<void> {
    await this.fetchJSON(`/v1/connectors/${encodeURIComponent(provider)}/disconnect`, {
      method: "POST",
      body: JSON.stringify({})
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

  async reviewDeepAgentLearning(id: string, action: "accept" | "reject" | "expire" | "rollback", reason = ""): Promise<MemoryItem> {
    return this.fetchJSON<MemoryItem>(`/v1/deep-agent/learnings/${encodeURIComponent(id)}/review`, {
      method: "POST",
      body: JSON.stringify({ action, reason })
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

  async adminOpsLoopTriggers(adminToken: string, userId: string, options: { sessionId?: string; limit?: number } = {}): Promise<LoopTriggerRecord[]> {
    const params = new URLSearchParams({ user_id: userId });
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await this.fetchJSON<{ triggers: LoopTriggerRecord[] }>(`/v1/admin/ops/loop/triggers?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.triggers || [];
  }

  async adminOpsSubmitLoopDiscovery(adminToken: string, userId: string, event: LoopDiscoveryEvent): Promise<LoopDiscoveryResult> {
    const params = new URLSearchParams({ user_id: userId });
    return this.fetchJSON<LoopDiscoveryResult>(`/v1/admin/ops/loop/discovery?${params.toString()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(event)
    });
  }

  async submitLoopDiscovery(event: LoopDiscoveryEvent): Promise<LoopDiscoveryResult> {
    return this.fetchJSON<LoopDiscoveryResult>("/v1/loop/discovery", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(event)
    });
  }

  async adminOpsWorkflows(adminToken: string, userId: string, options: { sessionId?: string; jobId?: string; name?: string; status?: string; limit?: number } = {}): Promise<WorkflowRun[]> {
    const params = new URLSearchParams({ user_id: userId });
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.jobId) params.set("job_id", options.jobId);
    if (options.name) params.set("name", options.name);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.limit) params.set("limit", String(options.limit));
    const payload = await this.fetchJSON<{ workflows: WorkflowRun[] }>(`/v1/admin/ops/workflows?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.workflows || [];
  }

  async adminOpsDeepAgentReplay(adminToken: string, userId: string, runId: string): Promise<DeepAgentReplayReport> {
    const params = new URLSearchParams({ user_id: userId });
    const payload = await this.fetchJSON<{ replay: DeepAgentReplayReport }>(`/v1/admin/ops/workflows/${encodeURIComponent(runId)}/deep-agent/replay?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.replay;
  }

  async adminOpsWorkflow(adminToken: string, userId: string, runId: string): Promise<{ workflow: WorkflowRun; steps: WorkflowStepRun[]; deepAgent?: DeepAgentWorkflowSummary }> {
    const params = new URLSearchParams({ user_id: userId });
    const payload = await this.fetchJSON<{ workflow: WorkflowRun; steps: WorkflowStepRun[]; deep_agent?: DeepAgentWorkflowSummary }>(`/v1/admin/ops/workflows/${encodeURIComponent(runId)}?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return { workflow: payload.workflow, steps: payload.steps || [], deepAgent: payload.deep_agent };
  }

  async adminOpsResumeWorkflow(adminToken: string, userId: string, runId: string, request: DeepAgentResumeRequest = {}): Promise<WorkflowRun> {
    const params = new URLSearchParams({ user_id: userId });
    const payload = await this.fetchJSON<{ workflow: WorkflowRun }>(`/v1/admin/ops/workflows/${encodeURIComponent(runId)}/resume?${params.toString()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ ...request, run_id: runId })
    });
    return payload.workflow;
  }

  async adminOpsReviewDeepAgentLearning(adminToken: string, userId: string, candidateId: string, action: "accept" | "reject" | "expire" | "rollback", reason = ""): Promise<MemoryItem> {
    const params = new URLSearchParams({ user_id: userId });
    return this.fetchJSON<MemoryItem>(`/v1/admin/ops/deep-agent/learnings/${encodeURIComponent(candidateId)}/review?${params.toString()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ action, reason })
    });
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

  async adminOpsLLMUsage(adminToken: string, options: { userId?: string; days?: number; limit?: number; promptId?: string; promptVersion?: string; promptHash?: string; experimentId?: string; variantId?: string } = {}): Promise<LLMUsageAdminSummary> {
    const params = new URLSearchParams();
    if (options.userId) params.set("user_id", options.userId);
    if (options.days) params.set("days", String(options.days));
    if (options.promptId) params.set("prompt_id", options.promptId);
    if (options.promptVersion) params.set("prompt_version", options.promptVersion);
    if (options.promptHash) params.set("prompt_hash", options.promptHash);
    if (options.experimentId) params.set("experiment_id", options.experimentId);
    if (options.variantId) params.set("variant_id", options.variantId);
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

  async adminOpsPrompts(adminToken: string, options: { scope?: string; status?: string; q?: string; limit?: number } = {}): Promise<PromptTemplate[]> {
    const params = new URLSearchParams();
    if (options.scope && options.scope !== "all") params.set("scope", options.scope);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.q) params.set("q", options.q);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ prompts: PromptTemplate[] }>(`/v1/admin/ops/prompts${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.prompts || [];
  }

  async adminOpsPrompt(adminToken: string, id: string): Promise<PromptDetail> {
    const payload = await this.fetchJSON<PromptDetail>(`/v1/admin/ops/prompts/${encodeURIComponent(id)}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return {
      prompt: payload.prompt,
      versions: payload.versions || [],
      published_version: payload.published_version,
      env_pins: payload.env_pins || []
    };
  }

  async adminOpsPromptEnvPins(adminToken: string, promptID: string): Promise<PromptEnvironmentPin[]> {
    const payload = await this.fetchJSON<{ env_pins: PromptEnvironmentPin[] }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/env-pins`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.env_pins || [];
  }

  async createPromptVersion(adminToken: string, promptID: string, version: Partial<PromptVersion> & { version: string; content: string }): Promise<PromptVersion> {
    const response = await this.fetchJSON<{ version: PromptVersion }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/versions`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        ...version,
        prompt_id: promptID
      })
    });
    return response.version;
  }

  async publishPromptVersion(adminToken: string, promptID: string, version: string, changelog = ""): Promise<PromptVersion> {
    const response = await this.fetchJSON<{ version: PromptVersion }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/publish`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ version, changelog })
    });
    return response.version;
  }

  async rollbackPromptVersion(adminToken: string, promptID: string, version: string, changelog = ""): Promise<PromptVersion> {
    const response = await this.fetchJSON<{ version: PromptVersion }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/rollback`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ version, changelog })
    });
    return response.version;
  }

  async diffPromptVersions(adminToken: string, promptID: string, fromVersion: string, toVersion: string): Promise<PromptVersionDiff> {
    const params = new URLSearchParams();
    if (fromVersion) params.set("from_version", fromVersion);
    if (toVersion) params.set("to_version", toVersion);
    return this.fetchJSON<PromptVersionDiff>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/versions/diff?${params.toString()}`, {
      headers: { "X-Admin-Token": adminToken }
    });
  }

  async setPromptEnvironmentPin(adminToken: string, promptID: string, environment: string, payload: { version: string; changelog?: string; evalRunId?: string }): Promise<PromptEnvironmentPin> {
    const response = await this.fetchJSON<{ env_pin: PromptEnvironmentPin }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/env-pins/${encodeURIComponent(environment)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        version: payload.version,
        changelog: payload.changelog,
        eval_run_id: payload.evalRunId
      })
    });
    return response.env_pin;
  }

  async rollbackPromptEnvironmentPin(adminToken: string, promptID: string, environment: string, payload: { version: string; changelog?: string; evalRunId?: string }): Promise<PromptEnvironmentPin> {
    const response = await this.fetchJSON<{ env_pin: PromptEnvironmentPin }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/env-pins/${encodeURIComponent(environment)}/rollback`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        version: payload.version,
        changelog: payload.changelog,
        eval_run_id: payload.evalRunId
      })
    });
    return response.env_pin;
  }

  async renderPromptVersionPreview(adminToken: string, promptID: string, version: string, variables: Record<string, unknown> = {}): Promise<PromptRenderResult> {
    const payload = await this.fetchJSON<{ render: PromptRenderResult }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/versions/${encodeURIComponent(version)}/render-preview`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ variables })
    });
    return payload.render;
  }

  async createPromptVersionEvaluationRun(adminToken: string, promptID: string, version: string, payload: { setId: string; setVersion?: string; judge?: "heuristic" | "llm" | string; candidates: GoldenCandidate[]; name?: string; trigger?: string; thresholds?: EvaluationThresholds }): Promise<EvaluationRunReport> {
    const response = await this.fetchJSON<EvaluationRunReport>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/versions/${encodeURIComponent(version)}/eval`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        set_id: payload.setId,
        set_version: payload.setVersion,
        judge: payload.judge,
        candidates: payload.candidates,
        name: payload.name,
        trigger: payload.trigger,
        thresholds: payload.thresholds
      })
    });
    return normalizeEvaluationReport(response);
  }

  async upsertPromptExperiment(adminToken: string, payload: { experiment: PromptExperiment; variants: PromptExperimentVariant[] }): Promise<PromptExperimentDetail> {
    const response = await this.fetchJSON<PromptExperimentDetail>("/v1/admin/ops/prompt-experiments", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(payload)
    });
    return { ...response, variants: response.variants || [] };
  }

  async adminOpsPromptExperiments(adminToken: string, options: { promptId?: string; status?: string; q?: string; limit?: number } = {}): Promise<PromptExperiment[]> {
    const params = new URLSearchParams();
    if (options.promptId) params.set("prompt_id", options.promptId);
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.q) params.set("q", options.q);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ experiments: PromptExperiment[] }>(`/v1/admin/ops/prompt-experiments${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.experiments || [];
  }

  async adminOpsPromptExperiment(adminToken: string, id: string): Promise<PromptExperimentDetail> {
    const payload = await this.fetchJSON<PromptExperimentDetail>(`/v1/admin/ops/prompt-experiments/${encodeURIComponent(id)}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return { ...payload, variants: payload.variants || [], usage_by_variant: payload.usage_by_variant || [] };
  }

  async updatePromptExperimentStatus(adminToken: string, id: string, action: "start" | "pause" | "complete" | string, winnerVariantId = ""): Promise<PromptExperimentDetail> {
    const payload = await this.fetchJSON<PromptExperimentDetail>(`/v1/admin/ops/prompt-experiments/${encodeURIComponent(id)}/${encodeURIComponent(action)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({ winner_variant_id: winnerVariantId })
    });
    return { ...payload, variants: payload.variants || [] };
  }

  async optimizePrompt(adminToken: string, promptID: string, payload: { baselineVersion?: string; setId?: string; setVersion?: string; judge?: string; maxBadcases?: number; thresholds?: EvaluationThresholds } = {}): Promise<{ workflow: WorkflowRun; steps: WorkflowStepRun[] }> {
    return this.fetchJSON<{ workflow: WorkflowRun; steps: WorkflowStepRun[] }>(`/v1/admin/ops/prompts/${encodeURIComponent(promptID)}/optimize`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        baseline_version: payload.baselineVersion,
        set_id: payload.setId,
        set_version: payload.setVersion,
        judge: payload.judge,
        max_badcases: payload.maxBadcases,
        thresholds: payload.thresholds
      })
    });
  }

  async adminOpsPromptOptimizationRuns(adminToken: string, options: { status?: string; limit?: number } = {}): Promise<WorkflowRun[]> {
    const params = new URLSearchParams();
    if (options.status && options.status !== "all") params.set("status", options.status);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ workflows: WorkflowRun[] }>(`/v1/admin/ops/prompt-optimization-runs${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.workflows || [];
  }

  async adminOpsPromptOptimizationRun(adminToken: string, id: string): Promise<{ workflow: WorkflowRun; steps: WorkflowStepRun[] }> {
    const payload = await this.fetchJSON<{ workflow: WorkflowRun; steps: WorkflowStepRun[] }>(`/v1/admin/ops/prompt-optimization-runs/${encodeURIComponent(id)}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return { workflow: payload.workflow, steps: payload.steps || [] };
  }

  async upsertGoldenSet(adminToken: string, set: GoldenSet): Promise<GoldenSet> {
    const response = await this.fetchJSON<{ set: GoldenSet }>("/v1/admin/ops/eval/golden-sets", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(set)
    });
    return response.set;
  }

  async adminOpsGoldenSets(adminToken: string, options: { id?: string; version?: string; limit?: number } = {}): Promise<GoldenSet[]> {
    const params = new URLSearchParams();
    if (options.id) params.set("id", options.id);
    if (options.version) params.set("version", options.version);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    const payload = await this.fetchJSON<{ sets: GoldenSet[] }>(`/v1/admin/ops/eval/golden-sets${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.sets || [];
  }

  async adminOpsGoldenSet(adminToken: string, id: string, version = ""): Promise<GoldenSet> {
    const params = new URLSearchParams();
    if (version) params.set("version", version);
    const query = params.toString();
    const payload = await this.fetchJSON<{ set: GoldenSet }>(`/v1/admin/ops/eval/golden-sets/${encodeURIComponent(id)}${query ? `?${query}` : ""}`, {
      headers: { "X-Admin-Token": adminToken }
    });
    return payload.set;
  }

  async createGoldenSetVersion(adminToken: string, id: string, payload: { sourceVersion?: string; targetVersion: string; name?: string; description?: string; metadata?: Record<string, unknown> }): Promise<GoldenSet> {
    const response = await this.fetchJSON<{ set: GoldenSet }>(`/v1/admin/ops/eval/golden-sets/${encodeURIComponent(id)}/versions`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        source_version: payload.sourceVersion,
        target_version: payload.targetVersion,
        name: payload.name,
        description: payload.description,
        metadata: payload.metadata
      })
    });
    return response.set;
  }

  async createGoldenCasesFromTrace(adminToken: string, id: string, payload: GoldenTraceCaptureRequest): Promise<{ set: GoldenSet; cases: GoldenCase[] }> {
    return this.fetchJSON<{ set: GoldenSet; cases: GoldenCase[] }>(`/v1/admin/ops/eval/golden-sets/${encodeURIComponent(id)}/cases/from-trace`, {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify(payload)
    });
  }

  async deleteGoldenSet(adminToken: string, id: string): Promise<void> {
    await this.fetchText(`/v1/admin/ops/eval/golden-sets/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: { "X-Admin-Token": adminToken }
    });
  }

  async createGoldenEvaluationRun(adminToken: string, payload: { setId: string; setVersion?: string; judge?: "heuristic" | "llm" | string; candidates: GoldenCandidate[]; name?: string; trigger?: string; thresholds?: EvaluationThresholds }): Promise<EvaluationRunReport> {
    const response = await this.fetchJSON<EvaluationRunReport>("/v1/admin/ops/eval/golden-runs", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        set_id: payload.setId,
        set_version: payload.setVersion,
        judge: payload.judge,
        candidates: payload.candidates,
        name: payload.name,
        trigger: payload.trigger,
        thresholds: payload.thresholds
      })
    });
    return normalizeEvaluationReport(response);
  }

  async createRAGEvaluationRun(adminToken: string, payload: { setId?: string; setVersion?: string; name?: string; description?: string; knowledgeText: string; csvContent: string; judge?: "heuristic" | "llm" | string; chunkSize?: number; chunkOverlap?: number; topK?: number; persistSet?: boolean; thresholds?: EvaluationThresholds }): Promise<RAGEvaluationRunResponse> {
    const response = await this.fetchJSON<RAGEvaluationRunResponse>("/v1/admin/ops/eval/rag-runs", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        set_id: payload.setId,
        set_version: payload.setVersion,
        name: payload.name,
        description: payload.description,
        knowledge_text: payload.knowledgeText,
        csv_content: payload.csvContent,
        judge: payload.judge,
        chunk_size: payload.chunkSize,
        chunk_overlap: payload.chunkOverlap,
        top_k: payload.topK,
        persist_set: payload.persistSet,
        thresholds: payload.thresholds
      })
    });
    return { ...normalizeEvaluationReport(response), set: response.set, candidates: response.candidates || [], chunk_count: response.chunk_count || 0 };
  }

  async createMemoryEvaluationRun(adminToken: string, payload: { setId: string; setVersion?: string; userId?: string; cleanup?: boolean; judge?: "heuristic" | "llm" | string; name?: string; trigger?: string; thresholds?: EvaluationThresholds }): Promise<MemoryEvaluationRunResponse> {
    const response = await this.fetchJSON<MemoryEvaluationRunResponse>("/v1/admin/ops/eval/memory-runs", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Admin-Token": adminToken },
      body: JSON.stringify({
        set_id: payload.setId,
        set_version: payload.setVersion,
        user_id: payload.userId,
        cleanup: payload.cleanup,
        judge: payload.judge,
        name: payload.name,
        trigger: payload.trigger,
        thresholds: payload.thresholds
      })
    });
    return { ...normalizeEvaluationReport(response), set: response.set, candidates: response.candidates || [], user_id: response.user_id || "", cleanup: Boolean(response.cleanup) };
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

  async adminOpsEvaluationResults(adminToken: string, options: { runId?: string; status?: string; subjectType?: string; userId?: string; sessionId?: string; jobId?: string; skillName?: string; provider?: string; model?: string; promptId?: string; promptVersion?: string; promptHash?: string; experimentId?: string; variantId?: string; limit?: number } = {}): Promise<EvaluationResult[]> {
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

  async adminOpsEvaluationResultsCSV(adminToken: string, options: { runId?: string; status?: string; subjectType?: string; userId?: string; sessionId?: string; jobId?: string; skillName?: string; provider?: string; model?: string; promptId?: string; promptVersion?: string; promptHash?: string; experimentId?: string; variantId?: string; limit?: number } = {}): Promise<string> {
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

  async taskInbox(options: { sessionId?: string; limit?: number } = {}): Promise<TaskInboxResponse> {
    const params = new URLSearchParams();
    if (options.sessionId) params.set("session_id", options.sessionId);
    if (options.limit) params.set("limit", String(options.limit));
    const query = params.toString();
    return this.fetchJSON<TaskInboxResponse>(`/v1/tasks/inbox${query ? `?${query}` : ""}`);
  }

  async browserPushConfig(): Promise<BrowserPushConfig> {
    return this.fetchJSON<BrowserPushConfig>("/v1/browser-push/config");
  }

  async saveBrowserPushSubscription(subscription: PushSubscriptionJSON): Promise<BrowserPushSubscriptionResponse> {
    const payload = await this.fetchJSON<{ subscription: BrowserPushSubscriptionResponse }>("/v1/browser-push/subscriptions", {
      method: "POST",
      headers: this.headers({ "Content-Type": "application/json" }),
      body: JSON.stringify(subscription)
    });
    return payload.subscription;
  }

  async deleteBrowserPushSubscription(subscriptionId: string): Promise<void> {
    await this.fetchJSON(`/v1/browser-push/subscriptions/${encodeURIComponent(subscriptionId)}`, { method: "DELETE" });
  }

  async testBrowserPush(): Promise<void> {
    await this.fetchJSON("/v1/browser-push/test", { method: "POST" });
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

  async attachmentBlob(id: string): Promise<Blob> {
    return this.fetchBlob(`/v1/attachments/${encodeURIComponent(id)}`);
  }

  async artifactBlob(id: string): Promise<Blob> {
    return this.fetchBlob(`/v1/artifacts/${encodeURIComponent(id)}`);
  }

  async artifactPreviewBlob(id: string): Promise<Blob> {
    return this.fetchBlob(`/v1/artifacts/${encodeURIComponent(id)}/preview`);
  }

  jobStreamURL(jobId: string, afterId?: string): string {
    const params = new URLSearchParams({ stream: "1" });
    if (afterId) params.set("after_id", afterId);
    return this.apiURL(`/v1/jobs/${encodeURIComponent(jobId)}/events?${params.toString()}`);
  }

  async jobStreamResponse(jobId: string, afterId?: string, signal?: AbortSignal, retry = true): Promise<Response> {
    await this.ensureFreshAccess();
    const response = await fetch(this.jobStreamURL(jobId, afterId), {
      credentials: "include",
      headers: this.headers(),
      signal
    });
    if (response.status === 401 && retry && this.auth?.refresh_token) {
      if (await this.refresh({ clearOnFailure: true })) return this.jobStreamResponse(jobId, afterId, signal, false);
    }
    if (!response.ok) throw await toApiError(response);
    return response;
  }

  liveSessionURL(sessionId: string, resumeHandle?: string | null): string {
    const path = `/v1/sessions/${encodeURIComponent(sessionId)}/live/ws`;
    const url = this.apiURL(path);
    const withResume = resumeHandle
      ? `${url}${url.includes("?") ? "&" : "?"}resume_handle=${encodeURIComponent(resumeHandle)}`
      : url;
    return withResume.replace(/^http:/, "ws:").replace(/^https:/, "wss:");
  }

  webSocketProtocols(): string[] {
    if (!this.auth?.access_token || typeof btoa === "undefined") return [];
    const encoded = btoa(this.auth.access_token).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    return ["agentapi.bearer", encoded];
  }

  async chatResponse(
    sessionId: string,
    content: string,
    attachmentIds: string[] = [],
    signal?: AbortSignal,
    options: { thinkingMode?: boolean; agentMode?: "chat" | "plan_execute" | "web_search"; connectorContext?: string[] } = {},
    retry = true
  ): Promise<Response> {
    await this.ensureFreshAccess();
    const response = await fetch(this.apiURL(`/v1/sessions/${encodeURIComponent(sessionId)}/messages`), {
      method: "POST",
      credentials: "include",
      headers: this.headers({ "Content-Type": "application/json" }),
      signal,
      body: JSON.stringify({
        content,
        attachment_ids: attachmentIds,
        thinking_mode: options.thinkingMode || undefined,
        agent_mode: options.agentMode || undefined,
        connector_context: options.connectorContext?.length ? options.connectorContext : undefined
      })
    });
    if (response.status === 401 && retry && await this.refresh({ clearOnFailure: true })) {
      return this.chatResponse(sessionId, content, attachmentIds, signal, options, false);
    }
    if (!response.ok) throw await toApiError(response);
    return response;
  }

  chatRunStreamURL(runId: string, afterId?: string): string {
    const params = new URLSearchParams({ stream: "1" });
    if (afterId) params.set("after_id", afterId);
    return this.apiURL(`/v1/chat/runs/${encodeURIComponent(runId)}/events?${params.toString()}`);
  }

  async prepareEventSourceAuth(): Promise<void> {
    if (this.auth?.refresh_token) await this.refresh({ clearOnFailure: false });
  }

  async chatRunStreamResponse(runId: string, afterId?: string, signal?: AbortSignal, retry = true): Promise<Response> {
    await this.ensureFreshAccess();
    const response = await fetch(this.chatRunStreamURL(runId, afterId), {
      credentials: "include",
      headers: this.headers(),
      signal
    });
    if (response.status === 401 && retry && this.auth?.refresh_token) {
      if (await this.refresh({ clearOnFailure: true })) return this.chatRunStreamResponse(runId, afterId, signal, false);
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

  private async fetchBlob(path: string, options: RequestInit = {}, retry = true): Promise<Blob> {
    await this.ensureFreshAccess();
    const response = await fetch(this.apiURL(path), {
      ...options,
      credentials: "include",
      headers: this.headers(options.headers)
    });
    if (response.status === 401 && retry && this.auth?.refresh_token) {
      if (await this.refresh({ clearOnFailure: true })) return this.fetchBlob(path, options, false);
    }
    if (!response.ok) throw await toApiError(response);
    return response.blob();
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

function evaluationResultParams(options: { runId?: string; status?: string; subjectType?: string; userId?: string; sessionId?: string; jobId?: string; skillName?: string; provider?: string; model?: string; promptId?: string; promptVersion?: string; promptHash?: string; experimentId?: string; variantId?: string; limit?: number } = {}): URLSearchParams {
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
  if (options.promptId) params.set("prompt_id", options.promptId);
  if (options.promptVersion) params.set("prompt_version", options.promptVersion);
  if (options.promptHash) params.set("prompt_hash", options.promptHash);
  if (options.experimentId) params.set("experiment_id", options.experimentId);
  if (options.variantId) params.set("variant_id", options.variantId);
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
    return new ApiError(userFacingErrorMessage(payload.message || payload.error || response.statusText), response.status, payload.code, payload.request_id || requestId);
  } catch {
    return new ApiError(userFacingErrorMessage(text || response.statusText), response.status, undefined, requestId);
  }
}

function isAuthSession(value: AuthSession | AuthRegistrationPending): value is AuthSession {
  return Boolean((value as AuthSession).access_token && (value as AuthSession).refresh_token);
}

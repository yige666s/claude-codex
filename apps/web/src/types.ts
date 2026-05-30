export type UserProfile = {
  id: string;
  email: string;
  display_name: string;
  status: string;
  email_verified?: boolean;
  email_verified_at?: string;
  created_at: string;
  last_login_at?: string;
};

export type AdminUser = UserProfile & {
  updated_at: string;
  refresh_token_count: number;
  active_refresh_token_count: number;
};

export type AuthSession = {
  user: UserProfile;
  access_token: string;
  refresh_token: string;
  csrf_token?: string;
  expires_at: string;
};

export type AuthRegistrationPending = {
  verification_required: true;
  email: string;
};

export type Message = {
  role: "user" | "assistant" | "tool" | string;
  message_index?: number;
  content?: string;
  attachments?: MessageAttachment[];
  tool_name?: string;
  tool_output?: string;
  created_at?: string;
  hidden?: boolean;
};

export type MessageAttachment = {
  id: string;
  file_type?: string;
  mime_type?: string;
  file_name?: string;
  file_size?: number;
  thumbnail_key?: string;
};

export type MessageSearchResult = {
  session_id: string;
  message_index: number;
  role: string;
  content?: string;
  snippet: string;
  session_title: string;
  created_at: string;
};

export type MemoryItem = {
  id: string;
  session_id?: string;
  namespace?: string;
  kind: string;
  level?: string;
  category: string;
  tags?: string[];
  source: string;
  source_refs?: MemorySourceRef[];
  visibility: string;
  status: string;
  content: string;
  confidence: number;
  weight: number;
  access_count: number;
  parent_id?: string;
  related_ids?: string[];
  conflict_ids?: string[];
  supersedes_id?: string;
  superseded_by_id?: string;
  last_injected_at?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};

export type MemorySourceRef = {
  kind: "attachment" | "artifact" | string;
  id: string;
  filename?: string;
  content_type?: string;
  session_id?: string;
  job_id?: string;
  uri?: string;
};

export type MemoryMaintenanceAction = {
  id: string;
  type: string;
  memory_ids: string[];
  reason: string;
  confidence: number;
  status: string;
  created_at: string;
};

export type MemoryMaintenanceRunReport = {
  actions: MemoryMaintenanceAction[];
  applied: MemoryMaintenanceAction[];
  planned: MemoryMaintenanceAction[];
};

export type MemorySettings = {
  enabled: boolean;
  capture_enabled: boolean;
  context_enabled: boolean;
  updated_at: string;
};

export type PersonalizationSettings = {
  profile: {
    nickname?: string;
    occupation?: string;
    about?: string;
  };
  style: {
    preset: string;
    tone: string;
  };
  traits: {
    warmth: string;
    enthusiasm: string;
    headings_and_lists: string;
    emoji: string;
  };
  custom_instructions: string;
  feature_flags: {
    quick_answers: boolean;
    use_saved_memory: boolean;
    use_chat_history: boolean;
    use_browser_memory: boolean;
  };
  version: number;
  updated_at: string;
};

export type BrowserMemoryRequest = {
  url?: string;
  title?: string;
  content?: string;
  session_id?: string;
  visibility?: string;
  tags?: string[];
};

export type Session = {
  id: string;
  title?: string;
  working_dir: string;
  started_at: string;
  updated_at: string;
  messages?: Message[];
  message_count?: number;
  description?: string;
};

export type Skill = {
  name: string;
  display_name?: string;
  description?: string;
  short_description?: string;
  long_description?: string;
  category?: string;
  icon?: string;
  version?: string;
  tags?: string[];
  usage?: string;
  usage_examples?: string[];
  input_schema?: Record<string, unknown>;
  output_artifact_types?: string[];
  expected_duration?: string;
  produces_artifacts?: boolean;
  run_as_job?: boolean;
  featured?: boolean;
  sort_order?: number;
};

export type SkillPolicyConfig = {
  allowed_tools?: string[];
  allowed_env?: string[];
  network_allowlist?: string[];
  artifact_content_types?: string[];
  shell_timeout?: string;
  sandbox?: {
    runner?: string;
    image?: string;
    network?: string;
    memory?: string;
    cpus?: string;
    pids_limit?: number;
    tmpfs_size?: string;
    max_output_bytes?: number;
  };
  [key: string]: unknown;
};

export type AdminSkill = Skill & {
  status?: string;
  source?: string;
  skill_root?: string;
  metadata?: Record<string, unknown>;
  content_hash?: string;
  created_at?: string;
  updated_at?: string;
  published_at?: string;
};

export type SkillVersion = {
  skill_name: string;
  version?: string;
  content_hash?: string;
  changelog?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  published_at?: string;
};

export type SkillReviewIssue = {
  severity: "error" | "warning" | string;
  code: string;
  field?: string;
  message: string;
};

export type SkillReviewResult = {
  skill_name: string;
  status: string;
  passed: boolean;
  issues: SkillReviewIssue[];
  reviewed_at: string;
};

export type SkillExecution = {
  id: string;
  skill_name: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  request_id?: string;
  status: string;
  error?: string;
  error_kind?: string;
  provider?: string;
  model?: string;
  input_summary?: string;
  artifact_count?: number;
  duration_ms: number;
  diagnostic_json?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  started_at: string;
  completed_at: string;
};

export type SkillExecutionSummary = {
  skill_name?: string;
  total: number;
  succeeded: number;
  failed: number;
  failure_rate: number;
  average_latency_ms: number;
};

export type LLMBackendStatus = {
  name: string;
  provider: string;
  model: string;
  healthy: boolean;
  consecutive_failures: number;
  last_success_at?: string;
  last_error_at?: string;
  last_error?: string;
  disabled_until?: string;
};

export type LLMGovernanceStatus = {
  backends: LLMBackendStatus[];
  config: LLMGovernanceConfig;
};

export type LLMGovernanceConfig = {
  provider?: string;
  model?: string;
  vertex_location?: string;
  model_routes?: string;
  allowed_models?: LLMModelOption[];
  max_attempts?: number;
  retry_backoff_ms?: number;
  chat_timeout_ms?: number;
  skill_timeout_ms?: number;
  daily_token_quota?: number;
  daily_request_quota?: number;
  daily_cost_quota_usd?: number;
  input_cost_per_million?: number;
  output_cost_per_million?: number;
  failure_threshold?: number;
  circuit_cooldown_seconds?: number;
};

export type LLMModelOption = {
  id: string;
  label: string;
  provider: string;
  vertex_location: string;
};

export type LLMUsageRecord = {
  id: string;
  user_id: string;
  session_id: string;
  request_id?: string;
  skill_name?: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
  attempt: number;
  status: string;
  error?: string;
  latency_ms: number;
  ttft_ms?: number;
  created_at: string;
};

export type LLMUsageAdminGroup = {
  provider: string;
  model: string;
  status: string;
  requests: number;
  total_tokens: number;
  estimated_cost_usd: number;
};

export type LLMUsageAdminSummary = {
  since: string;
  requests: number;
  successes: number;
  failures: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
  average_latency_ms: number;
  by_provider: LLMUsageAdminGroup[];
  recent: LLMUsageRecord[];
};

export type LLMUsageSummary = {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
};

export type LLMQuotaAdjustment = {
  id: string;
  user_id: string;
  actor_user_id?: string;
  reason?: string;
  request_delta: number;
  input_token_delta: number;
  output_token_delta: number;
  total_token_delta: number;
  estimated_cost_delta_usd: number;
  created_at: string;
};

export type LLMQuotaAdminSummary = {
  since: string;
  raw_usage: LLMUsageSummary;
  adjustments: LLMUsageSummary;
  effective_usage: LLMUsageSummary;
  recent_adjustments: LLMQuotaAdjustment[];
};

export type MetricPair = {
  key: string;
  count: number;
};

export type LiveHealthStatus = {
  active_sessions: number;
  sessions: number;
  succeeded: number;
  failed: number;
  disconnected: number;
  audio_chunks: number;
  audio_bytes: number;
  average_duration_ms: number;
  average_first_transcript_ms: number;
  average_first_audio_ms: number;
  transcription_success_rate: number;
  error_rate: number;
  errors_by_code: MetricPair[];
};

export type AdminHealthStatus = {
  readiness: ReadinessStatus;
  llm: LLMGovernanceStatus;
  live?: LiveHealthStatus;
};

export type AuditLogRecord = {
  id: string;
  event: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  asset_id?: string;
  request_id?: string;
  ip_address?: string;
  user_agent?: string;
  metadata?: Record<string, unknown>;
  risk_level?: "high" | "medium" | "low" | string;
  created_at: string;
};

export type AuditLogGroup = {
  key: string;
  count: number;
};

export type AuditLogSummary = {
  since: string;
  total: number;
  high_risk: number;
  medium_risk: number;
  low_risk: number;
  by_event: AuditLogGroup[];
  by_risk: AuditLogGroup[];
  records: AuditLogRecord[];
};

export type RiskEvent = {
  id: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  asset_id?: string;
  request_id?: string;
  ip_address?: string;
  operation: string;
  reason: string;
  risk_level: "high" | "medium" | "low" | string;
  score_delta: number;
  metadata?: Record<string, unknown>;
  created_at: string;
};

export type RiskScore = {
  subject_type: "user" | "ip" | string;
  subject_id: string;
  user_id?: string;
  session_id?: string;
  ip_address?: string;
  score: number;
  risk_level: "high" | "medium" | "low" | string;
  event_count: number;
  last_event_at: string;
  updated_at: string;
};

export type RiskSummary = {
  since: string;
  total: number;
  high_risk: number;
  medium_risk: number;
  low_risk: number;
  by_operation: AuditLogGroup[];
  by_risk: AuditLogGroup[];
  events: RiskEvent[];
  scores: RiskScore[];
};

export type RiskReviewItem = {
  id: string;
  risk_event_id: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  asset_id?: string;
  request_id?: string;
  ip_address?: string;
  operation: string;
  reason: string;
  risk_level: "high" | "medium" | "low" | string;
  priority: "high" | "medium" | "low" | string;
  status: "pending" | "in_review" | "resolved" | "dismissed" | string;
  assigned_to?: string;
  resolution?: string;
  note?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
};

export type RiskReviewSummary = {
  since: string;
  total: number;
  pending: number;
  in_review: number;
  resolved: number;
  dismissed: number;
  by_status: AuditLogGroup[];
  items: RiskReviewItem[];
};

export type EvaluationScope = {
  from?: string;
  to?: string;
  subject_type?: "job" | "session" | "skill_execution" | string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  job_status?: string;
  skill_name?: string;
  provider?: string;
  model?: string;
};

export type EvaluationThresholds = {
  min_success_rate?: number;
  max_tool_error_rate?: number;
  max_llm_error_rate?: number;
  max_high_risk_count?: number;
  max_p95_latency_ms?: number;
  max_cost_usd?: number;
  max_empty_output_rate?: number;
};

export type EvaluationRun = {
  id: string;
  name: string;
  status: "pending" | "running" | "completed" | "failed" | string;
  trigger?: string;
  scope: EvaluationScope;
  started_at: string;
  completed_at?: string;
  total: number;
  passed: number;
  failed: number;
  warning: number;
  metrics?: Record<string, unknown>;
  threshold_status?: string;
  summary?: string;
};

export type EvaluationFinding = {
  severity: "info" | "warning" | "error" | string;
  code: string;
  message: string;
  metadata?: Record<string, unknown>;
};

export type EvaluationResult = {
  id: string;
  run_id: string;
  subject_type: "job" | "session" | "skill_execution" | string;
  subject_id: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  skill_name?: string;
  provider?: string;
  model?: string;
  status: "passed" | "failed" | "warning" | string;
  score: number;
  input?: string;
  output?: string;
  metrics?: Record<string, unknown>;
  findings?: EvaluationFinding[];
  created_at: string;
};

export type EvaluationReview = {
  id: string;
  result_id: string;
  status: "pending" | "passed" | "failed" | "ignored" | string;
  reviewer?: string;
  note?: string;
  created_at: string;
  updated_at: string;
};

export type EvaluationRunSummary = {
  run_id?: string;
  total: number;
  passed: number;
  failed: number;
  warning: number;
  pass_rate: number;
  failure_rate: number;
  warning_rate: number;
  metrics?: Record<string, unknown>;
  threshold_status?: string;
};

export type EvaluationRunReport = {
  run: EvaluationRun;
  results: EvaluationResult[];
  reviews: EvaluationReview[];
  summary: EvaluationRunSummary;
};

export type Asset = {
  id: string;
  kind: "attachment" | "artifact";
  user_id?: string;
  session_id?: string;
  job_id?: string;
  object_key?: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
  deleted_at?: string;
};

export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";

export type Job = {
  id: string;
  user_id?: string;
  session_id: string;
  type: string;
  status: JobStatus;
  content?: string;
  attachment_ids?: string[];
  attachment_urls?: Array<{ url: string; content_type?: string; filename?: string }>;
  error?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  finished_at?: string;
};

export type RuntimeEvent = {
  type: string;
  session_id?: string;
  role?: string;
  content?: string;
  error?: string;
  job_id?: string;
  job?: Job;
  job_reason?: string;
  data?: unknown;
};

export type LiveClientEvent = {
  type: "audio" | "audio_end" | "audio_end_and_close" | "activity_start" | "activity_end" | "text" | "client_trace" | "close" | "done";
  mime_type?: string;
  data?: string;
  content?: string;
};

export type JobEvent = {
  id: string;
  job_id: string;
  session_id?: string;
  type: string;
  event: RuntimeEvent;
  created_at: string;
};

export type ReadinessCheck = {
  name: string;
  status: "ok" | "error" | string;
  error?: string;
};

export type ReadinessStatus = {
  status: "ok" | "error" | string;
  checks: ReadinessCheck[];
};

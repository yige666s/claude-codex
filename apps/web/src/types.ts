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

export type AgentActivityItem = {
  id: string;
  type: string;
  channel?: "thinking" | "tool" | "answer" | "citation" | "checkpoint" | "notice";
  title: string;
  detail?: string;
  status: "running" | "succeeded" | "failed" | "cancelled" | "default";
  created_at: string;
  metadata?: Record<string, unknown>;
};

export type AgentActivity = {
  session_id: string;
  job_id?: string;
  running: boolean;
  items: AgentActivityItem[];
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
  expires_at?: string;
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

export type ConnectorPolicy = "read_only" | "draft_write" | "write_with_review" | "disabled";

export type ConnectorProvider = {
  id: string;
  name: string;
  description: string;
  category: string;
  auth_url?: string;
  client_id_env?: string;
  scopes: string[];
  capabilities: string[];
  default_policy: ConnectorPolicy;
  configured: boolean;
  review_by_default: boolean;
  connection_kind?: string;
  default_mcp_server_url?: string;
  official_mcp_server?: boolean;
  supports_synced_index?: boolean;
};

export type ConnectorConnection = {
  id: string;
  user_id: string;
  workspace_id?: string;
  provider: string;
  status: string;
  permission_policy: ConnectorPolicy;
  scopes: string[];
  token_ref?: string;
  external_account_id?: string;
  external_account_label?: string;
  metadata?: Record<string, unknown>;
  connected_at?: string;
  last_sync_at?: string;
  expires_at?: string;
  created_at: string;
  updated_at: string;
  disconnected_at?: string;
};

export type MCPServerBinding = {
  server_id: string;
  user_id: string;
  workspace_id?: string;
  provider: string;
  display_name: string;
  transport: string;
  url?: string;
  status: string;
  last_discovered_at?: string;
  instructions?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};

export type MCPToolPolicy = {
  policy_id: string;
  user_id: string;
  workspace_id?: string;
  server_id: string;
  provider: string;
  tool_name: string;
  permission_policy: ConnectorPolicy;
  requires_review: boolean;
  side_effect_level: string;
  allowed: boolean;
  metadata?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
};

export type ConnectorContextHint = {
  enabled: boolean;
  task_types: string[];
  evidence: string[];
  policy_hint: string;
};

export type ConnectorStatus = {
  provider: ConnectorProvider;
  connection?: ConnectorConnection;
  context: ConnectorContextHint;
  mcp_server?: MCPServerBinding;
  mcp_tools?: MCPToolPolicy[];
};

export type ConnectorAuthStart = {
  provider: string;
  state: string;
  auth_url: string;
  scopes: string[];
  configured: boolean;
  expires_at: string;
  redirect_uri?: string;
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
  max_loop_duration_ms?: number;
  max_loop_actions?: number;
  max_branch_count?: number;
  max_branch_concurrency?: number;
  max_parallel_branches?: number;
  parallel_branch_timeout_ms?: number;
  parallel_max_tool_calls?: number;
  parallel_max_sources?: number;
  parallel_max_tokens?: number;
  evaluator_timeout_ms?: number;
  conflict_reconciliation_timeout_ms?: number;
  max_sources_per_branch?: number;
  search_quality_threshold?: number;
  automatic_trigger_enabled?: boolean;
  risky_write_approval_mode?: string;
  daily_token_quota?: number;
  daily_request_quota?: number;
  api_rate_limit_per_minute?: number;
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
  prompt_id?: string;
  prompt_version?: string;
  prompt_hash?: string;
  experiment_id?: string;
  variant_id?: string;
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
  subject_type?: "job" | "session" | "skill_execution" | "deep_agent" | string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  job_status?: string;
  template_id?: string;
  task_type?: string;
  skill_name?: string;
  provider?: string;
  model?: string;
  prompt_id?: string;
  prompt_version?: string;
  prompt_hash?: string;
  experiment_id?: string;
  variant_id?: string;
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
  subject_type: "job" | "session" | "skill_execution" | "deep_agent" | string;
  subject_id: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  skill_name?: string;
  provider?: string;
  model?: string;
  prompt_id?: string;
  prompt_version?: string;
  prompt_hash?: string;
  experiment_id?: string;
  variant_id?: string;
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

export type PromptTemplate = {
  id: string;
  name: string;
  description?: string;
  scope?: string;
  owner?: string;
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
};

export type PromptVersion = {
  prompt_id: string;
  version: string;
  status: "draft" | "review_pending" | "published" | "archived" | string;
  content: string;
  variables_schema?: Record<string, unknown>;
  render_config?: Record<string, unknown>;
  content_hash: string;
  base_version?: string;
  changelog?: string;
  created_by?: string;
  reviewed_by?: string;
  created_at?: string;
  published_at?: string;
};

export type PromptDetail = {
  prompt: PromptTemplate;
  versions: PromptVersion[];
  published_version?: PromptVersion;
};

export type PromptRenderResult = {
  prompt_id: string;
  prompt_version: string;
  prompt_hash: string;
  content: string;
  rendered_preview?: string;
  token_estimate?: number;
  missing_variables?: string[];
  metadata?: Record<string, unknown>;
};

export type PromptExperiment = {
  id: string;
  name: string;
  prompt_id: string;
  status: "draft" | "running" | "paused" | "completed" | string;
  traffic_scope: "user" | "session" | "tenant" | string;
  allocation?: Record<string, unknown>;
  guardrails?: Record<string, unknown>;
  winner_variant_id?: string;
  created_by?: string;
  updated_by?: string;
  started_at?: string;
  ended_at?: string;
  created_at?: string;
  updated_at?: string;
};

export type PromptExperimentVariant = {
  experiment_id: string;
  variant_id: string;
  prompt_version: string;
  weight: number;
  metadata?: Record<string, unknown>;
  created_at?: string;
};

export type PromptExperimentDetail = {
  experiment: PromptExperiment;
  variants: PromptExperimentVariant[];
  usage_by_variant?: Array<Record<string, unknown>>;
};

export type GoldenEvidence = {
  id: string;
  content: string;
  source?: string;
  metadata?: Record<string, unknown>;
};

export type GoldenCase = {
  id: string;
  query: string;
  expected_answer?: string;
  expected_facts?: string[];
  gold_evidence?: GoldenEvidence[];
  tags?: string[];
  metadata?: Record<string, unknown>;
};

export type GoldenCandidate = {
  case_id: string;
  output: string;
  retrieved_evidence?: GoldenEvidence[];
  metadata?: Record<string, unknown>;
};

export type GoldenSet = {
  id: string;
  name: string;
  description?: string;
  version?: string;
  metadata?: Record<string, unknown>;
  cases: GoldenCase[];
  created_at?: string;
  updated_at?: string;
};

export type GoldenTraceCaptureRequest = {
  source_version?: string;
  target_version?: string;
  scope: EvaluationScope;
  subject_id?: string;
  expected_answer?: string;
  expected_facts?: string[];
  tags?: string[];
  max_cases?: number;
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

export type DeepAgentStepRoute = {
  step_id?: string;
  version?: string;
  mode?: string;
  executor?: string;
  skill_name?: string;
  requires_artifact?: boolean;
  deliverable_type?: string;
  filename_hint?: string;
  allowed_tools?: string[];
  search_scope?: string;
  success_criteria?: string[];
  reason?: string;
  confidence?: string;
  shadow_route?: Record<string, unknown>;
  shadow_diff?: string[];
};

export type RuntimeEvent = {
  type: string;
  id?: string;
  session_id?: string;
  run_id?: string;
  role?: string;
  content?: string;
  text?: string;
  tool?: string;
  input?: unknown;
  summary?: string;
  sources?: unknown[];
  source_id?: string;
  answer_span?: string;
  message?: string;
  error?: string;
  job_id?: string;
  job?: Job;
  job_reason?: string;
  data?: unknown;
};

export type ChatRunSummary = {
  run_id: string;
  session_id?: string;
  terminal?: boolean;
  last_event_id?: string;
  updated_at?: string;
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

export type TaskInboxGroup = "running" | "needs_review" | "failed" | "blocked" | "completed" | "scheduled";

export type TaskInboxReviewAction = {
  kind: string;
  run_id?: string;
  step_id?: string;
  action_hash?: string;
};

export type TaskInboxItem = {
  id: string;
  kind: "job" | "artifact" | string;
  group: TaskInboxGroup;
  title: string;
  status: string;
  session_id?: string;
  session_available?: boolean;
  job_id?: string;
  artifact_id?: string;
  trigger?: string;
  last_event?: string;
  last_event_at?: string;
  artifact_count: number;
  primary_artifact_id?: string;
  next_action?: string;
  notification_type?: string;
  review?: TaskInboxReviewAction;
  created_at: string;
  updated_at: string;
};

export type TaskInboxResponse = {
  items: TaskInboxItem[];
  groups: Record<TaskInboxGroup, number>;
  generated_at: string;
};

export type LoopDiscoveryEvent = {
  session_id?: string;
  trigger_type: "manual" | "schedule" | "webhook" | "monitor" | "eval_failure" | "connector_event" | string;
  source?: string;
  dedupe_key?: string;
  objective?: string;
  payload?: Record<string, unknown>;
};

export type LoopTriggerRecord = {
  id: string;
  user_id?: string;
  session_id?: string;
  dedupe_key: string;
  trigger_type: string;
  source?: string;
  payload?: Record<string, unknown>;
  job_id?: string;
  loop_goal_id?: string;
  status: string;
  failure_reason?: string;
  created_at: string;
  expires_at: string;
};

export type LoopDiscoveryResult = {
  trigger: LoopTriggerRecord;
  job?: Job;
  duplicate: boolean;
};

export type BrowserPushConfig = {
  enabled: boolean;
  public_key?: string;
};

export type BrowserPushSubscriptionResponse = {
  id: string;
  enabled: boolean;
  endpoint_hash?: string;
  created_at: string;
  updated_at: string;
  last_sent_at?: string;
};

export type WorkflowRun = {
  id: string;
  user_id?: string;
  session_id?: string;
  job_id?: string;
  name: string;
  version: string;
  status: string;
  state?: Record<string, unknown>;
  error?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  finished_at?: string;
};

export type LoopContract = {
  id?: string;
  version?: string;
  objective?: string;
  task_type?: string;
  deliverable?: {
    type?: string;
    format?: string;
    filename_hint?: string;
  };
  rubric?: {
    acceptance_criteria?: string[];
    required_evidence?: string[];
    required_artifacts?: string[];
    forbidden_actions?: string[];
    quality_bar?: string;
  };
  budget?: {
    max_steps?: number;
    max_actions?: number;
    max_duration_ms?: number;
    step_timeout_ms?: number;
    no_progress_limit?: number;
  };
  tool_policy?: {
    allowed_modes?: string[];
    connector_context?: string[];
    write_mode?: string;
  };
  source_policy?: {
    requires_sources?: boolean;
    min_source_count?: number;
    preferred_sources?: string[];
    preferred_domains?: string[];
    blocked_domains?: string[];
    max_sources_per_branch?: number;
    max_duplicate_domains?: number;
    require_primary_source?: boolean;
    recency_requirement?: string;
    min_source_score?: number;
    quality_bar?: string;
  };
  risk_policy?: {
    requires_review?: boolean;
    forbidden_actions?: string[];
    review_policy?: string;
  };
  stop_policy?: {
    done_when?: string[];
    max_no_progress?: number;
    on_budget_exceeded?: string;
  };
  evaluator_policy?: {
    requires_final_verification?: boolean;
    verifier?: string;
    evidence_required?: string[];
    artifact_required?: string[];
  };
  created_from?: string;
  created_at?: string;
};

export type GateDecision = {
  gate?: string;
  allow: boolean;
  block_reason?: string;
  requires_review?: boolean;
  repair_hint?: string;
  evidence_refs?: string[];
  category?: string;
};

export type DeepAgentWorkflowSummary = {
  present: boolean;
  goal?: string;
  status?: string;
  blocker?: string;
  recovery?: DeepAgentRecoveryState;
  loop_contract?: LoopContract;
  handoff?: LoopHandoff;
  gate_decisions?: GateDecision[];
  final_answer?: DeepAgentFinalAnswerEvidence;
  metrics?: Record<string, unknown>;
  timeline?: DeepAgentTimelineItem[];
  governance?: DeepAgentGovernanceState;
  current_step_id?: string;
  current_step?: {
    id: string;
    title: string;
    intent?: string;
    status: string;
    done_condition?: string;
    risk_level?: string;
    metadata?: Record<string, unknown>;
  };
  plan?: {
    goal?: string;
    steps?: Array<{
      id: string;
      title: string;
      intent?: string;
      status: string;
      done_condition?: string;
      risk_level?: string;
      metadata?: Record<string, unknown>;
    }>;
  };
  step_context?: Record<string, unknown>;
  routes?: Array<Record<string, unknown>>;
  evidence?: Array<Record<string, unknown>>;
  artifact_refs?: Array<Record<string, unknown>>;
  final_verifier?: Record<string, unknown>;
  evaluator_verdict?: {
    verdict?: string;
    passed?: boolean;
    failed_criteria?: string[];
    confidence?: string;
    repair_plan?: string[];
    reason?: string;
    source_coverage?: Record<string, unknown>;
    rubric_coverage?: Record<string, unknown>;
  };
  action_history?: Array<{
    id?: string;
    step_id: string;
    tool: string;
    args?: Record<string, unknown>;
    hash?: string;
  }>;
  learnings?: Array<{
    id: string;
    type: string;
    content: string;
    status: string;
    source?: string;
    user_id?: string;
    session_id?: string;
    run_id?: string;
    step_id?: string;
    evidence_id?: string;
    evidence_refs?: string[];
    source_job?: string;
    owner?: string;
    memory_item_id?: string;
    risk_level?: string;
    sensitivity?: string;
    visibility?: string;
    confidence?: number;
    requires_user_confirmation?: boolean;
    policy_reason?: string;
    user_confirmed?: boolean;
    reviewed_by?: string;
    reviewed_at?: string;
    expires_at?: string;
    metadata?: Record<string, unknown>;
    created_at: string;
  }>;
  completed_count: number;
  failed_count: number;
  action_count: number;
  no_progress_count: number;
};

export type DeepAgentTimelineItem = {
  kind: string;
  step_id?: string;
  title?: string;
  status?: string;
  tool?: string;
  action_hash?: string;
  summary?: string;
  created_at?: string;
  metadata?: Record<string, unknown>;
};

export type DeepAgentGovernanceState = {
  kill_switch?: boolean;
  allowed_high_risk_tools?: string[];
  policy_blocked?: boolean;
  policy_block_reason?: string;
  high_risk_policy?: string;
  side_effect_audit?: DeepAgentTimelineItem[];
  user_data_access_audit?: DeepAgentTimelineItem[];
};

export type DeepAgentReplayReport = {
  run_id: string;
  goal?: string;
  status?: string;
  trace_summary?: DeepAgentTraceSummary;
  task_type?: string;
  trigger_payload?: Record<string, unknown>;
  planner_decisions?: DeepAgentTimelineItem[];
  router_decisions?: Array<Record<string, unknown>>;
  executor_decisions?: DeepAgentTimelineItem[];
  verifier_checks?: Array<{ name: string; passed: boolean; reason?: string }>;
  metrics?: Record<string, unknown>;
  findings?: EvaluationFinding[];
};

export type DeepAgentTraceSummary = {
  final_status?: string;
  root_cause?: string;
  category?: string;
  failed_phase?: string;
  failed_gate?: string;
  failed_tool?: string;
  suggested_repair?: string;
  top_evidence?: string[];
};

export type DeepAgentResumeBudget = {
  max_actions?: number;
  max_duration_ms?: number;
  max_steps?: number;
};

export type DeepAgentReviewDecision = {
  action?: "approve" | "reject" | "edit" | string;
  step_id?: string;
  action_hash?: string;
  args_patch?: Record<string, unknown>;
  reason?: string;
};

export type DeepAgentResumeRequest = {
  run_id?: string;
  state_patch?: Record<string, unknown>;
  handoff_patch?: LoopHandoff;
  additional_budget?: DeepAgentResumeBudget;
  review_decision?: DeepAgentReviewDecision;
};

export type LoopHandoff = {
  type?: string;
  summary?: string;
  resume_point?: string;
  resume_available?: boolean;
  workspace?: {
    repo?: string;
    branch?: string;
    worktree?: string;
    base_commit?: string;
    changed_files?: string[];
    test_commands?: string[];
    rollback_plan?: string;
  };
  artifact?: {
    source_artifacts?: Array<Record<string, unknown>>;
    draft_artifact?: Record<string, unknown>;
    final_artifact?: Record<string, unknown>;
    review_state?: string;
  };
  connector?: {
    provider?: string;
    scopes?: string[];
    risk_level?: string;
    pending_write_actions?: Array<Record<string, unknown>>;
  };
  review_state?: string;
  blocking_reason?: string;
  recommended_action?: string;
  metadata?: Record<string, unknown>;
  updated_at?: string;
};

export type DeepAgentFinalAnswerEvidence = {
  artifacts?: Array<Record<string, unknown>>;
  sources?: Array<Record<string, unknown>>;
  tests?: Array<Record<string, unknown>>;
  known_gaps?: string[];
  research_quality?: {
    required?: boolean;
    source_count?: number;
    citation_count?: number;
    source_quality?: Record<string, number>;
    average_source_quality?: number;
    citation_verification?: Record<string, unknown>;
    coverage?: {
      covered?: string[];
      missing?: string[];
    };
    entity_disambiguation?: Record<string, unknown>;
    unresolved_gaps?: string[];
    confidence?: string;
    traceable_source_titles?: string[];
  };
};

export type DeepAgentRecoveryState = {
  blocked_reason?: string;
  blocked_category?: string;
  user_facing_reason?: string;
  last_action?: {
    id?: string;
    step_id: string;
    tool: string;
    args?: Record<string, unknown>;
    hash?: string;
  };
  missing_info?: string[];
  recommended_next_action?: string;
  resume_available: boolean;
  review_pending?: boolean;
  budget_exceeded?: boolean;
  review_action_hash?: string;
  review_step_id?: string;
  resume_point?: string;
  handoff_summary?: string;
  additional_budget_hint?: DeepAgentResumeBudget;
};

export type WorkflowStepRun = {
  id: string;
  run_id: string;
  step_name: string;
  status: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
  started_at: string;
  finished_at?: string;
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

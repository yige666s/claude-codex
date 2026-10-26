# System Prompt Baseline Inventory

Phase 0 baseline for system prompt engineering.

Code source of truth: `prompt_baselines.go`.

## Scope

This inventory covers hardcoded prompts that shape runtime, memory, evaluation, live, and DeepAgent behavior. The registered built-in baselines are seeded automatically when AgentAPI starts. Published Admin versions and production pins take precedence over code fallbacks. Existing Admin edits are never overwritten by the startup seed. This inventory covers reusable baseline templates, not every one-off dynamically assembled task-context string.

## Naming Rules

- Canonical ID format: `{domain}/{agent_or_runtime}/{layer_or_capability}/{name}`.
- Legacy IDs remain as aliases until runtime callers are migrated.
- `handlebars` prompts can be rendered by `RenderPrompt`.
- `fmt` prompts are still formatted by Go callers and are seeded for governance first.
- `go-const` prompts are static text or currently assembled directly from constants.

## Inventory

| Canonical prompt ID | Legacy alias | Layer | Priority | Render style | Current source |
|---|---|---:|---:|---|---|
| `runtime/chat/base_behavior` |  | L0 | P0 | go-const | `PromptChatBaseBehavior` |
| `runtime/chat/consumer_security` |  | L2 | P0 | go-const | `PromptConsumerSecuritySystemContext` |
| `runtime/chat/connector_context` |  | L1 | P0 | go-const | `PromptConnectorContextHeader/Suffix` |
| `runtime/chat/web_search` |  | L1 | P1 | handlebars | `PromptChatWebSearchTemplate` |
| `runtime/multi_agent/route` |  | L0 | P0 | handlebars | `PromptMultiAgentRouteTemplate` |
| `runtime/multi_agent/coordinator` |  | L1 | P0 | go-const | `PromptMultiAgentCoordinator` |
| `runtime/chat/locale_context` |  | L3 | P1 | fmt | `PromptLocaleContextTemplate` |
| `runtime/chat/temporal_context` |  | L4 | P1 | fmt | `PromptTemporalContextTemplate` |
| `runtime/deep_agent/planner` |  | L0 | P0 | fmt | `PromptDeepAgentPlannerTemplate` |
| `runtime/deep_agent/router` |  | L1 | P0 | fmt | `PromptDeepAgentRouteTemplate` |
| `runtime/deep_agent/mode_classifier` |  | L1 | P0 | fmt | `PromptDeepAgentExecutionModeClassifierTemplate` |
| `runtime/deep_agent/tool_usage_reminder` |  | L1 | P1 | go-const | `PromptDeepAgentToolUsageReminder` |
| `runtime/deep_agent/plan_repair` |  | L4 | P1 | fmt | `PromptDeepAgentPlanRepairContextTemplate` |
| `memory/extract/default` | `memory_extract` | L0 | P0 | handlebars | `PromptMemoryExtractionTemplate` |
| `memory/extract/repair` |  | L0 | P1 | fmt | `PromptMemoryExtractionRepairTemplate` |
| `memory/organizer/default` |  | L0 | P1 | fmt | `PromptMemoryOrganizerTemplate` |
| `memory/recall/trigger` |  | L0 | P1 | fmt | `PromptMemoryRecallLLMTriggerTemplate` |
| `memory/episode_summarize/default` | `memory_episode_summarize` | L0 | P0 | handlebars | `PromptMemoryEpisodeSummarizeTemplate` |
| `memory/asset/text_extract` |  | L0 | P1 | fmt | `PromptAssetMemoryTextTemplate` |
| `memory/asset/image_extract` |  | L0 | P1 | go-const | `PromptImageMemoryExtraction` |
| `asset/vision/insight` |  | L0 | P1 | go-const | `PromptVisionAssetInsight` |
| `runtime/structured_json/repair` |  | L0 | P1 | fmt | `PromptStructuredJSONRepairTemplate` |
| `eval/judge/default` | `eval_judge` | L0 | P0 | go-const | `PromptGoldenJudgeSystem` |
| `live/setup/default` | `live_setup` | L0 | P1 | handlebars | default live setup fallback |
| `live/default_assistant` |  | L0 | P1 | go-const | `PromptLiveDefaultAssistantInstruction` |
| `live/tool/run_skill_description` |  | L1 | P1 | go-const | `PromptLiveRunSkillFunctionDescription` |
| `live/tool/web_research_description` |  | L1 | P1 | go-const | `PromptLiveWebResearchFunctionDescription` |
| `live/web_research/preamble` |  | L0 | P1 | go-const | `PromptLiveWebResearchPreamble` |
| `live/skill_router` |  | L1 | P1 | fmt | `PromptLiveSkillRouterTemplate` |
| `runtime/failure_recovery` |  | L0 | P1 | fmt | `PromptFailureRecoveryTemplate` |

## Remaining dynamic prompt builders

The Admin catalog is now automatically populated for the built-in reusable baseline list above. Some prompts are assembled from per-run evidence instead of a stable reusable template and have **not** been migrated in this change. In particular: `buildContextSummaryPrompt` (context compaction), `deepResearchWorkerPrompt` and `deepResearchHarnessAgentPrompt` (worker task/evidence), `deepResearchDeliverableDecisionPrompt` (artifact decision), and the Live startup greeting. Do not assume that editing an unrelated catalog entry changes these builders. Migrate each one with a stable ID, variable contract, and execution test before exposing it as an editable template. The synthetic `runtime/chat/system_prompt_snapshot` is a composed snapshot ID, not an editable source template.

## Legacy Alias Mapping

| Legacy ID | Canonical ID |
|---|---|
| `live_setup` | `live/setup/default` |
| `eval_judge` | `eval/judge/default` |
| `memory_extract` | `memory/extract/default` |
| `memory_episode_summarize` | `memory/episode_summarize/default` |

## Admin Management

Open `/admin` → Build → Prompts. The built-in templates are created on AgentAPI startup when absent, so manual seeding is not required. Filter by `runtime/multi_agent` to edit the route classifier and coordinator. Publish a candidate version (or pin it to production) to change execution; drafts alone do not affect the runtime. The route template variables are `{{recent_conversation}}` and `{{user_request}}` (the former is `(none)` on a new conversation). The coordinator is a static instruction segment; the separate consumer security segment and server tool permissions remain in force. Code baselines are retained as fallback if the prompt registry is unavailable.

The manual seed command below is still available for auditing and migration, but startup seeding only inserts missing canonical prompts and does not update existing Admin versions.

## Seed Command

Dry run:

```bash
go run ./scripts/seed-system-prompts.go -dry-run
```

Seed through Admin API:

```bash
AGENT_API_ADMIN_TOKEN=<token> go run ./scripts/seed-system-prompts.go \
  -api-url http://127.0.0.1:8081 \
  -user-id system-prompt-seed
```

Seed directly through Postgres, useful for local containers and startup jobs where the Admin API requires a browser session cookie:

```bash
AGENT_API_SQL_DSN='postgres://agentapi:agentapi@127.0.0.1:15432/agentapi?sslmode=disable' \
  go run ./scripts/seed-system-prompts.go
```

Print machine-readable inventory:

```bash
go run ./scripts/seed-system-prompts.go -print-inventory
```

## Phase 0 Evidence

- `BuiltinSystemPromptBaselines()` provides the seedable source-of-truth.
- `defaultPromptFallbacks()` now derives from the baseline list, including legacy aliases.
- `scripts/seed-system-prompts.go` can seed via Admin API or direct Postgres.
- Tests cover canonical ID resolution, legacy alias fallback, and unique seed IDs.

## Phase 1 Env Pins

Environment pins are stored in `agent_prompt_environment_pins` and move an environment pointer without mutating immutable prompt versions.

Resolver priority:

1. forced version
2. running experiment variant
3. environment pin
4. published version
5. code fallback

Supported environments:

- `dev`
- `staging`
- `production`

Admin API:

```http
GET  /v1/admin/ops/prompts/{promptID}/env-pins
PUT  /v1/admin/ops/prompts/{promptID}/env-pins/{environment}
POST /v1/admin/ops/prompts/{promptID}/env-pins/{environment}/rollback
```

`PUT` and rollback body:

```json
{
  "version": "v1.2.0",
  "changelog": "Promote after regression pass",
  "eval_run_id": "eval-run-id"
}
```

Local verification:

```bash
docker compose -f deploy/local/docker-compose.yml --env-file .env up -d --build --force-recreate agentapi agentweb
AGENT_API_SQL_DSN='postgres://agentapi:agentapi@127.0.0.1:15432/agentapi?sslmode=disable' \
  go run ./scripts/seed-system-prompts.go
go test ./internal/backend/agentruntime
```

## Phase 2 Chat Assembler

Ordinary chat now assembles model-facing system prompt context through `SystemPromptAssembler`.

Implemented runtime boundary:

- L0 `runtime/chat/base_behavior` resolves through `PromptResolver`.
- L1 `runtime/chat/connector_context` resolves through `PromptResolver` and receives connector runtime lines as template variables.
- L2 `runtime/chat/consumer_security` resolves through `PromptResolver`.
- L3 locale and personalization context enter the assembled snapshot.
- L4 temporal, saved memory, episodic memory, and browser memory enter the assembled snapshot.
- The assembled snapshot is injected only into the LLM session clone and stripped before persistence.
- Normal chat LLM calls write `runtime/chat/system_prompt_snapshot`, `assembled-v1`, and snapshot hash into prompt metadata for usage/eval filtering.

Verification:

```bash
go test ./internal/backend/agentruntime -run 'TestSystemPromptAssembler|TestRuntimeChatInjectsSnapshotAndPromptMetadata|TestRuntimeInjectsTransientContextsForLLMOnly|TestBuiltinSystemPromptBaselinesHaveUniqueSeedIDs'
go test ./internal/backend/agentruntime
```

## Phase 3 DeepAgent Prompt Migration

DeepAgent prompt templates now resolve through the prompt registry before falling back to built-in code baselines.

Migrated prompt IDs:

- `runtime/deep_agent/planner`
- `runtime/deep_agent/router`
- `runtime/deep_agent/mode_classifier`
- `runtime/deep_agent/tool_usage_reminder`
- `runtime/deep_agent/plan_repair`

Runtime behavior:

- Planner, router, and execution-mode classifier LLM calls attach prompt metadata to the call context.
- Tool usage reminder and plan repair context are registry-backed templates used inside downstream prompt construction.
- Production env pin changes for DeepAgent prompt IDs require a completed `eval_run_id`.
- Admin Prompt API decodes URL path parameters so canonical prompt IDs such as `runtime/deep_agent/planner` can be evaluated and pinned through encoded routes.

Default prompt evaluation corpus:

- `deep_agent_prompt_planner@phase3-v1`
- `deep_agent_prompt_router@phase3-v1`

Verification:

```bash
go test ./internal/backend/agentruntime -run 'TestRuntimeDeepAgent.*PromptRegistry|TestRuntimeDeepAgentClassifierAndReminderUsePromptRegistry|TestDeepAgentPromptGoldenSetsAndProductionGate|TestPromptVersionEvalUsesBuiltinDeepAgentPromptGoldenSet|TestRuntimeDeepAgentPlannerCreatesStructuredPlan|TestRuntimeDeepAgentStepRouterParsesLLMJSONRoute|TestPromptEnvironmentPinsResolveAndRollback'
go test ./internal/backend/agentruntime
```

# Prompt 版本管理 / 自动优化 / A/B 对比实施文档

## 背景

当前项目已经具备几块可复用能力：

- `EvaluationEngine`、Golden Set、LLM-as-Judge 和人工 review，可以做离线回放与效果评估。
- `WorkflowEngine` 可以记录复杂流程的 run / step / state，适合作为 prompt 优化流水线的执行载体。
- Admin Console 已有 Evaluation、Skill 版本管理和运行观测入口。
- LLM governance 已记录 provider、model、latency、token、cost、failure 等运行指标。

不足是：业务 prompt 还没有统一版本注册、发布、回滚、线上实验分流和自动候选生成机制。现在如果靠人工直接改代码里的 prompt，很难回答“当前线上用了哪个 prompt、为什么换、效果是否变好、是否能回滚、不同版本如何 A/B 对比”。

本方案目标是补齐一套可面试、可上线、可审计的 prompt engineering 闭环。

## 目标

1. 建立 Prompt Registry：所有关键 prompt 都有 ID、版本、适用场景、变量 schema、状态、changelog、作者、hash。
2. 建立发布流转：draft -> review_pending -> published -> archived，支持回滚。
3. 建立运行绑定：每次 LLM 调用记录 prompt_id、prompt_version、prompt_hash、experiment_id、variant_id。
4. 建立离线评估：新 prompt 版本必须跑 Golden Set / trace replay，对比 baseline。
5. 建立线上 A/B：按用户、会话或租户稳定分桶，灰度观察效果和成本。
6. 建立自动优化：从 failed eval、用户差评、人工 review 中聚类 badcase，生成候选 prompt，离线验证后进入人工 review。

## 非目标

- 不让 LLM 自动把候选 prompt 直接发布到线上。
- 不把 prompt 当成唯一治理手段，schema 校验、工具参数校验、RAG evidence、权限控制仍然是硬约束。
- 不在第一阶段实现复杂多臂赌博机，先做稳定 A/B 和灰度。

## 总体架构

```text
Admin UI
  |-- Prompt Registry
  |-- Prompt Version Diff
  |-- Eval Comparison
  |-- Experiment Console
  |
Backend API
  |-- Prompt API
  |-- Prompt Render API
  |-- Prompt Experiment API
  |-- Prompt Optimization API
  |
Runtime
  |-- Prompt Resolver
  |-- Prompt Renderer
  |-- Experiment Assigner
  |-- LLM Usage Metadata Writer
  |
Evaluation / Workflow
  |-- Golden Set Replay
  |-- Trace Replay
  |-- Candidate Generation Workflow
  |-- Human Review
```

## 数据模型

### agent_prompt_templates

保存一个 prompt 的逻辑实体。

| 字段 | 说明 |
| --- | --- |
| `id` | prompt ID，例如 `live_setup`、`memory_extract`、`rag_answer` |
| `name` | 展示名称 |
| `description` | 用途说明 |
| `scope` | `runtime` / `live` / `memory` / `skill` / `eval` |
| `owner` | 负责人 |
| `metadata` | JSON，记录适用模型、风险级别、默认 golden set 等 |
| `created_at` / `updated_at` | 时间 |

### agent_prompt_versions

保存具体版本内容。

| 字段 | 说明 |
| --- | --- |
| `prompt_id` | 关联 prompt template |
| `version` | 版本号，例如 `v1`、`2026-06-02.1` |
| `status` | `draft` / `review_pending` / `published` / `archived` |
| `content` | prompt 模板正文 |
| `variables_schema` | JSON schema，声明可注入变量 |
| `render_config` | JSON，声明变量缺失策略、token 预算、裁剪策略 |
| `content_hash` | 规范化内容 hash |
| `base_version` | 从哪个版本派生 |
| `changelog` | 变更说明 |
| `created_by` / `reviewed_by` | 创建人与审核人 |
| `created_at` / `published_at` | 时间 |

### agent_prompt_experiments

保存实验配置。

| 字段 | 说明 |
| --- | --- |
| `id` | 实验 ID |
| `name` | 实验名称 |
| `prompt_id` | 目标 prompt |
| `status` | `draft` / `running` / `paused` / `completed` |
| `traffic_scope` | `user` / `session` / `tenant` |
| `allocation` | JSON，例如 `{ "control": 50, "candidate": 50 }` |
| `start_at` / `end_at` | 时间窗口 |
| `guardrails` | JSON，失败率、成本、延迟阈值 |
| `winner_variant_id` | 胜出版本 |

### agent_prompt_experiment_variants

保存实验组。

| 字段 | 说明 |
| --- | --- |
| `experiment_id` | 实验 ID |
| `variant_id` | `control` / `candidate_a` |
| `prompt_version` | 绑定版本 |
| `weight` | 流量权重 |
| `metadata` | JSON |

### agent_prompt_runs

保存每次 prompt 渲染和调用摘要，避免日志里只剩最终回答。

| 字段 | 说明 |
| --- | --- |
| `id` | run ID |
| `request_id` / `job_id` / `session_id` / `user_id` | 关联上下文 |
| `prompt_id` / `prompt_version` / `prompt_hash` | 版本信息 |
| `experiment_id` / `variant_id` | 实验信息 |
| `model` / `provider` | 模型信息 |
| `input_token_count` | prompt token |
| `rendered_preview` | 截断后的渲染预览，避免保存完整敏感上下文 |
| `metadata` | JSON，记录 retrieval version、memory ids、skill name 等 |
| `created_at` | 时间 |

## Prompt Resolver

运行时不再直接引用硬编码 prompt，而是通过 resolver 获取。

输入：

- `prompt_id`
- `user_id`、`session_id`、`tenant_id`
- `model`、`provider`
- `runtime_mode`，例如 chat/live/job/eval
- 可选 `forced_version`

输出：

- `PromptTemplate`
- `PromptVersion`
- `ExperimentAssignment`

解析顺序：

1. 如果请求指定 `forced_version`，优先使用，用于评测和回放。
2. 如果存在 running experiment，按稳定 hash 分桶选 variant。
3. 否则使用当前 `published` 版本。
4. 如果 registry 不可用，回退到代码内置默认 prompt，并记录 `fallback=true`。

稳定分桶 key：

```text
hash(experiment_id + traffic_scope_id) % 100
```

这样同一用户或同一会话在实验期内始终命中同一版本。

## Prompt Renderer

Renderer 负责变量注入、裁剪和安全处理。

能力：

- 按 `variables_schema` 校验必填变量。
- 对 memory、retrieval evidence、history summary 设置独立 token budget。
- 对敏感字段做脱敏或只记录 ID。
- 输出 `prompt_hash`、`rendered_preview`、`token_estimate`。
- 支持 dry-run，供 Admin UI 预览模板。

关键原则：

- prompt 模板版本只表达“如何组织上下文和约束模型”。
- 用户输入、memory、检索结果、工具结果必须作为变量注入，不能混在模板版本正文中。
- 线上日志默认不保存完整 rendered prompt，只保存截断预览和关联 ID。

## API 设计

Prompt 管理：

- `POST /v1/admin/ops/prompts`
- `GET /v1/admin/ops/prompts`
- `GET /v1/admin/ops/prompts/{id}`
- `POST /v1/admin/ops/prompts/{id}/versions`
- `GET /v1/admin/ops/prompts/{id}/versions`
- `GET /v1/admin/ops/prompts/{id}/versions/diff?from_version=...&to_version=...`
- `POST /v1/admin/ops/prompts/{id}/publish`
- `POST /v1/admin/ops/prompts/{id}/rollback`

Prompt 评估：

- `POST /v1/admin/ops/prompts/{id}/versions/{version}/eval`
- `GET /v1/admin/ops/prompts/{id}/versions/{version}/eval-runs`
- `POST /v1/admin/ops/prompts/{id}/versions/{version}/render-preview`

Prompt 实验：

- `POST /v1/admin/ops/prompt-experiments`
- `GET /v1/admin/ops/prompt-experiments`
- `GET /v1/admin/ops/prompt-experiments/{id}`
- `POST /v1/admin/ops/prompt-experiments/{id}/start`
- `POST /v1/admin/ops/prompt-experiments/{id}/pause`
- `POST /v1/admin/ops/prompt-experiments/{id}/complete`

Prompt 自动优化：

- `POST /v1/admin/ops/prompts/{id}/optimize`
- `GET /v1/admin/ops/prompt-optimization-runs`
- `GET /v1/admin/ops/prompt-optimization-runs/{id}`

## 自动优化工作流

复用 `WorkflowEngine`，新增 workflow name：`prompt_optimization`。

步骤：

1. `collect_badcases`
   - 来源：eval failed/warning、用户差评、人工 review、风险事件、工具失败、低 faithfulness。
   - 输出：badcase IDs、分桶标签、失败指标。

2. `cluster_failures`
   - 按失败类型聚类：上下文缺失、指令冲突、格式错误、工具参数错误、幻觉、语气问题、live 主动回复旧任务等。
   - 输出：cluster summary。

3. `generate_candidate_prompt`
   - LLM 基于当前 prompt、cluster summary、约束条件生成候选版本。
   - 输出：candidate content、candidate changelog。

4. `offline_replay`
   - 使用 Golden Set 和 trace replay，对 baseline 与 candidate 做并行评估。
   - 输出：pass rate、faithfulness、format compliance、latency、token、cost 对比。

5. `create_review`
   - 如果达到阈值，创建 `review_pending` prompt version。
   - 如果未达到阈值，只保存 optimization run 和失败原因。

自动优化必须遵守：

- 候选 prompt 不自动发布。
- 每个候选必须说明“解决哪些 badcase”和“可能影响哪些场景”。
- 所有候选必须绑定 baseline version 和 evaluation run ID。

## A/B 对比指标

离线指标：

- Golden Set pass rate
- answer correctness
- answer relevancy
- faithfulness
- context precision / recall
- format compliance
- tool call success rate
- risk finding count

线上指标：

- 用户显式反馈通过率
- 人工 review failed rate
- empty output rate
- LLM failure rate
- tool error rate
- retry / repair rate
- P50 / P95 latency
- TTFT
- prompt token / completion token
- cost per successful task
- Live 模式额外看：误唤醒率、无用户意图时主动回复率、跨 session 内容泄漏率

判胜策略：

- 首先必须满足 guardrails：失败率、风险事件、成本、延迟不能超过阈值。
- 再比较主指标，例如 pass rate 或用户反馈通过率。
- 若主指标提升不足 2% 或置信不足，保持 control。
- 对 high-risk prompt 不直接全量，先人工 review，再 5% -> 25% -> 50% -> 100% 灰度。

## 与现有模块的改造关系

### Evaluation

已有 Golden Set 和 `EvaluateGolden`，需要扩展：

- Candidate metadata 增加 `prompt_id`、`prompt_version`、`prompt_hash`。
- EvaluationResult filter 增加 prompt 维度。
- Admin UI 支持按 prompt version 对比 run。

### LLM Governance

需要在 LLM usage / provider attempt 记录中增加：

- `prompt_id`
- `prompt_version`
- `prompt_hash`
- `experiment_id`
- `variant_id`

这样线上问题能从“模型答错了”定位到“哪个 prompt 版本和实验组导致”。

### Runtime / Live

需要优先接入的 prompt：

- `chat_system`
- `live_setup`
- `live_first_turn`
- `memory_extract`
- `memory_context`
- `rag_answer`
- `tool_repair`
- `skill_wrapper`
- `eval_judge`

Live 模式尤其要把“允许问候，但不要在用户没有明确要求时主动处理历史请求”做成 `live_setup` 的版本化 prompt，并用误触发 badcase 做专项评估。

### Skill

Skill 已有版本管理，但 skill prompt 和系统 prompt 是两个维度：

- Skill 自身版本：能力包内容、允许工具、metadata。
- Prompt Registry：通用包装 prompt、工具调用约束、repair prompt。

两者在运行时都写入 metadata，方便回答“某次结果是 skill 变了，还是 prompt 变了”。

### Admin UI

新增 Prompt 页面：

- Prompt 列表：状态、当前发布版本、最近评测结果、实验状态。
- Version 详情：正文、变量 schema、diff、changelog、hash。
- Eval 面板：选择 golden set，跑 baseline vs candidate。
- Experiment 面板：配置流量、guardrails、查看分桶指标。
- Optimization 面板：查看 badcase cluster、候选 prompt、是否提交 review。

## 分阶段实施计划

### M1：Prompt Registry 与版本 API

- [x] 新增 prompt 数据类型、MemoryStore、SQL schema、sqlc query。
- [x] 新增 Admin Prompt API。
- [x] 支持 create/list/get/version/publish/rollback/diff。
- [x] 单元测试覆盖状态流转、hash、rollback、diff。

验收：

- Admin token 可创建 prompt version。
- 同一 prompt 可保存多个版本。
- published 版本唯一。
- rollback 会生成新的 published 版本或恢复目标版本状态。

### M2：Prompt Resolver / Renderer 接入 Runtime

- [x] 新增 `PromptStore`、`PromptResolver`、`PromptRenderer`。
- [x] 先接入 `eval_judge`、`live_setup`、`memory_extract` 三个高价值 prompt。
- [x] LLM 调用 metadata 写入 prompt 信息。
- [x] Registry 不可用时回退内置 prompt。

验收：

- 线上每次关键 LLM 调用能查到 prompt_id/version/hash。
- Live setup prompt 可通过 Admin 修改后发布生效。
- 测试覆盖变量缺失、fallback、render preview。

### M3：离线评估对比

- [x] Golden evaluation 支持按 prompt version 生成 candidate。
- [x] EvaluationResult 增加 prompt 维度。
- [x] Admin UI 展示 baseline vs candidate 指标。
- [x] 支持一键把 failed/warning 样本加入 review。

验收：

- 能用同一 Golden Set 对比两个 prompt version。
- 能看到指标差异、成本差异、失败样本列表。

### M4：线上 A/B 实验

- [x] 新增 prompt experiment 数据模型和 API。
- [x] Runtime 增加稳定分桶。
- [x] LLM usage / eval summary 支持 experiment filter。
- [x] Admin UI 支持启动、暂停、结束实验。

验收：

- 同一用户或会话稳定命中同一 variant。
- 实验指标可按 variant 聚合。
- 超过 guardrail 后能手动或自动暂停。

### M5：自动优化 Workflow

- [x] 新增 `prompt_optimization` workflow。
- [x] 从 eval/review/feedback 聚合 badcase。
- [x] 生成候选 prompt draft。
- [x] 自动运行 offline replay。
- [x] 通过阈值后创建 `review_pending` version。

验收：

- 输入一个 prompt_id 后，可以生成候选版本和评估报告。
- 候选版本不会自动 publish。
- Workflow steps 可在现有 ops workflow 页面追踪。

## 风险与控制

Prompt 版本过多：

- 控制策略：只对核心 prompt 建 registry；低价值 prompt 暂保留代码默认。

自动优化过拟合：

- 控制策略：按场景分桶评估；保留固定 regression golden set；候选必须人工 review。

A/B 实验污染用户体验：

- 控制策略：默认小流量；高风险 prompt 先内部用户；guardrail 超阈值暂停。

敏感上下文泄露到 prompt run：

- 控制策略：默认保存 preview 和关联 ID，不保存完整 rendered prompt；敏感变量脱敏。

线上 registry 故障影响对话：

- 控制策略：resolver 回退到内置 prompt；记录 fallback 指标；read-through cache。

## 面试回答口径

可以这样概括：

> 我们不是靠感觉调 prompt，而是把 prompt 当成可版本化、可评测、可灰度、可回滚的配置资产。每个核心 prompt 都有版本、hash、changelog 和状态流转；每次 LLM 调用都会记录 prompt version、model version、实验组和评测结果。新版本先跑 golden set 和 trace replay，再进入人工 review，最后通过 A/B 小流量灰度观察 pass rate、faithfulness、工具成功率、延迟和成本。自动优化也只是从 badcase 聚类生成候选，不会直接发布，最终仍受评测和人工审核约束。

## 当前状态

- [x] 已有 Golden Set、LLM-as-Judge、Eval Review 和 Admin Evaluation 基础能力。
- [x] 已有 WorkflowEngine，可承载 prompt 自动优化流水线。
- [x] 已有 Skill 版本管理，可参考其 Admin API 和 diff / rollback 设计。
- [x] 已补 Prompt Registry。
- [x] 已补 Prompt Resolver / Renderer。
- [x] 已补 Prompt Run metadata。
- [x] 已补 Prompt 维度的离线对比。
- [x] 已补 Prompt A/B 实验分流。
- [x] 已补自动优化 workflow。

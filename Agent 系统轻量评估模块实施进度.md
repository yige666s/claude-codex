# Agent 系统轻量评估模块实施进度

**版本：** v0.2  
**日期：** 2026-05-16  
**状态：** 实施中  
**关联文档：** `Agent 系统评估模块技术方案.md`

---

## 1. 实施目标

本阶段不建设独立、重型、专业化评测平台，而是在当前 AgentAPI 产品内实现一套基于真实运行数据的轻量评估闭环，用于回答以下问题：

- Agent 在真实用户会话和真实 Job 中是否跑通，失败在哪里。
- 工具调用是否合理，是否出现权限拒绝、工具失败或空输出。
- LLM 请求是否稳定，成本、Token、延迟是否异常。
- 输出是否为空、是否产出 Artifact、是否触发风险或异常信号。
- 不同时间窗口、用户、Skill、模型、任务类型下的质量指标是否变化。

第一版优先复用当前依赖和已有模块：

- Go 后端：`cmd/agentapi`、`internal/backend/agentruntime`
- 前端：`apps/web`
- 存储：Postgres / 现有 SQL store 模式
- 运维入口：现有 Admin Console
- 运行轨迹：Session messages、Job events、Skill executions、LLM usage、Audit/Risk
- 指标输出：现有 `/metrics` 与 Admin API

---

## 2. 第一版范围

### 2.1 做什么

- 真实数据评估：按时间窗口、用户、session、job、skill、模型筛选真实运行记录。
- 评估批次管理：对一组真实数据生成 eval run/result，不重新执行 Agent。
- 指标计算：用确定性规则和统计聚合计算成功率、失败率、耗时、成本、风险等指标。
- 任务与工具评估：统计 Job、Skill、Tool 的状态、耗时、失败原因、调用分布。
- 成本性能评估：复用 LLM usage 统计 token、成本、延迟、错误。
- 风险评估：复用 Risk scanner/store，标记高风险输出或操作。
- Admin 看板：查看运行记录、通过率、失败样本、人工复核状态。
- 阈值告警：对低成功率、高工具失败率、高风险数量、成本异常进行标记。

### 2.2 暂不做什么

- 不接入 Langfuse、Phoenix、Grafana 等外部评测平台。
- 不引入 Kafka 实时评估流。
- 不做多 Agent 辩论式 Judge。
- 不做复杂语义相似度或向量评分。
- 不新增前端图表库。
- 不做完整在线漂移检测。
- 第一版不维护模拟测试用例，不跑合成样本，不把评估和回归测试混在一起。

---

## 3. 里程碑总览

| 阶段 | 周期 | 状态 | 目标 | 主要交付 |
| --- | --- | --- | --- | --- |
| M0 | 第 0 周 | 完成 | 对齐范围与数据边界 | 本实施进度文档 |
| M1 | 第 1 周 | 完成 | 数据模型与 Store | eval run/result/review SQL 表、Go Store、单元测试 |
| M2 | 第 2 周 | 完成 | 真实数据采集与指标引擎 | Trace 聚合、Stats/Rule/Risk evaluator、单元测试 |
| M3 | 第 3 周 | 完成 | 评估运行 API | Admin Eval API、run/result/review/summary 查询、单元测试 |
| M4 | 第 4 周 | 完成 | Admin UI | Evaluation 页面、失败详情、review 操作 |
| M5 | 第 5 周 | 完成 | 阈值、导出与收尾 | 可配置阈值、CSV/Markdown 导出、文档、测试 |
| M6 | 第 6 周 | 完成 | 日增量指标更新 | UTC+8 05:00 定时汇总前一日真实数据，幂等写入 daily eval run |

---

## 4. 详细实施计划

### M0：范围确认与设计收敛

**状态：** 完成  
**目标：** 固定第一版功能边界，避免按重型评测平台过度设计。

任务：

- 确定第一版只评估真实运行数据，不创建模拟样本，不重新执行 Agent。
- 确定评估数据范围支持按时间窗口、用户、session、job、skill、模型筛选。
- 确定 Admin Console 是唯一管理入口。
- 确定阈值判断优先用于运维提示和报告，不作为第一版发布门禁主路径。

验收：

- 本文档合入仓库。
- 后续实现不需要新增外部服务或新依赖。

---

### M1：数据模型与 Store

**状态：** 完成  
**预计周期：** 3-5 天

新增后端文件建议：

- `internal/backend/agentruntime/evaluation_types.go`
- `internal/backend/agentruntime/evaluation_store.go`
- `internal/backend/agentruntime/evaluation_store_sql.go`
- `internal/backend/agentruntime/evaluation_store_test.go`

建议表结构：

- `agent_eval_runs`
  - `id`
  - `name`
  - `status`
  - `trigger`
  - `scope`
  - `started_at`
  - `completed_at`
  - `total`
  - `passed`
  - `failed`
  - `warning`
  - `metrics`
  - `threshold_status`
  - `summary`

- `agent_eval_results`
  - `id`
  - `run_id`
  - `subject_type`
  - `subject_id`
  - `user_id`
  - `session_id`
  - `job_id`
  - `skill_name`
  - `provider`
  - `model`
  - `status`
  - `score`
  - `input`
  - `output`
  - `metrics`
  - `findings`
  - `created_at`

- `agent_eval_reviews`
  - `id`
  - `result_id`
  - `status`
  - `reviewer`
  - `note`
  - `created_at`
  - `updated_at`

字段说明：

- `scope` 保存本次评估的数据范围，例如 `{"from":"2026-05-15T00:00:00Z","to":"2026-05-16T00:00:00Z","user_id":"","job_status":"","skill_name":""}`。
- `subject_type` 表示结果粒度，第一版支持 `job`、`session`、`skill_execution`。
- `metrics` 保存可聚合指标，例如 latency、token、cost、tool count、risk count。
- `findings` 保存可解释问题列表，例如 tool failed、empty output、risk high、job failed。

验收：

- Memory store 与 SQL store 都能初始化。
- SQL store 支持 run/result/review 的基础 CRUD 与 summary 查询。
- 单元测试覆盖创建、列表、状态更新、结果聚合。

---

### M2：真实数据采集与指标引擎

**状态：** 完成  
**预计周期：** 4-6 天

新增后端文件建议：

- `internal/backend/agentruntime/evaluation_engine.go`
- `internal/backend/agentruntime/evaluation_trace.go`
- `internal/backend/agentruntime/evaluation_rules.go`
- `internal/backend/agentruntime/evaluation_metrics.go`
- `internal/backend/agentruntime/evaluation_engine_test.go`

核心能力：

- 从已有数据组装轻量 trace：
  - Session messages
  - Job events
  - Skill executions
  - LLM usage
  - Risk events

- Stats evaluator：
  - 总任务数
  - 成功率、失败率、取消率
  - job status
  - P50/P95/P99 duration ms
  - token usage 总量与均值
  - estimated cost 总量与均值
  - tool call count
  - skill failure count
  - risk event count

- Rule evaluator：
  - 输出非空
  - Job 未失败或取消
  - 有工具调用时没有工具失败
  - 有 Artifact 预期信号时实际生成 Artifact
  - 无工具失败或权限拒绝
  - 无明显错误文本或异常事件

- Risk evaluator：
  - 关联 risk events
  - 高风险结果标记为 warning 或 failed
  - 需要人工复核的结果创建 review item

验收：

- 已实现 `RuntimeEvaluationTraceSource`，从现有 runtime 数据读取 job/session/skill execution，并关联 messages、job events、skill executions、LLM usage、risk events、artifacts。
- 已实现 `EvaluationEngine`，对真实运行 trace 计算 result、run summary、threshold status，并为失败或高风险结果生成 pending review。
- 已实现 Stats/Rule/Risk 评估逻辑，输出 pass/fail/warning 与可解释 findings。
- 已用单元测试覆盖正常成功 trace、失败/高风险 trace、聚合指标和 review item 生成。
- 规则失败不依赖 LLM，不产生额外推理成本。

---

### M3：评估运行 API

**状态：** 完成  
**预计周期：** 4-6 天

新增或修改：

- `internal/backend/agentruntime/server.go`
- `internal/backend/agentruntime/evaluation_api.go`
- `internal/backend/agentruntime/evaluation_api_test.go`
- `cmd/agentapi/main.go`

Admin API 建议：

- `POST /v1/admin/ops/eval/runs`
- `GET /v1/admin/ops/eval/runs`
- `GET /v1/admin/ops/eval/runs/{id}`
- `GET /v1/admin/ops/eval/results?run_id=...`
- `GET /v1/admin/ops/eval/reviews?result_id=...`
- `GET /v1/admin/ops/eval/summary?from=...&to=...`
- `PATCH /v1/admin/ops/eval/reviews/{id}`

评估请求示例：

```json
{
  "name": "last_24h_job_quality",
  "scope": {
    "from": "2026-05-15T00:00:00Z",
    "to": "2026-05-16T00:00:00Z",
    "subject_type": "job",
    "user_id": "目标用户 ID",
    "session_id": "",
    "job_status": "",
    "skill_name": "",
    "provider": "",
    "model": ""
  },
  "thresholds": {
    "min_success_rate": 0.85,
    "max_tool_error_rate": 0.05,
    "max_high_risk_count": 0,
    "max_p95_latency_ms": 10000
  }
}
```

验收：

- 已实现 Admin token 保护的 Eval API。
- `POST /v1/admin/ops/eval/runs` 会基于真实 runtime 数据执行评测，并持久化 run/result/review。
- `GET /v1/admin/ops/eval/runs`、`GET /v1/admin/ops/eval/runs/{id}`、`GET /v1/admin/ops/eval/results`、`GET /v1/admin/ops/eval/reviews`、`GET /v1/admin/ops/eval/summary` 已可查询评估数据。
- `PATCH /v1/admin/ops/eval/reviews/{id}` 已支持人工复核状态更新。
- 结果查询支持按 run/status/subject/user/session/job/skill/provider/model 过滤。
- 第一版 runtime 评测要求显式传入 `scope.user_id`，避免 Admin 误扫跨用户数据。

---

### M4：Admin Evaluation 页面

**状态：** 完成  
**预计周期：** 4-6 天

修改文件建议：

- `apps/web/src/types.ts`
- `apps/web/src/api/client.ts`
- `apps/web/src/App.tsx`
- `apps/web/src/styles/app.css`

页面结构：

- 顶部汇总：
  - 评估对象总数
  - 通过率
  - 失败数
  - warning 数
  - P95 耗时
  - Token 总量
  - 估算成本
  - 高风险数量

- Run 列表：
  - name
  - trigger
  - status
  - scope
  - pass rate
  - started/completed time

- Result 明细：
  - 输入
  - 输出
  - 规则 findings
  - 关联 job/session
  - 工具调用摘要
  - 风险摘要

- Review 区域：
  - 待复核
  - 已确认通过
  - 已确认失败
  - 备注

验收：

- `/admin` 下已新增 Evaluation 页面。
- 可以查看 run/result/review。
- 失败原因能在页面直接定位。
- 可以按时间、用户、subject type、session、job、Skill、provider、模型过滤。
- 可以创建真实数据 eval run，并更新 eval review 状态。
- 不引入新的前端依赖。

---

### M5：阈值、导出与收尾

**状态：** 完成  
**预计周期：** 3-5 天

第一版重点是运营评估和真实数据报告，不默认把评估作为发布门禁。阈值用于标记当前运行质量是否健康。

阈值指标建议：

- 真实 Job 成功率 >= 85%
- critical risk 数量 = 0
- 工具失败率 <= 5%
- LLM error rate <= 5%
- P95 任务耗时 <= 配置阈值
- 单日成本 <= 配置阈值

报告导出建议：

- JSON：供脚本或外部 BI 读取。
- CSV：导出 result 明细。
- Markdown：生成简要日报/周报。

可选脚本目标：

- `GET /v1/admin/ops/eval/summary?from=...&to=...`
- `GET /v1/admin/ops/eval/results?run_id=...&format=csv`

验收：

- Run summary 已显示阈值状态，并在 summary metrics 中聚合阈值通过/告警/失败批次数。
- Admin UI 支持配置成功率、工具错误率、LLM 错误率、高风险数、P95 延迟、成本阈值。
- `GET /v1/admin/ops/eval/results?run_id=...&format=csv` 已支持导出 result 明细。
- `GET /v1/admin/ops/eval/summary?from=...&to=...&format=markdown` 已支持导出简要报告。
- README 已补充 Admin Evaluation 使用说明。
- 后端单元测试、前端构建与前端测试通过。

---

### M6：日增量指标更新

**状态：** 完成  
**预计周期：** 1-2 天

目标是每天在 UTC+8 05:00 自动处理前一个 UTC+8 自然日的数据，只追加或跳过对应日期的增量评估批次，不每天重扫历史全量数据。

实现方式：

- 后端启动 `DailyEvaluationScheduler`，默认启用。
- 调度时间默认是 UTC+8 05:00，可通过 `AGENT_API_EVAL_DAILY_HOUR` 和 `AGENT_API_EVAL_DAILY_MINUTE` 调整。
- 每次执行计算窗口为 `[前一天 00:00, 当天 00:00)`，均按 UTC+8 自然日切分。
- 每个用户生成确定性的 `daily_incremental` eval run ID；同一天同一用户已存在时跳过，避免重复累计。
- 默认按 active 内置用户分页处理；也可通过 `AGENT_API_EVAL_DAILY_USER_IDS` 指定用户列表。
- 每日增量 run 落库后，现有 summary 查询会聚合这些 run，从而更新当前指标。

配置项：

- `AGENT_API_EVAL_DAILY_ENABLED`：是否启用，默认 `true`。
- `AGENT_API_EVAL_DAILY_HOUR`：UTC+8 本地小时，默认 `5`。
- `AGENT_API_EVAL_DAILY_MINUTE`：UTC+8 本地分钟，默认 `0`。
- `AGENT_API_EVAL_DAILY_USER_IDS`：逗号分隔用户 ID；为空时使用内置用户系统的 active 用户。
- `AGENT_API_EVAL_DAILY_BATCH_LIMIT`：单次最多处理用户数，默认 `200`。
- `AGENT_API_EVAL_DAILY_TIMEOUT`：单次任务超时，默认 `10m`。

验收：

- 每日任务只统计前一个 UTC+8 自然日的数据。
- 同一天同一用户重复触发不会重复写入指标。
- 不执行历史全量重算。
- 后端单元测试通过。

---

## 5. 验证计划

后端：

- `go test ./internal/backend/agentruntime`
- `go test ./cmd/agentapi`

前端：

- `cd apps/web && npm run build`
- `cd apps/web && npm run test`

手动验证：

- 准备真实或本地手动产生的 session/job/skill/usage/risk 数据。
- 触发最近 24 小时 evaluation run。
- 在 Admin 页面查看 run summary。
- 检查成功率、工具失败率、LLM 错误率、成本、P95 延迟。
- 更新一条 review 状态。
- 验证失败结果在 findings 中可解释。

---

## 6. 首批真实数据指标

| 指标类别 | 指标 | 数据来源 |
| --- | --- | --- |
| 任务完成 | Job 成功率、失败率、取消率 | `agent_jobs` |
| 任务耗时 | 平均耗时、P50、P95、P99 | `agent_jobs.started_at/finished_at` |
| 工具质量 | 工具调用次数、失败率、权限拒绝数 | `agent_messages`、`agent_job_events` |
| Skill 质量 | Skill 成功率、失败率、平均耗时、错误类型 | `agent_skill_executions` |
| LLM 性能 | 请求数、错误率、平均延迟 | LLM usage store |
| 成本消耗 | input/output/total tokens、估算成本 | LLM usage store |
| 安全风险 | high/medium/low risk 数量、待复核数 | Risk store |
| 输出健康 | 空输出数、错误输出数、Artifact 产出数 | messages、artifacts、job events |

---

## 7. 风险与处理

| 风险 | 影响 | 处理 |
| --- | --- | --- |
| 真实数据包含敏感内容 | Admin 暴露隐私 | 默认展示摘要，详情沿用 Admin 权限，并支持输出截断 |
| 规则过于粗糙 | 误判较多 | findings 可解释，支持人工 review |
| 历史数据量过大 | 查询慢 | 强制时间窗口、limit、分页，后续加聚合缓存 |
| Trace 不完整 | 难以定位失败 | 第一版从 messages/job events/skill executions 聚合，后续补 telemetry |
| 指标口径不一致 | 团队难以解读 | 在文档和 API 中固定每个指标的计算公式 |
| Admin UI 过重 | 增加维护成本 | 使用现有组件风格，不引入图表库 |

---

## 8. 当前进度记录

| 日期 | 事项 | 状态 | 备注 |
| --- | --- | --- | --- |
| 2026-05-16 | 轻量评估模块范围确认 | 完成 | 确定不采用重型评测平台路线 |
| 2026-05-16 | 实施进度文档创建 | 完成 | 作为后续开发跟踪基线 |
| 2026-05-16 | 调整为真实数据评测路线 | 完成 | 移除第一版测试用例/模拟数据方案 |
| 2026-05-16 | M1 数据模型与 Store 实现 | 完成 | 新增 eval run/result/review 类型、Memory/SQL Store、单元测试 |
| 2026-05-16 | M2 真实数据采集与指标引擎实现 | 完成 | 新增 RuntimeEvaluationTraceSource、EvaluationEngine、Stats/Rule/Risk evaluator、review item 生成与单元测试 |
| 2026-05-16 | M3 评估运行 API 实现 | 完成 | 新增 Admin Eval API、评估结果持久化、review 更新、summary 查询与路由测试 |
| 2026-05-16 | M4 Admin Evaluation UI 实现 | 完成 | 新增 Evaluation Admin 分栏、API client、类型、summary/result/review 界面与前端构建验证 |
| 2026-05-16 | M5 阈值、导出与收尾 | 完成 | 新增可配置阈值、CSV/Markdown 导出、README 说明与导出路由测试 |
| 2026-05-16 | M6 日增量指标更新 | 完成 | 新增 UTC+8 05:00 定时任务，按前一日数据写入幂等 daily_incremental eval run |

---

## 9. 完成定义

第一版完成时应满足：

- 可按时间窗口和过滤条件评估真实 job/session/skill 运行数据。
- 可生成 eval run/result/review。
- 每个失败或 warning 结果都有明确 findings。
- 可查看成功率、失败列表、工具失败、LLM 错误、风险触发、成本耗时。
- 可人工复核失败或 warning 结果。
- 可导出摘要和失败明细。
- 不新增外部基础设施依赖。

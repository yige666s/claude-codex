# PE 模式显式入口实施文档

## 背景

当前 Workspace 输入框下方有三个快捷入口：

- `生成图片`：通过 `/vertex-image-artifact` skill 触发图片生成。
- `思考一下`：前端发送 `thinking_mode=true`，后端仍走普通 Chat / ReAct 链路。
- `查找资料`：前端把用户输入包装成“请使用网页搜索查找最新资料...”。

PE（Plan and Execute）模式不适合再伪装成普通 thinking。它会生成计划、执行多步动作、记录 workflow checkpoint、支持恢复和审计，交互成本和执行成本都高于普通对话。因此本次采用显式入口：把 `思考一下` 替换成 `计划执行`，保留 `查找资料`。

## 当前实施状态

- [x] 已将前端 `思考一下` chip 替换为 `计划执行`。
- [x] 已新增 `agent_mode=plan_execute` 请求字段，`计划执行` 入口不再发送 `thinking_mode`。
- [x] 已在后端 `RouteChat` 中把 `plan_execute` 显式路由为 `deep_agent` durable job。
- [x] 已为 `runJob` 增加 `deep_agent` 分支，真正执行 `Runtime.ExecuteDeepAgentTask`。
- [x] 已将 DeepAgent job 与 `deep_agent_task/v1` workflow run 关联，并通过 job event 输出 workflow 进度。
- [x] 已在 PE job 完成或中止后向当前 session 写回 assistant 可见结果。
- [ ] Phase 4 灰度开关、治理指标细分和 Admin Ops 展示增强后续再做。

## 目标

用户点击 `计划执行` 后发送消息，系统应进入 DeepAgent / PE 模式：

1. 前端明确展示当前为 PE 模式。
2. 请求中携带结构化模式字段，而不是仅靠 prompt 文案或 slash command。
3. 后端把请求路由为 durable job。
4. job 执行时真正调用 `Runtime.ExecuteDeepAgentTask`，而不是继续普通 `Runtime.Chat`。
5. DeepAgent 的 workflow event 通过现有 job stream 展示到 Jobs 面板。
6. 完成后在当前 session 写入可见结果，并保留 workflow run 供 Admin Ops 查看、恢复、取消和审计。

## 非目标

- 不做自动复杂度判断；本次只做用户显式选择入口。
- 不替换 `查找资料`，因为搜索是高频明确动作。
- 不把 PE 模式做成 prompt 前缀，例如“请先规划再执行：...”。这会丢失后端可观测、可恢复、可控执行能力。
- 不把普通 `thinking_mode` 继续保留为独立 chip。若以后仍需要模型 thinking，可放到设置项或模型高级参数中。

## 当前链路

前端：

- 类型：`apps/web/src/features/workspace/workspaceTypes.ts`
  - `ComposerToolID = "image" | "web-search" | "thinking"`
- 入口：`apps/web/src/features/workspace/components/composer/ComposerToolChips.tsx`
  - `thinking` label 为 `思考一下`
- 发送：`apps/web/src/features/workspace/AgentWorkspace.tsx`
  - `selectedComposerTool === "thinking"` 时设置 `thinkingMode`
  - `web-search` 通过 `composerToolContent` 包装用户文本
- API：`apps/web/src/api/client.ts`
  - `chatResponse(..., { thinkingMode })`
  - body 字段为 `thinking_mode`

后端：

- 请求：`internal/backend/agentruntime/server_requests.go`
  - `chatMessageRequest.ThinkingMode`
- 运行时请求：`internal/backend/agentruntime/types.go`
  - `ChatRequest.ThinkingMode`
- 路由：`internal/backend/agentruntime/runtime.go`
  - `RouteChat` 目前只处理 run-as-job skill。
- job 执行：`internal/backend/agentruntime/runtime.go`
  - `runJob` 目前无论 `job.Type` 是什么，都调用 `Runtime.Chat`。
- DeepAgent：
  - `Runtime.ExecuteDeepAgentTask`
  - workflow name 为 `deep_agent_task/v1`
  - `ContextWorkflowEventSink` 已能把 workflow event 转为 job event。

关键风险：只让 `RouteChat` 返回 `JobType: "deep_agent"` 不够，因为当前 `runJob` 不按 job type 分发，最终仍会走普通 Chat。

## 推荐方案

### 1. 前端入口替换

把 `thinking` chip 替换为 `plan-execute`。

建议类型：

```ts
export type ComposerToolID = "image" | "web-search" | "plan-execute";
```

建议文案：

- label：`计划执行`
- aria label：`Use plan and execute mode`
- icon：优先用 lucide `Route`、`ListChecks` 或 `Workflow`
- 状态文案：`Plan and execute ready`

发送逻辑：

- `plan-execute` 不修改用户正文。
- 发送 body 中新增 `agent_mode: "plan_execute"`。
- `web-search` 继续使用现有 prompt 包装。
- `image` 继续使用 slash skill。

### 2. API 请求字段

把前端 API options 从单一布尔值扩展为结构化模式：

```ts
type ChatOptions = {
  thinkingMode?: boolean;
  agentMode?: "chat" | "plan_execute";
};
```

发送 body：

```json
{
  "content": "...",
  "attachment_ids": [],
  "agent_mode": "plan_execute"
}
```

兼容策略：

- `thinking_mode` 暂时保留，避免破坏历史调用。
- `计划执行` chip 只发送 `agent_mode=plan_execute`，不再发送 `thinking_mode`。
- `agent_mode=plan_execute` 优先级高于 `thinking_mode`。
- 后续如果不再需要 thinking chip，可以只在 UI 层移除，不急着删除后端字段。

### 3. 后端请求模型

新增字段：

```go
type chatMessageRequest struct {
    Content        string              `json:"content"`
    AttachmentIDs  []string            `json:"attachment_ids"`
    AttachmentURLs []ChatAttachmentURL `json:"attachment_urls"`
    ThinkingMode   bool                `json:"thinking_mode,omitempty"`
    AgentMode      string              `json:"agent_mode,omitempty"`
}

type ChatRequest struct {
    UserID         string
    SessionID      string
    Content        string
    AttachmentIDs  []string
    AttachmentURLs []ChatAttachmentURL
    ThinkingMode   bool
    AgentMode      string
}
```

建议常量：

```go
const (
    AgentModeChat        = "chat"
    AgentModePlanExecute = "plan_execute"
    JobTypeDeepAgent     = "deep_agent"
)
```

### 4. 路由到 PE job

在 `Runtime.RouteChat` 中增加显式模式分支：

```go
if strings.EqualFold(strings.TrimSpace(req.AgentMode), AgentModePlanExecute) {
    return JobRoutingDecision{
        RunAsJob: true,
        JobType:  JobTypeDeepAgent,
        Reason:   "user selected plan-and-execute mode",
    }
}
```

注意顺序：PE 模式应先于 slash skill 路由判断。否则用户在 PE 模式下输入 `/xxx` 时会被 skill 路由截走，无法表达“用 PE 去完成这个目标”。

### 5. job 执行分发

修改 `runJob`，按 `job.Type` 分发：

```go
switch job.Type {
case JobTypeDeepAgent:
    err = r.runDeepAgentJob(ctx, job, sink)
default:
    err = r.Chat(ctx, ChatRequest{...}, sink)
}
```

新增 `runDeepAgentJob`：

1. 确保用户原始消息写入 session。
2. 调用 `ExecuteDeepAgentTask`。
3. 把 `job.ID` 传入 `DeepAgentTaskRequest.JobID`，使 workflow run 和 job 关联。
4. 发送关键 job event：
   - `deep_agent_started`
   - workflow step events 由 `ContextWorkflowEventSink` 自动输出
   - `deep_agent_completed` / `deep_agent_blocked` / `deep_agent_review_required`
5. 生成最终 assistant 消息并写入 session。

建议请求：

```go
result, err := r.ExecuteDeepAgentTask(ctx, DeepAgentTaskRequest{
    UserID:    job.UserID,
    SessionID: job.SessionID,
    JobID:     job.ID,
    Goal:      job.Content,
    Policy: DeepAgentPolicy{
        MaxSteps:        6,
        MaxActions:      12,
        MaxDuration:     10 * time.Minute,
        StepTimeout:     60 * time.Second,
        NoProgressLimit: 2,
    },
}, nil, nil, nil)
```

最终 assistant 消息建议包含：

- 计划摘要。
- 已完成步骤。
- 最终输出。
- 如果 blocked/review pending，说明阻塞原因和需要用户确认的动作。

### 6. 前端 job 展示

现有 Workspace 已能处理 `event.type === "job"`，并打开 Jobs 面板的流式更新。需要补充：

- `jobStartedMessage(event)` 中增加 deep agent 文案：

```ts
if (event.job?.type === "deep_agent") {
  return "已进入计划执行模式，系统会先生成计划再逐步执行。你也可以从左侧 Jobs 查看进度。";
}
```

- Jobs 面板里 `job.type` 显示为 `PE` 或 `Plan Execute`。
- 对 workflow event 增加更友好的展示：
  - `plan_task succeeded` -> `计划已生成`
  - `execute_controller_loop running` -> `正在执行计划`
  - `verify_final_result running` -> `正在校验结果`

当前 `ContextWorkflowEventSink` 已经把 workflow event 转成 job event，前端可以先复用原始事件列表，后续再做格式化。

### 7. 结果写回 session

PE job 完成后必须在当前会话中生成 assistant 可见消息，否则用户只能从 Jobs 面板看进度。

建议封装：

```go
func (r *Runtime) appendDeepAgentResultMessage(ctx context.Context, userID, sessionID string, result *DeepAgentTaskResult) error
```

内容结构：

```text
计划执行完成。

计划：
1. ...
2. ...

结果：
...
```

如果失败：

```text
计划执行暂时中止：...

已完成：
...

下一步需要你确认：
...
```

### 8. Admin Ops 和恢复

当前 Admin Ops 已能展示 workflow detail，并且 `ResumeWorkflowRun` 对 `deep_agent_task` 有特殊恢复逻辑。因此 PE 入口需要保证：

- `WorkflowRun.JobID = job.ID`
- `WorkflowRun.UserID = job.UserID`
- `WorkflowRun.SessionID = job.SessionID`
- job 事件里包含 `run_id`

这样管理员可以从 job 查到 workflow，也能恢复 DeepAgent。

## 测试计划

### 前端测试

1. `ComposerToolChips` 显示 `计划执行`，不再显示 `思考一下`。
2. 选择 `计划执行` 后状态为 `Plan and execute ready`。
3. 发送消息时 body 包含 `agent_mode: "plan_execute"`。
4. `web-search` 和 `image` 入口行为不变。
5. 收到 `deep_agent` job event 后展示 PE 启动文案。

### 后端测试

1. `RouteChat`：
   - `AgentMode=plan_execute` 返回 `RunAsJob=true`、`JobType=deep_agent`。
   - 普通 chat 不路由 job。
   - run-as-job skill 仍能路由为 `skill`。
2. `handlePostMessage`：
   - JSON body 的 `agent_mode` 正确进入 `ChatRequest`。
3. `runJob`：
   - `job.Type=deep_agent` 调用 `ExecuteDeepAgentTask` 分支。
   - `job.Type=chat/skill` 维持旧行为。
4. DeepAgent job：
   - 创建 workflow run，名称为 `deep_agent_task`。
   - workflow run 关联 `job_id/session_id/user_id`。
   - job events 中出现 workflow step event。
   - 成功后 session 有 assistant 可见结果。
5. 失败和阻塞：
   - blocked/review required 不应被当成普通成功静默吞掉。
   - job status 和 assistant 消息能体现阻塞原因。

建议命令：

```bash
go test ./internal/backend/agentruntime -run 'TestRuntimeRouteChat|TestRuntimeRunDeepAgentJob|TestServerPostMessage'
npm --prefix apps/web test -- --run ComposerToolChips AgentWorkspace
```

具体前端测试命令以项目现有测试脚本为准。

## 分阶段实施

### Phase 1：显式入口和请求字段

- 替换 `thinking` chip 为 `plan-execute`。
- API body 增加 `agent_mode`。
- 后端 request / `ChatRequest` 增加 `AgentMode`。
- 增加 `RouteChat` 单元测试。

完成标记：点击 `计划执行` 后后端能收到 `agent_mode=plan_execute`，并路由为 `deep_agent` job。

### Phase 2：DeepAgent job 真执行

- `runJob` 增加 `deep_agent` 分支。
- 实现 `runDeepAgentJob`。
- 关联 workflow run 和 job。
- 成功/失败结果写回 session。
- 增加后端测试。

完成标记：PE job 不再进入普通 Chat，而是创建 `deep_agent_task/v1` workflow run。

### Phase 3：前端进度体验

- `jobStartedMessage` 增加 deep agent 文案。
- Jobs 面板格式化 workflow event。
- 完成后刷新 session messages、jobs、artifacts。

完成标记：用户能从当前会话看到 PE 最终结果，也能从 Jobs 查看计划和步骤进度。

### Phase 4：灰度和治理

- 增加开关：`AGENTAPI_ENABLE_PLAN_EXECUTE_ENTRY=true`。
- Admin Ops 增加 PE job/filter 标识。
- LLM governance 记录 `agent_mode=plan_execute`。
- Evaluation trace 标记 `execution_mode=plan_execute`。

完成标记：可以按环境打开 PE 入口，并在治理页面区分普通 ReAct 与 PE 运行。

## 验收标准

- UI 不再出现 `思考一下` chip，出现 `计划执行` chip。
- 选择 `计划执行` 后发送任意复杂任务，会收到 `job` event，job type 为 `deep_agent`。
- 该 job 创建 `deep_agent_task/v1` workflow run。
- Jobs 面板能看到计划/执行/校验相关事件。
- 会话最终出现 assistant 结果消息。
- 普通聊天、图片生成、查找资料入口不回归。

## 主要风险

- `runJob` 分支遗漏会导致 PE 入口实际仍走普通 Chat。
- DeepAgent 的最终输出如果只保存在 workflow state，用户会感觉“任务完成但聊天窗口没结果”。
- PE 模式成本更高，需要治理侧记录 token、cost、latency 和失败原因。
- 若 PE 模式允许高风险工具动作，必须依赖现有 risk gate 和 human review，不应绕过权限控制。

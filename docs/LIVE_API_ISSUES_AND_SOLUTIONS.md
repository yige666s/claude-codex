# Live API 问题与方案总账

## 目的

本文档用于沉淀 Gemini Live API 相关的“大型问题”和对应设计方案。

后续处理 Live API 相关问题时遵循这个规则：

- 小问题：直接在对话中回答即可，例如某个参数含义、某段代码位置、一次简单现象解释。
- 大问题：如果涉及架构、状态机、上下文、工具调用、延迟、稳定性、存储一致性或跨前后端改造，需要补充到本文档。
- 已解决问题：记录现象、根因、方案、落地状态、关键代码位置和验证方式。
- 未完全解决问题：记录当前判断、推荐方案和后续落地点，避免重复分析。

## 当前 Live 链路基线

当前 Live 模式不是普通 Chat API 的“每轮完整 messages 请求”。

完整技术方案见：

- `docs/LIVE_API_TECHNICAL_DESIGN.md`

整体链路：

```text
浏览器
-> /v1/sessions/{session_id}/live/ws
-> 后端 VertexLiveService
-> Vertex Gemini Live BidiGenerateContent WebSocket
-> Gemini Live 返回 audio / transcript / tool call
-> 后端转发事件并在 turnComplete 后落库
```

关键文件：

- `internal/backend/agentruntime/live.go`
- `internal/backend/agentruntime/runtime.go`
- `internal/backend/agentruntime/live_e2e_test.go`
- `apps/web/src/features/workspace/hooks/useLiveVoice.ts`

Live 和 Chat 共用同一个业务 session。Live turn 完成后，后端会把语音转写文本和 assistant 回复追加为普通 session messages，因此切回 Chat 模式也能看到 Live 产生的上下文。

## 维护规则

追加新问题时使用以下模板：

```md
## 问题：一句话标题

### 现象

### 根因

### 方案

### 当前状态

### 关键代码

### 验证

### 后续注意
```

如果只是一次参数解释或代码定位，不需要写入本文档。

## 问题：首轮语音响应慢

### 现象

用户首次打开 Live 语音后，第一句话响应明显慢；同一个 Live 会话后续轮次明显更快。

### 根因

首轮慢主要不是模型持续推理慢，而是冷启动链路集中在用户点击后发生：

```text
获取 Live 鉴权 cookie
-> 浏览器 WebSocket 建连
-> 后端获取 Vertex access token
-> 后端连接 Gemini Live 上游 WebSocket
-> 发送 setup
-> 等待 setupComplete
-> 前端再启动麦克风采集
```

### 方案

推荐方案是预热 Live 会话，把 WebSocket、Vertex token、上游 setup 等冷启动步骤前移到用户真正说话前。

详细方案见：

- `docs/LIVE_API_FIRST_TURN_LATENCY_SOLUTION.md`

### 当前状态

已有预热方案文档，并已做过 Live 预热、setup prompt 缓存、token 复用等相关实现。

### 后续注意

首轮延迟问题不能只靠调 VAD 参数解决。VAD 尾音通常是几百毫秒级延迟，首轮慢的核心是连接、鉴权和 setup。

## 问题：Live 上下文是否会忘记前文

### 现象

用户担心 Live 模式不像 Chat 模式每轮发送全量 messages，因此可能忘记之前的对话内容。

### 根因

Chat 模式通常是：

```text
数据库历史 messages + 当前 user message
-> 每轮请求都发送给 LLM
-> assistant message 追加回数据库
```

Live 模式是：

```text
建立一个 Gemini Live WebSocket session
-> setup 时注入上下文
-> 连接期间由 Gemini Live session 维护短期上下文
-> 每个 turnComplete 后，后端把转写和回复落库
```

因此 Live 的记忆分两层：

- 连接内短期上下文：由 Gemini Live API 的有状态 session 维护。
- 连接外恢复上下文：由本项目数据库中的 messages、memory、personalization、summary 等在下次 setup 时注入。

### 方案

不把 Live 当作普通 Chat 的全量 messages 重放，而是使用官方 Live API 支持的历史注入方式：

- setup 中启用 `historyConfig.initialHistoryInClientContent`。
- 收到上游 `setupComplete` 后，后端发送 `clientContent.turns` 作为初始历史。
- 用 `clientContent.turnComplete=true` 结束初始历史注入。
- 再把 `live_setup_complete` 发给前端，允许前端开始采音。

这样历史是作为官方对话历史进入 Live session，而不是混入 `systemInstruction`。

### 当前状态

已落地。

历史窗口参数：

```go
defaultLiveInitialHistoryMaxMessages = 32
defaultLiveInitialHistoryMaxTokens   = 16000
```

关键变化：

- 最近历史不再拼进 `systemInstruction`。
- 通过官方 `clientContent` 发送最近历史。
- 开启官方 `contextWindowCompression.slidingWindow.targetTokens=16000`。
- 如果有 session summary，会作为初始历史的一部分注入。

### 关键代码

- `internal/backend/agentruntime/live.go`
  - `setupMessage`
  - `sendInitialHistory`
  - `liveInitialHistoryPayload`
- `internal/backend/agentruntime/runtime.go`
  - `LiveSystemInstruction`
  - `LiveInitialHistory`

### 验证

已验证：

```bash
go test ./internal/backend/agentruntime -run 'TestLive|TestRuntimeLive'
go test ./internal/backend/agentruntime ./internal/backend/agentapi/run
go test ./...
git diff --check
```

### 后续注意

不要把大量最近对话重新塞回 `systemInstruction`。`systemInstruction` 应保留给系统策略、工具说明、个性化和记忆；对话历史应优先走官方 `clientContent`。

## 问题：`MaxMessages` 和 `MaxTokens` 是官方参数吗

### 现象

代码中曾经使用 `MaxMessages: 12` 和 `MaxTokens: 6000`，容易误以为这是 Google Live API 参数。

### 根因

这两个参数是本项目 `SessionLoadService` 的本地上下文裁剪参数，不是 Google 官方 Live API 字段。

Google 官方相关能力是：

- Live session 自己有上下文窗口。
- `historyConfig.initialHistoryInClientContent` 控制初始历史是否通过 `clientContent` 注入。
- `contextWindowCompression` 控制上下文窗口压缩。

### 方案

本项目仍需要本地裁剪，以控制发送给 Gemini Live 的初始历史规模；但注入方式应走官方 `clientContent`。

当前本地裁剪值：

- `MaxMessages = 32`
- `MaxTokens = 16000`

### 当前状态

已调整为 `32 / 16000`，并从 `systemInstruction` 迁移到官方 `clientContent`。

## 问题：Live 模式容易误触发 Skill/工具

### 现象

用户在 Live 模式下说一些普通话，例如“能听到吗”“今日天气”等，模型可能误触发后端 skill/job。

### 根因

Live 模式接入了 Gemini Live 原生 function calling。当前后端向 Gemini 暴露了统一函数：

```text
run_skill(skill, args, reason)
```

如果函数描述或 system instruction 过于宽泛，例如“create / generate / transform / fetch / analyze / process 都可调用 skill”，模型会把普通口语误判为工具意图。

这和 Chat 模式不同。Chat 模式通常有完整 messages、普通工具选择流程和更明确的文本输入；Live 模式的口语转写更短、更噪、更容易被最近任务上下文带偏。

### 当前状态

已确认：

- 显式 `/skill`  fallback 只处理明确 slash command。
- 非 slash 的自然语言 skill 调用主要来自 Gemini Live 原生 function calling，而不是本地 keyword fallback。

### 推荐方案

需要从三层控制：

1. **收窄函数描述**
   - 只在用户有明确 artifact/skill 意图时调用。
   - 对寒暄、测试麦克风、噪声、模糊问句禁止调用。

2. **服务端二次校验**
   - Gemini 返回 `run_skill` 后，不应无条件执行。
   - 用 skill 的 `description`、`when_to_use`、是否 artifact-producing、用户原始转写、近期上下文做校验。
   - 低置信度时拒绝执行，并让模型正常回答。

3. **工具调用后输出管控**
   - 后端 job/skill 结果和 Gemini Live 后续自然语言回复只能保留一个最终答复来源。
   - 避免工具结果 + 模型补话 + job 刷新共同产生重复回复。

### 后续注意

这是 Live 体验稳定性的核心问题之一。后续如果再次出现“莫名其妙调用工具”，优先检查：

- `liveRunSkillFunctionDeclaration`
- `Runtime.liveSkillContext`
- `ExecuteLiveSkillFunctionCall`
- `receiveLoop` 中 function call 后的输出抑制逻辑

## 问题：Live 模式无法使用 Chat 已接入的 harness tools

### 现象

Chat 模式已经能通过 harness `tools` 目录下的工具完成搜索、抓取、Skill 等能力；但 Live 模式只暴露了一个 `run_skill` 函数。

因此用户在 Live 中说“查一下近期北京天气”时，模型可能回答“我现在不能联网搜索”，而不是像 Chat 一样调用 `WebSearch`。

### 根因

Chat 和 Live 的工具链路不同：

```text
Chat:
session messages -> harness planner -> tool descriptors -> engine executeTool -> tool result -> 下一轮模型

Live:
Gemini Live WebSocket setup -> functionDeclarations -> toolCall -> toolResponse
```

此前 Live setup 中只写死了：

```text
run_skill(skill, args, reason)
```

并没有把 `engine.Descriptors()` 中的 harness tools 转成 Gemini Live `functionDeclarations`，也没有把 Gemini Live 返回的通用 `toolCall` 分发到 `engine.ExecuteTool()`。

### 方案

新增 Live tool 适配层：

1. `Runtime.LiveToolFunctionDeclarations` 从当前 session scope 的 runner 中读取 harness tool descriptors。
2. 只允许安全、用户侧可解释的工具进入 Live，目前为：
   - `WebSearch`
   - `WebFetch`
3. `Skill` 不直接暴露为 harness `Skill` tool，因为它的描述对语音场景过宽，容易误触发；Live 继续使用收窄后的 `run_skill` 专用函数处理 published skill/job。
4. `VertexLiveService` setup 阶段动态写入 Gemini Live `functionDeclarations`。
5. Gemini Live 返回 `toolCall` 后，后端按函数名分发：
   - `web_research` -> 后端文本 runner 聚合执行 WebSearch / WebFetch。
   - `run_skill` -> Live Skill/job 专用执行链路。
6. 工具调用结果会以隐藏 tool call / tool result 形式写回当前 session，供后续上下文和审计使用。

### 当前状态

已落地。

Live 当前支持：

- Gemini Live 原生 function calling。
- 网页研究聚合工具：`web_research`。
- Skill/job 专用函数：`run_skill`。

### 关键代码

- `internal/backend/agentruntime/live.go`
  - `LiveToolFunctionHandler`
  - `liveToolFunctionDeclarations`
  - `handleToolFunctionCalls`
- `internal/backend/agentruntime/runtime.go`
  - `LiveToolFunctionDeclarations`
  - `ExecuteLiveToolFunctionCall`
  - `executeLiveWebResearchFunctionCall`
  - `liveFunctionDeclarationFromDescriptor`
  - `liveHarnessToolAllowlist`

### 验证

新增并通过测试：

```bash
go test ./internal/backend/agentruntime -run 'TestRuntimeLiveToolFunctionDeclarationsExposeWebResearchAndRunSkill|TestRuntimeExecuteLiveToolFunctionCallRunsWebResearch'
go test ./internal/backend/agentruntime
```

### 后续注意

不要把 `Bash`、`Read`、`Write`、`Edit` 等内部/高危工具直接暴露给 Live。语音转写天然更短、更噪，工具描述越宽泛越容易误触发。

如果后续要扩展 Live 工具，优先走窄语义的聚合函数，并为每个工具写面向语音场景的窄描述。

## 问题：工具调用后重复回复

### 现象

Live 中触发 image/job skill 后，页面可能出现多条类似“图片已经生成，可以在 Artifacts 查看”的重复回复。

### 根因

可能同时存在多个输出源：

- Gemini Live 在 tool call 前后产生自然语言。
- 后端 `toolResponse` 发回 Gemini 后，Gemini 继续补一段 assistant 回复。
- skill/job 自己通过事件流写入结果。
- 前端 job refresh 又把已持久化 assistant message 拉回来。

如果后端只清掉 input，没有抑制 Gemini 后续 output，就会出现重复。

### 推荐方案

对于 Live skill/job：

- job skill：后端 job 结果是唯一最终答复来源。
- inline skill：后端结果和 Gemini 总结二选一。
- `handleToolFunctionCalls` 成功执行 skill 后，应抑制当前 turn 后续 Gemini output。
- `RecordLiveTurn` 不应把工具调用期间的中间 assistant 补话重复落库。
- 前端可以做 event id/message id 去重，但后端才是主控制点。

### 当前状态

已完成根因分析，仍建议继续做一轮后端输出抑制和二次校验改造。

## 问题：Live 输入噪声与转写误触发

### 现象

Live 语音输入中会出现短 filler、误唤醒词、重复字词或 ASR 噪声。噪声如果进入上下文或工具判断，会造成误回复或误触发 skill。

### 方案

使用统一噪声词库和规则过滤。

关键文件：

- `scripts/live_transcript_noise.json`
- `apps/web/src/features/workspace/liveTranscriptNoiseConfig.ts`
- `internal/backend/agentruntime/live_noise.go`
- `internal/backend/agentruntime/live_noise_config_gen.go`

### 后续注意

噪声过滤只能处理明确无意义输入，不能用来替代工具意图判断。对于“生成图片”“查询天气”这类语义有效但是否需要 skill 的判断，应由 function calling 约束和服务端二次校验处理。

## 问题：Live 模式是否应该发送全量 messages

### 结论

不应该按普通 Chat 的方式每轮发送全量 messages。

原因：

- Gemini Live 是有状态 WebSocket session，连接内由 provider 维护上下文。
- 实时音频会持续消耗上下文窗口，全量重放会增加 setup 和 turn 延迟。
- 每轮重发全量历史会制造重复上下文，也可能让模型更容易被历史任务带偏。

当前设计：

- setup 阶段发送系统策略、工具、记忆、个性化。
- setupComplete 后通过官方 `clientContent` 注入裁剪后的初始历史。
- 连接内实时输入走 `realtimeInput`。
- turnComplete 后把转写和回复落库，供下次重连恢复。

## 问题：Live 与 Chat 是否共用上下文

### 结论

共用同一个业务 session，但使用方式不同。

- Chat：每次调用时由后端加载 messages 并构造普通 LLM 请求。
- Live：连接启动时注入最近历史和记忆；连接内由 Gemini Live session 维护上下文；turn 结束后再落库。

因此：

- Live 中说过的话会进入同一个 session，Chat 后续能看到。
- Chat 中已有的最近历史也会在下次 Live setup 时注入。
- Live 当前连接内的短期状态在 provider 侧，不完全等同于数据库中已经持久化的 messages。

## 问题：Live 中 attachment/artifact 与视觉记忆

### 现象

图片类 artifact 曾经有“抽取记忆”的功能，但容易把一次性的图片内容污染长期记忆。

### 当前方案

图片/artifact 生成后自动触发 asset insight，而不是直接写入 durable memory。

当前设计区分：

- artifact insight：用于描述、检索、回看、候选记忆。
- durable memory：只保存稳定偏好、长期事实和用户确认后的记忆。

关键文件：

- `internal/backend/agentruntime/asset_insights.go`
- `internal/backend/agentruntime/migrations/postgres/00025_asset_insights.sql`

### 后续注意

Live 中如果生成图片，不应因为模型一句“这是一只狗”就直接写入长期记忆。应先进入 artifact insight，再由用户行为或后续确认决定是否升级为 memory。

## 问题：Live 模式复杂网页搜索不稳定

### 现象

当用户在 Live 模式中要求“去网上搜索具体数字”“比较一段时间内走势”“查最新排名/模型/价格”等复杂网页问题时，模型可能出现：

- 先凭记忆回答，没有真正搜索。
- 只说半句，例如“根据 3 月 1 日至 5 月...”，没有完成整合。
- 多轮搜索上下文混乱，把用户追问当成普通语音轮次。
- 工具调用结果回来后，Live 模型仍然补出不完整或缺来源的回答。

### 根因

Live 的强项是低延迟实时对话，不是长链路检索研究。之前 Live 直接暴露底层 `WebSearch` / `WebFetch` 给 Gemini Live：

```text
Live 模型
-> 决定是否 WebSearch
-> 等搜索
-> 决定是否 WebFetch
-> 等抓取
-> 再组织语音回复
```

这个链路对实时语音不友好：任意一步慢、没有继续调用、或中间产生了 assistant transcript，都可能导致回复半截或缺少事实依据。

### 方案

Live 不再直接暴露底层 `WebSearch` / `WebFetch`，而是暴露一个 Live 专用虚拟函数：

```text
web_research(query, requirements, reason)
```

Gemini Live 只负责判断“当前用户是否需要网页研究”。一旦需要，后端用普通文本 runner 执行完整研究子任务：

```text
Live function call web_research
-> 后端 RunGeneratedPrompt
-> 内部可多步调用 WebSearch / WebFetch
-> 产出完整、有来源、不断句的答案
-> 作为 functionResponse 返回给 Gemini Live
```

这样把复杂检索从实时语音回合里拆出去，降低 Live 模型自己多步规划失败的概率。

### 当前状态

已落地：

- Live setup 只暴露 `web_research`，不再直接暴露 `WebSearch` / `WebFetch`。
- `web_research` 使用更长超时（默认 75s，且不低于 runtime turn timeout）。
- 研究结果以 hidden tool call/tool result 落库，后续上下文可见。
- 仍保留低层 `WebSearch` / `WebFetch` 的服务端兼容执行能力，但不主动声明给 Live 模型。

### 关键代码

- `internal/backend/agentruntime/runtime.go`
  - `liveWebResearchFunctionDeclaration`
  - `executeLiveWebResearchFunctionCall`
  - `liveWebResearchPrompt`
- `internal/backend/agentruntime/live_test.go`
  - `TestRuntimeLiveToolFunctionDeclarationsExposeWebResearchAndRunSkill`
  - `TestRuntimeExecuteLiveToolFunctionCallRunsWebResearch`

### 验证

已验证：

```bash
go test ./internal/backend/agentruntime -run 'TestRuntimeLiveToolFunctionDeclarationsExposeWebResearchAndRunSkill|TestRuntimeExecuteLiveToolFunctionCallRunsWebResearch|TestLiveSetupMessageDisablesProviderVADAndEnablesResumption'
go test ./internal/backend/agentruntime
go test ./...
```

### 后续注意

`web_research` 解决的是复杂搜索的稳定性，不等于所有 Live 搜索都应该长时间阻塞。后续可以继续优化：

- 前端展示“正在搜索/正在整理资料”的 Live tool 状态。
- 对超长研究自动建议切到文本模式。
- 对高风险事实（金融、医疗、法律）强制输出来源和日期。

## 已落地决策摘要

| 主题 | 决策 | 状态 |
| --- | --- | --- |
| 首轮慢 | 预热 Live session，减少首轮冷启动 | 已有方案和实现 |
| 上下文注入 | 用官方 `clientContent` 初始历史，不再塞进 `systemInstruction` | 已落地 |
| 本地历史窗口 | `32 messages / 16000 tokens` | 已落地 |
| 上下文压缩 | 用官方 `contextWindowCompression.slidingWindow` | 已落地 |
| 明确 slash skill | 本地 fallback 只处理明确 `/skill` | 已落地 |
| 自然语言 skill | 走 Gemini Live 原生 function calling，但需要二次校验 | 待加强 |
| 重复回复 | 需要后端统一最终输出源并抑制 tool 后模型补话 | 待加强 |
| 噪声过滤 | 使用统一噪声词库 | 已落地 |
| 图片记忆 | artifact insight 先行，避免污染 durable memory | 已落地 |
| 复杂网页搜索 | Live 暴露 `web_research` 聚合工具，后端完成多步搜索/抓取/整合 | 已落地 |

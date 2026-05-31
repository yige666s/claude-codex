# Web Search Grounding Design

## 背景

当前系统已有两类搜索能力：

- Harness 工具层的 `WebSearch` / `WebFetch`，适合所有 provider 的通用 fallback。
- Vertex/Gemini 的原生 Google Search grounding，适合支持 `googleSearch` tool 的 Gemini 模型，由模型自行判断是否需要搜索并返回 grounding metadata。

用户诉求是：Chat 和 Live 模式在需要联网事实时优先使用官方能力，不能支持时自动回落到现有工具；业务层不要靠关键词穷举覆盖所有搜索意图。

## 官方依据

- Vertex AI Google Search grounding 文档要求在请求 `tools` 中传入 `googleSearch`，REST 形态是 `{"tools":[{"googleSearch":{}}]}`，也支持 `exclude_domains`。
- Vertex Gemini 推理请求体中 `tools` 是标准外部工具入口，可包含 function declarations。
- Google 文档列出的搜索 grounding 支持模型包括 Gemini 3 Pro、Gemini 2.5 Pro/Flash/Flash-Lite、Gemini 2.0 Flash，以及 Gemini 2.5/2.0 Flash Live 相关预览模型。
- Grounded response 会返回 `groundingMetadata`，其中可能包含 `webSearchQueries`、搜索建议、grounding chunks/supports。展示接地回答时，Google Search suggestion 有额外展示合规要求。
- `DynamicRetrievalConfig` 属于旧/特定 Agent Platform 检索配置；当前 Gemini 2.0+ 推荐使用 `googleSearch` 字段。

参考：

- https://docs.cloud.google.com/vertex-ai/generative-ai/docs/grounding/grounding-with-google-search?hl=zh-cn
- https://docs.cloud.google.com/vertex-ai/generative-ai/docs/model-reference/inference?hl=zh-cn
- https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/Shared.Types/DynamicRetrievalConfig

## 设计目标

1. 支持模型优先使用原生 `googleSearch`，把“是否搜索”的判断交给模型。
2. 不支持原生 grounding 的模型继续使用系统已有 `WebSearch` / `WebFetch`。
3. Chat 和 Live 模式共用一套 provider 能力判断，避免两套规则漂移。
4. 保留运维开关，遇到模型/API 兼容问题时可快速关闭。
5. 保存 grounding metadata，给后续 UI 展示来源、搜索建议和合规标识留接口。

## 模式

`GoogleSearchGrounding` 支持三种模式：

| 模式 | 行为 |
| --- | --- |
| `auto` | 默认行为。支持模型会挂载 `googleSearch`，由模型决定是否实际搜索；不支持模型保留 `WebSearch` fallback。 |
| `always` | 强制在支持模型上挂载 `googleSearch`。用于 Live `web_research` 这类明确搜索子任务。 |
| `off` | 完全禁用原生 `googleSearch`，只走现有工具 fallback。 |

配置入口：

- `AGENT_API_GOOGLE_SEARCH_GROUNDING`
- `GOOGLE_SEARCH_GROUNDING`

## Chat 流程

```text
用户请求
-> Planner 构造 MessageRequest，默认 GoogleSearchGrounding=auto
-> provider 判断 provider/model 是否支持 googleSearch
   -> 支持：请求体 tools 追加 googleSearch；过滤 WebSearch fallback，保留 WebFetch 等其他工具
   -> 不支持：不追加 googleSearch；保留 WebSearch/WebFetch
-> Gemini/Vertex 返回文本、tool calls、groundingMetadata
-> MessageResponse 暴露 groundingMetadata
```

为什么原生 grounding 时过滤 `WebSearch`：

- 避免 Gemini 同时看到两个“搜索入口”，减少重复搜索和工具选择抖动。
- `WebFetch` 保留，因为用户指定 URL 或模型需要抓取明确页面时，它仍然有价值。

## Live 流程

Live setup 会在支持的 Vertex Live 模型上声明：

```json
{"googleSearch": {}}
```

同时继续暴露 Live 专用聚合函数：

```text
web_research(query, requirements, reason)
```

二者分工：

- 简单事实问题：Live 模型可以直接使用原生 Google Search grounding。
- 复杂、长链路、需要多步搜索/抓取/整合的问题：Live 模型调用 `web_research`，后端使用文本 runner 完成研究子任务。
- `web_research` 子任务会通过上下文强制 `GoogleSearchGrounding=always`，支持模型走原生搜索，不支持模型继续使用 `WebSearch` / `WebFetch`。

## 支持模型判断

当前本地白名单按模型 ID 字符串匹配：

- `gemini-2.0-flash`
- `gemini-2.5-pro`
- `gemini-2.5-flash`，覆盖 Flash-Lite
- `gemini-3` 且包含 `pro`
- `gemini-live-2.5-flash`
- `gemini-2.5-flash-live`
- `gemini-2.0-flash-live`

provider 限定为 `vertex` / `gcp` / `gemini` / `google`。完整 Vertex resource path 会先规整为 `/models/{model}` 后面的模型 ID。

## 已实现代码

- `internal/harness/provider/google_search_grounding.go`
  - 原生 grounding 模式解析、支持模型判断、fallback tool 过滤。
- `internal/harness/provider/gemini.go`
  - Gemini request 支持 `googleSearch` tool。
  - 非流式和流式响应解析 `groundingMetadata`。
- `internal/harness/provider/vertex.go`
  - Vertex Gemini request 复用同一套 `googleSearch` tool 构造逻辑。
- `internal/harness/provider/planner.go`
  - Chat planner 默认请求 `GoogleSearchGrounding=auto`。
- `internal/backend/agentruntime/live.go`
  - Live setup 在支持模型上声明原生 `googleSearch`。
- `internal/backend/agentruntime/runtime.go`
  - Live `web_research` 强制搜索子任务使用原生 grounding，不能支持时自动 fallback。
- `internal/harness/query/query.go`
  - 兼容 harness query 入口的默认 auto 模式。
- `internal/backend/agentapi/bootstrap/llm.go`
  - 从环境变量注入 provider config。

## 验证

覆盖测试：

- 支持模型会向 Vertex 请求体写入 `googleSearch`。
- 原生 grounding 开启时会过滤 `WebSearch` fallback、保留 `WebFetch`。
- 不支持模型保留 `WebSearch` fallback。
- planner 默认请求 `auto`，上下文可以覆盖为 `off`。
- Live setup 会为支持的 Live 模型追加 `googleSearch`，环境变量 `off` 可关闭。
- Live `web_research` 会把 grounding 模式传成 `always`。

## 后续事项

- UI 目前只保存/传递 `groundingMetadata`，还未完整渲染 Google Search suggestion、grounding chunks 和引用位置。正式面向用户展示 grounding answer 前，应按 Google 展示要求补齐 UI。
- 支持模型列表后续最好从 provider/model 数据库配置中维护，避免官方模型变更后需要改代码。
- `exclude_domains`、用户地理位置 `latLng` 暂未接入；如业务需要本地化搜索或域名排除，可以扩展 `GoogleSearchGrounding` 配置。

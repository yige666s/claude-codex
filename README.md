# claude-codex

本仓库包含两个相关但定位不同的项目：

1. **AgentAPI**：主项目。面向消费者/产品形态的 Agent 工作区，包含 Go 后端、React Web 前端、生产运行时和部署方案。
2. **claudecode Go 重构**：副项目。将 Claude Code 的 TypeScript CLI/TUI、编排、工具、运行时和外围能力逐步重构为 Go，主要代码位于 `internal/app`、`internal/ui`、`internal/harness` 和部分 `internal/backend`。

This repository contains two related projects:

1. **AgentAPI**: the primary product. It is a consumer-facing agent workspace with a Go backend, a React web frontend, a production runtime, and deployment assets.
2. **claudecode Go refactor**: the secondary project. It ports Claude Code's TypeScript CLI/TUI, orchestration, tools, runtime, and platform capabilities to Go.

---

## 中文

### 第一部分：AgentAPI 主项目

AgentAPI 是当前仓库的主要产品面。它由 `cmd/agentapi` 后端和 `apps/web` 前端组成，提供登录注册、会话聊天、长任务、技能、附件、生成物、记忆、管理后台、审计、风控和生产部署能力。

#### 当前定位

- **后端入口**：`cmd/agentapi`
- **前端入口**：`apps/web`
- **核心后端运行时**：`internal/backend/agentruntime`
- **生产装配层**：`internal/backend/agentapi`
- **默认数据库**：Postgres
- **缓存和限流**：Redis
- **对象存储**：S3 兼容接口，当前生产模板面向 Cloudflare R2
- **默认 LLM 提供方**：Vertex AI，也保留多 provider 抽象
- **部署方式**：Docker Compose，本仓库包含本地和生产模板

#### 主要能力

- 用户注册、登录、刷新令牌、邮箱验证、退出登录和账号删除。
- 会话列表、聊天记录、SSE 流式响应、断线重连和事件回放。
- 长任务创建、取消、事件时间线和任务状态管理。
- 附件上传、下载、删除、预览，以及消息附件发送。
- 生成物 artifact 的登记、列表、预览、下载和删除。
- Skills 注册表、技能浏览、技能执行历史、策略控制和后台管理。
- 记忆抽取、整理、评分、维护、删除、导出和用户级开关。
- 管理后台：用户、技能、会话、任务、artifact、健康、成本、审计和风险处理。
- 生产基础设施集成：Postgres、Redis、Kafka、S3/R2、Qdrant/Elastic、Prometheus。

#### 目录结构

| 路径 | 说明 |
| --- | --- |
| `cmd/agentapi` | AgentAPI 后端二进制入口。 |
| `apps/web` | React/Vite 前端和 `/admin` 管理后台。 |
| `internal/backend/agentapi` | AgentAPI 的配置、依赖装配、运行生命周期和 workers。 |
| `internal/backend/agentruntime` | 核心 Web Agent Runtime，负责 HTTP API、WebSocket、sessions、jobs、memory、skills、artifacts、admin/risk 等。 |
| `internal/backend/services` | 产品服务层，例如 analytics、api、compact、context、cost、history、mcp、oauth、tasks、tokens、tools、voice、x402。 |
| `internal/backend/googleauth` | Google service account 到 Vertex AI 访问令牌的辅助实现。 |
| `.claude/skills` | 本地 skills 目录，AgentAPI 可以加载这里的技能。 |
| `deploy/local` | 本地 Docker Compose 开发栈。 |
| `deploy/production` | 生产部署示例、前端 nginx 配置、发布、扩容和备份恢复文档。 |
| `docs/api/agentapi.openapi.yaml` | AgentAPI OpenAPI 描述。 |

#### 技术栈

- **后端**：Go 1.24.4、chi、gorilla/websocket、pgx、goose、Prometheus client、Redis、Kafka、MinIO S3 client、JWT。
- **前端**：React 18、Vite、TypeScript、Tailwind CSS 4、Radix UI、lucide-react、Framer Motion、Vitest、Playwright。
- **部署**：Docker Compose，可扩展到拆分 API/worker、托管 Postgres/Redis/object storage 和 Kubernetes 风格部署。

#### 快速启动：本地完整栈

推荐用 Docker Compose 跑完整 AgentAPI 栈：

```bash
mkdir -p secrets
# 放入具备 Vertex AI 权限的 service account JSON:
# secrets/vertex-service-account.json

export GOOGLE_APPLICATION_CREDENTIALS="secrets/vertex-service-account.json"
export VERTEX_PROJECT_ID="REPLACE_WITH_GCP_PROJECT"
export VERTEX_LOCATION="us-central1"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"

docker compose -f deploy/local/docker-compose.yml up --build
```

打开：

- Web App: `http://localhost:8080`
- AgentAPI: `http://localhost:8081`
- Health: `http://localhost:8081/healthz`
- Readiness: `http://localhost:8081/readyz`

更多本地配置见 [deploy/local/README.md](deploy/local/README.md)。

#### 前端开发

先启动后端，然后运行：

```bash
cd apps/web
npm ci
npm run dev
```

Vite 默认把 `/v1`、`/healthz`、`/readyz` 和 `/metrics` 代理到 `http://localhost:8081`。需要换后端地址时：

```bash
AGENT_API_DEV_TARGET=http://localhost:8082 npm run dev
```

#### 直接运行后端

```bash
go run ./cmd/agentapi \
  -addr :8081 \
  -store-backend sql \
  -sql-driver pgx \
  -sql-dialect postgres \
  -sql-dsn "$AGENT_API_SQL_DSN" \
  -enable-user-system \
  -auth-mode jwt \
  -jwt-secret "$AGENT_API_JWT_SECRET" \
  -llm-provider vertex \
  -model gemini-2.5-flash
```

#### 生产配置

生产配置模板在 [deploy/production/.env.example](deploy/production/.env.example)。重要配置分组：

| 分组 | 关键变量 |
| --- | --- |
| Auth | `AGENT_API_JWT_SECRET`、`AGENT_API_ADMIN_TOKEN`、JWT/session 设置 |
| Email | `AGENT_API_EMAIL_PROVIDER=resend`、`AGENT_API_RESEND_API_KEY`、`AGENT_API_EMAIL_FROM` |
| Storage | `AGENT_API_ARTIFACT_S3_ENDPOINT`、`AGENT_API_ARTIFACT_S3_ACCESS_KEY`、`AGENT_API_ARTIFACT_S3_SECRET_KEY`、`AGENT_API_ARTIFACT_S3_BUCKET` |
| Vertex | `GOOGLE_APPLICATION_CREDENTIALS`、`VERTEX_PROJECT_ID`、`GOOGLE_CLOUD_PROJECT`、`VERTEX_LOCATION` |
| Runtime | `AGENT_API_SKILL_DIRS`、`AGENT_API_SKILL_POLICY`、`AGENT_API_OPERATION_RATE_LIMITS`、`AGENT_API_RETENTION_DAYS` |
| Frontend | `VITE_AGENT_API_BASE_URL`，用于前后端分离域名 |

生产环境建议使用 service account JSON，而不是短期 access token：

```env
AGENT_API_SECRETS_DIR=/opt/agentapi/secrets
GOOGLE_APPLICATION_CREDENTIALS=/run/agentapi/secrets/vertex-service-account.json
VERTEX_PROJECT_ID=your-gcp-project
GOOGLE_CLOUD_PROJECT=your-gcp-project
VERTEX_LOCATION=us-central1
```

#### 部署与运维

测试服务器上的 Compose 部署示例：

```bash
cd /opt/agentapi/repo
docker compose --env-file /opt/agentapi/.env -f deploy/local/docker-compose.yml up -d --build
```

常用检查：

```bash
curl -fsS http://localhost:8081/healthz
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8081/metrics

docker ps
docker logs --tail=100 claude-codex-agentapi
docker logs --tail=100 claude-codex-agentweb
```

更多部署文档：

- [deploy/production/README.md](deploy/production/README.md)
- [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md)
- [deploy/production/RELEASE_FLOW.md](deploy/production/RELEASE_FLOW.md)
- [deploy/production/SIZING.md](deploy/production/SIZING.md)
- [deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md)

### 第二部分：claudecode Go 重构副项目

claudecode Go 重构是仓库里的第二条主线：把 Claude Code 的 TypeScript 实现迁移到 Go。它不是当前主要产品入口，但为 AgentAPI 和本地开发代理提供了大量可复用的 agent runtime、工具、权限、skills、memory、MCP、provider 和 orchestration 能力。

#### 当前定位

- **本地 TUI 入口**：`cmd/tui`
- **应用装配层**：`internal/app`
- **终端 UI**：`internal/ui/tui`
- **核心 harness**：`internal/harness`
- **远程/服务端边界**：`internal/backend/bridge`、`internal/backend/remote`、`internal/backend/upstreamproxy`
- **公共基础包**：`internal/public`

默认本地执行链是：

```text
cmd/tui
  -> internal/app/cli
  -> internal/harness/engine
  -> queryRuntime
  -> internal/harness/queryengine
  -> internal/harness/query
```

#### 核心模块地图

| 模块域 | 说明 |
| --- | --- |
| `internal/app/cli` | Cobra 命令树、slash 命令、运行模式选择、TUI registry、MCP stdio、bridge continuation、engine 构建。 |
| `internal/app/config`、`settings` | 本地配置、环境变量覆盖、settings schema、MDM/managed settings、工具权限配置。 |
| `internal/app/bootstrap`、`entrypoints`、`migrations` | 启动生命周期、session/worktree/tmux 初始化、迁移、关闭流程。 |
| `internal/app/auth`、`securestorage` | OAuth、trusted device、keychain/plaintext secret store。 |
| `internal/ui/tui` | Bubble Tea TUI，包含 model/update/view、overlay、permission broker、markdown 渲染和命令适配。 |
| `internal/harness/engine` | 本地执行 facade，连接 planner、tools registry、permission checker、session 和 query runtime。 |
| `internal/harness/queryengine`、`query` | TS 对齐的 query runtime，负责 turn loop、模型调用、工具执行、预算、auto compact、SDK 形态事件。 |
| `internal/harness/tools`、`tool` | 工具注册表、simple tool、rich tool、ToolUseContext、StreamingExecutor，以及 bash/file/search/web/mcp/lsp/tasks/team/worktree 等工具族。 |
| `internal/harness/permissions` | default/plan/bypass/auto 权限模式、规则解析、审批请求和 TUI/bridge 决策通道。 |
| `internal/harness/agent`、`coordinator`、`swarm` | 子 agent、team、worker、tmux/iTerm2/in-process teammate backend 和协作编排。 |
| `internal/harness/plugins`、`skills` | 插件 manifest、skills、agents、hooks、MCP 配置加载和 runtime 消费。 |
| `internal/harness/state`、`storage` | session 状态、messages、usage、JSONL transcript、snapshot。 |
| `internal/harness/memory`、`memdir`、`compact`、`budget` | 记忆、上下文压缩、token 预算和兼容层。 |
| `internal/harness/provider`、`api`、`anthropic` | Anthropic/OpenAI/Qwen/Gemini/Bedrock/Vertex/custom provider 抽象。 |
| `internal/harness/mcp` | MCP server/client/resource 支持，包含 stdio、HTTP 和 SSE 面。 |
| `internal/public` | 公共类型、schema、错误、文件系统工具和限流基础。 |

#### 需要单独关注的能力模块

这些能力不是简单工具子目录，而是影响运行时安全、后台执行、平台能力或可观测性的独立模块：

- `internal/harness/analysis`：命令/工具分析、只读判断和安全提示基础。
- `internal/harness/sandboxadapter`：沙箱策略和工具执行适配边界。
- `internal/harness/websandbox`：Web/浏览器沙箱相关能力。
- `internal/harness/computeruse`：计算机/浏览器控制能力。
- `internal/harness/hooks`：生命周期 hook 和插件扩展点。
- `internal/harness/background`：后台作业和长期任务基础。
- `internal/harness/tasks`：任务状态、输出、停止、快照和工具面。
- `internal/harness/telemetry`：事件、trace、运行时观测。
- `internal/harness/powershell`：Windows PowerShell 执行支持。

外围能力目前合并看待：

- `internal/harness/github`
- `internal/harness/deeplink`
- `internal/harness/teleport`
- `internal/harness/dxt`
- `internal/harness/nativeinstaller`
- `internal/harness/vim`
- `internal/harness/buddy`
- `internal/harness/ultraplan`

#### 运行本地 TUI

```bash
make run-tui
```

或直接运行：

```bash
go run ./cmd/tui
```

常见 provider 相关配置可以通过环境变量或 `~/.claude-codex/config.json` 设置。配置 home 可用 `CLAUDE_GO_HOME` 覆盖。provider 支持包括 Anthropic、OpenAI、Qwen、Gemini、Bedrock、Vertex 和 custom OpenAI-compatible endpoint。

#### 重构状态

整体判断：

- 核心执行链已经形成闭环，`engine` 已经收敛为薄适配层，默认 runtime 切到 `queryengine -> query`。
- tools、permissions、skills、tasks、memory、compact、provider、MCP、storage 等主链已具备较高完成度。
- CLI 命令矩阵已经成型，但命令覆盖和 TS 细节仍在补齐。
- TUI 具备基本骨架，但与 TypeScript 版本相比仍是最大差距。
- bridge/remote/server、插件管理 UI、GitHub/git、computer/browser 等外围能力仍有迁移和体验完善空间。

详细迁移文档：

- [docs/REFACTORING_MODULES.md](docs/REFACTORING_MODULES.md)
- [docs/MIGRATION_STATUS.md](docs/MIGRATION_STATUS.md)
- [docs/REFACTORING_SUMMARY.md](docs/REFACTORING_SUMMARY.md)
- [docs/REFACTOR_PROGRESS.md](docs/REFACTOR_PROGRESS.md)
- [docs/refactor/CORE_REFACTOR_STATUS.md](docs/refactor/CORE_REFACTOR_STATUS.md)
- [docs/Architecture-diagram/claudecode Go 重构子项目完整架构图.drawio_副本.pdf](docs/Architecture-diagram/claudecode%20Go%20重构子项目完整架构图.drawio_副本.pdf)
- [docs/Architecture-diagram/claudecode Go 重构：internal 全模块地图.drawio_副本.pdf](docs/Architecture-diagram/claudecode%20Go%20重构：internal%20全模块地图.drawio_副本.pdf)

### 开发命令

```bash
make fmt
make test
go test ./internal/backend/agentruntime ./internal/backend/googleauth ./internal/harness/provider ./cmd/agentapi
go build ./cmd/agentapi
go build ./cmd/tui

cd apps/web
npm ci
npm run build
npm run test
npm run e2e
```

### 安全注意事项

- 不要提交 `.env`、R2 keys、Resend keys、admin token、JWT secret 或 service account JSON。
- `secrets/` 只用于本地密钥材料，并应保持 gitignored。
- 生产环境优先使用 service account 调用 Vertex AI。
- 容器内 `/run/agentapi/secrets` 建议只读挂载。
- 前后端分离部署时，CORS 必须限制到明确域名。
- 开启 cookie auth 前必须使用 HTTPS。
- R2 S3 key 应和 Cloudflare API token 分离管理。

### 许可证

本项目使用 [MIT License](./LICENSE)。

---

## English

### Part 1: AgentAPI, the Primary Project

AgentAPI is the main product surface in this repository. It combines the `cmd/agentapi` Go backend and the `apps/web` React frontend to provide authenticated agent workspaces, chat sessions, long-running jobs, skills, attachments, generated artifacts, memory, admin operations, audit logs, risk controls, and production deployment assets.

#### Current Positioning

- **Backend entrypoint**: `cmd/agentapi`
- **Frontend entrypoint**: `apps/web`
- **Core backend runtime**: `internal/backend/agentruntime`
- **Production assembly layer**: `internal/backend/agentapi`
- **Default database**: Postgres
- **Cache and rate limiting**: Redis
- **Object storage**: S3-compatible storage, with Cloudflare R2 used by the production template
- **Default LLM provider**: Vertex AI, while the runtime keeps a multi-provider abstraction
- **Deployment model**: Docker Compose, with local and production examples in this repository

#### Core Capabilities

- User registration, login, refresh tokens, email verification, logout, and account deletion.
- Session lists, chat transcripts, SSE streaming, reconnect support, and event replay.
- Long-running jobs, cancellation, job status, and timeline events.
- Attachment upload, download, delete, preview, and message attachment sending.
- Generated artifact registration, listing, preview, download, and deletion.
- Skill registry, skill browsing, execution history, policy controls, and admin management.
- Memory extraction, organization, scoring, maintenance, deletion, export, and user controls.
- Admin console for users, skills, sessions, jobs, artifacts, health, cost, audit, and risk review.
- Production infrastructure integrations with Postgres, Redis, Kafka, S3/R2, Qdrant/Elastic, and Prometheus.

#### Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/agentapi` | AgentAPI backend binary entrypoint. |
| `apps/web` | React/Vite product frontend and `/admin` operations console. |
| `internal/backend/agentapi` | AgentAPI configuration, dependency wiring, runtime lifecycle, and workers. |
| `internal/backend/agentruntime` | Core web agent runtime for HTTP APIs, WebSockets, sessions, jobs, memory, skills, artifacts, admin, and risk APIs. |
| `internal/backend/services` | Product services such as analytics, api, compact, context, cost, history, mcp, oauth, tasks, tokens, tools, voice, and x402. |
| `internal/backend/googleauth` | Google service-account helper for Vertex AI access tokens. |
| `.claude/skills` | Local skills that AgentAPI can load. |
| `deploy/local` | Local Docker Compose development stack. |
| `deploy/production` | Production deployment examples, frontend nginx config, release, sizing, and backup/restore docs. |
| `docs/api/agentapi.openapi.yaml` | AgentAPI OpenAPI description. |

#### Technology Stack

- **Backend**: Go 1.24.4, chi, gorilla/websocket, pgx, goose, Prometheus client, Redis, Kafka, MinIO S3 client, JWT.
- **Frontend**: React 18, Vite, TypeScript, Tailwind CSS 4, Radix UI, lucide-react, Framer Motion, Vitest, Playwright.
- **Deployment**: Docker Compose, with a path toward separate API/worker services, managed Postgres/Redis/object storage, and Kubernetes-style production deployment.

#### Quick Start: Local Full Stack

Docker Compose is the recommended way to run the full AgentAPI stack locally:

```bash
mkdir -p secrets
# Put a Vertex-enabled service account JSON here:
# secrets/vertex-service-account.json

export GOOGLE_APPLICATION_CREDENTIALS="secrets/vertex-service-account.json"
export VERTEX_PROJECT_ID="REPLACE_WITH_GCP_PROJECT"
export VERTEX_LOCATION="us-central1"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"

docker compose -f deploy/local/docker-compose.yml up --build
```

Open:

- Web app: `http://localhost:8080`
- AgentAPI: `http://localhost:8081`
- Health: `http://localhost:8081/healthz`
- Readiness: `http://localhost:8081/readyz`

See [deploy/local/README.md](deploy/local/README.md) for more details.

#### Frontend Development

Start the backend first, then run:

```bash
cd apps/web
npm ci
npm run dev
```

The Vite dev server proxies `/v1`, `/healthz`, `/readyz`, and `/metrics` to `http://localhost:8081` by default. Override the backend target with:

```bash
AGENT_API_DEV_TARGET=http://localhost:8082 npm run dev
```

#### Backend Without Compose

```bash
go run ./cmd/agentapi \
  -addr :8081 \
  -store-backend sql \
  -sql-driver pgx \
  -sql-dialect postgres \
  -sql-dsn "$AGENT_API_SQL_DSN" \
  -enable-user-system \
  -auth-mode jwt \
  -jwt-secret "$AGENT_API_JWT_SECRET" \
  -llm-provider vertex \
  -model gemini-2.5-flash
```

#### Production Configuration

The production environment template is [deploy/production/.env.example](deploy/production/.env.example). Important variable groups:

| Group | Key Variables |
| --- | --- |
| Auth | `AGENT_API_JWT_SECRET`, `AGENT_API_ADMIN_TOKEN`, JWT/session settings |
| Email | `AGENT_API_EMAIL_PROVIDER=resend`, `AGENT_API_RESEND_API_KEY`, `AGENT_API_EMAIL_FROM` |
| Storage | `AGENT_API_ARTIFACT_S3_ENDPOINT`, `AGENT_API_ARTIFACT_S3_ACCESS_KEY`, `AGENT_API_ARTIFACT_S3_SECRET_KEY`, `AGENT_API_ARTIFACT_S3_BUCKET` |
| Vertex | `GOOGLE_APPLICATION_CREDENTIALS`, `VERTEX_PROJECT_ID`, `GOOGLE_CLOUD_PROJECT`, `VERTEX_LOCATION` |
| Runtime | `AGENT_API_SKILL_DIRS`, `AGENT_API_SKILL_POLICY`, `AGENT_API_OPERATION_RATE_LIMITS`, `AGENT_API_RETENTION_DAYS` |
| Frontend | `VITE_AGENT_API_BASE_URL` for split frontend/API origins |

Production should prefer a service account JSON over short-lived access-token environment variables:

```env
AGENT_API_SECRETS_DIR=/opt/agentapi/secrets
GOOGLE_APPLICATION_CREDENTIALS=/run/agentapi/secrets/vertex-service-account.json
VERTEX_PROJECT_ID=your-gcp-project
GOOGLE_CLOUD_PROJECT=your-gcp-project
VERTEX_LOCATION=us-central1
```

#### Deployment And Operations

Example Compose deployment on a test server:

```bash
cd /opt/agentapi/repo
docker compose --env-file /opt/agentapi/.env -f deploy/local/docker-compose.yml up -d --build
```

Useful checks:

```bash
curl -fsS http://localhost:8081/healthz
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8081/metrics

docker ps
docker logs --tail=100 claude-codex-agentapi
docker logs --tail=100 claude-codex-agentweb
```

More deployment docs:

- [deploy/production/README.md](deploy/production/README.md)
- [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md)
- [deploy/production/RELEASE_FLOW.md](deploy/production/RELEASE_FLOW.md)
- [deploy/production/SIZING.md](deploy/production/SIZING.md)
- [deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md)

### Part 2: claudecode Go Refactor, the Secondary Project

The claudecode Go refactor is the second major workstream in this repository. It ports Claude Code's TypeScript implementation to Go. It is not the primary product entrypoint today, but it provides reusable agent runtime, tools, permissions, skills, memory, MCP, provider, and orchestration capabilities for AgentAPI and local agent development.

#### Current Positioning

- **Local TUI entrypoint**: `cmd/tui`
- **Application assembly layer**: `internal/app`
- **Terminal UI**: `internal/ui/tui`
- **Core harness**: `internal/harness`
- **Remote/server boundaries**: `internal/backend/bridge`, `internal/backend/remote`, `internal/backend/upstreamproxy`
- **Public foundation packages**: `internal/public`

The default local execution chain is:

```text
cmd/tui
  -> internal/app/cli
  -> internal/harness/engine
  -> queryRuntime
  -> internal/harness/queryengine
  -> internal/harness/query
```

#### Core Module Map

| Module Area | Purpose |
| --- | --- |
| `internal/app/cli` | Cobra command tree, slash commands, mode selection, TUI registry, MCP stdio, bridge continuation, and engine construction. |
| `internal/app/config`, `settings` | Local config, environment overrides, settings schema, MDM/managed settings, and tool permission config. |
| `internal/app/bootstrap`, `entrypoints`, `migrations` | Startup lifecycle, session/worktree/tmux initialization, migrations, and shutdown flow. |
| `internal/app/auth`, `securestorage` | OAuth, trusted device support, keychain/plaintext secret store. |
| `internal/ui/tui` | Bubble Tea TUI with model/update/view, overlays, permission broker, markdown rendering, and command adaptation. |
| `internal/harness/engine` | Local execution facade connecting planner, tool registry, permission checker, session, and query runtime. |
| `internal/harness/queryengine`, `query` | TS-aligned query runtime for the turn loop, model calls, tool execution, budgets, auto compact, and SDK-shaped events. |
| `internal/harness/tools`, `tool` | Tool registry, simple tools, rich tools, ToolUseContext, StreamingExecutor, and tool families such as bash/file/search/web/mcp/lsp/tasks/team/worktree. |
| `internal/harness/permissions` | default/plan/bypass/auto permission modes, rule parsing, approval requests, and TUI/bridge decision channels. |
| `internal/harness/agent`, `coordinator`, `swarm` | Sub-agents, teams, workers, tmux/iTerm2/in-process teammate backends, and orchestration. |
| `internal/harness/plugins`, `skills` | Plugin manifests, skills, agents, hooks, MCP config loading, and runtime consumption. |
| `internal/harness/state`, `storage` | Session state, messages, usage, JSONL transcripts, and snapshots. |
| `internal/harness/memory`, `memdir`, `compact`, `budget` | Memory, context compaction, token budgets, and compatibility layers. |
| `internal/harness/provider`, `api`, `anthropic` | Provider abstraction for Anthropic, OpenAI, Qwen, Gemini, Bedrock, Vertex, and custom endpoints. |
| `internal/harness/mcp` | MCP server/client/resource support over stdio, HTTP, and SSE. |
| `internal/public` | Shared types, schemas, errors, filesystem helpers, and rate-limit foundations. |

#### Capability Modules Worth Tracking Separately

These are not just small tool folders; they affect runtime safety, background execution, platform capabilities, or observability:

- `internal/harness/analysis`: command/tool analysis, read-only detection, and safety guidance foundations.
- `internal/harness/sandboxadapter`: sandbox policy and tool execution boundary adapters.
- `internal/harness/websandbox`: web/browser sandbox capabilities.
- `internal/harness/computeruse`: computer/browser control capabilities.
- `internal/harness/hooks`: lifecycle hooks and plugin extension points.
- `internal/harness/background`: background jobs and long-running task foundations.
- `internal/harness/tasks`: task state, output, stop behavior, snapshots, and tool surface.
- `internal/harness/telemetry`: events, tracing, and runtime observability.
- `internal/harness/powershell`: Windows PowerShell execution support.

Peripheral capability modules are usually considered together:

- `internal/harness/github`
- `internal/harness/deeplink`
- `internal/harness/teleport`
- `internal/harness/dxt`
- `internal/harness/nativeinstaller`
- `internal/harness/vim`
- `internal/harness/buddy`
- `internal/harness/ultraplan`

#### Running The Local TUI

```bash
make run-tui
```

Or run it directly:

```bash
go run ./cmd/tui
```

Provider configuration can be supplied through environment variables or `~/.claude-codex/config.json`. The config home can be overridden with `CLAUDE_GO_HOME`. Supported provider families include Anthropic, OpenAI, Qwen, Gemini, Bedrock, Vertex, and custom OpenAI-compatible endpoints.

#### Refactor Status

High-level status:

- The core execution chain is closed; `engine` is now a thin adapter and the default runtime goes through `queryengine -> query`.
- Tools, permissions, skills, tasks, memory, compact, provider, MCP, and storage have usable core paths.
- The CLI command matrix exists, but command coverage and TS-level details are still being filled in.
- The TUI has a working skeleton, but it remains the largest gap compared with the TypeScript implementation.
- bridge/remote/server, plugin management UI, GitHub/git, computer/browser, and other peripheral capabilities still need migration and UX work.

Detailed refactor docs:

- [docs/REFACTORING_MODULES.md](docs/REFACTORING_MODULES.md)
- [docs/MIGRATION_STATUS.md](docs/MIGRATION_STATUS.md)
- [docs/REFACTORING_SUMMARY.md](docs/REFACTORING_SUMMARY.md)
- [docs/REFACTOR_PROGRESS.md](docs/REFACTOR_PROGRESS.md)
- [docs/refactor/CORE_REFACTOR_STATUS.md](docs/refactor/CORE_REFACTOR_STATUS.md)
- [docs/Architecture-diagram/claudecode Go 重构子项目完整架构图.drawio_副本.pdf](docs/Architecture-diagram/claudecode%20Go%20重构子项目完整架构图.drawio_副本.pdf)
- [docs/Architecture-diagram/claudecode Go 重构：internal 全模块地图.drawio_副本.pdf](docs/Architecture-diagram/claudecode%20Go%20重构：internal%20全模块地图.drawio_副本.pdf)

### Development Commands

```bash
make fmt
make test
go test ./internal/backend/agentruntime ./internal/backend/googleauth ./internal/harness/provider ./cmd/agentapi
go build ./cmd/agentapi
go build ./cmd/tui

cd apps/web
npm ci
npm run build
npm run test
npm run e2e
```

### Security Notes

- Never commit `.env` files, R2 keys, Resend keys, admin tokens, JWT secrets, or service account JSON files.
- `secrets/` is intended for local secret material only and should remain gitignored.
- Prefer service account credentials for Vertex AI in production.
- Mount `/run/agentapi/secrets` read-only inside containers.
- Restrict CORS to exact frontend origins when using split-origin hosting.
- Use HTTPS before enabling cookie auth.
- Keep Cloudflare R2 S3 keys separate from Cloudflare API tokens.

### License

This project is licensed under the [MIT License](./LICENSE).

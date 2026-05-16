# AgentAPI

AgentAPI 是一个面向 C 端用户的 Agent 工作区产品，由 Go 后端和
React Web 前端组成。它支持用户认证、会话对话、长任务、Skill、附件、
生成产物、Memory、Admin 运维、Audit Log、风控，以及基于 Docker
Compose 的生产部署。

仓库中仍保留本地 TUI/CLI harness，但当前主要产品面是
`cmd/agentapi` + `apps/web`。

## 目录

- [当前状态](#当前状态)
- [目录结构](#目录结构)
- [核心能力](#核心能力)
- [快速启动](#快速启动)
- [配置说明](#配置说明)
- [部署](#部署)
- [运维](#运维)
- [开发命令](#开发命令)
- [安全注意事项](#安全注意事项)
- [相关文档](#相关文档)

## 当前状态

当前生产路径：

- 后端：`cmd/agentapi`
- 前端：`apps/web`
- 数据库：Postgres
- 缓存和限流：Redis
- 对象存储：Cloudflare R2，使用 S3-compatible API
- LLM provider：Vertex AI，使用 service account 凭据
- 邮件服务：Resend，用于注册邮箱验证
- 部署：Docker Compose，并支持 GitHub Actions push-to-main 自动部署

已经实现的产品模块：

- JWT 用户系统、refresh token、可选邮箱验证。
- 会话对话、流式响应、断线恢复/回放、带附件消息。
- Job 执行和 Job Timeline 事件回放。
- 附件和生成产物通过 R2 存储。
- Skill Registry、产品化 Skill 列表、执行历史、Review Checks、Admin 管理 API/UI。
- Memory 捕获、治理、用户控制、删除/导出、维护、质量反馈。
- Admin Console：Skill、用户、Session/Job/Artifact、Health/Cost、Audit、Risk、Agent Evaluation。
- 风控和滥用防护基础设施：限流、风险事件、Admin Review、审计日志。

暂缓内容：

- Product Analytics 的完整产品分析面板。
- 更高级的向量/多模态 Memory 检索增强。
- 一部分偏 To B 的深度治理流程。

## 目录结构

| 路径 | 作用 |
| --- | --- |
| `cmd/agentapi` | 产品后端入口。 |
| `apps/web` | React/Vite C 端前端和 Admin 控制台。 |
| `internal/backend/agentruntime` | Web Agent Runtime：认证、会话、Memory、Job、Skill、Artifact、Admin/Risk API。 |
| `internal/backend/googleauth` | Vertex AI service account OAuth token exchange。 |
| `internal/harness` | 本地 agent harness、provider、tool、state、skill 和 CLI 运行时。 |
| `.claude/skills` | AgentAPI 暴露的本地 skills，包括 docx 和 Vertex image artifact。 |
| `deploy/local` | 本地 Docker Compose 栈。 |
| `deploy/production` | 生产部署示例、前端 nginx 配置、备份恢复文档。 |
| `.github/workflows/deploy-main.yml` | main 分支自动部署 workflow。 |

## 核心能力

### Web 产品

- 登录、注册、邮箱验证、登出、删除账号。
- 会话列表、聊天记录、流式响应、全局搜索、桌面/移动端响应式布局。
- 附件上传进度、预览、下载、删除，以及随消息发送附件。
- Artifact 列表、预览、下载、删除，以及生成产物入口。
- Skill 分类、搜索、详情弹窗、插入到对话/Job 流程。
- 设置弹窗、Memory 开关、Memory 管理、导出/删除数据、账号操作。

网站 logo 位于 `apps/web/public/logo.png`，用于浏览器图标和应用内品牌标识。

### Agent Runtime

- SQL 持久化用户、会话、消息、Memory、Skill、Job、Artifact、Audit、Risk。
- SSE Chat Stream 和 Job Stream replay。
- Governed LLM：重试、请求/Token 配额、用量/成本统计、fallback hooks。
- Skill 执行：policy merge、sandbox 设置、allowed env/tool、产物注册、执行遥测。
- Memory：抽取、抽象、评分/维护、敏感信息脱敏、用户 opt-in/out。

### Admin 与运维

- `/admin` 控制台，使用 Admin Token 保护。
- Skill Registry 和 Policy 管理。
- 用户状态管理。
- Session / Job / Artifact 排障台。
- Runtime Health、Readiness、Cost 面板。
- Audit Log 和 Risk Review Console。
- Agent Evaluation 页面：基于真实运行数据创建 eval run，按用户、时间窗口、session、job、skill、provider/model 过滤，查看通过率、失败 findings、review 队列和阈值状态。
- Agent Evaluation 日增量任务：默认每天 UTC+8 05:00 汇总前一个 UTC+8 自然日的数据，写入 `daily_incremental` eval run，重复触发同一天同一用户会跳过，避免重复累计。

## 快速启动

### 前置要求

- Go `1.24.4`，或与 `go.mod` 兼容。
- Node.js `22`，用于 `apps/web`。
- Docker 和 Docker Compose。
- Postgres、Redis、Cloudflare R2 凭据。
- Vertex AI service account JSON。

### 本地 Web 栈

推荐使用 Docker Compose：

```bash
mkdir -p secrets
# 将有 Vertex 权限的 service account JSON 放到：
# secrets/vertex-service-account.json

export GOOGLE_APPLICATION_CREDENTIALS="secrets/vertex-service-account.json"
export VERTEX_PROJECT_ID="REPLACE_WITH_GCP_PROJECT"
export VERTEX_LOCATION="us-central1"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="REPLACE_WITH_R2_ACCESS_KEY_ID"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="REPLACE_WITH_R2_SECRET_ACCESS_KEY"

docker compose -f deploy/local/docker-compose.yml up --build
```

访问：

- Web App：`http://localhost:8080`
- AgentAPI：`http://localhost:8081`
- Health：`http://localhost:8081/healthz`
- Readiness：`http://localhost:8081/readyz`

更多说明见 [deploy/local/README.md](deploy/local/README.md)。

### 前端开发服务器

先启动后端，然后运行：

```bash
cd apps/web
npm ci
npm run dev
```

Vite 默认将 `/v1`、`/healthz`、`/readyz`、`/metrics` 代理到
`http://localhost:8081`。

### 不使用 Compose 启动后端

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

## 配置说明

生产环境模板见 [deploy/production/.env.example](deploy/production/.env.example)。
不要提交真实 `.env`、service account JSON 或任何密钥。

关键配置组：

| 配置组 | 关键变量 |
| --- | --- |
| Auth | `AGENT_API_JWT_SECRET`、`AGENT_API_ADMIN_TOKEN`、JWT/session 设置 |
| Email | `AGENT_API_EMAIL_PROVIDER=resend`、`AGENT_API_RESEND_API_KEY`、`AGENT_API_EMAIL_FROM`、`AGENT_API_EMAIL_PUBLIC_BASE_URL` |
| Storage | `AGENT_API_ARTIFACT_S3_ENDPOINT`、`AGENT_API_ARTIFACT_S3_ACCESS_KEY`、`AGENT_API_ARTIFACT_S3_SECRET_KEY`、`AGENT_API_ARTIFACT_S3_BUCKET`、`AGENT_API_ARTIFACT_S3_PREFIX` |
| Vertex | `GOOGLE_APPLICATION_CREDENTIALS`、`VERTEX_PROJECT_ID`、`GOOGLE_CLOUD_PROJECT`、`VERTEX_LOCATION` |
| Runtime | `AGENT_API_SKILL_DIRS`、`AGENT_API_SKILL_POLICY`、`AGENT_API_OPERATION_RATE_LIMITS`、`AGENT_API_RETENTION_DAYS` |
| Frontend | `VITE_AGENT_API_BASE_URL`，用于前端/API 分离部署 |

### Vertex AI 凭据

生产环境应使用 service account JSON，不要依赖短期 access token。

Docker Compose 中推荐挂载宿主机 secrets 目录，并让容器读取挂载后的路径：

```env
AGENT_API_SECRETS_DIR=/opt/agentapi/secrets
GOOGLE_APPLICATION_CREDENTIALS=/run/agentapi/secrets/vertex-service-account.json
VERTEX_PROJECT_ID=your-gcp-project
GOOGLE_CLOUD_PROJECT=your-gcp-project
VERTEX_LOCATION=us-central1
```

该 service account 至少需要能够调用 Vertex AI，例如目标项目上的
`roles/aiplatform.user`。

## 部署

当前部署模型是在服务器上运行 Docker Compose：

```bash
cd /opt/agentapi/repo
docker compose --env-file /opt/agentapi/.env -f deploy/local/docker-compose.yml up -d --build
```

`.github/workflows/deploy-main.yml` 支持 main 分支 push 后自动部署。
Workflow 会 SSH 到服务器，更新仓库，然后重建/重启 Compose 栈。

前端生产部署有两种方式：

- 同源部署：服务 `apps/web/dist`，并将 `/v1`、`/healthz`、`/readyz`、
  `/metrics` 反代到 AgentAPI。
- 前后端分离：构建时设置
  `VITE_AGENT_API_BASE_URL=https://api.example.com`，并在 AgentAPI 配置 CORS。

见 [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md)。

## 运维

常用检查：

```bash
curl -fsS http://localhost:8081/healthz
curl -fsS http://localhost:8081/readyz
curl -fsS http://localhost:8081/metrics
```

Agent Evaluation 常用导出：

```bash
curl -fsS -H "X-User-ID: admin" -H "X-Admin-Token: $AGENT_API_ADMIN_TOKEN" \
  "http://localhost:8081/v1/admin/ops/eval/summary?format=markdown"

curl -fsS -H "X-User-ID: admin" -H "X-Admin-Token: $AGENT_API_ADMIN_TOKEN" \
  "http://localhost:8081/v1/admin/ops/eval/results?run_id=EVAL_RUN_ID&status=failed&format=csv"
```

Agent Evaluation 日增量任务配置：

```bash
AGENT_API_EVAL_DAILY_ENABLED=true
AGENT_API_EVAL_DAILY_HOUR=5
AGENT_API_EVAL_DAILY_MINUTE=0
AGENT_API_EVAL_DAILY_BATCH_LIMIT=200
AGENT_API_EVAL_DAILY_TIMEOUT=10m
# 可选：没有内置用户系统时显式指定评估用户
AGENT_API_EVAL_DAILY_USER_IDS="user_a,user_b"
```

服务器常用命令：

```bash
docker ps
docker logs --tail=100 claude-codex-agentapi
docker logs --tail=100 claude-codex-agentweb
```

备份恢复见 [deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md)。

## 开发命令

```bash
make fmt
make test
go test ./internal/backend/agentruntime ./internal/backend/googleauth ./internal/harness/provider ./cmd/agentapi
go build ./cmd/agentapi

cd apps/web
npm run build
npm run test
npm run e2e
```

仓库仍保留本地 TUI：

```bash
make run-tui
```

## 安全注意事项

- 不要提交 `.env`、R2 key、Resend key、Admin Token、service account JSON。
- `secrets/` 已加入 gitignore，只用于本地 secret。
- Vertex AI 优先使用 service account 凭据。
- 容器中的 `/run/agentapi/secrets` 应保持只读挂载。
- 前后端分离部署时，CORS 只允许精确的前端 origin。
- Cookie Auth 上生产前必须启用 HTTPS。
- Cloudflare R2 S3 key 与 Cloudflare API token 分开管理。

## 相关文档

- [README.md](README.md)
- [apps/web/README.md](apps/web/README.md)
- [deploy/local/README.md](deploy/local/README.md)
- [deploy/production/.env.example](deploy/production/.env.example)
- [deploy/production/FRONTEND.md](deploy/production/FRONTEND.md)
- [deploy/production/BACKUP_RESTORE.md](deploy/production/BACKUP_RESTORE.md)
- [internal/backend/agentruntime/README.md](internal/backend/agentruntime/README.md)
- [internal/backend/agentruntime/PRODUCTION_PROGRESS.md](internal/backend/agentruntime/PRODUCTION_PROGRESS.md)

## 许可证

本项目使用 [MIT License](./LICENSE)。

# claude-codex

> `claude-codex` 是 `claude-code`（TypeScript）核心能力的 Go 重构版本。  
> 当前阶段：**可运行、可测试，但尚未与 TS 版本完全功能对等**。

---

## 目录

- [项目概览](#项目概览)
- [可用性基线](#可用性基线)
- [Quickstart](#quickstart)
- [配置文件路径与含义](#配置文件路径与含义)
- [模块重构情况](#模块重构情况)
- [TUI vs AgentAPI](#tui-vs-agentapi)
- [注意事项](#注意事项)
- [常用命令](#常用命令)
- [许可证](#许可证)

---

## 项目概览

| 项 | 说明 |
|---|---|
| 代码仓 | `claude-codex` |
| TS 对照源码 | `claude-code/src` |
| Go 版本 | `1.24.4` |
| 主要依赖 | Cobra、Bubble Tea、Gorilla WebSocket |
| 入口 | `cmd/tui`、`cmd/agentapi` |

### 重构定位

- Go 侧采用分层架构：`app / backend / harness / public / ui`。
- TS 侧模块规模显著更大（尤其 `commands`、`components`、`tools`、`utils`）。
- Go 侧已具备核心能力，但仍有未完成区域。

### 当前不完整信号（实测统计）

- `internal` 下 TODO/FIXME 约 **117** 处。
- 其中 `internal/harness/query` + `internal/harness/queryengine` 约 **65** 处。

> 结论：当前适合开发验证、灰度使用与持续重构，不建议视为“完全替代 TS”的最终版本。

---

## 可用性基线

在当前仓库实测通过：

- ✅ `go test ./...`
- ✅ `go build ./cmd/tui && go build ./cmd/agentapi`
- ✅ `go run ./cmd/tui --help`
- ✅ `go run ./cmd/tui /help`
- ✅ `go run ./cmd/agentapi -h`

> 注意：**测试通过 ≠ 功能完全对等**。请结合“模块重构情况”和“注意事项”评估使用边界。

---

## Quickstart

### 1) 环境检查

```bash
go version
```

建议与 `go.mod` 保持一致。

### 2) 启动 TUI/CLI

```bash
cd claude-codex
go run ./cmd/tui --help
```

常用命令：

```bash
go run ./cmd/tui /help
go run ./cmd/tui /model
go run ./cmd/tui /limits
```

或：

```bash
make run-tui
```

### 3) 启动 AgentAPI

```bash
cd claude-codex
export ANTHROPIC_API_KEY="your-api-key"
go run ./cmd/agentapi -addr :8080 -llm-provider anthropic -model claude-sonnet-4-6 -auth-token dev-token
```

浏览器访问：`http://localhost:8080`

或：

```bash
make run-agentapi
```

### 4) 回归验证

```bash
cd claude-codex
go test ./...
```

---

## 配置文件路径与含义

`claude-codex` 当前支持“全局配置 + 工作区配置”两层：

- 全局配置：`~/.claude-codex/config.json`
- 自定义全局目录：可通过环境变量 `CLAUDE_GO_HOME` 修改，实际路径为 `${CLAUDE_GO_HOME}/config.json`
- 工作区配置：`<你的项目目录>/.claude-codex/config.json`

当工作区配置存在时，会覆盖同名全局配置项（按当前实现，主要覆盖模型、权限模式、主题、超时、回合数、Telemetry、OAuth、MCP 等字段）。

### 配置字段说明（`config.json`）

| 字段 | 含义 |
|---|---|
| `schema_version` | 配置版本号，程序会按当前版本自动归一化/迁移 |
| `backend` | 后端类型（支持 `anthropic` / `openai` 协议） |
| `provider` | LLM 提供商（如 `anthropic` / `openai`） |
| `model` | 默认模型名 |
| `permission_mode` | 权限模式：`default` / `plan` / `bypass` / `auto` |
| `theme` | 主题：`dark` 或 `light` |
| `api_base_url` | API 基础地址 |
| `api_key` / `api_token` | API 凭据（建议使用环境变量注入，不要提交到仓库） |
| `timeout_seconds` | 请求超时秒数 |
| `max_turns` | 单次会话最大轮数（最小为 1） |
| `secret_store` | 密钥存储策略：`auto` / `plaintext` / `keychain` |
| `plugin_dir` | 插件目录路径 |
| `bridge_secret` | bridge 鉴权密钥 |
| `telemetry.enabled` | 是否开启遥测 |
| `telemetry.exporter` | 遥测导出器，可逗号分隔） |
| `telemetry.endpoint` | 遥测上报地址 |
| `telemetry.insecure` | 遥测是否使用不安全连接 |
| `telemetry.service_name` | 遥测服务名（默认 `claude-codex`） |
| `oauth.client_id` / `oauth.client_secret` | OAuth 客户端凭据 |
| `oauth.auth_url` / `oauth.token_url` | OAuth 授权与换 token 地址 |
| `oauth.scopes` | OAuth scope 列表 |
| `oauth.redirect_host` / `oauth.redirect_port` | OAuth 回调监听地址 |
| `mcp_servers` | MCP 服务列表|

---

## 模块重构情况

### 已具备较好可用基础

- `internal/harness/*`：agent、engine、tools、state、skills 等核心框架能力
- `internal/backend/services/*`：analytics、api、tokens、tools、oauth 等服务
- `internal/app/cli/*`：CLI 主体与一批 slash 命令
- `internal/backend/agentruntime`：Web 侧服务入口

### 仍在持续重构

- `Query / QueryEngine`：TODO 密集，是主要未完成来源
- 部分工具与边缘能力：目录已在，但覆盖度和行为一致性仍在补齐
- 与 TS 大体量 `utils` 的能力映射未完全收敛

---

## TUI vs AgentAPI

| 维度 | TUI (`cmd/tui`) | AgentAPI (`cmd/agentapi`) |
|---|---|---|
| 交互形态 | 终端 CLI / TUI | 浏览器 + HTTP/WebSocket |
| 核心技术 | Cobra + Bubble Tea | Web server + `/ws` |
| 典型场景 | 本地开发、脚本化流程 | 可视化对话、演示与联调 |
| 会话特征 | CLI 习惯流 | 当前以内存会话为主 |
| 权限策略 | 跟随 CLI 运行配置 | 默认只启用读/搜索/Web/Skill，写入和执行需显式开启 |
| 风险点 | 命令覆盖仍在补齐 | 生产前需接入正式用户鉴权和持久化后端 |

---

## 注意事项

1. 这是重构中项目，不是最终完成态。  
2. 生产使用前，请先做 AgentAPI 权限与网络暴露加固。  
3. API Key 请仅通过环境变量注入，避免硬编码或提交。  
4. 每次改动建议最少执行：`go test ./...`。  
5. 根目录中可能存在 `tui`/`agentapi` 可执行文件，和 `cmd/tui`/`cmd/agentapi` 源码入口不是同一概念。  

---

## 常用命令

```bash
make fmt         # 格式化
make test        # 运行测试
make run-tui     # 启动 TUI
make run-agentapi   # 启动 AgentAPI
make clean       # 清理二进制
```

---

## 许可证

本项目采用 [MIT License](./LICENSE) 开源协议。

---

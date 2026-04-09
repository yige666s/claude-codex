# TypeScript 到 Go 迁移状态

## 概览

本文档记录了从 TypeScript (`/src`) 到 Go (`/claude-go`) 的重构迁移进度。

---

## ✅ 已完成的模块

### 核心系统
- **agent** - Agent 系统（定义、执行、Fork、进度追踪、工具集成、流式输出）
- **anthropic** - Anthropic API 客户端（消息、流式、Token 计数）
- **config** - 配置管理系统
- **core** - 核心类型和接口
- **engine** - 执行引擎
- **types** - 类型定义

### 工具系统
- **tools** - 工具注册表和基础工具
  - ✅ agent - Agent 工具
  - ✅ bash - Bash 命令执行
  - ✅ file - 文件读写编辑
  - ✅ lsp - LSP 集成
  - ✅ mcp - MCP 工具
  - ✅ notebook - Notebook 编辑
  - ✅ search - Glob/Grep 搜索
  - ✅ team - Team 管理
  - ✅ web - Web 搜索和抓取
  - ✅ worktree - Git worktree 管理

### 基础设施
- **bootstrap** - 启动初始化
- **bridge** - 桥接系统
- **cli** - 命令行接口和 Slash 命令
- **context** - 上下文管理
- **coordinator** - 协调器
- **entrypoints** - 入口点管理
- **fsutil** - 文件系统工具
- **hooks** - 钩子系统
- **lsp** - LSP 客户端
- **mcp** - MCP 协议支持
- **memdir** - 内存目录
- **memory** - 记忆系统（存储、提取、管理）
- **migrations** - 配置迁移
- **permissions** - 权限管理
- **plugins** - 插件系统
- **provider** - Provider 抽象
- **query** - 查询管道
- **ratelimit** - 限流追踪
- **remote** - Remote 客户端（WebSocket、消息适配）
- **schemas** - Schema 验证
- **secret** - 密钥管理
- **server** - 服务器（HTTP、WebSocket、SSE）
- **skills** - Skills 系统
- **state** - 状态管理
- **tasks** - 任务系统
- **upstreamproxy** - 上游代理配置

### 服务层
- **services/analytics** - 分析服务
- **services/api** - API 服务层
- **services/compact** - Compact 服务
- **services/context** - 上下文服务
- **services/cost** - 成本追踪服务
- **services/history** - 历史记录服务
- **services/oauth** - OAuth 服务
- **services/tasks** - 任务服务
- **services/tokens** - Token 服务（包含 tokenEstimation）
- **services/tools** - 工具服务

---

## ⏸️ 暂不迁移的模块

### UI 相关（前端专用）
- **components** (146 个文件) - React 组件，终端 UI 界面
- **hooks** (87 个文件) - React Hooks
- **ink** (50 个文件) - 终端渲染引擎（自定义 Ink 实现）
- **keybindings** - 键盘绑定（UI 交互）
- **screens** - 屏幕组件
- **vim** - Vim 模式（终端编辑器）
- **native-ts** - 原生模块（color-diff、yoga-layout、file-index）

### 其他
- **buddy** - Buddy 系统（优先级低）
- **moreright** - Moreright 功能（优先级低）
- **outputStyles** - 输出样式（UI 相关）
- **shims** - Shims 层（兼容性）
- **voice** - 语音功能（优先级低）

---

## 🚧 待迁移的模块

### 高优先级

#### 1. Commands 系统 (102 个命令文件)
**状态**: 部分完成（基础 Slash 命令已实现）

已实现的 Slash 命令：
- `/limits`, `/quota`, `/usage` - 限流状态
- `/mem2` - 记忆系统（新）
- `/memory` - 记忆系统（旧，兼容）

待实现的重要命令：
- `/help` - 帮助系统
- `/model` - 模型切换
- `/config` - 配置管理
- `/context` - 上下文管理
- `/files` - 文件管理
- `/clear` - 清除操作
- `/export` - 导出会话
- `/resume` - 恢复会话
- `/session` - 会话管理
- `/status` - 状态查看
- `/tasks` - 任务管理
- `/skills` - Skills 管理
- `/agents` - Agent 管理
- `/mcp` - MCP 管理
- `/plugin` - 插件管理

其他命令（优先级较低）：
- `/advisor`, `/brief`, `/btw`, `/chrome`, `/commit`, `/compact`, `/cost`, `/desktop`, `/diff`, `/doctor`, `/effort`, `/feedback`, `/heapdump`, `/ide`, `/insights`, `/keybindings`, `/login`, `/logout`, `/mobile`, `/passes`, `/permissions`, `/plan`, `/privacy-settings`, `/rate-limit-options`, `/release-notes`, `/remote-env`, `/rename`, `/review`, `/rewind`, `/sandbox-toggle`, `/stickers`, `/tag`, `/theme`, `/upgrade`, `/usage`, `/version`, `/vim`, `/voice` 等

#### 2. Services 层
**状态**: 核心服务已完成

已完成（位于 `internal/backend/services/`）：
- ✅ analytics - 分析服务
- ✅ api - API 服务层
- ✅ compact - Compact 服务
- ✅ context - 上下文服务
- ✅ cost - 成本追踪服务
- ✅ history - 历史记录服务
- ✅ oauth - OAuth 服务
- ✅ tasks - 任务服务
- ✅ tokens - Token 服务（包含 tokenEstimation）
- ✅ tools - 工具服务

已完成（位于 `internal/harness/`）：
- ✅ lsp - LSP 服务（在 harness/tools/lsp）
- ✅ mcp - MCP 协议支持（在 harness/mcp）
- ✅ plugins - 插件系统（在 harness/plugins）
- ✅ skills - Skills 系统（在 harness/skills）

暂不迁移（非核心功能，优先级低）：
- **autoDream** - 自动记忆整合（后台自动化功能）
- **remoteManagedSettings** - 远程托管设置（企业功能）
- **settingsSync** - 设置同步（多设备同步）
- **teamMemorySync** - 团队记忆同步（团队协作功能）
- **tips** - 提示服务（用户引导功能）
- **x402** - HTTP 402 加密支付（实验性功能）

其他服务文件：
- `awaySummary.ts` - 离开摘要
- `claudeAiLimits.ts` - Claude AI 限制（已部分实现在 ratelimit）
- `diagnosticTracking.ts` - 诊断追踪
- `internalLogging.ts` - 内部日志
- `mcpServerApproval.tsx` - MCP 服务器审批
- `mockRateLimits.ts` - 模拟限流
- `notifier.ts` - 通知器
- `preventSleep.ts` - 防止休眠
- `rateLimitMessages.ts` - 限流消息
- `rateLimitMocking.ts` - 限流模拟
- `vcr.ts` - VCR 录制
- `voice.ts` - 语音服务
- `voiceKeyterms.ts` - 语音关键词
- `voiceStreamSTT.ts` - 语音流 STT

#### 3. Utils 工具库 (331 个文件)
**状态**: 未开始

这是一个巨大的工具库，包含大量实用函数。需要评估哪些是后端必需的，哪些是前端专用的。

部分重要工具：
- `api.ts` - API 工具
- `auth.ts` - 认证工具
- `attachments.ts` - 附件处理
- `analyzeContext.ts` - 上下文分析
- `Shell.ts`, `ShellCommand.ts` - Shell 工具
- `Cursor.ts` - 光标管理
- `bash/*` - Bash 相关工具
- 等等...

建议：分阶段迁移，优先迁移核心功能依赖的工具。

---

## 📊 统计数据

### TypeScript 源码结构
```
src/
├── assistant/          会话历史
├── bootstrap/          启动逻辑
├── bridge/            桥接系统 (34 文件)
├── buddy/             Buddy 系统
├── cli/               CLI 相关 (10 文件)
├── commands/          命令系统 (102 文件)
├── components/        UI 组件 (146 文件)
├── constants/         常量定义
├── context/           上下文管理 (11 文件)
├── coordinator/       协调器
├── entrypoints/       入口点 (8 文件)
├── hooks/             React Hooks (87 文件)
├── ink/               终端渲染 (50 文件)
├── keybindings/       键盘绑定
├── memdir/            内存目录 (10 文件)
├── migrations/        迁移 (13 文件)
├── moreright/         Moreright
├── native-ts/         原生模块 (5 文件)
├── outputStyles/      输出样式
├── plugins/           插件 (4 文件)
├── query/             查询 (7 文件)
├── remote/            远程 (6 文件)
├── schemas/           Schema (3 文件)
├── screens/           屏幕 (5 文件)
├── server/            服务器 (6 文件)
├── services/          服务层 (39 模块)
├── shims/             Shims (5 文件)
├── skills/            Skills (6 文件)
├── state/             状态 (11 文件)
├── tasks/             任务 (11 文件)
├── tools/             工具 (45 工具)
├── types/             类型 (14 文件)
├── upstreamproxy/     上游代理 (4 文件)
├── utils/             工具库 (331 文件)
├── vim/               Vim 模式 (7 文件)
└── voice/             语音
```

### Go 实现结构（2026-04-07 更新）
```
internal/
├── app/                   ✅ 应用特定逻辑
│   ├── bootstrap/         ✅ 启动初始化
│   ├── cli/               ✅ 命令行接口（部分 Slash 命令）
│   ├── config/            ✅ 应用配置管理
│   ├── entrypoints/       ✅ 入口点管理
│   ├── lsp/               ✅ LSP 管理器
│   └── migrations/        ✅ 配置迁移
├── backend/               ✅ 后端服务
│   ├── bridge/            ✅ 桥接系统
│   ├── remote/            ✅ Remote 客户端
│   ├── server/            ✅ HTTP/WebSocket/SSE 服务器
│   ├── services/          ✅ 服务层（10个核心服务）
│   │   ├── analytics/     ✅ 分析服务
│   │   ├── api/           ✅ API 服务层
│   │   ├── compact/       ✅ Compact 服务
│   │   ├── context/       ✅ 上下文服务
│   │   ├── cost/          ✅ 成本追踪
│   │   ├── history/       ✅ 历史记录
│   │   ├── oauth/         ✅ OAuth 服务
│   │   ├── tasks/         ✅ 任务服务
│   │   ├── tokens/        ✅ Token 服务
│   │   └── tools/         ✅ 工具服务
│   └── upstreamproxy/     ✅ 上游代理
├── harness/               ✅ 可复用 Agent 框架
│   ├── agent/             ✅ Agent 系统
│   ├── anthropic/         ✅ Anthropic API 客户端
│   ├── context/           ✅ 上下文管理
│   ├── coordinator/       ✅ 协调器
│   ├── engine/            ✅ 执行引擎
│   ├── hooks/             ✅ 钩子系统
│   ├── mcp/               ✅ MCP 协议支持
│   ├── memdir/            ✅ 自动记忆系统
│   ├── memory/            ✅ 记忆提取和管理
│   ├── permissions/       ✅ 权限管理
│   ├── plugins/           ✅ 插件系统
│   ├── provider/          ✅ Provider 抽象
│   ├── query/             ✅ 查询管道
│   ├── queryengine/       ✅ 查询引擎
│   ├── skills/            ✅ Skills 系统
│   ├── state/             ✅ 状态管理
│   ├── tasks/             ✅ 任务系统
│   ├── tool/              ✅ 工具基础
│   └── tools/             ✅ 工具集（10+ 工具类型）
│       ├── agent/         ✅ Agent 工具
│       ├── bash/          ✅ Bash 执行
│       ├── file/          ✅ 文件操作
│       ├── lsp/           ✅ LSP 集成
│       ├── mcp/           ✅ MCP 工具
│       ├── notebook/      ✅ Notebook 编辑
│       ├── search/        ✅ Glob/Grep 搜索
│       ├── team/          ✅ Team 管理
│       ├── web/           ✅ Web 搜索和抓取
│       └── worktree/      ✅ Git worktree 管理
├── public/                ✅ 可复用公共组件
│   ├── apperrors/         ✅ 应用错误处理
│   ├── fsutil/            ✅ 文件系统工具
│   ├── ratelimit/         ✅ 速率限制
│   ├── schemas/           ✅ Schema 验证
│   └── types/             ✅ 通用类型定义
└── ui/                    ✅ 用户界面
    └── tui/               ✅ 终端用户界面
```

---

## 🎯 下一步计划

### 短期目标（1-2 周）
1. **完善 Commands 系统**
   - 实现核心 Slash 命令（/help, /model, /config, /context, /files）
   - 实现会话管理命令（/session, /resume, /export）
   - 实现状态查看命令（/status, /tasks）

2. **Utils 工具库评估**
   - 识别后端必需的工具函数
   - 创建迁移优先级列表
   - 开始迁移核心工具

### 中期目标（1-2 月）
1. 完成所有核心 Commands
2. 迁移关键 Utils 工具
3. 编写集成测试
4. 性能优化

### 长期目标（3-6 月）
1. 完整功能对等
2. 生产环境部署
3. 文档完善
4. 社区反馈迭代

### 暂不迁移的功能（优先级低）
以下服务为非核心功能，暂不迁移：
- **autoDream** - 自动记忆整合（后台自动化）
- **remoteManagedSettings** - 远程托管设置（企业功能）
- **settingsSync** - 设置同步（多设备同步）
- **teamMemorySync** - 团队记忆同步（团队协作）
- **tips** - 提示服务（用户引导）
- **x402** - HTTP 402 加密支付（实验性功能）

---

## 📝 注意事项

### 不需要迁移的内容
1. **UI 组件** - React/Ink 组件保留在 TypeScript
2. **终端渲染** - Ink 渲染引擎保留在 TypeScript
3. **前端交互** - 键盘绑定、Vim 模式等保留在 TypeScript
4. **原生模块** - color-diff、yoga-layout 等前端专用

### 架构差异
1. **Go 使用 TUI 包** - 替代 Ink 进行终端界面渲染
2. **Go 使用 Cobra** - 替代 TypeScript 的命令行解析
3. **Go 使用 Viper** - 替代 TypeScript 的配置管理
4. **Go 使用标准库** - 很多 TypeScript 的 npm 包在 Go 中有标准库替代

### 迁移原则
1. **保持 API 兼容** - 尽量保持与 TypeScript 版本的接口一致
2. **Go 惯用法** - 使用 Go 的最佳实践，不盲目照搬 TypeScript
3. **性能优先** - 利用 Go 的并发和性能优势
4. **测试覆盖** - 每个模块都要有完整的单元测试
5. **文档同步** - 代码和文档同步更新

---

## 📝 架构重组说明（2026-04-07）

### 目录结构优化
代码库已完成架构重组，采用清晰的分层结构：

**internal/app/** - 应用特定逻辑
- CLI 命令行接口
- 配置管理
- 启动初始化
- 入口点管理
- LSP 管理器
- 配置迁移

**internal/backend/** - 后端服务
- 桥接系统
- Remote 客户端
- HTTP/WebSocket/SSE 服务器
- 10 个核心服务（analytics, api, compact, context, cost, history, oauth, tasks, tokens, tools）
- 上游代理

**internal/harness/** - 可复用 Agent 框架
- 完整的 Agent 执行框架
- 支持构建 Web 界面或其他前端
- 包含 21 个子模块（agent, anthropic, context, coordinator, engine, hooks, mcp, memdir, memory, permissions, plugins, provider, query, queryengine, skills, state, tasks, tool, tools）
- 界面与逻辑完全分离

**internal/public/** - 可复用公共组件
- 基础工具库（apperrors, fsutil, ratelimit, schemas, types）
- 可被任何模块使用

**internal/ui/** - 用户界面
- TUI 终端用户界面
- 与 harness 解耦

### 架构优势
1. **清晰的职责分离** - app/backend/harness/public/ui 各司其职
2. **可复用性** - harness 可用于构建不同类型的前端
3. **可维护性** - 模块边界清晰，依赖关系明确
4. **可扩展性** - 易于添加新的服务或工具

---

生成时间：2026-04-07

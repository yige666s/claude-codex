# 6个核心模块重构计划

## 概述

将以下6个 TypeScript 模块重构到 Go：
1. **schemas** - 配置验证
2. **migrations** - 配置迁移
3. **entrypoints** - 初始化逻辑（后端部分）
4. **query** - 查询管道（核心业务逻辑）
5. **native-ts** - 原生工具函数
6. **upstreamproxy** - 代理配置

---

## 模块 1: schemas - 配置验证系统

### 优先级：⭐⭐⭐ 高（基础设施）

### TypeScript 实现分析

**规模**：
- 核心文件：8个主要 schema 文件
- 总代码量：约 200KB
- 使用 Zod v4 进行验证

**主要组件**：
1. **Hook Schemas** (`src/schemas/hooks.ts`)
   - 4种钩子类型：BashCommandHook, PromptHook, HttpHook, AgentHook
   - 条件执行支持（`if` 字段）
   - 超时和状态消息配置

2. **Plugin Schemas** (`src/utils/plugins/schemas.ts`, 64KB)
   - PluginManifest - 插件元数据
   - MarketplaceSource - 市场来源（GitHub, Git, local）
   - 安全特性：官方市场保护、同形异义攻击防护

3. **Settings Schemas** (`src/utils/settings/types.ts`, 46KB)
   - SettingsSchema - 根配置结构
   - PermissionsSchema - 权限规则
   - EnvironmentVariablesSchema - 环境变量
   - MCP 服务器白名单/黑名单

4. **Permission Validation** (`src/utils/settings/permissionValidation.ts`)
   - 括号匹配验证
   - 工具名大小写检查
   - MCP 特定验证
   - Bash 前缀验证
   - 文件模式验证

5. **MCP Server Schemas** (`src/services/mcp/types.ts`)
   - 5种传输类型：stdio, sse, http, ws, sdk
   - OAuth 配置支持

6. **Sandbox Schemas** (`src/entrypoints/sandboxTypes.ts`)
   - 网络配置（域名白名单、Unix socket）
   - 文件系统配置（读写权限）

7. **Keybindings Schema** (`src/keybindings/schema.ts`)
   - 18个上下文
   - 100+ 键绑定动作

8. **Validation Module** (`src/utils/settings/validation.ts`)
   - Zod 错误格式化
   - 设置文件内容验证
   - 无效规则过滤

### Go 实现计划

**目录结构**：
```
internal/schemas/
├── types.go              # 核心类型定义
├── hooks.go              # Hook schemas
├── plugins.go            # Plugin schemas
├── settings.go           # Settings schemas
├── permissions.go        # Permission validation
├── mcp.go                # MCP server schemas
├── sandbox.go            # Sandbox schemas
├── keybindings.go        # Keybindings schema
├── validation.go         # 验证逻辑和错误处理
└── schemas_test.go       # 测试
```

**技术选型**：
- 使用 Go struct tags 进行基础验证：`json`, `validate`
- 使用 `github.com/go-playground/validator/v10` 进行复杂验证
- 自定义验证器处理特殊逻辑（权限规则、括号匹配等）

**关键挑战**：
1. Zod 的 discriminated unions → Go 的 interface + type switch
2. Zod 的 refinements → 自定义 validator 函数
3. Zod 的 transforms → 单独的转换函数
4. 循环依赖处理 → Go 的 interface 和延迟初始化

**实现步骤**：
1. 定义核心类型（Hook, Plugin, Settings 等）
2. 实现基础验证器（struct tags）
3. 实现自定义验证器（权限规则、括号匹配）
4. 实现错误格式化和报告
5. 编写测试用例
6. 集成到配置系统

---

## 模块 2: migrations - 配置迁移系统

### 优先级：⭐⭐ 中（依赖 schemas）

### TypeScript 实现分析

**规模**：
- 迁移脚本：11个
- 每个脚本：50-150 行

**迁移列表**：
1. `migrateAutoUpdatesToSettings` - 自动更新配置
2. `migrateBypassPermissionsAcceptedToSettings` - 权限绕过
3. `migrateEnableAllProjectMcpServersToSettings` - MCP 服务器
4. `migrateFennecToOpus` - Fennec → Opus
5. `migrateLegacyOpusToCurrent` - 旧版 Opus
6. `migrateOpusToOpus1m` - Opus → Opus 1M
7. `migrateReplBridgeEnabledToRemoteControlAtStartup` - REPL Bridge
8. `migrateSonnet1mToSonnet45` - Sonnet 1M → 4.5
9. `migrateSonnet45ToSonnet46` - Sonnet 4.5 → 4.6
10. `resetAutoModeOptInForDefaultOffer` - 重置自动模式
11. `resetProToOpusDefault` - Pro 默认模型

**版本管理**：
- 使用 `GlobalConfig.migrationVersion` 追踪
- 当 `migrationVersion === CURRENT_MIGRATION_VERSION` 时跳过
- 优化启动性能

**迁移特点**：
- 幂等性：可安全多次执行
- 条件执行：每个迁移内部检查是否需要执行
- 独立性：迁移之间互不依赖
- 错误处理：使用 try-catch，失败不影响启动

### Go 实现计划

**目录结构**：
```
internal/migrations/
├── types.go              # 迁移类型定义
├── registry.go           # 迁移注册表
├── executor.go           # 迁移执行器
├── version.go            # 版本管理
├── migrations/           # 具体迁移脚本
│   ├── 001_auto_updates.go
│   ├── 002_bypass_permissions.go
│   ├── 003_mcp_servers.go
│   ├── 004_fennec_to_opus.go
│   ├── 005_legacy_opus.go
│   ├── 006_opus_to_opus1m.go
│   ├── 007_repl_bridge.go
│   ├── 008_sonnet1m_to_45.go
│   ├── 009_sonnet45_to_46.go
│   ├── 010_reset_auto_mode.go
│   └── 011_reset_pro_default.go
└── migrations_test.go    # 测试
```

**核心类型**：
```go
type Migration struct {
    Version     int
    Name        string
    Description string
    Migrate     func(ctx context.Context) error
}

type MigrationRegistry struct {
    migrations []Migration
    mu         sync.RWMutex
}

type MigrationExecutor struct {
    registry      *MigrationRegistry
    configManager *config.Manager
    analytics     *analytics.Client
}
```

**实现步骤**：
1. 定义迁移接口和注册表
2. 实现版本管理逻辑
3. 移植11个迁移脚本
4. 实现迁移执行器
5. 集成到初始化流程
6. 编写测试用例

---

## 模块 3: entrypoints - 初始化逻辑

### 优先级：⭐⭐⭐ 高（应用启动）

### TypeScript 实现分析

**规模**：
- 核心文件：`init.ts` (13.8KB)
- 相关文件：`cli.tsx` (39KB, 部分需要)

**初始化阶段**：

**阶段 1：配置系统启动**
- `enableConfigs()` - 启用配置系统
- `applySafeConfigEnvironmentVariables()` - 应用安全环境变量
- `applyExtraCACertsFromConfig()` - 应用 CA 证书

**阶段 2：后台服务初始化**
- `setupGracefulShutdown()` - 优雅关闭
- `initialize1PEventLogging()` - 第一方事件日志
- `populateOAuthAccountInfoIfNeeded()` - OAuth 账户信息
- `initJetBrainsDetection()` - JetBrains IDE 检测
- `detectCurrentRepository()` - GitHub 仓库检测

**阶段 3：策略和远程设置**
- `initializeRemoteManagedSettingsLoadingPromise()` - 远程托管设置
- `initializePolicyLimitsLoadingPromise()` - 策略限制

**阶段 4：网络配置**
- `configureGlobalAgents()` - 全局代理
- `configureGlobalMTLS()` - mTLS
- `preconnectAnthropicApi()` - API 预连接

**阶段 5：信任后初始化**
- `applyConfigEnvironmentVariables()` - 完整环境变量
- `ensureScratchpadDir()` - Scratchpad 目录
- `setShellIfWindows()` - Windows shell

### Go 实现计划

**目录结构**：
```
internal/entrypoints/
├── types.go              # 类型定义
├── init.go               # 主初始化逻辑
├── config.go             # 配置系统启动
├── services.go           # 后台服务初始化
├── network.go            # 网络配置
├── shutdown.go           # 优雅关闭
├── telemetry.go          # 遥测初始化
└── entrypoints_test.go   # 测试
```

**实现范围**：
- ✅ 配置系统启动
- ✅ 环境变量应用
- ✅ 后台服务初始化（分析、OAuth、GitHub）
- ✅ 网络配置（代理、mTLS、API预连接）
- ✅ 迁移执行集成
- ✅ 优雅关闭处理
- ❌ CLI UI（保留在 TypeScript）
- ❌ Ink 渲染（保留在 TypeScript）
- ❌ MCP 服务器入口（保留在 TypeScript）

**实现步骤**：
1. 定义初始化阶段和依赖关系
2. 实现配置系统启动
3. 实现后台服务初始化
4. 实现网络配置
5. 集成迁移系统
6. 实现优雅关闭
7. 编写测试用例

---

## 模块 4: query - 查询管道

### 优先级：⭐⭐⭐⭐ 最高（核心业务逻辑）

### TypeScript 实现分析

**规模**：
- 核心文件：`query.ts` (约 1000 行)
- 相关文件：20+ 文件

**核心组件**：

1. **主查询循环** (`query.ts`)
   - 状态机驱动的迭代循环
   - 支持最大轮次限制
   - Token 预算管理

2. **查询配置** (`query/config.ts`)
   - 配置快照（避免中途变化）
   - Feature gates

3. **依赖注入** (`query/deps.ts`)
   - 可测试的依赖管理
   - Mock 友好

4. **工具编排** (`services/tools/toolOrchestration.ts`)
   - 并发安全工具并行执行
   - 非安全工具串行执行
   - 分区算法

5. **流式工具执行器** (`services/tools/StreamingToolExecutor.ts`)
   - 工具流式到达时立即执行
   - 结果按顺序缓冲
   - 进度消息立即产出

6. **停止钩子** (`query/stopHooks.ts`)
   - Stop 钩子
   - TaskCompleted 钩子
   - TeammateIdle 钩子

7. **Token 预算** (`query/tokenBudget.ts`)
   - 递减检测
   - 预算追踪

8. **Agent 集成** (`tools/AgentTool/`)
   - Agent 执行
   - 子查询上下文
   - 资源清理

9. **查询引擎** (`QueryEngine.ts`)
   - 生命周期管理
   - 文件状态缓存
   - SDK 消息转换

10. **查询保护** (`utils/QueryGuard.ts`)
    - 并发保护
    - 状态机：idle → dispatching → running

### Go 实现计划

**目录结构**：
```
internal/query/
├── types.go              # 核心类型定义
├── query.go              # 主查询循环
├── config.go             # 查询配置快照
├── deps.go               # 依赖注入
├── state.go              # 查询状态管理
├── budget.go             # Token 预算管理
├── hooks.go              # 停止钩子处理
├── transitions.go        # 状态转换
├── engine.go             # 查询引擎封装
├── guard.go              # 并发保护
├── tools/                # 工具执行
│   ├── orchestration.go  # 工具编排
│   ├── executor.go       # 流式执行器
│   ├── partition.go      # 分区算法
│   └── execution.go      # 工具执行
├── agent/                # Agent 集成
│   ├── runner.go         # Agent 执行
│   ├── context.go        # 子查询上下文
│   └── cleanup.go        # 资源清理
└── query_test.go         # 测试
```

**核心类型**：
```go
type QueryParams struct {
    Messages        []Message
    SystemPrompt    []string
    UserContext     map[string]interface{}
    SystemContext   map[string]interface{}
    CanUseTool      func(string) bool
    ToolUseContext  *ToolUseContext
    QuerySource     string
    MaxTurns        int
    TaskBudget      *TaskBudget
}

type State struct {
    Messages                    []Message
    ToolUseContext              *ToolUseContext
    AutoCompactTracking         *AutoCompactTrackingState
    MaxOutputTokensRecoveryCount int
    HasAttemptedReactiveCompact bool
    MaxOutputTokensOverride     *int
    PendingToolUseSummary       chan *ToolUseSummaryMessage
    StopHookActive              *bool
    TurnCount                   int
    Transition                  *Continue
}

type Terminal struct {
    Reason  string
    Message string
}
```

**实现步骤**：
1. 定义核心类型和接口
2. 实现查询配置和依赖注入
3. 实现主查询循环
4. 实现工具编排和执行
5. 实现停止钩子处理
6. 实现 Token 预算管理
7. 实现 Agent 集成
8. 实现查询引擎和保护
9. 编写测试用例

**关键挑战**：
1. AsyncGenerator → Go channels
2. 流式处理 → goroutines + channels
3. 工具并发执行 → sync.WaitGroup + errgroup
4. 状态机管理 → 显式状态类型
5. 错误恢复 → defer + recover

---

## 模块 5: native-ts - 原生工具函数

### 优先级：⭐⭐ 中（独立工具）

### TypeScript 实现分析

**规模**：
- 3个子模块
- 总代码量：约 4000 行

**子模块**：

1. **yoga-layout** (2580 行)
   - Flexbox 布局引擎
   - 用于 Ink 终端 UI 布局
   - **评估**：可能不需要（UI 相关）

2. **file-index** (372 行)
   - 模糊文件搜索
   - 类似 fzf 的匹配算法
   - 字符位图快速过滤
   - 边界/驼峰命名奖励
   - **评估**：高优先级，后端需要

3. **color-diff** (1001 行)
   - 语法高亮差异显示
   - 基于 highlight.js
   - 行级和单词级差异
   - **评估**：中优先级，后端可能需要

### Go 实现计划

**目录结构**：
```
internal/native/
├── fileindex/            # 模糊文件搜索
│   ├── index.go          # 主实现
│   ├── matcher.go        # 匹配算法
│   ├── scorer.go         # 评分逻辑
│   └── fileindex_test.go # 测试
├── colordiff/            # 语法高亮差异
│   ├── diff.go           # 差异计算
│   ├── highlight.go      # 语法高亮
│   ├── render.go         # 渲染逻辑
│   └── colordiff_test.go # 测试
└── README.md             # 文档
```

**实现范围**：
- ✅ file-index - 完整移植
- ✅ color-diff - 完整移植
- ❌ yoga-layout - 不移植（UI 相关）

**file-index 实现**：
```go
type FileIndex struct {
    files   []string
    bitmap  []uint64
    cache   map[string][]Result
    mu      sync.RWMutex
}

type Result struct {
    Path  string
    Score float64
}

func (fi *FileIndex) LoadFromFileList(files []string)
func (fi *FileIndex) LoadFromFileListAsync(files []string) (<-chan struct{}, <-chan struct{})
func (fi *FileIndex) Search(query string, limit int) []Result
```

**color-diff 实现**：
```go
type ColorDiff struct {
    oldCode string
    newCode string
    lang    string
}

type ColorFile struct {
    code string
    lang string
}

func NewColorDiff(oldCode, newCode, filename string) *ColorDiff
func (cd *ColorDiff) Render(theme string, width int, lineNumbers bool) []string

func NewColorFile(code, filename string) *ColorFile
func (cf *ColorFile) Render(theme string, width int, lineNumbers bool) []string
```

**实现步骤**：
1. 实现 file-index 模糊匹配算法
2. 实现 file-index 评分逻辑
3. 实现 color-diff 差异计算
4. 集成 Go 语法高亮库（chroma）
5. 实现渲染逻辑
6. 编写测试用例

---

## 模块 6: upstreamproxy - 代理配置系统

### 优先级：⭐⭐ 中（网络基础设施）

### TypeScript 实现分析

**规模**：
- 核心文件：2个
- 相关文件：`utils/proxy.ts`

**核心组件**：

1. **代理配置管理** (`upstreamproxy.ts`)
   - `initUpstreamProxy()` - 初始化代理
   - `getUpstreamProxyEnv()` - 环境变量导出
   - NO_PROXY 列表管理

2. **CONNECT-over-WebSocket 中继** (`relay.ts`)
   - TCP 监听和 CONNECT 解析
   - WebSocket 隧道建立
   - Protobuf 编码/解码
   - 双向字节流转发
   - 保活机制

3. **代理检测和配置** (`utils/proxy.ts`)
   - 环境变量检测
   - NO_PROXY 匹配
   - 全局代理配置
   - WebSocket 代理支持
   - SDK 集成

### Go 实现计划

**目录结构**：
```
internal/upstreamproxy/
├── types.go              # 类型定义
├── config.go             # 代理配置管理
├── relay.go              # CONNECT-over-WebSocket 中继
├── protobuf.go           # Protobuf 编码/解码
├── auth.go               # Session token 认证
├── ca.go                 # CA 证书管理
├── noproxy.go            # NO_PROXY 匹配
├── integration.go        # SDK 集成
└── upstreamproxy_test.go # 测试
```

**核心类型**：
```go
type UpstreamProxyState struct {
    Enabled      bool
    Port         int
    CABundlePath string
}

type RelayServer struct {
    listener   net.Listener
    wsURL      string
    sessionID  string
    token      string
    mu         sync.RWMutex
}

type ProxyConfig struct {
    ProxyURL string
    NoProxy  []string
}
```

**实现步骤**：
1. 实现代理配置管理
2. 实现 CONNECT-over-WebSocket 中继
3. 实现 Protobuf 编码/解码
4. 实现 Session token 认证
5. 实现 CA 证书管理
6. 实现 NO_PROXY 匹配
7. 集成到网络层
8. 编写测试用例

---

## 实现优先级和依赖关系

### 依赖图

```
schemas (基础)
  ↓
migrations (依赖 schemas)
  ↓
entrypoints (依赖 schemas, migrations)
  ↓
query (依赖 schemas, entrypoints) ← 核心
  ↓
native-ts (独立)
upstreamproxy (独立)
```

### 实现顺序

**阶段 1：基础设施（2-3周）**
1. schemas - 配置验证系统
2. migrations - 配置迁移系统

**阶段 2：初始化（1-2周）**
3. entrypoints - 初始化逻辑

**阶段 3：核心逻辑（3-4周）**
4. query - 查询管道（最复杂）

**阶段 4：工具和网络（1-2周）**
5. native-ts - 原生工具函数
6. upstreamproxy - 代理配置系统

**总计：7-11周**

---

## 技术选型

### 验证库
- `github.com/go-playground/validator/v10` - 结构体验证
- 自定义验证器 - 复杂逻辑

### 配置管理
- `encoding/json` - JSON 解析
- `gopkg.in/yaml.v3` - YAML 解析（如需要）

### 网络库
- `net/http` - HTTP 客户端
- `gorilla/websocket` - WebSocket
- `golang.org/x/net/proxy` - 代理支持

### 并发控制
- `sync` - 标准库
- `golang.org/x/sync/errgroup` - 错误组

### 语法高亮
- `github.com/alecthomas/chroma` - 语法高亮

### Protobuf
- `google.golang.org/protobuf` - Protobuf 支持

---

## 测试策略

### 单元测试
- 每个模块 ≥ 80% 覆盖率
- 使用 table-driven tests
- Mock 外部依赖

### 集成测试
- 端到端查询流程
- 配置加载和验证
- 迁移执行

### 性能测试
- 查询循环性能
- 文件索引性能
- 工具并发执行

---

## 文档要求

每个模块需要：
1. README.md - 功能说明和使用示例
2. 架构文档 - 设计决策和组件关系
3. API 文档 - godoc 注释
4. 迁移指南 - 从 TypeScript 到 Go 的变化

---

## 风险和挑战

### 技术风险
1. **AsyncGenerator → Channels**：流式处理模式转换
2. **工具并发执行**：Go 的并发模型与 TS Promise.all 的差异
3. **错误处理**：Go 的显式错误 vs TS 的异常
4. **类型系统**：Zod 的运行时验证 vs Go 的编译时类型

### 业务风险
1. **功能遗漏**：可能遗漏 TS 中的边缘情况
2. **行为差异**：Go 和 TS 的细微行为差异
3. **性能回归**：需要性能基准测试
4. **兼容性**：与现有 TS 代码的互操作

### 缓解措施
1. 详细的单元测试和集成测试
2. 性能基准测试和对比
3. 逐步迁移，保持 TS 版本作为参考
4. 代码审查和文档

---

## 成功标准

### 功能完整性
- ✅ 所有核心功能移植完成
- ✅ 测试覆盖率 ≥ 80%
- ✅ 所有测试通过

### 性能指标
- ✅ 查询循环性能 ≥ TS 版本
- ✅ 文件索引性能 ≥ TS 版本
- ✅ 内存使用合理

### 代码质量
- ✅ 通过 golint 和 go vet
- ✅ 代码审查通过
- ✅ 文档完整

### 可维护性
- ✅ 清晰的模块边界
- ✅ 良好的错误处理
- ✅ 完整的日志记录

---

生成时间：2026-04-06

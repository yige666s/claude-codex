# Go 重构实现进度报告

## 已完成：限流系统 ✅

### 实现内容
1. **核心数据结构** (`internal/ratelimit/types.go`)
   - QuotaStatus (allowed, allowed_warning, rejected)
   - RateLimitType (5小时、7天、Opus、Sonnet、Overage)
   - ClaudeAILimits - 完整的限流状态
   - RawUtilization - 原始使用率追踪
   - EarlyWarningThreshold - 早期警告阈值

2. **限流追踪器** (`internal/ratelimit/tracker.go`)
   - 多窗口使用率追踪（5小时、7天、模型特定）
   - 响应头解析（anthropic-ratelimit-*）
   - 早期警告逻辑（基于使用率和时间进度）
   - 429错误处理
   - 状态变更监听器
   - 友好的错误和警告消息

3. **API 客户端集成** (`pkg/anthropic/client.go`)
   - 在每次 API 响应后处理限流头
   - 429 错误时更新限流状态
   - 暴露 GetRateLimiter() 方法

4. **Slash 命令** (`internal/cli/slash_limits.go`)
   - `/limits` 命令显示限流状态
   - 别名：`/quota`, `/usage`
   - 显示各窗口使用率和重置时间

5. **测试覆盖** (`internal/ratelimit/tracker_test.go`)
   - 12个测试用例，100%通过
   - 覆盖所有核心功能

### 架构特点
- 线程安全（使用 sync.RWMutex）
- 响应式更新（状态监听器模式）
- 精确的时间进度计算
- 支持多种限流窗口
- 友好的用户消息

---

## 已完成：记忆系统 ✅

### 实现内容
1. **核心数据结构** (`internal/memory/types.go`)
   - MemoryType (user, feedback, project, reference)
   - Memory - 记忆条目（名称、描述、类型、内容、时间戳）
   - MemoryIndex - MEMORY.md 索引
   - SessionMemoryConfig - 自动提取配置
   - ExtractionState - 提取状态追踪

2. **存储层** (`internal/memory/storage.go`)
   - Markdown 文件读写（带 YAML frontmatter）
   - MEMORY.md 索引管理
   - 文件系统操作（列表、删除、搜索）
   - Frontmatter 解析

3. **提取器** (`internal/memory/extractor.go`)
   - 自动提取触发逻辑
   - 基于 token 和工具调用阈值
   - 提取状态管理
   - 线程安全（sync.RWMutex）
   - Agent 集成接口
   - 完整的 Agent 调用实现
   - 提取提示词生成
   - JSON 响应解析
   - 自动保存提取的记忆

4. **管理器** (`internal/memory/manager.go`)
   - 高级记忆操作 API
   - 保存、加载、搜索、过滤
   - 自动索引更新
   - 类型过滤

5. **兼容层** (`internal/memory/legacy.go`)
   - 保持旧 API 兼容性
   - 简单行式文件操作
   - 向后兼容旧命令

6. **Slash 命令** (`internal/cli/slash_memory.go`)
   - `/mem2` 命令（新系统）
   - 子命令：list, show, search, filter, index, stats
   - 友好的输出格式
   - 旧 `/memory` 命令保持兼容

7. **测试覆盖** (`internal/memory/memory_test.go`)
   - Storage 测试（保存、加载、索引、列表、删除）
   - Extractor 测试（触发逻辑、状态管理）
   - Manager 测试（搜索、过滤、删除）
   - Agent 集成测试（SetAgentManager, ExtractMemories）
   - 所有测试通过

### 架构特点
- 基于 TypeScript 实现的完整移植
- Markdown + YAML frontmatter 存储格式
- 自动提取触发机制（待 Agent 系统完成后集成）
- 类型化记忆系统（4种类型）
- 索引文件管理
- 线程安全
- 向后兼容

---

## 已完成：Agent 系统 ✅

### 实现内容
1. **核心数据结构** (`internal/agent/types.go`)
   - AgentDefinition - 代理定义（类型、工具、模型、权限）
   - AgentInstance - 运行中的代理实例
   - AgentConfig - 代理配置
   - AgentResult - 执行结果
   - Message/ContentBlock - 消息和内容块
   - AgentStatus - 状态枚举（starting, running, completed, failed, aborted）

2. **执行器** (`internal/agent/executor.go`)
   - 代理执行引擎
   - 对话循环管理
   - 消息格式转换（内部 ↔ API）
   - 实例注册和追踪
   - 进度监听器模式
   - 中止机制

3. **Fork 系统** (`internal/agent/fork.go`)
   - Fork 代理定义（继承父上下文）
   - 构建 forked 消息序列
   - Fork 样板文本生成
   - 递归 fork 检测
   - Worktree 隔离支持

4. **进度追踪** (`internal/agent/progress.go`)
   - 后台进度摘要生成（每30秒）
   - 代理追踪器
   - 持续时间格式化
   - 进度通知

5. **管理器** (`internal/agent/manager.go`)
   - 高级代理管理 API
   - 代理定义注册
   - 按类型运行代理
   - Fork 代理创建
   - 内置代理初始化（fork, general-purpose, explore）
   - 状态查询和中止

6. **测试覆盖** (`internal/agent/agent_test.go`, `internal/agent/executor_tool_test.go`)
   - 类型测试
   - Fork 功能测试
   - 管理器测试
   - 进度追踪测试
   - 执行器测试
   - 工具执行测试（buildAPITools, extractToolUseBlocks, executeTools）
   - 消息转换测试（convertAPIResponse, convertMessagesToAPI）
   - 所有测试通过

7. **工具执行集成** (`internal/agent/executor.go`)
   - 工具注册表集成
   - 工具描述符转换为 API 格式
   - 通配符工具支持（"*"）
   - tool_use 块解析和提取
   - 工具执行和错误处理
   - tool_result 块生成
   - 多轮工具调用支持
   - 消息格式转换（tool_use 和 tool_result）

8. **流式输出支持** (`internal/agent/executor.go`, `internal/agent/types.go`)
   - StreamCallback 回调机制
   - StreamEvent 事件类型（text_delta, tool_use_start, tool_use_end）
   - executeWithStreaming 方法
   - SSE 事件解析（message_start, content_block_start, content_block_delta, etc.）
   - 实时文本增量通知
   - 工具使用开始/结束通知
   - 响应累积和最终构建
   - 自动检测是否启用流式输出

### 架构特点
- 基于 TypeScript 实现的完整移植
- 支持多种代理类型（built-in, plugin, user）
- Fork 机制实现上下文继承
- 后台进度追踪（30秒间隔）
- 权限模式（default, bubble, allow, deny）
- 模型继承和覆盖
- 线程安全的实例管理
- 监听器模式用于进度通知

### 待完善功能
- ✅ 工具执行集成（已完成）
- ✅ 流式输出支持（已完成）
- ❌ 完整的资源清理机制

---

## 待实现系统（暂不处理）

### 1. Slash 命令系统 ⏸️

**TypeScript 架构分析：**
- **命令类型**：
  - PromptCommand - 扩展为提示词
  - LocalCommand - JS 函数
  - LocalJSXCommand - React 组件
- **执行上下文**：
  - inline - 内联执行
  - fork - 独立子 agent
- **集成点**：
  - Skills 系统
  - Plugin 系统
  - Agent 系统
  - Hooks 系统

**当前状态：**
- ✅ 基础命令注册和执行
- ✅ 命令别名支持
- ✅ 自动补全系统
- ✅ 记忆系统命令（/mem2）
- ✅ 限流命令（/limits）
- ❌ Fork 执行上下文（需要集成 Agent 系统）
- ❌ Effort 系统
- ❌ Hooks 集成
- ❌ 插件命令

**Go 实现计划：**
```
internal/cli/
├── command_types.go  # 命令类型定义
├── registry.go       # 命令注册表（已有）
├── executor.go       # 命令执行器
└── fork.go           # Fork 执行上下文（需要 Agent 集成）
```

**说明：** 基础功能已满足当前需求，高级功能（Fork 执行、Hooks）暂不实现。

---

### 2. LSP 集成系统 ⏸️

**TypeScript 架构分析：**
- **LSPClient**：JSON-RPC 通信（基于 vscode-jsonrpc）
- **LSPServerInstance**：单服务器生命周期管理
  - 状态机：stopped → starting → running → stopping
  - 崩溃恢复（最大重启次数）
  - 重试逻辑
- **LSPServerManager**：多服务器路由
  - 文件扩展名映射
  - 文件同步（didOpen/didChange/didSave）
  - 懒加载

**当前状态：**
- ✅ 基础 LSPTool（简单符号扫描）
- ❌ 真正的 LSP 客户端
- ❌ 服务器生命周期管理
- ❌ 多服务器路由
- ❌ 文件同步

**Go 实现计划：**
```
internal/lsp/
├── client.go         # JSON-RPC 客户端
├── server.go         # 服务器实例管理
├── manager.go        # 多服务器路由
├── protocol.go       # LSP 协议类型
└── sync.go           # 文件同步
```

**说明：** 基础符号扫描已满足基本需求，完整的 LSP 集成工作量大，暂不实现。

---

### 3. Utils 工具库重构 📋

**TypeScript 现状分析：**
- **规模**：564 个文件，约 18 万行代码，36 个子系统
- **主要子系统**：
  - permissions/ - 权限系统
  - bash/ - Bash 工具执行
  - mcp/ - MCP 服务器集成
  - hooks/ - Hook 系统
  - task/ - 任务管理
  - settings/ - 设置管理
  - git/ - Git 集成
  - github/ - GitHub 集成
  - skills/ - Skills 系统
  - suggestions/ - 自动补全建议
  - telemetry/ - 遥测和分析
  - sandbox/ - Sandbox 环境
  - 其他 24 个子系统

**重构策略：**
- **阶段 1**：核心工具（文件操作、Shell 执行、格式化）
- **阶段 2**：业务逻辑工具（权限、任务、设置）
- **阶段 3**：集成工具（Git、GitHub、MCP）
- **阶段 4**：高级功能（Skills、Hooks、Telemetry）

**优先级：**
- 🔴 高优先级：与已重构系统（Agent、Memory、RateLimit）直接相关的工具
- 🟡 中优先级：独立的业务逻辑工具
- 🟢 低优先级：UI 相关、前端特定的工具

**说明：** 这是一个超大型重构任务，需要分阶段、分模块逐步进行。许多工具与前端 UI 紧密耦合，可能不需要重构到 Go。

---

## 实现优先级建议

基于依赖关系和用户价值：

1. **Agent 系统增强** ⭐⭐
   - 工具执行集成
   - 流式输出支持
   - 完整的资源清理
   - 可逐步完善

2. **Slash 命令增强** ⭐⭐
   - 基础功能已完善
   - Fork 执行需要 Agent 系统完善
   - 增强用户体验
   - 可逐步完善

3. **LSP 集成** ⭐⭐
   - 提升代码智能
   - 相对独立
   - 需要外部依赖

---

## 已完成：Tasks 模块 ✅

### 实现内容
1. **核心类型定义** (`internal/tasks/types.go`)
   - TaskType - 7 种任务类型（local_bash, local_agent, remote_agent, in_process_teammate, local_workflow, monitor_mcp, dream）
   - TaskStatus - 5 种状态（pending, running, completed, failed, killed）
   - TaskStateBase - 所有任务共享的基础字段
   - 7 种具体任务状态类型（LocalShellTaskState, LocalAgentTaskState, RemoteAgentTaskState, InProcessTeammateTaskState, LocalWorkflowTaskState, MonitorMCPTaskState, DreamTaskState）
   - TaskState 接口 - 统一的任务状态接口
   - TaskRegistry - 任务实现注册表

2. **任务 ID 生成** (`internal/tasks/task_id.go`)
   - GenerateTaskID - 生成类型前缀的任务 ID
   - GenerateMainSessionTaskID - 生成主会话任务 ID
   - 使用 crypto/rand 生成安全随机 ID
   - 36^8 ≈ 2.8 万亿组合防暴力攻击

3. **任务框架** (`internal/tasks/framework.go`)
   - TaskManager - 任务生命周期管理
   - UpdateTaskState - 更新任务状态
   - RegisterTask - 注册新任务
   - EvictTerminalTask - 清理终止任务
   - CreateTaskStateBase - 创建基础任务状态
   - GetRunningTasks - 获取运行中任务
   - GetBackgroundTasks - 获取后台任务

4. **任务输出管理** (`internal/tasks/output.go`)
   - DiskTaskOutput - 异步磁盘输出写入器
   - GetTaskOutputPath - 获取输出文件路径
   - ReadTaskOutput - 读取任务输出
   - GetTaskOutputDelta - 读取增量输出
   - EvictTaskOutput - 删除输出文件
   - InitTaskOutputAsSymlink - 创建输出符号链接
   - 5GB 大小限制
   - 线程安全的写入队列

5. **任务停止控制** (`internal/tasks/stop_task.go`)
   - StopTask - 停止运行中的任务
   - StopTaskError - 停止错误类型（not_found, not_running, unsupported_type）
   - StopTaskContext - 停止上下文
   - StopTaskResult - 停止结果
   - 优雅停止和通知抑制

6. **测试覆盖** (`internal/tasks/tasks_test.go`)
   - 6 个测试用例，100% 通过
   - 任务 ID 生成测试（7 种类型 + 主会话）
   - 任务状态判断测试
   - 任务注册表测试
   - 任务管理器测试

7. **文档** (`internal/tasks/README.md`)
   - 完整的功能说明
   - 架构组件详解
   - 使用示例
   - 最佳实践

### 架构特点
- 基于 TypeScript 实现的完整移植
- 支持 7 种任务类型
- 任务生命周期管理（pending → running → completed/failed/killed）
- 后台任务支持
- 异步磁盘输出（5GB 限制）
- 线程安全操作
- 可扩展的任务注册表
- 安全的任务 ID 生成

### 安全特性
- crypto/rand 生成随机 ID
- 36^8 组合防暴力攻击
- 输出文件大小限制（5GB）
- 线程安全的状态管理
- 优雅的资源清理

---

## 已完成：State 模块 ✅

### 实现内容
1. **应用状态定义** (`internal/state/app_state.go`)
   - AppState - 后端应用核心状态
   - ToolPermissionContext - 工具权限上下文
   - AgentDefinitionsResult - Agent 定义结果
   - FileHistoryState - 文件历史状态
   - MCPState - MCP 集成状态
   - PluginState - 插件系统状态
   - NotificationState - 通知队列状态
   - SessionHook - 会话钩子

2. **Store 实现** (`internal/state/store.go`)
   - Store - 状态管理器
   - GetState - 获取当前状态
   - SetState - 函数式状态更新
   - Subscribe - 订阅状态变更
   - 线程安全的读写操作

3. **辅助方法** (`internal/state/helpers.go`)
   - 任务管理（AddTask, GetTask, RemoveTask）
   - Agent 注册（RegisterAgentName, GetAgentIDByName）
   - 通知管理（AddNotification, GetCurrentNotification）
   - 文件历史（AddFileSnapshot, TrackFile, IsFileTracked）
   - MCP 工具（AddMCPTool, GetMCPTools）
   - 插件管理（AddPlugin, GetEnabledPlugins）
   - 权限模式（SetPermissionMode, GetPermissionMode）
   - 详细模式（SetVerbose, IsVerbose）
   - 认证版本（IncrementAuthVersion, GetAuthVersion）
   - **注意**：辅助方法不包含锁定逻辑，应通过 Store.SetState() 使用或在单线程上下文中直接调用

4. **测试覆盖** (`internal/state/state_test.go`)
   - 9 个测试用例，100% 通过
   - 状态初始化测试
   - Store 功能测试
   - 订阅机制测试
   - 辅助方法测试
   - 并发访问测试

5. **文档** (`internal/state/README.md`)
   - 完整的功能说明
   - 架构组件详解
   - 使用示例
   - 设计决策说明
   - 锁定策略说明

### 架构特点
- 基于 TypeScript 实现的后端状态提取
- 轻量级发布-订阅模式
- 分层锁定：Store 层处理并发控制，AppState 辅助方法保持简单
- 函数式状态更新
- 只包含后端逻辑相关的状态
- 排除所有前端 UI 状态

### 包含的后端状态
- ✅ 任务管理（Tasks）
- ✅ Agent 系统（AgentNameRegistry, AgentDefinitions）
- ✅ 权限系统（ToolPermissionContext）
- ✅ 文件历史（FileHistory）
- ✅ MCP 集成（MCP clients/tools/commands）
- ✅ 插件系统（Plugins enabled/disabled/errors）
- ✅ 通知队列（Notifications）
- ✅ 会话钩子（SessionHooks）
- ✅ 配置（Settings, Verbose, MainLoopModel）
- ✅ 远程连接（RemoteSessionURL, RemoteConnectionStatus）

### 排除的前端状态
- ❌ UI 视图状态（expandedView, viewSelectionMode）
- ❌ UI 选择状态（footerSelection, coordinatorTaskIndex）
- ❌ UI 显示状态（statusLineText, spinnerTip）
- ❌ 推测执行（speculation）
- ❌ 提示建议（promptSuggestion）
- ❌ Tmux/WebBrowser 面板状态

---

## 已完成功能总结

✅ **限流系统** - 完整实现，包括多窗口追踪、早期警告、CLI 命令
✅ **记忆系统** - 核心功能完成，存储、索引、搜索、CLI 命令、Agent 自动提取集成
✅ **Agent 系统** - 核心架构完成，执行器、Fork、进度追踪、管理器、工具执行、流式输出
✅ **Remote 客户端模块** - WebSocket 连接、消息适配、会话管理、权限处理
✅ **Memdir 模块** - 路径管理、团队记忆、智能检索、年龄追踪、安全验证
✅ **Tasks 模块** - 任务类型、生命周期、输出管理、停止控制、注册表
✅ **State 模块** - 后端状态管理、Store 实现、订阅机制、辅助方法
✅ **Schemas 模块** - 配置验证、权限规则、Hook/MCP 验证、市场验证
✅ **Migrations 模块** - 迁移注册表、执行器、版本管理、11个迁移脚本
✅ **Entrypoints 模块** - 初始化框架、5阶段启动、优雅关闭、接口抽象
✅ **Upstreamproxy 模块** - Protobuf 编解码、TCP 中继、代理配置、环境变量
✅ **Query 模块** - 查询配置、Token 预算、依赖注入、终止/继续原因
✅ **Skills 模块** - 技能注册表、文件加载、条件激活、MCP 集成、安全验证
✅ **Plugins 模块** - 内置插件注册、启用/禁用管理、技能集成、可用性检查

---

## 已完成：Skills 模块 ✅

### 实现内容
1. **核心类型定义** (`internal/skills/types.go`)
   - SkillDefinition - 技能定义（名称、描述、执行配置）
   - SkillSource - 技能来源（bundled, file, mcp, plugin, managed）
   - ExecutionContext - 执行上下文（inline, fork）
   - SkillRegistry - 技能注册表（名称和别名查询）
   - PromptGenerator - 提示词生成器接口
   - SkillContext - 技能执行上下文
   - ContentBlock - 内容块类型

2. **安全工具** (`internal/skills/security.go`)
   - ValidateSkillPath - 路径验证（防遍历攻击）
   - SafeWriteFile - 安全文件写入（O_EXCL | O_NOFOLLOW）
   - GetFileIdentity - 文件标识（符号链接解析）
   - IsPathSafe - 路径安全检查
   - SanitizeSkillName - 技能名称清理
   - WriteSkillFiles - 批量安全写入

3. **内置技能注册表** (`internal/skills/bundled.go`)
   - BundledSkillRegistry - 内置技能管理
   - RegisterBundledSkill - 注册内置技能
   - GetBundledSkills - 获取所有内置技能
   - ExtractBundledSkillFiles - 提取引用文件到磁盘
   - NewSimpleSkill - 创建简单文本技能
   - NewCustomSkill - 创建自定义技能

4. **Frontmatter 解析器** (`internal/skills/parser.go`)
   - ParseSkillFile - 解析 Markdown + YAML frontmatter
   - CoerceDescriptionToString - 描述类型转换
   - ParseBooleanFrontmatter - 布尔值解析
   - ParseStringArray - 字符串数组解析
   - ParseAllowedTools - 工具列表解析
   - ParsePaths - 路径模式解析
   - ParseEffort - 努力级别解析
   - ExtractDescriptionFromMarkdown - 从内容提取描述
   - EstimateTokenCount - Token 数量估算

5. **文件系统加载器** (`internal/skills/loader.go`)
   - SkillLoader - 技能加载器
   - LoadSkillsFromDirectory - 从目录加载技能
   - LoadSkillFromFile - 从文件加载单个技能
   - buildSkillDefinition - 构建技能定义
   - substituteArguments - 参数替换
   - LoadSkillsFromMultipleDirs - 从多个目录加载
   - ReloadSkill - 重新加载技能

6. **技能缓存和管理器** (`internal/skills/cache.go`)
   - SkillCache - 技能缓存（基于文件标识去重）
   - SkillManager - 技能管理器（统一管理所有技能）
   - LoadBundledSkills - 加载内置技能
   - GetSkill - 查询技能
   - ListSkills - 列出所有技能
   - ListUserInvocableSkills - 列出用户可调用技能
   - GetDynamicSkills - 获取动态加载的技能
   - ClearDynamicSkills - 清除动态技能
   - OnSkillsChanged - 变更监听器
   - GetStats - 统计信息

7. **条件激活系统** (`internal/skills/activation.go`)
   - ActivateConditionalSkillsForPaths - 基于路径激活技能
   - shouldActivateSkill - 技能激活判断
   - matchesPattern - Glob 模式匹配
   - DiscoverSkillDirsForPaths - 发现技能目录
   - AddSkillDirectories - 添加技能目录
   - GetSkillsForPaths - 获取路径相关技能
   - DeactivateSkill - 停用技能
   - ReloadSkillsForPaths - 重新加载技能

8. **MCP 桥接** (`internal/skills/mcp_bridge.go`)
   - MCPSkillBuilder - MCP 技能构建器接口
   - RegisterMCPSkillBuilder - 注册构建器
   - BuildMCPSkill - 从 MCP 元数据构建技能
   - DefaultMCPSkillBuilder - 默认构建器实现
   - LoadMCPSkills - 加载 MCP 技能
   - RemoveMCPSkills - 移除 MCP 技能
   - GetMCPSkills - 获取 MCP 技能

9. **测试覆盖** (`internal/skills/skills_test.go`)
   - 11 个测试函数，100% 通过
   - 技能注册表测试
   - 安全验证测试
   - Frontmatter 解析测试
   - 文件加载测试
   - 技能管理器测试
   - 条件激活测试
   - MCP 集成测试

10. **文档** (`internal/skills/README.md`)
    - 完整的使用文档
    - API 参考
    - 安全特性说明
    - 架构设计说明

### 架构特点
- 基于 TypeScript 实现的完整移植
- 支持 5 种技能来源（bundled, file, mcp, plugin, managed）
- Markdown + YAML frontmatter 格式
- 基于文件标识的去重机制
- 条件激活（基于路径模式）
- 安全路径验证（防遍历攻击）
- 安全文件写入（O_EXCL | O_NOFOLLOW）
- 线程安全的注册表和缓存
- 变更监听器模式
- MCP 技能集成

### 安全特性
- 路径遍历防护（ValidateSkillPath）
- 符号链接安全处理（GetFileIdentity）
- 安全文件写入（O_EXCL | O_NOFOLLOW）
- 文件权限控制（0700/0600）
- 技能名称清理（SanitizeSkillName）

### 性能优化
- 基于文件标识的缓存去重
- 懒加载技能内容
- 读写锁分离
- 异步变更通知
- 高效的 Glob 匹配（doublestar）

---

## 已完成：Plugins 模块 ✅

### 实现内容
1. **核心类型定义** (`internal/plugins/builtin.go`)
   - BuiltinPluginDefinition - 内置插件定义
   - LoadedPlugin - 已加载的插件
   - PluginManifest - 插件元数据
   - BuiltinPluginRegistry - 内置插件注册表

2. **插件注册和管理** (`internal/plugins/builtin.go`)
   - RegisterBuiltinPlugin - 注册内置插件
   - GetBuiltinPluginDefinition - 获取插件定义
   - GetBuiltinPlugins - 获取启用/禁用的插件列表
   - GetBuiltinPluginSkills - 获取插件提供的技能
   - IsBuiltinPluginID - 检查插件 ID 格式
   - ClearBuiltinPlugins - 清理注册表（测试用）
   - InitBuiltinPlugins - 初始化内置插件

3. **插件加载器** (`internal/plugins/loader.go`)
   - Loader - 插件加载器
   - Load - 从文件系统加载插件
   - findPluginManifest - 查找插件清单文件

4. **插件 ID 格式**
   - 内置插件：`{plugin-name}@builtin`
   - 市场插件：`{plugin-name}@{marketplace-name}`

5. **启用状态优先级**
   - 用户设置 > 插件默认值 > 全局默认值（true）
   - 支持用户覆盖插件默认启用状态

6. **可用性检查**
   - IsAvailable 函数动态检查插件可用性
   - 不可用的插件完全排除（不出现在启用或禁用列表）
   - 支持平台特定、环境特定的插件

7. **Skills 集成**
   - 插件可以提供技能定义
   - 只有启用的插件的技能才会被加载
   - 与 Skills 模块无缝集成

8. **测试覆盖** (`internal/plugins/plugins_test.go`)
   - 7 个测试函数，100% 通过
   - 插件注册测试
   - 插件 ID 验证测试
   - 启用/禁用逻辑测试
   - 用户设置优先级测试
   - 技能集成测试
   - 可用性检查测试
   - 注册表清理测试

9. **文档** (`internal/plugins/README.md`)
   - 完整的使用文档
   - API 参考
   - 使用示例
   - 架构设计说明

### 架构特点
- 基于 TypeScript 实现的完整移植
- 线程安全的插件注册表（sync.RWMutex）
- 用户可配置的启用/禁用状态
- 动态可用性检查
- 与 Skills 模块集成
- 支持 Hooks 和 MCP 服务器配置
- 单例模式的全局注册表

### 与 TypeScript 版本的差异
- TypeScript 使用 Map → Go 使用 map
- TypeScript 动态导入 → Go 静态注册
- TypeScript 设置系统 → Go 接受设置参数
- 更简单的 API 设计
- 更好的并发控制
- 无运行时依赖

### 安全特性
- 线程安全的注册表操作
- 插件 ID 格式验证
- 可用性检查防止加载不兼容插件

---

### 实现内容
1. **路径管理** (`internal/memdir/paths.go`)
   - 自动记忆启用检查
   - 记忆基础目录解析
   - 路径验证和安全检查
   - 波浪号扩展支持
   - 每日日志路径生成

2. **团队记忆路径** (`internal/memdir/team_paths.go`)
   - 团队记忆启用检查
   - 团队记忆路径管理
   - 路径键清理（防注入）
   - URL 编码遍历检测
   - Unicode 规范化攻击防护
   - 符号链接解析和验证
   - 悬空符号链接检测

3. **记忆年龄** (`internal/memdir/memory_age.go`)
   - 天数计算（floor-rounded）
   - 人类可读年龄字符串
   - 新鲜度警告文本
   - 系统提醒标签包装

4. **记忆扫描** (`internal/memdir/memory_scan.go`)
   - 递归目录扫描
   - YAML frontmatter 解析
   - 按修改时间排序
   - 限制最多 200 个文件
   - 记忆清单格式化

5. **智能检索** (`internal/memdir/find_relevant.go`)
   - 使用 Sonnet 模型选择相关记忆
   - 最多返回 5 个相关记忆
   - 过滤已展示的记忆
   - 最近使用工具过滤

6. **提示词构建** (`internal/memdir/memdir.go`)
   - 自动记忆提示词生成
   - 团队记忆提示词生成
   - 入口文件截断（行数和字节数）
   - 记忆类型定义
   - 保存和访问指导

7. **测试覆盖** (`internal/memdir/memdir_test.go`)
   - 14 个测试用例，100% 通过
   - 路径验证测试
   - 记忆年龄测试
   - 文件扫描测试
   - Frontmatter 解析测试
   - 入口文件截断测试
   - 安全验证测试

### 架构特点
- 安全优先：多层路径验证和注入防护
- 智能检索：使用 AI 模型选择相关记忆
- 年龄追踪：自动追踪记忆新鲜度
- 团队协作：支持私有和共享记忆
- 性能优化：限制文件数量和大小
- 错误恢复：优雅处理文件系统错误

### 安全特性
- 拒绝相对路径、根路径、UNC 路径
- 拒绝 URL 编码遍历（%2e%2e%2f）
- 拒绝 Unicode 规范化攻击（全角字符）
- 拒绝反斜杠和空字节
- 符号链接解析和验证
- 悬空符号链接检测

---

## 已完成：Remote 客户端模块 ✅

### 实现内容
1. **核心类型定义** (`internal/remote/types.go`)
   - SDK 消息类型（11种消息类型）
   - 控制消息类型（请求、响应、取消）
   - 会话配置类型
   - WebSocket 状态和常量

2. **WebSocket 客户端** (`internal/remote/websocket.go`)
   - WebSocket 连接管理
   - 自动重连机制（最多5次）
   - Ping/Pong 心跳（30秒间隔）
   - 会话未找到重试（最多3次）
   - 永久关闭码处理
   - 线程安全操作

3. **SDK 消息适配器** (`internal/remote/adapter.go`)
   - SDK 消息格式转换
   - 11种消息类型处理
   - 流式事件转换
   - 合成 AssistantMessage 创建
   - 工具存根生成

4. **远程会话管理器** (`internal/remote/manager.go`)
   - 会话生命周期管理
   - 权限请求处理
   - 消息路由
   - 控制请求发送
   - 会话中断支持

5. **测试覆盖** (`internal/remote/remote_test.go`)
   - 16个测试用例，100%通过
   - 消息转换测试
   - 会话管理测试
   - 类型和常量测试

### 架构特点
- 完整的 CCR 协议支持
- WebSocket 双向通信
- 自动重连和错误恢复
- 权限请求/响应流程
- 线程安全设计
- 可扩展的消息类型系统

---

## 已完成：entrypoints 模块 ✅

### 实现内容
1. **核心类型** (`internal/entrypoints/types.go`)
   - InitPhase - 初始化阶段枚举
   - InitOptions - 初始化选项配置
   - InitResult - 初始化结果
   - InitContext - 初始化上下文
   - 6个管理器接口（Config, Migration, Telemetry, Network, Service, Shutdown）

2. **初始化逻辑** (`internal/entrypoints/init.go`)
   - Initializer - 主初始化协调器
   - Initialize - 完整初始化流程（5个阶段）
   - InitializeAfterTrust - 信任后初始化
   - 分阶段执行（配置系统、后台服务、远程设置、网络配置）
   - 异步服务启动（不阻塞主流程）

3. **优雅关闭** (`internal/entrypoints/shutdown.go`)
   - DefaultShutdownManager - 关闭管理器实现
   - RegisterCleanup - 注册清理函数
   - Shutdown - 执行清理（反向顺序）
   - No-Op 管理器（测试用）

4. **测试覆盖** (`internal/entrypoints/entrypoints_test.go`)
   - 4个测试组，100%通过
   - 初始化测试（成功、远程设置、跳过选项、信任后）
   - 关闭管理器测试（注册、执行、双重关闭、关闭后注册）
   - 阶段和选项测试

5. **文档** (`internal/entrypoints/README.md`)
   - 完整的使用文档
   - 接口说明和示例
   - 与 migrations 模块集成示例

### 架构特点
- 接口驱动设计（所有管理器都是接口）
- 分阶段执行（清晰的依赖关系）
- 异步初始化（后台服务不阻塞）
- 优雅关闭（反向执行清理函数）
- 易于测试和扩展

### 待完成工作
- ⏳ ConfigManager 具体实现（需要 config 模块）
- ⏳ TelemetryManager 具体实现
- ⏳ NetworkManager 具体实现（代理、mTLS）
- ⏳ ServiceManager 具体实现（OAuth、分析、检测）

---

## 已完成：schemas 模块 ✅

### 实现内容
1. **核心类型** (`internal/schemas/types.go`)
   - Settings - 根配置结构
   - Permissions - 权限配置
   - Hook 接口和实现（Bash, Prompt, HTTP, Agent）
   - MCPServerConfig 接口和实现（Stdio, SSE, HTTP, WebSocket）
   - MarketplaceSource - 市场来源配置
   - SandboxSettings - 沙箱配置
   - Keybinding - 快捷键配置

2. **验证逻辑** (`internal/schemas/validation.go`)
   - ValidateSettings - 完整配置验证
   - validatePermissions - 权限规则验证
   - validateHooks - Hook 配置验证
   - validateMCPServers - MCP 服务器验证
   - validateMarketplaces - 市场验证（官方名称保护、同形异义攻击防护）
   - validateSandbox - 沙箱配置验证
   - validateKeybindings - 快捷键验证（重复检测）

3. **权限规则验证** (`internal/schemas/permissions.go`)
   - ValidatePermissionRule - 规则验证
   - validateParentheses - 括号匹配（支持转义）
   - validateMCPPattern - MCP 模式验证
   - validateBashPattern - Bash 模式验证
   - validateFilePattern - 文件模式验证
   - validateGlobPattern - Glob 模式验证
   - validateToolNameCapitalization - 工具名大小写检查

4. **测试覆盖** (`internal/schemas/schemas_test.go`)
   - 9个测试函数，100%通过
   - 覆盖所有核心功能

5. **文档** (`internal/schemas/README.md`)
   - 完整的使用文档
   - 架构设计说明
   - 与 TypeScript 版本的对比

### 架构特点
- 使用 Go interface 实现多态
- 使用 struct embedding 共享通用字段
- 累积错误验证（收集所有错误）
- 详细的错误路径和建议

---

## 已完成：migrations 模块 ✅

### 实现内容
1. **核心类型** (`internal/migrations/types.go`)
   - Migration - 迁移定义
   - MigrationError - 迁移错误
   - MigrationResult - 执行结果
   - CurrentMigrationVersion = 11

2. **注册表** (`internal/migrations/registry.go`)
   - Registry - 迁移注册表
   - 动态注册和查询
   - 按版本排序
   - 获取待处理迁移

3. **执行器** (`internal/migrations/executor.go`)
   - Executor - 迁移执行器
   - VersionManager 接口
   - AnalyticsLogger 接口
   - ExecuteOptions（DryRun, StopOnError, TargetVersion）
   - 批量执行和错误处理

4. **版本管理** (`internal/migrations/version.go`)
   - FileVersionManager - 基于文件的版本管理
   - JSON 格式存储
   - NoOpAnalyticsLogger - 测试用

5. **迁移脚本** (`internal/migrations/scripts/`)
   - 11个迁移脚本（占位实现）
   - 001_auto_updates.go
   - 002_bypass_permissions.go
   - 003_mcp_servers.go
   - 004_fennec_to_opus.go
   - 005_legacy_opus.go
   - 006_opus_to_opus1m.go
   - 007_repl_bridge.go
   - 008_sonnet1m_to_45.go
   - 009_sonnet45_to_46.go
   - 010_reset_auto_mode.go
   - 011_reset_pro_default.go

6. **测试覆盖** (`internal/migrations/migrations_test.go`)
   - 3个测试组，100%通过
   - Registry 测试（注册、查询、待处理）
   - FileVersionManager 测试
   - Executor 测试（执行、干运行、错误处理）

7. **文档** (`internal/migrations/README.md`)
   - 完整的使用文档
   - 迁移列表和说明
   - 架构设计说明

### 架构特点
- 注册表模式（全局 DefaultRegistry）
- 接口抽象（VersionManager, AnalyticsLogger）
- 并发安全（RWMutex, Mutex）
- 幂等性保证
- 独立性（迁移之间互不依赖）

### 待完成工作
- ⏳ 实现所有 11 个迁移脚本的具体逻辑（需要 config 模块）
- ⏳ 集成到应用初始化流程
- ⏳ 添加分析事件日志

---

## 已完成：upstreamproxy 模块 ✅

### 实现内容
1. **核心类型** (`internal/upstreamproxy/types.go`)
   - State - 代理状态（enabled, port, CA bundle path）
   - Relay - 运行中的中继
   - RelayOptions - 中继启动选项
   - InitOptions - 初始化选项
   - ConnState - 连接状态追踪
   - WebSocketLike - WebSocket 接口抽象
   - 常量定义（MaxChunkBytes, PingIntervalMS, NoProxyList）

2. **Protobuf 编解码** (`internal/upstreamproxy/protobuf.go`)
   - EncodeChunk - 手工编码 UpstreamProxyChunk
   - DecodeChunk - 解码 UpstreamProxyChunk
   - encodeVarint - Varint 编码
   - decodeVarint - Varint 解码
   - 支持任意大小的数据块

3. **代理初始化** (`internal/upstreamproxy/upstreamproxy.go`)
   - InitUpstreamProxy - 完整初始化流程
   - GetProxyEnv - 生成子进程环境变量
   - IsEnabled - 检查代理状态
   - GetState - 获取当前状态
   - readToken - 读取会话 token
   - setNonDumpable - 防 ptrace（占位）
   - downloadCABundle - CA 证书下载（占位）

4. **TCP 中继** (`internal/upstreamproxy/relay.go`)
   - StartUpstreamProxyRelay - 启动 TCP 监听器
   - acceptLoop - 接受连接循环
   - handleConnection - 处理 CONNECT 请求
   - forwardToWS - 转发数据到 WebSocket（占位）
   - sendKeepalive - 发送心跳（占位）
   - cleanupConn - 清理连接状态

5. **测试覆盖** (`internal/upstreamproxy/upstreamproxy_test.go`)
   - 6 个测试函数，100% 通过
   - Protobuf 编解码测试（正常和异常情况）
   - Varint 编解码测试
   - 环境变量生成测试
   - 状态管理测试

6. **文档** (`internal/upstreamproxy/README.md`)
   - 完整的使用文档
   - 协议说明（UpstreamProxyChunk, CONNECT 隧道）
   - 环境变量说明
   - NO_PROXY 列表
   - 安全特性说明

### 架构特点
- 基于 TypeScript 实现的核心逻辑移植
- Protobuf 手工编解码（无运行时依赖）
- TCP 监听器使用标准库
- 接口抽象（WebSocketLike）便于测试
- Fail-open 错误处理（任何错误都优雅降级）
- 线程安全的状态管理

### 安全特性
- Token 文件读取后立即删除
- PR_SET_DUMPABLE 防 ptrace（待实现）
- CA 证书验证
- NO_PROXY 列表防止拦截敏感流量

### 待完成工作
- ⏳ WebSocket 客户端集成（需要第三方库）
- ⏳ CA 证书下载实现（HTTP 客户端）
- ⏳ PR_SET_DUMPABLE 实现（Linux syscall）
- ⏳ 完整的 CONNECT 隧道逻辑
- ⏳ Keepalive ping 机制

---

## 已完成：query 模块 ✅

### 实现内容
1. **核心类型** (`internal/query/types.go`)
   - QueryConfig - 查询配置快照
   - Gates - 运行时特性门控
   - Terminal - 终止原因（11种）
   - Continue - 继续原因（8种）
   - BudgetTracker - Token 预算追踪器
   - TokenBudgetDecision - 预算决策
   - CompletionEvent - 完成事件

2. **配置构建** (`internal/query/config.go`)
   - BuildQueryConfig - 从环境变量构建配置
   - getEnvBool - 布尔环境变量解析
   - 支持多种布尔值格式（1/true/yes等）

3. **Token 预算** (`internal/query/token_budget.go`)
   - CreateBudgetTracker - 创建追踪器
   - CheckTokenBudget - 检查预算决策
   - 收益递减检测（3次继续 + 小增量）
   - 90% 阈值检测
   - 完成事件生成

4. **依赖注入** (`internal/query/deps.go`)
   - QueryDeps - 依赖接口定义
   - ProductionDeps - 生产环境实现
   - 支持测试 mock 注入
   - CallModel, Microcompact, Autocompact, UUID

5. **测试覆盖** (`internal/query/query_test.go`)
   - 9 个测试函数，100% 通过
   - 配置构建测试
   - 环境变量解析测试（11种情况）
   - Token 预算测试（6种场景）
   - 依赖注入测试

6. **文档** (`internal/query/README.md`)
   - 完整的使用文档
   - Token 预算算法说明
   - 终止和继续原因列表
   - 环境变量说明
   - 架构设计说明

### 架构特点
- 不可变配置快照（查询入口处冻结）
- 显式依赖注入（便于测试）
- 分离关注点（类型、配置、预算、依赖）
- 收益递减检测算法
- 类型安全的原因枚举

### Token 预算算法
- 继续条件：非 agent、有预算、< 90%、无递减
- 递减检测：3次继续 + 连续小增量（< 500）
- 停止条件：>= 90% 或递减或 agent 或无预算

### 待完成工作
- ⏳ 实际的模型 API 调用
- ⏳ 消息压缩实现
- ⏳ UUID 生成（crypto/rand）
- ⏳ Stop hooks 集成
- ⏳ 完整的查询循环逻辑

---

## 已完成：Context 模块 ✅

### 实现内容
1. **核心数据结构** (`internal/context/types.go`)
   - SystemContext - 系统上下文（git状态、缓存破坏器）
   - UserContext - 用户上下文（CLAUDE.md、当前日期）
   - GitStatusInfo - Git仓库状态信息
   - TokenUsage - Token使用统计

2. **Git 集成** (`internal/context/git.go`)
   - GetGitStatus() - 获取Git仓库状态
   - 分支检测（当前分支、主分支）
   - Git用户信息
   - 状态输出（2000字符截断）
   - 最近5次提交
   - 线程安全缓存（sync.Once）

3. **上下文管理** (`internal/context/context.go`)
   - GetSystemContext() - 系统上下文生成
   - GetUserContext() - 用户上下文生成
   - loadClaudeMd() - CLAUDE.md文件发现和加载
   - 目录树遍历查找CLAUDE.md
   - 全局CLAUDE.md支持（~/.claude/）
   - 线程安全缓存（sync.RWMutex）
   - 系统提示词注入支持

4. **上下文窗口管理** (`internal/context/window.go`)
   - GetContextWindowForModel() - 模型上下文窗口大小
   - GetModelMaxOutputTokens() - 模型最大输出token
   - CalculateContextPercentages() - 使用率百分比计算
   - ModelSupports1M() - 1M上下文支持检测
   - Has1MContext() - [1m]后缀检测
   - 环境变量覆盖支持

5. **测试覆盖** (`internal/context/context_test.go`)
   - 上下文窗口测试（Sonnet 4.6, Opus 4.6, Haiku 4.5）
   - 最大输出token测试
   - 使用率百分比计算测试
   - 1M上下文支持测试
   - 系统提示词注入测试
   - 系统/用户上下文生成测试
   - CLAUDE.md加载测试
   - 所有测试通过 ✅

6. **文档** (`internal/context/README.md`)
   - 完整的使用示例
   - 架构说明
   - 环境变量配置
   - 模型支持矩阵
   - TypeScript迁移说明

### 架构特点
- 完整移植自TypeScript实现（src/context.ts, src/utils/context.ts）
- 线程安全的缓存机制（sync.Once, sync.RWMutex）
- Git命令执行（os/exec）
- CLAUDE.md目录树遍历
- 模型能力检测（1M上下文、最大输出token）
- 环境变量配置覆盖
- 会话级缓存（避免重复昂贵操作）

### 支持的模型
- **1M上下文**: claude-sonnet-4-6, claude-opus-4-6, [1m]后缀模型
- **最大输出token**:
  - Opus 4.6: 64k默认, 128k上限
  - Sonnet 4.6: 32k默认, 128k上限
  - Sonnet 4: 32k默认, 64k上限
  - Claude 3 Opus: 4k
  - Claude 3 Sonnet: 8k

---

## 已完成：Token Estimation 服务 ✅

### 实现内容
1. **核心类型定义** (`internal/services/tokens/types.go`)
   - TokenCounter - Token 计数接口
   - FileTypeTokenEstimator - 文件类型感知估算接口
   - 常量定义（TokenCountThinkingBudget, TokenCountMaxTokens, DefaultBytesPerToken）

2. **API 客户端扩展** (`pkg/anthropic/types.go`, `pkg/anthropic/client.go`)
   - CountTokensRequest - Token 计数请求
   - CountTokensResponse - Token 计数响应
   - Thinking - 思考配置
   - CountTokens() - Token 计数 API 方法
   - Beta header 支持（token-counting-2024-11-01）

3. **Token 计数器** (`internal/services/tokens/counter.go`)
   - Counter - Token 计数器实现
   - CountTokensWithAPI - 使用 API 计数
   - CountMessagesTokensWithAPI - 消息和工具计数
   - CountToolsTokens - 仅工具计数
   - CountTokensViaHaikuFallback - Haiku 快速回退
   - stripToolSearchFieldsFromMessages - 移除 tool search beta 字段

4. **粗略估算** (`internal/services/tokens/estimation.go`)
   - RoughTokenCountEstimation - 基于字节的快速估算
   - BytesPerTokenForFileType - 文件类型特定比率
   - RoughTokenCountEstimationForFileType - 文件感知估算
   - RoughTokenCountEstimationForMessages - 消息数组估算
   - HasThinkingBlocks - 检测思考块

5. **测试覆盖** (`internal/services/tokens/counter_test.go`)
   - 9 个测试用例，100% 通过
   - 粗略估算测试
   - 文件类型比率测试
   - 思考块检测测试
   - Tool search 字段清理测试
   - 消息估算测试

6. **文档** (`internal/services/tokens/README.md`)
   - 完整的功能说明
   - 使用示例
   - 架构说明
   - API 集成文档
   - 最佳实践

### 架构特点
- 基于 TypeScript 实现的完整移植
- 支持 API 精确计数和本地粗略估算
- 文件类型感知的 token 估算
- 思考块（thinking blocks）支持
- Tool search beta 字段自动清理
- Haiku 模型快速回退
- 线程安全的实现

### 文件类型支持
- **密集格式**（JSON, XML, YAML）: 3 字节/token
- **代码**（JS, TS, Python, Go 等）: 3 字节/token
- **标记语言**（HTML, CSS）: 3 字节/token
- **纯文本**（TXT, Markdown）: 4 字节/token
- **默认**: 4 字节/token

### API 集成
- **端点**: `POST /v1/messages/count_tokens`
- **Beta Header**: `anthropic-beta: token-counting-2024-11-01`
- **思考块支持**: 自动配置 thinking.budget_tokens
- **Tool search 兼容**: 自动移除 beta 专用字段

---

## 下一步行动

1. ~~完善 Agent 系统~~ ✅ - 工具执行集成、流式输出（已完成）
2. ~~集成 Agent 到记忆系统的自动提取~~ ✅（已完成）
3. ~~Remote 客户端模块~~ ✅ - WebSocket、消息适配、会话管理（已完成）
4. ~~schemas 模块~~ ✅ - 配置验证系统（已完成）
5. ~~migrations 模块~~ ✅ - 配置迁移系统（已完成）
6. ~~entrypoints 模块~~ ✅ - 初始化逻辑（已完成）
7. ~~upstreamproxy 模块~~ ✅ - 代理配置系统（已完成）
8. ~~query 模块~~ ✅ - 查询管道配置（已完成）
9. ~~context 模块~~ ✅ - 上下文管理系统（已完成）
10. ~~Token Estimation 服务~~ ✅ - Token 计数和估算（已完成）
11. ~~native-ts 模块~~ ⏸️ - 暂不重构（见下方说明）
12. 继续重构 services 层其他模块
13. 编写集成测试
14. 更新文档

## 暂不处理的功能

- **native-ts 模块**：纯 TypeScript 实现的原生模块替代
  - 包含：color-diff（颜色差异计算）、yoga-layout（Flexbox 布局引擎）、file-index（模糊文件搜索）
  - 原因：这些是前端 UI 专用模块，用于终端界面渲染和交互，后端 Go 服务不需要这些功能
  - 评估结果：保留在 TypeScript，无需重构到 Go

- **Slash 命令系统高级功能**：Fork 执行、Effort 系统、Hooks 集成、插件命令
  - 原因：基础功能已满足需求，高级功能优先级低
  
- **LSP 完整集成**：真正的 LSP 客户端、服务器生命周期管理、多服务器路由
  - 原因：基础符号扫描已够用，完整实现工作量大

- **Utils 工具库**：暂时保留在 TypeScript，后续根据需要逐步重构
  - 原因：规模巨大（18万行代码），许多工具与前端 UI 紧密耦合，需要分阶段评估和迁移

---

生成时间：2026-04-06

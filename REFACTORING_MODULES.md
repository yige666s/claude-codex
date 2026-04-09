# Go 重构不完整模块分析报告

> 生成时间: 2026-04-08  
> 最后更新: 2026-04-09 (全量扫描 — TS 1,359 文件 vs Go 293 文件)  
> 对比源: TypeScript `/Users/ding/projectSrc/claude-code/src` vs Go `/Users/ding/projectSrc/claude-code/claude-go/internal`

## 执行摘要

**文件覆盖率**: **约 22%**（293 Go 文件 / 1,359 TS 文件）  
**核心引擎完整度**: **约 90%**（query/queryengine/harness 核心功能）  
**整体功能完整度**: **约 35-40%**（含 UI、工具安全、多 Agent 等大模块缺失）

---

## 模块状态总览

| 模块 | 完整性 | 优先级 | 状态 |
|------|--------|--------|------|
| Query/QueryEngine | 100% | - | ✅ 完成 |
| Hooks | 100% | - | ✅ 完成 |
| State/Session | 97% | P3 | ✅ 基本完整 |
| Context 收集 | 92% | P3 | ✅ 基本完整 |
| Agent/Coordinator | 85% | P2 | ⚠️ 核心完成，autoDream/forkSubagent 待实现 |
| Memory 系统 | 75% | P2 | ⚠️ autoDream/teamMemorySync 待实现 |
| Tools 工具集 | 78% | P2 | ⚠️ 9 个工具未实现 |
| BashTool 安全层 | 65% | P2 | ⚠️ security.go(23个检查)/readonly.go/path_validation.go 完成；bash解析器/AST层待实现 |
| AgentTool 完整实现 | 10% | P1 | ⚠️ stub 保留，Runner 注入模式可扩展；forkSubagent/resumeAgent/loadAgentsDir 缺失 |
| 权限系统 (utils/permissions) | 70% | P2 | ⚠️ types/rule_parser/shell_matching/dangerous_patterns/auto_mode 完成；classifier 待实现 |
| CLI 命令实现 | 20% | P2 | ❌ 94 个命令目录未单独移植，transports/ 完全缺失 |
| Bridge 协议 | 5% | P2 | ❌ 32 TS 文件 vs 1 Go 文件 |
| MCP 服务 | 25% | P2 | ❌ OAuth/channel权限/elicitation 缺失 |
| Analytics | 10% | P3 | ❌ Datadog/GrowthBook/firstPartyEventLogger 缺失 |
| Plugins 系统 | 5% | P3 | ❌ 43 TS 文件 vs 2 Go 文件 |
| Settings/Config | 15% | P2 | ❌ MDM/changeDetector/toolValidation 缺失 |
| UI 组件 (components/) | 2% | P3 | ❌ 389 TSX 文件完全未移植 |
| Swarm 多 Agent | 60% | P1 | ⚠️ types/constants/team_file/inprocess_backend/permission_sync 完成；Tmux/iTerm2/leaderBridge 跳过 |
| Bash 解析器 (utils/bash) | 20% | P1 | ⚠️ splitSubcommands/splitCommandTokens/validateOutputRedirections 已在 bash 包实现；AST/heredoc/tree-sitter 待实现 |
| Shell 工具 (utils/shell) | 0% | P2 | ❌ 10 TS 文件完全缺失 |
| Telemetry | 0% | P3 | ❌ 9 TS 文件完全缺失 |
| SecureStorage (Keychain) | 0% | P2 | ❌ 6 TS 文件完全缺失 |
| DeepLink 协议 | 0% | P3 | ❌ 6 TS 文件完全缺失 |
| ComputerUse | 0% | P3 | ❌ 13 TS 文件完全缺失 |
| x402 支付协议 | 0% | P3 | ❌ 6 TS 文件完全缺失 |
| Services (autoDream/vcr/tips 等) | 0-10% | P2-P3 | ❌ 多个服务完全缺失 |
| Vim/Voice/Buddy | 0% | P3 | ❌ 完全缺失（非核心功能） |

---

## 优先级 P1 — 影响核心安全与功能正确性

### 1. BashTool 安全层 ⚠️

**TS 代码量**: ~10,909 行（15 个文件）  
**Go 代码量**: ~800 行（4 个文件）  
**完整性**: **65%**

**已实现**：
- ✅ `security.go` — 23 个安全检查器（`BashCommandIsSafe`），misparsing gate，控制字符/heredoc/backtick/Unicode空白/brace expansion 等
- ✅ `readonly.go` — `IsCommandReadOnly()`，flag-based allowlist + 正则匹配，git/grep/find/sed 等只读命令
- ✅ `path_validation.go` — `CheckPathConstraints()`，路径提取/验证，危险路径检测，输出重定向验证

**待实现**：
- ❌ `bashPermissions.ts` (2,622 行) — 完整权限编排逻辑（subcommand分割、规则匹配、`bashToolHasPermission`）
- ❌ Bash AST 解析层（依赖 `utils/bash/` tree-sitter 模块）
- ❌ Sandbox auto-allow 逻辑
- ❌ LLM classifier 集成（`classifyBashCommand`）

---

### 2. 权限系统 (utils/permissions/) ⚠️

**TS 代码量**: 24 个文件  
**Go 代码量**: ~700 行（6 个文件）  
**完整性**: **70%**

**已实现**：
- ✅ `types.go` — `PermissionResult`/`Rule`/`ToolContext`/`PermissionUpdate`/`ApplyUpdate()` 完整类型体系
- ✅ `rule_parser.go` — `RuleValueFromString()`/`RuleValueToString()`，转义/反转义，legacy tool name aliases
- ✅ `shell_matching.go` — `ParseShellPermissionRule()`，`MatchWildcardPattern()` glob，`MatchesRule()` compound command guard
- ✅ `dangerous_patterns.go` — `DangerousBashPatterns`，`SafeYoloAllowlistedTools`，`IsDangerousBashPermission()`
- ✅ `auto_mode.go` — auto mode state，`DenialTrackingState`，circuit breaker

**待实现**：
- ❌ `yoloClassifier.ts` — YOLO 两阶段 LLM 分类器（依赖 Anthropic API 调用）
- ❌ `shadowedRuleDetection.ts` — 被遮蔽规则检测（`detectUnreachableRules`）
- ❌ `getNextPermissionMode.ts` — 权限模式切换逻辑
- ❌ `bashClassifier.ts` — bash prompt 规则分类（外部 API stub）

---

### 3. Swarm 多 Agent 系统 (utils/swarm/) ⚠️

**TS 代码量**: 21 个文件  
**Go 代码量**: 5 个文件（types/constants/team_file/inprocess_backend/permission_sync）  
**完整性**: **60%**

**已实现**：
- ✅ `types.go` — 完整类型系统（BackendType/AgentID/TeammateExecutor/TeamFile/SwarmPermissionRequest 等）
- ✅ `constants.go` — TeamLeadName/InProcessMarker/环境变量常量
- ✅ `team_file.go` — 团队配置文件 CRUD（原子写入、成员管理）
- ✅ `inprocess_backend.go` — 进程内后端（TeammateExecutor 实现，goroutine 管理）
- ✅ `permission_sync.go` — 文件级邮箱系统（drain 语义）+ 权限请求同步

**跳过（平台相关）**：
- ⏭️ `TmuxBackend.ts` — Tmux 终端复用后端（macOS/Linux 平台专属）
- ⏭️ `ITermBackend.ts` / `PaneBackendExecutor.ts` — iTerm2 集成（macOS 专属）

**待实现**：
- ❌ `leaderPermissionBridge.ts` — Leader-Worker 权限桥（依赖 queryengine）
- ❌ `reconnection.ts` — 重连逻辑
- ❌ `teammateInit.ts` / `teammateLayoutManager.ts` — 团队成员布局管理（依赖 UI）

---

### 4. Bash 解析器 (utils/bash/) ⚠️

**TS 代码量**: 23 个文件  
**Go 代码量**: 已在 `tools/bash/` 包实现核心子集  
**完整性**: **20%**

**已实现（在 tools/bash 包内）**：
- ✅ `splitSubcommands()` — 复合命令拆分（respecting quotes）
- ✅ `splitCommandTokens()` — 命令 token 分割
- ✅ `validateOutputRedirections()` — 输出重定向路径验证
- ✅ `stripSafeWrappersForPath()` — 剥离 sudo/env 包装

**待实现**：
- ❌ `bashParser.ts` — 完整 Bash 语法解析（AST 级）
- ❌ `heredoc.ts` — Heredoc 提取/恢复
- ❌ `shellCompletion.ts` — Shell 补全
- ❌ `ShellSnapshot.ts` — Shell 状态快照
- ❌ `treeSitterAnalysis.ts` — Tree-sitter 分析（依赖 CGO/外部库）

---

### 5. AgentTool 完整实现 ⚠️

**TS 代码量**: ~3,816 行（18 个文件）  
**Go 代码量**: 89 行（1 个文件，Runner 注入模式，可扩展）  
**完整性**: **10%**

**已实现**：
- ✅ `agent.go` — 基础工具框架（Runner 函数注入，同步执行路径，context 超时）

**待实现（无 tool/util 以外模块依赖的）**：
- ❌ `filterIncompleteToolCalls()` — 过滤不完整工具调用（纯逻辑，无外部依赖）
- ❌ 完整 input schema（description/subagent_type/model/run_in_background/isolation）

**待实现（依赖其他模块，可延后）**：
- ❌ `runAgent.ts` (974 行) — Agent 运行主逻辑（依赖 queryengine）
- ❌ `loadAgentsDir.ts` — Agent 定义加载（Go 已有 `harness/agent/load_agents_dir.go` 但未接入 tool）
- ❌ `agentToolUtils.ts` (687 行) — 异步任务跟踪/进度/finalizeAgentTool
- ❌ `resumeAgent.ts` — Agent 恢复执行（依赖 state/sessionStorage）
- ❌ `forkSubagent.ts` — Fork 子 Agent（依赖 queryengine）
- ❌ `builtin-agents/` 目录 — 内置 Agent 定义文件

---

## 优先级 P2 — 影响主要功能

### 6. CLI 命令实现 ❌

**TS 代码量**: 112 个文件，94 个命令子目录  
**Go 覆盖**: 通用 dispatch，无独立命令实现  
**完整性**: **20%**

**完全缺失的命令实现**：
`clear`, `compact`, `config`, `mcp`, `permissions`, `hooks`, `login`, `logout`, `model`, `upgrade`, `doctor`, `skills`, `resume`, `review` 以及约 80 个其他命令目录

**CLI Transports 完全缺失** (6 TS 文件):
- `HybridTransport.ts`, `SSETransport.ts`, `WebSocketTransport.ts`
- `SerialBatchEventUploader.ts`, `WorkerStateUploader.ts`, `ccrClient.ts`

---

### 7. Bridge 协议 ❌

**TS 代码量**: 32 个文件  
**Go 代码量**: 1 个文件 (`backend/bridge/server.go`)  
**完整性**: **5%**

**缺失内容**：
- `replBridge.ts` / `replBridgeHandle.ts` / `replBridgeTransport.ts` — REPL 桥接协议
- `bridgeMessaging.ts` / `bridgeMain.ts` / `bridgeApi.ts` — Bridge API
- `remoteBridgeCore.ts` / `sessionRunner.ts` — 远程会话管理
- `inboundMessages.ts` / `inboundAttachments.ts` — 消息处理
- `jwtUtils.ts` / `trustedDevice.ts` / `workSecret.ts` — 认证辅助

---

### 8. Settings/Config 系统 ❌

**TS 代码量**: 19 个文件（`settings.ts` 1,016 行 + `types.ts` 1,149 行）  
**Go 代码量**: 1 个文件（`app/config/config.go` 509 行）  
**完整性**: **15%**

**缺失内容**：
- MDM 企业策略设置
- `changeDetector.ts` — 配置变更检测
- `schemaOutput.ts` — Schema 输出
- `pluginOnlyPolicy.ts` — 插件策略
- `toolValidationConfig.ts` — 工具验证配置
- 远程托管设置 (`services/remoteManagedSettings/` 4 个文件)
- 设置同步 (`services/settingsSync/` 2 个文件)

---

### 9. MCP 服务 ❌

**TS 代码量**: 22 个文件  
**Go 代码量**: 4 个文件  
**完整性**: **25%**

**缺失内容**：
- `InProcessTransport.ts` — 进程内传输层
- `SdkControlTransport.ts` — SDK 控制传输
- Channel allowlist / notification / permissions 管理
- Elicitation handler（用户数据请求处理）
- OAuth 端口（`oauthPort.ts`）
- 官方注册中心集成（`officialRegistry.ts`）

---

### 10. SecureStorage (macOS Keychain) ❌

**TS 代码量**: 6 个文件  
**Go 代码量**: 0  
**完整性**: **0%**

**缺失内容**：
- `macOsKeychainStorage.ts` — macOS Keychain 存储
- `macOsKeychainHelpers.ts` — Keychain 辅助函数
- `keychainPrefetch.ts` — Keychain 预取
- `fallbackStorage.ts` — 降级存储策略

---

### 11. Services 缺失模块

**完全缺失** (0% 完整性):

| 服务 | TS 文件数 | 说明 |
|------|-----------|------|
| `services/autoDream/` | 4 | 周期性记忆整合（DreamTask 325 行） |
| `services/AgentSummary/` | 1 | Agent 摘要生成 |
| `services/MagicDocs/` | 2 | 文档生成服务 |
| `services/PromptSuggestion/` | 2 | 提示词建议 |
| `services/tips/` | 3 | 提示历史/注册/调度器 |
| `services/policyLimits/` | 2 (664行) | 策略限制管理 |
| `services/vcr.ts` | 1 (407行) | VCR 录制回放系统 |
| `services/voice*.ts` | 3 | 语音输入/STT |
| `services/notifier.ts` | 1 (157行) | 通知服务 |
| `services/diagnosticTracking.ts` | 1 | 诊断追踪 |
| `services/internalLogging.ts` | 1 | 内部日志 |
| `services/x402/` | 6 | x402 支付协议 |
| `services/claudeAiLimits.ts` | 2 | Claude.ai 限制管理 |
| `services/preventSleep.ts` | 1 | 系统防睡眠 |

---

### 12. Agent/Coordinator 剩余功能 ⚠️

**完整性**: **85%**

**已实现**: 常量/工具过滤/生命周期/内置Agent定义/Coordinator系统提示/SendMessage/文件加载

**缺失功能**：
- `autoDream` 记忆整合接入（依赖 `services/autoDream/`）
- `forkSubagent` 完整实现（AgentTool 层面）
- `resumeAgent` 恢复执行（AgentTool 层面）
- `extractMemories` 工具权限接入（`CreateAutoMemCanUseTool` 已实现但未接入）

---

### 13. Memory 系统 ⚠️

**完整性**: **75%**

**缺失功能**：
- `autoDream` 记忆整合（依赖 Agent/Coordinator）
- `teamMemorySync` 团队记忆 API 推拉同步（依赖 OAuth + Anthropic API）
- `teamMemorySync/secretScanner.ts` / `teamMemSecretGuard.ts` / `watcher.ts`

---

## 优先级 P3 — 非核心功能

### 14. UI 组件 (src/components/) ❌

**TS 代码量**: 389 个 TSX 文件  
**Go 代码量**: 8 个文件（最小 TUI）  
**完整性**: **2%**

完全未移植的 UI 区域：
- 所有工具类型的权限对话框（BashPermissionRequest 等）
- 沙箱配置 UI（5 个组件）
- Dream/Shell/InProcessTeammate/RemoteSession 任务进度对话框
- Teams 对话框、用户反馈调查（7 个组件）
- 上下文可视化、压缩摘要、工作量指示器
- 认证流程（ConsoleOAuthFlow, ApproveApiKey, AwsAuthStatusBox）
- 50+ 其他交互组件

---

### 15. Telemetry ❌

**TS 代码量**: 9 个文件  
**Go 代码量**: 0  
**完整性**: **0%**

**缺失**: betaSessionTracing, BigQuery 导出器, Perfetto 追踪, 会话追踪, 插件遥测

---

### 16. 其他完全缺失模块

| 模块 | TS 文件数 | 说明 |
|------|-----------|------|
| `utils/computerUse/` | 13 | 电脑控制子系统 |
| `utils/deepLink/` | 6 | 深链接/协议处理 |
| `utils/claudeInChrome/` | 6 | Chrome 原生宿主 |
| `utils/nativeInstaller/` | 5 | 包管理器集成 |
| `utils/teleport/` | 4 | Teleport 支持 |
| `utils/ultraplan/` | 2 | ultraplan 关键词/会话 |
| `utils/dxt/` | 2 | DXT 格式支持 |
| `utils/github/` | 1 | GitHub 集成辅助 |
| `utils/sandbox/` | 2 | 沙箱 UI 适配器 |
| `utils/powershell/` | 3 | PowerShell 支持 |
| `utils/background/` | 2 | 后台处理 |
| `vim/` | 5 | Vim 模式（motions/operators/textObjects） |
| `buddy/` | 4 | Buddy 伴侣（非核心） |

---

## 基本完整模块（已跟踪）

| 模块 | 完整性 | 状态 |
|------|--------|------|
| Query/QueryEngine | 100% | ✅ Phase 1-5 完成 |
| Hooks | 100% | ✅ Phase 4 完成，63 个测试通过 |
| State/Session | 97% | ✅ 压缩+SessionMemory+Query集成完成 |
| Context 收集 | 92% | ✅ MCP上下文注入，Scratchpad 待完成 |
| upstreamproxy | 95% | ✅ protobuf/relay/types/upstreamproxy 均已移植 |
| compact | 90% | ✅ autocompact/compact/grouping/microcompact/prompt/types 均已移植 |
| oauth | 90% | ✅ 5 个文件均已移植 |

---

**文档版本**: 2.0  
**最后更新**: 2026-04-09（全量扫描版本）

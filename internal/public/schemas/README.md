# Schemas 模块

配置验证和模式定义模块，提供类型安全的配置管理。

## 功能特性

- ✅ 配置验证（Permissions, Hooks, MCP, Sandbox, Keybindings）
- ✅ 权限规则验证（括号匹配、工具名大小写、MCP/Bash 模式）
- ✅ Hook 配置验证（4种类型：Bash, Prompt, HTTP, Agent）
- ✅ MCP 服务器配置验证（5种传输类型）
- ✅ Marketplace 安全验证（官方名称保护、同形异义攻击防护）
- ✅ Sandbox 配置验证（网络、文件系统）
- ✅ Keybinding 配置验证（重复检测）

## 核心类型

### Settings
根配置结构，包含所有配置项：
```go
type Settings struct {
    Permissions      *Permissions
    Hooks            map[HookEvent][]HookMatcher
    MCPServers       map[string]MCPServerConfig
    Env              EnvironmentVariables
    Marketplaces     map[string]MarketplaceSource
    Sandbox          *SandboxSettings
    Keybindings      []Keybinding
    // ...
}
```

### Permissions
权限配置：
```go
type Permissions struct {
    Allow       []PermissionRule
    Deny        []PermissionRule
    Ask         []PermissionRule
    DefaultMode PermissionMode
}
```

### Hook Types
4种钩子类型：
- `BashCommandHook` - Shell 命令执行
- `PromptHook` - LLM 提示评估
- `HTTPHook` - HTTP POST 请求
- `AgentHook` - Agent 验证

### MCP Server Configs
5种传输类型：
- `MCPStdioServerConfig` - 标准输入输出
- `MCPSSEServerConfig` - Server-Sent Events
- `MCPHTTPServerConfig` - HTTP
- `MCPWebSocketServerConfig` - WebSocket
- SDK 传输（待实现）

## 使用示例

### 验证配置

```go
import "github.com/ding/claude-code/claude-go/internal/schemas"

// 从 JSON 验证
result := schemas.ValidateSettingsJSON(jsonData)
if !result.Valid {
    fmt.Println(schemas.FormatValidationErrors(result.Errors))
}

// 直接验证对象
settings := &schemas.Settings{
    Permissions: &schemas.Permissions{
        Allow: []schemas.PermissionRule{"Read", "Write"},
        Deny:  []schemas.PermissionRule{"Bash:rm"},
    },
}
result := schemas.ValidateSettings(settings)
```

### 验证权限规则

```go
// 验证单个规则
err := schemas.ValidatePermissionRule("Read(*.go)")
if err != nil {
    fmt.Println("Invalid rule:", err)
}

// 过滤无效规则
rules := []schemas.PermissionRule{
    "Read",
    "read",  // 无效（小写）
    "Write(*.go)",
}
valid := schemas.FilterInvalidPermissionRules(rules)
// valid = ["Read", "Write(*.go)"]
```

### 验证 Hook 配置

```go
hook := &schemas.BashCommandHook{
    BaseHook: schemas.BaseHook{Type: schemas.HookTypeBash},
    Command:  "echo hello",
}

if err := hook.Validate(); err != nil {
    fmt.Println("Invalid hook:", err)
}
```

### 验证 MCP 服务器配置

```go
config := &schemas.MCPStdioServerConfig{
    Command: "node",
    Args:    []string{"server.js"},
}

if err := config.Validate(); err != nil {
    fmt.Println("Invalid config:", err)
}
```

## 权限规则语法

### 工具模式
- `ToolName` - 允许/拒绝整个工具
- `ToolName(pattern)` - 文件操作模式
- 工具名必须以大写字母开头

### Bash 模式
- `Bash` - 所有 Bash 命令
- `Bash:*` - 所有 Bash 命令（显式）
- `Bash:command` - 特定命令

### MCP 模式
- `mcp:serverName` - 整个 MCP 服务器
- `mcp:serverName:toolName` - 特定工具
- 不支持通配符

### 文件模式
- `Read(*.go)` - Glob 模式
- `Write(/path/to/file)` - 精确路径
- `Edit(**/*.ts)` - 递归模式

## 验证规则

### 权限规则验证
1. ✅ 括号匹配（支持转义）
2. ✅ 空括号检测
3. ✅ 工具名大小写
4. ✅ MCP 模式验证（无通配符）
5. ✅ Bash 模式验证
6. ✅ Glob 模式验证

### Hook 验证
1. ✅ 必填字段检查
2. ✅ URL 格式验证
3. ✅ 超时范围检查
4. ✅ 条件表达式验证

### MCP 服务器验证
1. ✅ 传输类型特定验证
2. ✅ 必填字段检查
3. ✅ URL 格式验证

### Marketplace 验证
1. ✅ 官方名称保护
2. ✅ ASCII 字符检查（防同形异义攻击）
3. ✅ 源类型特定验证

### Sandbox 验证
1. ✅ 端口范围检查
2. ✅ 冲突规则检测

### Keybinding 验证
1. ✅ 必填字段检查
2. ✅ 重复绑定检测

## 错误处理

### ValidationError
```go
type ValidationError struct {
    Path       string  // 错误路径（如 "permissions.allow[0]"）
    Message    string  // 错误消息
    Suggestion string  // 修复建议（可选）
    DocLink    string  // 文档链接（可选）
}
```

### ValidationResult
```go
type ValidationResult struct {
    Valid  bool
    Errors []ValidationError
}
```

### 格式化错误
```go
errors := result.Errors
formatted := schemas.FormatValidationErrors(errors)
fmt.Println(formatted)
// 输出：
// Found 2 validation error(s):
//
// 1. permissions.allow[0]: tool name should start with uppercase letter: read
//
// 2. hooks.PreToolUse[0]: command is required for bash hooks
```

## 测试

运行测试：
```bash
go test ./internal/schemas/... -v
```

测试覆盖率：
```bash
go test ./internal/schemas/... -cover
```

## 架构设计

### 类型系统
- 使用 Go interface 实现多态（Hook, MCPServerConfig）
- 使用 struct embedding 共享通用字段（BaseHook）
- 使用 type alias 提供语义化类型（PermissionRule）

### 验证策略
- 分层验证：先验证结构，再验证语义
- 累积错误：收集所有错误而不是快速失败
- 上下文路径：提供精确的错误位置

### 扩展性
- 新增 Hook 类型：实现 Hook interface
- 新增 MCP 传输：实现 MCPServerConfig interface
- 新增验证规则：添加自定义验证函数

## 与 TypeScript 版本的差异

### 优势
1. ✅ 编译时类型检查
2. ✅ 更好的性能
3. ✅ 更清晰的错误处理
4. ✅ 无运行时依赖

### 差异
1. Zod 的 discriminated unions → Go interface + type switch
2. Zod 的 refinements → 自定义验证函数
3. Zod 的 transforms → 单独的转换函数
4. 运行时验证 → 编译时 + 运行时混合

## 未来改进

- [ ] 添加更多 MCP 传输类型支持
- [ ] 实现配置序列化/反序列化
- [ ] 添加配置合并逻辑
- [ ] 实现配置热重载
- [ ] 添加更详细的错误建议
- [ ] 支持配置模板和继承

## 相关模块

- `internal/config` - 配置加载和管理
- `internal/migrations` - 配置迁移
- `internal/permissions` - 权限检查

## 参考

- TypeScript 实现：`src/schemas/`, `src/utils/settings/`
- 设计文档：`MODULES_REFACTOR_PLAN.md`

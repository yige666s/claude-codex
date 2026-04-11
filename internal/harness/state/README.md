# State 模块

State 模块实现了 Claude Code 后端的全局状态管理系统，负责管理应用的运行时状态。

## 功能特性

- **轻量级状态管理**：简单的发布-订阅模式，无需 Redux
- **线程安全**：使用 sync.RWMutex 保护并发访问
- **类型安全**：完整的 Go 类型定义
- **订阅机制**：支持状态变更监听
- **后端专注**：只包含后端逻辑相关的状态

## 架构组件

### 1. 应用状态 (`app_state.go`)

定义了后端应用的核心状态：

```go
type AppState struct {
    // 任务管理
    Tasks            map[string]tasks.TaskState
    AgentNameRegistry map[string]string
    
    // 权限系统
    ToolPermissionContext ToolPermissionContext
    
    // Agent 系统
    AgentDefinitions AgentDefinitionsResult
    
    // 文件历史
    FileHistory FileHistoryState
    
    // MCP 集成
    MCP MCPState
    
    // 插件系统
    Plugins PluginState
    
    // 通知队列
    Notifications NotificationState
    
    // 会话钩子
    SessionHooks map[string]SessionHook
    
    // 配置
    Settings map[string]interface{}
    Verbose  bool
    
    // 模型配置
    MainLoopModel *string
    
    // 远程连接
    RemoteSessionURL *string
    RemoteConnectionStatus string
}
```

### 2. Store (`store.go`)

状态管理器，提供订阅和更新机制：

```go
type Store struct {
    state     *AppState
    listeners map[int]Listener
    onChange  OnChange
    mu        sync.RWMutex
}

// 创建 Store
store := state.NewStore(initialState, onChange)

// 获取状态
currentState := store.GetState()

// 更新状态
store.SetState(func(prev *AppState) *AppState {
    newState := *prev
    newState.Verbose = true
    return &newState
})

// 订阅状态变更
unsubscribe := store.Subscribe(func() {
    fmt.Println("State changed!")
})
defer unsubscribe()
```

### 3. 辅助方法 (`helpers.go`)

提供便捷的状态访问和修改方法。

**重要：** 这些方法不包含锁定逻辑。它们应该：
1. 通过 `Store.SetState()` 使用（Store 在外层处理锁定）
2. 或在单线程上下文中直接使用（如测试）

```go
// 任务管理
state.AddTask(taskID, task)
task, ok := state.GetTask(taskID)
state.RemoveTask(taskID)

// Agent 注册
state.RegisterAgentName("my-agent", "agent-123")
agentID, ok := state.GetAgentIDByName("my-agent")

// 通知管理
state.AddNotification(notification)
current := state.GetCurrentNotification()

// 文件历史
state.AddFileSnapshot(snapshot)
state.TrackFile("/path/to/file")
tracked := state.IsFileTracked("/path/to/file")

// MCP 工具
state.AddMCPTool(tool)
tools := state.GetMCPTools()

// 插件管理
state.AddPlugin(plugin)
plugins := state.GetEnabledPlugins()

// 权限模式
state.SetPermissionMode("plan")
mode := state.GetPermissionMode()

// 详细模式
state.SetVerbose(true)
verbose := state.IsVerbose()

// 认证版本
state.IncrementAuthVersion()
version := state.GetAuthVersion()
```

## 使用示例

### 基本状态管理

```go
package main

import (
    "fmt"
    "claude-codex/internal/state"
)

func main() {
    // 创建初始状态
    initialState := state.NewAppState()
    
    // 创建 Store
    store := state.NewStore(initialState, func(newState, oldState *state.AppState) {
        fmt.Printf("State changed: verbose %v -> %v\n", 
            oldState.Verbose, newState.Verbose)
    })
    
    // 获取当前状态
    currentState := store.GetState()
    fmt.Printf("Verbose: %v\n", currentState.Verbose)
    
    // 更新状态
    store.SetState(func(prev *state.AppState) *state.AppState {
        newState := *prev
        newState.Verbose = true
        return &newState
    })
    
    // 验证更新
    updatedState := store.GetState()
    fmt.Printf("Verbose after update: %v\n", updatedState.Verbose)
}
```

### 订阅状态变更

```go
package main

import (
    "fmt"
    "claude-codex/internal/state"
)

func main() {
    store := state.NewStore(nil, nil)
    
    // 订阅状态变更
    unsubscribe := store.Subscribe(func() {
        currentState := store.GetState()
        fmt.Printf("State updated! Verbose: %v\n", currentState.Verbose)
    })
    defer unsubscribe()
    
    // 触发状态变更
    store.SetState(func(prev *state.AppState) *state.AppState {
        newState := *prev
        newState.Verbose = true
        return &newState
    })
    
    // 再次更新
    store.SetState(func(prev *state.AppState) *state.AppState {
        newState := *prev
        newState.FastMode = true
        return &newState
    })
}
```

### 任务管理

```go
package main

import (
    "fmt"
    "claude-codex/internal/state"
    "claude-codex/internal/tasks"
)

func main() {
    appState := state.NewAppState()
    
    // 创建任务
    taskID := "task-123"
    taskState := &tasks.LocalShellTaskState{
        TaskStateBase: tasks.TaskStateBase{
            ID:          taskID,
            Type:        tasks.TaskTypeLocalBash,
            Status:      tasks.TaskStatusRunning,
            Description: "ls -la",
        },
        Command: "ls -la",
    }
    
    // 添加任务
    appState.Tasks[taskID] = taskState
    
    // 获取任务
    if task, ok := appState.GetTask(taskID); ok {
        fmt.Printf("Task found: %v\n", task)
    }
    
    // 删除任务
    appState.RemoveTask(taskID)
}
```

### Agent 名称注册

```go
package main

import (
    "fmt"
    "claude-codex/internal/state"
)

func main() {
    appState := state.NewAppState()
    
    // 注册 Agent 名称
    appState.RegisterAgentName("code-reviewer", "agent-001")
    appState.RegisterAgentName("test-runner", "agent-002")
    
    // 通过名称查找 Agent ID
    if agentID, ok := appState.GetAgentIDByName("code-reviewer"); ok {
        fmt.Printf("Code reviewer agent ID: %s\n", agentID)
    }
    
    // 查找不存在的 Agent
    if _, ok := appState.GetAgentIDByName("non-existent"); !ok {
        fmt.Println("Agent not found")
    }
}
```

### 文件历史追踪

```go
package main

import (
    "fmt"
    "time"
    "claude-codex/internal/state"
)

func main() {
    appState := state.NewAppState()
    
    // 追踪文件
    appState.TrackFile("/src/main.go")
    appState.TrackFile("/src/utils.go")
    
    // 检查文件是否被追踪
    if appState.IsFileTracked("/src/main.go") {
        fmt.Println("main.go is tracked")
    }
    
    // 添加文件快照
    snapshot := state.FileSnapshot{
        Path:      "/src/main.go",
        Content:   "package main\n\nfunc main() {}",
        Timestamp: time.Now().UnixMilli(),
        Sequence:  1,
    }
    
    appState.AddFileSnapshot(snapshot)
    
    fmt.Printf("Total snapshots: %d\n", len(appState.FileHistory.Snapshots))
    fmt.Printf("Snapshot sequence: %d\n", appState.FileHistory.SnapshotSequence)
}
```

### 通知管理

```go
package main

import (
    "fmt"
    "time"
    "claude-codex/internal/state"
)

func main() {
    appState := state.NewAppState()
    
    // 创建通知
    notification := state.Notification{
        ID:        "notif-001",
        Type:      "info",
        Message:   "Task completed successfully",
        Timestamp: time.Now().UnixMilli(),
    }
    
    // 添加到队列
    appState.AddNotification(notification)
    
    // 设置为当前通知
    appState.SetCurrentNotification(&notification)
    
    // 获取当前通知
    if current := appState.GetCurrentNotification(); current != nil {
        fmt.Printf("Current notification: %s\n", current.Message)
    }
    
    fmt.Printf("Notification queue length: %d\n", len(appState.Notifications.Queue))
}
```

### MCP 工具管理

```go
package main

import (
    "fmt"
    "claude-codex/internal/state"
)

func main() {
    appState := state.NewAppState()
    
    // 添加 MCP 工具
    tool := state.MCPTool{
        Name:        "file-reader",
        Description: "Read file contents",
        Schema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "path": map[string]interface{}{
                    "type": "string",
                },
            },
        },
    }
    
    appState.AddMCPTool(tool)
    
    // 获取所有工具
    tools := appState.GetMCPTools()
    fmt.Printf("Total MCP tools: %d\n", len(tools))
    
    for _, t := range tools {
        fmt.Printf("Tool: %s - %s\n", t.Name, t.Description)
    }
}
```

### 权限模式管理

```go
package main

import (
    "fmt"
    "claude-codex/internal/state"
)

func main() {
    appState := state.NewAppState()
    
    // 获取默认权限模式
    fmt.Printf("Default mode: %s\n", appState.GetPermissionMode())
    
    // 设置权限模式
    appState.SetPermissionMode("plan")
    fmt.Printf("Updated mode: %s\n", appState.GetPermissionMode())
    
    // 设置为其他模式
    appState.SetPermissionMode("allow")
    fmt.Printf("Final mode: %s\n", appState.GetPermissionMode())
}
```

## 状态类型

### ToolPermissionContext

```go
type ToolPermissionContext struct {
    Mode                            string
    IsBypassPermissionsModeAvailable bool
    AllowedTools                    []string
    DeniedTools                     []string
}
```

### FileHistoryState

```go
type FileHistoryState struct {
    Snapshots        []FileSnapshot
    TrackedFiles     map[string]bool
    SnapshotSequence int
}

type FileSnapshot struct {
    Path      string
    Content   string
    Timestamp int64
    Sequence  int
}
```

### MCPState

```go
type MCPState struct {
    Clients             []MCPServerConnection
    Tools               []MCPTool
    Commands            []MCPCommand
    Resources           map[string][]ServerResource
    PluginReconnectKey  int
}
```

### PluginState

```go
type PluginState struct {
    Enabled            []LoadedPlugin
    Disabled           []LoadedPlugin
    Commands           []PluginCommand
    Errors             []PluginError
    InstallationStatus InstallationStatus
    NeedsRefresh       bool
}
```

### NotificationState

```go
type NotificationState struct {
    Current *Notification
    Queue   []Notification
}

type Notification struct {
    ID        string
    Type      string
    Message   string
    Timestamp int64
}
```

## 测试

运行测试：

```bash
go test ./internal/state/... -v
```

测试覆盖：
- ✅ 状态初始化
- ✅ Store 创建和更新
- ✅ 订阅机制
- ✅ 状态辅助方法
- ✅ 通知管理
- ✅ 文件历史
- ✅ MCP 工具
- ✅ 插件管理
- ✅ 并发访问

## 架构特点

- **轻量级**：不依赖外部状态管理库
- **线程安全**：Store 使用 RWMutex 保护并发访问
- **类型安全**：完整的 Go 类型系统
- **订阅模式**：支持状态变更监听
- **不可变性**：通过函数式更新保证状态一致性
- **后端专注**：只包含后端逻辑相关的状态
- **分层锁定**：Store 层处理并发控制，AppState 辅助方法保持简单

## 与 TypeScript 实现的对应关系

| TypeScript 文件 | Go 文件 | 说明 |
|----------------|---------|------|
| store.ts | store.go | Store 实现 |
| AppStateStore.ts | app_state.go | AppState 类型定义 |
| - | helpers.go | 状态辅助方法 |
| - | state_test.go | 测试 |

## 设计决策

### 为什么只重构后端状态？

1. **前端特定**：许多状态是 UI 特定的（expandedView, footerSelection 等）
2. **React 依赖**：TypeScript 实现使用 React hooks 和 context
3. **规模控制**：完整的 AppState 有 100+ 个字段，大部分是前端相关
4. **优先级**：Go 后端只需要核心业务逻辑状态

### 包含的后端状态

- ✅ 任务管理（Tasks）
- ✅ Agent 系统（AgentNameRegistry, AgentDefinitions）
- ✅ 权限系统（ToolPermissionContext）
- ✅ 文件历史（FileHistory）
- ✅ MCP 集成（MCP）
- ✅ 插件系统（Plugins）
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
- ❌ Tmux 面板（tungstenPanelVisible）
- ❌ WebBrowser 工具（bagelActive, bagelPanelVisible）
- ❌ 计算机使用 MCP（computerUseMcpState）

## 依赖

无外部依赖，仅使用 Go 标准库：
- `sync` - 并发控制

## 最佳实践

1. **状态更新**：始终使用 `Store.SetState()` 函数式更新以保证线程安全
2. **并发安全**：在多线程环境中，通过 Store 访问状态
3. **订阅管理**：记得调用 unsubscribe 清理监听器
4. **状态拷贝**：读取状态时注意是否需要深拷贝
5. **onChange 回调**：避免在回调中执行耗时操作
6. **辅助方法**：在单线程上下文（如测试）中可直接调用 AppState 的辅助方法，在多线程环境中应通过 Store.SetState() 使用

## 相关文档

- [Tasks 模块文档](../tasks/README.md)
- [Agent 系统文档](../agent/README.md)
- [Memory 系统文档](../memory/README.md)
- [TypeScript 原始实现](../../src/state/)

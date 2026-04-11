# Tasks 模块

Tasks 模块实现了 Claude Code 的后台任务管理系统，支持多种任务类型的执行、监控和生命周期管理。

## 功能特性

- **多任务类型支持**：7 种任务类型（Shell、Agent、Remote、Teammate、Workflow、MCP、Dream）
- **任务生命周期管理**：pending → running → completed/failed/killed
- **后台任务执行**：支持任务后台化和前台化切换
- **任务输出管理**：磁盘输出缓冲、增量读取、大小限制
- **任务停止控制**：优雅停止、强制终止、清理资源
- **任务注册表**：可扩展的任务实现注册机制

## 架构组件

### 1. 任务类型 (`types.go`)

定义了所有任务类型和状态：

```go
// 任务类型
type TaskType string

const (
    TaskTypeLocalBash          TaskType = "local_bash"
    TaskTypeLocalAgent         TaskType = "local_agent"
    TaskTypeRemoteAgent        TaskType = "remote_agent"
    TaskTypeInProcessTeammate  TaskType = "in_process_teammate"
    TaskTypeLocalWorkflow      TaskType = "local_workflow"
    TaskTypeMonitorMCP         TaskType = "monitor_mcp"
    TaskTypeDream              TaskType = "dream"
)

// 任务状态
type TaskStatus string

const (
    TaskStatusPending   TaskStatus = "pending"
    TaskStatusRunning   TaskStatus = "running"
    TaskStatusCompleted TaskStatus = "completed"
    TaskStatusFailed    TaskStatus = "failed"
    TaskStatusKilled    TaskStatus = "killed"
)
```

**任务状态类型：**

1. **LocalShellTaskState** - 本地 Shell 命令任务
   - 执行 bash 命令
   - 捕获输出和退出码
   - 支持超时控制

2. **LocalAgentTaskState** - 本地 Agent 任务
   - 运行 AI agent
   - 追踪 token 和工具使用
   - 支持进度报告

3. **RemoteAgentTaskState** - 远程 Agent 任务
   - 通过 CCR 协议执行
   - 远程会话管理

4. **InProcessTeammateTaskState** - 进程内团队成员任务
   - 团队协作任务

5. **LocalWorkflowTaskState** - 本地工作流任务
   - 工作流编排

6. **MonitorMCPTaskState** - MCP 监控任务
   - MCP 服务器监控

7. **DreamTaskState** - Dream 任务
   - 实验性任务类型

### 2. 任务 ID 生成 (`task_id.go`)

安全的任务 ID 生成：

```go
// 生成任务 ID
id, err := tasks.GenerateTaskID(tasks.TaskTypeLocalBash)
// 结果: "b3k7m9n2q" (前缀 + 8 位随机字符)

// 生成主会话任务 ID
id, err := tasks.GenerateMainSessionTaskID()
// 结果: "s4j8p1x5z" (前缀 's')
```

**安全特性：**
- 使用 crypto/rand 生成随机字节
- 36^8 ≈ 2.8 万亿组合，防止暴力攻击
- 大小写不敏感的字母表（0-9a-z）
- 类型前缀便于识别

### 3. 任务框架 (`framework.go`)

任务管理核心功能：

```go
// 创建任务管理器
manager := tasks.NewTaskManager()

// 注册任务实现
manager.Register(myTaskImpl)

// 添加任务
manager.AddTask(taskState)

// 获取任务
task, ok := manager.GetTask(taskID)

// 获取运行中的任务
running := manager.GetRunningTasks()

// 获取后台任务
background := manager.GetBackgroundTasks()

// 停止任务
err := manager.KillTask(taskID, setAppState)

// 清理终止的任务
manager.EvictTerminalTasks()
```

**TaskManager 功能：**
- 任务注册表管理
- 任务状态追踪
- 线程安全操作
- 自动清理终止任务

### 4. 任务输出 (`output.go`)

磁盘输出管理：

```go
// 获取任务输出路径
path := tasks.GetTaskOutputPath(projectTempDir, sessionID, taskID)

// 确保输出目录存在
err := tasks.EnsureOutputDir(projectTempDir, sessionID)

// 创建输出写入器
writer := tasks.NewDiskTaskOutput(path)

// 写入输出
err = writer.Write("command output\n")

// 关闭写入器
err = writer.Close()

// 读取任务输出
output, err := tasks.ReadTaskOutput(path, offset, maxBytes)

// 读取增量输出
delta, newOffset, err := tasks.GetTaskOutputDelta(path, lastOffset)

// 删除输出文件
err := tasks.EvictTaskOutput(path)
```

**输出管理特性：**
- 异步写入队列
- 5GB 大小限制
- 增量读取支持
- 符号链接支持
- 线程安全操作

### 5. 任务停止 (`stop_task.go`)

优雅停止任务：

```go
// 停止任务
context := tasks.StopTaskContext{
    GetAppState: getAppState,
    SetAppState: setAppState,
}

result, err := tasks.StopTask(taskID, context, manager)
if err != nil {
    if stopErr, ok := err.(*tasks.StopTaskError); ok {
        switch stopErr.Code {
        case tasks.StopTaskErrorNotFound:
            // 任务不存在
        case tasks.StopTaskErrorNotRunning:
            // 任务未运行
        case tasks.StopTaskErrorUnsupportedType:
            // 不支持的任务类型
        }
    }
}

fmt.Printf("Stopped task %s (type: %s)\n", result.TaskID, result.TaskType)
```

**停止流程：**
1. 验证任务存在
2. 验证任务正在运行
3. 调用任务实现的 Kill 方法
4. 标记任务为已通知
5. 发送 SDK 事件（如果需要）

## 使用示例

### 创建和管理任务

```go
package main

import (
    "fmt"
    "claude-codex/internal/tasks"
)

func main() {
    // 创建任务管理器
    manager := tasks.NewTaskManager()
    
    // 生成任务 ID
    taskID, err := tasks.GenerateTaskID(tasks.TaskTypeLocalBash)
    if err != nil {
        panic(err)
    }
    
    // 创建任务状态
    outputPath := tasks.GetTaskOutputPath("/tmp", "session123", taskID)
    baseState := tasks.CreateTaskStateBase(
        taskID,
        tasks.TaskTypeLocalBash,
        "ls -la",
        "tool_use_123",
        outputPath,
    )
    
    taskState := &tasks.LocalShellTaskState{
        TaskStateBase: baseState,
        Command: "ls -la",
        IsBackgrounded: false,
    }
    
    // 添加任务
    manager.AddTask(taskState)
    
    // 获取运行中的任务
    running := manager.GetRunningTasks()
    fmt.Printf("Running tasks: %d\n", len(running))
    
    // 获取后台任务
    background := manager.GetBackgroundTasks()
    fmt.Printf("Background tasks: %d\n", len(background))
}
```

### 任务输出管理

```go
package main

import (
    "fmt"
    "claude-codex/internal/tasks"
)

func main() {
    projectTempDir := "/tmp/claude-code"
    sessionID := "session123"
    taskID := "b3k7m9n2q"
    
    // 确保输出目录存在
    if err := tasks.EnsureOutputDir(projectTempDir, sessionID); err != nil {
        panic(err)
    }
    
    // 获取输出路径
    path := tasks.GetTaskOutputPath(projectTempDir, sessionID, taskID)
    
    // 创建输出写入器
    writer := tasks.NewDiskTaskOutput(path)
    defer writer.Close()
    
    // 写入输出
    if err := writer.Write("Starting command...\n"); err != nil {
        panic(err)
    }
    
    if err := writer.Write("Command completed\n"); err != nil {
        panic(err)
    }
    
    // 读取完整输出
    output, err := tasks.ReadTaskOutput(path, 0, 8*1024*1024)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Output: %s\n", output)
    
    // 读取增量输出
    lastOffset := 0
    delta, newOffset, err := tasks.GetTaskOutputDelta(path, lastOffset)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Delta: %s (new offset: %d)\n", delta, newOffset)
}
```

### 停止任务

```go
package main

import (
    "fmt"
    "claude-codex/internal/tasks"
)

func main() {
    manager := tasks.NewTaskManager()
    taskID := "b3k7m9n2q"
    
    // 创建停止上下文
    context := tasks.StopTaskContext{
        GetAppState: func() interface{} {
            return nil // 返回实际的 app state
        },
        SetAppState: func(updater func(prev interface{}) interface{}) {
            // 更新 app state
        },
    }
    
    // 停止任务
    result, err := tasks.StopTask(taskID, context, manager)
    if err != nil {
        if stopErr, ok := err.(*tasks.StopTaskError); ok {
            fmt.Printf("Stop error: %s (code: %s)\n", stopErr.Message, stopErr.Code)
            return
        }
        panic(err)
    }
    
    fmt.Printf("Stopped task %s\n", result.TaskID)
    fmt.Printf("Task type: %s\n", result.TaskType)
    if result.Command != nil {
        fmt.Printf("Command: %s\n", *result.Command)
    }
}
```

### 实现自定义任务类型

```go
package main

import (
    "claude-codex/internal/tasks"
)

// 自定义任务实现
type MyCustomTask struct {
    name string
}

func (t *MyCustomTask) GetName() string {
    return t.name
}

func (t *MyCustomTask) GetType() tasks.TaskType {
    return tasks.TaskTypeLocalBash // 或自定义类型
}

func (t *MyCustomTask) Kill(taskID string, setAppState tasks.SetAppState) error {
    // 实现任务停止逻辑
    setAppState(func(prev interface{}) interface{} {
        // 更新任务状态为 killed
        return prev
    })
    return nil
}

func main() {
    manager := tasks.NewTaskManager()
    
    // 注册自定义任务
    customTask := &MyCustomTask{name: "my-custom-task"}
    manager.Register(customTask)
    
    // 使用自定义任务
    // ...
}
```

## 常量配置

```go
const (
    // 轮询间隔
    PollIntervalMs = 1000 // 1 秒
    
    // 停止后显示时长
    StoppedDisplayMs = 3000 // 3 秒
    
    // Agent 任务面板保留时长
    PanelGraceMs = 30000 // 30 秒
    
    // 输出文件大小限制
    MaxTaskOutputBytes = 5 * 1024 * 1024 * 1024 // 5GB
    
    // 默认最大读取字节数
    DefaultMaxReadBytes = 8 * 1024 * 1024 // 8MB
)
```

## 任务 ID 前缀

| 任务类型 | 前缀 | 示例 |
|---------|------|------|
| local_bash | b | b3k7m9n2q |
| local_agent | a | a5j8p1x4z |
| remote_agent | r | r2n6k9m3w |
| in_process_teammate | t | t7q4j8n1p |
| local_workflow | w | w9m3k7j2x |
| monitor_mcp | m | m1p5n8k4j |
| dream | d | d6x2m9p3k |
| main_session | s | s4j8p1x5z |

## 测试

运行测试：

```bash
go test ./internal/tasks/... -v
```

测试覆盖：
- ✅ 任务 ID 生成（7 种类型 + 主会话）
- ✅ 任务状态判断（终止状态、后台任务）
- ✅ 任务注册表（注册、获取、列表）
- ✅ 任务管理器（添加、获取、删除、查询）

## 架构特点

- **类型安全**：强类型任务状态和接口
- **线程安全**：使用 sync.RWMutex 保护共享状态
- **可扩展**：注册表模式支持自定义任务类型
- **资源管理**：自动清理终止任务和输出文件
- **错误处理**：详细的错误类型和错误码
- **性能优化**：异步写入、增量读取、大小限制

## 与 TypeScript 实现的对应关系

| TypeScript 文件 | Go 文件 | 说明 |
|----------------|---------|------|
| Task.ts | types.go | 任务类型定义 |
| Task.ts | task_id.go | 任务 ID 生成 |
| tasks/types.ts | types.go | 任务状态联合类型 |
| tasks/stopTask.ts | stop_task.go | 任务停止逻辑 |
| utils/task/framework.ts | framework.go | 任务框架 |
| utils/task/diskOutput.ts | output.go | 磁盘输出管理 |

## 依赖

无外部依赖，仅使用 Go 标准库：
- `crypto/rand` - 安全随机数生成
- `sync` - 并发控制
- `os` - 文件系统操作
- `path/filepath` - 路径处理

## 最佳实践

1. **任务 ID 生成**：始终使用 `GenerateTaskID` 生成唯一 ID
2. **输出管理**：使用 `DiskTaskOutput` 进行异步写入
3. **状态更新**：通过 `SetAppState` 函数更新状态
4. **资源清理**：任务完成后调用 `EvictTerminalTasks`
5. **错误处理**：检查 `StopTaskError` 的错误码
6. **线程安全**：使用 `TaskManager` 的方法访问任务

## 待实现功能

- [ ] 完整的 AppState 集成
- [ ] SDK 事件发送
- [ ] 任务进度报告
- [ ] 任务暂停/恢复
- [ ] 任务依赖管理
- [ ] 任务优先级队列
- [ ] 任务执行历史
- [ ] 任务性能指标

## 相关文档

- [Agent 系统文档](../agent/README.md)
- [Memory 系统文档](../memory/README.md)
- [TypeScript 原始实现](../../src/tasks/)

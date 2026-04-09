# Remote 客户端模块

Remote 客户端模块提供了与 CCR (Claude Code Remote) 服务的完整集成，支持远程会话管理、WebSocket 通信和权限处理。

## 功能特性

- **WebSocket 连接管理**：自动连接、重连和心跳机制
- **SDK 消息适配**：支持 11 种 SDK 消息类型的转换
- **会话管理**：完整的会话生命周期管理
- **权限处理**：双向权限请求/响应流程
- **错误恢复**：自动重连和错误处理机制

## 架构组件

### 1. 类型定义 (`types.go`)

定义了所有 SDK 消息类型和控制消息类型：

```go
// SDK 消息类型
type SDKMessageType string

const (
    SDKMessageTypeAssistant      SDKMessageType = "assistant"
    SDKMessageTypeUser           SDKMessageType = "user"
    SDKMessageTypePartial        SDKMessageType = "partial_assistant"
    SDKMessageTypeResult         SDKMessageType = "result"
    // ... 更多类型
)

// 远程会话配置
type RemoteSessionConfig struct {
    SessionID        string
    GetAccessToken   func() string
    OrgUUID          string
    HasInitialPrompt bool
    ViewerOnly       bool
}
```

### 2. WebSocket 客户端 (`websocket.go`)

管理与 CCR 的 WebSocket 连接：

```go
ws := NewSessionsWebSocket(
    sessionID,
    orgUUID,
    getAccessToken,
    callbacks,
)

err := ws.Connect()
```

**特性：**
- 自动重连（最多 5 次尝试）
- Ping/Pong 心跳（30 秒间隔）
- 会话未找到重试（最多 3 次）
- 永久关闭码处理（4003 = 未授权）

### 3. SDK 消息适配器 (`adapter.go`)

转换 SDK 消息格式：

```go
// 转换 SDK 消息
result := ConvertSDKMessage(msg, &ConvertOptions{
    ConvertToolResults:      true,
    ConvertUserTextMessages: false,
})

// 检查会话结束
if IsSessionEndMessage(msg) {
    // 处理会话结束
}

// 创建合成消息
syntheticMsg := CreateSyntheticAssistantMessage(request, requestID)
```

### 4. 远程会话管理器 (`manager.go`)

管理远程会话的完整生命周期：

```go
// 创建会话管理器
config := CreateRemoteSessionConfig(
    sessionID,
    getAccessToken,
    orgUUID,
    false, // hasInitialPrompt
    false, // viewerOnly
)

callbacks := RemoteSessionCallbacks{
    OnMessage: func(message SDKMessage) {
        // 处理消息
    },
    OnPermissionRequest: func(request *PermissionRequestInner, requestID string) {
        // 处理权限请求
    },
    OnConnected: func() {
        // 连接成功
    },
}

manager := NewRemoteSessionManager(config, callbacks)
err := manager.Connect()
```

## 使用示例

### 基本会话连接

```go
package main

import (
    "fmt"
    "github.com/ding/claude-code/claude-go/internal/remote"
)

func main() {
    // 配置会话
    config := remote.CreateRemoteSessionConfig(
        "session-123",
        func() string { return "your-access-token" },
        "org-456",
        false,
        false,
    )

    // 设置回调
    callbacks := remote.RemoteSessionCallbacks{
        OnMessage: func(message remote.SDKMessage) {
            fmt.Printf("Received message: %s\n", message.GetType())
        },
        OnPermissionRequest: func(request *remote.PermissionRequestInner, requestID string) {
            fmt.Printf("Permission request for tool: %s\n", request.ToolName)
        },
        OnConnected: func() {
            fmt.Println("Connected to remote session")
        },
        OnDisconnected: func() {
            fmt.Println("Disconnected from remote session")
        },
        OnError: func(err error) {
            fmt.Printf("Error: %v\n", err)
        },
    }

    // 创建并连接
    manager := remote.NewRemoteSessionManager(config, callbacks)
    if err := manager.Connect(); err != nil {
        panic(err)
    }

    // 保持连接
    select {}
}
```

### 处理权限请求

```go
callbacks := remote.RemoteSessionCallbacks{
    OnPermissionRequest: func(request *remote.PermissionRequestInner, requestID string) {
        // 检查工具和输入
        if request.ToolName == "bash" {
            // 允许执行
            response := remote.RemotePermissionResponse{
                Behavior: "allow",
                UpdatedInput: request.Input,
            }
            manager.RespondToPermission(requestID, response)
        } else {
            // 拒绝执行
            response := remote.RemotePermissionResponse{
                Behavior: "deny",
                Message:  "Tool not allowed",
            }
            manager.RespondToPermission(requestID, response)
        }
    },
}
```

### 消息转换

```go
// 转换 SDK 消息
opts := &remote.ConvertOptions{
    ConvertToolResults:      true,
    ConvertUserTextMessages: true,
}

result := remote.ConvertSDKMessage(msg, opts)

switch result.Type {
case "message":
    // 处理普通消息
    fmt.Printf("Message: %v\n", result.Message)
case "stream_event":
    // 处理流式事件
    fmt.Printf("Stream event: %v\n", result.StreamEvent)
case "ignored":
    // 忽略的消息
}
```

### 会话控制

```go
// 取消当前请求
err := manager.CancelSession()

// 检查连接状态
if manager.IsConnected() {
    fmt.Println("Connected")
}

// 强制重连
manager.Reconnect()

// 断开连接
manager.Disconnect()
```

## 消息类型

### SDK 消息类型

| 类型 | 描述 |
|------|------|
| `assistant` | 完整的助手响应 |
| `user` | 用户输入 |
| `partial_assistant` | 流式助手内容 |
| `result` | 会话完成结果 |
| `system` | 系统初始化 |
| `status` | 状态更新 |
| `tool_progress` | 工具执行进度 |
| `auth_status` | 认证状态 |
| `tool_use_summary` | 工具使用摘要 |
| `rate_limit_event` | 限流事件 |
| `compact_boundary` | 对话压缩标记 |

### 控制消息类型

- **控制请求** (`control_request`)：客户端发送的控制命令
- **控制响应** (`control_response`)：服务器的确认响应
- **权限请求** (`permission`)：服务器请求工具执行权限
- **权限响应**：客户端的权限决策
- **取消请求** (`control_cancel_request`)：取消待处理的权限请求

## WebSocket 配置

```go
const (
    ReconnectDelayMS          = 2000  // 重连延迟（毫秒）
    MaxReconnectAttempts      = 5     // 最大重连次数
    PingIntervalMS            = 30000 // Ping 间隔（毫秒）
    MaxSessionNotFoundRetries = 3     // 会话未找到最大重试次数
)

// 永久关闭码
var PermanentCloseCodes = map[int]bool{
    4003: true, // 未授权
}
```

## 错误处理

```go
// WebSocket 连接错误
if err := ws.Connect(); err != nil {
    log.Printf("Connection failed: %v", err)
}

// 权限响应错误
if err := manager.RespondToPermission(requestID, response); err != nil {
    log.Printf("Permission response failed: %v", err)
}

// 会话取消错误
if err := manager.CancelSession(); err != nil {
    log.Printf("Cancel failed: %v", err)
}
```

## 测试

运行测试：

```bash
go test ./internal/remote/... -v
```

测试覆盖：
- 消息转换测试
- 会话管理测试
- WebSocket 状态测试
- 类型和常量测试
- 权限处理测试

## 线程安全

所有公共方法都是线程安全的：
- WebSocket 连接管理使用 `sync.RWMutex`
- 会话管理器使用 `sync.RWMutex` 保护内部状态
- 可以从多个 goroutine 安全调用

## 最佳实践

1. **错误处理**：始终检查返回的错误
2. **回调处理**：回调函数应该快速返回，避免阻塞
3. **资源清理**：使用完毕后调用 `Disconnect()`
4. **重连策略**：依赖自动重连机制，避免手动重连
5. **权限决策**：及时响应权限请求，避免超时

## 依赖

```go
import (
    "github.com/gorilla/websocket"
    "github.com/google/uuid"
)
```

安装依赖：

```bash
go get github.com/gorilla/websocket
go get github.com/google/uuid
```

## 相关文档

- [CCR 架构文档](../../docs/CCR_ARCHITECTURE.md)
- [Server 模块文档](../server/README.md)
- [Agent 系统文档](../agent/README.md)

# Upstreamproxy 模块

CCR 容器内的代理配置系统，提供 CONNECT-over-WebSocket 中继功能。

## 功能特性

- ✅ Protobuf 消息编解码（UpstreamProxyChunk）
- ✅ 环境变量配置（HTTPS_PROXY, SSL_CERT_FILE, NO_PROXY）
- ✅ 状态管理（enabled, port, CA bundle path）
- ✅ TCP 监听器（ephemeral port）
- ⏳ WebSocket 中继（待实现）
- ⏳ CA 证书下载（待实现）
- ⏳ PR_SET_DUMPABLE（待实现，Linux 特定）

## 核心类型

### State
代理状态：
```go
type State struct {
    Enabled      bool
    Port         int
    CABundlePath string
}
```

### Relay
运行中的中继：
```go
type Relay struct {
    Port int
    Stop func()
}
```

### RelayOptions
中继启动选项：
```go
type RelayOptions struct {
    WSUrl     string
    SessionID string
    Token     string
}
```

### InitOptions
初始化选项：
```go
type InitOptions struct {
    TokenPath    string
    SystemCAPath string
    CABundlePath string
    CCRBaseURL   string
}
```

## 使用示例

### 初始化代理

```go
import (
    "context"
    "claude-codex/internal/upstreamproxy"
)

ctx := context.Background()
opts := &upstreamproxy.InitOptions{
    TokenPath:    "/run/ccr/session_token",
    SystemCAPath: "/etc/ssl/certs/ca-certificates.crt",
    CABundlePath: "/home/user/.ccr/ca-bundle.crt",
    CCRBaseURL:   "https://api.anthropic.com",
}

state, err := upstreamproxy.InitUpstreamProxy(ctx, opts)
if err != nil {
    log.Fatalf("Failed to init proxy: %v", err)
}

if state.Enabled {
    log.Printf("Proxy enabled on port %d", state.Port)
}
```

### 获取代理环境变量

```go
// 获取子进程需要的环境变量
env := upstreamproxy.GetProxyEnv()

// 合并到现有环境
for k, v := range env {
    os.Setenv(k, v)
}

// 或者传递给子进程
cmd := exec.Command("curl", "https://example.com")
cmd.Env = append(os.Environ(), envMapToSlice(env)...)
```

### 检查代理状态

```go
if upstreamproxy.IsEnabled() {
    state := upstreamproxy.GetState()
    fmt.Printf("Proxy running on port %d\n", state.Port)
    fmt.Printf("CA bundle: %s\n", state.CABundlePath)
}
```

### Protobuf 编解码

```go
// 编码数据
data := []byte("hello world")
encoded := upstreamproxy.EncodeChunk(data)

// 解码数据
decoded := upstreamproxy.DecodeChunk(encoded)
if decoded == nil {
    log.Fatal("Malformed chunk")
}
```

## 协议说明

### UpstreamProxyChunk

Protobuf 消息格式：
```protobuf
message UpstreamProxyChunk {
    bytes data = 1;
}
```

线上格式：
- Tag: `0x0a` (field 1, wire type 2)
- Length: varint 编码
- Data: 原始字节

### CONNECT 隧道

1. 客户端发送 HTTP CONNECT 请求
2. 服务器建立 WebSocket 连接到 CCR
3. 双向转发字节流（通过 UpstreamProxyChunk）
4. 服务器返回 `200 Connection Established`
5. 开始 TLS 隧道

## 环境变量

### 输入环境变量

- `CLAUDE_CODE_REMOTE`: 是否在 CCR 容器中运行
- `CCR_UPSTREAM_PROXY_ENABLED`: 是否启用代理
- `CLAUDE_CODE_REMOTE_SESSION_ID`: CCR 会话 ID
- `ANTHROPIC_BASE_URL`: API 基础 URL

### 输出环境变量

- `HTTPS_PROXY` / `https_proxy`: 代理 URL
- `SSL_CERT_FILE`: CA 证书路径
- `NO_PROXY` / `no_proxy`: 不代理的主机列表

## NO_PROXY 列表

代理不拦截以下主机：
- Loopback: `localhost`, `127.0.0.1`, `::1`
- RFC1918: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
- IMDS: `169.254.0.0/16`
- Anthropic API: `anthropic.com`, `*.anthropic.com`
- GitHub: `github.com`, `*.github.com`, `*.githubusercontent.com`
- Package registries: `registry.npmjs.org`, `pypi.org`, `index.crates.io`, `proxy.golang.org`

## 安全特性

### PR_SET_DUMPABLE

在 Linux 上调用 `prctl(PR_SET_DUMPABLE, 0)` 防止同 UID 的 ptrace 攻击。

### Token 管理

1. 从 `/run/ccr/session_token` 读取 token
2. 启动中继后立即删除文件
3. Token 仅保留在内存中

### CA 证书

1. 下载 CCR CA 证书
2. 与系统 CA bundle 合并
3. 保存到 `~/.ccr/ca-bundle.crt`
4. 通过 `SSL_CERT_FILE` 暴露给子进程

## 测试

运行测试：
```bash
go test ./internal/upstreamproxy/... -v
```

测试覆盖率：
```bash
go test ./internal/upstreamproxy/... -cover
```

## 架构设计

### 分层结构

1. **Protobuf 层** (`protobuf.go`)
   - 手工编解码 UpstreamProxyChunk
   - Varint 编解码

2. **中继层** (`relay.go`)
   - TCP 监听器
   - CONNECT 请求解析
   - WebSocket 隧道（待实现）

3. **初始化层** (`upstreamproxy.go`)
   - 环境检查
   - Token 读取
   - CA 证书下载
   - 中继启动
   - 环境变量生成

### 错误处理

所有错误都 fail open：
- 任何步骤失败都记录警告并禁用代理
- 不影响正常会话运行
- 优雅降级

## 实现范围

### 已实现 ✅

- 核心类型定义
- Protobuf 编解码
- 环境变量管理
- 状态管理
- TCP 监听器框架
- 完整测试套件

### 待实现 ⏳

- WebSocket 客户端集成
- CA 证书下载（HTTP 客户端）
- PR_SET_DUMPABLE（Linux syscall）
- 完整的 CONNECT 隧道逻辑
- Keepalive ping 机制

### 不实现 ❌

- Bun 特定的 FFI 调用（Go 使用 syscall）
- Node.js 特定的 ws 包（Go 使用标准库或第三方 WebSocket）

## 与 TypeScript 版本的差异

### 优势

1. ✅ 类型安全的 Protobuf 编解码
2. ✅ 标准库 TCP 监听器
3. ✅ 更好的并发控制（goroutines）
4. ✅ 无运行时依赖

### 差异

1. TypeScript 使用 ws 包 → Go 需要 WebSocket 库
2. TypeScript 使用 Bun FFI → Go 使用 syscall
3. TypeScript 使用 fetch → Go 使用 net/http
4. TypeScript 事件驱动 → Go channel 驱动

## 依赖

### 标准库

- `net` - TCP 监听器
- `bufio` - CONNECT 请求解析
- `context` - 取消和超时
- `os` - 环境变量和文件操作
- `sync` - 并发控制

### 待添加

- WebSocket 库（如 `gorilla/websocket` 或 `nhooyr.io/websocket`）

## 相关模块

- `internal/entrypoints` - 初始化入口（已完成）
- `internal/config` - 配置管理（待实现）
- `internal/network` - 网络配置（待实现）

## 参考

- TypeScript 实现：`src/upstreamproxy/`
- 设计文档：`api-go/ccr/docs/plans/CCR_AUTH_DESIGN.md`
- Protobuf 规范：https://protobuf.dev/programming-guides/encoding/

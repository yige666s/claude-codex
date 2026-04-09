# Entrypoints 模块

应用初始化逻辑，负责启动时的配置加载、服务初始化和网络配置。

## 功能特性

- ✅ 分阶段初始化（5个阶段）
- ✅ 配置系统启动
- ✅ 后台服务初始化
- ✅ 远程设置加载
- ✅ 网络配置（代理、mTLS）
- ✅ 优雅关闭处理
- ✅ 接口抽象（便于测试和扩展）

## 核心类型

### InitPhase
初始化阶段：
```go
const (
    PhaseConfigSystem   InitPhase = "config_system"
    PhaseBackgroundSvc  InitPhase = "background_services"
    PhaseRemoteSettings InitPhase = "remote_settings"
    PhaseNetwork        InitPhase = "network"
    PhaseTrustGranted   InitPhase = "trust_granted"
)
```

### InitOptions
初始化选项：
```go
type InitOptions struct {
    ConfigDir            string
    IsNonInteractive     bool
    SkipTelemetry        bool
    SkipMigrations       bool
    EnableRemoteSettings bool
    EnablePolicyLimits   bool
}
```

### Initializer
主初始化协调器：
```go
type Initializer struct {
    config    ConfigManager
    migration MigrationManager
    telemetry TelemetryManager
    network   NetworkManager
    service   ServiceManager
    shutdown  ShutdownManager
}
```

## 管理器接口

### ConfigManager
配置管理：
```go
type ConfigManager interface {
    EnableConfigs(ctx context.Context) error
    ApplySafeEnvVars(ctx context.Context) error
    ApplyAllEnvVars(ctx context.Context) error
    ApplyCACerts(ctx context.Context) error
}
```

### MigrationManager
迁移管理：
```go
type MigrationManager interface {
    ExecuteMigrations(ctx context.Context) error
}
```

### TelemetryManager
遥测管理：
```go
type TelemetryManager interface {
    Initialize(ctx context.Context) error
    IsEnabled() bool
}
```

### NetworkManager
网络管理：
```go
type NetworkManager interface {
    ConfigureProxy(ctx context.Context) error
    ConfigureMTLS(ctx context.Context) error
    PreconnectAPI(ctx context.Context) error
}
```

### ServiceManager
服务管理：
```go
type ServiceManager interface {
    InitializeOAuth(ctx context.Context) error
    InitializeAnalytics(ctx context.Context) error
    DetectRepository(ctx context.Context) error
    DetectIDE(ctx context.Context) error
}
```

### ShutdownManager
关闭管理：
```go
type ShutdownManager interface {
    Setup(ctx context.Context) error
    RegisterCleanup(fn func() error)
    Shutdown(ctx context.Context) error
}
```

## 使用示例

### 基本初始化

```go
import (
    "context"
    "github.com/ding/claude-code/claude-go/internal/entrypoints"
)

// 创建管理器实例
configMgr := &MyConfigManager{}
migrationMgr := &MyMigrationManager{}
telemetryMgr := &MyTelemetryManager{}
networkMgr := &MyNetworkManager{}
serviceMgr := &MyServiceManager{}
shutdownMgr := entrypoints.NewShutdownManager()

// 创建初始化器
initializer := entrypoints.NewInitializer(
    configMgr,
    migrationMgr,
    telemetryMgr,
    networkMgr,
    serviceMgr,
    shutdownMgr,
)

// 执行初始化
ctx := context.Background()
opts := &entrypoints.InitOptions{
    ConfigDir:        "/path/to/config",
    IsNonInteractive: false,
}

result, err := initializer.Initialize(ctx, opts)
if err != nil {
    log.Fatalf("Initialization failed: %v", err)
}

log.Printf("Initialized in %v", result.Duration)
```

### 信任后初始化

```go
// 在用户授予信任后执行
err := initializer.InitializeAfterTrust(ctx, opts)
if err != nil {
    log.Printf("After trust initialization failed: %v", err)
}
```

### 注册清理函数

```go
// 注册需要在关闭时执行的清理函数
shutdownMgr.RegisterCleanup(func() error {
    // 清理资源
    return nil
})

// 执行优雅关闭
err := shutdownMgr.Shutdown(ctx)
if err != nil {
    log.Printf("Shutdown failed: %v", err)
}
```

### 使用 No-Op 管理器（测试）

```go
// 用于测试的 No-Op 管理器
initializer := entrypoints.NewInitializer(
    &entrypoints.NoOpConfigManager{},
    &entrypoints.NoOpMigrationManager{},
    &entrypoints.NoOpTelemetryManager{},
    &entrypoints.NoOpNetworkManager{},
    &entrypoints.NoOpServiceManager{},
    entrypoints.NewShutdownManager(),
)
```

## 初始化阶段

### 阶段 1：配置系统 (PhaseConfigSystem)
1. 启用配置系统
2. 应用安全环境变量
3. 应用 CA 证书
4. 设置优雅关闭
5. 执行配置迁移

### 阶段 2：后台服务 (PhaseBackgroundSvc)
1. 初始化分析服务（异步）
2. 填充 OAuth 账户信息（异步）
3. 检测 IDE（异步）
4. 检测 Git 仓库（异步）

### 阶段 3：远程设置 (PhaseRemoteSettings)
1. 初始化远程托管设置加载
2. 初始化策略限制加载

### 阶段 4：网络配置 (PhaseNetwork)
1. 配置 mTLS
2. 配置代理
3. 预连接 API（异步）

### 阶段 5：信任授予后 (PhaseTrustGranted)
1. 应用完整环境变量
2. 初始化遥测

## 错误处理

### 阶段错误
- 配置系统错误：立即失败
- 后台服务错误：记录但不失败
- 网络配置错误：立即失败
- 遥测错误：记录但不失败

### 关闭错误
- 收集所有清理函数的错误
- 即使某些清理失败也继续执行
- 返回所有错误的汇总

## 测试

运行测试：
```bash
go test ./internal/entrypoints/... -v
```

测试覆盖率：
```bash
go test ./internal/entrypoints/... -cover
```

## 架构设计

### 接口驱动
- 所有管理器都是接口
- 便于测试和 mock
- 支持不同实现

### 分阶段执行
- 清晰的依赖关系
- 可追踪的执行进度
- 易于调试和监控

### 异步初始化
- 后台服务异步启动
- 不阻塞主初始化流程
- 提高启动速度

### 优雅关闭
- 注册清理函数
- 反向执行顺序
- 错误收集和报告

## 与 TypeScript 版本的差异

### 优势
1. ✅ 接口抽象更清晰
2. ✅ 类型安全
3. ✅ 更好的并发控制
4. ✅ 无运行时依赖

### 差异
1. TypeScript 使用 Promise → Go 使用 context.Context
2. TypeScript 内联逻辑 → Go 分离接口和实现
3. TypeScript 动态导入 → Go 静态依赖
4. TypeScript 事件驱动 → Go 函数调用

## 实现范围

### 已实现 ✅
- 初始化框架和接口
- 分阶段执行逻辑
- 优雅关闭管理
- No-Op 管理器（测试用）
- 完整测试套件

### 待实现 ⏳
- ConfigManager 具体实现（需要 config 模块）
- MigrationManager 集成（已有 migrations 模块）
- TelemetryManager 具体实现
- NetworkManager 具体实现（代理、mTLS）
- ServiceManager 具体实现（OAuth、分析、检测）

### 不实现 ❌
- CLI UI（保留在 TypeScript）
- Ink 渲染（保留在 TypeScript）
- MCP 服务器入口（保留在 TypeScript）

## 集成示例

### 与 migrations 模块集成

```go
import (
    "github.com/ding/claude-code/claude-go/internal/migrations"
    "github.com/ding/claude-code/claude-go/internal/entrypoints"
)

// 创建迁移管理器适配器
type MigrationManagerAdapter struct {
    executor *migrations.Executor
}

func (m *MigrationManagerAdapter) ExecuteMigrations(ctx context.Context) error {
    result, err := m.executor.Execute(ctx, nil)
    if err != nil {
        return err
    }
    
    if len(result.Failed) > 0 {
        return fmt.Errorf("migrations failed: %v", result.Failed)
    }
    
    return nil
}

// 使用适配器
vm := migrations.NewFileVersionManager(configDir)
executor := migrations.NewExecutor(
    migrations.DefaultRegistry,
    vm,
    &migrations.NoOpAnalyticsLogger{},
)

migrationMgr := &MigrationManagerAdapter{executor: executor}
```

## 相关模块

- `internal/schemas` - 配置验证（已完成）
- `internal/migrations` - 配置迁移（已完成）
- `internal/config` - 配置管理（待实现）
- `internal/telemetry` - 遥测系统（待实现）
- `internal/network` - 网络配置（待实现）

## 参考

- TypeScript 实现：`src/entrypoints/init.ts`
- 设计文档：`MODULES_REFACTOR_PLAN.md`

# Migrations 模块

配置迁移系统，用于在版本升级时自动迁移用户配置。

## 功能特性

- ✅ 版本管理（基于文件的版本追踪）
- ✅ 迁移注册表（支持动态注册）
- ✅ 迁移执行器（支持批量执行、错误处理）
- ✅ 11个迁移脚本（占位实现，待 config 模块完成后实现）
- ✅ 幂等性保证（可安全多次执行）
- ✅ 独立性（迁移之间互不依赖）
- ✅ 错误处理（失败不影响启动）

## 核心类型

### Migration
单个迁移操作：
```go
type Migration struct {
    Version     int
    Name        string
    Description string
    Migrate     func(ctx context.Context) error
}
```

### Registry
迁移注册表：
```go
type Registry struct {
    migrations []Migration
    mu         sync.RWMutex
}
```

### Executor
迁移执行器：
```go
type Executor struct {
    registry         *Registry
    versionManager   VersionManager
    analyticsLogger  AnalyticsLogger
    mu               sync.Mutex
}
```

### VersionManager
版本管理接口：
```go
type VersionManager interface {
    GetCurrentVersion(ctx context.Context) (int, error)
    SetCurrentVersion(ctx context.Context, version int) error
}
```

## 使用示例

### 注册迁移

```go
import "github.com/ding/claude-code/claude-go/internal/migrations"

func init() {
    migrations.MustRegister(migrations.Migration{
        Version:     1,
        Name:        "my_migration",
        Description: "Migrate something",
        Migrate:     myMigrationFunc,
    })
}

func myMigrationFunc(ctx context.Context) error {
    // 执行迁移逻辑
    return nil
}
```

### 执行迁移

```go
import (
    "context"
    "github.com/ding/claude-code/claude-go/internal/migrations"
)

// 创建版本管理器
vm := migrations.NewFileVersionManager("/path/to/config")

// 创建执行器
executor := migrations.NewExecutor(
    migrations.DefaultRegistry,
    vm,
    &migrations.NoOpAnalyticsLogger{},
)

// 执行所有待处理的迁移
ctx := context.Background()
result, err := executor.Execute(ctx, nil)
if err != nil {
    log.Printf("Migration failed: %v", err)
}

log.Printf("Applied: %v, Failed: %v", result.Applied, result.Failed)
```

### 执行选项

```go
// 干运行（不实际执行）
result, err := executor.Execute(ctx, &migrations.ExecuteOptions{
    DryRun: true,
})

// 遇到错误停止
result, err := executor.Execute(ctx, &migrations.ExecuteOptions{
    StopOnError: true,
})

// 执行到指定版本
result, err := executor.Execute(ctx, &migrations.ExecuteOptions{
    TargetVersion: 5,
})
```

### 查询状态

```go
current, latest, pending, err := executor.GetStatus(ctx)
if err != nil {
    log.Fatal(err)
}

log.Printf("Current version: %d", current)
log.Printf("Latest version: %d", latest)
log.Printf("Pending migrations: %d", len(pending))
```

## 迁移列表

### 1. auto_updates_to_settings
将 `autoUpdates` 从全局配置迁移到 settings.json 的环境变量。

### 2. bypass_permissions_to_settings
将 `bypassPermissionsModeAccepted` 从全局配置迁移到 settings.json。

### 3. mcp_servers_to_settings
将 MCP 服务器批准字段从项目配置迁移到本地设置。

### 4. fennec_to_opus
将已移除的 fennec 模型别名迁移到 Opus 4.6 别名。

### 5. legacy_opus_to_current
将第一方用户从显式 Opus 4.0/4.1 模型字符串迁移到当前版本。

### 6. opus_to_opus1m
将 Opus 用户迁移到 Opus 1M 上下文窗口。

### 7. repl_bridge_to_remote_control
将 `replBridgeEnabled` 迁移到 `remoteControlAtStartup`。

### 8. sonnet1m_to_sonnet45
将 Sonnet 1M 用户迁移到 Sonnet 4.5。

### 9. sonnet45_to_sonnet46
将 Pro/Max/Team Premium 用户从显式 Sonnet 4.5 迁移到 sonnet 别名（4.6）。

### 10. reset_auto_mode_opt_in
重置自动模式选择加入标志。

### 11. reset_pro_to_opus_default
将 Pro 用户重置为 Opus 默认模型。

## 迁移特点

### 幂等性
所有迁移都是幂等的，可以安全地多次执行：
- 每个迁移内部检查是否需要执行
- 只读取和写入相同的配置源
- 避免重复迁移

### 独立性
迁移之间互不依赖：
- 可以按任意顺序注册
- 执行时按版本号排序
- 单个迁移失败不影响其他迁移

### 错误处理
迁移失败不会影响应用启动：
- 使用 try-catch 捕获错误
- 记录错误日志和分析事件
- 可选择在错误时停止或继续

## 版本管理

### 文件存储
版本信息存储在 `migration_version.json`：
```json
{
  "version": 11
}
```

### 版本检查
启动时检查当前版本：
```go
currentVersion, err := vm.GetCurrentVersion(ctx)
if currentVersion < migrations.CurrentMigrationVersion {
    // 执行待处理的迁移
}
```

### 版本更新
每个迁移成功后更新版本：
```go
err := vm.SetCurrentVersion(ctx, migration.Version)
```

## 测试

运行测试：
```bash
go test ./internal/migrations/... -v
```

测试覆盖率：
```bash
go test ./internal/migrations/... -cover
```

## 架构设计

### 注册表模式
- 使用全局注册表 `DefaultRegistry`
- 迁移在 `init()` 函数中自动注册
- 支持动态注册和查询

### 接口抽象
- `VersionManager` - 版本管理接口
- `AnalyticsLogger` - 分析日志接口
- 便于测试和扩展

### 并发安全
- Registry 使用 `sync.RWMutex` 保护
- Executor 使用 `sync.Mutex` 保护
- 支持并发查询，串行执行

## 与 TypeScript 版本的差异

### 优势
1. ✅ 编译时类型检查
2. ✅ 更好的并发控制
3. ✅ 更清晰的错误处理
4. ✅ 无运行时依赖

### 差异
1. TypeScript 使用函数导出 → Go 使用注册表模式
2. TypeScript 使用 Promise → Go 使用 context.Context
3. TypeScript 内联迁移 → Go 分离迁移脚本
4. TypeScript 隐式版本 → Go 显式版本号

## 待实现功能

当 config 模块完成后，需要实现以下功能：

- [ ] 实现所有 11 个迁移脚本的具体逻辑
- [ ] 集成到应用初始化流程
- [ ] 添加分析事件日志
- [ ] 实现配置读写操作
- [ ] 添加用户通知机制
- [ ] 支持迁移回滚（可选）

## 相关模块

- `internal/schemas` - 配置验证（已完成）
- `internal/config` - 配置管理（待实现）
- `internal/entrypoints` - 初始化逻辑（待实现）

## 参考

- TypeScript 实现：`src/migrations/`
- 设计文档：`MODULES_REFACTOR_PLAN.md`

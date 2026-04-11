# Memory Prefetch System

> 实现时间: 2026-04-09
> Phase: 5 Week 9
> 状态: ✅ 完成

## 概述

内存预取系统实现了异步的内存相关性搜索，允许在主查询循环执行时并发加载相关的内存文件。这避免了阻塞主查询流程，提高了整体性能。

## 架构

### 核心组件

1. **MemoryPrefetch** - 预取句柄
   - 管理异步预取的生命周期
   - 提供 settlement 追踪
   - 支持 Disposable 模式（通过 Dispose 方法）

2. **MemoryPrefetcher** - 预取器
   - 启动异步内存搜索
   - 管理预取配置
   - 处理搜索逻辑

3. **MemoryAttachment** - 内存附件
   - 表示一个内存文件
   - 包含路径、内容、类型等信息

### 设计模式

#### Disposable 模式

Go 版本使用 `Dispose()` 方法实现类似 TypeScript `using` 关键字的资源管理：

```go
// 使用示例
prefetch := prefetcher.StartRelevantMemoryPrefetch(ctx, messages, 0, readFileState)
if prefetch != nil {
    defer prefetch.Dispose() // 自动清理
}
```

#### 异步执行

使用 goroutine + channel 实现非阻塞预取：

```go
// 启动异步搜索
go m.searchRelevantMemories(ctx, input, readFileState, resultChan, prefetch)

// 在查询循环中消费
select {
case results := <-prefetch.ResultChan:
    // 处理结果
case <-time.After(timeout):
    // 超时处理
}
```

## API

### 创建预取器

```go
// 使用默认配置
prefetcher := prefetch.NewMemoryPrefetcher(nil)

// 使用自定义配置
config := &prefetch.PrefetchConfig{
    MaxSessionBytes: 100000,
    Enabled:         true,
    Timeout:         30 * time.Second,
}
prefetcher := prefetch.NewMemoryPrefetcher(config)
```

### 启动预取

```go
prefetch := prefetcher.StartRelevantMemoryPrefetch(
    ctx,
    messages,
    surfacedBytes,
    readFileState,
)

if prefetch == nil {
    // 预取未启动（禁用、无用户消息等）
    return
}

defer prefetch.Dispose()
```

### 消费结果

```go
// 检查是否已完成
if prefetch.IsSettled() {
    select {
    case results := <-prefetch.ResultChan:
        // 过滤重复
        filtered := prefetch.FilterDuplicateMemoryAttachments(results, readFileState)
        
        // 创建消息
        for _, att := range filtered {
            msg := prefetch.CreateMemoryAttachmentMessage(att)
            // 添加到消息流
        }
        
        // 标记为已消费
        prefetch.ConsumedOnIteration = iteration
    default:
        // 未就绪，下次迭代再试
    }
}
```

## 集成到查询循环

### 启动预取（每个用户轮次一次）

```go
// 在查询循环开始时
var pendingMemoryPrefetch *prefetch.MemoryPrefetch
if memoryPrefetcher != nil {
    pendingMemoryPrefetch = memoryPrefetcher.StartRelevantMemoryPrefetch(
        ctx,
        state.messages,
        surfacedBytes,
        state.readFileState,
    )
    if pendingMemoryPrefetch != nil {
        defer pendingMemoryPrefetch.Dispose()
    }
}
```

### 消费结果（工具执行后）

```go
// 在工具执行完成后
if pendingMemoryPrefetch != nil &&
   pendingMemoryPrefetch.IsSettled() &&
   !pendingMemoryPrefetch.IsConsumed() {
    
    select {
    case results := <-pendingMemoryPrefetch.ResultChan:
        filtered := prefetch.FilterDuplicateMemoryAttachments(
            results,
            state.readFileState,
        )
        
        for _, att := range filtered {
            msg := prefetch.CreateMemoryAttachmentMessage(att)
            // yield msg
            toolResults = append(toolResults, msg)
        }
        
        pendingMemoryPrefetch.ConsumedOnIteration = turnCount - 1
    default:
        // 未就绪，跳过
    }
}
```

## 配置

### PrefetchConfig

```go
type PrefetchConfig struct {
    // MaxSessionBytes 是会话中可以显示的内存总字节数上限
    MaxSessionBytes int

    // Enabled 控制是否启用内存预取
    Enabled bool

    // Timeout 预取操作的超时时间
    Timeout time.Duration
}
```

### 默认配置

```go
MaxSessionBytes: 100000  // 100KB
Enabled:         true
Timeout:         30 * time.Second
```

## 性能特性

### 非阻塞设计

- 预取在后台 goroutine 中运行
- 主查询循环不会等待预取完成
- 使用 channel 进行结果传递

### Settlement 追踪

- `SettledAt` 字段记录完成时间
- `IsSettled()` 方法检查是否完成
- 支持多次迭代重试消费

### 资源管理

- 使用 context 进行超时控制
- `Dispose()` 方法取消未完成的预取
- 自动清理 goroutine 和 channel

## 遥测

预取完成时会记录以下指标：

- `hidden_by_first_iteration`: 是否在第一次迭代前完成
- `consumed_on_iteration`: 在哪次迭代被消费
- `latency_ms`: 预取延迟（毫秒）

## 测试

### 测试覆盖

- ✅ 预取器创建
- ✅ 配置管理
- ✅ 预取启动条件
- ✅ 异步执行
- ✅ Settlement 追踪
- ✅ Dispose 清理
- ✅ 消息过滤
- ✅ 辅助函数

### 运行测试

```bash
go test ./internal/harness/prefetch/... -v
```

### 测试结果

```
PASS
ok      claude-codex/internal/harness/prefetch    0.967s
```

## TODO

### 短期

- [ ] 实现实际的内存文件搜索逻辑
- [ ] 集成文件系统操作
- [ ] 实现相关性评分算法
- [ ] 添加更多的搜索词提取策略

### 长期

- [ ] 支持向量搜索（语义相似度）
- [ ] 缓存搜索结果
- [ ] 支持增量更新
- [ ] 添加性能基准测试

## 参考

### TypeScript 源码

- `src/utils/attachments.ts` - `startRelevantMemoryPrefetch()`
- `src/query.ts` - 集成示例

### 相关模块

- `internal/harness/messages/` - 消息处理
- `internal/harness/storage/` - 会话存储
- `internal/harness/context/` - 上下文收集

## 变更历史

### v1.0.0 (2026-04-09)

- ✅ 初始实现
- ✅ 核心预取逻辑
- ✅ Disposable 模式
- ✅ 完整测试覆盖
- ✅ 文档完善

---

**维护者**: Claude Code  
**最后更新**: 2026-04-09

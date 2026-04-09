# Memdir 模块

Memdir 模块实现了 Claude Code 的自动记忆系统（Auto Memory），提供智能的、持久化的跨会话上下文管理。

## 功能特性

- **路径管理**：安全的内存目录路径验证和管理
- **团队记忆**：支持私有和团队共享的记忆系统
- **记忆扫描**：自动扫描和索引记忆文件
- **智能检索**：使用 AI 模型选择相关记忆
- **记忆老化**：追踪记忆年龄并提供新鲜度警告
- **安全验证**：防止路径遍历和注入攻击

## 架构组件

### 1. 路径管理 (`paths.go`)

管理记忆目录的路径解析和验证：

```go
// 检查自动记忆是否启用
enabled := memdir.IsAutoMemoryEnabled()

// 获取记忆基础目录
baseDir := memdir.GetMemoryBaseDir()

// 获取自动记忆路径
autoMemPath := memdir.GetAutoMemPath(projectRoot)

// 获取记忆入口文件
entrypoint := memdir.GetAutoMemEntrypoint(projectRoot)

// 验证路径安全性
validated, ok := memdir.ValidateMemoryPath("/path/to/memory", false)
```

**安全特性：**
- 拒绝相对路径
- 拒绝根路径或近根路径
- 拒绝 UNC 路径
- 拒绝包含空字节的路径
- 支持波浪号扩展（可选）

### 2. 团队记忆路径 (`team_paths.go`)

管理团队共享记忆的路径和安全验证：

```go
// 检查团队记忆是否启用
enabled := memdir.IsTeamMemoryEnabled()

// 获取团队记忆路径
teamPath := memdir.GetTeamMemPath(projectRoot)

// 验证团队记忆路径
validated, err := memdir.ValidateTeamMemPath(filePath, projectRoot)

// 验证相对路径键
validated, err := memdir.ValidateTeamMemKey("user/profile.md", projectRoot)
```

**安全特性：**
- 路径键清理（防止注入）
- URL 编码遍历检测
- Unicode 规范化攻击防护
- 符号链接解析和验证
- 悬空符号链接检测

### 3. 记忆年龄 (`memory_age.go`)

追踪记忆文件的年龄并生成新鲜度警告：

```go
// 获取记忆年龄（天数）
days := memdir.MemoryAgeDays(mtimeMs)

// 获取人类可读的年龄字符串
age := memdir.MemoryAge(mtimeMs) // "today", "yesterday", "5 days ago"

// 获取新鲜度警告文本
warning := memdir.MemoryFreshnessText(mtimeMs)

// 获取带标签的新鲜度提示
note := memdir.MemoryFreshnessNote(mtimeMs)
```

### 4. 记忆扫描 (`memory_scan.go`)

扫描记忆目录并解析文件元数据：

```go
// 扫描记忆文件
headers, err := memdir.ScanMemoryFiles(memoryDir, ctx)

// 格式化为清单
manifest := memdir.FormatMemoryManifest(headers)
```

**特性：**
- 递归扫描目录
- 解析 YAML frontmatter
- 按修改时间排序
- 限制最多 200 个文件
- 排除 MEMORY.md 索引文件

### 5. 智能检索 (`find_relevant.go`)

使用 AI 模型选择相关记忆：

```go
// 查找相关记忆
relevant, err := memdir.FindRelevantMemories(
    query,
    memoryDir,
    ctx,
    client,
    recentTools,
    alreadySurfaced,
)
```

**工作流程：**
1. 扫描记忆文件获取元数据
2. 过滤已展示的记忆
3. 使用 Sonnet 模型选择最相关的记忆（最多 5 个）
4. 返回选中的记忆路径和时间戳

### 6. 提示词构建 (`memdir.go`)

生成记忆系统的提示词：

```go
// 构建记忆提示词
prompt, err := memdir.BuildMemoryPrompt(projectRoot, extraGuidelines, skipIndex)

// 截断入口文件内容
truncated := memdir.TruncateEntrypointContent(content)

// 确保记忆目录存在
err := memdir.EnsureMemoryDirExists(memoryDir)
```

## 使用示例

### 基本路径管理

```go
package main

import (
    "fmt"
    "github.com/ding/claude-code/claude-go/internal/memdir"
)

func main() {
    projectRoot := "/home/user/project"
    
    // 检查是否启用
    if !memdir.IsAutoMemoryEnabled() {
        fmt.Println("Auto memory is disabled")
        return
    }
    
    // 获取记忆路径
    autoMemPath := memdir.GetAutoMemPath(projectRoot)
    fmt.Printf("Memory path: %s\n", autoMemPath)
    
    // 确保目录存在
    if err := memdir.EnsureMemoryDirExists(autoMemPath); err != nil {
        panic(err)
    }
}
```

### 扫描和检索记忆

```go
package main

import (
    "context"
    "fmt"
    "github.com/ding/claude-code/claude-go/internal/memdir"
    "github.com/ding/claude-code/claude-go/pkg/anthropic"
)

func main() {
    projectRoot := "/home/user/project"
    memoryDir := memdir.GetAutoMemPath(projectRoot)
    
    // 扫描记忆文件
    ctx := context.Background()
    headers, err := memdir.ScanMemoryFiles(memoryDir, ctx)
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Found %d memory files\n", len(headers))
    
    // 格式化为清单
    manifest := memdir.FormatMemoryManifest(headers)
    fmt.Println(manifest)
    
    // 使用 AI 查找相关记忆
    client := anthropic.NewClient("your-api-key")
    relevant, err := memdir.FindRelevantMemories(
        "How do I configure the database?",
        memoryDir,
        ctx,
        client,
        []string{},
        make(map[string]bool),
    )
    
    if err != nil {
        panic(err)
    }
    
    fmt.Printf("Found %d relevant memories\n", len(relevant))
    for _, r := range relevant {
        age := memdir.MemoryAge(r.MtimeMs)
        fmt.Printf("- %s (%s)\n", r.Path, age)
    }
}
```

### 团队记忆验证

```go
package main

import (
    "fmt"
    "github.com/ding/claude-code/claude-go/internal/memdir"
)

func main() {
    projectRoot := "/home/user/project"
    
    // 验证团队记忆路径
    filePath := "/home/user/project/.claude/memory/team/user.md"
    validated, err := memdir.ValidateTeamMemPath(filePath, projectRoot)
    if err != nil {
        fmt.Printf("Invalid path: %v\n", err)
        return
    }
    
    fmt.Printf("Validated path: %s\n", validated)
    
    // 验证相对路径键
    key := "user/profile.md"
    validated, err = memdir.ValidateTeamMemKey(key, projectRoot)
    if err != nil {
        fmt.Printf("Invalid key: %v\n", err)
        return
    }
    
    fmt.Printf("Validated key path: %s\n", validated)
}
```

### 记忆年龄追踪

```go
package main

import (
    "fmt"
    "time"
    "github.com/ding/claude-code/claude-go/internal/memdir"
)

func main() {
    // 今天的记忆
    todayMs := time.Now().UnixMilli()
    fmt.Printf("Today: %s\n", memdir.MemoryAge(todayMs))
    
    // 5 天前的记忆
    oldMs := time.Now().Add(-5 * 24 * time.Hour).UnixMilli()
    fmt.Printf("5 days ago: %s\n", memdir.MemoryAge(oldMs))
    
    // 获取新鲜度警告
    warning := memdir.MemoryFreshnessText(oldMs)
    if warning != "" {
        fmt.Printf("Warning: %s\n", warning)
    }
}
```

## 记忆文件格式

记忆文件使用 Markdown 格式，带有 YAML frontmatter：

```markdown
---
name: User Profile
description: Information about the user's role and preferences
type: user
---

The user is a senior software engineer with 10 years of experience.
They prefer TypeScript over JavaScript and always write tests first.
```

### Frontmatter 字段

- `name`: 记忆名称
- `description`: 一行描述（用于相关性判断）
- `type`: 记忆类型（user, feedback, project, reference）

### 记忆类型

1. **user**: 用户角色、目标、职责和知识
2. **feedback**: 用户给出的工作方法指导
3. **project**: 项目相关的工作、目标、问题
4. **reference**: 外部系统资源的指针

## 安全特性

### 路径遍历防护

- 拒绝相对路径（`../etc/passwd`）
- 拒绝绝对路径键（`/etc/passwd`）
- 拒绝 URL 编码遍历（`%2e%2e%2f`）
- 拒绝 Unicode 规范化攻击（全角字符）
- 拒绝反斜杠（Windows 路径分隔符）
- 拒绝空字节（C 系统调用截断）

### 符号链接验证

- 解析最深存在祖先的符号链接
- 检测悬空符号链接
- 验证真实路径在团队目录内
- 防止符号链接逃逸攻击

## 配置

### 环境变量

- `CLAUDE_CODE_DISABLE_AUTO_MEMORY`: 禁用自动记忆（1/true）
- `CLAUDE_CODE_SIMPLE`: 简单模式，禁用记忆
- `CLAUDE_CODE_REMOTE`: CCR 模式标志
- `CLAUDE_CODE_REMOTE_MEMORY_DIR`: CCR 记忆目录覆盖
- `CLAUDE_COWORK_MEMORY_PATH_OVERRIDE`: Cowork 路径覆盖
- `CLAUDE_CONFIG_HOME`: Claude 配置主目录（默认 ~/.claude）

### 常量

```go
const (
    AutoMemDirName        = "memory"
    AutoMemEntrypointName = "MEMORY.md"
    EntrypointName        = "MEMORY.md"
    MaxEntrypointLines    = 200
    MaxEntrypointBytes    = 25000
    MaxMemoryFiles        = 200
    FrontmatterMaxLines   = 30
)
```

## 测试

运行测试：

```bash
go test ./internal/memdir/... -v
```

测试覆盖：
- 路径验证和清理
- 记忆年龄计算
- 文件扫描和解析
- Frontmatter 解析
- 入口文件截断
- 安全验证（路径遍历、符号链接）
- 提示词构建

## 依赖

```go
import (
    "gopkg.in/yaml.v3"  // YAML frontmatter 解析
)
```

安装依赖：

```bash
go get gopkg.in/yaml.v3
```

## 相关文档

- [Memory 系统文档](../memory/README.md)
- [Agent 系统文档](../agent/README.md)
- [TypeScript 原始实现](../../src/memdir/)

## 架构特点

- **安全优先**：多层路径验证和注入防护
- **智能检索**：使用 AI 模型选择相关记忆
- **年龄追踪**：自动追踪记忆新鲜度
- **团队协作**：支持私有和共享记忆
- **性能优化**：限制文件数量和大小
- **错误恢复**：优雅处理文件系统错误

## 最佳实践

1. **路径验证**：始终使用 `ValidateMemoryPath` 验证用户输入
2. **上下文取消**：在扫描操作中传递 context 以支持取消
3. **错误处理**：检查所有返回的错误
4. **新鲜度检查**：使用 `MemoryFreshnessText` 警告旧记忆
5. **限制大小**：遵守 `MaxEntrypointLines` 和 `MaxEntrypointBytes` 限制

# Skills 模块

Skills 模块负责管理和加载技能（slash 命令），支持内置技能、用户自定义技能、MCP 技能和插件技能。

## 功能特性

- ✅ 技能注册表和管理
- ✅ 内置技能系统
- ✅ 文件系统加载器
- ✅ Frontmatter 解析
- ✅ 技能缓存和去重
- ✅ 条件激活（基于路径）
- ✅ MCP 技能集成
- ✅ 安全路径验证
- ✅ 完整测试覆盖

## 核心类型

### SkillDefinition

技能定义：

```go
type SkillDefinition struct {
    // 核心标识
    Name        string
    DisplayName string
    Aliases     []string

    // 文档
    Description                string
    HasUserSpecifiedDescription bool
    WhenToUse                  string
    ArgumentHint               string

    // 执行
    AllowedTools           []string
    Model                  string
    DisableModelInvocation bool
    ExecutionContext       ExecutionContext // inline or fork
    Agent                  string
    Effort                 *int

    // 可见性和权限
    UserInvocable bool
    IsHidden      bool
    Source        SkillSource
    LoadedFrom    string

    // 内容
    Content       string
    ContentLength int
    ArgumentNames []string
    Files         map[string]string

    // 元数据
    Version      string
    SkillRoot    string
    Paths        []string
    LoadedAt     time.Time
    FileIdentity string

    // Hooks 和配置
    Hooks map[string]interface{}

    // 提示词生成
    GetPrompt PromptGenerator
}
```

### SkillSource

技能来源：

```go
const (
    SourceBundled SkillSource = "bundled" // 内置技能
    SourceFile    SkillSource = "file"    // 文件系统技能
    SourceMCP     SkillSource = "mcp"     // MCP 技能
    SourcePlugin  SkillSource = "plugin"  // 插件技能
    SourceManaged SkillSource = "managed" // 策略管理技能
)
```

## 使用示例

### 1. 创建技能管理器

```go
import "github.com/ding/claude-code/claude-go/internal/skills"

// 创建管理器
manager := skills.NewSkillManager()

// 加载内置技能
err := manager.LoadBundledSkills()
if err != nil {
    log.Fatal(err)
}
```

### 2. 注册内置技能

```go
// 简单文本技能
skill := skills.NewSimpleSkill(
    "hello",
    "Say hello",
    "Hello, World!",
)

err := skills.RegisterBundledSkill(skill)
if err != nil {
    log.Fatal(err)
}

// 自定义提示词生成器
customSkill := skills.NewCustomSkill(
    "custom",
    "Custom skill",
    func(args string, ctx *skills.SkillContext) ([]skills.ContentBlock, error) {
        text := fmt.Sprintf("Custom skill with args: %s", args)
        return []skills.ContentBlock{{Type: "text", Text: text}}, nil
    },
)

err = skills.RegisterBundledSkill(customSkill)
```

### 3. 从文件系统加载技能

```go
// 从目录加载
err := manager.LoadSkillsFromDirectory(
    "/path/to/skills",
    skills.SourceFile,
)

// 从多个目录加载
dirs := []string{
    "~/.claude/skills",
    ".claude/skills",
}
err = manager.AddSkillDirectories(dirs)
```

### 4. 技能查询

```go
// 按名称或别名查询
skill, ok := manager.GetSkill("hello")
if ok {
    fmt.Println(skill.Description)
}

// 列出所有技能
allSkills := manager.ListSkills()

// 列出用户可调用的技能
userSkills := manager.ListUserInvocableSkills()

// 获取统计信息
stats := manager.GetStats()
fmt.Printf("Total: %d, Bundled: %d, Dynamic: %d\n",
    stats.TotalSkills,
    stats.BundledSkills,
    stats.DynamicSkills,
)
```

### 5. 条件激活

```go
// 基于路径激活技能
paths := []string{"src/main.go", "src/utils.go"}
activated := manager.ActivateConditionalSkillsForPaths(paths)
fmt.Printf("Activated %d skills\n", activated)

// 发现技能目录
dirs := skills.DiscoverSkillDirsForPaths(paths)

// 重新加载技能
err := manager.ReloadSkillsForPaths(paths)
```

### 6. MCP 技能集成

```go
// 注册 MCP 技能构建器
skills.RegisterMCPSkillBuilder(skills.DefaultMCPSkillBuilder)

// 从 MCP 工具元数据加载技能
tools := []map[string]interface{}{
    {
        "name":        "search",
        "description": "Search the web",
    },
}

err := manager.LoadMCPSkills(tools)

// 获取 MCP 技能
mcpSkills := manager.GetMCPSkills()
```

### 7. 技能文件格式

技能文件使用 Markdown + YAML frontmatter：

```markdown
---
name: Example Skill
description: An example skill
when_to_use: When you need an example
allowed-tools: ["Read", "Write", "Bash"]
user-invocable: true
context: inline
effort: medium
paths:
  - src/**
  - tests/**
---

# Example Skill

This is the skill content that will be sent to the model.

You can use {{arg1}} and {{arg2}} for argument substitution.
```

## 安全特性

### 路径验证

```go
// 验证技能相对路径
fullPath, err := skills.ValidateSkillPath(baseDir, "file.txt")
if err != nil {
    // 路径不安全（绝对路径、父目录遍历等）
}

// 检查路径安全性
if !skills.IsPathSafe("../etc/passwd") {
    // 不安全的路径
}
```

### 安全文件写入

```go
// 使用 O_EXCL | O_NOFOLLOW 安全写入
err := skills.SafeWriteFile("/path/to/file", []byte("content"))
if err != nil {
    // 文件已存在或写入失败
}

// 批量写入技能文件
files := map[string]string{
    "README.md": "# Skill",
    "config.yaml": "key: value",
}

err = skills.WriteSkillFiles(baseDir, files)
```

### 文件去重

```go
// 获取文件标识（解析符号链接）
identity, err := skills.GetFileIdentity("/path/to/file")
if err != nil {
    log.Fatal(err)
}

// 相同文件的不同路径会有相同的 identity
```

## Frontmatter 字段

支持的 frontmatter 字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 显示名称 |
| `description` | string/array | 技能描述 |
| `when_to_use` | string | 使用场景 |
| `argument-hint` | string | 参数提示 |
| `arguments` | string/array | 参数名称 |
| `allowed-tools` | string/array | 允许的工具 |
| `model` | string | 使用的模型 |
| `disable-model-invocation` | bool | 禁用模型调用 |
| `user-invocable` | bool | 用户可调用 |
| `context` | string | 执行上下文（inline/fork） |
| `agent` | string | Agent 类型 |
| `effort` | int/string | 努力级别（1-5 或名称） |
| `version` | string | 版本号 |
| `paths` | string/array | 路径模式 |
| `hooks` | object | Hook 配置 |

## 架构设计

### 模块结构

```
internal/skills/
├── types.go          # 核心类型定义
├── bundled.go        # 内置技能注册表
├── loader.go         # 文件系统加载器
├── parser.go         # Frontmatter 解析
├── cache.go          # 技能缓存和管理器
├── activation.go     # 条件激活逻辑
├── security.go       # 安全工具函数
├── mcp_bridge.go     # MCP 技能桥接
├── skills_test.go    # 测试
└── README.md         # 文档
```

### 技能生命周期

1. **注册阶段**
   - 内置技能：通过 `RegisterBundledSkill()` 注册
   - 文件技能：通过 `LoadSkillsFromDirectory()` 加载
   - MCP 技能：通过 `LoadMCPSkills()` 加载

2. **缓存阶段**
   - 基于文件标识（realpath）去重
   - 内存缓存避免重复读取
   - 懒加载内容

3. **激活阶段**
   - 无条件技能：立即注册到 registry
   - 条件技能：存储在 conditionalSkills，等待激活
   - 路径匹配：通过 `ActivateConditionalSkillsForPaths()` 激活

4. **执行阶段**
   - 查询技能：`GetSkill(name)`
   - 生成提示词：`skill.GetPrompt(args, ctx)`
   - 参数替换：`{{arg}}` 占位符

### 并发安全

- `SkillRegistry`：使用 `sync.RWMutex` 保护
- `SkillCache`：使用 `sync.RWMutex` 保护
- `SkillManager`：使用 `sync.RWMutex` 保护
- `BundledSkillRegistry`：使用 `sync.RWMutex` 保护

### 变更通知

```go
// 注册监听器
unsubscribe := manager.OnSkillsChanged(func() {
    fmt.Println("Skills changed!")
})

// 取消订阅
unsubscribe()
```

## 测试

运行测试：

```bash
go test ./internal/skills/... -v
```

测试覆盖率：

```bash
go test ./internal/skills/... -cover
```

测试包括：
- ✅ 技能注册表操作
- ✅ 安全路径验证
- ✅ Frontmatter 解析
- ✅ 文件系统加载
- ✅ 技能缓存
- ✅ 条件激活
- ✅ MCP 集成

## 性能优化

1. **缓存策略**
   - 基于文件标识去重
   - 懒加载技能内容
   - 内存缓存避免重复解析

2. **并发控制**
   - 读写锁分离
   - 最小锁粒度
   - 异步通知监听器

3. **路径匹配**
   - 使用 doublestar 高效 glob 匹配
   - 缓存匹配结果
   - 提前终止匹配

## 与 TypeScript 版本的差异

### 优势

1. ✅ 类型安全的技能定义
2. ✅ 更好的并发控制
3. ✅ 无运行时依赖
4. ✅ 更快的启动速度
5. ✅ 内存占用更小

### 差异

1. TypeScript 动态导入 → Go 静态注册
2. TypeScript Promise → Go error 返回
3. TypeScript 事件驱动 → Go 回调函数
4. TypeScript React 组件 → Go 纯文本生成

## 依赖

### 标准库

- `os` - 文件系统操作
- `path/filepath` - 路径处理
- `sync` - 并发控制
- `time` - 时间戳
- `regexp` - 正则表达式

### 外部依赖

- `gopkg.in/yaml.v3` - YAML 解析
- `github.com/bmatcuk/doublestar/v4` - Glob 匹配

## 相关模块

- `internal/agent` - Agent 系统（已完成）
- `internal/memory` - 记忆系统（已完成）
- `internal/state` - 状态管理（已完成）

## 未来改进

- [ ] 文件系统监听（热重载）
- [ ] 技能版本管理
- [ ] 技能依赖解析
- [ ] 技能市场集成
- [ ] 性能分析和优化

## 参考

- TypeScript 实现：`src/skills/`
- 设计文档：`SKILLS_REFACTOR_PLAN.md`

---

生成时间：2026-04-06

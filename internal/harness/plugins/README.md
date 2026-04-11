# Plugins 模块

Plugins 模块管理内置插件的注册、启用/禁用和技能集成。

## 功能特性

- ✅ 内置插件注册表
- ✅ 插件启用/禁用管理
- ✅ 用户设置集成
- ✅ 技能集成
- ✅ 可用性检查
- ✅ 完整测试覆盖

## 核心类型

### BuiltinPluginDefinition

内置插件定义：

```go
type BuiltinPluginDefinition struct {
    Name           string
    Description    string
    Version        string
    DefaultEnabled bool
    IsAvailable    func() bool // 可用性检查
    Skills         []*skills.SkillDefinition
    Hooks          map[string]interface{}
    MCPServers     map[string]interface{}
}
```

### LoadedPlugin

已加载的插件：

```go
type LoadedPlugin struct {
    Name        string
    Manifest    PluginManifest
    Path        string // "builtin" for built-in plugins
    Source      string // Plugin ID (name@marketplace)
    Repository  string
    Enabled     bool
    IsBuiltin   bool
    HooksConfig map[string]interface{}
    MCPServers  map[string]interface{}
}
```

## 使用示例

### 1. 注册内置插件

```go
import "claude-codex/internal/plugins"

// 注册插件
plugin := &plugins.BuiltinPluginDefinition{
    Name:           "example-plugin",
    Description:    "An example plugin",
    Version:        "1.0.0",
    DefaultEnabled: true,
    Skills: []*skills.SkillDefinition{
        // 插件提供的技能
    },
}

plugins.RegisterBuiltinPlugin(plugin)
```

### 2. 带可用性检查的插件

```go
plugin := &plugins.BuiltinPluginDefinition{
    Name:           "conditional-plugin",
    Description:    "Only available on certain platforms",
    Version:        "1.0.0",
    DefaultEnabled: true,
    IsAvailable: func() bool {
        // 检查平台、环境变量等
        return runtime.GOOS == "darwin"
    },
}

plugins.RegisterBuiltinPlugin(plugin)
```

### 3. 获取插件列表

```go
// 用户设置（从配置文件加载）
userSettings := map[string]bool{
    "example-plugin@builtin": true,
    "other-plugin@builtin":   false,
}

// 获取启用和禁用的插件
enabled, disabled := plugins.GetBuiltinPlugins(userSettings)

for _, plugin := range enabled {
    fmt.Printf("Enabled: %s - %s\n", plugin.Name, plugin.Manifest.Description)
}

for _, plugin := range disabled {
    fmt.Printf("Disabled: %s - %s\n", plugin.Name, plugin.Manifest.Description)
}
```

### 4. 获取插件技能

```go
// 获取所有启用插件的技能
skills := plugins.GetBuiltinPluginSkills(userSettings)

for _, skill := range skills {
    fmt.Printf("Skill: %s - %s\n", skill.Name, skill.Description)
}
```

### 5. 检查插件 ID

```go
// 检查是否为内置插件 ID
if plugins.IsBuiltinPluginID("example@builtin") {
    fmt.Println("This is a built-in plugin")
}
```

### 6. 查询特定插件

```go
// 获取插件定义
definition, ok := plugins.GetBuiltinPluginDefinition("example-plugin")
if ok {
    fmt.Printf("Plugin: %s v%s\n", definition.Name, definition.Version)
}
```

## 插件 ID 格式

内置插件使用特殊的 ID 格式：

```
{plugin-name}@builtin
```

例如：
- `example-plugin@builtin`
- `my-tools@builtin`

这与市场插件的格式区分：
- `example-plugin@marketplace-name`

## 启用状态优先级

插件的启用状态按以下优先级确定：

1. **用户设置**：用户在配置中明确启用/禁用
2. **插件默认值**：插件定义中的 `DefaultEnabled` 字段
3. **全局默认值**：如果都未设置，默认为 `true`

```go
// 优先级示例
plugin := &plugins.BuiltinPluginDefinition{
    Name:           "example",
    DefaultEnabled: false, // 默认禁用
}

// 场景 1：无用户设置
enabled, _ := plugins.GetBuiltinPlugins(nil)
// 结果：插件禁用（使用 DefaultEnabled）

// 场景 2：用户启用
userSettings := map[string]bool{"example@builtin": true}
enabled, _ = plugins.GetBuiltinPlugins(userSettings)
// 结果：插件启用（用户设置优先）
```

## 可用性检查

插件可以定义 `IsAvailable` 函数来动态检查可用性：

```go
plugin := &plugins.BuiltinPluginDefinition{
    Name: "platform-specific",
    IsAvailable: func() bool {
        // 只在 macOS 上可用
        return runtime.GOOS == "darwin"
    },
}
```

不可用的插件会被完全排除，不会出现在启用或禁用列表中。

## 与 Skills 模块集成

插件可以提供技能：

```go
plugin := &plugins.BuiltinPluginDefinition{
    Name: "my-plugin",
    Skills: []*skills.SkillDefinition{
        {
            Name:        "my-skill",
            Description: "A skill from my plugin",
            GetPrompt: func(args string, ctx *skills.SkillContext) ([]skills.ContentBlock, error) {
                return []skills.ContentBlock{{Type: "text", Text: "Hello!"}}, nil
            },
        },
    },
}
```

只有启用的插件的技能才会被加载。

## 初始化

在应用启动时调用：

```go
func main() {
    // 初始化内置插件
    plugins.InitBuiltinPlugins()
    
    // 加载用户设置
    userSettings := loadUserSettings()
    
    // 获取启用的插件
    enabled, _ := plugins.GetBuiltinPlugins(userSettings)
    
    // 加载插件技能
    pluginSkills := plugins.GetBuiltinPluginSkills(userSettings)
}
```

## 测试

运行测试：

```bash
go test ./internal/plugins/... -v
```

测试覆盖：
- ✅ 插件注册
- ✅ 插件 ID 验证
- ✅ 启用/禁用逻辑
- ✅ 用户设置优先级
- ✅ 技能集成
- ✅ 可用性检查
- ✅ 注册表清理

## 架构设计

### 线程安全

使用 `sync.RWMutex` 保护插件注册表：
- 注册操作：写锁
- 查询操作：读锁

### 单例模式

全局 `builtinRegistry` 实例管理所有内置插件。

### 懒加载

插件定义在注册时存储，但技能只在需要时加载。

## 与 TypeScript 版本的差异

### 优势

1. ✅ 类型安全的插件定义
2. ✅ 更简单的 API
3. ✅ 更好的并发控制
4. ✅ 无运行时依赖

### 差异

1. TypeScript 使用 Map → Go 使用 map
2. TypeScript 动态导入 → Go 静态注册
3. TypeScript 设置系统 → Go 接受设置参数

## 依赖

### 内部依赖

- `internal/skills` - 技能系统

### 标准库

- `sync` - 并发控制

## 未来改进

- [ ] 插件市场集成
- [ ] 插件版本管理
- [ ] 插件依赖解析
- [ ] 插件热重载
- [ ] 插件沙箱隔离

## 参考

- TypeScript 实现：`src/plugins/`
- Skills 模块：`internal/skills/`

---

生成时间：2026-04-06

# Skills 模块重构计划

## 模块概述

Skills 模块负责管理和加载技能（slash 命令），包括：
- 内置技能（bundled skills）
- 用户自定义技能（从文件系统加载）
- MCP 技能（从 MCP 服务器加载）
- 插件技能

## TypeScript 架构分析

### 核心文件
1. **bundledSkills.ts** (221 行)
   - 内置技能注册表
   - 技能文件提取到磁盘
   - 安全的文件写入（防路径遍历）

2. **loadSkillsDir.ts** (约 900 行)
   - 从文件系统加载技能
   - Frontmatter 解析
   - 技能去重和缓存
   - 条件激活（基于路径）
   - 动态技能发现

3. **mcpSkillBuilders.ts** (46 行)
   - MCP 技能构建器注册
   - 避免循环依赖的桥接模块

4. **bundled/** (17 个内置技能)
   - remember.ts - 记忆管理
   - verify.ts - 验证工具
   - loop.ts - 循环执行
   - claudeApi.ts - API 调用
   - debug.ts - 调试工具
   - stuck.ts - 卡住处理
   - batch.ts - 批处理
   - updateConfig.ts - 配置更新
   - keybindings.ts - 快捷键
   - skillify.ts - 技能化
   - simplify.ts - 简化
   - 等等...

### 关键功能

#### 1. 技能定义
```typescript
type BundledSkillDefinition = {
  name: string
  description: string
  aliases?: string[]
  whenToUse?: string
  argumentHint?: string
  allowedTools?: string[]
  model?: string
  disableModelInvocation?: boolean
  userInvocable?: boolean
  hooks?: HooksSettings
  context?: 'inline' | 'fork'
  agent?: string
  files?: Record<string, string>  // 引用文件
  getPromptForCommand: (args, context) => Promise<ContentBlockParam[]>
}
```

#### 2. 技能加载
- 从多个目录加载（用户、项目、策略、插件）
- Markdown 文件解析（frontmatter + 内容）
- 去重（基于 realpath）
- 缓存和热重载
- Gitignore 过滤

#### 3. 技能激活
- 条件激活（基于 paths 字段）
- 动态发现（监听文件系统变化）
- 权限检查（plugin-only 策略）

#### 4. 安全特性
- 路径遍历防护
- 符号链接安全处理
- O_NOFOLLOW | O_EXCL 文件写入
- 0o700/0o600 权限模式

## Go 实现计划

### 目录结构
```
internal/skills/
├── types.go              # 核心类型定义
├── bundled.go            # 内置技能注册表
├── loader.go             # 文件系统加载器
├── parser.go             # Frontmatter 解析
├── cache.go              # 技能缓存
├── activation.go         # 条件激活逻辑
├── security.go           # 安全工具（路径验证、安全写入）
├── mcp_bridge.go         # MCP 技能桥接
├── bundled/              # 内置技能实现
│   ├── remember.go
│   ├── verify.go
│   ├── loop.go
│   └── ...
├── skills_test.go        # 测试
└── README.md             # 文档
```

### 实现阶段

#### 阶段 1：核心类型和注册表 ✅
- [ ] types.go - 技能定义类型
- [ ] bundled.go - 内置技能注册表
- [ ] security.go - 安全工具函数

#### 阶段 2：文件系统加载器 ✅
- [ ] parser.go - Frontmatter 解析
- [ ] loader.go - 从目录加载技能
- [ ] cache.go - 技能缓存和去重

#### 阶段 3：条件激活 ✅
- [ ] activation.go - 基于路径的条件激活
- [ ] 动态技能发现

#### 阶段 4：MCP 集成 ✅
- [ ] mcp_bridge.go - MCP 技能桥接

#### 阶段 5：内置技能 ⏸️
- [ ] 移植 17 个内置技能
- [ ] 优先级：remember, verify, debug

#### 阶段 6：测试和文档 ✅
- [ ] 单元测试
- [ ] 集成测试
- [ ] README 文档

## 依赖关系

### 已有模块
- ✅ internal/agent - Agent 系统
- ✅ internal/memory - 记忆系统
- ✅ internal/state - 状态管理
- ✅ internal/schemas - 配置验证

### 需要的模块
- ⏳ internal/config - 配置管理（部分功能）
- ⏳ internal/frontmatter - Frontmatter 解析器
- ⏳ internal/markdown - Markdown 处理
- ⏳ pkg/anthropic - API 客户端

### 外部依赖
- gopkg.in/yaml.v3 - YAML 解析
- github.com/bmatcuk/doublestar/v4 - Glob 匹配

## 关键设计决策

### 1. 技能存储格式
保持与 TypeScript 兼容：
- Markdown 文件 + YAML frontmatter
- 文件名即技能名
- 支持别名

### 2. 缓存策略
- 基于 realpath 去重
- 内存缓存 + 文件系统监听
- 懒加载（首次使用时加载内容）

### 3. 安全模型
- 路径规范化和验证
- 安全文件写入（O_NOFOLLOW | O_EXCL）
- 权限检查（0o700/0o600）

### 4. 扩展性
- 插件式架构（注册表模式）
- 支持多种技能来源（bundled, file, mcp, plugin）
- 条件激活机制

## 实现优先级

### 高优先级 ⭐⭐⭐
1. 核心类型和注册表
2. 文件系统加载器
3. Frontmatter 解析
4. 安全工具

### 中优先级 ⭐⭐
1. 技能缓存
2. 条件激活
3. MCP 桥接
4. 基础内置技能（remember, verify）

### 低优先级 ⭐
1. 完整的内置技能移植
2. 文件系统监听
3. 热重载

## 测试策略

### 单元测试
- 类型定义测试
- 注册表操作测试
- Frontmatter 解析测试
- 路径验证测试
- 安全写入测试

### 集成测试
- 从目录加载技能
- 技能去重
- 条件激活
- MCP 技能集成

### 安全测试
- 路径遍历攻击
- 符号链接攻击
- 权限检查

## 与 TypeScript 的差异

### 优势
1. ✅ 类型安全的技能定义
2. ✅ 更好的并发控制
3. ✅ 无运行时依赖
4. ✅ 更快的启动速度

### 差异
1. TypeScript 动态导入 → Go 静态注册
2. TypeScript Promise → Go channel/context
3. TypeScript 事件驱动 → Go 回调函数
4. TypeScript React 组件 → Go 纯文本生成

## 待解决问题

1. **内置技能的实现方式**
   - 是否需要移植所有 17 个内置技能？
   - 哪些技能是后端必需的？
   - 哪些技能可以保留在 TypeScript？

2. **文件系统监听**
   - 使用 fsnotify 还是轮询？
   - 如何处理大量文件变更？

3. **MCP 集成**
   - MCP 技能的生命周期管理
   - 如何与 MCP 服务器通信？

4. **性能优化**
   - 技能加载的并发控制
   - 缓存失效策略
   - 内存占用优化

## 时间估算

- 阶段 1：2-3 小时
- 阶段 2：3-4 小时
- 阶段 3：2-3 小时
- 阶段 4：2-3 小时
- 阶段 5：4-6 小时（取决于移植多少内置技能）
- 阶段 6：2-3 小时

**总计：15-22 小时**

## 下一步行动

1. 创建 internal/skills 目录结构
2. 实现核心类型定义（types.go）
3. 实现内置技能注册表（bundled.go）
4. 实现安全工具（security.go）
5. 编写单元测试

---

生成时间：2026-04-06

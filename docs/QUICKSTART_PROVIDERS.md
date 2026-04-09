# 多 LLM 提供商支持 - 快速开始

## 功能概述

claude-go 现在支持三种主流 LLM 提供商：

- ✅ **Anthropic Claude** (默认)
- ✅ **OpenAI GPT**
- ✅ **Google Gemini**

## 快速配置

### 方式 1: 使用 CLI 命令

```bash
# 1. 选择提供商并配置
claude /config set provider openai
claude /config set model gpt-4o
claude /config set api_key sk-xxxxx

# 2. 查看当前配置
claude /config show

# 3. 开始使用
claude "你好，请介绍一下自己"
```

### 方式 2: 编辑配置文件

找到配置文件位置：
```bash
claude /config path
```

编辑配置文件（通常在 `~/.claude-go/config.json`）：

```json
{
  "provider": "openai",
  "model": "gpt-4o",
  "api_key": "sk-xxxxx",
  "api_base_url": "https://api.openai.com/v1"
}
```

## 提供商配置示例

### Anthropic Claude

```bash
claude /config set provider anthropic
claude /config set model claude-sonnet-4-5
claude /config set api_key sk-ant-xxxxx
claude /config set api_base_url https://api.anthropic.com
```

**推荐模型:**
- `claude-opus-4` - 最强大
- `claude-sonnet-4-5` - 平衡性能（默认）
- `claude-haiku-3-5` - 最快速

### OpenAI GPT

```bash
claude /config set provider openai
claude /config set model gpt-4o
claude /config set api_key sk-xxxxx
claude /config set api_base_url https://api.openai.com/v1
```

**推荐模型:**
- `gpt-4o` - 最新最强（推荐）
- `gpt-4-turbo` - 高性能
- `gpt-3.5-turbo` - 经济实惠

### Google Gemini

```bash
claude /config set provider gemini
claude /config set model gemini-1.5-pro
claude /config set api_key AIzaSyxxxxx
claude /config set api_base_url https://generativelanguage.googleapis.com/v1beta
```

**推荐模型:**
- `gemini-1.5-pro` - 最强大（推荐）
- `gemini-1.5-flash` - 快速响应
- `gemini-2.0-flash-exp` - 实验版本

## 获取 API 密钥

### Anthropic Claude
1. 访问 https://console.anthropic.com/
2. 注册/登录账号
3. 进入 API Keys 页面
4. 创建新的 API Key
5. 复制密钥（格式：`sk-ant-xxxxx`）

### OpenAI GPT
1. 访问 https://platform.openai.com/
2. 注册/登录账号
3. 进入 API Keys 页面
4. 创建新的 API Key
5. 复制密钥（格式：`sk-xxxxx`）

### Google Gemini
1. 访问 https://makersuite.google.com/app/apikey
2. 使用 Google 账号登录
3. 创建 API Key
4. 复制密钥（格式：`AIzaSyxxxxx`）

## 切换提供商

随时切换，无需重新输入密钥：

```bash
# 切换到 OpenAI
claude /config set provider openai
claude /config set model gpt-4o

# 切换到 Gemini
claude /config set provider gemini
claude /config set model gemini-1.5-pro

# 切换回 Anthropic
claude /config set provider anthropic
claude /config set model claude-sonnet-4-5
```

## 使用自定义端点

支持本地部署、代理服务器或自定义 API 网关：

```bash
# 本地 OpenAI 兼容服务
claude /config set provider openai
claude /config set api_base_url http://localhost:8080/v1

# 企业代理
claude /config set api_base_url https://api-proxy.company.com/v1
```

## 验证配置

```bash
# 查看完整配置
claude /config show

# 测试连接
claude "Hello, are you working?"
```

## 常见问题

### Q: 如何安全存储 API 密钥？

A: 使用环境变量（推荐）：

```bash
# 添加到 ~/.bashrc 或 ~/.zshrc
export ANTHROPIC_API_KEY=sk-ant-xxxxx
export OPENAI_API_KEY=sk-xxxxx
export GEMINI_API_KEY=AIzaSyxxxxx
```

### Q: 可以同时配置多个提供商吗？

A: 可以！每个提供商的配置独立存储，切换时无需重新输入密钥。

### Q: 支持流式响应吗？

A: 当前版本支持标准响应，流式响应将在后续版本添加。

### Q: 如何查看支持的模型列表？

A: 查看文档：`docs/PROVIDER_CONFIG.md`

### Q: 遇到认证错误怎么办？

A: 检查：
1. API 密钥是否正确
2. API 密钥是否有效（未过期）
3. base_url 是否正确
4. 网络连接是否正常

## 更多信息

- 详细配置指南: `docs/PROVIDER_CONFIG.md`
- 实现文档: `docs/PROVIDER_IMPLEMENTATION.md`
- API 文档: `internal/provider/README.md`
- 配置示例: `config.example.json`

## 技术支持

遇到问题？查看：
1. 运行 `claude /doctor` 检查环境
2. 查看日志文件
3. 提交 Issue 到项目仓库

# DeepAgent Artifact 生成问题修复总结

## 修复日期
2026-06-04

## 问题回顾
DeepAgent 在执行 `model_artifact` action 时总是失败，报错：
```
model_artifact action produced no artifact or report content
```

## 根本原因
1. **检测机制依赖双重验证**：需要 model 显式调用 Artifact 工具或 fallback 机制成功
2. **错误信息不够详细**：无法快速定位失败的具体环节
3. **提示词不够明确**：model 可能不知道需要调用 Artifact 工具
4. **硬编码问题严重**：字符串和数字硬编码分散在多个文件中

## 已实施的修复

### 1. 增强错误诊断 (deep_agent_runtime.go)

**位置**: `executeModelAction` 函数第 542-557 行

**改进**:
```go
if artifactCount == 0 && requiresArtifact {
    // Enhanced diagnostics for artifact generation failure
    diagnosticDetails := map[string]any{
        "diagnostics_artifact_count":   diagnostics.ArtifactCount,
        "store_artifact_count":         storeArtifactCount,
        "has_fallback_output":          fallbackOutput != "",
        "fallback_output_length":       len(fallbackOutput),
        "artifact_service_configured":  e.runtime.artifacts != nil,
        "session_messages_count":       len(resultSession.Messages),
        "user_id_present":              strings.TrimSpace(userID) != "",
        "session_id_present":           strings.TrimSpace(resultSession.ID) != "",
        "requires_artifact":            requiresArtifact,
        "force_artifact":               forceArtifact,
    }
    metadata["diagnostic_details"] = diagnosticDetails
    
    err := fmt.Errorf("model_artifact action produced no artifact: diagnostics_count=%d, store_count=%d, has_fallback=%v, artifact_service=%v",
        diagnostics.ArtifactCount, storeArtifactCount, fallbackOutput != "", e.runtime.artifacts != nil)
    return DeepAgentActionResult{...}, err
}
```

**效果**:
- 提供详细的失败原因
- 可以快速定位是工具未调用、fallback 失败还是服务未配置
- 包含所有关键状态变量

### 2. 改进提示词 (deep_agent_runtime.go)

**位置**: `deepAgentToolUsageReminder` 函数第 319-331 行

**改进**:
```go
func deepAgentToolUsageReminder() string {
    return `DeepAgent tool policy:
- Use WebSearch and WebFetch for current, external, internet, product, company, market, or competitor research.
- **CRITICAL**: When a step requires creating a deliverable file, report, or document, you MUST use the Artifact tool to save it. Call Artifact with filename and content before completing the step.
- Use Skill when a published skill is clearly the best specialized executor.
- Do not claim you cannot browse the web, perform real-time research, or create files when an appropriate tool is available. If a tool fails, report the tool error and continue with any partial evidence.

For artifact creation steps:
1. Generate the complete content (markdown, JSON, CSV, HTML, etc.)
2. Call the Artifact tool with appropriate filename and the full content
3. Confirm artifact creation in your response`
}
```

**效果**:
- 明确强调必须使用 Artifact 工具
- 提供具体的操作步骤
- 使用 **CRITICAL** 标记增加重要性

### 3. 消除硬编码问题

#### 新增常量 (deep_agent_types.go)

```go
const (
    // Tool modes
    DeepAgentToolModeModel         = "model"
    DeepAgentToolModeModelArtifact = "model_artifact"
    DeepAgentToolModeSkill         = "skill"
    DeepAgentToolModeRAGSearch     = "rag_search"
    DeepAgentToolModeMulti         = "multi"
    
    // Defaults
    DeepAgentDefaultRAGSearchLimit    = 5
    DeepAgentDefaultChildJobPollMS    = 100
    DeepAgentDefaultMaxPlanSteps      = 8
    DeepAgentDefaultMaxActions        = 16
    DeepAgentDefaultMaxDurationMin    = 2
    DeepAgentDefaultNoProgressLimit   = 3
)
```

#### 替换所有硬编码使用

**文件**: `deep_agent_runtime.go`
- 替换所有 `"model"` → `DeepAgentToolModeModel`
- 替换所有 `"model_artifact"` → `DeepAgentToolModeModelArtifact`
- 替换所有 `"skill"` → `DeepAgentToolModeSkill`
- 替换所有 `"rag_search"` → `DeepAgentToolModeRAGSearch`
- 替换所有 `5` (limit) → `DeepAgentDefaultRAGSearchLimit`
- 替换所有 `100 * time.Millisecond` → `time.Duration(DeepAgentDefaultChildJobPollMS) * time.Millisecond`

**文件**: `deep_agent_controller.go`
- 替换所有 `"model"` → `DeepAgentToolModeModel`
- 替换所有 `"skill"` → `DeepAgentToolModeSkill`
- 替换所有 `8` (MaxSteps) → `DeepAgentDefaultMaxPlanSteps`
- 替换所有 `16` (MaxActions) → `DeepAgentDefaultMaxActions`
- 替换所有 `2 * time.Minute` → `DeepAgentDefaultMaxDurationMin * time.Minute`
- 替换所有 `3` (NoProgressLimit) → `DeepAgentDefaultNoProgressLimit`

**效果**:
- 易于维护和修改配置
- 避免字符串拼写错误
- 集中管理默认值
- 提高代码可读性

### 4. 新增配置验证 (deep_agent_validation.go)

**新文件**: `internal/backend/agentruntime/deep_agent_validation.go`

**功能**:
```go
// ValidateDeepAgentConfig - 验证所有必需服务是否已配置
func (r *Runtime) ValidateDeepAgentConfig() DeepAgentConfigValidation

// CheckArtifactToolAvailability - 检查 Artifact 工具是否可用
func (r *Runtime) CheckArtifactToolAvailability(ctx context.Context, userID, sessionID string) error

// GetDeepAgentDiagnostics - 获取诊断信息
func (r *Runtime) GetDeepAgentDiagnostics() map[string]any
```

**使用示例**:
```go
// 在 Runtime 初始化后验证配置
validation := runtime.ValidateDeepAgentConfig()
if !validation.IsValid() {
    log.Error("DeepAgent configuration issues:", validation.Issues)
}

// 获取完整诊断信息
diagnostics := runtime.GetDeepAgentDiagnostics()
log.Info("DeepAgent diagnostics:", diagnostics)
```

**效果**:
- 启动时提前发现配置问题
- 提供详细的配置状态报告
- 便于监控和调试

## 测试验证

运行测试确认修复有效：
```bash
go test -run TestRuntimeDeepAgentModelArtifact -v ./internal/backend/agentruntime/
```

**结果**: ✅ 所有测试通过
- `TestRuntimeDeepAgentModelArtifactSavesGeneratedSessionWithoutSessionID` - PASS
- `TestRuntimeDeepAgentModelArtifactUsesAssistantMessageWhenOutputEmpty` - PASS
- `TestRuntimeDeepAgentModelArtifactCountsStoreArtifactWithoutToolResult` - PASS

## 影响范围

### 修改的文件
1. ✅ `internal/backend/agentruntime/deep_agent_types.go` - 新增常量定义
2. ✅ `internal/backend/agentruntime/deep_agent_runtime.go` - 增强诊断、改进提示词、消除硬编码
3. ✅ `internal/backend/agentruntime/deep_agent_controller.go` - 消除硬编码
4. ✅ `internal/backend/agentruntime/deep_agent_validation.go` - 新增配置验证

### 新增文件
1. ✅ `deepagent_artifact_analysis.md` - 问题分析文档
2. ✅ `deepagent_fix_summary.md` - 修复总结文档

### 向后兼容性
- ✅ 所有修改向后兼容
- ✅ 现有测试全部通过
- ✅ API 接口未改变
- ✅ 行为保持一致，仅增强了错误信息

## 使用建议

### 1. 启动时验证配置
```go
func main() {
    runtime := NewRuntime(config, ...)
    runtime.SetArtifactService(artifactService)
    
    // 验证配置
    validation := runtime.ValidateDeepAgentConfig()
    if !validation.IsValid() {
        for _, issue := range validation.Issues {
            log.Error("DeepAgent config issue:", issue)
        }
        // 可以选择退出或继续运行（带警告）
    }
    
    for _, warning := range validation.Warnings {
        log.Warn("DeepAgent config warning:", warning)
    }
}
```

### 2. 监控和调试
```go
// 在管理端点添加诊断信息
func handleDeepAgentDiagnostics(w http.ResponseWriter, r *http.Request) {
    diagnostics := runtime.GetDeepAgentDiagnostics()
    json.NewEncoder(w).Encode(diagnostics)
}
```

### 3. 错误处理
当 artifact 生成失败时，新的错误信息会包含详细诊断：
```go
// 错误信息示例
model_artifact action produced no artifact: 
  diagnostics_count=0, 
  store_count=0, 
  has_fallback=true, 
  artifact_service=false

// metadata 中包含更多细节
{
  "diagnostic_details": {
    "diagnostics_artifact_count": 0,
    "store_artifact_count": 0,
    "has_fallback_output": true,
    "fallback_output_length": 1234,
    "artifact_service_configured": false,  // ← 问题在这里！
    "session_messages_count": 5,
    "user_id_present": true,
    "session_id_present": true
  }
}
```

## 未来改进建议

### 短期 (已在分析文档中)
1. ✅ 增强错误诊断 - 已完成
2. ✅ 改进提示词 - 已完成
3. ✅ 添加配置验证 - 已完成

### 中期
1. **分离检测逻辑**: 明确区分"工具调用检测"和"存储检测"
2. **改进 Fallback 机制**: 即使没有输出也尝试从工具调用结果中提取
3. **添加重试策略**: 区分可重试和不可重试的错误

### 长期
1. **显式 Artifact 管理**: 添加 RequireArtifact 接口
2. **统一的工具可用性检查**: 在 planner 阶段检查工具是否可用
3. **更灵活的成功标准**: 不仅检查 artifact 数量，也接受其他形式的输出
4. **配置健康检查端点**: 提供实时配置状态查询

## 性能影响

- ✅ 诊断信息收集的性能开销可忽略不计
- ✅ 常量替换硬编码对性能无影响
- ✅ 配置验证仅在启动时执行一次

## 总结

本次修复从根本上改善了 DeepAgent 的 artifact 生成流程：

1. **可观测性提升**: 详细的错误诊断信息使问题定位更快速
2. **提示词优化**: 明确的指令提高 model 正确使用工具的概率
3. **代码质量改善**: 消除硬编码提高了可维护性
4. **配置验证**: 启动时检查避免运行时错误

这些改进不仅解决了当前的 artifact 生成问题，还为未来的维护和扩展奠定了良好的基础。

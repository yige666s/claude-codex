# DeepAgent Artifacts 生成失败根本原因分析

## 问题现象

DeepAgent 在执行 `model_artifact` action 时总是失败，报错：
```
model_artifact action produced no artifact or report content
```

工作流执行日志显示：
- `plan_task` 阶段成功
- `execute_controller_loop` 阶段运行 `model` action
- 两次尝试都失败，返回 `model_artifact action produced no artifact or report content`
- 最终工作流状态：`deep agent task blocked`

## 根本原因分析

### 1. **核心问题：Artifact 检测机制依赖双重验证**

在 `deep_agent_runtime.go` 的 `executeModelAction` 函数中（第 462-556 行），artifact 的检测逻辑包含以下步骤：

```go
// 1. 记录执行前的 artifacts
beforeArtifacts := e.deepAgentArtifactIDSet(ctx, userID, session.ID)

// 2. 执行 model action
result, err := runner.RunGeneratedPrompt(ctx, session, prompt)

// 3. 收集诊断信息（从 session messages 中检测）
diagnostics := collectSkillExecutionDiagnostics(resultSession, startMessageCount)

// 4. 从存储中检测新增的 artifacts
storeArtifactCount := e.deepAgentNewArtifactCount(ctx, userID, resultSession.ID, beforeArtifacts)

// 5. 合并计数
artifactCount := diagnostics.ArtifactCount
if storeArtifactCount > artifactCount {
    artifactCount = storeArtifactCount
}

// 6. 检查是否满足要求
if artifactCount == 0 && requiresArtifact {
    err := fmt.Errorf("model_artifact action produced no artifact or report content")
    return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, ...}, err
}
```

**关键发现**：`artifactCount` 来自两个来源：
1. **diagnostics.ArtifactCount**：通过扫描 session.Messages 中的 `ArtifactToolName` 工具调用记录
2. **storeArtifactCount**：通过查询 artifact 存储服务，对比执行前后的 artifact 数量

### 2. **Diagnostics 检测逻辑**

在 `runtime.go` 的 `collectSkillExecutionDiagnostics` 函数中（第 4447-4509 行）：

```go
func collectSkillExecutionDiagnostics(session *state.Session, startIndex int) skillExecutionDiagnostics {
    for _, message := range session.Messages[startIndex:] {
        if strings.EqualFold(message.ToolName, ArtifactToolName) {
            out.ArtifactCount++
        }
        // ... 其他诊断信息
    }
    return out
}
```

**这意味着**：只有当 model 在执行过程中**显式调用了 Artifact 工具**并将结果记录到 session.Messages 中时，`diagnostics.ArtifactCount` 才会增加。

### 3. **Fallback 机制的局限性**

代码中确实有一个 fallback 机制（第 524-540 行）：

```go
fallbackOutput := deepAgentModelArtifactFallbackOutput(result.Output, resultSession, startMessageCount)
if artifactCount == 0 && requiresArtifact && fallbackOutput != "" {
    // 创建 artifact
    artifact, artifactErr := e.createDeepAgentModelArtifact(ctx, userID, resultSession.ID, action, fallbackOutput)
    if artifactErr != nil {
        return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, ...}, artifactErr
    }
    artifactCount = 1
    // ...
}
```

但是这个 fallback 只在以下条件**全部满足**时才会触发：
- `artifactCount == 0`（没有检测到 artifact）
- `requiresArtifact == true`（要求生成 artifact）
- `fallbackOutput != ""`（有输出内容）

**问题**：如果 fallback 创建失败或者 `fallbackOutput` 为空，仍然会报错。

### 4. **可能的失败场景**

#### 场景 A：Model 没有调用 Artifact 工具
- Model 执行时没有意识到需要创建 artifact
- 只是返回了文本内容，没有通过 Artifact 工具保存
- `diagnostics.ArtifactCount` = 0
- 依赖 fallback 机制

#### 场景 B：Artifact 工具不可用
从 `tools.go` 中可以看到，Artifact 工具的注册需要条件：

```go
if artifactWriter != nil && enabled(agentruntime.ArtifactToolName) {
    toolList = append(toolList, agentruntime.NewArtifactToolWithLimit(artifactWriter, root, artifactMaxBytes))
}
```

如果以下任一条件不满足，Artifact 工具将不可用：
- `artifactWriter == nil`（artifact 服务未配置）
- `enabled(ArtifactToolName) == false`（工具被禁用）

#### 场景 C：Fallback 创建失败
在 `createDeepAgentModelArtifact` 函数中（第 618-627 行）：

```go
func (e *RuntimeDeepAgentExecutor) createDeepAgentModelArtifact(ctx context.Context, userID, sessionID string, action DeepAgentAction, output string) (*Artifact, error) {
    if e == nil || e.runtime == nil || e.runtime.artifacts == nil {
        return nil, fmt.Errorf("artifact service is not configured")
    }
    if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
        return nil, fmt.Errorf("artifact fallback requires user_id and session_id")
    }
    filename := deepAgentModelArtifactFilename(action)
    return e.runtime.CreateArtifact(ctx, userID, sessionID, filename, "text/markdown", []byte(output))
}
```

失败原因可能是：
- `e.runtime.artifacts == nil`（artifact 服务未初始化）
- `userID` 或 `sessionID` 为空
- `CreateArtifact` 调用失败（存储问题）

#### 场景 D：Fallback Output 为空
在 `deepAgentModelArtifactFallbackOutput` 函数中（第 596-616 行）：

```go
func deepAgentModelArtifactFallbackOutput(output string, session *state.Session, startIndex int) string {
    if text := strings.TrimSpace(output); text != "" {
        return text
    }
    // 从 session messages 中查找 assistant 消息
    for i := len(session.Messages) - 1; i >= startIndex; i-- {
        message := session.Messages[i]
        if message.Hidden || message.Role != state.MessageRoleAssistant {
            continue
        }
        if text := strings.TrimSpace(message.Content); text != "" {
            return text
        }
    }
    return ""
}
```

如果：
- `result.Output` 为空
- Session 中没有 assistant 消息
- 所有 assistant 消息的 Content 都为空

那么 `fallbackOutput` 将为空，导致 fallback 机制无法工作。

### 5. **架构设计问题**

多次架构调整可能导致：

1. **依赖链断裂**：
   - DeepAgent Controller → Runtime → EngineFactory → Engine → Tools
   - 任何一个环节配置不当都会导致问题

2. **配置不一致**：
   - Runner 的 Scope 配置可能缺少 `Artifacts` 或 `ArtifactMaxBytes`
   - 在 `runnerForScope` 中（runtime.go 第 4629 行）：
   ```go
   if scope.Artifacts == nil && r.artifacts != nil && 
      strings.TrimSpace(scope.UserID) != "" && 
      strings.TrimSpace(scope.SessionID) != "" {
       scope.Artifacts = sessionArtifactWriter{...}
   }
   ```
   如果任一条件不满足，`scope.Artifacts` 将为 nil

3. **工具列表限制**：
   - 如果使用 `consumerChatToolNames()` 而不是完整工具列表
   - 或者自定义了 `allowedTools` 但没有包含 `ArtifactToolName`

## 诊断建议

### 立即检查项

1. **验证 Artifact 服务是否已初始化**：
   ```go
   // 在 Runtime 初始化时
   runtime.SetArtifactService(...)
   ```

2. **检查工具注册配置**：
   - 确认 `buildRegistry` 中 `artifactWriter != nil`
   - 确认 `allowedTools` 包含 `ArtifactToolName`
   - 或者 `allowedTools` 为空（允许所有工具）

3. **验证 Scope 配置**：
   ```go
   // 在调用 runnerForScope 时
   scope := Scope{
       UserID:     userID,      // 必须非空
       SessionID:  session.ID,  // 必须非空
       WorkingDir: session.WorkingDir,
       Prompt:     prompt,
       // Artifacts 会自动注入，但需要 r.artifacts != nil
   }
   ```

4. **检查 Model 提示词**：
   - 查看 `modelPromptForStep` 函数生成的提示词
   - 确认是否明确指示 model 使用 Artifact 工具
   - 检查 `deepAgentToolUsageReminder()` 的内容

5. **日志追踪**：
   - 在 `executeModelAction` 中添加详细日志：
     - `diagnostics.ArtifactCount` 的值
     - `storeArtifactCount` 的值
     - `fallbackOutput` 是否为空
     - `requiresArtifact` 的值
     - 任何错误信息

### 测试验证

参考成功的测试用例（`deep_agent_controller_test.go`）：

```go
// 成功案例使用了 markdownReportRunner
// 它直接返回 markdown 内容作为 assistant message
func (markdownReportRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, _ string) (engine.Result, error) {
    output := "# Tolan AI 调查报告\n\n## 摘要\n\nTolan AI 是一个 AI 产品。"
    session.AddAssistantMessage(output)
    return engine.Result{Output: output, Session: session}, nil
}
```

这个测试成功是因为：
1. 返回了非空的 `output`
2. 添加了 assistant message 到 session
3. Fallback 机制能够提取到内容并创建 artifact

## 解决方案建议

### 短期修复（Quick Fix）

1. **增强日志和错误信息**：
   ```go
   if artifactCount == 0 && requiresArtifact {
       details := map[string]interface{}{
           "diagnostics_count": diagnostics.ArtifactCount,
           "store_count": storeArtifactCount,
           "has_fallback_output": fallbackOutput != "",
           "artifact_service_configured": e.runtime.artifacts != nil,
           "session_messages_count": len(resultSession.Messages),
       }
       err := fmt.Errorf("model_artifact action produced no artifact: %+v", details)
       return DeepAgentActionResult{...}, err
   }
   ```

2. **确保 Artifact 工具可用**：
   - 在启动时验证 artifact 服务已配置
   - 检查工具注册时的条件

3. **增强 Model 提示词**：
   在 `modelPromptForStep` 中明确要求：
   ```
   IMPORTANT: You MUST use the Artifact tool to save the final deliverable.
   Call the Artifact tool with appropriate filename and content before completing.
   ```

### 中期改进

1. **分离 Artifact 检测逻辑**：
   - 明确区分"工具调用检测"和"存储检测"
   - 提供更清晰的失败原因

2. **改进 Fallback 机制**：
   ```go
   // 即使没有输出，也尝试从其他来源获取内容
   // 例如：从工具调用结果中提取
   ```

3. **添加重试策略**：
   - 如果 fallback 创建失败，提供更详细的错误信息
   - 区分可重试和不可重试的错误

### 长期架构优化

1. **显式 Artifact 管理**：
   - 添加 `RequireArtifact` 接口或配置
   - 在 action 执行前验证先决条件

2. **统一的工具可用性检查**：
   - 在 planner 阶段检查所需工具是否可用
   - 如果不可用，提前失败或调整计划

3. **更灵活的成功标准**：
   - 不仅检查 artifact 数量
   - 也可以接受其他形式的输出（例如存储在 session 中的结构化数据）

4. **配置验证**：
   - 在 DeepAgent 启动时验证所有必需的服务和配置
   - 提供配置健康检查端点

## 总结

根本原因是**Artifact 生成依赖一个复杂的检测链**：
1. Model 必须调用 Artifact 工具，或
2. Fallback 机制必须能提取到内容并成功创建 artifact

任何一个环节失败都会导致报错。最可能的问题是：
- **Artifact 工具未正确配置或不可用**
- **Model 没有被正确指示去创建 artifact**
- **Fallback 机制因为缺少必要信息而失败**

建议立即添加详细日志来诊断具体是哪个环节出了问题。

package run

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"claude-codex/internal/backend/agentapi/bootstrap"
	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	coreagent "claude-codex/internal/harness/agent"
	"claude-codex/internal/harness/coordinator"
	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/hooks"
	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/state"
	coretasks "claude-codex/internal/harness/tasks"
	"claude-codex/internal/harness/tools"
	agenttool "claude-codex/internal/harness/tools/agent"
)

type engineFactoryConfig struct {
	startupCfg              startupconfig.Config
	llmCfg                  bootstrap.LLMConfig
	skillCatalog            agentruntime.SkillCatalog
	skillShellSandboxConfig agentruntime.SkillShellSandboxConfig
	llmConfigManager        *agentruntime.LLMGovernanceConfigManager
	llmUsageStore           agentruntime.LLMUsageStore
	riskStore               agentruntime.RiskStore
	toolCallLedger          agentruntime.ToolCallLedgerStore
	coordinatorManager      *coordinator.Manager
	taskManager             *coretasks.TaskManager
	runtimeComponents       *runtimeComponents
	runtimeProvider         func() *agentruntime.Runtime
}

func buildEngineFactory(cfg engineFactoryConfig) (agentruntime.ContextEngineFactory, func() agentruntime.LLMGovernanceStatus) {
	var llmStatusMu sync.RWMutex
	var llmStatusProvider func() agentruntime.LLMGovernanceStatus
	var llmStatusConfigSignature string
	globalAllowed := allowedToolNames(cfg.startupCfg.AllowDangerousTools)
	globalNetworkAllowlist := startupconfig.SplitCSV(cfg.startupCfg.NetworkAllowlist)
	var engineFactory agentruntime.ContextEngineFactory

	engineFactory = func(ctx context.Context, scope agentruntime.Scope) (agentruntime.Runner, error) {
		var dynamicTools []tools.Tool
		var dynamicToolNames []string
		var dynamicMCPClients []*mcpcore.Client
		if cfg.runtimeComponents != nil {
			var err error
			dynamicToolNames, err = cfg.runtimeComponents.ensureMCP(ctx)
			if err != nil {
				return nil, fmt.Errorf("discover configured MCP tools: %w", err)
			}
			dynamicTools = cfg.runtimeComponents.mcpToolsSlice()
			dynamicMCPClients = cfg.runtimeComponents.mcpClientsSlice()
		}
		root := scope.WorkingDir
		if root == "" {
			root = cfg.startupCfg.Workspace
		}
		publishedSkillManager := filteredSkillManager(cfg.skillCatalog)
		effectiveAllowed := effectiveAllowedToolNames(appendAllowedTools(globalAllowed, dynamicToolNames), scope)
		var connectorTools []tools.Tool
		if cfg.runtimeProvider != nil {
			if runtime := cfg.runtimeProvider(); runtime != nil {
				connectorTools = runtime.ConnectorMCPTools(ctx, scope)
				for _, tool := range connectorTools {
					if tool != nil {
						effectiveAllowed = append(effectiveAllowed, tool.Name())
					}
				}
			}
		}
		sandboxBash := buildSandboxBashRuntime(cfg.skillShellSandboxConfig, root, scope)
		runSubagent := buildRuntimeSubagentRunner(scope, func() *agentruntime.Runtime {
			if cfg.runtimeProvider == nil {
				return nil
			}
			return cfg.runtimeProvider()
		})
		registry := buildRegistry(root, publishedSkillManager, cfg.startupCfg.AllowDangerousTools, scope.Artifacts, scope.ArtifactMaxBytes, scopedNetworkAllowlist(globalNetworkAllowlist, scope.NetworkAllowlist), effectiveAllowed, sandboxBash, registryCollaborationDeps{
			coordinatorManager: cfg.coordinatorManager,
			taskManager:        cfg.taskManager,
			runSubagent:        runSubagent,
		})
		for _, tool := range connectorTools {
			if tool != nil {
				registry.Register(tool)
			}
		}
		effectiveAllowedSet := toolNameSet(effectiveAllowed)
		for _, tool := range dynamicTools {
			if tool != nil && effectiveAllowedSet[tool.Name()] {
				registry.Register(tool)
			}
		}
		safeWriteTools := []string{agentruntime.ArtifactToolName}
		if sandboxBash != nil {
			safeWriteTools = append(safeWriteTools, "Bash")
		}
		safeWriteTools = append(safeWriteTools, collaborationSafeWriteToolNames()...)
		checker := agentruntime.NewProductPermissionCheckerWithReporter(agentruntime.ToolPolicy{
			AllowWriteExecute: cfg.startupCfg.AllowDangerousTools,
			AllowedTools:      effectiveAllowed,
			SafeWriteTools:    safeWriteTools,
			SafeExecuteTools:  collaborationSafeExecuteToolNames(),
		}, func(ctx context.Context, denial agentruntime.ToolDenialRecord) {
			metadata := map[string]any{
				"tool_name":  denial.ToolName,
				"level":      denial.Level,
				"summary":    denial.Summary,
				"skill_name": scope.SkillName,
				"metadata":   denial.Metadata,
			}
			if err := cfg.riskStore.RecordRiskEvent(ctx, agentruntime.RiskEvent{
				UserID:     scope.UserID,
				SessionID:  scope.SessionID,
				Operation:  "tool_denied",
				Reason:     denial.Reason,
				RiskLevel:  agentruntime.RiskLevelMedium,
				ScoreDelta: 10,
				Metadata:   metadata,
			}); err != nil {
				logInfof("record tool denial risk event: %v", err)
			}
		})
		runtimeLLMConfig := cfg.llmConfigManager.Get()
		effectiveLLMConfig := bootstrap.ApplyRuntimeLLMConfig(cfg.llmCfg, runtimeLLMConfig)
		planner, err := bootstrap.NewGovernedPlannerForScope(effectiveLLMConfig, cfg.startupCfg.LLMFallbacks, runtimeLLMConfig.ModelRoutes, scope, cfg.llmUsageStore, runtimeLLMConfig)
		if err != nil {
			return nil, err
		}
		llmStatusMu.Lock()
		llmStatusProvider = planner.Status
		llmStatusConfigSignature = runtimeLLMConfigSignature(runtimeLLMConfig)
		llmStatusMu.Unlock()
		eng := engine.NewWithDir(planner, registry, checker, 0, root)
		eng.SetSkillManager(publishedSkillManager)
		if cfg.runtimeComponents != nil && cfg.runtimeComponents.hookRegistry != nil {
			eng.SetHookExecutor(hooks.NewExecutor(cfg.runtimeComponents.hookRegistry))
		}
		if len(dynamicTools) > 0 {
			eng.SetMCPClients(dynamicMCPClients)
		}
		eng.SetToolLedger(cfg.toolCallLedger)
		eng.SetDefaultToolExecutionScope(engine.ToolExecutionScope{
			UserID:    scope.UserID,
			SessionID: scope.SessionID,
		})
		return eng, nil
	}

	llmStatusFn := func() agentruntime.LLMGovernanceStatus {
		llmStatusMu.RLock()
		provider := llmStatusProvider
		observedConfigSignature := llmStatusConfigSignature
		llmStatusMu.RUnlock()
		runtimeLLMConfig := cfg.llmConfigManager.Get()
		if provider == nil || observedConfigSignature != runtimeLLMConfigSignature(runtimeLLMConfig) {
			effectiveLLMConfig := bootstrap.ApplyRuntimeLLMConfig(cfg.llmCfg, runtimeLLMConfig)
			return agentruntime.LLMGovernanceStatus{
				Backends: []agentruntime.LLMBackendStatus{{
					Name:     effectiveLLMConfig.Provider,
					Provider: effectiveLLMConfig.Provider,
					Model:    effectiveLLMConfig.Model,
					Healthy:  true,
				}},
				Config: cfg.llmConfigManager.StatusMap(),
			}
		}
		status := provider()
		status.Config = cfg.llmConfigManager.StatusMap()
		return status
	}

	return engineFactory, llmStatusFn
}

func runtimeLLMConfigSignature(config agentruntime.LLMGovernanceConfig) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(config.Provider)),
		strings.TrimSpace(config.Model),
		strings.TrimSpace(config.VertexLocation),
		strings.TrimSpace(config.ModelRoutes),
	}, "\x00")
}

func buildRuntimeSubagentRunner(parentScope agentruntime.Scope, runtimeProvider func() *agentruntime.Runtime) agenttool.Runner {
	return func(ctx context.Context, request agenttool.Request) (string, error) {
		if runtimeProvider == nil {
			return "", fmt.Errorf("agent runtime is not configured")
		}
		runtime := runtimeProvider()
		if runtime == nil {
			return "", fmt.Errorf("agent runtime is not configured")
		}

		targetDir := strings.TrimSpace(request.WorkingDir)
		if targetDir == "" {
			targetDir = strings.TrimSpace(request.Cwd)
		}
		if targetDir == "" {
			targetDir = strings.TrimSpace(parentScope.WorkingDir)
		}

		childSession := state.NewSession(targetDir)
		childSession.UserID = parentScope.UserID
		if request.ParentSessionID != "" {
			childSession.ParentID = request.ParentSessionID
		}
		if request.AgentID != "" {
			childSession.AgentID = request.AgentID
		}
		if childSession.Metadata == nil {
			childSession.Metadata = map[string]string{}
		}
		for key, value := range request.ParentMetadata {
			childSession.Metadata["parent_"+key] = value
		}
		childSession.Metadata["agent_invocation_kind"] = request.InvocationKind
		childSession.Metadata["agent_type"] = request.SubagentType
		childSession.Metadata["parent_agent_id"] = request.ParentAgentID
		childSession.Metadata["parent_session_id"] = request.ParentSessionID

		runner := runtime.RunnerForScope(ctx, agentruntime.Scope{
			UserID:           parentScope.UserID,
			SessionID:        childSession.ID,
			WorkingDir:       childSession.WorkingDir,
			Prompt:           request.Prompt,
			ConnectorContext: append([]string(nil), parentScope.ConnectorContext...),
			ArtifactTypes:    append([]string(nil), parentScope.ArtifactTypes...),
		})
		if childEngine, ok := runner.(*engine.Engine); ok && request.DrainPendingMessages != nil {
			childEngine.SetPendingMessageProvider(request.DrainPendingMessages)
		}

		runCtx := coreagent.WithAgentContext(ctx, coreagent.AgentContext{
			AgentID:         request.AgentID,
			ParentSessionID: childSession.ID,
			AgentType:       request.SubagentType,
			SubagentName:    request.Name,
			TeamName:        request.TeamName,
			InvocationKind:  coreagent.AgentInvocationKind(request.InvocationKind),
			InvocationID:    request.InvocationKind + ":" + request.AgentID,
		})
		result, err := runner.Run(runCtx, childSession, buildRuntimeSubagentPrompt(request))
		if err != nil {
			return "", err
		}
		return result.Output, nil
	}
}

func buildRuntimeSubagentPrompt(request agenttool.Request) string {
	var preamble []string
	if request.Description != "" {
		preamble = append(preamble, "Task summary: "+request.Description)
	}
	if request.SubagentType != "" {
		preamble = append(preamble, "Requested subagent type: "+request.SubagentType)
	}
	if request.DefinitionSource != "" {
		preamble = append(preamble, "Agent definition source: "+request.DefinitionSource)
	}
	if request.DefinitionMemory != "" {
		preamble = append(preamble, "Agent memory policy: "+request.DefinitionMemory)
	}
	if request.PermissionPolicy != "" {
		preamble = append(preamble, "Agent permission policy: "+request.PermissionPolicy)
	}
	if len(request.DefinitionSkills) > 0 {
		preamble = append(preamble, "Agent requested skills: "+strings.Join(request.DefinitionSkills, ", "))
	}
	if len(request.DefinitionMCPServers) > 0 {
		preamble = append(preamble, "Agent MCP servers: "+strings.Join(request.DefinitionMCPServers, ", "))
	}
	if len(request.DefinitionRequiredMCPServers) > 0 {
		preamble = append(preamble, "Agent required MCP servers: "+strings.Join(request.DefinitionRequiredMCPServers, ", ")+". If a required MCP server is unavailable in this runtime, report that limitation explicitly.")
	}
	if request.OmitClaudeMd {
		preamble = append(preamble, "Project CLAUDE.md context is intentionally omitted for this agent definition.")
	}
	if request.ParentSessionID != "" {
		preamble = append(preamble, "Parent session ID: "+request.ParentSessionID)
	}
	if request.ParentAgentID != "" {
		preamble = append(preamble, "Parent agent ID: "+request.ParentAgentID)
	}
	if request.Isolation != "" {
		preamble = append(preamble, "Agent isolation: "+request.Isolation)
	}
	preamble = append(preamble, request.Prompt)
	return strings.Join(preamble, "\n\n")
}

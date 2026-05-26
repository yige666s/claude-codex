package run

import (
	"context"
	"sync"

	"claude-codex/internal/backend/agentapi/bootstrap"
	startupconfig "claude-codex/internal/backend/agentapi/config"
	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/harness/engine"
)

type engineFactoryConfig struct {
	startupCfg              startupconfig.Config
	llmCfg                  bootstrap.LLMConfig
	skillCatalog            agentruntime.SkillCatalog
	skillShellSandboxConfig agentruntime.SkillShellSandboxConfig
	llmConfigManager        *agentruntime.LLMGovernanceConfigManager
	llmUsageStore           agentruntime.LLMUsageStore
	riskStore               agentruntime.RiskStore
}

func buildEngineFactory(cfg engineFactoryConfig) (func(agentruntime.Scope) agentruntime.Runner, func() agentruntime.LLMGovernanceStatus) {
	var llmStatusMu sync.RWMutex
	var llmStatusProvider func() agentruntime.LLMGovernanceStatus
	globalAllowed := allowedToolNames(cfg.startupCfg.AllowDangerousTools)
	globalNetworkAllowlist := startupconfig.SplitCSV(cfg.startupCfg.NetworkAllowlist)

	engineFactory := func(scope agentruntime.Scope) agentruntime.Runner {
		root := scope.WorkingDir
		if root == "" {
			root = cfg.startupCfg.Workspace
		}
		publishedSkillManager := filteredSkillManager(cfg.skillCatalog)
		effectiveAllowed := effectiveAllowedToolNames(globalAllowed, scope)
		sandboxBash := buildSandboxBashRuntime(cfg.skillShellSandboxConfig, root, scope)
		registry := buildRegistry(root, publishedSkillManager, cfg.startupCfg.AllowDangerousTools, scope.Artifacts, scope.ArtifactMaxBytes, scopedNetworkAllowlist(globalNetworkAllowlist, scope.NetworkAllowlist), effectiveAllowed, sandboxBash)
		safeWriteTools := []string{agentruntime.ArtifactToolName}
		if sandboxBash != nil {
			safeWriteTools = append(safeWriteTools, "Bash")
		}
		checker := agentruntime.NewProductPermissionCheckerWithReporter(agentruntime.ToolPolicy{
			AllowWriteExecute: cfg.startupCfg.AllowDangerousTools,
			AllowedTools:      effectiveAllowed,
			SafeWriteTools:    safeWriteTools,
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
			logFatal(err)
		}
		llmStatusMu.Lock()
		llmStatusProvider = planner.Status
		llmStatusMu.Unlock()
		eng := engine.NewWithDir(planner, registry, checker, 0, root)
		eng.SetSkillManager(publishedSkillManager)
		return eng
	}

	llmStatusFn := func() agentruntime.LLMGovernanceStatus {
		llmStatusMu.RLock()
		provider := llmStatusProvider
		llmStatusMu.RUnlock()
		if provider == nil {
			runtimeLLMConfig := cfg.llmConfigManager.Get()
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

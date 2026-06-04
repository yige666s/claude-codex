package agentruntime

import (
	"context"
	"fmt"
)

// DeepAgentConfigValidation holds validation results for DeepAgent configuration
type DeepAgentConfigValidation struct {
	ArtifactServiceConfigured bool
	SessionStoreConfigured    bool
	EngineFactoryConfigured   bool
	SkillsConfigured          bool
	Issues                    []string
	Warnings                  []string
}

// ValidateDeepAgentConfig validates that all required services are configured for DeepAgent
func (r *Runtime) ValidateDeepAgentConfig() DeepAgentConfigValidation {
	result := DeepAgentConfigValidation{}

	// Check artifact service
	if r.artifacts == nil {
		result.Issues = append(result.Issues, "Artifact service is not configured - model_artifact actions will fail")
	} else {
		result.ArtifactServiceConfigured = true
	}

	// Check session store
	if r.sessions == nil {
		result.Issues = append(result.Issues, "Session store is not configured")
	} else {
		result.SessionStoreConfigured = true
	}

	// Check engine factory
	if r.engineFactory == nil {
		result.Issues = append(result.Issues, "Engine factory is not configured")
	} else {
		result.EngineFactoryConfigured = true
	}

	// Check skills (warning only, not critical)
	if r.skills == nil {
		result.Warnings = append(result.Warnings, "Skills manager is not configured - skill actions will be limited")
	} else {
		result.SkillsConfigured = true
	}

	return result
}

// IsValid returns true if all critical components are configured
func (v DeepAgentConfigValidation) IsValid() bool {
	return len(v.Issues) == 0
}

// CheckArtifactToolAvailability checks if the Artifact tool is available in the runner's scope
func (r *Runtime) CheckArtifactToolAvailability(ctx context.Context, userID, sessionID string) error {
	if r.artifacts == nil {
		return fmt.Errorf("artifact service is not configured in runtime")
	}

	// Verify that userID and sessionID are provided (required for artifact writer injection)
	if userID == "" || sessionID == "" {
		return fmt.Errorf("artifact tool may not be available: missing userID or sessionID")
	}

	// Artifact writer should be injected by runnerForScope when both are present
	return nil
}

// GetDeepAgentDiagnostics returns diagnostic information about DeepAgent configuration
func (r *Runtime) GetDeepAgentDiagnostics() map[string]any {
	validation := r.ValidateDeepAgentConfig()

	return map[string]any{
		"artifact_service_configured": validation.ArtifactServiceConfigured,
		"session_store_configured":    validation.SessionStoreConfigured,
		"engine_factory_configured":   validation.EngineFactoryConfigured,
		"skills_configured":           validation.SkillsConfigured,
		"is_valid":                    validation.IsValid(),
		"issues":                      validation.Issues,
		"warnings":                    validation.Warnings,
		"default_max_steps":           DeepAgentDefaultMaxPlanSteps,
		"default_max_actions":         DeepAgentDefaultMaxActions,
		"default_max_duration_min":    DeepAgentDefaultMaxDurationMin,
		"default_no_progress_limit":   DeepAgentDefaultNoProgressLimit,
	}
}

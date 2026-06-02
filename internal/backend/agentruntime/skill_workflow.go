package agentruntime

import (
	"context"
	"fmt"
	"strings"

	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
)

const (
	skillExecutionWorkflowName    = "skill_execution"
	skillExecutionWorkflowVersion = "v1"
)

func skillExecutionWorkflowDefinition() WorkflowDefinition {
	return WorkflowDefinition{
		Name:    skillExecutionWorkflowName,
		Version: skillExecutionWorkflowVersion,
		Steps: []WorkflowStepDefinition{
			{Name: "resolve_skill"},
			{Name: "policy_check"},
			{Name: "prepare_sandbox"},
			{Name: "execute_skill"},
			{Name: "collect_result"},
		},
	}
}

func (r *Runtime) executeSkillWorkflow(ctx context.Context, userID string, session *state.Session, skill *skills.SkillDefinition, args string, onToken func(string)) (runnerResult, error) {
	if r == nil || skill == nil {
		return runnerResult{}, fmt.Errorf("skill is required")
	}
	store := r.workflowStore
	if store == nil {
		store = NewMemoryWorkflowStore()
	}
	engine := NewWorkflowEngine(store, ContextWorkflowEventSink{})
	var result runnerResult
	var runErr error
	var policy SkillRuntimePolicy
	engine.RegisterStepHandler("resolve_skill", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		if r.skills == nil {
			return nil, fmt.Errorf("skills are not configured")
		}
		current, ok := r.skills.GetSkill(skill.Name)
		if !ok || current == nil {
			return nil, fmt.Errorf("unknown skill: /%s", skill.Name)
		}
		if !current.UserInvocable {
			return nil, fmt.Errorf("skill /%s is not user-invocable", skill.Name)
		}
		return map[string]any{
			"skill_name":        current.Name,
			"display_name":      current.DisplayName,
			"execution_context": string(current.ExecutionContext),
			"run_as_job":        current.RunAsJob,
			"produces_artifact": skillProducesArtifacts(current),
		}, nil
	})
	engine.RegisterStepHandler("policy_check", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		policy = r.skillRuntimePolicy(skill)
		return map[string]any{
			"allowed_tools":     policy.AllowedTools,
			"allowed_env":       policy.AllowedEnv,
			"network_allowlist": policy.NetworkAllowlist,
			"artifact_types":    policy.ArtifactTypes,
			"shell_timeout_ms":  policy.ShellTimeout.Milliseconds(),
		}, nil
	})
	engine.RegisterStepHandler("prepare_sandbox", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		sandbox := applySkillSandboxPolicy(r.config.SkillShellSandbox, policy.Sandbox)
		workspace := r.sandboxedWorkingDir(userID, session.WorkingDir)
		if strings.TrimSpace(workspace) == "" {
			workspace = r.config.DefaultWorkingDir
		}
		return map[string]any{
			"workspace":       workspace,
			"skill_root":      firstNonEmptyString(skill.SkillRoot, workspace),
			"sandbox_runner":  sandbox.Runner,
			"sandbox_network": sandbox.Network,
			"sandbox_image":   sandbox.Image,
		}, nil
	})
	engine.RegisterStepHandler("execute_skill", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		result, runErr = r.runSkillDirect(ctx, userID, session, skill, args, onToken)
		output := map[string]any{
			"output_length": len(result.Output),
			"has_session":   result.Session != nil,
			"job_started":   result.Job != nil,
		}
		if result.Job != nil {
			output["job_id"] = result.Job.ID
			output["job_type"] = result.Job.Type
		}
		return output, runErr
	})
	engine.RegisterStepHandler("collect_result", func(ctx context.Context, run *WorkflowRun, input map[string]any) (map[string]any, error) {
		diagnostics := skillExecutionDiagnostics{}
		if result.Session != nil {
			diagnostics = collectSkillExecutionDiagnostics(result.Session, 0)
		}
		return map[string]any{
			"artifact_count": diagnostics.ArtifactCount,
			"error_kind":     diagnostics.ErrorKind,
			"provider":       diagnostics.Provider,
			"model":          diagnostics.Model,
		}, nil
	})
	_, err := engine.Execute(ctx, WorkflowRequest{
		Definition: skillExecutionWorkflowDefinition(),
		UserID:     userID,
		SessionID:  session.ID,
		JobID:      jobIDFromContext(ctx),
		State: map[string]any{
			"skill_name":    skill.Name,
			"args_length":   len(args),
			"input_summary": summarizeSkillInput(args),
		},
	})
	if err != nil {
		return result, err
	}
	return result, runErr
}

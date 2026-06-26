package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	deepAgentErrorPolicyBlocked = "policy_blocked"
	deepAgentSideEffectReadonly = "readonly"
	deepAgentSideEffectWrite    = "workspace-write"
)

type runtimeDeepAgentTestExecutor struct {
	runtime *Runtime
}

func (e *runtimeDeepAgentTestExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentStepEvidence, error) {
	route = finalizeDeepAgentActionRoute(route, action)
	if deepAgentBool(action.Args, "state_verification", false) || deepAgentBool(action.Args, "verify_state", false) {
		return deepAgentStateVerificationEvidence(route, action, agentState)
	}
	command, args, display, err := deepAgentCommandFromAction(action)
	if err != nil {
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       deepAgentErrorPolicyBlocked,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	allowed := deepAgentAllowedCommandPatterns(action, []string{
		"go test *",
		"go vet *",
		"git diff --check",
		"npm test *",
		"npm run *",
		"npm --prefix *",
		"pnpm test *",
		"pnpm --dir *",
	})
	if !deepAgentCommandAllowed(display, allowed) {
		err := fmt.Errorf("test command is not allowed by deep agent policy: %s", display)
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"command":           display,
			"allowed_commands":  allowed,
			"error_class":       deepAgentErrorPolicyBlocked,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	workingDir, err := deepAgentExecutorWorkingDir(action)
	if err != nil {
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"command":           display,
			"error_class":       deepAgentErrorPolicyBlocked,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	timeout := time.Duration(deepAgentActionInt(action, "timeout_ms", 120000)) * time.Millisecond
	if timeout <= 0 || timeout > 10*time.Minute {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	started := time.Now().UTC()
	cmd := exec.CommandContext(runCtx, command, args...)
	cmd.Dir = workingDir
	output, runErr := cmd.CombinedOutput()
	duration := time.Since(started)
	exitCode := 0
	if runErr != nil {
		exitCode = 1
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if runCtx.Err() == context.DeadlineExceeded {
			runErr = fmt.Errorf("test command timed out after %s", timeout)
		}
	}
	outputText := truncateDeepAgentDiagnosticText(string(output), 4000)
	status := DeepAgentActionStatusSucceeded
	completed := true
	errorClass := ""
	if runErr != nil {
		status = DeepAgentActionStatusFailed
		completed = false
		errorClass = DeepAgentErrorDeterministic
	}
	return deepAgentDedicatedEvidence(route, action, status, outputText, map[string]any{
		"command":             display,
		"working_dir":         workingDir,
		"exit_code":           exitCode,
		"duration_ms":         duration.Milliseconds(),
		"stdout_stderr":       outputText,
		"failure_excerpt":     deepAgentFailureExcerpt(outputText),
		"completed":           completed,
		"error_class":         errorClass,
		"side_effect_level":   deepAgentSideEffectReadonly,
		"dedicated_executor":  deepAgentRouteExecutorTest,
		"allowlist_enforced":  true,
		"allowed_commands":    allowed,
		"tool_result_valid":   runErr == nil,
		"verification_source": "process_exit",
	}), runErr
}

func deepAgentStateVerificationEvidence(route DeepAgentStepRoute, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentStepEvidence, error) {
	if agentState == nil {
		err := fmt.Errorf("state verification requires deep agent state")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       DeepAgentErrorValidation,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	verifyState := cloneDeepAgentStateForVerification(agentState, firstNonEmptyString(route.StepID, action.StepID))
	final := verifyDeepAgentFinalState(verifyState)
	output := deepAgentFinalVerificationOutput(final)
	status := DeepAgentActionStatusSucceeded
	var execErr error
	errorClass := ""
	if !final.Done {
		status = DeepAgentActionStatusFailed
		errorClass = DeepAgentErrorValidation
		execErr = fmt.Errorf("%s", firstNonEmptyString(final.Reason, "deep agent state verification failed"))
	}
	evidence := deepAgentDedicatedEvidence(route, action, status, output, map[string]any{
		"completed":               final.Done,
		"error_class":             errorClass,
		"side_effect_level":       deepAgentSideEffectReadonly,
		"dedicated_executor":      deepAgentRouteExecutorTest,
		"tool_result_valid":       final.Done,
		"verification_source":     "deep_agent_state",
		"verification_checks":     final.Checks,
		"verification_missing":    final.Missing,
		"verification_reason":     final.Reason,
		"verification_confidence": final.Confidence,
	})
	evidence.Artifacts = deepAgentStateCurrentArtifactRefs(agentState)
	for _, item := range (StateDeepAgentEvidenceStore{}).ListStepEvidence(agentState) {
		evidence.Sources = append(evidence.Sources, item.Sources...)
	}
	return evidence, execErr
}

func cloneDeepAgentStateForVerification(state *DeepAgentState, currentStepID string) *DeepAgentState {
	if state == nil {
		return nil
	}
	clone := *state
	clone.Plan = state.Plan
	if len(state.Plan.Steps) > 0 {
		clone.Plan.Steps = append([]DeepAgentStep(nil), state.Plan.Steps...)
		for i := range clone.Plan.Steps {
			if clone.Plan.Steps[i].ID == currentStepID {
				clone.Plan.Steps[i].Status = DeepAgentStepStatusSucceeded
			}
		}
	}
	return &clone
}

func deepAgentFinalVerificationOutput(final DeepAgentFinalVerification) string {
	var b strings.Builder
	if final.Done {
		b.WriteString("DeepAgent state verification passed.")
	} else {
		b.WriteString("DeepAgent state verification failed.")
	}
	if strings.TrimSpace(final.Reason) != "" {
		b.WriteString("\nReason: ")
		b.WriteString(final.Reason)
	}
	for _, check := range final.Checks {
		b.WriteString("\n- ")
		if check.Passed {
			b.WriteString("PASS ")
		} else {
			b.WriteString("FAIL ")
		}
		b.WriteString(check.Name)
		if strings.TrimSpace(check.Reason) != "" {
			b.WriteString(": ")
			b.WriteString(check.Reason)
		}
	}
	return b.String()
}

type runtimeDeepAgentWebExecutor struct {
	runtime *Runtime
}

func (e *runtimeDeepAgentWebExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, _ *DeepAgentState) (DeepAgentStepEvidence, error) {
	route = finalizeDeepAgentActionRoute(route, action)
	rawURL := firstNonEmptyString(deepAgentActionString(action, "url"), deepAgentActionString(action, "target_url"), deepAgentActionString(action, "input"))
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		if err == nil {
			err = fmt.Errorf("web executor requires an http(s) url")
		}
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"url":               rawURL,
			"error_class":       deepAgentErrorPolicyBlocked,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	timeout := time.Duration(deepAgentActionInt(action, "timeout_ms", 30000)) * time.Millisecond
	if timeout <= 0 || timeout > 2*time.Minute {
		timeout = 30 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"url":               parsed.String(),
			"error_class":       DeepAgentErrorDeterministic,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	started := time.Now().UTC()
	resp, err := client.Do(req)
	duration := time.Since(started)
	if err != nil {
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"url":                parsed.String(),
			"duration_ms":        duration.Milliseconds(),
			"error_class":        DeepAgentErrorTransient,
			"side_effect_level":  deepAgentSideEffectReadonly,
			"dedicated_executor": deepAgentRouteExecutorWeb,
		}), err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		err = readErr
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"url":               parsed.String(),
			"status_code":       resp.StatusCode,
			"error_class":       DeepAgentErrorTransient,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	bodyText := strings.TrimSpace(string(body))
	snippet := truncateDeepAgentDiagnosticText(bodyText, 1200)
	passed := resp.StatusCode >= 200 && resp.StatusCode < 400
	status := DeepAgentActionStatusSucceeded
	var execErr error
	errorClass := ""
	if !passed {
		status = DeepAgentActionStatusFailed
		errorClass = DeepAgentErrorDeterministic
		execErr = fmt.Errorf("web verification returned status %d", resp.StatusCode)
	}
	evidence := deepAgentDedicatedEvidence(route, action, status, fmt.Sprintf("GET %s -> %d\n%s", parsed.String(), resp.StatusCode, snippet), map[string]any{
		"url":                parsed.String(),
		"status_code":        resp.StatusCode,
		"content_type":       resp.Header.Get("Content-Type"),
		"duration_ms":        duration.Milliseconds(),
		"body_excerpt":       snippet,
		"dom_summary":        deepAgentHTMLSummary(bodyText),
		"completed":          passed,
		"error_class":        errorClass,
		"side_effect_level":  deepAgentSideEffectReadonly,
		"dedicated_executor": deepAgentRouteExecutorWeb,
		"tool_result_valid":  passed,
	})
	evidence.Sources = []DeepAgentSourceRef{{URL: parsed.String(), Title: parsed.String(), Snippet: snippet, Provider: "http_get"}}
	return evidence, execErr
}

type runtimeDeepAgentCodePatchExecutor struct {
	runtime *Runtime
}

func (e *runtimeDeepAgentCodePatchExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, _ *DeepAgentState) (DeepAgentStepEvidence, error) {
	route = finalizeDeepAgentActionRoute(route, action)
	diff := firstNonEmptyString(deepAgentActionString(action, "patch"), deepAgentActionString(action, "diff"))
	changedFiles := append([]string(nil), deepAgentStringSlice(action.Args["changed_files"])...)
	if diff != "" {
		changedFiles = appendUniqueStrings(changedFiles, deepAgentChangedFilesFromDiff(diff))
	}
	if diff == "" && len(changedFiles) == 0 {
		err := fmt.Errorf("code_patch executor requires patch/diff or changed_files evidence")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       DeepAgentErrorValidation,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	applyPatch := deepAgentBool(action.Args, "apply", false) || deepAgentBool(action.Args, "apply_patch", false)
	allowWrite := deepAgentBool(action.Args, "allow_workspace_write", false)
	workingDir, err := deepAgentExecutorWorkingDir(action)
	if err != nil {
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       deepAgentErrorPolicyBlocked,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	metadata := map[string]any{
		"changed_files":                    changedFiles,
		"diff_summary":                     firstNonEmptyString(deepAgentActionString(action, "diff_summary"), deepAgentDiffSummary(diff, changedFiles)),
		"rollback_hint":                    firstNonEmptyString(deepAgentActionString(action, "rollback_hint"), "Revert the listed changed files or apply the inverse patch."),
		"side_effect_level":                deepAgentSideEffectReadonly,
		"dedicated_executor":               deepAgentRouteExecutorCodePatch,
		"workspace_write":                  false,
		"tool_result_valid":                true,
		"patch_preview":                    truncateDeepAgentDiagnosticText(diff, 4000),
		"working_dir":                      workingDir,
		"allowlist_enforced":               true,
		"requires_explicit_write_approval": true,
	}
	output := fmt.Sprintf("Code patch evidence recorded for %d file(s).\n%s", len(changedFiles), deepAgentWorkflowString(metadata, "diff_summary"))
	if applyPatch {
		if !allowWrite {
			err := fmt.Errorf("code_patch apply requires allow_workspace_write=true")
			metadata["error_class"] = deepAgentErrorPolicyBlocked
			return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), metadata), err
		}
		if strings.TrimSpace(diff) == "" {
			err := fmt.Errorf("code_patch apply requires non-empty patch/diff")
			metadata["error_class"] = DeepAgentErrorValidation
			return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), metadata), err
		}
		if err := deepAgentGitApply(ctx, workingDir, diff, false); err != nil {
			metadata["error_class"] = DeepAgentErrorDeterministic
			return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), metadata), err
		}
		metadata["side_effect_level"] = deepAgentSideEffectWrite
		metadata["workspace_write"] = true
		output = fmt.Sprintf("Applied code patch for %d file(s).\n%s", len(changedFiles), deepAgentWorkflowString(metadata, "diff_summary"))
	}
	evidence := deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusSucceeded, output, metadata)
	evidence.SideEffectLevel = deepAgentWorkflowString(metadata, "side_effect_level")
	evidence.RollbackHint = deepAgentWorkflowString(metadata, "rollback_hint")
	return evidence, nil
}

type runtimeDeepAgentSubplanExecutor struct {
	runtime *Runtime
	parent  *RuntimeDeepAgentExecutor
}

func (e *runtimeDeepAgentSubplanExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentStepEvidence, error) {
	route = finalizeDeepAgentActionRoute(route, action)
	task := firstNonEmptyString(deepAgentActionString(action, "task"), deepAgentActionString(action, "prompt"), deepAgentActionString(action, "query"))
	childJobID := firstNonEmptyString(deepAgentActionString(action, "child_job_id"), deepAgentActionString(action, "job_id"))
	if _, hasBranchSpecs := action.Args["branch_specs"]; hasBranchSpecs || action.Args["branches"] != nil || action.Args["branch_results"] != nil || (task != "" && childJobID == "") {
		return e.executeParallelStep(ctx, route, action, agentState)
	}
	if task == "" && childJobID == "" {
		err := fmt.Errorf("subplan executor requires task or child_job_id")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       DeepAgentErrorValidation,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	status := firstNonEmptyString(deepAgentActionString(action, "child_job_status"), "bounded")
	output := firstNonEmptyString(deepAgentActionString(action, "summary"), "Subplan task captured as bounded child evidence: "+task)
	evidence := deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusSucceeded, output, map[string]any{
		"task":               task,
		"child_job_id":       childJobID,
		"child_job_status":   status,
		"child_job_type":     firstNonEmptyString(deepAgentActionString(action, "child_job_type"), "subplan"),
		"side_effect_level":  firstNonEmptyString(deepAgentActionString(action, "side_effect_level"), deepAgentSideEffectReadonly),
		"dedicated_executor": deepAgentRouteExecutorSubPlan,
		"bounded":            true,
		"tool_result_valid":  true,
	})
	if childJobID != "" {
		evidence.ChildJobs = []DeepAgentChildJobRef{{ID: childJobID, Type: firstNonEmptyString(deepAgentActionString(action, "child_job_type"), "subplan"), Status: status}}
	}
	return evidence, nil
}

type runtimeDeepAgentConnectorExecutor struct {
	runtime *Runtime
}

func (e *runtimeDeepAgentConnectorExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentStepEvidence, error) {
	route = finalizeDeepAgentActionRoute(route, action)
	if e == nil || e.runtime == nil {
		err := fmt.Errorf("connector executor is not configured")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       DeepAgentErrorValidation,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	userID := firstNonEmptyString(deepAgentActionString(action, "user_id"), deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id"))
	workspaceID := deepAgentActionString(action, "workspace_id")
	provider := deepAgentConnectorProvider(action, route, agentState)
	toolName := deepAgentConnectorToolName(action, route)
	if provider != "" && (provider != "github" || toolName != "") {
		return e.executeGenericMCPConnectorTool(ctx, route, action, userID, workspaceID, provider, toolName, agentState)
	}
	owner := firstNonEmptyString(deepAgentActionString(action, "owner"), deepAgentActionString(action, "org"))
	repo := deepAgentActionString(action, "repo")
	if owner == "" || repo == "" {
		owner, repo = githubOwnerRepoFromText(firstNonEmptyString(deepAgentActionString(action, "url"), deepAgentActionString(action, "query"), stateGoal(agentState)))
	}
	issueNumber := deepAgentActionInt(action, "issue_number", 0)
	if issueNumber == 0 {
		issueNumber = githubIssueNumberFromText(firstNonEmptyString(deepAgentActionString(action, "url"), deepAgentActionString(action, "query"), stateGoal(agentState)))
	}
	if owner == "" || repo == "" {
		err := fmt.Errorf("connector executor requires github owner and repo")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"provider":           "github",
			"error_class":        DeepAgentErrorValidation,
			"side_effect_level":  deepAgentSideEffectReadonly,
			"dedicated_executor": deepAgentRouteExecutorConnector,
		}), err
	}
	metadata := map[string]any{
		"provider":           "github",
		"owner":              owner,
		"repo":               repo,
		"side_effect_level":  deepAgentSideEffectReadonly,
		"dedicated_executor": deepAgentRouteExecutorConnector,
		"tool_result_valid":  true,
	}
	var (
		output  string
		sources []DeepAgentSourceRef
		err     error
	)
	if issueNumber > 0 {
		toolName = MCPToolGitHubReadIssue
		metadata["issue_number"] = issueNumber
		args, _ := json.Marshal(map[string]any{"user_id": userID, "workspace_id": workspaceID, "owner": owner, "repo": repo, "issue_number": issueNumber})
		result, _, policy, readErr := e.runtime.CallConnectorMCPTool(ctx, MCPConnectorToolCall{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Provider:    "github",
			ToolName:    toolName,
			Args:        args,
		})
		err = readErr
		metadata["mcp_policy"] = policy.PermissionPolicy
		if err == nil {
			var issue GitHubIssueInfo
			if unmarshalErr := json.Unmarshal([]byte(result.Output), &issue); unmarshalErr != nil {
				err = unmarshalErr
			} else {
				output = fmt.Sprintf("GitHub issue %s/%s#%d: %s\nState: %s\nAuthor: %s\nLabels: %s\n\n%s", owner, repo, issue.Number, issue.Title, issue.State, issue.Author, strings.Join(issue.Labels, ", "), truncateDeepAgentDiagnosticText(issue.Body, 3000))
				sources = []DeepAgentSourceRef{{URL: issue.HTMLURL, Title: issue.Title, Snippet: truncateDeepAgentDiagnosticText(issue.Body, 600), Provider: "github"}}
			}
		}
	} else {
		toolName = MCPToolGitHubReadRepository
		args, _ := json.Marshal(map[string]any{"user_id": userID, "workspace_id": workspaceID, "owner": owner, "repo": repo})
		result, _, policy, readErr := e.runtime.CallConnectorMCPTool(ctx, MCPConnectorToolCall{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Provider:    "github",
			ToolName:    toolName,
			Args:        args,
		})
		err = readErr
		metadata["mcp_policy"] = policy.PermissionPolicy
		if err == nil {
			var repository GitHubRepositoryInfo
			if unmarshalErr := json.Unmarshal([]byte(result.Output), &repository); unmarshalErr != nil {
				err = unmarshalErr
			} else {
				output = fmt.Sprintf("GitHub repository %s\nDescription: %s\nDefault branch: %s\nOpen issues: %d\nStars: %d\nForks: %d", repository.FullName, repository.Description, repository.DefaultBranch, repository.OpenIssuesCount, repository.StargazersCount, repository.ForksCount)
				sources = []DeepAgentSourceRef{{URL: repository.HTMLURL, Title: repository.FullName, Snippet: repository.Description, Provider: "github"}}
			}
		}
	}
	metadata["tool_name"] = toolName
	status := DeepAgentActionStatusSucceeded
	if err != nil {
		status = DeepAgentActionStatusFailed
		metadata["error_class"] = DeepAgentErrorTransient
		metadata["tool_result_valid"] = false
		output = err.Error()
	}
	evidence := deepAgentDedicatedEvidence(route, action, status, output, metadata)
	evidence.Sources = sources
	evidence.ToolCalls = []DeepAgentToolCallRef{{Name: toolName, Status: status}}
	return evidence, err
}

func (e *runtimeDeepAgentConnectorExecutor) executeGenericMCPConnectorTool(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, userID, workspaceID, provider, toolName string, agentState *DeepAgentState) (DeepAgentStepEvidence, error) {
	if userID == "" {
		err := fmt.Errorf("connector executor requires user_id")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"provider":           provider,
			"tool_name":          toolName,
			"error_class":        DeepAgentErrorValidation,
			"side_effect_level":  deepAgentSideEffectReadonly,
			"dedicated_executor": deepAgentRouteExecutorConnector,
		}), err
	}
	if provider == "" || toolName == "" {
		err := fmt.Errorf("connector executor requires provider and MCP tool_name")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"provider":           provider,
			"tool_name":          toolName,
			"error_class":        DeepAgentErrorValidation,
			"side_effect_level":  deepAgentSideEffectReadonly,
			"dedicated_executor": deepAgentRouteExecutorConnector,
		}), err
	}
	args := deepAgentConnectorToolArgs(action, agentState)
	result, server, policy, err := e.runtime.CallConnectorMCPTool(ctx, MCPConnectorToolCall{
		UserID:      userID,
		WorkspaceID: workspaceID,
		Provider:    provider,
		ToolName:    toolName,
		Args:        args,
	})
	metadata := map[string]any{
		"provider":           provider,
		"tool_name":          toolName,
		"mcp_policy":         policy.PermissionPolicy,
		"mcp_server_id":      server.ID,
		"mcp_server_status":  server.Status,
		"side_effect_level":  deepAgentSideEffectReadonly,
		"dedicated_executor": deepAgentRouteExecutorConnector,
		"tool_result_valid":  err == nil,
	}
	output := result.Output
	status := DeepAgentActionStatusSucceeded
	if err != nil {
		status = DeepAgentActionStatusFailed
		metadata["error_class"] = DeepAgentErrorConfig
		output = err.Error()
	}
	evidence := deepAgentDedicatedEvidence(route, action, status, output, metadata)
	evidence.ToolCalls = []DeepAgentToolCallRef{{Name: toolName, Status: status}}
	return evidence, err
}

func deepAgentConnectorProvider(action DeepAgentAction, route DeepAgentStepRoute, state *DeepAgentState) string {
	provider := firstNonEmptyString(
		deepAgentActionString(action, "provider"),
		deepAgentActionString(action, "connector_provider"),
		deepAgentActionString(action, "connector"),
		route.SearchScope,
	)
	provider = normalizeConnectorProviderID(provider)
	if provider != "" && provider != "github" {
		return provider
	}
	if provider == "github" {
		return provider
	}
	if selected := normalizeConnectorScopes(deepAgentStringSlice(stateWorkingMemory(state)["connector_context"])); len(selected) == 1 {
		return selected[0]
	}
	return provider
}

func deepAgentConnectorToolName(action DeepAgentAction, route DeepAgentStepRoute) string {
	toolName := firstNonEmptyString(
		deepAgentActionString(action, "tool_name"),
		deepAgentActionString(action, "mcp_tool"),
		deepAgentActionString(action, "connector_tool"),
		deepAgentActionString(action, "mcp_tool_name"),
	)
	if toolName != "" {
		return toolName
	}
	if len(route.AllowedTools) == 1 {
		return strings.TrimSpace(route.AllowedTools[0])
	}
	if tools := deepAgentStringSlice(action.Args["allowed_tools"]); len(tools) == 1 {
		return strings.TrimSpace(tools[0])
	}
	return ""
}

func deepAgentConnectorToolArgs(action DeepAgentAction, state *DeepAgentState) json.RawMessage {
	for _, key := range []string{"tool_args", "mcp_args", "arguments", "input"} {
		if raw, ok := deepAgentRawJSONFromAny(action.Args[key]); ok {
			return raw
		}
	}
	args := map[string]any{}
	for _, key := range []string{"query", "prompt", "task"} {
		if value := deepAgentActionString(action, key); value != "" {
			args[key] = value
			break
		}
	}
	if len(args) == 0 {
		if goal := stateGoal(state); goal != "" {
			args["query"] = goal
		}
	}
	if len(args) == 0 {
		args["query"] = ""
	}
	raw, _ := json.Marshal(args)
	return raw
}

func deepAgentRawJSONFromAny(value any) (json.RawMessage, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case json.RawMessage:
		return typed, len(typed) > 0
	case []byte:
		return json.RawMessage(typed), len(typed) > 0
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil, false
		}
		if json.Valid([]byte(trimmed)) {
			return json.RawMessage(trimmed), true
		}
		raw, _ := json.Marshal(map[string]any{"query": trimmed})
		return raw, true
	default:
		raw, err := json.Marshal(typed)
		return raw, err == nil && len(raw) > 0 && string(raw) != "null"
	}
}

func deepAgentDedicatedEvidence(route DeepAgentStepRoute, action DeepAgentAction, status, output string, metadata map[string]any) DeepAgentStepEvidence {
	metadata = cloneWorkflowMap(metadata)
	errorClass := firstNonEmptyString(deepAgentWorkflowString(metadata, "error_class"))
	completed := deepAgentBool(metadata, "completed", status == DeepAgentActionStatusSucceeded)
	diagnostics := map[string]any{
		"result_status": status,
		"completed":     completed,
		"retryable":     deepAgentErrorRetryable(errorClass),
		"metadata":      metadata,
	}
	for key, value := range metadata {
		if _, exists := diagnostics[key]; !exists {
			diagnostics[key] = value
		}
	}
	return DeepAgentStepEvidence{
		StepID:          firstNonEmptyString(route.StepID, action.StepID),
		ActionID:        firstNonEmptyString(action.ID, action.Hash),
		Route:           route,
		Output:          output,
		Summary:         truncateDeepAgentDiagnosticText(output, 800),
		ToolCalls:       []DeepAgentToolCallRef{{Name: firstNonEmptyString(route.Executor, route.Mode), Status: status}},
		Diagnostics:     diagnostics,
		ErrorClass:      errorClass,
		SideEffectLevel: deepAgentWorkflowString(metadata, "side_effect_level"),
		RollbackHint:    deepAgentWorkflowString(metadata, "rollback_hint"),
	}
}

func deepAgentCommandFromAction(action DeepAgentAction) (string, []string, string, error) {
	if args := deepAgentStringSlice(action.Args["command_args"]); len(args) > 0 {
		command := strings.TrimSpace(args[0])
		if command == "" {
			return "", nil, "", fmt.Errorf("command_args[0] is required")
		}
		display := strings.Join(args, " ")
		if deepAgentCommandHasShellMeta(display) {
			return "", nil, display, fmt.Errorf("shell operators are not allowed in dedicated test executor")
		}
		return command, args[1:], display, nil
	}
	raw := firstNonEmptyString(deepAgentActionString(action, "command"), deepAgentActionString(action, "cmd"))
	if raw == "" {
		return "", nil, "", fmt.Errorf("test executor requires command or command_args")
	}
	if deepAgentCommandHasShellMeta(raw) {
		return "", nil, raw, fmt.Errorf("shell operators are not allowed in dedicated test executor")
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", nil, raw, fmt.Errorf("test executor requires command")
	}
	return fields[0], fields[1:], strings.Join(fields, " "), nil
}

func githubOwnerRepoFromText(text string) (string, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ""
	}
	candidates := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ')' || r == '(' || r == ',' || r == ';'
	})
	for _, candidate := range candidates {
		candidate = strings.Trim(candidate, ".,，。\"'")
		if strings.Contains(candidate, "github.com/") {
			if parsed, err := url.Parse(candidate); err == nil {
				parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
				if len(parts) >= 2 {
					return parts[0], strings.TrimSuffix(parts[1], ".git")
				}
			}
			idx := strings.Index(candidate, "github.com/")
			rest := strings.Trim(candidate[idx+len("github.com/"):], "/")
			parts := strings.Split(rest, "/")
			if len(parts) >= 2 {
				return parts[0], strings.TrimSuffix(parts[1], ".git")
			}
		}
		if strings.Count(candidate, "/") == 1 {
			parts := strings.Split(candidate, "/")
			if len(parts[0]) > 0 && len(parts[1]) > 0 && !strings.Contains(parts[0], ":") {
				return parts[0], strings.TrimSuffix(parts[1], ".git")
			}
		}
	}
	return "", ""
}

func githubIssueNumberFromText(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	if idx := strings.Index(text, "/issues/"); idx >= 0 {
		rest := text[idx+len("/issues/"):]
		var digits strings.Builder
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			digits.WriteRune(r)
		}
		if n, err := strconv.Atoi(digits.String()); err == nil {
			return n
		}
	}
	if idx := strings.Index(text, "#"); idx >= 0 {
		rest := text[idx+1:]
		var digits strings.Builder
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			digits.WriteRune(r)
		}
		if n, err := strconv.Atoi(digits.String()); err == nil {
			return n
		}
	}
	return 0
}

func deepAgentAllowedCommandPatterns(action DeepAgentAction, defaults []string) []string {
	patterns := append([]string(nil), defaults...)
	patterns = append(patterns, deepAgentStringSlice(action.Args["allowed_commands"])...)
	patterns = append(patterns, deepAgentStringSlice(action.Args["command_allowlist"])...)
	return appendUniqueStrings(nil, patterns)
}

func deepAgentCommandAllowed(command string, patterns []string) bool {
	command = strings.TrimSpace(command)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(command, strings.TrimSpace(strings.TrimSuffix(pattern, "*"))) {
				return true
			}
			continue
		}
		if command == pattern {
			return true
		}
	}
	return false
}

func deepAgentCommandHasShellMeta(command string) bool {
	return strings.ContainsAny(command, "|&;<>()`$")
}

func deepAgentExecutorWorkingDir(action DeepAgentAction) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	raw := firstNonEmptyString(deepAgentActionString(action, "working_dir"), deepAgentActionString(action, "cwd"))
	if raw == "" {
		return cwd, nil
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(cwd, raw)
	}
	clean := filepath.Clean(raw)
	if _, err := os.Stat(clean); err != nil {
		return "", err
	}
	return clean, nil
}

func deepAgentFailureExcerpt(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= 12 {
		return output
	}
	return strings.Join(lines[len(lines)-12:], "\n")
}

func deepAgentHTMLSummary(body string) map[string]any {
	lowered := strings.ToLower(body)
	return map[string]any{
		"title":        deepAgentHTMLTitle(body),
		"has_html":     strings.Contains(lowered, "<html"),
		"body_bytes":   len(body),
		"script_count": strings.Count(lowered, "<script"),
		"link_count":   strings.Count(lowered, "<a "),
	}
}

func deepAgentHTMLTitle(body string) string {
	lowered := strings.ToLower(body)
	start := strings.Index(lowered, "<title")
	if start < 0 {
		return ""
	}
	start = strings.Index(lowered[start:], ">") + start
	if start < 0 {
		return ""
	}
	end := strings.Index(lowered[start+1:], "</title>")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(body[start+1 : start+1+end])
}

func deepAgentChangedFilesFromDiff(diff string) []string {
	var files []string
	for _, line := range strings.Split(diff, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+++ b/") || strings.HasPrefix(line, "--- a/") {
			files = append(files, strings.TrimSpace(line[6:]))
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				files = append(files, strings.TrimPrefix(parts[3], "b/"))
			}
		}
	}
	return appendUniqueStrings(nil, files)
}

func deepAgentDiffSummary(diff string, files []string) string {
	if strings.TrimSpace(diff) == "" {
		return fmt.Sprintf("changed files: %s", strings.Join(files, ", "))
	}
	added := 0
	removed := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added++
		}
		if strings.HasPrefix(line, "-") {
			removed++
		}
	}
	return fmt.Sprintf("files=%d added=%d removed=%d", len(files), added, removed)
}

func deepAgentGitApply(ctx context.Context, workingDir, diff string, checkOnly bool) error {
	args := []string{"apply"}
	if checkOnly {
		args = append(args, "--check")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workingDir
	cmd.Stdin = bytes.NewBufferString(diff)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply failed: %s", truncateDeepAgentDiagnosticText(string(output), 800))
	}
	return nil
}

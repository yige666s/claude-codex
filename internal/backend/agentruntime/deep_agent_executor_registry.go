package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type RuntimeDeepAgentExecutorRegistry struct {
	runtime *Runtime
	legacy  *RuntimeDeepAgentExecutor
}

func NewRuntimeDeepAgentExecutorRegistry(runtime *Runtime, legacy *RuntimeDeepAgentExecutor) *RuntimeDeepAgentExecutorRegistry {
	if legacy == nil {
		legacy = NewRuntimeDeepAgentExecutor(runtime)
	}
	return &RuntimeDeepAgentExecutorRegistry{runtime: runtime, legacy: legacy}
}

func (r *RuntimeDeepAgentExecutorRegistry) ExecuteAction(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error) {
	evidence, err := r.ExecuteStep(ctx, route, action, state)
	result := deepAgentActionResultFromEvidence(evidence, err)
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["route"] = deepAgentStepRouteMap(evidence.Route)
	result.Metadata["step_evidence"] = deepAgentStepEvidenceMap(evidence)
	if len(evidence.Artifacts) > 0 {
		result.Metadata["artifact_refs"] = evidence.Artifacts
		result.Metadata["artifact_count"] = len(evidence.Artifacts)
	}
	if len(evidence.Sources) > 0 {
		result.Metadata["sources"] = evidence.Sources
	}
	if len(evidence.ToolCalls) > 0 {
		result.Metadata["tool_calls"] = evidence.ToolCalls
	}
	if len(evidence.ChildJobs) > 0 {
		result.Metadata["child_jobs"] = evidence.ChildJobs
	}
	if evidence.ErrorClass != "" {
		result.Metadata["error_class"] = evidence.ErrorClass
	}
	return result, err
}

func (r *RuntimeDeepAgentExecutorRegistry) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error) {
	if r == nil || r.legacy == nil {
		err := fmt.Errorf("deep agent executor registry is not configured")
		return DeepAgentStepEvidence{
			StepID:     action.StepID,
			Route:      route,
			ErrorClass: "config",
			Diagnostics: map[string]any{
				"result_status": DeepAgentActionStatusFailed,
				"completed":     false,
				"retryable":     false,
			},
		}, err
	}
	route = finalizeDeepAgentActionRoute(route, action)
	var (
		result DeepAgentActionResult
		err    error
	)
	switch route.Executor {
	case deepAgentRouteExecutorArtifact:
		result, err = (&runtimeDeepAgentArtifactExecutor{parent: r.legacy}).execute(ctx, route, action, state)
	case deepAgentRouteExecutorSkill:
		result, err = (&runtimeDeepAgentSkillExecutor{parent: r.legacy}).execute(ctx, route, action, state)
	case deepAgentRouteExecutorRAG:
		result, err = (&runtimeDeepAgentRAGSearchExecutor{parent: r.legacy}).execute(ctx, route, action, state)
	case deepAgentRouteExecutorTest:
		return (&runtimeDeepAgentTestExecutor{runtime: r.runtime}).ExecuteStep(ctx, route, action, state)
	case deepAgentRouteExecutorWeb:
		return (&runtimeDeepAgentWebExecutor{runtime: r.runtime}).ExecuteStep(ctx, route, action, state)
	case deepAgentRouteExecutorCodePatch:
		return (&runtimeDeepAgentCodePatchExecutor{runtime: r.runtime}).ExecuteStep(ctx, route, action, state)
	case deepAgentRouteExecutorSubPlan:
		return (&runtimeDeepAgentSubplanExecutor{runtime: r.runtime, parent: r.legacy}).ExecuteStep(ctx, route, action, state)
	case deepAgentRouteExecutorConnector:
		return (&runtimeDeepAgentConnectorExecutor{runtime: r.runtime}).ExecuteStep(ctx, route, action, state)
	case deepAgentRouteExecutorModel, "":
		result, err = (&runtimeDeepAgentModelExecutor{parent: r.legacy}).execute(ctx, route, action, state)
	default:
		err = fmt.Errorf("unsupported deep agent executor: %s", route.Executor)
		result = DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}
	}
	return deepAgentEvidenceFromActionResult(route, action, result, err), err
}

type runtimeDeepAgentModelExecutor struct {
	parent *RuntimeDeepAgentExecutor
}

func (e *runtimeDeepAgentModelExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error) {
	result, err := e.execute(ctx, route, action, state)
	return deepAgentEvidenceFromActionResult(route, action, result, err), err
}

func (e *runtimeDeepAgentModelExecutor) execute(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error) {
	action = deepAgentActionWithRoute(action, route)
	return e.parent.executeModelAction(ctx, action, state, false)
}

type runtimeDeepAgentArtifactExecutor struct {
	parent *RuntimeDeepAgentExecutor
}

func (e *runtimeDeepAgentArtifactExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error) {
	result, err := e.execute(ctx, route, action, state)
	return deepAgentEvidenceFromActionResult(route, action, result, err), err
}

func (e *runtimeDeepAgentArtifactExecutor) execute(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error) {
	action = deepAgentActionWithRoute(action, route)
	return e.parent.executeModelAction(ctx, action, state, true)
}

type runtimeDeepAgentSkillExecutor struct {
	parent *RuntimeDeepAgentExecutor
}

func (e *runtimeDeepAgentSkillExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error) {
	result, err := e.execute(ctx, route, action, state)
	return deepAgentEvidenceFromActionResult(route, action, result, err), err
}

func (e *runtimeDeepAgentSkillExecutor) execute(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error) {
	action = deepAgentActionWithRoute(action, route)
	return e.parent.executeSkillAction(ctx, action, state)
}

type runtimeDeepAgentRAGSearchExecutor struct {
	parent *RuntimeDeepAgentExecutor
}

func (e *runtimeDeepAgentRAGSearchExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error) {
	result, err := e.execute(ctx, route, action, state)
	return deepAgentEvidenceFromActionResult(route, action, result, err), err
}

func (e *runtimeDeepAgentRAGSearchExecutor) execute(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error) {
	action = deepAgentActionWithRoute(action, route)
	return e.parent.executeRAGSearchAction(ctx, action, state)
}

type runtimeDeepAgentEvidenceExecutor struct {
	parent *RuntimeDeepAgentExecutor
}

func (e *runtimeDeepAgentEvidenceExecutor) ExecuteStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentStepEvidence, error) {
	result, err := e.execute(ctx, route, action, state)
	return deepAgentEvidenceFromActionResult(route, action, result, err), err
}

func (e *runtimeDeepAgentEvidenceExecutor) execute(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, state *DeepAgentState) (DeepAgentActionResult, error) {
	action = deepAgentActionWithRoute(action, route)
	return e.parent.executeModelAction(ctx, action, state, false)
}

func finalizeDeepAgentActionRoute(route DeepAgentStepRoute, action DeepAgentAction) DeepAgentStepRoute {
	route.Version = firstNonEmptyString(route.Version, deepAgentActionString(action, "route_version"), "v1")
	if strings.TrimSpace(route.Mode) == "" {
		route.Mode = normalizeDeepAgentRouteMode(action.Tool)
	}
	route.Mode = normalizeDeepAgentRouteMode(route.Mode)
	route.Executor = firstNonEmptyString(route.Executor, deepAgentExecutorForMode(route.Mode))
	route.StepID = firstNonEmptyString(route.StepID, action.StepID, deepAgentActionString(action, "step_id"))
	route.RequiresArtifact = route.RequiresArtifact || route.Mode == DeepAgentToolModeModelArtifact || deepAgentActionRequiresArtifact(action)
	route.DeliverableType = firstNonEmptyString(route.DeliverableType, deepAgentDeliverableNone)
	if strings.TrimSpace(route.SkillName) == "" {
		route.SkillName = strings.TrimPrefix(firstNonEmptyString(deepAgentActionString(action, "skill_name"), deepAgentActionString(action, "skill")), "/")
	}
	if len(route.AllowedTools) == 0 {
		route.AllowedTools = deepAgentStringSlice(action.Args["allowed_tools"])
	}
	return route
}

func deepAgentActionWithRoute(action DeepAgentAction, route DeepAgentStepRoute) DeepAgentAction {
	if action.Args == nil {
		action.Args = map[string]any{}
	}
	action.Tool = firstNonEmptyString(strings.TrimSpace(action.Tool), route.Mode)
	action.Args["step_route"] = deepAgentStepRouteMap(route)
	action.Args["route_version"] = firstNonEmptyString(route.Version, "v1")
	if len(route.AllowedTools) > 0 {
		action.Args["allowed_tools"] = append([]string(nil), route.AllowedTools...)
	}
	if route.RequiresArtifact {
		action.Args["requires_artifact"] = true
	}
	if route.DeliverableType != "" {
		action.Args["deliverable_type"] = route.DeliverableType
	}
	if route.FilenameHint != "" {
		action.Args["filename_hint"] = route.FilenameHint
	}
	return action
}

func deepAgentEvidenceFromActionResult(route DeepAgentStepRoute, action DeepAgentAction, result DeepAgentActionResult, execErr error) DeepAgentStepEvidence {
	status := firstNonEmptyString(result.Status, DeepAgentActionStatusSucceeded)
	if execErr != nil {
		status = DeepAgentActionStatusFailed
	}
	metadata := cloneWorkflowMap(result.Metadata)
	errorClass := classifyDeepAgentError(execErr, result)
	retryable := result.Retryable
	if errorClass != "" {
		retryable = deepAgentErrorRetryable(errorClass)
	}
	diagnostics := map[string]any{
		"result_status": status,
		"completed":     result.Completed,
		"retryable":     retryable,
		"metadata":      metadata,
	}
	if result.Error != "" {
		diagnostics["error"] = result.Error
	}
	if execErr != nil {
		diagnostics["exec_error"] = execErr.Error()
	}
	if details, ok := metadata["diagnostic_details"]; ok {
		diagnostics["diagnostic_details"] = details
	}
	if errorClass != "" {
		diagnostics["error_class"] = errorClass
		metadata["error_class"] = errorClass
	}
	evidence := DeepAgentStepEvidence{
		StepID:      firstNonEmptyString(route.StepID, action.StepID),
		ActionID:    firstNonEmptyString(action.ID, action.Hash),
		Route:       route,
		Output:      result.Output,
		Summary:     truncateDeepAgentDiagnosticText(result.Output, 800),
		Artifacts:   deepAgentArtifactRefsFromAny(metadata["artifact_refs"]),
		Sources:     deepAgentSourceRefsFromAny(metadata["sources"]),
		ToolCalls:   deepAgentToolCallRefsFromMetadata(metadata),
		ChildJobs:   deepAgentChildJobRefsFromMetadata(metadata),
		Diagnostics: diagnostics,
	}
	if len(evidence.Artifacts) == 0 {
		evidence.Artifacts = deepAgentArtifactRefsFromMetadata(metadata)
	}
	if len(evidence.Sources) == 0 {
		evidence.Sources = deepAgentSourceRefsFromText(firstNonEmptyString(result.Output, evidence.Summary))
	}
	evidence.ErrorClass = errorClass
	return evidence
}

func deepAgentActionResultFromEvidence(evidence DeepAgentStepEvidence, execErr error) DeepAgentActionResult {
	diagnostics := evidence.Diagnostics
	status := deepAgentWorkflowString(diagnostics, "result_status")
	if status == "" {
		status = DeepAgentActionStatusSucceeded
	}
	if execErr != nil {
		status = DeepAgentActionStatusFailed
	}
	metadata, _ := diagnostics["metadata"].(map[string]any)
	metadata = cloneWorkflowMap(metadata)
	errText := deepAgentWorkflowString(diagnostics, "error")
	if execErr != nil {
		errText = execErr.Error()
	}
	return DeepAgentActionResult{
		Status:    status,
		Output:    evidence.Output,
		Completed: deepAgentBool(diagnostics, "completed", execErr == nil && status == DeepAgentActionStatusSucceeded),
		Retryable: deepAgentBool(diagnostics, "retryable", false),
		Error:     errText,
		Metadata:  metadata,
	}
}

func deepAgentStepEvidenceMap(evidence DeepAgentStepEvidence) map[string]any {
	data, err := json.Marshal(evidence)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func deepAgentStepEvidenceFromAny(raw any) (DeepAgentStepEvidence, bool) {
	if raw == nil {
		return DeepAgentStepEvidence{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return DeepAgentStepEvidence{}, false
	}
	var evidence DeepAgentStepEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return DeepAgentStepEvidence{}, false
	}
	return evidence, true
}

func deepAgentSourceRefsFromAny(raw any) []DeepAgentSourceRef {
	data, err := json.Marshal(raw)
	if err != nil || raw == nil {
		return nil
	}
	var refs []DeepAgentSourceRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil
	}
	return refs
}

func deepAgentToolCallRefsFromAny(raw any) []DeepAgentToolCallRef {
	data, err := json.Marshal(raw)
	if err != nil || raw == nil {
		return nil
	}
	var refs []DeepAgentToolCallRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil
	}
	return refs
}

var deepAgentURLPattern = regexp.MustCompile(`https?://[^\s\])>"']+`)

func deepAgentSourceRefsFromText(text string) []DeepAgentSourceRef {
	text = strings.TrimSpace(text)
	matches := deepAgentURLPattern.FindAllString(text, -1)
	seen := map[string]struct{}{}
	refs := make([]DeepAgentSourceRef, 0, len(matches))
	for _, url := range matches {
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		refs = append(refs, DeepAgentSourceRef{URL: url, Title: url, Provider: "output"})
	}
	for _, ref := range deepAgentSourceTitleRefsFromText(text) {
		key := strings.ToLower(strings.TrimSpace(firstNonEmptyString(ref.URL, ref.Title+"|"+ref.Provider)))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func deepAgentSourceTitleRefsFromText(text string) []DeepAgentSourceRef {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var refs []DeepAgentSourceRef
	inSourceBlock := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			inSourceBlock = false
			continue
		}
		lowered := strings.ToLower(line)
		if strings.Contains(line, "来源") || strings.Contains(lowered, "source") || strings.Contains(lowered, "references") {
			inSourceBlock = true
			continue
		}
		if !inSourceBlock {
			continue
		}
		cleaned := strings.TrimSpace(strings.TrimLeft(line, "-*•0123456789.）) \t"))
		cleaned = strings.Trim(cleaned, "\"“”")
		if cleaned == "" || strings.HasPrefix(cleaned, "#") {
			continue
		}
		title := cleaned
		provider := "output"
		for _, sep := range []string{" - ", " — ", " – ", "｜", " | "} {
			if parts := strings.Split(cleaned, sep); len(parts) >= 2 {
				title = strings.Trim(strings.TrimSpace(strings.Join(parts[:len(parts)-1], sep)), "\"“”")
				provider = strings.Trim(strings.TrimSpace(parts[len(parts)-1]), "\"“”")
				break
			}
		}
		if title == "" {
			title = cleaned
		}
		if len([]rune(title)) < 6 {
			continue
		}
		refs = append(refs, DeepAgentSourceRef{Title: title, Provider: firstNonEmptyString(provider, "output")})
		if len(refs) >= 12 {
			break
		}
	}
	return refs
}

func deepAgentArtifactRefsFromAny(raw any) []DeepAgentArtifactRef {
	data, err := json.Marshal(raw)
	if err != nil || raw == nil {
		return nil
	}
	var refs []DeepAgentArtifactRef
	if err := json.Unmarshal(data, &refs); err == nil && len(refs) > 0 {
		return refs
	}
	var maps []map[string]any
	if err := json.Unmarshal(data, &maps); err != nil {
		return nil
	}
	out := make([]DeepAgentArtifactRef, 0, len(maps))
	for _, item := range maps {
		out = append(out, DeepAgentArtifactRef{
			ID:          firstNonEmptyString(deepAgentWorkflowString(item, "id"), deepAgentWorkflowString(item, "artifact_id")),
			JobID:       deepAgentWorkflowString(item, "job_id"),
			RunID:       deepAgentWorkflowString(item, "run_id"),
			StepID:      deepAgentWorkflowString(item, "step_id"),
			Filename:    deepAgentWorkflowString(item, "filename"),
			ContentType: deepAgentWorkflowString(item, "content_type"),
			Source:      deepAgentWorkflowString(item, "source"),
		})
	}
	return out
}

func deepAgentToolCallRefsFromMetadata(metadata map[string]any) []DeepAgentToolCallRef {
	var out []DeepAgentToolCallRef
	for _, name := range deepAgentStringSlice(metadata["assistant_tool_names"]) {
		out = append(out, DeepAgentToolCallRef{Name: name, Status: "called"})
	}
	for _, name := range deepAgentStringSlice(metadata["tool_result_names"]) {
		out = append(out, DeepAgentToolCallRef{Name: name, Status: "result"})
	}
	if details, _ := metadata["diagnostic_details"].(map[string]any); len(details) > 0 {
		for _, name := range deepAgentStringSlice(details["assistant_tool_names"]) {
			out = append(out, DeepAgentToolCallRef{Name: name, Status: "called"})
		}
		for _, name := range deepAgentStringSlice(details["tool_result_names"]) {
			out = append(out, DeepAgentToolCallRef{Name: name, Status: "result"})
		}
	}
	return out
}

func deepAgentChildJobRefsFromMetadata(metadata map[string]any) []DeepAgentChildJobRef {
	if refs := deepAgentChildJobRefsFromAny(metadata["child_jobs"]); len(refs) > 0 {
		return refs
	}
	jobID := deepAgentWorkflowString(metadata, "child_job_id")
	if jobID == "" {
		jobID = deepAgentWorkflowString(metadata, "job_id")
	}
	if jobID == "" {
		return nil
	}
	return []DeepAgentChildJobRef{{
		ID:     jobID,
		Type:   deepAgentWorkflowString(metadata, "child_job_type"),
		Status: deepAgentWorkflowString(metadata, "child_job_status"),
	}}
}

func deepAgentChildJobRefsFromAny(raw any) []DeepAgentChildJobRef {
	data, err := json.Marshal(raw)
	if err != nil || raw == nil {
		return nil
	}
	var refs []DeepAgentChildJobRef
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil
	}
	return refs
}

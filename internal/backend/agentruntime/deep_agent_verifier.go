package agentruntime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func verifyDeepAgentStepResult(step DeepAgentStep, result DeepAgentActionResult) (bool, string) {
	if evidence, ok := deepAgentEvidenceFromResult(result); ok {
		if ok, reason := verifyDeepAgentStepEvidence(step, result, evidence); !ok {
			return false, reason
		}
	}
	verification := deepAgentVerificationConfig(step)
	if deepAgentBool(verification, "require_tool_result_valid", false) {
		if result.Status != DeepAgentActionStatusSucceeded || strings.TrimSpace(result.Error) != "" {
			return false, "tool result is not valid"
		}
		if valid, ok := deepAgentMetadataBool(result.Metadata, "tool_result_valid"); ok && !valid {
			return false, "tool result marked invalid"
		}
	}
	if deepAgentBool(verification, "require_output", false) && strings.TrimSpace(result.Output) == "" {
		return false, "required output is missing"
	}
	if minResults := deepAgentIntFromMap(verification, "min_result_count", 0); minResults > 0 {
		if got := deepAgentResultCount(result); got < minResults {
			return false, fmt.Sprintf("result count %d below required %d", got, minResults)
		}
	}
	if minArtifacts := firstPositiveInt(
		deepAgentIntFromMap(verification, "min_artifact_count", 0),
		deepAgentIntFromMap(verification, "artifact_count_min", 0),
	); minArtifacts > 0 {
		if got := deepAgentArtifactCount(result); got < minArtifacts {
			return false, fmt.Sprintf("artifact count %d below required %d", got, minArtifacts)
		}
	}
	if deepAgentBool(verification, "require_artifact", false) {
		if got := deepAgentArtifactCount(result); got < 1 {
			return false, "required artifact is missing"
		}
	}
	if deepAgentBool(verification, "require_tests_passed", false) {
		if !deepAgentTestsPassed(result) {
			return false, "tests did not pass"
		}
	}
	if minCitations := deepAgentIntFromMap(verification, "min_citations", 0); minCitations > 0 {
		if got := deepAgentCitationCount(result); got < minCitations {
			return false, fmt.Sprintf("citation count %d below required %d", got, minCitations)
		}
	}
	if fields := deepAgentStringSlice(verification["required_fields"]); len(fields) > 0 {
		doc := deepAgentResultDocument(result)
		for _, field := range fields {
			if !deepAgentHasField(doc, field) {
				return false, fmt.Sprintf("required field %s is missing", field)
			}
		}
	}
	if values := deepAgentStringSlice(verification["required_output_substrings"]); len(values) > 0 {
		output := strings.ToLower(result.Output)
		for _, value := range values {
			if !strings.Contains(output, strings.ToLower(value)) {
				return false, fmt.Sprintf("required output substring %q is missing", value)
			}
		}
	}
	return true, ""
}

func deepAgentVerificationConfig(step DeepAgentStep) map[string]any {
	out := map[string]any{}
	if raw, ok := step.Metadata["verification"].(map[string]any); ok {
		for key, value := range raw {
			out[key] = value
		}
	}
	if raw, ok := step.Metadata["verify"].(map[string]any); ok {
		for key, value := range raw {
			out[key] = value
		}
	}
	if deepAgentStepRequiresArtifact(step) {
		if _, ok := out["require_artifact"]; !ok {
			out["require_artifact"] = true
		}
		if _, ok := out["min_artifact_count"]; !ok {
			out["min_artifact_count"] = 1
		}
		if _, ok := out["require_tool_result_valid"]; !ok {
			out["require_tool_result_valid"] = true
		}
	}
	return out
}

func deepAgentBool(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1", "required":
			return true
		case "false", "no", "0":
			return false
		}
	}
	return fallback
}

func deepAgentMetadataBool(values map[string]any, key string) (bool, bool) {
	value, ok := values[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1", "passed", "success", "valid":
			return true, true
		case "false", "no", "0", "failed", "invalid":
			return false, true
		}
	}
	return false, false
}

func deepAgentIntFromMap(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	return deepAgentAnyInt(values[key], fallback)
}

func deepAgentAnyInt(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return int(n)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func deepAgentResultCount(result DeepAgentActionResult) int {
	for _, key := range []string{"result_count", "results_count", "hit_count"} {
		if value := deepAgentAnyInt(result.Metadata[key], -1); value >= 0 {
			return value
		}
	}
	var results []any
	if err := json.Unmarshal([]byte(result.Output), &results); err == nil {
		return len(results)
	}
	return 0
}

func deepAgentArtifactCount(result DeepAgentActionResult) int {
	if refs := deepAgentArtifactRefsFromMetadata(result.Metadata); len(refs) > 0 {
		return len(refs)
	}
	for _, key := range []string{"artifact_count", "artifacts_count"} {
		if value := deepAgentAnyInt(result.Metadata[key], -1); value >= 0 {
			return value
		}
	}
	if raw, ok := result.Metadata["artifacts"].([]any); ok {
		return len(raw)
	}
	return 0
}

func deepAgentEvidenceFromResult(result DeepAgentActionResult) (DeepAgentStepEvidence, bool) {
	if evidence, ok := deepAgentStepEvidenceFromAny(result.Metadata["step_evidence"]); ok {
		return evidence, true
	}
	route, routeOK := deepAgentStepRouteFromMap(result.Metadata)
	refs := deepAgentArtifactRefsFromMetadata(result.Metadata)
	sources := deepAgentSourceRefsFromAny(result.Metadata["sources"])
	toolCalls := deepAgentToolCallRefsFromMetadata(result.Metadata)
	if !routeOK && len(refs) == 0 && len(sources) == 0 && len(toolCalls) == 0 {
		return DeepAgentStepEvidence{}, false
	}
	return DeepAgentStepEvidence{
		StepID:      firstNonEmptyString(route.StepID, deepAgentWorkflowString(result.Metadata, "step_id")),
		Route:       route,
		Output:      result.Output,
		Summary:     truncateDeepAgentDiagnosticText(result.Output, 800),
		Artifacts:   refs,
		Sources:     sources,
		ToolCalls:   toolCalls,
		ChildJobs:   deepAgentChildJobRefsFromMetadata(result.Metadata),
		Diagnostics: cloneWorkflowMap(result.Metadata),
		ErrorClass:  deepAgentWorkflowString(result.Metadata, "error_class"),
	}, true
}

func verifyDeepAgentStepEvidence(step DeepAgentStep, result DeepAgentActionResult, evidence DeepAgentStepEvidence) (bool, string) {
	route := evidence.Route
	if strings.TrimSpace(route.Mode) == "" {
		route.Mode = deepAgentWorkflowString(result.Metadata, "tool")
	}
	route.Mode = normalizeDeepAgentRouteMode(route.Mode)
	route.Executor = firstNonEmptyString(route.Executor, deepAgentExecutorForMode(route.Mode))
	route.RequiresArtifact = route.RequiresArtifact || route.Mode == DeepAgentToolModeModelArtifact || deepAgentStepRequiresArtifact(step)
	if route.RequiresArtifact {
		if ok, reason := verifyDeepAgentArtifactEvidence(step, result, evidence); !ok {
			return false, reason
		}
	}
	if deepAgentRouteLooksLikeResearch(route, step) {
		if strings.TrimSpace(firstNonEmptyString(evidence.Summary, evidence.Output, result.Output)) == "" {
			return false, "research step output summary is missing"
		}
		if len(evidence.Sources) == 0 && len(evidence.ToolCalls) == 0 && countDeepAgentCitationMarkers(firstNonEmptyString(evidence.Output, result.Output)) == 0 {
			return false, "research step source evidence is missing"
		}
	}
	if route.Executor == deepAgentRouteExecutorSkill || route.Mode == DeepAgentToolModeSkill {
		for _, child := range evidence.ChildJobs {
			if child.Status != "" && child.Status != JobStatusSucceeded {
				return false, "skill child job did not succeed"
			}
		}
		if route.RequiresArtifact && len(evidence.Artifacts) == 0 {
			return false, "artifact-producing skill did not return artifact refs"
		}
	}
	return true, ""
}

func verifyDeepAgentArtifactEvidence(step DeepAgentStep, result DeepAgentActionResult, evidence DeepAgentStepEvidence) (bool, string) {
	refs := evidence.Artifacts
	if len(refs) == 0 {
		refs = deepAgentArtifactRefsFromMetadata(result.Metadata)
	}
	if len(refs) == 0 {
		return false, "required artifact refs are missing"
	}
	expectedRunID := deepAgentWorkflowString(result.Metadata, "run_id")
	expectedJobID := deepAgentWorkflowString(result.Metadata, "job_id")
	expectedStepID := firstNonEmptyString(step.ID, deepAgentWorkflowString(result.Metadata, "step_id"), evidence.StepID)
	deliverable := firstNonEmptyString(evidence.Route.DeliverableType, deepAgentDeliverableTypeForStep(step))
	hasSizedArtifact := false
	for _, ref := range refs {
		if expectedStepID != "" && ref.StepID != "" && ref.StepID != expectedStepID {
			return false, fmt.Sprintf("artifact %s belongs to step %s, want %s", firstNonEmptyString(ref.ID, ref.Filename), ref.StepID, expectedStepID)
		}
		if expectedRunID != "" && ref.RunID != "" && ref.RunID != expectedRunID {
			return false, fmt.Sprintf("artifact %s belongs to run %s, want %s", firstNonEmptyString(ref.ID, ref.Filename), ref.RunID, expectedRunID)
		}
		if expectedJobID != "" && ref.JobID != "" && ref.JobID != expectedJobID {
			return false, fmt.Sprintf("artifact %s belongs to job %s, want %s", firstNonEmptyString(ref.ID, ref.Filename), ref.JobID, expectedJobID)
		}
		if ref.SizeBytes > 0 {
			hasSizedArtifact = true
		}
		if !deepAgentArtifactRefMatchesDeliverable(ref, deliverable) {
			return false, fmt.Sprintf("artifact %s does not match deliverable type %s", firstNonEmptyString(ref.Filename, ref.ID), deliverable)
		}
	}
	if !hasSizedArtifact {
		return false, "artifact size is missing or zero"
	}
	return true, ""
}

func deepAgentRouteLooksLikeResearch(route DeepAgentStepRoute, step DeepAgentStep) bool {
	if route.Mode != "" && normalizeDeepAgentRouteMode(route.Mode) != DeepAgentToolModeModel {
		return false
	}
	if route.SearchScope == "web" {
		return true
	}
	for _, tool := range route.AllowedTools {
		if strings.EqualFold(tool, "WebSearch") || strings.EqualFold(tool, "WebFetch") {
			return true
		}
	}
	return deepAgentContainsAny(deepAgentRouteText(step), "联网", "外部资料", "公开资料", "网络资料", "互联网", "web research", "web search", "internet research", "external research")
}

func deepAgentArtifactRefMatchesDeliverable(ref DeepAgentArtifactRef, deliverable string) bool {
	deliverable = strings.ToLower(strings.TrimSpace(deliverable))
	filename := strings.ToLower(strings.TrimSpace(ref.Filename))
	contentType := strings.ToLower(strings.TrimSpace(ref.ContentType))
	switch deliverable {
	case "", deepAgentDeliverableNone:
		return true
	case deepAgentDeliverableMarkdown:
		return strings.HasSuffix(filename, ".md") || strings.Contains(contentType, "markdown") || strings.Contains(contentType, "text/plain")
	case deepAgentDeliverableDocx:
		return strings.HasSuffix(filename, ".docx") || strings.Contains(contentType, "wordprocessingml.document")
	case deepAgentDeliverableImage:
		return strings.HasPrefix(contentType, "image/") || strings.HasSuffix(filename, ".png") || strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") || strings.HasSuffix(filename, ".webp")
	case deepAgentDeliverableSVG:
		return strings.HasSuffix(filename, ".svg") || strings.Contains(contentType, "image/svg")
	case "json":
		return strings.HasSuffix(filename, ".json") || strings.Contains(contentType, "json")
	default:
		return true
	}
}

func deepAgentStateCurrentArtifactRefs(state *DeepAgentState) []DeepAgentArtifactRef {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	if len(store) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]DeepAgentArtifactRef, 0)
	for stepID, raw := range store {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, ref := range deepAgentArtifactRefsFromStepContextRecord(stepID, record) {
			key := firstNonEmptyString(ref.ID, ref.Filename)
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	return out
}

func deepAgentArtifactRefsFromStepContextRecord(stepID string, record map[string]any) []DeepAgentArtifactRef {
	var refs []DeepAgentArtifactRef
	appendRefs := func(values []DeepAgentArtifactRef) {
		for _, ref := range values {
			ref.StepID = firstNonEmptyString(ref.StepID, deepAgentWorkflowString(record, "step_id"), stepID)
			refs = append(refs, ref)
		}
	}
	appendRefs(deepAgentArtifactRefsFromAny(record["artifact_refs"]))
	appendRefs(deepAgentArtifactRefsFromMetadata(record))
	if metadata, _ := record["metadata"].(map[string]any); len(metadata) > 0 {
		appendRefs(deepAgentArtifactRefsFromMetadata(metadata))
		if evidence, ok := deepAgentStepEvidenceFromAny(metadata["step_evidence"]); ok {
			appendRefs(evidence.Artifacts)
		}
	}
	if evidence, ok := deepAgentStepEvidenceFromAny(record["step_evidence"]); ok {
		appendRefs(evidence.Artifacts)
	}
	return refs
}

func deepAgentGoalDeliverableType(goal string) string {
	text := strings.ToLower(strings.TrimSpace(goal))
	switch {
	case deepAgentExplicitDocxText(text):
		return deepAgentDeliverableDocx
	case deepAgentContainsAny(text, ".svg", "svg", "流程图", "架构图", "技术图", "diagram", "flowchart", "architecture diagram"):
		return deepAgentDeliverableSVG
	case deepAgentStepLooksLikeImageGeneration(DeepAgentStep{Title: goal, Intent: goal, DoneCondition: goal}):
		return deepAgentDeliverableImage
	case deepAgentContainsAny(text, "json"):
		return "json"
	case deepAgentContainsAny(text, ".md", "markdown", "md格式", "markdown格式"):
		return deepAgentDeliverableMarkdown
	case deepAgentTextRequiresArtifact(text):
		return deepAgentDeliverableMarkdown
	default:
		return deepAgentDeliverableNone
	}
}

func deepAgentArtifactRefsMatchFinalDeliverable(refs []DeepAgentArtifactRef, deliverable string) []DeepAgentArtifactRef {
	if deliverable == "" || deliverable == deepAgentDeliverableNone {
		return refs
	}
	out := make([]DeepAgentArtifactRef, 0, len(refs))
	for _, ref := range refs {
		if ref.SizeBytes <= 0 {
			continue
		}
		if deepAgentArtifactRefMatchesDeliverable(ref, deliverable) {
			out = append(out, ref)
		}
	}
	return out
}

func deepAgentTestsPassed(result DeepAgentActionResult) bool {
	if passed, ok := deepAgentMetadataBool(result.Metadata, "tests_passed"); ok {
		return passed
	}
	if passed, ok := deepAgentMetadataBool(result.Metadata, "test_passed"); ok {
		return passed
	}
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(result.Metadata["test_status"])))
	return status == "passed" || status == "success" || status == "ok"
}

func deepAgentCitationCount(result DeepAgentActionResult) int {
	if value := deepAgentAnyInt(result.Metadata["citation_count"], -1); value >= 0 {
		return value
	}
	if value := deepAgentAnyInt(result.Metadata["citations_count"], -1); value >= 0 {
		return value
	}
	return countDeepAgentCitationMarkers(result.Output)
}

var deepAgentCitationPattern = regexp.MustCompile(`\[[0-9]+\]|https?://|source:`)

func countDeepAgentCitationMarkers(output string) int {
	return len(deepAgentCitationPattern.FindAllString(output, -1))
}

func deepAgentResultDocument(result DeepAgentActionResult) map[string]any {
	doc := cloneWorkflowMap(result.Metadata)
	if strings.TrimSpace(result.Output) == "" {
		return doc
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(result.Output), &output); err == nil {
		doc["output"] = output
		for key, value := range output {
			if _, exists := doc[key]; !exists {
				doc[key] = value
			}
		}
		return doc
	}
	doc["output"] = result.Output
	return doc
}

func deepAgentHasField(doc map[string]any, field string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return true
	}
	parts := strings.Split(field, ".")
	var current any = doc
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		asMap, ok := current.(map[string]any)
		if !ok {
			return false
		}
		value, exists := asMap[part]
		if !exists || value == nil {
			return false
		}
		current = value
	}
	if str, ok := current.(string); ok {
		return strings.TrimSpace(str) != ""
	}
	return true
}

func deepAgentStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str := strings.TrimSpace(fmt.Sprint(item)); str != "" {
				out = append(out, str)
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if str := strings.TrimSpace(part); str != "" {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

package agentruntime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func verifyDeepAgentStepResult(step DeepAgentStep, result DeepAgentActionResult) (bool, string) {
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

package agentruntime

import (
	"fmt"
	"strings"
	"time"
)

const (
	SkillReviewSeverityError   = "error"
	SkillReviewSeverityWarning = "warning"
)

type SkillReviewIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
}

type SkillReviewResult struct {
	SkillName  string             `json:"skill_name"`
	Status     string             `json:"status"`
	Passed     bool               `json:"passed"`
	Issues     []SkillReviewIssue `json:"issues"`
	ReviewedAt time.Time          `json:"reviewed_at"`
}

func ReviewSkillForPublication(record SkillRegistryRecord) SkillReviewResult {
	record = normalizeSkillRegistryRecord(record)
	result := SkillReviewResult{
		SkillName:  record.Name,
		Status:     record.Status,
		Passed:     true,
		ReviewedAt: time.Now().UTC(),
	}
	add := func(severity, code, field, message string) {
		if severity == SkillReviewSeverityError {
			result.Passed = false
		}
		result.Issues = append(result.Issues, SkillReviewIssue{
			Severity: severity,
			Code:     code,
			Field:    field,
			Message:  message,
		})
	}
	if record.Name == "" {
		add(SkillReviewSeverityError, "missing_name", "name", "Skill name is required.")
	}
	if len([]rune(strings.TrimSpace(record.Description))) < 12 {
		add(SkillReviewSeverityError, "missing_description", "description", "Skill description is required before publishing.")
	}
	if record.ContentHash == "" {
		add(SkillReviewSeverityError, "missing_content_hash", "content_hash", "Skill content hash is required before publishing.")
	}
	if record.Status == SkillStatusDisabled || record.Status == SkillStatusArchived {
		add(SkillReviewSeverityError, "inactive_status", "status", fmt.Sprintf("Skill status %q cannot be published directly.", record.Status))
	}
	if boolMetadata(record.Metadata, "hidden") {
		add(SkillReviewSeverityError, "hidden_skill", "metadata.hidden", "Hidden skills cannot be published to consumers.")
	}
	if !boolMetadataDefault(record.Metadata, "user_invocable", true) {
		add(SkillReviewSeverityError, "not_user_invocable", "metadata.user_invocable", "Skill must be user-invocable before publishing.")
	}
	if record.Version == "" {
		add(SkillReviewSeverityWarning, "missing_version", "version", "Add a version to make release tracking clearer.")
	}
	if record.Category == "" {
		add(SkillReviewSeverityWarning, "missing_category", "category", "Add a category for marketplace grouping.")
	}
	if record.Icon == "" {
		add(SkillReviewSeverityWarning, "missing_icon", "icon", "Add an icon for marketplace presentation.")
	}
	if record.SkillRoot == "" {
		add(SkillReviewSeverityWarning, "missing_skill_root", "skill_root", "Skill root is empty, which can make debugging harder.")
	}
	policy := SkillRuntimePolicy{}
	applySkillPolicyMetadata(&policy, record.Metadata)
	allowedTools := policy.AllowedTools
	if len(allowedTools) == 0 {
		allowedTools = stringSliceMetadata(record.Metadata, "allowed_tools")
	}
	allowedEnv := policy.AllowedEnv
	if len(allowedEnv) == 0 {
		allowedEnv = stringSliceMetadata(record.Metadata, "allowed_env")
	}
	if containsToolPrefix(allowedTools, "Bash") && len(allowedEnv) == 0 {
		add(SkillReviewSeverityWarning, "bash_without_env_contract", "metadata.allowed_env", "Bash-capable skills should declare their allowed environment contract.")
	}
	if boolMetadata(record.Metadata, "run_as_job") && len(policy.ArtifactTypes) == 0 && boolMetadata(record.Metadata, "produces_artifacts") {
		add(SkillReviewSeverityWarning, "missing_artifact_types", "metadata.policy.artifact_content_types", "Artifact-producing skills should declare allowed output content types.")
	}
	if len(policy.NetworkAllowlist) == 0 && strings.EqualFold(strings.TrimSpace(policy.Sandbox.Network), "bridge") {
		add(SkillReviewSeverityWarning, "sandbox_network_without_allowlist", "metadata.policy.network_allowlist", "Network-enabled sandbox skills should declare allowed domains.")
	}
	return result
}

func boolMetadata(metadata map[string]any, key string) bool {
	return boolMetadataDefault(metadata, key, false)
}

func boolMetadataDefault(metadata map[string]any, key string, fallback bool) bool {
	if metadata == nil {
		return fallback
	}
	value, ok := metadata[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1", "on":
			return true
		case "false", "no", "0", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func stringSliceMetadata(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}

func containsToolPrefix(tools []string, prefix string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, tool := range tools {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(tool)), prefix) {
			return true
		}
	}
	return false
}

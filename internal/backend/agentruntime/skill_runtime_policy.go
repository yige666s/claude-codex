package agentruntime

import (
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/skills"
)

type SkillRuntimePolicy struct {
	AllowedTools     []string                 `json:"allowed_tools,omitempty"`
	AllowedEnv       []string                 `json:"allowed_env,omitempty"`
	NetworkAllowlist []string                 `json:"network_allowlist,omitempty"`
	ArtifactTypes    []string                 `json:"artifact_content_types,omitempty"`
	ShellTimeout     time.Duration            `json:"-"`
	Sandbox          SkillShellSandboxConfig  `json:"sandbox,omitempty"`
	Raw              map[string]any           `json:"raw,omitempty"`
	Marketplace      SkillMarketplaceMetadata `json:"marketplace,omitempty"`
}

type SkillMarketplaceMetadata struct {
	InputSchema         map[string]any `json:"input_schema,omitempty"`
	OutputArtifactTypes []string       `json:"output_artifact_types,omitempty"`
	Summary             string         `json:"summary,omitempty"`
}

func (r *Runtime) skillRuntimePolicy(skill *skills.SkillDefinition) SkillRuntimePolicy {
	policy := SkillRuntimePolicy{}
	if skill != nil {
		policy.AllowedTools = cleanStringSlice(skill.AllowedTools)
		policy.AllowedEnv = cleanStringSlice(skill.AllowedEnv)
	}
	record, ok := r.skillRecord(skillName(skill))
	if ok {
		applySkillPolicyMetadata(&policy, record.Metadata)
	}
	return policy
}

func (r *Runtime) skillRecord(name string) (SkillRegistryRecord, bool) {
	if r == nil || strings.TrimSpace(name) == "" {
		return SkillRegistryRecord{}, false
	}
	if registry, ok := r.skills.(*RegistrySkillCatalog); ok {
		return registry.SkillRecord(name)
	}
	return SkillRegistryRecord{}, false
}

func applySkillPolicyMetadata(policy *SkillRuntimePolicy, metadata map[string]any) {
	if policy == nil || metadata == nil {
		return
	}
	policyMap := skillPolicyMap(metadata)
	if len(policyMap) == 0 {
		return
	}
	policy.Raw = policyMap
	if values := firstStringSlice(policyMap, "allowed_tools", "allowedTools", "tools"); len(values) > 0 {
		policy.AllowedTools = values
	}
	if values := firstStringSlice(policyMap, "allowed_env", "allowedEnv", "env"); len(values) > 0 {
		policy.AllowedEnv = values
	}
	if values := firstStringSlice(policyMap, "network_allowlist", "networkAllowlist", "allowed_domains", "allowedDomains", "domains"); len(values) > 0 {
		policy.NetworkAllowlist = values
	}
	if values := firstStringSlice(policyMap, "artifact_content_types", "artifactContentTypes", "artifact_types", "artifactTypes", "output_artifact_types", "outputArtifactTypes"); len(values) > 0 {
		policy.ArtifactTypes = values
		policy.Marketplace.OutputArtifactTypes = values
	}
	if schema, ok := firstMap(policyMap, "input_schema", "inputSchema"); ok {
		policy.Marketplace.InputSchema = schema
	}
	if summary := firstString(policyMap, "summary", "marketplace_summary", "marketplaceSummary"); summary != "" {
		policy.Marketplace.Summary = summary
	}
	if timeout := firstDuration(policyMap, "shell_timeout", "shellTimeout", "timeout"); timeout > 0 {
		policy.ShellTimeout = timeout
	}
	if sandbox, ok := firstMap(policyMap, "sandbox", "shell_sandbox", "shellSandbox"); ok {
		policy.Sandbox = parseSkillSandboxPolicy(sandbox)
	}
}

func skillPolicyMap(metadata map[string]any) map[string]any {
	for _, key := range []string{"policy", "permissions", "runtime_policy", "runtimePolicy"} {
		if value, ok := mapMetadata(metadata, key); ok {
			return value
		}
	}
	for _, key := range []string{"agentapi", "runtime", "openclaw"} {
		nested, ok := mapMetadata(metadata, key)
		if !ok {
			continue
		}
		for _, policyKey := range []string{"policy", "permissions", "runtime_policy", "runtimePolicy"} {
			if value, ok := mapMetadata(nested, policyKey); ok {
				return value
			}
		}
	}
	return nil
}

func parseSkillSandboxPolicy(in map[string]any) SkillShellSandboxConfig {
	return SkillShellSandboxConfig{
		Runner:         firstString(in, "runner"),
		Image:          firstString(in, "image"),
		Network:        firstString(in, "network"),
		Memory:         firstString(in, "memory"),
		CPUs:           firstString(in, "cpus", "cpu"),
		PidsLimit:      firstInt(in, "pids_limit", "pidsLimit"),
		TmpfsSize:      firstString(in, "tmpfs_size", "tmpfsSize"),
		MaxOutputBytes: firstInt(in, "max_output_bytes", "maxOutputBytes"),
	}
}

func applySkillSandboxPolicy(base SkillShellSandboxConfig, override SkillShellSandboxConfig) SkillShellSandboxConfig {
	if strings.TrimSpace(override.Runner) != "" {
		base.Runner = override.Runner
	}
	if strings.TrimSpace(override.Image) != "" {
		base.Image = override.Image
	}
	if strings.TrimSpace(override.Network) != "" {
		base.Network = override.Network
	}
	if strings.TrimSpace(override.Memory) != "" {
		base.Memory = override.Memory
	}
	if strings.TrimSpace(override.CPUs) != "" {
		base.CPUs = override.CPUs
	}
	if override.PidsLimit > 0 {
		base.PidsLimit = override.PidsLimit
	}
	if strings.TrimSpace(override.TmpfsSize) != "" {
		base.TmpfsSize = override.TmpfsSize
	}
	if override.MaxOutputBytes > 0 {
		base.MaxOutputBytes = override.MaxOutputBytes
	}
	return base.normalized()
}

func skillName(skill *skills.SkillDefinition) string {
	if skill == nil {
		return ""
	}
	return skill.Name
}

func mapMetadata(metadata map[string]any, key string) (map[string]any, bool) {
	value, ok := metadata[key]
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func firstStringSlice(metadata map[string]any, keys ...string) []string {
	for _, key := range keys {
		if values := stringSliceMetadata(metadata, key); len(values) > 0 {
			return values
		}
	}
	return nil
}

func firstMap(metadata map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := mapMetadata(metadata, key); ok {
			return value, true
		}
	}
	return nil, false
}

func firstString(metadata map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := metadata[key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				return text
			}
		}
	}
	return ""
}

func firstInt(metadata map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		case string:
			var parsed int
			if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstDuration(metadata map[string]any, keys ...string) time.Duration {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case time.Duration:
			return typed
		case int:
			return time.Duration(typed) * time.Second
		case int64:
			return time.Duration(typed) * time.Second
		case float64:
			return time.Duration(typed * float64(time.Second))
		case string:
			text := strings.TrimSpace(typed)
			if text == "" {
				continue
			}
			if duration, err := time.ParseDuration(text); err == nil {
				return duration
			}
			var seconds int
			if _, err := fmt.Sscanf(text, "%d", &seconds); err == nil {
				return time.Duration(seconds) * time.Second
			}
		}
	}
	return 0
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

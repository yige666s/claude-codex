package settings

import (
	"encoding/json"
	"sort"
	"strings"
)

var dangerousShellSettings = []string{
	"apiKeyHelper",
	"awsAuthRefresh",
	"awsCredentialExport",
	"gcpAuthRefresh",
	"otelHeadersHelper",
	"statusLine",
}

var safeManagedEnvVars = map[string]struct{}{
	"ANTHROPIC_CUSTOM_HEADERS":                              {},
	"ANTHROPIC_CUSTOM_MODEL_OPTION":                         {},
	"ANTHROPIC_CUSTOM_MODEL_OPTION_DESCRIPTION":             {},
	"ANTHROPIC_CUSTOM_MODEL_OPTION_NAME":                    {},
	"ANTHROPIC_DEFAULT_HAIKU_MODEL":                         {},
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_DESCRIPTION":             {},
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME":                    {},
	"ANTHROPIC_DEFAULT_HAIKU_MODEL_SUPPORTED_CAPABILITIES":  {},
	"ANTHROPIC_DEFAULT_OPUS_MODEL":                          {},
	"ANTHROPIC_DEFAULT_OPUS_MODEL_DESCRIPTION":              {},
	"ANTHROPIC_DEFAULT_OPUS_MODEL_NAME":                     {},
	"ANTHROPIC_DEFAULT_OPUS_MODEL_SUPPORTED_CAPABILITIES":   {},
	"ANTHROPIC_DEFAULT_SONNET_MODEL":                        {},
	"ANTHROPIC_DEFAULT_SONNET_MODEL_DESCRIPTION":            {},
	"ANTHROPIC_DEFAULT_SONNET_MODEL_NAME":                   {},
	"ANTHROPIC_DEFAULT_SONNET_MODEL_SUPPORTED_CAPABILITIES": {},
	"ANTHROPIC_FOUNDRY_API_KEY":                             {},
	"ANTHROPIC_MODEL":                                       {},
	"ANTHROPIC_SMALL_FAST_MODEL":                            {},
	"ANTHROPIC_SMALL_FAST_MODEL_AWS_REGION":                 {},
	"AWS_DEFAULT_REGION":                                    {},
	"AWS_PROFILE":                                           {},
	"AWS_REGION":                                            {},
	"BASH_DEFAULT_TIMEOUT_MS":                               {},
	"BASH_MAX_OUTPUT_LENGTH":                                {},
	"BASH_MAX_TIMEOUT_MS":                                   {},
	"CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR":              {},
	"CLAUDE_CODE_API_KEY_HELPER_TTL_MS":                     {},
	"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS":                {},
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC":              {},
	"CLAUDE_CODE_DISABLE_TERMINAL_TITLE":                    {},
	"CLAUDE_CODE_ENABLE_TELEMETRY":                          {},
	"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS":                  {},
	"CLAUDE_CODE_IDE_SKIP_AUTO_INSTALL":                     {},
	"CLAUDE_CODE_MAX_OUTPUT_TOKENS":                         {},
	"CLAUDE_CODE_SKIP_BEDROCK_AUTH":                         {},
	"CLAUDE_CODE_SKIP_FOUNDRY_AUTH":                         {},
	"CLAUDE_CODE_SKIP_VERTEX_AUTH":                          {},
	"CLAUDE_CODE_SUBAGENT_MODEL":                            {},
	"CLAUDE_CODE_USE_BEDROCK":                               {},
	"CLAUDE_CODE_USE_FOUNDRY":                               {},
	"CLAUDE_CODE_USE_VERTEX":                                {},
	"DISABLE_AUTOUPDATER":                                   {},
	"DISABLE_BUG_COMMAND":                                   {},
	"DISABLE_COST_WARNINGS":                                 {},
	"DISABLE_ERROR_REPORTING":                               {},
	"DISABLE_FEEDBACK_COMMAND":                              {},
	"DISABLE_TELEMETRY":                                     {},
	"ENABLE_TOOL_SEARCH":                                    {},
	"MAX_MCP_OUTPUT_TOKENS":                                 {},
	"MAX_THINKING_TOKENS":                                   {},
	"MCP_TIMEOUT":                                           {},
	"MCP_TOOL_TIMEOUT":                                      {},
	"OTEL_EXPORTER_OTLP_HEADERS":                            {},
	"OTEL_EXPORTER_OTLP_LOGS_HEADERS":                       {},
	"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL":                      {},
	"OTEL_EXPORTER_OTLP_METRICS_CLIENT_CERTIFICATE":         {},
	"OTEL_EXPORTER_OTLP_METRICS_CLIENT_KEY":                 {},
	"OTEL_EXPORTER_OTLP_METRICS_HEADERS":                    {},
	"OTEL_EXPORTER_OTLP_METRICS_PROTOCOL":                   {},
	"OTEL_EXPORTER_OTLP_PROTOCOL":                           {},
	"OTEL_EXPORTER_OTLP_TRACES_HEADERS":                     {},
	"OTEL_LOG_TOOL_DETAILS":                                 {},
	"OTEL_LOG_USER_PROMPTS":                                 {},
	"OTEL_LOGS_EXPORT_INTERVAL":                             {},
	"OTEL_LOGS_EXPORTER":                                    {},
	"OTEL_METRIC_EXPORT_INTERVAL":                           {},
	"OTEL_METRICS_EXPORTER":                                 {},
	"OTEL_METRICS_INCLUDE_ACCOUNT_UUID":                     {},
	"OTEL_METRICS_INCLUDE_SESSION_ID":                       {},
	"OTEL_METRICS_INCLUDE_VERSION":                          {},
	"OTEL_RESOURCE_ATTRIBUTES":                              {},
	"USE_BUILTIN_RIPGREP":                                   {},
	"VERTEX_REGION_CLAUDE_3_5_HAIKU":                        {},
	"VERTEX_REGION_CLAUDE_3_5_SONNET":                       {},
	"VERTEX_REGION_CLAUDE_3_7_SONNET":                       {},
	"VERTEX_REGION_CLAUDE_4_0_OPUS":                         {},
	"VERTEX_REGION_CLAUDE_4_0_SONNET":                       {},
	"VERTEX_REGION_CLAUDE_4_1_OPUS":                         {},
	"VERTEX_REGION_CLAUDE_4_5_SONNET":                       {},
	"VERTEX_REGION_CLAUDE_4_6_SONNET":                       {},
	"VERTEX_REGION_CLAUDE_HAIKU_4_5":                        {},
}

type DangerousSettings struct {
	ShellSettings map[string]any
	EnvVars       map[string]string
	HasHooks      bool
	Hooks         any
}

type ManagedSettingsSecurityReview struct {
	RequiresApproval bool
	Items            []string
	Dangerous        DangerousSettings
	Message          string
}

func ReviewManagedSettingsSecurity(settings Document) ManagedSettingsSecurityReview {
	dangerous := ExtractDangerousSettings(settings)
	items := FormatDangerousSettingsList(dangerous)
	review := ManagedSettingsSecurityReview{
		RequiresApproval: HasDangerousSettings(dangerous),
		Items:            items,
		Dangerous:        dangerous,
	}
	if review.RequiresApproval {
		review.Message = "Managed settings can execute code or intercept prompts and responses; approve only if you trust the administrator."
	}
	return review
}

func ExtractDangerousSettings(settings Document) DangerousSettings {
	out := DangerousSettings{
		ShellSettings: map[string]any{},
		EnvVars:       map[string]string{},
	}
	if settings == nil {
		return out
	}

	for _, key := range dangerousShellSettings {
		value, ok := settings[key]
		if !ok || isEmptyDangerousValue(value) {
			continue
		}
		out.ShellSettings[key] = value
	}

	if env, ok := asDocument(settings["env"]); ok {
		for key, raw := range env {
			value, ok := raw.(string)
			if !ok || value == "" {
				continue
			}
			if _, safe := safeManagedEnvVars[strings.ToUpper(key)]; !safe {
				out.EnvVars[key] = value
			}
		}
	}

	if hooks, ok := settings["hooks"]; ok && !isEmptyDangerousValue(hooks) {
		if doc, ok := asDocument(hooks); ok && len(doc) > 0 {
			out.HasHooks = true
			out.Hooks = hooks
		}
	}
	return out
}

func HasDangerousSettings(dangerous DangerousSettings) bool {
	return len(dangerous.ShellSettings) > 0 || len(dangerous.EnvVars) > 0 || dangerous.HasHooks
}

func HasDangerousSettingsChanged(oldSettings, newSettings Document) bool {
	oldDangerous := ExtractDangerousSettings(oldSettings)
	newDangerous := ExtractDangerousSettings(newSettings)
	if !HasDangerousSettings(newDangerous) {
		return false
	}
	if !HasDangerousSettings(oldDangerous) {
		return true
	}
	return canonicalDangerousJSON(oldDangerous) != canonicalDangerousJSON(newDangerous)
}

func FormatDangerousSettingsList(dangerous DangerousSettings) []string {
	items := make([]string, 0, len(dangerous.ShellSettings)+len(dangerous.EnvVars)+1)
	for key := range dangerous.ShellSettings {
		items = append(items, key)
	}
	for key := range dangerous.EnvVars {
		items = append(items, key)
	}
	if dangerous.HasHooks {
		items = append(items, "hooks")
	}
	sort.Strings(items)
	return items
}

func canonicalDangerousJSON(dangerous DangerousSettings) string {
	payload := map[string]any{
		"shellSettings": dangerous.ShellSettings,
		"envVars":       dangerous.EnvVars,
		"hooks":         dangerous.Hooks,
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func isEmptyDangerousValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case map[string]any:
		return len(v) == 0
	case Document:
		return len(v) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

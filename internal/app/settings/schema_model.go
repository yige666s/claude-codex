package settings

import (
	"fmt"
	"regexp"
	"strings"
)

const SettingsSchemaURL = "https://json.schemastore.org/claude-code-settings.json"

var mcpServerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func settingsSchemaProperties() map[string]any {
	stringSchema := map[string]any{"type": "string"}
	booleanSchema := map[string]any{"type": "boolean"}
	numberSchema := map[string]any{"type": "number"}
	stringArraySchema := map[string]any{"type": "array", "items": stringSchema}
	stringRecordSchema := map[string]any{"type": "object", "additionalProperties": stringSchema}

	props := map[string]any{
		"$schema":                           map[string]any{"const": SettingsSchemaURL},
		"apiKeyHelper":                      stringSchema,
		"awsCredentialExport":               stringSchema,
		"awsAuthRefresh":                    stringSchema,
		"gcpAuthRefresh":                    stringSchema,
		"otelHeadersHelper":                 stringSchema,
		"fileSuggestion":                    objectProperty(map[string]any{"type": map[string]any{"const": "command"}, "command": stringSchema}),
		"respectGitignore":                  booleanSchema,
		"cleanupPeriodDays":                 map[string]any{"type": "integer", "minimum": 0},
		"env":                               stringRecordSchema,
		"attribution":                       objectProperty(map[string]any{"commit": stringSchema, "pr": stringSchema}),
		"includeCoAuthoredBy":               booleanSchema,
		"includeGitInstructions":            booleanSchema,
		"permissions":                       permissionsSchema(),
		"model":                             stringSchema,
		"availableModels":                   stringArraySchema,
		"modelOverrides":                    stringRecordSchema,
		"enableAllProjectMcpServers":        booleanSchema,
		"enabledMcpjsonServers":             stringArraySchema,
		"disabledMcpjsonServers":            stringArraySchema,
		"allowedMcpServers":                 mcpPolicyEntriesSchema(),
		"deniedMcpServers":                  mcpPolicyEntriesSchema(),
		"hooks":                             map[string]any{"type": "object"},
		"worktree":                          objectProperty(map[string]any{"symlinkDirectories": stringArraySchema, "sparsePaths": stringArraySchema}),
		"disableAllHooks":                   booleanSchema,
		"defaultShell":                      enumSchema("bash", "powershell"),
		"allowManagedHooksOnly":             booleanSchema,
		"allowedHttpHookUrls":               stringArraySchema,
		"httpHookAllowedEnvVars":            stringArraySchema,
		"allowManagedPermissionRulesOnly":   booleanSchema,
		"allowManagedMcpServersOnly":        booleanSchema,
		"strictPluginOnlyCustomization":     map[string]any{"anyOf": []any{booleanSchema, map[string]any{"type": "array", "items": enumSchema(CustomizationSurfaces...)}}},
		"statusLine":                        objectProperty(map[string]any{"type": map[string]any{"const": "command"}, "command": stringSchema, "padding": numberSchema}),
		"enabledPlugins":                    map[string]any{"type": "object"},
		"extraKnownMarketplaces":            map[string]any{"type": "object"},
		"strictKnownMarketplaces":           map[string]any{"type": "array"},
		"blockedMarketplaces":               map[string]any{"type": "array"},
		"forceLoginMethod":                  enumSchema("claudeai", "console"),
		"forceLoginOrgUUID":                 stringSchema,
		"outputStyle":                       stringSchema,
		"language":                          stringSchema,
		"skipWebFetchPreflight":             booleanSchema,
		"sandbox":                           map[string]any{"type": "object"},
		"feedbackSurveyRate":                map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"spinnerTipsEnabled":                booleanSchema,
		"spinnerVerbs":                      objectProperty(map[string]any{"mode": enumSchema("append", "replace"), "verbs": stringArraySchema}),
		"spinnerTipsOverride":               objectProperty(map[string]any{"excludeDefault": booleanSchema, "tips": stringArraySchema}),
		"syntaxHighlightingDisabled":        booleanSchema,
		"terminalTitleFromRename":           booleanSchema,
		"alwaysThinkingEnabled":             booleanSchema,
		"effortLevel":                       enumSchema("low", "medium", "high", "max"),
		"advisorModel":                      stringSchema,
		"fastMode":                          booleanSchema,
		"fastModePerSessionOptIn":           booleanSchema,
		"promptSuggestionEnabled":           booleanSchema,
		"showClearContextOnPlanAccept":      booleanSchema,
		"agent":                             stringSchema,
		"companyAnnouncements":              stringArraySchema,
		"pluginConfigs":                     pluginConfigsSchema(),
		"remote":                            objectProperty(map[string]any{"defaultEnvironmentId": stringSchema}),
		"remoteControlAtStartup":            booleanSchema,
		"autoUpdatesChannel":                enumSchema("latest", "stable"),
		"minimumVersion":                    stringSchema,
		"plansDirectory":                    stringSchema,
		"classifierPermissionsEnabled":      booleanSchema,
		"minSleepDurationMs":                map[string]any{"type": "integer", "minimum": 0},
		"maxSleepDurationMs":                map[string]any{"type": "integer", "minimum": -1},
		"voiceEnabled":                      booleanSchema,
		"assistant":                         booleanSchema,
		"assistantName":                     stringSchema,
		"channelsEnabled":                   booleanSchema,
		"allowedChannelPlugins":             map[string]any{"type": "array", "items": objectProperty(map[string]any{"marketplace": stringSchema, "plugin": stringSchema})},
		"defaultView":                       enumSchema("chat", "transcript"),
		"prefersReducedMotion":              booleanSchema,
		"autoMemoryEnabled":                 booleanSchema,
		"autoMemoryDirectory":               stringSchema,
		"autoDreamEnabled":                  booleanSchema,
		"showThinkingSummaries":             booleanSchema,
		"skipDangerousModePermissionPrompt": booleanSchema,
		"skipAutoPermissionPrompt":          booleanSchema,
		"useAutoModeDuringPlan":             booleanSchema,
		"autoMode":                          objectProperty(map[string]any{"allow": stringArraySchema, "soft_deny": stringArraySchema, "deny": stringArraySchema, "environment": stringArraySchema}),
		"disableAutoMode":                   enumSchema("disable"),
		"sshConfigs":                        map[string]any{"type": "array", "items": objectProperty(map[string]any{"id": stringSchema, "name": stringSchema, "sshHost": stringSchema, "sshPort": map[string]any{"type": "integer"}, "sshIdentityFile": stringSchema, "startDirectory": stringSchema})},
		"claudeMdExcludes":                  stringArraySchema,
		"pluginTrustMessage":                stringSchema,
		"xaaIdp":                            objectProperty(map[string]any{"issuer": stringSchema, "clientId": stringSchema, "callbackPort": map[string]any{"type": "integer", "minimum": 1}}),
		"autoUpdates":                       booleanSchema,
		"telemetry":                         booleanSchema,
		"keybindings":                       map[string]any{"type": "array"},
		"marketplaces":                      map[string]any{"type": "object"},
		"mcpServers":                        map[string]any{"type": "object"},
		"verbose":                           booleanSchema,
	}
	return props
}

func objectProperty(properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": properties}
}

func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func permissionsSchema() map[string]any {
	stringArraySchema := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	return objectProperty(map[string]any{
		"allow":                        stringArraySchema,
		"deny":                         stringArraySchema,
		"ask":                          stringArraySchema,
		"defaultMode":                  enumSchema("default", "plan", "acceptEdits", "dontAsk", "auto", "ask", "allow", "yolo", "bypass"),
		"disableBypassPermissionsMode": enumSchema("disable"),
		"disableAutoMode":              enumSchema("disable"),
		"additionalDirectories":        stringArraySchema,
	})
}

func mcpPolicyEntriesSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": objectProperty(map[string]any{
			"serverName":    map[string]any{"type": "string", "pattern": "^[a-zA-Z0-9_-]+$"},
			"serverCommand": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1},
			"serverUrl":     map[string]any{"type": "string"},
		}),
	}
}

func pluginConfigsSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": objectProperty(map[string]any{
		"mcpServers": map[string]any{"type": "object"},
		"options":    map[string]any{"type": "object"},
	})}
}

func knownSettingsKeys() map[string]struct{} {
	props := settingsSchemaProperties()
	keys := make(map[string]struct{}, len(props))
	for key := range props {
		keys[key] = struct{}{}
	}
	return keys
}

func ValidateSettingsDocument(doc Document, strict bool) []ValidationError {
	if doc == nil {
		return nil
	}
	known := knownSettingsKeys()
	var errs []ValidationError
	for key, value := range doc {
		if _, ok := known[key]; !ok {
			if strict {
				errs = append(errs, ValidationError{
					Path:         key,
					Message:      fmt.Sprintf("unrecognized field %q", key),
					InvalidValue: value,
				})
			}
			continue
		}
		errs = append(errs, validateTopLevelSetting(key, value)...)
	}
	return errs
}

func validateTopLevelSetting(key string, value any) []ValidationError {
	switch key {
	case "$schema", "apiKeyHelper", "awsCredentialExport", "awsAuthRefresh", "gcpAuthRefresh", "otelHeadersHelper", "model", "forceLoginOrgUUID", "outputStyle", "language", "advisorModel", "agent", "minimumVersion", "plansDirectory", "assistantName", "autoMemoryDirectory", "pluginTrustMessage":
		return requireString(key, value)
	case "respectGitignore", "includeCoAuthoredBy", "includeGitInstructions", "enableAllProjectMcpServers", "disableAllHooks", "allowManagedHooksOnly", "allowManagedPermissionRulesOnly", "allowManagedMcpServersOnly", "skipWebFetchPreflight", "spinnerTipsEnabled", "syntaxHighlightingDisabled", "terminalTitleFromRename", "alwaysThinkingEnabled", "fastMode", "fastModePerSessionOptIn", "promptSuggestionEnabled", "showClearContextOnPlanAccept", "classifierPermissionsEnabled", "voiceEnabled", "assistant", "channelsEnabled", "prefersReducedMotion", "autoMemoryEnabled", "autoDreamEnabled", "showThinkingSummaries", "skipDangerousModePermissionPrompt", "skipAutoPermissionPrompt", "useAutoModeDuringPlan", "remoteControlAtStartup", "autoUpdates", "telemetry", "verbose":
		return requireBool(key, value)
	case "cleanupPeriodDays", "minSleepDurationMs":
		return requireNumberRange(key, value, 0, 0, true)
	case "maxSleepDurationMs":
		return requireNumberRange(key, value, -1, 0, true)
	case "feedbackSurveyRate":
		return requireNumberRange(key, value, 0, 1, false)
	case "availableModels", "enabledMcpjsonServers", "disabledMcpjsonServers", "allowedHttpHookUrls", "httpHookAllowedEnvVars", "companyAnnouncements", "claudeMdExcludes":
		return requireStringArray(key, value)
	case "defaultShell":
		return requireEnum(key, value, "bash", "powershell")
	case "forceLoginMethod":
		return requireEnum(key, value, "claudeai", "console")
	case "effortLevel":
		return requireEnum(key, value, "low", "medium", "high", "max")
	case "autoUpdatesChannel":
		return requireEnum(key, value, "latest", "stable")
	case "defaultView":
		return requireEnum(key, value, "chat", "transcript")
	case "disableAutoMode":
		return requireEnum(key, value, "disable")
	case "permissions":
		return validatePermissionsDocument(value)
	case "allowedMcpServers", "deniedMcpServers":
		return validateMCPPolicyEntries(key, value)
	case "strictPluginOnlyCustomization":
		return validateStrictPluginOnlyCustomization(Document{key: value})
	case "env", "modelOverrides":
		return validateStringRecord(key, value)
	case "autoMode":
		return validateAutoMode(value)
	case "spinnerVerbs":
		return validateSpinnerVerbs(value)
	case "fileSuggestion", "statusLine":
		return validateCommandObject(key, value)
	case "remote":
		return validateRemote(value)
	case "xaaIdp":
		return validateXAAIDP(value)
	default:
		if _, ok := asDocument(value); ok || isArray(value) {
			return nil
		}
		return nil
	}
}

func validatePermissionsDocument(value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError("permissions", "object", value)
	}
	var errs []ValidationError
	for key, field := range doc {
		path := "permissions." + key
		switch key {
		case "allow", "deny", "ask", "additionalDirectories":
			errs = append(errs, requireStringArray(path, field)...)
		case "defaultMode":
			errs = append(errs, requireEnum(path, field, "default", "plan", "acceptEdits", "dontAsk", "auto", "ask", "allow", "yolo", "bypass")...)
		case "disableBypassPermissionsMode", "disableAutoMode":
			errs = append(errs, requireEnum(path, field, "disable")...)
		}
	}
	return errs
}

func validateMCPPolicyEntries(path string, value any) []ValidationError {
	items, ok := value.([]any)
	if !ok {
		return typeError(path, "array", value)
	}
	var errs []ValidationError
	for i, item := range items {
		entryPath := fmt.Sprintf("%s[%d]", path, i)
		doc, ok := asDocument(item)
		if !ok {
			errs = append(errs, typeError(entryPath, "object", item)...)
			continue
		}
		defined := 0
		if value, ok := doc["serverName"]; ok {
			defined++
			if err := requireString(entryPath+".serverName", value); len(err) > 0 {
				errs = append(errs, err...)
			} else if !mcpServerNamePattern.MatchString(value.(string)) {
				errs = append(errs, ValidationError{Path: entryPath + ".serverName", Message: "serverName can only contain letters, numbers, hyphens, and underscores", InvalidValue: value})
			}
		}
		if value, ok := doc["serverCommand"]; ok {
			defined++
			errs = append(errs, requireStringArray(entryPath+".serverCommand", value)...)
			if arr, ok := value.([]any); ok && len(arr) == 0 {
				errs = append(errs, ValidationError{Path: entryPath + ".serverCommand", Message: "server command must have at least one element"})
			}
		}
		if value, ok := doc["serverUrl"]; ok {
			defined++
			errs = append(errs, requireString(entryPath+".serverUrl", value)...)
		}
		if defined != 1 {
			errs = append(errs, ValidationError{Path: entryPath, Message: `entry must have exactly one of "serverName", "serverCommand", or "serverUrl"`})
		}
	}
	return errs
}

func validateAutoMode(value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError("autoMode", "object", value)
	}
	var errs []ValidationError
	for _, key := range []string{"allow", "soft_deny", "deny", "environment"} {
		if field, ok := doc[key]; ok {
			errs = append(errs, requireStringArray("autoMode."+key, field)...)
		}
	}
	return errs
}

func validateSpinnerVerbs(value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError("spinnerVerbs", "object", value)
	}
	var errs []ValidationError
	if mode, ok := doc["mode"]; ok {
		errs = append(errs, requireEnum("spinnerVerbs.mode", mode, "append", "replace")...)
	}
	if verbs, ok := doc["verbs"]; ok {
		errs = append(errs, requireStringArray("spinnerVerbs.verbs", verbs)...)
	}
	return errs
}

func validateCommandObject(path string, value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError(path, "object", value)
	}
	var errs []ValidationError
	if typ, ok := doc["type"]; ok {
		errs = append(errs, requireEnum(path+".type", typ, "command")...)
	}
	if command, ok := doc["command"]; ok {
		errs = append(errs, requireString(path+".command", command)...)
	}
	return errs
}

func validateRemote(value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError("remote", "object", value)
	}
	if id, ok := doc["defaultEnvironmentId"]; ok {
		return requireString("remote.defaultEnvironmentId", id)
	}
	return nil
}

func validateXAAIDP(value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError("xaaIdp", "object", value)
	}
	var errs []ValidationError
	if issuer, ok := doc["issuer"]; ok {
		errs = append(errs, requireString("xaaIdp.issuer", issuer)...)
	}
	if clientID, ok := doc["clientId"]; ok {
		errs = append(errs, requireString("xaaIdp.clientId", clientID)...)
	}
	if port, ok := doc["callbackPort"]; ok {
		errs = append(errs, requireNumberRange("xaaIdp.callbackPort", port, 1, 0, true)...)
	}
	return errs
}

func validateStringRecord(path string, value any) []ValidationError {
	doc, ok := asDocument(value)
	if !ok {
		return typeError(path, "object", value)
	}
	var errs []ValidationError
	for key, field := range doc {
		errs = append(errs, requireString(path+"."+key, field)...)
	}
	return errs
}

func requireString(path string, value any) []ValidationError {
	if _, ok := value.(string); !ok {
		return typeError(path, "string", value)
	}
	return nil
}

func requireBool(path string, value any) []ValidationError {
	if _, ok := value.(bool); !ok {
		return typeError(path, "boolean", value)
	}
	return nil
}

func requireStringArray(path string, value any) []ValidationError {
	arr, ok := value.([]any)
	if !ok {
		return typeError(path, "array", value)
	}
	var errs []ValidationError
	for i, item := range arr {
		if _, ok := item.(string); !ok {
			errs = append(errs, typeError(fmt.Sprintf("%s[%d]", path, i), "string", item)...)
		}
	}
	return errs
}

func requireEnum(path string, value any, allowed ...string) []ValidationError {
	text, ok := value.(string)
	if !ok {
		return typeError(path, "string", value)
	}
	for _, item := range allowed {
		if text == item {
			return nil
		}
	}
	return []ValidationError{{Path: path, Message: fmt.Sprintf("invalid value %q, expected one of: %s", text, strings.Join(allowed, ", ")), InvalidValue: value}}
}

func requireNumberRange(path string, value any, min, max float64, integer bool) []ValidationError {
	number, ok := value.(float64)
	if !ok {
		return typeError(path, "number", value)
	}
	if integer && number != float64(int64(number)) {
		return []ValidationError{{Path: path, Message: "expected integer", InvalidValue: value}}
	}
	if number < min {
		return []ValidationError{{Path: path, Message: fmt.Sprintf("number must be greater than or equal to %v", min), InvalidValue: value}}
	}
	if max > min && number > max {
		return []ValidationError{{Path: path, Message: fmt.Sprintf("number must be less than or equal to %v", max), InvalidValue: value}}
	}
	return nil
}

func typeError(path, expected string, value any) []ValidationError {
	return []ValidationError{{Path: path, Message: fmt.Sprintf("expected %s, got %T", expected, value), Expected: expected, InvalidValue: value}}
}

func isArray(value any) bool {
	_, ok := value.([]any)
	return ok
}

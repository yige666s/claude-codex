package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"claude-codex/internal/app/config"
	appsettings "claude-codex/internal/app/settings"
	"claude-codex/internal/harness/permissions"
	providerbackend "claude-codex/internal/harness/provider"
	"claude-codex/internal/public/apperrors"
)

func newPermissionRuntimeOptions(cfg config.Config, mode permissions.Mode, workingDir string) ([]permissions.Option, error) {
	toolContext := loadPermissionToolContext(workingDir, mode)
	options := []permissions.Option{
		permissions.WithToolContext(toolContext),
		permissions.WithUpdatePersister(persistPermissionUpdates(workingDir)),
	}
	if mode == permissions.ModeAuto {
		classifier, err := newPermissionClassifier(cfg)
		if err != nil {
			return nil, err
		}
		if classifier != nil {
			options = append(options, permissions.WithClassifier(classifier))
		}
	}
	return options, nil
}

func loadPermissionToolContext(workingDir string, mode permissions.Mode) *permissions.ToolContext {
	ctx := permissions.NewToolContext(mode)
	for _, source := range []appsettings.SettingSource{
		appsettings.SourceUser,
		appsettings.SourceProject,
		appsettings.SourceLocal,
		appsettings.SourcePolicy,
	} {
		result := appsettings.LoadSettingsForSource(source, workingDir)
		if result.Settings == nil {
			continue
		}
		permissionSource, ok := permissionSourceForSettings(source)
		if !ok {
			continue
		}
		addRulesByBehavior(ctx, permissionSource, result.Settings)
	}
	return ctx
}

func addRulesByBehavior(ctx *permissions.ToolContext, source permissions.RuleSource, doc appsettings.Document) {
	permissionDoc := documentValue(doc["permissions"])
	if permissionDoc == nil {
		return
	}
	if rules := stringArrayValue(permissionDoc["allow"]); len(rules) > 0 {
		ctx.AlwaysAllowRules[source] = append(ctx.AlwaysAllowRules[source], rules...)
	}
	if rules := stringArrayValue(permissionDoc["deny"]); len(rules) > 0 {
		ctx.AlwaysDenyRules[source] = append(ctx.AlwaysDenyRules[source], rules...)
	}
	if rules := stringArrayValue(permissionDoc["ask"]); len(rules) > 0 {
		ctx.AlwaysAskRules[source] = append(ctx.AlwaysAskRules[source], rules...)
	}
}

func persistPermissionUpdates(workingDir string) permissions.UpdatePersister {
	return func(_ context.Context, updates []permissions.PermissionUpdate) error {
		grouped := make(map[permissions.RuleSource][]permissions.PermissionUpdate)
		for _, update := range updates {
			if !permissions.SupportsPersistence(update.Destination) {
				continue
			}
			switch update.Type {
			case permissions.UpdateAddRules, permissions.UpdateReplaceRules, permissions.UpdateRemoveRules, permissions.UpdateSetMode:
				grouped[update.Destination] = append(grouped[update.Destination], update)
			}
		}
		for source, sourceUpdates := range grouped {
			editable, ok := editableSettingsSourceForPermission(source)
			if !ok {
				continue
			}
			if err := persistPermissionUpdatesForSource(editable, workingDir, sourceUpdates); err != nil {
				return err
			}
		}
		return nil
	}
}

func persistPermissionUpdatesForSource(source appsettings.EditableSettingSource, workingDir string, updates []permissions.PermissionUpdate) error {
	result := appsettings.LoadSettingsForSource(appsettings.SettingSource(source), workingDir)
	if result.Settings == nil && len(result.Errors) > 0 {
		return fmt.Errorf("cannot update invalid %s permissions settings: %s", source, result.Errors[0].Message)
	}
	doc := appsettings.CloneDocument(result.Settings)
	if doc == nil {
		doc = appsettings.Document{}
	}
	permissionDoc := documentValue(doc["permissions"])
	if permissionDoc == nil {
		permissionDoc = appsettings.Document{}
	}

	for _, update := range updates {
		if update.Type == permissions.UpdateSetMode {
			if update.Mode != "" {
				permissionDoc["defaultMode"] = string(update.Mode)
			}
			continue
		}
		key, ok := settingsKeyForBehavior(update.Behavior)
		if !ok {
			continue
		}
		current := stringArrayValue(permissionDoc[key])
		rules := permissionRuleStrings(update.Rules)
		switch update.Type {
		case permissions.UpdateAddRules:
			current = appendUniqueStrings(current, rules...)
		case permissions.UpdateReplaceRules:
			current = appendUniqueStrings(nil, rules...)
		case permissions.UpdateRemoveRules:
			current = removeStrings(current, rules...)
		}
		permissionDoc[key] = anySlice(current)
	}

	return appsettings.UpdateSettingsForSource(source, workingDir, appsettings.Document{
		"permissions": permissionDoc,
	})
}

func newPermissionClassifier(cfg config.Config) (permissions.Classifier, error) {
	provider, err := newPermissionClassifierProvider(cfg)
	if err != nil || provider == nil {
		return nil, err
	}
	return permissions.NewTextClassifier(providerbackend.TextCompletionClient{
		Provider:  provider,
		Model:     cfg.Model,
		MaxTokens: 256,
	}), nil
}

func newPermissionClassifierProvider(cfg config.Config) (providerbackend.Provider, error) {
	switch cfg.Backend {
	case "", "simple":
		return nil, nil
	case "anthropic":
		apiKey := cfg.APIKey
		if config.IsPlaceholderAPIKey(apiKey) {
			apiKey = ""
		}
		if apiKey == "" {
			apiKey = cfg.APIToken
		}
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, apperrors.Auth(
				"API key is required for the anthropic auto-mode classifier.",
				"Set api_key in ~/.claude-codex/config.json, or export ANTHROPIC_API_KEY.",
				nil,
			)
		}
		return providerbackend.NewAnthropicProvider(providerbackend.Config{
			Provider: "anthropic",
			APIKey:   apiKey,
			BaseURL:  cfg.APIBaseURL,
			Model:    cfg.Model,
			Timeout:  cfg.TimeoutSeconds,
		})
	case "openai":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = cfg.APIToken
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, apperrors.Auth(
				"API key is required for the openai auto-mode classifier.",
				"Set api_key in ~/.claude-codex/config.json, or export OPENAI_API_KEY.",
				nil,
			)
		}
		baseURL := cfg.APIBaseURL
		if strings.TrimSpace(baseURL) == "" || strings.Contains(baseURL, "api.anthropic.com") {
			baseURL = ""
		}
		return providerbackend.NewOpenAIProvider(providerbackend.Config{
			Provider: "openai",
			APIKey:   apiKey,
			BaseURL:  baseURL,
			Model:    cfg.Model,
			Timeout:  cfg.TimeoutSeconds,
		})
	default:
		return nil, apperrors.Config(
			fmt.Sprintf("Unsupported backend %q for auto-mode classifier.", cfg.Backend),
			"Use backend anthropic or openai for auto-mode classification.",
			nil,
		)
	}
}

func permissionSourceForSettings(source appsettings.SettingSource) (permissions.RuleSource, bool) {
	switch source {
	case appsettings.SourceUser:
		return permissions.SourceUserSettings, true
	case appsettings.SourceProject:
		return permissions.SourceProjectSettings, true
	case appsettings.SourceLocal:
		return permissions.SourceLocalSettings, true
	case appsettings.SourcePolicy:
		return permissions.SourcePolicySettings, true
	default:
		return "", false
	}
}

func editableSettingsSourceForPermission(source permissions.RuleSource) (appsettings.EditableSettingSource, bool) {
	switch source {
	case permissions.SourceUserSettings:
		return appsettings.EditableUser, true
	case permissions.SourceProjectSettings:
		return appsettings.EditableProject, true
	case permissions.SourceLocalSettings:
		return appsettings.EditableLocal, true
	default:
		return "", false
	}
}

func settingsKeyForBehavior(behavior permissions.Behavior) (string, bool) {
	switch behavior {
	case permissions.BehaviorAllow:
		return "allow", true
	case permissions.BehaviorDeny:
		return "deny", true
	case permissions.BehaviorAsk:
		return "ask", true
	default:
		return "", false
	}
}

func permissionRuleStrings(rules []permissions.RuleValue) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		value := strings.TrimSpace(permissions.RuleValueToString(rule))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func documentValue(value any) appsettings.Document {
	switch v := value.(type) {
	case appsettings.Document:
		return v
	case map[string]any:
		return appsettings.Document(v)
	default:
		return nil
	}
}

func stringArrayValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func anySlice(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	out := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func removeStrings(values []string, removals ...string) []string {
	remove := make(map[string]bool, len(removals))
	for _, value := range removals {
		remove[strings.TrimSpace(value)] = true
	}
	out := values[:0]
	for _, value := range values {
		if !remove[strings.TrimSpace(value)] {
			out = append(out, value)
		}
	}
	return out
}

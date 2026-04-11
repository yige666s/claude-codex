package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"claude-codex/internal/harness/plugins"
)

const pluginTelemetrySalt = "claude-plugin-telemetry-v1"

type TelemetryPluginScope string

const (
	PluginScopeOfficial      TelemetryPluginScope = "official"
	PluginScopeOrg           TelemetryPluginScope = "org"
	PluginScopeUserLocal     TelemetryPluginScope = "user-local"
	PluginScopeDefaultBundle TelemetryPluginScope = "default-bundle"
)

func HashPluginID(name string, marketplace string) string {
	key := name
	if strings.TrimSpace(marketplace) != "" {
		key += "@" + strings.ToLower(strings.TrimSpace(marketplace))
	}
	sum := sha256.Sum256([]byte(key + pluginTelemetrySalt))
	return hex.EncodeToString(sum[:])[:16]
}

func GetTelemetryPluginScope(name string, marketplace string, managedNames map[string]bool) TelemetryPluginScope {
	switch {
	case strings.EqualFold(marketplace, plugins.BuiltinMarketplaceName):
		return PluginScopeDefaultBundle
	case isOfficialMarketplaceName(marketplace):
		return PluginScopeOfficial
	case managedNames != nil && managedNames[name]:
		return PluginScopeOrg
	default:
		return PluginScopeUserLocal
	}
}

func BuildPluginTelemetryFields(name string, marketplace string, managedNames map[string]bool) map[string]any {
	scope := GetTelemetryPluginScope(name, marketplace, managedNames)
	anthropicControlled := scope == PluginScopeOfficial || scope == PluginScopeDefaultBundle

	pluginName := "third-party"
	marketplaceName := "third-party"
	if anthropicControlled {
		pluginName = name
		if strings.TrimSpace(marketplace) != "" {
			marketplaceName = marketplace
		}
	}

	return map[string]any{
		"plugin_id_hash":            HashPluginID(name, marketplace),
		"plugin_scope":              string(scope),
		"plugin_name_redacted":      pluginName,
		"marketplace_name_redacted": marketplaceName,
		"is_official_plugin":        anthropicControlled,
	}
}

func BuildPluginEvent(manifest plugins.Manifest, managedNames map[string]bool) TraceEvent {
	name, marketplace := parsePluginSource(manifest.Name)
	if name == "" {
		name = manifest.Name
	}
	attrs := BuildPluginTelemetryFields(name, marketplace, managedNames)
	attrs["version"] = manifest.Version
	attrs["path"] = manifest.Path
	attrs["mcp_server_count"] = len(manifest.MCPServers)
	return TraceEvent{
		Name:  "plugin.loaded",
		Kind:  "plugin",
		Attrs: attrs,
	}
}

func parsePluginSource(value string) (string, string) {
	parts := strings.Split(strings.TrimSpace(value), "@")
	if len(parts) != 2 {
		return strings.TrimSpace(value), ""
	}
	return parts[0], parts[1]
}

func isOfficialMarketplaceName(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "anthropic", "builtin", "official":
		return true
	default:
		return false
	}
}

package plugins

import "strings"

type ParsedPluginIdentifier struct {
	Name        string
	Marketplace string
}

func ParsePluginIdentifier(plugin string) ParsedPluginIdentifier {
	parts := strings.Split(strings.TrimSpace(plugin), "@")
	if len(parts) > 1 {
		return ParsedPluginIdentifier{Name: parts[0], Marketplace: parts[1]}
	}
	return ParsedPluginIdentifier{Name: parts[0]}
}

func BuildPluginID(name string, marketplace string) string {
	name = strings.TrimSpace(name)
	marketplace = strings.TrimSpace(marketplace)
	if marketplace == "" {
		return name
	}
	return name + "@" + marketplace
}

func SettingSourceToPluginScope(source string) PluginScope {
	switch strings.TrimSpace(source) {
	case "policySettings":
		return PluginScopeManaged
	case "projectSettings":
		return PluginScopeProject
	case "localSettings":
		return PluginScopeLocal
	case "flagSettings":
		return PluginScopeFlag
	default:
		return PluginScopeUser
	}
}

func PluginScopeToSettingSource(scope PluginScope) (string, bool) {
	switch scope {
	case PluginScopeUser:
		return "userSettings", true
	case PluginScopeProject:
		return "projectSettings", true
	case PluginScopeLocal:
		return "localSettings", true
	default:
		return "", false
	}
}

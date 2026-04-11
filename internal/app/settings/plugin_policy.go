package settings

import "strings"

func IsRestrictedToPluginOnly(policy Document, surface string) bool {
	value, ok := policy["strictPluginOnlyCustomization"]
	if !ok {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == surface {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if item == surface {
				return true
			}
		}
	}
	return false
}

func IsSourceAdminTrusted(source string) bool {
	switch strings.TrimSpace(source) {
	case "plugin", "policySettings", "built-in", "builtin", "bundled":
		return true
	default:
		return false
	}
}

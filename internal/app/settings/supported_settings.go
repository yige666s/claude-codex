package settings

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type SupportedSettingType string

const (
	SettingTypeBoolean SupportedSettingType = "boolean"
	SettingTypeString  SupportedSettingType = "string"
)

type SupportedSetting struct {
	Key         string
	Type        SupportedSettingType
	Description string
	Path        []string
	Options     []string
}

var supportedSettings = map[string]SupportedSetting{
	"autoMemoryEnabled": {
		Key:         "autoMemoryEnabled",
		Type:        SettingTypeBoolean,
		Description: "Enable auto-memory",
	},
	"autoDreamEnabled": {
		Key:         "autoDreamEnabled",
		Type:        SettingTypeBoolean,
		Description: "Enable background memory consolidation",
	},
	"model": {
		Key:         "model",
		Type:        SettingTypeString,
		Description: "Override the default model",
	},
	"alwaysThinkingEnabled": {
		Key:         "alwaysThinkingEnabled",
		Type:        SettingTypeBoolean,
		Description: "Enable extended thinking",
	},
	"permissions.defaultMode": {
		Key:         "permissions.defaultMode",
		Type:        SettingTypeString,
		Description: "Default permission mode for tool usage",
		Options:     []string{"default", "plan", "acceptEdits", "dontAsk", "auto", "ask", "allow", "yolo", "bypass"},
	},
	"language": {
		Key:         "language",
		Type:        SettingTypeString,
		Description: "Preferred language for responses and dictation",
	},
	"fastMode": {
		Key:         "fastMode",
		Type:        SettingTypeBoolean,
		Description: "Enable fast mode",
	},
	"promptSuggestionEnabled": {
		Key:         "promptSuggestionEnabled",
		Type:        SettingTypeBoolean,
		Description: "Enable prompt suggestions",
	},
	"showThinkingSummaries": {
		Key:         "showThinkingSummaries",
		Type:        SettingTypeBoolean,
		Description: "Show thinking summaries in transcript view",
	},
	"skipDangerousModePermissionPrompt": {
		Key:         "skipDangerousModePermissionPrompt",
		Type:        SettingTypeBoolean,
		Description: "Record bypass-permissions prompt acceptance",
	},
}

func ListSupportedSettings() []SupportedSetting {
	keys := make([]string, 0, len(supportedSettings))
	for key := range supportedSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]SupportedSetting, 0, len(keys))
	for _, key := range keys {
		out = append(out, normalizeSupportedSetting(supportedSettings[key]))
	}
	return out
}

func GetSupportedSetting(key string) (SupportedSetting, bool) {
	setting, ok := supportedSettings[key]
	if !ok {
		return SupportedSetting{}, false
	}
	return normalizeSupportedSetting(setting), true
}

func CoerceSupportedSettingValue(setting SupportedSetting, raw string) (any, error) {
	switch setting.Type {
	case SettingTypeBoolean:
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%s requires true or false", setting.Key)
		}
		return value, nil
	case SettingTypeString:
		value := strings.TrimSpace(raw)
		if len(setting.Options) > 0 && !stringIn(value, setting.Options) {
			return nil, fmt.Errorf("invalid value %q for %s; options: %s", value, setting.Key, strings.Join(setting.Options, ", "))
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported setting type %q", setting.Type)
	}
}

func BuildSettingUpdate(setting SupportedSetting, value any) Document {
	path := setting.Path
	if len(path) == 0 {
		path = strings.Split(setting.Key, ".")
	}
	return buildNestedDocument(path, value)
}

func ReadSettingValue(doc Document, setting SupportedSetting) (any, bool) {
	path := setting.Path
	if len(path) == 0 {
		path = strings.Split(setting.Key, ".")
	}
	var current any = doc
	for _, part := range path {
		next, ok := asDocument(current)
		if !ok {
			return nil, false
		}
		value, ok := next[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func normalizeSupportedSetting(setting SupportedSetting) SupportedSetting {
	if len(setting.Path) == 0 {
		setting.Path = strings.Split(setting.Key, ".")
	}
	return setting
}

func buildNestedDocument(path []string, value any) Document {
	if len(path) == 0 {
		return Document{}
	}
	root := Document{}
	current := root
	for i, part := range path {
		if i == len(path)-1 {
			current[part] = value
			break
		}
		next := Document{}
		current[part] = next
		current = next
	}
	return root
}

func stringIn(value string, allowed []string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

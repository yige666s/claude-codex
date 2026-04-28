package configtool

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	appconfig "claude-codex/internal/app/config"
	appsettings "claude-codex/internal/app/settings"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const ToolName = "Config"

type Tool struct {
	workingDir string
}

type inputEnvelope struct {
	Setting string          `json:"setting"`
	Value   json.RawMessage `json:"value,omitempty"`
}

type output struct {
	Success       bool   `json:"success"`
	Operation     string `json:"operation,omitempty"`
	Setting       string `json:"setting,omitempty"`
	Value         any    `json:"value,omitempty"`
	PreviousValue any    `json:"previousValue,omitempty"`
	NewValue      any    `json:"newValue,omitempty"`
	Error         string `json:"error,omitempty"`
}

type globalSetting struct {
	Key         string
	Description string
	Options     []string
	Get         func(appconfig.Config) any
	Set         func(*appconfig.Config, any)
}

var globalSettings = map[string]globalSetting{
	"theme": {
		Key:         "theme",
		Description: "Color theme for the UI",
		Options:     []string{"light", "dark"},
		Get: func(cfg appconfig.Config) any {
			return cfg.Theme
		},
		Set: func(cfg *appconfig.Config, value any) {
			cfg.Theme = value.(string)
		},
	},
	"model": {
		Key:         "model",
		Description: "Active model override",
		Get: func(cfg appconfig.Config) any {
			return cfg.Model
		},
		Set: func(cfg *appconfig.Config, value any) {
			cfg.Model = value.(string)
		},
	},
	"permission_mode": {
		Key:         "permission_mode",
		Description: "Current default permission mode",
		Options:     []string{"default", "plan", "bypass", "auto"},
		Get: func(cfg appconfig.Config) any {
			return cfg.PermissionMode
		},
		Set: func(cfg *appconfig.Config, value any) {
			cfg.PermissionMode = value.(string)
		},
	},
}

func NewTool(workingDir string) toolkit.Tool {
	return &Tool{workingDir: workingDir}
}

func (t *Tool) Name() string {
	return ToolName
}

func (t *Tool) Description() string {
	return "Get or set CLI config and settings values such as theme, model, or permissions.defaultMode."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "setting": {
      "type": "string",
      "description": "The setting key, for example \"theme\", \"model\", or \"permissions.defaultMode\"."
    },
    "value": {
      "description": "Optional new value. Omit this field to read the current value.",
      "oneOf": [
        {"type": "string"},
        {"type": "boolean"},
        {"type": "number"}
      ]
    }
  },
  "required": ["setting"]
}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelWrite
}

func (t *Tool) IsConcurrencySafe() bool {
	return false
}

func (t *Tool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var input inputEnvelope
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolkit.Result{}, fmt.Errorf("Config: invalid input: %w", err)
	}
	input.Setting = strings.TrimSpace(input.Setting)
	if input.Setting == "" {
		return t.marshal(output{
			Success: false,
			Error:   "Setting is required",
		})
	}

	if setting, ok := globalSettings[input.Setting]; ok {
		if len(input.Value) == 0 {
			return t.readGlobalSetting(setting)
		}
		value, err := coerceStringValue(setting.Key, input.Value, setting.Options)
		if err != nil {
			return t.marshal(output{
				Success:   false,
				Operation: "set",
				Setting:   setting.Key,
				Error:     err.Error(),
			})
		}
		return t.writeGlobalSetting(setting, value)
	}

	setting, ok := appsettings.GetSupportedSetting(input.Setting)
	if !ok {
		return t.marshal(output{
			Success: false,
			Error:   fmt.Sprintf("Unknown setting: %q", input.Setting),
		})
	}
	if len(input.Value) == 0 {
		return t.readAppSetting(setting)
	}
	value, err := coerceSupportedSettingValue(setting, input.Value)
	if err != nil {
		return t.marshal(output{
			Success:   false,
			Operation: "set",
			Setting:   setting.Key,
			Error:     err.Error(),
		})
	}
	return t.writeAppSetting(setting, value)
}

func (t *Tool) readGlobalSetting(setting globalSetting) (toolkit.Result, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return t.marshal(output{
			Success: false,
			Setting: setting.Key,
			Error:   err.Error(),
		})
	}
	return t.marshal(output{
		Success:   true,
		Operation: "get",
		Setting:   setting.Key,
		Value:     setting.Get(cfg),
	})
}

func (t *Tool) writeGlobalSetting(setting globalSetting, value string) (toolkit.Result, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return t.marshal(output{
			Success:   false,
			Operation: "set",
			Setting:   setting.Key,
			Error:     err.Error(),
		})
	}
	previous := setting.Get(cfg)
	setting.Set(&cfg, value)
	if err := appconfig.Save(cfg); err != nil {
		return t.marshal(output{
			Success:   false,
			Operation: "set",
			Setting:   setting.Key,
			Error:     err.Error(),
		})
	}
	return t.marshal(output{
		Success:       true,
		Operation:     "set",
		Setting:       setting.Key,
		PreviousValue: previous,
		NewValue:      setting.Get(cfg),
	})
}

func (t *Tool) readAppSetting(setting appsettings.SupportedSetting) (toolkit.Result, error) {
	merged := appsettings.LoadMergedSettings(t.workingDir)
	if len(merged.Errors) > 0 {
		return t.marshal(output{
			Success: false,
			Setting: setting.Key,
			Error:   fmt.Sprintf("cannot read invalid settings: %s", merged.Errors[0].Message),
		})
	}
	value, ok := appsettings.ReadSettingValue(merged.Settings, setting)
	if !ok {
		value = nil
	}
	return t.marshal(output{
		Success:   true,
		Operation: "get",
		Setting:   setting.Key,
		Value:     value,
	})
}

func (t *Tool) writeAppSetting(setting appsettings.SupportedSetting, value any) (toolkit.Result, error) {
	merged := appsettings.LoadMergedSettings(t.workingDir)
	var previous any
	if len(merged.Errors) == 0 {
		previous, _ = appsettings.ReadSettingValue(merged.Settings, setting)
	}
	update := appsettings.BuildSettingUpdate(setting, value)
	if err := appsettings.UpdateSettingsForSource(appsettings.EditableUser, t.workingDir, update); err != nil {
		return t.marshal(output{
			Success:   false,
			Operation: "set",
			Setting:   setting.Key,
			Error:     err.Error(),
		})
	}
	return t.marshal(output{
		Success:       true,
		Operation:     "set",
		Setting:       setting.Key,
		PreviousValue: previous,
		NewValue:      value,
	})
}

func (t *Tool) marshal(result output) (toolkit.Result, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func coerceSupportedSettingValue(setting appsettings.SupportedSetting, raw json.RawMessage) (any, error) {
	switch setting.Type {
	case appsettings.SettingTypeBoolean:
		return coerceBooleanValue(setting.Key, raw)
	case appsettings.SettingTypeString:
		value, err := coerceStringValue(setting.Key, raw, setting.Options)
		if err != nil {
			return nil, err
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported setting type %q", setting.Type)
	}
}

func coerceBooleanValue(setting string, raw json.RawMessage) (bool, error) {
	var boolValue bool
	if err := json.Unmarshal(raw, &boolValue); err == nil {
		return boolValue, nil
	}

	var stringValue string
	if err := json.Unmarshal(raw, &stringValue); err == nil {
		parsed, err := strconv.ParseBool(strings.TrimSpace(stringValue))
		if err != nil {
			return false, fmt.Errorf("%s requires true or false", setting)
		}
		return parsed, nil
	}

	return false, fmt.Errorf("%s requires true or false", setting)
}

func coerceStringValue(setting string, raw json.RawMessage, options []string) (string, error) {
	var stringValue string
	switch {
	case json.Unmarshal(raw, &stringValue) == nil:
		stringValue = strings.TrimSpace(stringValue)
	default:
		var scalar any
		if err := json.Unmarshal(raw, &scalar); err != nil {
			return "", fmt.Errorf("invalid value for %s", setting)
		}
		stringValue = strings.TrimSpace(fmt.Sprint(scalar))
	}

	if stringValue == "" {
		return "", fmt.Errorf("%s requires a non-empty string", setting)
	}
	if len(options) > 0 && !stringIn(stringValue, options) {
		return "", fmt.Errorf("invalid value %q for %s; options: %s", stringValue, setting, strings.Join(options, ", "))
	}
	return stringValue, nil
}

func stringIn(value string, allowed []string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

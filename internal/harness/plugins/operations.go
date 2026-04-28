package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func IsPluginEnabled(pluginID string, enabledPlugins map[string]bool, defaultEnabled bool) bool {
	if enabledPlugins == nil {
		return defaultEnabled
	}
	if value, ok := enabledPlugins[pluginID]; ok {
		return value
	}
	return defaultEnabled
}

func SetEnabledPlugin(document map[string]any, pluginID string, enabled bool) error {
	if pluginID == "" {
		return fmt.Errorf("plugin ID is required")
	}
	if document == nil {
		return fmt.Errorf("settings document is required")
	}
	raw, _ := document["enabledPlugins"].(map[string]any)
	if raw == nil {
		raw = map[string]any{}
		document["enabledPlugins"] = raw
	}
	raw[pluginID] = enabled
	return nil
}

func UpdateEnabledPluginSetting(settingsPath string, pluginID string, enabled bool) error {
	if settingsPath == "" {
		return fmt.Errorf("settings path is required")
	}
	document := map[string]any{}
	data, err := os.ReadFile(settingsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
	}
	if err := SetEnabledPlugin(document, pluginID, enabled); err != nil {
		return err
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(data, '\n'), 0o644)
}

func RemoveEnabledPluginSetting(settingsPath string, pluginID string) error {
	if settingsPath == "" {
		return fmt.Errorf("settings path is required")
	}
	document := map[string]any{}
	data, err := os.ReadFile(settingsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
	}
	if raw, ok := document["enabledPlugins"].(map[string]any); ok {
		delete(raw, pluginID)
		if len(raw) == 0 {
			delete(document, "enabledPlugins")
		}
	}
	data, err = json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(data, '\n'), 0o644)
}

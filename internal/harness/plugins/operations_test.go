package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateEnabledPluginSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := UpdateEnabledPluginSetting(path, "demo@market", true); err != nil {
		t.Fatalf("update setting: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	enabled := doc["enabledPlugins"].(map[string]any)
	if enabled["demo@market"] != true {
		t.Fatalf("unexpected enabledPlugins: %#v", enabled)
	}
}

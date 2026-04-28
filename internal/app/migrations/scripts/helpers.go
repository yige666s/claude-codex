package scripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	appconfig "claude-codex/internal/app/config"
	"claude-codex/internal/app/settings"
	"claude-codex/internal/public/fsutil"
)

func loadRawConfig() (settings.Document, string, error) {
	path, err := appconfig.ConfigPath()
	if err != nil {
		return nil, "", err
	}
	return readJSONDocument(path)
}

func saveRawConfig(path string, doc settings.Document) error {
	return writeJSONDocument(path, doc)
}

func loadSettings(source settings.SettingSource, workingDir string) (settings.Document, string, error) {
	path := settings.SettingsFilePathForSource(source, workingDir)
	return readJSONDocument(path)
}

func saveSettings(path string, doc settings.Document) error {
	return writeJSONDocument(path, doc)
}

func readJSONDocument(path string) (settings.Document, string, error) {
	if strings.TrimSpace(path) == "" {
		return settings.Document{}, path, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return settings.Document{}, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return settings.Document{}, path, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, path, err
	}
	return settings.Document(raw), path, nil
}

func writeJSONDocument(path string, doc settings.Document) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func workingDirFromContext() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return ""
}

func boolValue(doc settings.Document, key string) bool {
	value, ok := doc[key].(bool)
	return ok && value
}

func stringValue(doc settings.Document, key string) string {
	value, _ := doc[key].(string)
	return strings.TrimSpace(value)
}

func setUserSetting(updates settings.Document) error {
	workingDir := workingDirFromContext()
	doc, path, err := loadSettings(settings.SourceUser, workingDir)
	if err != nil {
		return err
	}
	merged := settings.MergeDocumentsReplacingArrays(doc, updates)
	return saveSettings(path, merged)
}

func setLocalSetting(updates settings.Document) error {
	workingDir := workingDirFromContext()
	doc, path, err := loadSettings(settings.SourceLocal, workingDir)
	if err != nil {
		return err
	}
	merged := settings.MergeDocuments(doc, updates)
	return saveSettings(path, merged)
}

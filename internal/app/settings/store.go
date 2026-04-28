package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"claude-codex/internal/public/fsutil"
)

var (
	remotePolicyMu    sync.RWMutex
	remotePolicyCache Document
)

func ParseSettingsFile(path string) SettingsWithErrors {
	return parseSettingsFile(path)
}

func parseSettingsFile(path string) SettingsWithErrors {
	content, err := os.ReadFile(path)
	if err != nil {
		return SettingsWithErrors{Settings: nil}
	}
	if strings.TrimSpace(string(content)) == "" {
		return SettingsWithErrors{Settings: Document{}}
	}

	var raw any
	if err := json.Unmarshal(content, &raw); err != nil {
		return SettingsWithErrors{
			Errors: []ValidationError{{
				File:    path,
				Path:    "root",
				Message: "invalid JSON: " + err.Error(),
			}},
		}
	}

	filtered, warnings := FilterInvalidPermissionRules(raw, path)
	doc, ok := filtered.(map[string]any)
	if !ok {
		return SettingsWithErrors{Settings: Document{}, Errors: warnings}
	}

	result := validateSettingsFileContent(string(mustJSON(doc)), false)
	if !result.IsValid {
		return SettingsWithErrors{
			Settings: nil,
			Errors: append(warnings, ValidationError{
				File:    path,
				Path:    "root",
				Message: result.Error,
			}),
		}
	}
	return SettingsWithErrors{Settings: doc, Errors: warnings}
}

func LoadSettingsForSource(source SettingSource, workingDir string) SettingsWithErrors {
	if source == SourcePolicy {
		remotePolicyMu.RLock()
		remote := CloneDocument(remotePolicyCache)
		remotePolicyMu.RUnlock()
		if len(remote) > 0 {
			return SettingsWithErrors{Settings: remote}
		}
		mdm := GetMDMSettings()
		if len(mdm.Settings) > 0 {
			return mdm
		}
		managed := LoadManagedFileSettings()
		if len(managed.Settings) > 0 {
			return managed
		}
		hkcu := GetHKCUSettings()
		if len(hkcu.Settings) > 0 {
			return hkcu
		}
		return SettingsWithErrors{Settings: Document{}}
	}
	path := SettingsFilePathForSource(source, workingDir)
	if path == "" {
		return SettingsWithErrors{Settings: Document{}}
	}
	return parseSettingsFile(path)
}

func SetRemoteManagedSettingsCache(doc Document) {
	remotePolicyMu.Lock()
	defer remotePolicyMu.Unlock()
	remotePolicyCache = CloneDocument(doc)
}

func GetRemoteManagedSettingsCache() Document {
	remotePolicyMu.RLock()
	defer remotePolicyMu.RUnlock()
	return CloneDocument(remotePolicyCache)
}

func ClearRemoteManagedSettingsCache() {
	SetRemoteManagedSettingsCache(Document{})
}

func LoadMergedSettings(workingDir string, sources ...SettingSource) SettingsWithErrors {
	if len(sources) == 0 {
		sources = SettingSources
	}
	var merged Document
	var errs []ValidationError
	for _, source := range sources {
		result := LoadSettingsForSource(source, workingDir)
		if result.Settings != nil {
			merged = MergeDocuments(merged, result.Settings)
		}
		errs = append(errs, result.Errors...)
	}
	if merged == nil {
		merged = Document{}
	}
	return SettingsWithErrors{Settings: merged, Errors: errs}
}

func UpdateSettingsForSource(source EditableSettingSource, workingDir string, updates Document) error {
	path := SettingsFilePathForSource(SettingSource(source), workingDir)
	if path == "" {
		return nil
	}

	existing := parseSettingsFile(path).Settings
	if existing == nil {
		rawExisting, err := readRawSettingsDocument(path)
		if err != nil {
			return err
		}
		existing = rawExisting
	}
	updated := MergeDocumentsReplacingArrays(existing, updates)
	if updated == nil {
		updated = Document{}
	}
	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func readRawSettingsDocument(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Document{}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return Document{}, nil
	}
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON syntax in settings file at %s: %w", path, err)
	}
	doc, ok := raw.(map[string]any)
	if !ok {
		return Document{}, nil
	}
	return Document(doc), nil
}

func MergeDocuments(base, overlay Document) Document {
	return mergeDocuments(base, overlay, true)
}

func MergeDocumentsReplacingArrays(base, overlay Document) Document {
	return mergeDocuments(base, overlay, false)
}

func mergeDocuments(base, overlay Document, mergeArrays bool) Document {
	if base == nil && overlay == nil {
		return nil
	}
	if base == nil {
		return CloneDocument(overlay)
	}
	result := CloneDocument(base)
	for key, value := range overlay {
		if value == nil {
			delete(result, key)
			continue
		}
		if existingMap, ok := asDocument(result[key]); ok {
			if incomingMap, ok := asDocument(value); ok {
				result[key] = mergeDocuments(existingMap, incomingMap, mergeArrays)
				continue
			}
		}
		if mergeArrays {
			if existingArray, ok := result[key].([]any); ok {
				if incomingArray, ok := value.([]any); ok {
					result[key] = mergeUniqueArrays(existingArray, incomingArray)
					continue
				}
			}
		}
		result[key] = deepCloneAny(value)
	}
	return result
}

func mergeUniqueArrays(base, overlay []any) []any {
	out := make([]any, 0, len(base)+len(overlay))
	seen := map[string]struct{}{}
	for _, item := range append(base, overlay...) {
		key := stableArrayItemKey(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, deepCloneAny(item))
	}
	return out
}

func stableArrayItemKey(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("%#v", value))
	}
	return string(data)
}

func CloneDocument(doc Document) Document {
	if doc == nil {
		return nil
	}
	out := make(Document, len(doc))
	for k, v := range doc {
		out[k] = deepCloneAny(v)
	}
	return out
}

func deepCloneAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return CloneDocument(v)
	case Document:
		return CloneDocument(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = deepCloneAny(v[i])
		}
		return out
	default:
		return v
	}
}

func asDocument(value any) (Document, bool) {
	switch v := value.(type) {
	case map[string]any:
		return Document(v), true
	case Document:
		return v, true
	default:
		return nil, false
	}
}

func ProjectID(workingDir string) string {
	gitDir := filepath.Join(workingDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return sanitizeProjectID(workingDir)
	}
	return sanitizeProjectID(workingDir)
}

func sanitizeProjectID(value string) string {
	replacer := strings.NewReplacer(string(filepath.Separator), "_", ":", "_", " ", "_")
	return replacer.Replace(strings.TrimSpace(value))
}

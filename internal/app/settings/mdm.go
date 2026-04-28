package settings

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

var (
	mdmCache  = SettingsWithErrors{Settings: Document{}}
	hkcuCache = SettingsWithErrors{Settings: Document{}}
	mdmMu     sync.RWMutex
)

func GetMDMSettings() SettingsWithErrors {
	mdmMu.RLock()
	defer mdmMu.RUnlock()
	return cloneSettingsWithErrors(mdmCache)
}

func GetHKCUSettings() SettingsWithErrors {
	mdmMu.RLock()
	defer mdmMu.RUnlock()
	return cloneSettingsWithErrors(hkcuCache)
}

func SetMDMSettingsCache(mdm, hkcu SettingsWithErrors) {
	mdmMu.Lock()
	defer mdmMu.Unlock()
	mdmCache = cloneSettingsWithErrors(mdm)
	hkcuCache = cloneSettingsWithErrors(hkcu)
}

func RefreshMDMSettings() (SettingsWithErrors, SettingsWithErrors) {
	mdm := readAdminManagedSettings()
	hkcu := readHKCUSettings()
	return mdm, hkcu
}

func LoadManagedFileSettings() SettingsWithErrors {
	var merged Document
	var errors []ValidationError

	base := parseSettingsFile(ManagedSettingsFilePath())
	if base.Settings != nil {
		merged = MergeDocuments(merged, base.Settings)
	}
	errors = append(errors, base.Errors...)

	entries, err := os.ReadDir(ManagedSettingsDropInDir())
	if err == nil {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		for _, name := range names {
			result := parseSettingsFile(filepath.Join(ManagedSettingsDropInDir(), name))
			if result.Settings != nil {
				merged = MergeDocuments(merged, result.Settings)
			}
			errors = append(errors, result.Errors...)
		}
	}

	if merged == nil {
		merged = Document{}
	}
	return SettingsWithErrors{Settings: merged, Errors: errors}
}

func readAdminManagedSettings() SettingsWithErrors {
	switch runtime.GOOS {
	case "darwin":
		return readDarwinManagedSettings()
	case "windows":
		return readWindowsRegistrySettings(`HKLM\SOFTWARE\Policies\ClaudeCode`)
	default:
		return LoadManagedFileSettings()
	}
}

func readHKCUSettings() SettingsWithErrors {
	if runtime.GOOS != "windows" {
		return SettingsWithErrors{Settings: Document{}}
	}
	return readWindowsRegistrySettings(`HKCU\SOFTWARE\Policies\ClaudeCode`)
}

func readDarwinManagedSettings() SettingsWithErrors {
	path := filepath.Join(ManagedFilePath(), "com.anthropic.claudecode.plist")
	if _, err := os.Stat(path); err != nil {
		return LoadManagedFileSettings()
	}
	output, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		return SettingsWithErrors{Settings: Document{}}
	}
	var raw any
	if err := json.Unmarshal(output, &raw); err != nil {
		return SettingsWithErrors{Settings: Document{}}
	}
	filtered, warnings := FilterInvalidPermissionRules(raw, path)
	doc, ok := filtered.(map[string]any)
	if !ok {
		return SettingsWithErrors{Settings: Document{}}
	}
	res := validateSettingsFileContent(string(mustJSON(doc)), false)
	if !res.IsValid && len(warnings) == 0 {
		return SettingsWithErrors{Settings: Document{}, Errors: []ValidationError{{File: path, Path: "root", Message: res.Error}}}
	}
	return SettingsWithErrors{Settings: doc, Errors: warnings}
}

func readWindowsRegistrySettings(key string) SettingsWithErrors {
	output, err := exec.Command("reg", "query", key, "/v", "Settings").Output()
	if err != nil {
		return SettingsWithErrors{Settings: Document{}}
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "REG_") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		value := strings.Join(parts[2:], " ")
		var raw any
		if err := json.Unmarshal([]byte(value), &raw); err != nil {
			return SettingsWithErrors{Settings: Document{}}
		}
		filtered, warnings := FilterInvalidPermissionRules(raw, key)
		doc, ok := filtered.(map[string]any)
		if !ok {
			return SettingsWithErrors{Settings: Document{}}
		}
		return SettingsWithErrors{Settings: doc, Errors: warnings}
	}
	return SettingsWithErrors{Settings: Document{}}
}

func cloneSettingsWithErrors(value SettingsWithErrors) SettingsWithErrors {
	return SettingsWithErrors{
		Settings: CloneDocument(value.Settings),
		Errors:   append([]ValidationError(nil), value.Errors...),
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}

func init() {
	SetMDMSettingsCache(SettingsWithErrors{Settings: Document{}}, SettingsWithErrors{Settings: Document{}})
}

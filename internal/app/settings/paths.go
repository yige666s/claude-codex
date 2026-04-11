package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	managedSettingsEnvOverride = "CLAUDE_GO_MANAGED_SETTINGS_PATH"
	flagSettingsEnvPath        = "CLAUDE_GO_FLAG_SETTINGS_PATH"
)

func ClaudeConfigHomeDir() string {
	if value := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_HOME")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

func ManagedFilePath() string {
	if value := strings.TrimSpace(os.Getenv(managedSettingsEnvOverride)); value != "" {
		return value
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
}

func ManagedSettingsFilePath() string {
	return filepath.Join(ManagedFilePath(), "managed-settings.json")
}

func ManagedSettingsDropInDir() string {
	return filepath.Join(ManagedFilePath(), "managed-settings.d")
}

func RelativeSettingsFilePath(source SettingSource) string {
	switch source {
	case SourceProject:
		return filepath.Join(".claude", "settings.json")
	case SourceLocal:
		return filepath.Join(".claude", "settings.local.json")
	default:
		return ""
	}
}

func SettingsRootPath(source SettingSource, workingDir string) string {
	switch source {
	case SourceUser:
		return ClaudeConfigHomeDir()
	case SourceProject, SourceLocal, SourcePolicy:
		if strings.TrimSpace(workingDir) == "" {
			cwd, err := os.Getwd()
			if err == nil {
				return cwd
			}
		}
		return workingDir
	case SourceFlag:
		path := strings.TrimSpace(os.Getenv(flagSettingsEnvPath))
		if path == "" {
			if strings.TrimSpace(workingDir) == "" {
				cwd, err := os.Getwd()
				if err == nil {
					return cwd
				}
			}
			return workingDir
		}
		return filepath.Dir(path)
	default:
		return workingDir
	}
}

func SettingsFilePathForSource(source SettingSource, workingDir string) string {
	switch source {
	case SourceUser:
		return filepath.Join(SettingsRootPath(source, workingDir), "settings.json")
	case SourceProject, SourceLocal:
		return filepath.Join(SettingsRootPath(source, workingDir), RelativeSettingsFilePath(source))
	case SourcePolicy:
		return ManagedSettingsFilePath()
	case SourceFlag:
		if path := strings.TrimSpace(os.Getenv(flagSettingsEnvPath)); path != "" {
			return path
		}
		return ""
	default:
		return ""
	}
}

package utils

import (
	"os"
	"path/filepath"
	"strings"
)

func GetClaudeConfigHomeDir() string {
	if value := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude-codex"
	}
	return filepath.Join(home, ".claude-codex")
}

func GetTeamsDir() string {
	return filepath.Join(GetClaudeConfigHomeDir(), "teams")
}

func HasNodeOption(flag string) bool {
	nodeOptions := os.Getenv("NODE_OPTIONS")
	if nodeOptions == "" {
		return false
	}
	for _, item := range strings.Fields(nodeOptions) {
		if item == flag {
			return true
		}
	}
	return false
}

func IsEnvTruthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func IsEnvDefinedFalsy(value any) bool {
	switch v := value.(type) {
	case bool:
		return !v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "no", "off":
			return true
		}
	}
	return false
}

func ParseEnvVars(raw []string) (map[string]string, error) {
	parsed := make(map[string]string, len(raw))
	for _, item := range raw {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, &ParseEnvError{Value: item}
		}
		parsed[parts[0]] = parts[1]
	}
	return parsed, nil
}

type ParseEnvError struct {
	Value string
}

func (e *ParseEnvError) Error() string {
	return "invalid environment variable format: " + e.Value
}

func GetAWSRegion() string {
	if value := os.Getenv("AWS_REGION"); value != "" {
		return value
	}
	if value := os.Getenv("AWS_DEFAULT_REGION"); value != "" {
		return value
	}
	return "us-east-1"
}

func GetDefaultVertexRegion() string {
	if value := os.Getenv("CLOUD_ML_REGION"); value != "" {
		return value
	}
	return "us-east5"
}

func IsBareMode() bool {
	return IsEnvTruthy(os.Getenv("CLAUDE_CODE_SIMPLE"))
}

func ShouldMaintainProjectWorkingDir() bool {
	return IsEnvTruthy(os.Getenv("CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR"))
}

package memdir

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	AutoMemDirName        = "memory"
	AutoMemEntrypointName = "MEMORY.md"
)

// IsAutoMemoryEnabled checks if auto-memory features are enabled
// Priority chain (first defined wins):
//  1. CLAUDE_CODE_DISABLE_AUTO_MEMORY env var (1/true → OFF, 0/false → ON)
//  2. CLAUDE_CODE_SIMPLE (--bare) → OFF
//  3. CCR without persistent storage → OFF (no CLAUDE_CODE_REMOTE_MEMORY_DIR)
//  4. Default: enabled
func IsAutoMemoryEnabled() bool {
	envVal := os.Getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY")
	if isEnvTruthy(envVal) {
		return false
	}
	if isEnvDefinedFalsy(envVal) {
		return true
	}

	// --bare / SIMPLE mode
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_SIMPLE")) {
		return false
	}

	// CCR without persistent storage
	if isEnvTruthy(os.Getenv("CLAUDE_CODE_REMOTE")) &&
		os.Getenv("CLAUDE_CODE_REMOTE_MEMORY_DIR") == "" {
		return false
	}

	return true
}

// GetMemoryBaseDir returns the base directory for persistent memory storage
// Resolution order:
//  1. CLAUDE_CODE_REMOTE_MEMORY_DIR env var (explicit override, set in CCR)
//  2. ~/.claude (default config home)
func GetMemoryBaseDir() string {
	if dir := os.Getenv("CLAUDE_CODE_REMOTE_MEMORY_DIR"); dir != "" {
		return dir
	}
	return getClaudeConfigHomeDir()
}

// ValidateMemoryPath normalizes and validates a candidate auto-memory directory path
// SECURITY: Rejects paths that would be dangerous as a read-allowlist root
func ValidateMemoryPath(raw string, expandTilde bool) (string, bool) {
	if raw == "" {
		return "", false
	}

	candidate := raw

	// Tilde expansion for settings.json paths
	if expandTilde && (strings.HasPrefix(candidate, "~/") || strings.HasPrefix(candidate, "~\\")) {
		rest := candidate[2:]
		// Reject trivial remainders that would expand to $HOME or an ancestor
		restNorm := filepath.Clean(rest)
		if restNorm == "." || restNorm == ".." {
			return "", false
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		candidate = filepath.Join(homeDir, rest)
	}

	// Normalize and strip trailing separators
	normalized := filepath.Clean(candidate)
	normalized = strings.TrimRight(normalized, string(filepath.Separator))

	// Security checks
	if !filepath.IsAbs(normalized) ||
		len(normalized) < 3 ||
		regexp.MustCompile(`^[A-Za-z]:$`).MatchString(normalized) ||
		strings.HasPrefix(normalized, "\\\\") ||
		strings.HasPrefix(normalized, "//") ||
		strings.Contains(normalized, "\x00") {
		return "", false
	}

	// Add exactly one trailing separator and normalize Unicode
	result := normalized + string(filepath.Separator)
	if !utf8.ValidString(result) {
		return "", false
	}

	return result, true
}

// GetAutoMemPath returns the auto-memory directory path
// Resolution order:
//  1. CLAUDE_COWORK_MEMORY_PATH_OVERRIDE env var (Cowork override)
//  2. autoMemoryDirectory in settings.json
//  3. {base}/projects/{sanitized-cwd}/memory/ (default)
func GetAutoMemPath(projectRoot string) string {
	// Direct override from Cowork
	if override := os.Getenv("CLAUDE_COWORK_MEMORY_PATH_OVERRIDE"); override != "" {
		if validated, ok := ValidateMemoryPath(override, false); ok {
			return validated
		}
	}

	// TODO: Check settings.json for autoMemoryDirectory
	// For now, use default path

	// Default: {base}/projects/{sanitized-cwd}/memory/
	base := GetMemoryBaseDir()
	sanitized := sanitizePath(projectRoot)
	return filepath.Join(base, "projects", sanitized, AutoMemDirName) + string(filepath.Separator)
}

// GetAutoMemEntrypoint returns the auto-memory entrypoint (MEMORY.md)
func GetAutoMemEntrypoint(projectRoot string) string {
	return filepath.Join(GetAutoMemPath(projectRoot), AutoMemEntrypointName)
}

// GetAutoMemDailyLogPath returns the daily log file path for the given date
// Shape: <autoMemPath>/logs/YYYY/MM/YYYY-MM-DD.md
func GetAutoMemDailyLogPath(projectRoot string, date string) string {
	// date format: YYYY-MM-DD
	parts := strings.Split(date, "-")
	if len(parts) != 3 {
		return ""
	}
	yyyy, mm := parts[0], parts[1]
	return filepath.Join(GetAutoMemPath(projectRoot), "logs", yyyy, mm, date+".md")
}

// IsAutoMemPath checks if an absolute path is within the auto-memory directory
func IsAutoMemPath(absolutePath, projectRoot string) bool {
	normalized := filepath.Clean(absolutePath)
	autoMemPath := GetAutoMemPath(projectRoot)
	return strings.HasPrefix(normalized, autoMemPath)
}

// Helper functions

func isEnvTruthy(val string) bool {
	return val == "1" || val == "true" || val == "TRUE"
}

func isEnvDefinedFalsy(val string) bool {
	return val == "0" || val == "false" || val == "FALSE"
}

func getClaudeConfigHomeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_HOME"); dir != "" {
		return dir
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".claude")
}

func sanitizePath(path string) string {
	// Replace path separators with dashes
	sanitized := strings.ReplaceAll(path, string(filepath.Separator), "-")
	sanitized = strings.ReplaceAll(sanitized, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")
	// Replace multiple consecutive dashes with single dash
	re := regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	return sanitized
}

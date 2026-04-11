package sandboxadapter

import (
	"os"
	"path/filepath"
	"strings"
)

func ResolvePathPatternForSandbox(pattern string, settingsRoot string) string {
	switch {
	case strings.HasPrefix(pattern, "//"):
		return pattern[1:]
	case strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "//"):
		return filepath.Clean(filepath.Join(settingsRoot, pattern[1:]))
	default:
		return pattern
	}
}

func ResolveSandboxFilesystemPath(pattern string, settingsRoot string) string {
	if strings.HasPrefix(pattern, "//") {
		return pattern[1:]
	}
	if strings.HasPrefix(pattern, "~") {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, strings.TrimPrefix(pattern, "~"))
		}
	}
	if filepath.IsAbs(pattern) {
		return filepath.Clean(pattern)
	}
	return filepath.Clean(filepath.Join(settingsRoot, pattern))
}

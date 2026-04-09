package memdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathTraversalError is thrown when path validation detects a traversal or injection attempt
type PathTraversalError struct {
	Message string
}

func (e *PathTraversalError) Error() string {
	return e.Message
}

// IsTeamMemoryEnabled checks if team memory features are enabled
// Team memory requires auto memory to be enabled
func IsTeamMemoryEnabled() bool {
	if !IsAutoMemoryEnabled() {
		return false
	}
	// TODO: Check feature flag 'tengu_herring_clock'
	return false // Disabled by default until feature flag system is implemented
}

// GetTeamMemPath returns the team memory path
// Lives as a subdirectory of the auto-memory directory
func GetTeamMemPath(projectRoot string) string {
	return filepath.Join(GetAutoMemPath(projectRoot), "team") + string(filepath.Separator)
}

// GetTeamMemEntrypoint returns the team memory entrypoint (MEMORY.md)
func GetTeamMemEntrypoint(projectRoot string) string {
	return filepath.Join(GetTeamMemPath(projectRoot), AutoMemEntrypointName)
}

// SanitizePathKey sanitizes a file path key by rejecting dangerous patterns
func SanitizePathKey(key string) error {
	// Null bytes can truncate paths in C-based syscalls
	if strings.Contains(key, "\x00") {
		return &PathTraversalError{Message: fmt.Sprintf("Null byte in path key: %q", key)}
	}

	// URL-encoded traversals (e.g. %2e%2e%2f = ../)
	decoded := key
	// Simple URL decode check for common patterns
	if strings.Contains(key, "%") {
		decoded = strings.ReplaceAll(key, "%2e", ".")
		decoded = strings.ReplaceAll(decoded, "%2E", ".")
		decoded = strings.ReplaceAll(decoded, "%2f", "/")
		decoded = strings.ReplaceAll(decoded, "%2F", "/")
	}
	if decoded != key && (strings.Contains(decoded, "..") || strings.Contains(decoded, "/")) {
		return &PathTraversalError{Message: fmt.Sprintf("URL-encoded traversal in path key: %q", key)}
	}

	// Unicode normalization attacks
	normalized := normalizeNFKC(key)
	if normalized != key &&
		(strings.Contains(normalized, "..") ||
			strings.Contains(normalized, "/") ||
			strings.Contains(normalized, "\\") ||
			strings.Contains(normalized, "\x00")) {
		return &PathTraversalError{Message: fmt.Sprintf("Unicode-normalized traversal in path key: %q", key)}
	}

	// Reject backslashes (Windows path separator used as traversal vector)
	if strings.Contains(key, "\\") {
		return &PathTraversalError{Message: fmt.Sprintf("Backslash in path key: %q", key)}
	}

	// Reject absolute paths
	if strings.HasPrefix(key, "/") {
		return &PathTraversalError{Message: fmt.Sprintf("Absolute path key: %q", key)}
	}

	return nil
}

// RealpathDeepestExisting resolves symlinks for the deepest existing ancestor of a path
func RealpathDeepestExisting(absolutePath string) (string, error) {
	tail := []string{}
	current := absolutePath

	for {
		parent := filepath.Dir(current)
		if current == parent {
			// Reached filesystem root
			break
		}

		// Try to resolve symlinks
		realCurrent, err := filepath.EvalSymlinks(current)
		if err == nil {
			// Success - rejoin the tail
			if len(tail) == 0 {
				return realCurrent, nil
			}
			// Reverse tail and join
			for i := len(tail) - 1; i >= 0; i-- {
				realCurrent = filepath.Join(realCurrent, tail[i])
			}
			return realCurrent, nil
		}

		// Check if it's a dangling symlink
		if _, statErr := os.Lstat(current); statErr == nil {
			// lstat succeeded - check if it's a symlink
			info, _ := os.Lstat(current)
			if info.Mode()&os.ModeSymlink != 0 {
				return "", &PathTraversalError{
					Message: fmt.Sprintf("Dangling symlink detected (target does not exist): %q", current),
				}
			}
		}

		// Path doesn't exist yet - pop and continue
		tail = append(tail, filepath.Base(current))
		current = parent
	}

	return "", errors.New("failed to resolve path")
}

// IsRealPathWithinTeamDir checks if a real path is within the team directory
func IsRealPathWithinTeamDir(realPath, projectRoot string) (bool, error) {
	teamDir := GetTeamMemPath(projectRoot)
	realTeamDir, err := filepath.EvalSymlinks(teamDir)
	if err != nil {
		// Team dir doesn't exist yet - use the path as-is
		realTeamDir = teamDir
	}
	return strings.HasPrefix(realPath, realTeamDir), nil
}

// ValidateTeamMemPath validates a file path against the team memory directory
func ValidateTeamMemPath(filePath, projectRoot string) (string, error) {
	teamDir := GetTeamMemPath(projectRoot)

	// First pass: normalize .. segments and check string-level containment
	resolvedPath := filepath.Clean(filePath)
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(teamDir, resolvedPath)
	}

	if !strings.HasPrefix(resolvedPath, teamDir) {
		return "", &PathTraversalError{
			Message: fmt.Sprintf("Path escapes team memory directory: %q", filePath),
		}
	}

	// Second pass: resolve symlinks and verify real containment
	realPath, err := RealpathDeepestExisting(resolvedPath)
	if err != nil {
		return "", err
	}

	withinTeam, err := IsRealPathWithinTeamDir(realPath, projectRoot)
	if err != nil {
		return "", err
	}
	if !withinTeam {
		return "", &PathTraversalError{
			Message: fmt.Sprintf("Path escapes team memory directory via symlink: %q", filePath),
		}
	}

	return resolvedPath, nil
}

// ValidateTeamMemKey validates a relative path key from the server
func ValidateTeamMemKey(relativeKey, projectRoot string) (string, error) {
	if err := SanitizePathKey(relativeKey); err != nil {
		return "", err
	}

	teamDir := GetTeamMemPath(projectRoot)
	fullPath := filepath.Join(teamDir, relativeKey)

	// First pass: normalize and check containment
	resolvedPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(resolvedPath, teamDir) {
		return "", &PathTraversalError{
			Message: fmt.Sprintf("Key escapes team memory directory: %q", relativeKey),
		}
	}

	// Second pass: resolve symlinks and verify real containment
	realPath, err := RealpathDeepestExisting(resolvedPath)
	if err != nil {
		return "", err
	}

	withinTeam, err := IsRealPathWithinTeamDir(realPath, projectRoot)
	if err != nil {
		return "", err
	}
	if !withinTeam {
		return "", &PathTraversalError{
			Message: fmt.Sprintf("Key escapes team memory directory via symlink: %q", relativeKey),
		}
	}

	return resolvedPath, nil
}

// IsTeamMemPath checks if a path is within the team memory directory
func IsTeamMemPath(filePath, projectRoot string) bool {
	normalized := filepath.Clean(filePath)
	teamPath := GetTeamMemPath(projectRoot)
	return strings.HasPrefix(normalized, teamPath)
}

// IsTeamMemFile checks if a file path is within the team memory directory
// and team memory is enabled
func IsTeamMemFile(filePath, projectRoot string) bool {
	return IsTeamMemoryEnabled() && IsTeamMemPath(filePath, projectRoot)
}

// Helper: Simple NFKC normalization (basic implementation)
func normalizeNFKC(s string) string {
	// This is a simplified version - full NFKC requires unicode package
	var result strings.Builder
	for _, r := range s {
		// Convert fullwidth characters to ASCII equivalents
		if r >= 0xFF01 && r <= 0xFF5E {
			result.WriteRune(r - 0xFEE0)
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

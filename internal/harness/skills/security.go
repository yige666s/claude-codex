package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Security utilities for safe file operations

// ValidateSkillPath validates a skill-relative path and prevents directory traversal
func ValidateSkillPath(baseDir, relPath string) (string, error) {
	// Normalize the relative path
	normalized := filepath.Clean(relPath)

	// Check for absolute paths
	if filepath.IsAbs(normalized) {
		return "", fmt.Errorf("skill file path must be relative: %s", relPath)
	}

	// Check for parent directory traversal
	if strings.HasPrefix(normalized, ".."+string(filepath.Separator)) ||
		strings.Contains(normalized, string(filepath.Separator)+".."+string(filepath.Separator)) ||
		strings.HasSuffix(normalized, string(filepath.Separator)+"..") ||
		normalized == ".." {
		return "", fmt.Errorf("skill file path escapes skill dir: %s", relPath)
	}

	// Join with base directory
	fullPath := filepath.Join(baseDir, normalized)

	// Verify the result is still under baseDir
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base dir: %w", err)
	}

	absFull, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve full path: %w", err)
	}

	if !strings.HasPrefix(absFull, absBase+string(filepath.Separator)) && absFull != absBase {
		return "", fmt.Errorf("skill file path escapes skill dir: %s", relPath)
	}

	return fullPath, nil
}

// SafeWriteFile writes a file with security protections:
// - O_EXCL: fail if file exists (no overwrite)
// - O_NOFOLLOW: don't follow symlinks (Unix)
// - 0600: owner read/write only
func SafeWriteFile(path string, content []byte) error {
	// Create parent directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open with safe flags
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL

	// Add O_NOFOLLOW on Unix systems (not available on Windows)
	if syscall.O_NOFOLLOW != 0 {
		flags |= syscall.O_NOFOLLOW
	}

	file, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("file already exists: %s", path)
		}
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Write content
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// GetFileIdentity returns a canonical path for a file by resolving symlinks
// This is used for deduplication - files accessed through different paths
// (e.g., via symlinks) will have the same identity
func GetFileIdentity(path string) (string, error) {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	absPath, err := filepath.Abs(realPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	return absPath, nil
}

// IsPathSafe checks if a path is safe to use (no traversal, no absolute paths)
func IsPathSafe(path string) bool {
	// Reject absolute paths
	if filepath.IsAbs(path) {
		return false
	}

	// Reject paths with parent directory references (before normalization)
	if strings.Contains(path, "..") {
		return false
	}

	// Normalize and check again
	normalized := filepath.Clean(path)

	// After normalization, check for parent references
	parts := strings.Split(normalized, string(filepath.Separator))
	for _, part := range parts {
		if part == ".." {
			return false
		}
	}

	return true
}

// SanitizeSkillName sanitizes a skill name to be filesystem-safe
func SanitizeSkillName(name string) string {
	// Replace unsafe characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\x00", "_",
	)

	sanitized := replacer.Replace(name)

	// Trim whitespace
	sanitized = strings.TrimSpace(sanitized)

	// Ensure not empty
	if sanitized == "" {
		sanitized = "unnamed"
	}

	return sanitized
}

// CreateSecureDirectory creates a directory with secure permissions (0700)
func CreateSecureDirectory(path string) error {
	return os.MkdirAll(path, 0700)
}

// WriteSkillFiles writes multiple skill files to disk safely
func WriteSkillFiles(baseDir string, files map[string]string) error {
	// Group files by parent directory
	byParent := make(map[string][]struct {
		path    string
		content string
	})

	for relPath, content := range files {
		fullPath, err := ValidateSkillPath(baseDir, relPath)
		if err != nil {
			return err
		}

		parent := filepath.Dir(fullPath)
		byParent[parent] = append(byParent[parent], struct {
			path    string
			content string
		}{fullPath, content})
	}

	// Create directories and write files
	for parent, entries := range byParent {
		// Create parent directory
		if err := CreateSecureDirectory(parent); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", parent, err)
		}

		// Write files
		for _, entry := range entries {
			if err := SafeWriteFile(entry.path, []byte(entry.content)); err != nil {
				return fmt.Errorf("failed to write file %s: %w", entry.path, err)
			}
		}
	}

	return nil
}

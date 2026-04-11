package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"claude-codex/internal/app/config"
	"claude-codex/internal/public/fsutil"
)

const (
	DefaultMaxMCPOutputChars = 100000
	PreviewSizeBytes         = 2000
)

func toolResultsDir() (string, error) {
	home, err := config.AppHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "tool-results"), nil
}

func PersistToolResult(content, prefix string) (*PersistedToolResult, error) {
	dir, err := toolResultsDir()
	if err != nil {
		return nil, err
	}
	id := randomID()
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.txt", sanitizeName(prefix), id))
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return nil, err
	}
	preview := content
	hasMore := false
	if len(preview) > PreviewSizeBytes {
		preview = preview[:PreviewSizeBytes]
		hasMore = true
	}
	return &PersistedToolResult{
		Filepath:     path,
		OriginalSize: len(content),
		Preview:      preview,
		HasMore:      hasMore,
	}, nil
}

func BuildLargeOutputInstructions(result *PersistedToolResult, formatDescription string) string {
	if result == nil {
		return ""
	}
	message := fmt.Sprintf("Error: result (%d characters) exceeds maximum allowed size. Output has been saved to %s.\nFormat: %s\nUse Read with offsets or Grep to inspect the persisted file.\n", result.OriginalSize, result.Filepath, formatDescription)
	if result.Preview != "" {
		message += "\nPreview:\n" + result.Preview
		if result.HasMore {
			message += "\n..."
		}
	}
	return message
}

func randomID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "random"
	}
	return hex.EncodeToString(buf)
}

func sanitizeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "mcp"
	}
	return value
}

func EnsureToolResultsDir() error {
	dir, err := toolResultsDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

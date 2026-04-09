package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolvePath(rootDir, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("path is required")
	}

	resolvedRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", err
	}

	resolvedTarget := target
	if !filepath.IsAbs(resolvedTarget) {
		resolvedTarget = filepath.Join(resolvedRoot, resolvedTarget)
	}

	resolvedTarget = filepath.Clean(resolvedTarget)
	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %s escapes project root %s", target, resolvedRoot)
	}

	return resolvedTarget, nil
}

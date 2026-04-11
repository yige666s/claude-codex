package utils

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func ExpandPath(path string, baseDir string) (string, error) {
	if baseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		baseDir = cwd
	}
	if strings.Contains(path, "\x00") || strings.Contains(baseDir, "\x00") {
		return "", errNullBytePath
	}

	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return filepath.Clean(baseDir), nil
	}

	home, _ := os.UserHomeDir()
	switch {
	case trimmed == "~":
		return home, nil
	case strings.HasPrefix(trimmed, "~/"):
		return filepath.Join(home, trimmed[2:]), nil
	}

	processed := trimmed
	if runtime.GOOS == "windows" && regexp.MustCompile(`^/[a-zA-Z]/`).MatchString(trimmed) {
		processed = strings.ToUpper(trimmed[1:2]) + ":\\" + strings.ReplaceAll(trimmed[3:], "/", "\\")
	}

	if filepath.IsAbs(processed) {
		return filepath.Clean(processed), nil
	}
	return filepath.Clean(filepath.Join(baseDir, processed)), nil
}

func ToRelativePath(absolutePath string, cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	rel, err := filepath.Rel(cwd, absolutePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return absolutePath
	}
	return rel
}

func GetDirectoryForPath(path string, baseDir string) (string, error) {
	absolute, err := ExpandPath(path, baseDir)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(absolute, `\\`) || strings.HasPrefix(absolute, "//") {
		return filepath.Dir(absolute), nil
	}
	info, err := os.Stat(absolute)
	if err == nil && info.IsDir() {
		return absolute, nil
	}
	return filepath.Dir(absolute), nil
}

func ContainsPathTraversal(path string) bool {
	return regexp.MustCompile(`(^|[\\/])\.\.([\\/]|$)`).MatchString(path)
}

func NormalizePathForConfigKey(path string) string {
	return strings.ReplaceAll(filepath.Clean(path), `\`, `/`)
}

var errNullBytePath = &PathError{Message: "path contains null bytes"}

type PathError struct {
	Message string
}

func (e *PathError) Error() string { return e.Message }

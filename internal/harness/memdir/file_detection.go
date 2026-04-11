package memdir

import (
	"path/filepath"
	"runtime"
	"strings"
)

type MemoryScope string

const (
	MemoryScopePersonal MemoryScope = "personal"
	MemoryScopeTeam     MemoryScope = "team"
)

// DetectSessionFileType mirrors the TS session/transcript detection for paths
// under the Claude config home.
func DetectSessionFileType(filePath string) string {
	configDir := comparablePath(GetMemoryBaseDir())
	normalized := comparablePath(filePath)
	if !strings.HasPrefix(normalized, configDir) {
		return ""
	}
	switch {
	case strings.Contains(normalized, "/session-memory/") && strings.HasSuffix(normalized, ".md"):
		return "session_memory"
	case strings.Contains(normalized, "/projects/") && strings.HasSuffix(normalized, ".jsonl"):
		return "session_transcript"
	default:
		return ""
	}
}

func DetectSessionPatternType(pattern string) string {
	normalized := strings.ReplaceAll(pattern, `\`, `/`)
	switch {
	case strings.Contains(normalized, "session-memory") && (strings.Contains(normalized, ".md") || strings.HasSuffix(normalized, "*")):
		return "session_memory"
	case strings.Contains(normalized, ".jsonl") || (strings.Contains(normalized, "projects") && strings.Contains(normalized, "*.jsonl")):
		return "session_transcript"
	default:
		return ""
	}
}

func MemoryScopeForPath(filePath, projectRoot string) MemoryScope {
	if IsTeamMemPath(filePath, projectRoot) {
		return MemoryScopeTeam
	}
	if IsAutoMemPath(filePath, projectRoot) {
		return MemoryScopePersonal
	}
	return ""
}

func IsAutoManagedMemoryFile(filePath, projectRoot string) bool {
	return IsAutoMemPath(filePath, projectRoot) ||
		IsTeamMemPath(filePath, projectRoot) ||
		DetectSessionFileType(filePath) != ""
}

func IsMemoryDirectory(dirPath, projectRoot string) bool {
	normalized := comparablePath(filepath.Clean(dirPath))
	autoMem := comparablePath(strings.TrimRight(GetAutoMemPath(projectRoot), `/\`))
	teamMem := comparablePath(strings.TrimRight(GetTeamMemPath(projectRoot), `/\`))
	baseDir := comparablePath(GetMemoryBaseDir())
	if strings.Contains(normalized, "/agent-memory/") || strings.Contains(normalized, "/agent-memory-local/") {
		return true
	}
	if teamMem != "" && (normalized == teamMem || strings.HasPrefix(normalized, teamMem+"/")) {
		return true
	}
	if autoMem != "" && (normalized == autoMem || strings.HasPrefix(normalized, autoMem+"/")) {
		return true
	}
	if !strings.HasPrefix(normalized, baseDir) {
		return false
	}
	return strings.Contains(normalized, "/session-memory/") || strings.Contains(normalized, "/projects/") || strings.Contains(normalized, "/memory/")
}

func IsAutoManagedMemoryPattern(pattern string) bool {
	if DetectSessionPatternType(pattern) != "" {
		return true
	}
	normalized := strings.ReplaceAll(pattern, `\`, `/`)
	return strings.Contains(normalized, "agent-memory/") || strings.Contains(normalized, "agent-memory-local/")
}

func comparablePath(path string) string {
	path = strings.ReplaceAll(path, `\`, `/`)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

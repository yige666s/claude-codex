package memdir

import (
	"os"
	"path/filepath"
	"strings"
)

// SessionFileType identifies the kind of session file.
type SessionFileType string

const (
	SessionFileTypeMemory     SessionFileType = "session_memory"
	SessionFileTypeTranscript SessionFileType = "session_transcript"
)

// MemoryScope indicates whether a memory belongs to the personal or team store.
type MemoryScope string

const (
	MemoryScopePersonal MemoryScope = "personal"
	MemoryScopeTeam     MemoryScope = "team"
)

// DetectSessionFileType checks whether a file path is a Claude-managed session
// file.  Returns the type and true on match, or zero value and false otherwise.
//
// Mirrors src/utils/memoryFileDetection.ts detectSessionFileType.
func DetectSessionFileType(filePath string) (SessionFileType, bool) {
	configDir := getClaudeConfigHomeDir()
	norm := toComparable(filePath)
	base := toComparable(configDir)

	if !strings.HasPrefix(norm, base) {
		return "", false
	}
	if strings.Contains(norm, "/session-memory/") && strings.HasSuffix(norm, ".md") {
		return SessionFileTypeMemory, true
	}
	if strings.Contains(norm, "/projects/") && strings.HasSuffix(norm, ".jsonl") {
		return SessionFileTypeTranscript, true
	}
	return "", false
}

// DetectSessionPatternType checks a glob/pattern string for session file access
// intent without requiring a real path.
func DetectSessionPatternType(pattern string) (SessionFileType, bool) {
	norm := strings.ReplaceAll(pattern, string(filepath.Separator), "/")
	if strings.Contains(norm, "session-memory") &&
		(strings.Contains(norm, ".md") || strings.HasSuffix(norm, "*")) {
		return SessionFileTypeMemory, true
	}
	if strings.Contains(norm, ".jsonl") ||
		(strings.Contains(norm, "projects") && strings.Contains(norm, "*.jsonl")) {
		return SessionFileTypeTranscript, true
	}
	return "", false
}

// IsAutoMemFile returns true if filePath is within the personal auto-memory
// directory.
func IsAutoMemFile(filePath string) bool {
	if !IsAutoMemoryEnabled() {
		return false
	}
	// Use empty projectRoot — GetAutoMemPath falls back to config dir.
	return IsAutoMemPath(filePath, "")
}

// MemoryScopeForPath returns the scope (personal / team) of a memory path,
// or empty string if the path is not a managed memory path.
func MemoryScopeForPath(projectRoot, filePath string) (MemoryScope, bool) {
	if IsTeamMemoryEnabled() && IsTeamMemFile(filePath, projectRoot) {
		return MemoryScopeTeam, true
	}
	if IsAutoMemFile(filePath) {
		return MemoryScopePersonal, true
	}
	return "", false
}

// IsAutoManagedMemoryFile returns true for any Claude-managed memory file
// (auto-memory, team memory, session memory/transcripts).  Returns false for
// user-managed instruction files like CLAUDE.md or .claude/rules/*.md.
func IsAutoManagedMemoryFile(projectRoot, filePath string) bool {
	if IsAutoMemFile(filePath) {
		return true
	}
	if IsTeamMemoryEnabled() && IsTeamMemFile(filePath, projectRoot) {
		return true
	}
	if _, ok := DetectSessionFileType(filePath); ok {
		return true
	}
	return false
}

// IsMemoryDirectory returns true when dirPath is a Claude-managed memory
// directory.
func IsMemoryDirectory(dirPath string) bool {
	norm := toComparable(filepath.Clean(dirPath))

	if IsAutoMemoryEnabled() {
		if strings.Contains(norm, "/agent-memory/") ||
			strings.Contains(norm, "/agent-memory-local/") {
			return true
		}
		autoMem := GetAutoMemPath("")
		autoNorm := toComparable(strings.TrimRight(autoMem, string(filepath.Separator)+"/"))
		if norm == autoNorm || strings.HasPrefix(norm, toComparable(autoMem)) {
			return true
		}
	}

	configDir := getClaudeConfigHomeDir()
	configNorm := toComparable(configDir)
	memBase := toComparable(GetMemoryBaseDir())

	underConfig := strings.HasPrefix(norm, configNorm)
	underMemBase := strings.HasPrefix(norm, memBase)

	if !underConfig && !underMemBase {
		return false
	}
	if strings.Contains(norm, "/session-memory/") {
		return true
	}
	if underConfig && strings.Contains(norm, "/projects/") {
		return true
	}
	if IsAutoMemoryEnabled() && strings.Contains(norm, "/memory/") {
		return true
	}
	return false
}

// IsAutoManagedMemoryPattern returns true when a glob pattern targets
// auto-managed memory files (excludes user-managed CLAUDE.md etc.).
func IsAutoManagedMemoryPattern(pattern string) bool {
	if _, ok := DetectSessionPatternType(pattern); ok {
		return true
	}
	slash := strings.ReplaceAll(pattern, string(filepath.Separator), "/")
	if IsAutoMemoryEnabled() &&
		(strings.Contains(slash, "agent-memory/") ||
			strings.Contains(slash, "agent-memory-local/")) {
		return true
	}
	return false
}

// toComparable normalises a path for string comparison (forward slashes,
// lower-cased on Windows where the FS is case-insensitive).
func toComparable(p string) string {
	slash := strings.ReplaceAll(p, string(filepath.Separator), "/")
	if os.PathSeparator == '\\' { // Windows
		return strings.ToLower(slash)
	}
	return slash
}

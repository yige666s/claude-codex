package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	systemContextCache   map[string]string
	systemContextCacheMu sync.RWMutex
	systemContextOnce    sync.Once

	userContextCache   map[string]string
	userContextCacheMu sync.RWMutex
	userContextOnce    sync.Once

	systemPromptInjection string
	injectionMu           sync.RWMutex
)

// GetSystemPromptInjection returns the current system prompt injection
func GetSystemPromptInjection() string {
	injectionMu.RLock()
	defer injectionMu.RUnlock()
	return systemPromptInjection
}

// SetSystemPromptInjection sets the system prompt injection and clears caches
func SetSystemPromptInjection(value string) {
	injectionMu.Lock()
	systemPromptInjection = value
	injectionMu.Unlock()

	// Clear context caches
	ClearSystemContextCache()
	ClearUserContextCache()
}

// GetSystemContext retrieves system-level context (git status, cache breaker)
func GetSystemContext(workingDir string, includeGitStatus bool) (map[string]string, error) {
	systemContextOnce.Do(func() {
		ctx := make(map[string]string)

		// Get git status if requested
		if includeGitStatus {
			gitInfo, err := GetGitStatus(workingDir)
			if err == nil {
				ctx["gitStatus"] = FormatGitStatus(gitInfo)
			}
		}

		// Include system prompt injection if set
		injection := GetSystemPromptInjection()
		if injection != "" {
			ctx["cacheBreaker"] = fmt.Sprintf("[CACHE_BREAKER: %s]", injection)
		}

		systemContextCacheMu.Lock()
		systemContextCache = ctx
		systemContextCacheMu.Unlock()
	})

	systemContextCacheMu.RLock()
	defer systemContextCacheMu.RUnlock()

	if systemContextCache == nil {
		return make(map[string]string), nil
	}

	// Return a copy to prevent external modification
	result := make(map[string]string)
	for k, v := range systemContextCache {
		result[k] = v
	}

	return result, nil
}

// GetUserContext retrieves user-level context (CLAUDE.md, current date)
func GetUserContext(workingDir string, disableClaudeMd bool) (map[string]string, error) {
	userContextOnce.Do(func() {
		ctx := make(map[string]string)

		// Load CLAUDE.md if not disabled
		if !disableClaudeMd {
			claudeMd, err := loadClaudeMd(workingDir)
			if err == nil && claudeMd != "" {
				ctx["claudeMd"] = claudeMd
			}
		}

		// Add current date
		ctx["currentDate"] = fmt.Sprintf("Today's date is %s.", getCurrentDate())

		userContextCacheMu.Lock()
		userContextCache = ctx
		userContextCacheMu.Unlock()
	})

	userContextCacheMu.RLock()
	defer userContextCacheMu.RUnlock()

	if userContextCache == nil {
		return make(map[string]string), nil
	}

	// Return a copy to prevent external modification
	result := make(map[string]string)
	for k, v := range userContextCache {
		result[k] = v
	}

	return result, nil
}

// loadClaudeMd loads CLAUDE.md files from the working directory
func loadClaudeMd(workingDir string) (string, error) {
	var contents []string

	// Look for CLAUDE.md in current directory and parent directories
	currentDir := workingDir
	for {
		claudeMdPath := filepath.Join(currentDir, "CLAUDE.md")
		if _, err := os.Stat(claudeMdPath); err == nil {
			content, err := os.ReadFile(claudeMdPath)
			if err == nil {
				contents = append(contents, string(content))
			}
		}

		// Check if we've reached the root
		parent := filepath.Dir(currentDir)
		if parent == currentDir {
			break
		}
		currentDir = parent
	}

	// Also check global CLAUDE.md
	homeDir, err := os.UserHomeDir()
	if err == nil {
		globalClaudeMd := filepath.Join(homeDir, ".claude", "CLAUDE.md")
		if _, err := os.Stat(globalClaudeMd); err == nil {
			content, err := os.ReadFile(globalClaudeMd)
			if err == nil {
				contents = append([]string{string(content)}, contents...)
			}
		}
	}

	if len(contents) == 0 {
		return "", nil
	}

	return strings.Join(contents, "\n\n---\n\n"), nil
}

// getCurrentDate returns the current date in ISO format
func getCurrentDate() string {
	return time.Now().Format("2006/01/02")
}

// ClearSystemContextCache clears the system context cache
func ClearSystemContextCache() {
	systemContextCacheMu.Lock()
	defer systemContextCacheMu.Unlock()
	systemContextCache = nil
	systemContextOnce = sync.Once{}
}

// ClearUserContextCache clears the user context cache
func ClearUserContextCache() {
	userContextCacheMu.Lock()
	defer userContextCacheMu.Unlock()
	userContextCache = nil
	userContextOnce = sync.Once{}
}

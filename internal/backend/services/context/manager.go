package context

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	maxStatusChars = 2000
)

// SystemContext contains system-level context information.
type SystemContext struct {
	GitStatus    string `json:"git_status,omitempty"`
	CacheBreaker string `json:"cache_breaker,omitempty"`
}

// UserContext contains user-level context information.
type UserContext struct {
	ClaudeMd    string `json:"claude_md,omitempty"`
	CurrentDate string `json:"current_date"`
}

// Manager manages context collection and caching.
type Manager struct {
	mu                   sync.RWMutex
	projectRoot          string
	systemContextCache   *SystemContext
	userContextCache     *UserContext
	systemContextCached  bool
	userContextCached    bool
	gitStatusCache       string
	gitStatusCached      bool
	systemPromptInjection string
}

// NewManager creates a new context manager.
func NewManager(projectRoot string) *Manager {
	return &Manager{
		projectRoot: projectRoot,
	}
}

// GetSystemContext returns system context (cached for session).
func (m *Manager) GetSystemContext(includeGit bool) (*SystemContext, error) {
	m.mu.RLock()
	if m.systemContextCached {
		ctx := m.systemContextCache
		m.mu.RUnlock()
		return ctx, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if m.systemContextCached {
		return m.systemContextCache, nil
	}

	ctx := &SystemContext{}

	if includeGit {
		gitStatus, err := m.getGitStatus()
		if err == nil && gitStatus != "" {
			ctx.GitStatus = gitStatus
		}
	}

	if m.systemPromptInjection != "" {
		ctx.CacheBreaker = fmt.Sprintf("[CACHE_BREAKER: %s]", m.systemPromptInjection)
	}

	m.systemContextCache = ctx
	m.systemContextCached = true

	return ctx, nil
}

// GetUserContext returns user context (cached for session).
func (m *Manager) GetUserContext(claudeMdContent string) (*UserContext, error) {
	m.mu.RLock()
	if m.userContextCached {
		ctx := m.userContextCache
		m.mu.RUnlock()
		return ctx, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if m.userContextCached {
		return m.userContextCache, nil
	}

	ctx := &UserContext{
		CurrentDate: GetLocalISODate(),
	}

	if claudeMdContent != "" {
		ctx.ClaudeMd = claudeMdContent
	}

	m.userContextCache = ctx
	m.userContextCached = true

	return ctx, nil
}

// getGitStatus collects git status information.
func (m *Manager) getGitStatus() (string, error) {
	m.mu.RLock()
	if m.gitStatusCached {
		status := m.gitStatusCache
		m.mu.RUnlock()
		return status, nil
	}
	m.mu.RUnlock()

	// Check if this is a git repository
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = m.projectRoot
	if err := cmd.Run(); err != nil {
		return "", nil // Not a git repo
	}

	// Get branch name
	branch, err := m.execGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}

	// Get default branch
	mainBranch, _ := m.getDefaultBranch()

	// Get status
	status, err := m.execGit("--no-optional-locks", "status", "--short")
	if err != nil {
		return "", err
	}

	// Truncate if too long
	if len(status) > maxStatusChars {
		status = status[:maxStatusChars] + "\n... (truncated because it exceeds 2k characters. If you need more information, run \"git status\" using BashTool)"
	}

	// Get recent commits
	log, err := m.execGit("--no-optional-locks", "log", "--oneline", "-n", "5")
	if err != nil {
		log = ""
	}

	// Get user name
	userName, _ := m.execGit("config", "user.name")

	// Build status string
	var parts []string
	parts = append(parts, "This is the git status at the start of the conversation. Note that this status is a snapshot in time, and will not update during the conversation.")
	parts = append(parts, fmt.Sprintf("Current branch: %s", strings.TrimSpace(branch)))
	parts = append(parts, fmt.Sprintf("Main branch (you will usually use this for PRs): %s", strings.TrimSpace(mainBranch)))

	if userName != "" {
		parts = append(parts, fmt.Sprintf("Git user: %s", strings.TrimSpace(userName)))
	}

	statusDisplay := strings.TrimSpace(status)
	if statusDisplay == "" {
		statusDisplay = "(clean)"
	}
	parts = append(parts, fmt.Sprintf("Status:\n%s", statusDisplay))

	if log != "" {
		parts = append(parts, fmt.Sprintf("Recent commits:\n%s", strings.TrimSpace(log)))
	}

	result := strings.Join(parts, "\n\n")

	m.mu.Lock()
	m.gitStatusCache = result
	m.gitStatusCached = true
	m.mu.Unlock()

	return result, nil
}

// getDefaultBranch attempts to determine the default branch.
func (m *Manager) getDefaultBranch() (string, error) {
	// Try to get from remote
	output, err := m.execGit("symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		parts := strings.Split(strings.TrimSpace(output), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fallback to common names
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "rev-parse", "--verify", branch)
		cmd.Dir = m.projectRoot
		if err := cmd.Run(); err == nil {
			return branch, nil
		}
	}

	return "main", nil
}

// execGit executes a git command and returns stdout.
func (m *Manager) execGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = m.projectRoot
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// SetSystemPromptInjection sets cache breaker injection.
func (m *Manager) SetSystemPromptInjection(value string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.systemPromptInjection = value
	m.ClearCache()
}

// GetSystemPromptInjection returns the current injection value.
func (m *Manager) GetSystemPromptInjection() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.systemPromptInjection
}

// ClearCache clears all cached context.
func (m *Manager) ClearCache() {
	m.systemContextCache = nil
	m.userContextCache = nil
	m.systemContextCached = false
	m.userContextCached = false
	m.gitStatusCache = ""
	m.gitStatusCached = false
}

// GetLocalISODate returns the current date in ISO format (YYYY/MM/DD).
func GetLocalISODate() string {
	now := time.Now()
	return now.Format("2006/01/02")
}

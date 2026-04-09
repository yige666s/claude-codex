package context

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	MaxStatusChars = 2000
)

var (
	gitStatusCache     *GitStatusInfo
	gitStatusCacheMu   sync.RWMutex
	gitStatusCacheOnce sync.Once
)

// GetGitStatus retrieves git repository status information
func GetGitStatus(workingDir string) (*GitStatusInfo, error) {
	gitStatusCacheOnce.Do(func() {
		info, err := fetchGitStatus(workingDir)
		if err == nil {
			gitStatusCacheMu.Lock()
			gitStatusCache = info
			gitStatusCacheMu.Unlock()
		}
	})

	gitStatusCacheMu.RLock()
	defer gitStatusCacheMu.RUnlock()

	if gitStatusCache == nil {
		return nil, fmt.Errorf("git status not available")
	}

	return gitStatusCache, nil
}

// fetchGitStatus performs the actual git status retrieval
func fetchGitStatus(workingDir string) (*GitStatusInfo, error) {
	// Check if directory is a git repository
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = workingDir
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("not a git repository")
	}

	info := &GitStatusInfo{
		Timestamp: time.Now(),
	}

	// Get current branch
	branch, err := execGitCommand(workingDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	info.CurrentBranch = strings.TrimSpace(branch)

	// Get main branch
	mainBranch, err := getMainBranch(workingDir)
	if err != nil {
		return nil, err
	}
	info.MainBranch = mainBranch

	// Get git user
	userName, err := execGitCommand(workingDir, "config", "user.name")
	if err == nil {
		info.GitUser = strings.TrimSpace(userName)
	}

	// Get status
	status, err := execGitCommand(workingDir, "--no-optional-locks", "status", "--short")
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)

	// Truncate status if too long
	if len(status) > MaxStatusChars {
		status = status[:MaxStatusChars] + "\n... (truncated because it exceeds 2k characters. If you need more information, run \"git status\" using BashTool)"
	}

	if status == "" {
		info.Status = "(clean)"
	} else {
		info.Status = status
	}

	// Get recent commits
	log, err := execGitCommand(workingDir, "--no-optional-locks", "log", "--oneline", "-n", "5")
	if err != nil {
		return nil, err
	}
	info.RecentCommits = strings.TrimSpace(log)

	return info, nil
}

// getMainBranch determines the main branch name
func getMainBranch(workingDir string) (string, error) {
	// Try common main branch names
	branches := []string{"main", "master"}

	for _, branch := range branches {
		cmd := exec.Command("git", "rev-parse", "--verify", branch)
		cmd.Dir = workingDir
		if err := cmd.Run(); err == nil {
			return branch, nil
		}
	}

	// Try to get default branch from remote
	output, err := execGitCommand(workingDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		parts := strings.Split(strings.TrimSpace(output), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fallback to main
	return "main", nil
}

// execGitCommand executes a git command and returns stdout
func execGitCommand(workingDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workingDir
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// FormatGitStatus formats git status info for display
func FormatGitStatus(info *GitStatusInfo) string {
	var parts []string

	parts = append(parts, "This is the git status at the start of the conversation. Note that this status is a snapshot in time, and will not update during the conversation.")
	parts = append(parts, fmt.Sprintf("Current branch: %s", info.CurrentBranch))
	parts = append(parts, fmt.Sprintf("Main branch (you will usually use this for PRs): %s", info.MainBranch))

	if info.GitUser != "" {
		parts = append(parts, fmt.Sprintf("Git user: %s", info.GitUser))
	}

	parts = append(parts, fmt.Sprintf("Status:\n%s", info.Status))
	parts = append(parts, fmt.Sprintf("Recent commits:\n%s", info.RecentCommits))

	return strings.Join(parts, "\n\n")
}

// ClearGitStatusCache clears the cached git status (for testing)
func ClearGitStatusCache() {
	gitStatusCacheMu.Lock()
	defer gitStatusCacheMu.Unlock()
	gitStatusCache = nil
	gitStatusCacheOnce = sync.Once{}
}

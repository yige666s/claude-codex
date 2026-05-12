package coordinator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type WorktreeManager struct {
	root   string
	mu     sync.Mutex
	active *WorktreeSession
}

type WorktreeSession struct {
	OriginalRoot   string `json:"originalRoot"`
	OriginalHead   string `json:"originalHead,omitempty"`
	WorktreePath   string `json:"worktreePath"`
	WorktreeBranch string `json:"worktreeBranch"`
	CreatedBranch  bool   `json:"createdBranch"`
}

type ExitWorktreeOptions struct {
	Action         string
	DiscardChanges bool
}

type ExitWorktreeResult struct {
	Action          string `json:"action"`
	OriginalRoot    string `json:"originalRoot,omitempty"`
	WorktreePath    string `json:"worktreePath,omitempty"`
	WorktreeBranch  string `json:"worktreeBranch,omitempty"`
	ChangedFiles    int    `json:"changedFiles,omitempty"`
	UnmergedCommits int    `json:"unmergedCommits,omitempty"`
	RemovedWorktree bool   `json:"removedWorktree"`
	RemovedBranch   bool   `json:"removedBranch"`
	Message         string `json:"message"`
}

func NewWorktreeManager(root string) *WorktreeManager {
	return &WorktreeManager{root: root}
}

func (m *WorktreeManager) Enter(branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("branch is required")
	}

	gitRoot, err := m.gitRoot()
	if err != nil {
		return "", err
	}

	originalHead, err := gitOutput(gitRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}

	dir := filepath.Join(gitRoot, ".claude-worktrees", branch)
	if _, err := os.Stat(dir); err == nil {
		m.setActive(WorktreeSession{
			OriginalRoot:   gitRoot,
			OriginalHead:   originalHead,
			WorktreePath:   dir,
			WorktreeBranch: branch,
			CreatedBranch:  false,
		})
		return dir, nil
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}

	createdBranch := true
	if err := runGit(gitRoot, "worktree", "add", dir, "-b", branch, "HEAD"); err != nil {
		if err := runGit(gitRoot, "worktree", "add", dir, branch); err != nil {
			return "", err
		}
		createdBranch = false
	}
	m.setActive(WorktreeSession{
		OriginalRoot:   gitRoot,
		OriginalHead:   originalHead,
		WorktreePath:   dir,
		WorktreeBranch: branch,
		CreatedBranch:  createdBranch,
	})
	return dir, nil
}

func (m *WorktreeManager) Exit(options ExitWorktreeOptions) (ExitWorktreeResult, error) {
	action := strings.TrimSpace(options.Action)
	if action == "" {
		action = "keep"
	}
	if action != "keep" && action != "remove" {
		return ExitWorktreeResult{}, fmt.Errorf("action must be keep or remove")
	}

	m.mu.Lock()
	session := m.active
	if session == nil {
		m.mu.Unlock()
		return ExitWorktreeResult{
			Action:  action,
			Message: "No active EnterWorktree session to exit. No filesystem changes were made.",
		}, nil
	}
	active := *session
	if action == "keep" {
		m.active = nil
		m.mu.Unlock()
		return ExitWorktreeResult{
			Action:         action,
			OriginalRoot:   active.OriginalRoot,
			WorktreePath:   active.WorktreePath,
			WorktreeBranch: active.WorktreeBranch,
			Message:        fmt.Sprintf("Kept worktree %s and returned to %s.", active.WorktreePath, active.OriginalRoot),
		}, nil
	}
	m.mu.Unlock()

	changedFiles, err := changedFileCount(active.WorktreePath)
	if err != nil {
		return ExitWorktreeResult{}, err
	}
	unmergedCommits, err := commitCount(active.WorktreePath, active.OriginalHead)
	if err != nil {
		return ExitWorktreeResult{}, err
	}
	if !options.DiscardChanges && (changedFiles > 0 || unmergedCommits > 0) {
		return ExitWorktreeResult{}, fmt.Errorf("worktree has %d changed files and %d commits; re-run with discard_changes=true to remove it", changedFiles, unmergedCommits)
	}

	args := []string{"worktree", "remove", active.WorktreePath}
	if options.DiscardChanges {
		args = append(args, "--force")
	}
	if err := runGit(active.OriginalRoot, args...); err != nil {
		return ExitWorktreeResult{}, err
	}

	removedBranch := false
	if active.CreatedBranch {
		branchArgs := []string{"branch", "-d", active.WorktreeBranch}
		if options.DiscardChanges {
			branchArgs = []string{"branch", "-D", active.WorktreeBranch}
		}
		if err := runGit(active.OriginalRoot, branchArgs...); err != nil {
			return ExitWorktreeResult{}, err
		}
		removedBranch = true
	}

	m.mu.Lock()
	if m.active != nil && m.active.WorktreePath == active.WorktreePath {
		m.active = nil
	}
	m.mu.Unlock()

	return ExitWorktreeResult{
		Action:          action,
		OriginalRoot:    active.OriginalRoot,
		WorktreePath:    active.WorktreePath,
		WorktreeBranch:  active.WorktreeBranch,
		ChangedFiles:    changedFiles,
		UnmergedCommits: unmergedCommits,
		RemovedWorktree: true,
		RemovedBranch:   removedBranch,
		Message:         fmt.Sprintf("Removed worktree %s.", active.WorktreePath),
	}, nil
}

func (m *WorktreeManager) setActive(session WorktreeSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = &session
}

func (r ExitWorktreeResult) JSON() (string, error) {
	encoded, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func (m *WorktreeManager) gitRoot() (string, error) {
	root := m.root
	if root == "" {
		root = "."
	}
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve git root: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func changedFileCount(dir string) (int, error) {
	status, err := gitOutput(dir, "status", "--porcelain")
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(status) == "" {
		return 0, nil
	}
	count := 0
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count, nil
}

func commitCount(dir, originalHead string) (int, error) {
	if strings.TrimSpace(originalHead) == "" {
		return 0, fmt.Errorf("original head is unknown; refusing to infer worktree commit safety")
	}
	countText, err := gitOutput(dir, "rev-list", "--count", originalHead+"..HEAD")
	if err != nil {
		return 0, err
	}
	var count int
	if _, err := fmt.Sscanf(strings.TrimSpace(countText), "%d", &count); err != nil {
		return 0, fmt.Errorf("parse worktree commit count %q: %w", strings.TrimSpace(countText), err)
	}
	return count, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func runGit(dir string, args ...string) error {
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

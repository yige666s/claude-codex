package coordinator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WorktreeManager struct {
	root string
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

	dir := filepath.Join(gitRoot, ".claude-worktrees", branch)
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}

	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}

	if err := runGit(gitRoot, "worktree", "add", dir, "-b", branch, "HEAD"); err != nil {
		if err := runGit(gitRoot, "worktree", "add", dir, branch); err != nil {
			return "", err
		}
	}
	return dir, nil
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

func runGit(dir string, args ...string) error {
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

package worktree

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/harness/coordinator"
)

func TestEnterToolCreatesWorktree(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")

	tool := NewEnterTool(coordinator.NewWorktreeManager(root))
	raw, _ := json.Marshal(map[string]any{"branch": "feature-tui"})
	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("enter worktree: %v", err)
	}
	if _, err := os.Stat(result.Output); err != nil {
		t.Fatalf("expected worktree path: %v", err)
	}
}

func TestExitToolNoopsWithoutActiveWorktree(t *testing.T) {
	tool := NewExitTool(coordinator.NewWorktreeManager(t.TempDir()))
	raw, _ := json.Marshal(map[string]any{"action": "keep"})
	result, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("exit worktree: %v", err)
	}
	if !strings.Contains(result.Output, "No active EnterWorktree session") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestExitToolRefusesToRemoveDirtyWorktreeWithoutDiscard(t *testing.T) {
	root := initGitRepo(t)
	manager := coordinator.NewWorktreeManager(root)
	enter := NewEnterTool(manager)
	exit := NewExitTool(manager)

	raw, _ := json.Marshal(map[string]any{"branch": "feature-dirty"})
	result, err := enter.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("enter worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(result.Output, "dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, _ = json.Marshal(map[string]any{"action": "remove"})
	_, err = exit.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "discard_changes=true") {
		t.Fatalf("expected dirty worktree safety error, got %v", err)
	}
}

func TestExitToolRemovesCleanWorktree(t *testing.T) {
	root := initGitRepo(t)
	manager := coordinator.NewWorktreeManager(root)
	enter := NewEnterTool(manager)
	exit := NewExitTool(manager)

	raw, _ := json.Marshal(map[string]any{"branch": "feature-clean"})
	result, err := enter.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("enter worktree: %v", err)
	}
	worktreePath := result.Output

	raw, _ = json.Marshal(map[string]any{"action": "remove"})
	result, err = exit.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("exit worktree: %v", err)
	}
	if !strings.Contains(result.Output, `"removedWorktree":true`) {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree to be removed, stat err: %v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

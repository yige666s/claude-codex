package worktree

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/harness/coordinator"
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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

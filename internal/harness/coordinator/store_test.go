package coordinator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCreateAndDelete(t *testing.T) {
	store := NewTeamManager(t.TempDir())
	team, err := store.Create("alpha")
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if team.Name != "alpha" {
		t.Fatalf("unexpected team: %#v", team)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("unexpected list: %#v err=%v", list, err)
	}
	removed, err := store.Delete("alpha")
	if err != nil || !removed {
		t.Fatalf("delete team: removed=%v err=%v", removed, err)
	}
	list, err = store.List()
	if err != nil || len(list) != 0 {
		t.Fatalf("unexpected list after delete: %#v err=%v", list, err)
	}
}

func TestWorktreeManagerEnter(t *testing.T) {
	root := t.TempDir()
	runGitTest(t, root, "init")
	runGitTest(t, root, "config", "user.email", "test@example.com")
	runGitTest(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "README.md")
	runGitTest(t, root, "commit", "-m", "init")

	manager := NewWorktreeManager(root)
	path, err := manager.Enter("feature/test")
	if err != nil {
		t.Fatalf("enter worktree: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected worktree dir: %v", err)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := runGit(dir, args...); err != nil {
		t.Fatal(err)
	}
}

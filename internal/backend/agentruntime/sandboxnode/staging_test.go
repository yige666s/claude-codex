package sandboxnode

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"claude-codex/internal/backend/agentruntime/sandboxartifact"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

func TestNodeStagesCachedSkillAndPrivateInput(t *testing.T) {
	base := t.TempDir()
	source := filepath.Join(base, "skills")
	skill := filepath.Join(source, "demo")
	workRoot := filepath.Join(base, "work")
	workspace := filepath.Join(workRoot, "alice")
	for _, dir := range []string{skill, workspace} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte("private-data"), 0600); err != nil {
		t.Fatal(err)
	}
	lim := sandboxtransfer.DevLimits()
	store := sandboxartifact.Store{Dir: filepath.Join(base, "published"), Builder: sandboxtransfer.Builder{SkillRoots: []string{source}, Limits: lim}}
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	input, err := (sandboxtransfer.Builder{WorkspaceRoot: workRoot, Limits: lim}).BuildWorkspace(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	cache := &sandboxartifact.Cache{Dir: filepath.Join(base, "cache"), Limits: lim, Source: sandboxartifact.FetchFunc(func(ctx context.Context, ref sandboxartifact.Ref) ([]byte, error) { calls++; return store.Get(ref) })}
	taskRoot := filepath.Join(base, "tasks")
	if err := os.Mkdir(taskRoot, 0700); err != nil {
		t.Fatal(err)
	}
	stage := Stager{Root: taskRoot, Cache: cache, Limits: lim}
	first, err := stage.Stage(context.Background(), ref, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stage.Stage(context.Background(), ref, input)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first.SkillPath != second.SkillPath || first.WorkspacePath == second.WorkspacePath {
		t.Fatal("cache not reused or task workspaces shared")
	}
	mode, err := os.Stat(first.WorkspacePath)
	if err != nil || mode.Mode().Perm() != 0755 {
		t.Fatalf("input directory not readable to sandbox UID: %v %v", mode, err)
	}
	fileMode, err := os.Stat(filepath.Join(first.WorkspacePath, "input.txt"))
	if err != nil || fileMode.Mode().Perm() != 0444 {
		t.Fatalf("input file not readonly: %v %v", fileMode, err)
	}
	data, err := os.ReadFile(filepath.Join(first.WorkspacePath, "input.txt"))
	if err != nil || string(data) != "private-data" {
		t.Fatalf("input: %q %v", data, err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.WorkspacePath); !os.IsNotExist(err) {
		t.Fatal("released task input still exists")
	}
	if _, err := os.Stat(second.WorkspacePath); err != nil {
		t.Fatal("other task input was deleted")
	}
	if _, err := os.Stat(second.SkillPath); err != nil {
		t.Fatal("shared cache was deleted")
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Stage(context.Background(), ref, []byte("bad archive")); err == nil {
		t.Fatal("invalid input accepted")
	}
	entries, err := os.ReadDir(taskRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("orphaned task staging: %v %v", entries, err)
	}
}
func TestNodeStagerRejectsUnsafeRoot(t *testing.T) {
	base := t.TempDir()
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validPrivateRoot(loose); err == nil {
		t.Fatal("world accessible task root accepted")
	}
	link := filepath.Join(base, "alias")
	if err := os.Symlink(base, link); err == nil {
		if err := validPrivateRoot(filepath.Join(link, "loose")); err == nil {
			t.Fatal("symlink task root accepted")
		}
	}
}

package sandboxoci

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagedMountsAreNarrowAndReadOnly(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	tasks := filepath.Join(base, "tasks")
	skill := filepath.Join(cache, strings.Repeat("a", 64), "skill")
	work := filepath.Join(tasks, "task-a", "inputs", "workspace")
	for _, dir := range []string{skill, work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	executor := StagedMountPlanner{SkillCacheRoot: cache, TaskRoot: tasks}
	if _, err := executor.Plan(validProfile(), skill, work); err != nil {
		t.Fatalf("validated paths not admitted: %v", err)
	}
	args := stagedRunArgs(validProfile(), "test-container", skill, work)
	joined := strings.Join(args, "\x00")
	for _, required := range []string{"--network=none", "--read-only", "65534:65534", "target=/skill,readonly", "target=/input,readonly", "/workspace:rw,nosuid,nodev,size=64m", "--cap-drop=ALL", "--pull=never"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing %q in %#v", required, args)
		}
	}
	for _, bad := range []string{"--volumes-from", "--privileged", "docker.sock", "--network=host"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unsafe arg %q", bad)
		}
	}
	if _, err := executor.Plan(validProfile(), skill, base); err == nil {
		t.Fatal("outside task workspace accepted")
	}
	if err := os.Symlink(work, filepath.Join(tasks, "task-a", "inputs", "alias")); err == nil {
		if _, err := validatedBind(tasks, filepath.Join(tasks, "task-a", "inputs", "alias"), "workspace"); err == nil {
			t.Fatal("symlink bind accepted")
		}
	}
	if _, err := validatedBind(cache, filepath.Join(cache, "nope", "skill"), "skill"); err == nil {
		t.Fatal("missing cache accepted")
	}
}

func TestBoundedScratchAndOutputCollection(t *testing.T) {
	p := validProfile()
	args := stagedRunArgs(p, "task", "/private/cache/sha/skill", "/private/task/inputs/workspace")
	flags := strings.Join(args, " ")
	if strings.Contains(flags, "target=/workspace,bind") || !strings.Contains(flags, "target=/input,readonly") ||
		!strings.Contains(flags, "/workspace:rw,nosuid,nodev,size=64m,uid=65534,gid=65534,mode=0700") {
		t.Fatalf("unsafe staged mount plan: %s", flags)
	}
	if !strings.HasPrefix(CopyInputToScratch, "set -eu") || !strings.Contains(CopyInputToScratch, "cp -R /input/. /workspace/") || strings.Contains(CopyInputToScratch, "/private") {
		t.Fatal("unsafe scratch bootstrap")
	}
	var buf bytes.Buffer
	tarWriter := tar.NewWriter(&buf)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "outputs/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.WriteHeader(&tar.Header{Name: "outputs/result.txt", Typeflag: tar.TypeReg, Size: 2, Mode: 0644}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := CollectStagedOutputs(context.Background(), bytes.NewReader(buf.Bytes()), p)
	if err != nil || len(files) != 1 || files[0].Path != "result.txt" {
		t.Fatalf("outputs: %+v %v", files, err)
	}
	p.MaxArtifactBytes = 1
	if _, err := CollectStagedOutputs(context.Background(), bytes.NewReader(buf.Bytes()), p); err == nil {
		t.Fatal("artifact budget ignored")
	}
	p = validProfile()
	p.ScratchMiB = 0
	if _, err := CollectStagedOutputs(context.Background(), bytes.NewReader(buf.Bytes()), p); err == nil {
		t.Fatal("unconfigured scratch accepted")
	}
}

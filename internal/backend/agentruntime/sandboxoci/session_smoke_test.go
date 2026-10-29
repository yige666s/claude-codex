package sandboxoci

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in WSL/Linux smoke test with disposable fake input only. Never part of
// normal CI, never runs a user command or contacts a production Node.
func TestDevelopmentSessionDockerSmoke(t *testing.T) {
	image := os.Getenv("SANDBOX_OCI_SMOKE_IMAGE")
	if image == "" {
		t.Skip("set SANDBOX_OCI_SMOKE_IMAGE to a locally cached immutable digest")
	}
	if !strings.Contains(image, "@sha256:") {
		t.Fatal("smoke image must be a digest")
	}
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	tasks := filepath.Join(base, "tasks")
	skill := filepath.Join(cache, strings.Repeat("a", 64), "skill")
	input := filepath.Join(tasks, "task-smoke", "inputs", "workspace")
	for _, dir := range []string{skill, input} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("fake skill"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "input.txt"), []byte("fake input"), 0644); err != nil {
		t.Fatal(err)
	}
	p := validProfile()
	p.ImageDigest = image
	p.TimeoutSeconds = 30
	driver := SessionDriver{Planner: StagedMountPlanner{SkillCacheRoot: cache, TaskRoot: tasks}, Runner: loggingRunner{t: t}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := driver.Start(ctx, p, skill, input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
	}()
	first, err := session.Run(ctx, []byte("cat /workspace/input.txt; echo one > /workspace/outputs/report.txt"))
	if err != nil || strings.TrimSpace(first.Stdout) != "fake input" {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := session.Run(ctx, []byte("echo two >> /workspace/outputs/report.txt"))
	if err != nil {
		t.Fatalf("second: %+v %v", second, err)
	}
	files, err := session.Collect(ctx)
	if err != nil || len(files) != 1 || string(files[0].Content) != "one\ntwo\n" {
		t.Fatalf("outputs: %+v %v", files, err)
	}
}

type loggingRunner struct{ t *testing.T }

func (r loggingRunner) Run(ctx context.Context, args []string, input []byte, stdout, stderr io.Writer) error {
	var errBuf bytes.Buffer
	err := (ExecCommandRunner{}).Run(ctx, args, input, stdout, io.MultiWriter(stderr, &errBuf))
	if err != nil {
		r.t.Logf("docker %v failed: %v; stderr=%q", args, err, errBuf.String())
	}
	return err
}

package sandboxoci

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fakeDocker struct {
	mu          sync.Mutex
	commands    [][]string
	scripts     []string
	output      []byte
	failExec    bool
	failCleanup bool
	inventory   []string
}

func (f *fakeDocker) Run(ctx context.Context, args []string, input []byte, stdout, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, append([]string(nil), args...))
	switch args[0] {
	case "create":
		_, _ = io.WriteString(stdout, "container-id")
	case "start":
		_, _ = io.WriteString(stdout, args[1])
	case "exec":
		if len(args) >= 3 && args[2] == "test" {
			return nil
		}
		if len(args) >= 3 && args[2] == "tar" {
			_, err := stdout.Write(f.output)
			return err
		}
		if f.failExec {
			return errors.New("command process exited")
		}
		f.scripts = append(f.scripts, string(input))
		_, _ = io.WriteString(stdout, "ok")
	case "rm":
		if f.failCleanup {
			return errors.New("daemon unavailable")
		}
		if len(f.inventory) > 0 {
			f.inventory = f.inventory[1:]
		}
	case "ps":
		_, _ = io.WriteString(stdout, strings.Join(f.inventory, "\n"))
	default:
		return errors.New("unexpected Docker call")
	}
	return nil
}
func setupDriver(t *testing.T) (SessionDriver, *fakeDocker, string, string) {
	t.Helper()
	base := t.TempDir()
	cache := filepath.Join(base, "cache")
	tasks := filepath.Join(base, "tasks")
	skill := filepath.Join(cache, strings.Repeat("a", 64), "skill")
	input := filepath.Join(tasks, "task-fake", "inputs", "workspace")
	for _, path := range []string{skill, input} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeDocker{}
	return SessionDriver{Planner: StagedMountPlanner{SkillCacheRoot: cache, TaskRoot: tasks}, Runner: fake}, fake, skill, input
}
func validOutputArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := writer.WriteHeader(&tar.Header{Name: "outputs/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteHeader(&tar.Header{Name: "outputs/report.txt", Typeflag: tar.TypeReg, Size: 5, Mode: 0644}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func TestSessionReusesOneContainerAndCollectsBeforeCleanup(t *testing.T) {
	driver, fake, skill, input := setupDriver(t)
	fake.output = validOutputArchive(t)
	session, err := driver.Start(context.Background(), validProfile(), skill, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"echo one", "echo two"} {
		result, err := session.Run(context.Background(), []byte(script))
		if err != nil || result.Stdout != "ok" {
			t.Fatalf("run %q: %+v %v", script, result, err)
		}
	}
	files, err := session.Collect(context.Background())
	if err != nil || len(files) != 1 || files[0].Path != "report.txt" || string(files[0].Content) != "hello" {
		t.Fatalf("collect: %+v %v", files, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), []byte("echo three")); err == nil {
		t.Fatal("closed session executed")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.scripts) != 2 || fake.scripts[0] != "echo one" || fake.scripts[1] != "echo two" {
		t.Fatalf("scripts=%v", fake.scripts)
	}
	var created, removed int
	for _, args := range fake.commands {
		if args[0] == "create" {
			created++
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "target=/skill,readonly") || !strings.Contains(joined, "target=/input,readonly") || strings.Contains(joined, "source="+input+",target=/workspace") || !strings.Contains(joined, "--network=none") {
				t.Fatalf("unsafe create: %v", args)
			}
			if args[len(args)-2] != "-c" || !strings.Contains(args[len(args)-1], CopyInputToScratch) {
				t.Fatalf("bootstrap absent: %v", args)
			}
		}
		if args[0] == "rm" {
			removed++
		}
	}
	if created != 1 || removed != 1 {
		t.Fatalf("create=%d remove=%d", created, removed)
	}
}
func TestFailedCommandAndInvalidOutputForceCleanup(t *testing.T) {
	driver, fake, skill, input := setupDriver(t)
	session, err := driver.Start(context.Background(), validProfile(), skill, input)
	if err != nil {
		t.Fatal(err)
	}
	fake.failExec = true
	if _, err := session.Run(context.Background(), []byte("false")); err == nil {
		t.Fatal("failed command accepted")
	}
	if _, err := session.Run(context.Background(), []byte("echo later")); err == nil {
		t.Fatal("uncertain session reused")
	}
	fake.failExec = false
	session, err = driver.Start(context.Background(), validProfile(), skill, input)
	if err != nil {
		t.Fatal(err)
	}
	fake.output = []byte("malformed tar")
	if _, err := session.Collect(context.Background()); err == nil {
		t.Fatal("bad archive accepted")
	}
	if _, err := session.Collect(context.Background()); err == nil {
		t.Fatal("uncertain collection retried")
	}
}
func TestCleanupFailureAndReconcileFailClosed(t *testing.T) {
	driver, fake, skill, input := setupDriver(t)
	session, err := driver.Start(context.Background(), validProfile(), skill, input)
	if err != nil {
		t.Fatal(err)
	}
	fake.failCleanup = true
	if err := session.Close(); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	fake.failCleanup = false
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	fake.inventory = []string{"agentapi-oci-" + strings.Repeat("a", 32)}
	if err := ReconcileDevelopmentContainers(context.Background(), fake); err != nil {
		t.Fatal(err)
	}
	fake.inventory = []string{"unrelated-container"}
	if err := ReconcileDevelopmentContainers(context.Background(), fake); err == nil {
		t.Fatal("unexpected label owner ignored")
	}
}

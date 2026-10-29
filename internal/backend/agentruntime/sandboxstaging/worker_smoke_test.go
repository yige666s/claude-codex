//go:build linux

package sandboxstaging

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxoci"
)

// Opt-in WSL test. It only runs these fixed commands against fake fixtures.
// It never registers an HTTP execute route or contacts a production node.
func TestDevelopmentWorkerDockerSmoke(t *testing.T) {
	image := os.Getenv("SANDBOX_OCI_SMOKE_IMAGE")
	if image == "" {
		t.Skip("requires locally cached digest and explicit opt-in")
	}
	if !strings.Contains(image, "@sha256:") {
		t.Fatal("smoke image must be immutable")
	}
	service, request, journalPath := stagingFixture(t)
	request.Profile.ImageDigest = image
	request.Profile.TimeoutSeconds = 30
	request.Deadline = time.Now().Add(25 * time.Second).UTC()
	service.Profile = request.Profile
	runner := sandboxoci.ExecCommandRunner{}
	recovered, err := ReconcileDevelopmentStartup(context.Background(), service.Stager, journalPath, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := recovered.Close(); err != nil {
			t.Error(err)
		}
	}()
	service.Journal = recovered.Journal
	handler, err := NewStagingHandler([]byte(strings.Repeat("k", 32)), service)
	if err != nil || handler == nil {
		t.Fatalf("stage handler: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	receipt, err := service.prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	defer func() {
		if err := service.dispose(control, true); err != nil {
			t.Error(err)
		}
	}()
	worker := DevelopmentWorker{Recovery: recovered, Service: service, Driver: sandboxoci.SessionDriver{
		Planner: sandboxoci.StagedMountPlanner{SkillCacheRoot: service.Stager.Cache.Dir, TaskRoot: service.Stager.Root}, Runner: runner,
	}}
	first, err := worker.Execute(ctx, control, strings.Repeat("1", 32), []byte("test -f /skill/SKILL.md && test -f /workspace/input.bin && printf one > /workspace/outputs/result.txt"))
	if err != nil {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := worker.Execute(ctx, control, strings.Repeat("2", 32), []byte("printf two >> /workspace/outputs/result.txt"))
	if err != nil {
		t.Fatalf("second: %+v %v", second, err)
	}
	outputs, err := worker.Collect(ctx, control)
	if err != nil || len(outputs) != 1 || outputs[0].Path != "result.txt" || string(outputs[0].Content) != "onetwo" {
		t.Fatalf("outputs: %+v %v", outputs, err)
	}
	if err := service.dispose(control, false); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(service.Stager.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".node.lock" {
			t.Fatalf("private input survived: %s", entry.Name())
		}
	}
}

func TestDevelopmentWorkerCancelDockerSmoke(t *testing.T) {
	image := os.Getenv("SANDBOX_OCI_SMOKE_IMAGE")
	if image == "" {
		t.Skip("requires explicit local Docker opt-in")
	}
	service, request, journalPath := stagingFixture(t)
	request.Profile.ImageDigest = image
	request.Profile.TimeoutSeconds = 35
	request.Deadline = time.Now().Add(30 * time.Second).UTC()
	service.Profile = request.Profile
	runner := sandboxoci.ExecCommandRunner{}
	recovery, err := ReconcileDevelopmentStartup(context.Background(), service.Stager, journalPath, runner)
	if err != nil {
		t.Fatal(err)
	}
	defer recovery.Close()
	service.Journal = recovery.Journal
	handler, err := NewStagingHandler([]byte(strings.Repeat("k", 32)), service)
	if err != nil || handler == nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 25*time.Second)
	defer stop()
	receipt, err := service.prepare(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	defer func() { _ = service.dispose(control, true) }()
	worker := DevelopmentWorker{Recovery: recovery, Service: service, Driver: sandboxoci.SessionDriver{
		Planner: sandboxoci.StagedMountPlanner{SkillCacheRoot: service.Stager.Cache.Dir, TaskRoot: service.Stager.Root}, Runner: runner,
	}}
	completed := make(chan error, 1)
	go func() {
		_, err := worker.Execute(ctx, control, strings.Repeat("d", 32), []byte("touch /workspace/outputs/running; sleep 20"))
		completed <- err
	}()
	name := "agentapi-oci-" + receipt.LeaseID
	ready := false
	for i := 0; i < 80; i++ {
		probe, probeCtx := context.WithTimeout(ctx, time.Second)
		var stdout, stderr bytes.Buffer
		err := runner.Run(probe, []string{"exec", name, "test", "-f", "/workspace/outputs/running"}, nil, &stdout, &stderr)
		probeCtx()
		if err == nil {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("waiting for command marker expired")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("command did not become active")
	}
	started := time.Now()
	if err := service.dispose(control, true); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("cancel failed to promptly terminate live Docker command")
	}
	select {
	case err := <-completed:
		if err == nil {
			t.Fatal("canceled Docker command succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled Docker command still running")
	}
	check, checkCancel := context.WithTimeout(context.Background(), time.Second)
	defer checkCancel()
	var stdout, stderr bytes.Buffer
	if err := runner.Run(check, []string{"inspect", name}, nil, &stdout, &stderr); err == nil {
		t.Fatal("Docker container survived cancel")
	}
}

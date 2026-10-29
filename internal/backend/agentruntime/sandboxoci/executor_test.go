package sandboxoci

import (
	"context"
	"strings"
	"sync"
	"testing"

	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
)

func validProfile() sandboxpolicy.Profile {
	return sandboxpolicy.Profile{
		Driver: "oci", Network: "deny", ImageDigest: "python@sha256:" + strings.Repeat("a", 64),
		CPUs: 0.5, MemoryMiB: 256, PIDs: 32, TimeoutSeconds: 2, MaxOutputBytes: 512, ScratchMiB: 64, MaxArtifactBytes: 1 << 20,
	}
}

func TestDockerRunArgsHaveNoHostAccess(t *testing.T) {
	p := validProfile()
	args := dockerRunArgs(p, "agentapi-oci-test")
	joined := strings.Join(args, "\x00")
	for _, want := range []string{
		"-i", "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL",
		"--security-opt=no-new-privileges", "--cgroupns=private",
		"--memory\x00256m", "--memory-swap\x00256m", "--cpus\x000.5", "--pids-limit\x0032",
		"--user\x0065534:65534", "--entrypoint\x00/bin/sh",
		"--tmpfs\x00/workspace:", "--tmpfs\x00/tmp:", p.ImageDigest,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %#v", want, args)
		}
	}
	for _, bad := range []string{"--mount", "--volumes-from", "--privileged", "-v", "docker.sock", "/run/agentapi/secrets", "--network=host", "--env-file"} {
		for _, arg := range args {
			if arg == bad || strings.Contains(arg, bad) {
				t.Fatalf("unsafe arg %q in %#v", bad, args)
			}
		}
	}
}

func TestExecuteRejectsUnsafeProfileBeforeDocker(t *testing.T) {
	executor := DockerExecutor{DockerBinary: "definitely-not-docker"}
	p := validProfile()
	p.Network = "host"
	if _, err := executor.Execute(context.Background(), p, []byte("echo hello")); err == nil || !strings.Contains(err.Error(), "reject OCI profile") {
		t.Fatalf("host network accepted: %v", err)
	}
	p = validProfile()
	p.Driver = "native"
	if _, err := executor.Execute(context.Background(), p, []byte("echo hello")); err == nil || !strings.Contains(err.Error(), "reject OCI profile") {
		t.Fatalf("native accepted: %v", err)
	}
	p = validProfile()
	p.ImageDigest = "python:latest"
	if _, err := executor.Execute(context.Background(), p, []byte("echo hello")); err == nil || !strings.Contains(err.Error(), "reject OCI profile") {
		t.Fatalf("mutable image accepted: %v", err)
	}
	p = validProfile()
	if _, err := executor.Execute(context.Background(), p, make([]byte, maxInlineScriptBytes+1)); err == nil || !strings.Contains(err.Error(), "oversized") {
		t.Fatalf("oversized script accepted: %v", err)
	}
	if _, err := executor.Execute(context.Background(), p, nil); err == nil {
		t.Fatal("empty script accepted")
	}
}

func TestOutputBudgetIsSharedAndBounded(t *testing.T) {
	budget := &outputBudget{remaining: 5}
	a, b := &captureWriter{budget: budget}, &captureWriter{budget: budget}
	var wg sync.WaitGroup
	for _, writer := range []*captureWriter{a, b} {
		wg.Add(1)
		go func(w *captureWriter) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = w.Write([]byte("hello"))
			}
		}(writer)
	}
	wg.Wait()
	if got := len(a.String()) + len(b.String()); got != 5 {
		t.Fatalf("captured %d bytes", got)
	}
	if !budget.exceeded {
		t.Fatal("output overflow not recorded")
	}
}

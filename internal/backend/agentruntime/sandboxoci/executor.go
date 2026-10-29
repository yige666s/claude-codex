package sandboxoci

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
)

const maxInlineScriptBytes = 64 << 10

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// DockerExecutor is an isolated, single-command OCI experiment. It is not
// connected to the Node's public handler or AgentAPI's Job Queue.
type DockerExecutor struct{ DockerBinary string }

func (e DockerExecutor) Execute(ctx context.Context, profile sandboxpolicy.Profile, script []byte) (Result, error) {
	if err := sandboxpolicy.ValidateOCIProfile(profile); err != nil {
		return Result{}, fmt.Errorf("reject OCI profile: %w", err)
	}
	if ctx == nil || len(script) == 0 || len(script) > maxInlineScriptBytes {
		return Result{}, errors.New("invalid or oversized sandbox script")
	}
	bin := e.DockerBinary
	if bin == "" {
		bin = "docker"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return Result{}, fmt.Errorf("OCI runtime unavailable: %w", err)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Result{}, fmt.Errorf("sandbox name generation: %w", err)
	}
	name := "agentapi-oci-" + hex.EncodeToString(nonce[:])
	deadlineCtx, cancel := context.WithTimeout(ctx, time.Duration(profile.TimeoutSeconds)*time.Second)
	defer cancel()
	args := dockerRunArgs(profile, name)
	cmd := exec.CommandContext(deadlineCtx, bin, args...)
	cmd.Stdin = bytes.NewReader(script)
	budget := &outputBudget{remaining: profile.MaxOutputBytes}
	stdout := &captureWriter{budget: budget}
	stderr := &captureWriter{budget: budget}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started := time.Now()
	runErr := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(started)}
	if exitErr := new(exec.ExitError); errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	// The CLI can die before its container. Always issue an independent cleanup
	// request, including on timeout/cancellation; never fall back to host exec.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	cleanupOut, cleanupErr := exec.CommandContext(cleanupCtx, bin, "rm", "-f", name).CombinedOutput()
	cleanupCancel()
	if cleanupErr != nil && !strings.Contains(string(cleanupOut), "No such container") {
		return result, fmt.Errorf("sandbox cleanup failed: %w", cleanupErr)
	}
	if deadlineCtx.Err() != nil {
		return result, deadlineCtx.Err()
	}
	if budget.exceeded {
		return result, errors.New("sandbox output limit exceeded")
	}
	if runErr != nil {
		return result, fmt.Errorf("sandbox command failed: %w", runErr)
	}
	return result, nil
}

func dockerRunArgs(p sandboxpolicy.Profile, name string) []string {
	// No -v, --mount, --volumes-from, --privileged, host network, or caller env.
	// Workspace and /tmp are private tmpfs; scripts arrive only on stdin.
	scratch := p.MemoryMiB / 2
	if scratch > 128 {
		scratch = 128
	}
	return []string{
		"run", "--rm", "-i", "--name", name, "--label", "agentapi.sandbox_task=true",
		"--pull=never", "--network=none", "--read-only", "--cap-drop=ALL",
		"--security-opt=no-new-privileges", "--cgroupns=private",
		"--memory", strconv.Itoa(p.MemoryMiB) + "m", "--memory-swap", strconv.Itoa(p.MemoryMiB) + "m",
		"--cpus", strconv.FormatFloat(p.CPUs, 'f', -1, 64), "--pids-limit", strconv.Itoa(p.PIDs),
		"--tmpfs", "/workspace:rw,nosuid,nodev,noexec,size=" + strconv.Itoa(scratch) + "m,uid=65534,gid=65534,mode=0700",
		"--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=32m,mode=1777",
		"--user", "65534:65534", "--workdir", "/workspace", "--env", "HOME=/workspace",
		"--entrypoint", "/bin/sh", p.ImageDigest, "-s",
	}
}

type outputBudget struct {
	mu        sync.Mutex
	remaining int
	exceeded  bool
}
type captureWriter struct {
	budget *outputBudget
	buffer bytes.Buffer
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.budget.mu.Lock()
	defer w.budget.mu.Unlock()
	n := len(data)
	kept := n
	if kept > w.budget.remaining {
		kept = w.budget.remaining
		w.budget.exceeded = true
	}
	if kept > 0 {
		_, _ = w.buffer.Write(data[:kept])
		w.budget.remaining -= kept
	}
	return n, nil // Drain excess output without allocating unbounded memory.
}
func (w *captureWriter) String() string { return w.buffer.String() }

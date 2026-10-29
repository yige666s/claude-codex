package sandboxoci

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

// CommandRunner is injected for contract tests. Production callers must use
// ExecCommandRunner and first complete the Node admission/recovery gates.
type CommandRunner interface {
	Run(context.Context, []string, []byte, io.Writer, io.Writer) error
}
type ExecCommandRunner struct{ Binary string }

func (r ExecCommandRunner) Run(ctx context.Context, args []string, input []byte, stdout, stderr io.Writer) error {
	binary := r.Binary
	if binary == "" {
		binary = "docker"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, binary, args...)
	command.Stdin = bytes.NewReader(input)
	command.Stdout, command.Stderr = stdout, stderr
	return command.Run()
}

type SessionDriver struct {
	Planner StagedMountPlanner
	Runner  CommandRunner
}

// Session has a serial command stream and never accepts a host path from a
// model. Docker process identity must be journaled by the Node before this
// driver can be connected to a real network endpoint.
type Session struct {
	runner   CommandRunner
	name     string
	profile  sandboxpolicy.Profile
	deadline time.Time
	mu       sync.Mutex
	closed   bool
}

const lifecycleTimeout = 10 * time.Second
const startupPoll = 30 * time.Millisecond

// Start is a dev-only wrapper. A Node lease must instead call StartLease so
// its persisted random lease ID determines the container name for recovery.
func (d SessionDriver) Start(ctx context.Context, profile sandboxpolicy.Profile, skillPath, workspacePath string) (*Session, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	return d.StartLease(ctx, profile, skillPath, workspacePath, hex.EncodeToString(random[:]))
}

// StartLease binds a container name to an already persisted random lease ID.
// It must only be called after the journal has durably committed the lease.
func (d SessionDriver) StartLease(ctx context.Context, profile sandboxpolicy.Profile, skillPath, workspacePath, leaseID string) (*Session, error) {
	if ctx == nil || ctx.Err() != nil || d.Runner == nil || len(leaseID) != 32 {
		return nil, errors.New("OCI lease unavailable")
	}
	decoded, err := hex.DecodeString(leaseID)
	if err != nil || hex.EncodeToString(decoded) != leaseID {
		return nil, errors.New("invalid OCI lease identity")
	}
	if err := sandboxpolicy.ValidateFileOCIProfile(profile); err != nil {
		return nil, err
	}
	name := "agentapi-oci-" + leaseID
	args := createArgs(d.Planner, profile, skillPath, workspacePath, name)
	if len(args) == 0 {
		return nil, errors.New("invalid OCI session mount plan")
	}
	session := &Session{runner: d.Runner, name: name, profile: profile, deadline: time.Now().Add(time.Duration(profile.TimeoutSeconds) * time.Second)}
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(session.deadline) {
		session.deadline = deadline
	}
	attempt := func(args ...string) error {
		limited, cancel := context.WithDeadline(ctx, session.deadline)
		defer cancel()
		var out, errout bytes.Buffer
		if err := d.Runner.Run(limited, args, nil, &out, &errout); err != nil {
			return fmt.Errorf("OCI lifecycle command failed: %w", err)
		}
		return nil
	}
	if err := attempt(args...); err != nil {
		return nil, joinCleanup(err, session.Close())
	}
	if err := attempt("start", name); err != nil {
		return nil, joinCleanup(err, session.Close())
	}
	// docker start acknowledges creation, not successful bootstrap. Probe a
	// fixed marker inside the container before accepting any user command.
	startupDeadline := time.Now().Add(5 * time.Second)
	if session.deadline.Before(startupDeadline) {
		startupDeadline = session.deadline
	}
	for {
		if err := attempt("exec", name, "test", "-f", "/workspace/.sandbox-ready"); err == nil {
			return session, nil
		}
		if ctx.Err() != nil || time.Now().After(startupDeadline) {
			return nil, joinCleanup(errors.New("OCI bootstrap not ready"), session.Close())
		}
		timer := time.NewTimer(startupPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, joinCleanup(ctx.Err(), session.Close())
		case <-timer.C:
		}
	}
}
func createArgs(p StagedMountPlanner, profile sandboxpolicy.Profile, skill, workspace, name string) []string {
	args, err := p.Plan(profile, skill, workspace)
	if err != nil {
		return nil
	}
	args[0] = "create"
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--name" {
			args[i+1] = name
			break
		}
	}
	args[len(args)-1] = "-c"
	return append(args, CopyInputToScratch+"touch /workspace/.sandbox-ready\nexec sleep 3600\n")
}

// ErrCleanupUncertain prevents a Node from finalizing a lease after Docker
// removal failed. Only exclusive startup reconciliation may clear the state.
var ErrCleanupUncertain = errors.New("OCI cleanup is uncertain")

func joinCleanup(primary, cleanup error) error {
	if cleanup != nil {
		return errors.Join(primary, fmt.Errorf("%w: %v", ErrCleanupUncertain, cleanup))
	}
	return primary
}

// Run executes a single script with a bounded output budget. Any CLI failure
// makes the session uncertain and forces cleanup: it is never retried here.
func (s *Session) Run(ctx context.Context, script []byte) (Result, error) {
	if s == nil {
		return Result{}, errors.New("OCI session missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || ctx == nil || ctx.Err() != nil || len(script) == 0 || len(script) > maxInlineScriptBytes || !time.Now().Before(s.deadline) {
		return Result{}, errors.New("OCI session command rejected")
	}
	limited, cancel := context.WithDeadline(ctx, s.deadline)
	defer cancel()
	budget := &outputBudget{remaining: s.profile.MaxOutputBytes}
	out, errout := &captureWriter{budget: budget}, &captureWriter{budget: budget}
	started := time.Now()
	runErr := s.runner.Run(limited, []string{"exec", "-i", s.name, "/bin/sh", "-s"}, script, out, errout)
	result := Result{Stdout: out.String(), Stderr: errout.String(), Duration: time.Since(started)}
	var exit *exec.ExitError
	if errors.As(runErr, &exit) {
		result.ExitCode = exit.ExitCode()
	}
	if runErr != nil || budget.exceeded || limited.Err() != nil {
		cleanup := s.closeLocked()
		return result, joinCleanup(fmt.Errorf("OCI command failed or exceeded budget: %v", runErr), cleanup)
	}
	return result, nil
}

// Collect only reads /workspace/outputs via a fixed tar command while the
// container is alive. The archive is validated before any result is returned.
func (s *Session) Collect(ctx context.Context) ([]sandboxtransfer.OutputFile, error) {
	if s == nil {
		return nil, errors.New("OCI session missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || ctx == nil || ctx.Err() != nil || !time.Now().Before(s.deadline) {
		return nil, errors.New("OCI collection rejected")
	}
	limited, cancel := context.WithDeadline(ctx, s.deadline)
	defer cancel()
	var archive, errout bytes.Buffer
	max := int64(s.profile.MaxArtifactBytes) + int64(sandboxtransfer.HardMaxFiles)*4096 + 2048
	writer := &boundedWriter{out: &archive, remaining: max}
	runErr := s.runner.Run(limited, []string{"exec", s.name, "tar", "-C", "/workspace", "-cf", "-", "outputs"}, nil, writer, &limitedWriter{out: &errout, remaining: 4096})
	if runErr != nil || writer.exceeded || limited.Err() != nil {
		return nil, joinCleanup(errors.New("OCI output export failed"), s.closeLocked())
	}
	files, err := CollectStagedOutputs(limited, bytes.NewReader(archive.Bytes()), s.profile)
	if err != nil {
		return nil, joinCleanup(err, s.closeLocked())
	}
	return files, nil
}
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}
func (s *Session) closeLocked() error {
	if s.closed {
		return nil
	}
	// Only mark closed once the daemon confirms removal. Failed removal must
	// be surfaced; a future recovery sweep must reconcile it before admission.
	cleanup, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
	defer cancel()
	var out, errout bytes.Buffer
	err := s.runner.Run(cleanup, []string{"rm", "-f", s.name}, nil, &out, &errout)
	if err != nil {
		return errors.New("OCI container removal failed")
	}
	s.closed = true
	return nil
}

// ReconcileDevelopmentContainers must run before any new Node admission.
// It only removes containers bearing the sandbox label and a strict name.
// Cross-process mutual exclusion, persistent fencing and tmpfs output recovery
// remain separate requirements before exposing this driver over HTTP.
func ReconcileDevelopmentContainers(ctx context.Context, runner CommandRunner) error {
	if ctx == nil || runner == nil {
		return errors.New("OCI reconciliation unavailable")
	}
	limited, cancel := context.WithTimeout(ctx, lifecycleTimeout)
	defer cancel()
	var out, errout bytes.Buffer
	inventory := &boundedWriter{out: &out, remaining: 64 << 10}
	errorsWriter := &boundedWriter{out: &errout, remaining: 4096}
	args := []string{"ps", "-aq", "--filter", "label=agentapi.sandbox_task=true", "--format", "{{.Names}}"}
	if err := runner.Run(limited, args, nil, inventory, errorsWriter); err != nil || inventory.exceeded || errorsWriter.exceeded {
		return errors.New("OCI container inventory failed or exceeded bound")
	}
	for _, name := range strings.Fields(out.String()) {
		if !validSessionName(name) {
			return errors.New("OCI container inventory contains unexpected name")
		}
		var result, errorsOut bytes.Buffer
		if err := runner.Run(limited, []string{"rm", "-f", name}, nil, &result, &errorsOut); err != nil {
			return errors.New("OCI orphan removal failed")
		}
	}
	out.Reset()
	errout.Reset()
	inventory = &boundedWriter{out: &out, remaining: 64 << 10}
	errorsWriter = &boundedWriter{out: &errout, remaining: 4096}
	if err := runner.Run(limited, args, nil, inventory, errorsWriter); err != nil || inventory.exceeded || errorsWriter.exceeded || strings.TrimSpace(out.String()) != "" {
		return errors.New("OCI reconciliation incomplete")
	}
	return nil
}
func validSessionName(name string) bool {
	if !strings.HasPrefix(name, "agentapi-oci-") || len(name) != len("agentapi-oci-")+32 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(name, "agentapi-oci-"))
	return err == nil
}

type boundedWriter struct {
	out       *bytes.Buffer
	remaining int64
	exceeded  bool
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		w.exceeded = true
		return 0, errors.New("OCI stream exceeds bound")
	}
	w.remaining -= int64(len(p))
	return w.out.Write(p)
}

type limitedWriter struct {
	out       *bytes.Buffer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("OCI stderr exceeds bound")
	}
	w.remaining -= int64(len(p))
	return w.out.Write(p)
}

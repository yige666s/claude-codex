//go:build linux

package sandboxstaging

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
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxoci"
)

type workerDocker struct {
	mu             sync.Mutex
	calls          [][]string
	scripts        []string
	names          []string
	archive        []byte
	failCommand    bool
	failStart      bool
	failRemove     bool
	blockCommand   chan struct{}
	commandStarted chan struct{}
}

func (d *workerDocker) Run(ctx context.Context, args []string, input []byte, out, errout io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, append([]string(nil), args...))
	switch args[0] {
	case "ps":
		_, err := io.WriteString(out, strings.Join(d.names, "\n"))
		return err
	case "create":
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--name" {
				d.names = append(d.names, args[i+1])
				break
			}
		}
		return nil
	case "start":
		if d.failStart {
			return errors.New("daemon refused start")
		}
		return nil
	case "exec":
		if len(args) > 2 && args[2] == "test" {
			return nil
		}
		if len(args) > 2 && args[2] == "tar" {
			_, err := out.Write(d.archive)
			return err
		}
		if d.blockCommand != nil {
			started := d.commandStarted
			d.mu.Unlock()
			if started != nil {
				close(started)
			}
			select {
			case <-ctx.Done():
				d.mu.Lock()
				return ctx.Err()
			case <-d.blockCommand:
				d.mu.Lock()
			}
		}
		if d.failCommand {
			return errors.New("command failed")
		}
		d.scripts = append(d.scripts, string(input))
		_, err := io.WriteString(out, "ok")
		return err
	case "rm":
		if d.failRemove {
			return errors.New("daemon failure")
		}
		for i, name := range d.names {
			if name == args[2] {
				d.names = append(d.names[:i], d.names[i+1:]...)
				return nil
			}
		}
		return errors.New("container missing")
	default:
		return errors.New("unexpected Docker command")
	}
}
func fakeWorkerOutput(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{Name: "outputs/", Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteHeader(&tar.Header{Name: "outputs/result.txt", Typeflag: tar.TypeReg, Size: 6, Mode: 0644}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("result")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func setupRecoveredWorker(t *testing.T) (*DevelopmentWorker, *StagingService, *workerDocker, StageRequest) {
	t.Helper()
	service, request, journalPath := stagingFixture(t)
	docker := &workerDocker{archive: fakeWorkerOutput(t)}
	recovery, err := ReconcileDevelopmentStartup(context.Background(), service.Stager, journalPath, docker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := recovery.Close(); err != nil {
			t.Error(err)
		}
	})
	service.Journal = recovery.Journal
	handler, err := NewStagingHandler([]byte(strings.Repeat("z", 32)), service)
	if err != nil || handler == nil {
		t.Fatalf("handler: %v", err)
	}
	worker := &DevelopmentWorker{Recovery: recovery, Service: service, Driver: sandboxoci.SessionDriver{
		Planner: sandboxoci.StagedMountPlanner{SkillCacheRoot: service.Stager.Cache.Dir, TaskRoot: service.Stager.Root}, Runner: docker,
	}}
	return worker, service, docker, request
}
func TestRecoveredWorkerReusesContainerExecIDsAndCollects(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	firstID := strings.Repeat("1", 32)
	first, err := worker.Execute(context.Background(), control, firstID, []byte("echo one"))
	if err != nil || first.Stdout != "ok" {
		t.Fatalf("first command: %+v %v", first, err)
	}
	retry, err := worker.Execute(context.Background(), control, firstID, []byte("echo one"))
	if err != nil || retry != first {
		t.Fatalf("idempotent retry: %+v %v", retry, err)
	}
	if _, err := worker.Execute(context.Background(), control, firstID, []byte("echo changed")); err == nil {
		t.Fatal("execution ID reused with changed script")
	}
	if _, err := worker.Execute(context.Background(), StageControl{LeaseID: receipt.LeaseID, UserID: "mallory"}, strings.Repeat("2", 32), []byte("echo attack")); err == nil {
		t.Fatal("cross-user execution accepted")
	}
	second, err := worker.Execute(context.Background(), control, strings.Repeat("2", 32), []byte("echo two"))
	if err != nil || second.Stdout != "ok" {
		t.Fatalf("second command: %+v %v", second, err)
	}
	files, err := worker.Collect(context.Background(), control)
	if err != nil || len(files) != 1 || string(files[0].Content) != "result" {
		t.Fatalf("collected: %+v %v", files, err)
	}
	files[0].Content[0] = 'X'
	again, err := worker.Collect(context.Background(), control)
	if err != nil || len(again) != 1 || string(again[0].Content) != "result" {
		t.Fatalf("result changed after collection: %+v %v", again, err)
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("3", 32), []byte("echo after collect")); err == nil {
		t.Fatal("command accepted after collection")
	}
	if err := service.dispose(control, false); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("3", 32), []byte("echo later")); err == nil {
		t.Fatal("disposed lease executed")
	}
	docker.mu.Lock()
	defer docker.mu.Unlock()
	if len(docker.scripts) != 2 || docker.scripts[0] != "echo one" || docker.scripts[1] != "echo two" {
		t.Fatalf("scripts=%v", docker.scripts)
	}
	var created, removed int
	for _, args := range docker.calls {
		if args[0] == "create" {
			created++
			if !strings.Contains(strings.Join(args, " "), "agentapi-oci-"+receipt.LeaseID) {
				t.Fatalf("container not bound to lease: %v", args)
			}
		}
		if args[0] == "rm" {
			removed++
		}
	}
	if created != 1 || removed != 1 || len(docker.names) != 0 {
		t.Fatalf("create=%d remove=%d containers=%v", created, removed, docker.names)
	}
}
func TestWorkerCommandFailureFencesLeaseAndCleanupFailureBlocksDispose(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	docker.failCommand = true
	id := strings.Repeat("a", 32)
	if _, err := worker.Execute(context.Background(), control, id, []byte("false")); err == nil {
		t.Fatal("failed command accepted")
	}
	docker.failCommand = false
	if _, err := worker.Execute(context.Background(), control, id, []byte("false")); err == nil {
		t.Fatal("failed ID replayed")
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("b", 32), []byte("echo later")); err == nil {
		t.Fatal("failed lease reused")
	}
	if err := service.dispose(control, true); err != nil {
		t.Fatal(err)
	}
	if len(docker.names) != 0 {
		t.Fatalf("container left behind: %v", docker.names)
	}
	// A fresh task checks that failed daemon cleanup is surfaced, never
	// marked disposed while the container may still be running.
	request.TaskID = "second-task"
	request.Deadline = time.Now().Add(5 * time.Minute).UTC()
	second, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	other := StageControl{LeaseID: second.LeaseID, UserID: request.UserID}
	if _, err := worker.Execute(context.Background(), other, strings.Repeat("c", 32), []byte("echo ready")); err != nil {
		t.Fatal(err)
	}
	docker.failRemove = true
	if err := service.dispose(other, true); err == nil {
		t.Fatal("cleanup failure hidden")
	}
	snap, err := service.Journal.Snapshot(other.LeaseID, other.UserID)
	if err != nil || snap.State != sandboxnode.LeasePrepared {
		t.Fatalf("lease terminal despite orphan: %+v %v", snap, err)
	}
	docker.failRemove = false
	if err := service.dispose(other, true); err != nil {
		t.Fatal(err)
	}
}
func TestWorkerRequiresRecoveredExclusiveLock(t *testing.T) {
	worker, service, _, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	if err := worker.Recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("d", 32), []byte("echo denied")); err == nil {
		t.Fatal("unlocked worker executed")
	}
	if err := service.dispose(control, true); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(service.Stager.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".node.lock" {
			t.Fatalf("scratch survived: %s", entry.Name())
		}
	}
}

func TestCancelInterruptsRunningCommandAndCleansContainer(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	docker.blockCommand = make(chan struct{})
	docker.commandStarted = make(chan struct{})
	commandErr := make(chan error, 1)
	go func() {
		_, err := worker.Execute(context.Background(), control, strings.Repeat("e", 32), []byte("sleep 3600"))
		commandErr <- err
	}()
	select {
	case <-docker.commandStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("command never started")
	}
	canceled := make(chan error, 1)
	go func() { canceled <- service.dispose(control, true) }()
	select {
	case err := <-canceled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel blocked behind command")
	}
	select {
	case err := <-commandErr:
		if err == nil {
			t.Fatal("canceled command reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command survived cancellation")
	}
	lease, err := service.Journal.Snapshot(control.LeaseID, control.UserID)
	if err != nil || lease.State != sandboxnode.LeaseCanceled {
		t.Fatalf("not canceled: %+v %v", lease, err)
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("f", 32), []byte("echo later")); err == nil {
		t.Fatal("canceled lease reused")
	}
	docker.mu.Lock()
	defer docker.mu.Unlock()
	if len(docker.names) != 0 {
		t.Fatalf("orphaned containers: %v", docker.names)
	}
}
func TestUnauthorizedCancelCannotInterruptActiveCommand(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	docker.blockCommand = make(chan struct{})
	docker.commandStarted = make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := worker.Execute(context.Background(), control, strings.Repeat("a", 32), []byte("echo ok"))
		result <- err
	}()
	select {
	case <-docker.commandStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("command never started")
	}
	if err := service.dispose(StageControl{LeaseID: receipt.LeaseID, UserID: "mallory"}, true); err == nil {
		t.Fatal("other user canceled task")
	}
	close(docker.blockCommand)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("other user interrupted command: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command did not resume")
	}
	if err := service.dispose(control, false); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryLockHeldThroughActiveCommand(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	docker.blockCommand = make(chan struct{})
	docker.commandStarted = make(chan struct{})
	completed := make(chan error, 1)
	go func() {
		_, err := worker.Execute(context.Background(), control, strings.Repeat("b", 32), []byte("echo fake"))
		completed <- err
	}()
	select {
	case <-docker.commandStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("command did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- worker.Recovery.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("lock released during active command: %v", err)
	case <-time.After(80 * time.Millisecond):
	}
	close(docker.blockCommand)
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("command did not finish")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recovery close did not finish")
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("c", 32), []byte("echo denied")); err == nil {
		t.Fatal("worker ran after lock release")
	}
	if err := service.dispose(control, true); err != nil {
		t.Fatal(err)
	}
}

func TestStartupAndRemovalFailureLeavesLeaseForRecovery(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	docker.failStart = true
	docker.failRemove = true
	_, err = worker.Execute(context.Background(), control, strings.Repeat("a", 32), []byte("echo fake"))
	if !errors.Is(err, sandboxoci.ErrCleanupUncertain) {
		t.Fatalf("uncertain cleanup was not surfaced: %v", err)
	}
	if err := service.dispose(control, true); !errors.Is(err, sandboxoci.ErrCleanupUncertain) {
		t.Fatalf("uncertain lease disposed: %v", err)
	}
	snap, err := service.Journal.Snapshot(control.LeaseID, control.UserID)
	if err != nil || snap.State != sandboxnode.LeasePrepared {
		t.Fatalf("lease prematurely closed: %+v %v", snap, err)
	}
	if len(docker.names) != 1 {
		t.Fatalf("orphan not represented: %v", docker.names)
	}
	// Simulate operator repairing Docker before a new process reconciles.
	docker.failStart = false
	docker.failRemove = false
	if err := worker.Recovery.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := ReconcileDevelopmentStartup(context.Background(), service.Stager, filepath.Join(filepath.Dir(service.Stager.Root), "journal.json"), docker)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if len(docker.names) != 0 {
		t.Fatalf("orphan survived reconciliation: %v", docker.names)
	}
	snap, err = recovered.Journal.Snapshot(control.LeaseID, control.UserID)
	if err != nil || snap.State != sandboxnode.LeaseCanceled {
		t.Fatalf("old lease not fenced: %+v %v", snap, err)
	}
}

func TestReaperInterruptsActiveCommandBeforeTaskLock(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	docker.blockCommand = make(chan struct{})
	docker.commandStarted = make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := worker.Execute(context.Background(), control, strings.Repeat("e", 32), []byte("sleep 3600"))
		result <- err
	}()
	select {
	case <-docker.commandStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("command not started")
	}
	reaped := make(chan error, 1)
	go func() { _, err := service.ReapExpired(request.Deadline.Add(time.Second)); reaped <- err }()
	select {
	case err := <-reaped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reaper did not interrupt command")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expired command succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expired command still running")
	}
	snap, err := service.Journal.Snapshot(control.LeaseID, control.UserID)
	if err != nil || snap.State != sandboxnode.LeaseCanceled {
		t.Fatalf("expired lease remained active: %+v %v", snap, err)
	}
}

func TestCollectWithoutCommandsReturnsEmptySealedResult(t *testing.T) {
	worker, service, _, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	files, err := worker.Collect(context.Background(), control)
	if err != nil || len(files) != 0 {
		t.Fatalf("empty collection: %+v %v", files, err)
	}
	if _, err := worker.Execute(context.Background(), control, strings.Repeat("a", 32), []byte("echo after collection")); err == nil {
		t.Fatal("command after sealing succeeded")
	}
	if err := service.dispose(control, false); err != nil {
		t.Fatal(err)
	}
}

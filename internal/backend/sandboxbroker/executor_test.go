package sandboxbroker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxpolicy"
	"claude-codex/internal/backend/sandboxtransfer"
)

type staticPolicy struct{ revision sandboxpolicy.Revision }

func (s staticPolicy) Active(context.Context) (sandboxpolicy.Revision, error) { return s.revision, nil }

type staticCatalog struct{ ref sandboxartifact.Ref }

func (c staticCatalog) Resolve(_ context.Context, _ agentruntime.RemoteSkillRequest) (sandboxartifact.Ref, error) {
	return c.ref, nil
}

type fakeNode struct {
	prepares                []ExecutionEnvelope
	calls                   []ExecutionEnvelope
	disposals               []ExecutionEnvelope
	endpoint, token, output string
	err                     error
	disposeErr              error
}

func (n *fakeNode) CollectIsolated(context.Context, string, string, ExecutionEnvelope) ([]sandboxtransfer.OutputFile, error) {
	return nil, nil
}
func (n *fakeNode) PrepareIsolated(_ context.Context, endpoint, token string, request ExecutionEnvelope) (string, error) {
	n.endpoint, n.token = endpoint, token
	n.prepares = append(n.prepares, request)
	return "lease-example", nil
}
func (n *fakeNode) DisposeIsolated(_ context.Context, endpoint, token string, request ExecutionEnvelope) error {
	n.disposals = append(n.disposals, request)
	return n.disposeErr
}
func (n *fakeNode) ExecuteIsolated(_ context.Context, endpoint, token string, request ExecutionEnvelope) (string, error) {
	n.endpoint, n.token = endpoint, token
	n.calls = append(n.calls, request)
	return n.output, n.err
}
func profileDoc() sandboxpolicy.Document {
	return sandboxpolicy.Document{
		Enabled: true, DefaultProfile: "isolated", Profiles: map[string]sandboxpolicy.Profile{"isolated": {
			Driver: "oci", NodePool: "dev", ImageDigest: "busybox@sha256:" + strings.Repeat("a", 64), Network: "deny", CPUs: 1, MemoryMiB: 256, PIDs: 32, TimeoutSeconds: 60, MaxOutputBytes: 1024, ScratchMiB: 64, MaxArtifactBytes: 1 << 20,
		}}, NodePools: map[string]sandboxpolicy.NodePool{"dev": {
			Endpoint: "https://node.example.test", CredentialRef: "node_dev", Environment: "development", MaxConcurrency: 1,
		}},
	}
}
func testBroker(t *testing.T) (*Executor, agentruntime.RemoteSkillRequest, *fakeNode) {
	t.Helper()
	base := t.TempDir()
	workBase := filepath.Join(base, "workspaces")
	work := filepath.Join(workBase, "alice")
	skillsBase := filepath.Join(base, "skills")
	skill := filepath.Join(skillsBase, "docx")
	for _, dir := range []string{work, skill} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "input.txt"), []byte("fake-only"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: docx\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "run.py"), []byte("print('fake')"), 0600); err != nil {
		t.Fatal(err)
	}
	store := sandboxartifact.Store{Dir: filepath.Join(base, "artifacts"), Builder: sandboxtransfer.Builder{SkillRoots: []string{skillsBase}, Limits: sandboxtransfer.DevLimits()}}
	ref, err := store.Publish(context.Background(), "docx", skill)
	if err != nil {
		t.Fatal(err)
	}
	node := &fakeNode{output: "remote-ok"}
	broker := &Executor{
		Policy: staticPolicy{sandboxpolicy.Revision{ID: 5, Document: profileDoc()}},
		ResolveCredential: func(_ context.Context, ref string) (string, error) {
			if ref != "node_dev" {
				t.Fatalf("ref=%q", ref)
			}
			return strings.Repeat("x", 32), nil
		},
		Snapshot: sandboxtransfer.Builder{WorkspaceRoot: workBase, SkillRoots: []string{skillsBase}, Limits: sandboxtransfer.DevLimits()},
		Node:     node, Artifacts: store, SkillCatalog: staticCatalog{ref}, AllowedUserIDs: map[string]bool{"alice": true},
	}
	req := agentruntime.RemoteSkillRequest{UserID: "alice", SessionID: "session", SkillName: "docx", WorkingDir: work, SkillRoot: skill,
		Command: "python " + filepath.Join(skill, "run.py") + " " + filepath.Join(work, "input.txt")}
	return broker, req, node
}
func TestBrokerBuildsOnlyIsolatedSnapshotForCanary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("remote broker execution requires Linux AgentAPI")
	}
	broker, req, node := testBroker(t)
	ctx, published, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil || !strings.Contains(string(published), "name: docx") {
		t.Fatalf("prepare: %s, %v", published, err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	output, err := broker.ExecuteSkillCommand(ctx, req)
	if err != nil || output != "remote-ok" || len(node.calls) != 1 {
		t.Fatalf("output=%q calls=%d err=%v", output, len(node.calls), err)
	}
	got := node.calls[0]
	if got.PolicyRevision != "5" || got.UserID != "alice" || got.Profile.Driver != "oci" || got.Profile.Network != "deny" || got.Command != "python /skill/run.py /workspace/input.txt" || len(node.prepares) != 1 || len(node.prepares[0].Snapshot) == 0 || len(got.Snapshot) != 0 || got.LeaseID != "lease-example" || got.SkillRef.ID != "docx" || node.endpoint != "https://node.example.test" || node.token != strings.Repeat("x", 32) {
		t.Fatalf("bad envelope: revision=%q user=%q profile=%+v command=%q archive_bytes=%d endpoint=%q", got.PolicyRevision, got.UserID, got.Profile, got.Command, len(got.Snapshot), node.endpoint)
	}
	if got.TaskID == "" || got.ExecutionID == "" || got.Deadline.Before(time.Now()) {
		t.Fatal("missing task id/deadline")
	}
	dest := t.TempDir()
	if err := sandboxtransfer.ExtractWorkspace(node.prepares[0].Snapshot, dest, sandboxtransfer.DevLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skill")); !os.IsNotExist(err) {
		t.Fatalf("skill shipped with task snapshot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "workspace", "input.txt"))
	if err != nil || string(data) != "fake-only" {
		t.Fatalf("snapshot=%q, %v", data, err)
	}
}
func TestBrokerFailClosedOnMissingBackendPolicyAndUser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("remote broker execution requires Linux AgentAPI")
	}
	broker, req, node := testBroker(t)
	broker.Node = nil
	if _, err := broker.ExecuteSkillCommand(context.Background(), req); err == nil {
		t.Fatal("missing node accepted")
	}
	broker.Node = node
	delete(broker.AllowedUserIDs, "alice")
	if _, err := broker.ExecuteSkillCommand(context.Background(), req); err == nil {
		t.Fatal("unauthorized user accepted")
	}
	broker.AllowedUserIDs["alice"] = true
	broker.Policy = staticPolicy{sandboxpolicy.Revision{ID: 5, Document: sandboxpolicy.DefaultDocument()}}
	if _, err := broker.ExecuteSkillCommand(context.Background(), req); err == nil {
		t.Fatal("disabled DB policy accepted")
	}
	broker.Policy = staticPolicy{sandboxpolicy.Revision{ID: 5, Document: profileDoc()}}
	invalid := req
	invalid.WorkingDir = t.TempDir()
	if _, _, _, err := broker.PrepareSkill(context.Background(), invalid); err == nil || !strings.Contains(err.Error(), "snapshot rejected") {
		t.Fatalf("workspace escape accepted: %v", err)
	}
	if len(node.calls) != 0 {
		t.Fatal("failed request reached node")
	}
	doc := profileDoc()
	pool := doc.NodePools["dev"]
	pool.Environment = "production"
	doc.NodePools["dev"] = pool
	broker.Policy = staticPolicy{sandboxpolicy.Revision{ID: 5, Document: doc}}
	if _, err := broker.ExecuteSkillCommand(context.Background(), req); err == nil {
		t.Fatal("production pool accepted in dev bridge")
	}
}
func TestBrokerNeverFallsBackWhenNodeFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("remote broker execution requires Linux AgentAPI")
	}
	broker, req, node := testBroker(t)
	node.err = errors.New("node unavailable")
	ctx, _, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := broker.ExecuteSkillCommand(ctx, req); err == nil || !strings.Contains(err.Error(), "remote sandbox execution failed") {
		t.Fatalf("node error=%v", err)
	}
	if len(node.calls) != 1 {
		t.Fatal("unexpected retry or fallback")
	}
}

func TestBrokerRefusesWindowsControlPlane(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux path tested in integration")
	}
	broker, req, node := testBroker(t)
	if _, err := broker.ExecuteSkillCommand(context.Background(), req); err == nil || !strings.Contains(err.Error(), "requires Linux") {
		t.Fatalf("Windows route allowed: %v", err)
	}
	if len(node.calls) != 0 {
		t.Fatal("Windows request reached Node")
	}
}

func TestTaskPinReusesSkillAndWorkspaceDespiteNewPublication(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Broker only")
	}
	broker, req, node := testBroker(t)
	ctx, _, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	initial := broker.SkillCatalog.(staticCatalog).ref
	if _, err := broker.ExecuteSkillCommand(ctx, req); err != nil {
		t.Fatal(err)
	}
	// Updating workspace after preparation cannot change this invocation's
	// sealed input. Publishing another digest does not change the task pin.
	if err := os.WriteFile(filepath.Join(req.WorkingDir, "input.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(req.SkillRoot, "run.py"), []byte("print('v2')"), 0600); err != nil {
		t.Fatal(err)
	}
	updated, err := broker.Artifacts.Publish(context.Background(), req.SkillName, req.SkillRoot)
	if err != nil || updated == initial {
		t.Fatalf("update: %+v, %v", updated, err)
	}
	broker.SkillCatalog = staticCatalog{ref: updated}
	if _, err := broker.ExecuteSkillCommand(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(node.calls) != 2 || node.calls[0].TaskID != node.calls[1].TaskID || node.calls[0].ExecutionID == node.calls[1].ExecutionID || node.calls[0].SkillRef != initial || node.calls[1].SkillRef != initial || len(node.prepares) != 1 || len(node.prepares[0].Snapshot) == 0 || len(node.calls[0].Snapshot) != 0 || len(node.calls[1].Snapshot) != 0 {
		t.Fatalf("task pin changed: %+v", node.calls)
	}
	other := req
	other.SessionID = "other"
	if _, err := broker.ExecuteSkillCommand(ctx, other); err == nil {
		t.Fatal("cross-session prepared context accepted")
	}
	if _, err := broker.ExecuteSkillCommand(context.Background(), req); err == nil {
		t.Fatal("unprepared command accepted")
	}
	if len(node.calls) != 2 {
		t.Fatal("denied calls reached node")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.ExecuteSkillCommand(ctx, req); err == nil {
		t.Fatal("released task could still execute")
	}
	if len(node.calls) != 2 {
		t.Fatal("released task reached node")
	}
}

func TestPreparationRejectsUnpublishedOrChangedEntrypoint(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Broker only")
	}
	broker, req, node := testBroker(t)
	if err := os.WriteFile(filepath.Join(req.SkillRoot, "SKILL.md"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := broker.PrepareSkill(context.Background(), req); err == nil {
		t.Fatal("unpublished entrypoint accepted")
	}
	if len(node.calls) != 0 {
		t.Fatal("unpublished request reached node")
	}
}

func TestPreparedTaskRefusesPolicyRevisionChange(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Broker only")
	}
	broker, req, node := testBroker(t)
	ctx, _, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	broker.Policy = staticPolicy{sandboxpolicy.Revision{ID: 6, Document: profileDoc()}}
	if _, err := broker.ExecuteSkillCommand(ctx, req); err == nil {
		t.Fatal("old task executed with new policy")
	}
	if len(node.calls) != 0 {
		t.Fatal("stale task reached node")
	}
}

func TestPreparedTaskRefusesPolicyMutationWithinRevision(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Broker only")
	}
	broker, req, node := testBroker(t)
	ctx, _, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Error(err)
		}
	}()
	changed := profileDoc()
	profile := changed.Profiles["isolated"]
	profile.MemoryMiB = 512
	changed.Profiles["isolated"] = profile
	broker.Policy = staticPolicy{sandboxpolicy.Revision{ID: 5, Document: changed}}
	if _, err := broker.ExecuteSkillCommand(ctx, req); err == nil {
		t.Fatal("policy mutation accepted")
	}
	if len(node.calls) != 0 {
		t.Fatal("mutated policy reached node")
	}
}

func TestReleaseReportsNodeCleanupFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Broker only")
	}
	broker, req, node := testBroker(t)
	_, _, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	node.disposeErr = errors.New("node offline")
	if err := release(); err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("cleanup failure hidden: %v", err)
	}
	if len(node.disposals) != 1 {
		t.Fatalf("cleanup calls=%d", len(node.disposals))
	}
}

package sandboxstaging

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/agentruntime/sandboxartifact"
	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

func stagingFixture(t *testing.T) (*StagingService, StageRequest, string) {
	t.Helper()
	root := t.TempDir()
	skills := filepath.Join(root, "skills")
	skill := filepath.Join(skills, "demo")
	workRoot := filepath.Join(root, "users")
	work := filepath.Join(workRoot, "alice")
	for _, dir := range []string{skill, work} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("demo"), 0600); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 48<<10)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "input.bin"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	lim := sandboxtransfer.DevLimits()
	store := sandboxartifact.Store{Dir: filepath.Join(root, "published"), Builder: sandboxtransfer.Builder{SkillRoots: []string{skills}, Limits: lim}}
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (sandboxtransfer.Builder{WorkspaceRoot: workRoot, Limits: lim}).BuildWorkspace(context.Background(), work)
	if err != nil || len(snapshot) < 16<<10 {
		t.Fatalf("invalid large input: %d %v", len(snapshot), err)
	}
	cache := &sandboxartifact.Cache{Dir: filepath.Join(root, "cache"), Limits: lim, Source: sandboxartifact.FetchFunc(func(ctx context.Context, ref sandboxartifact.Ref) ([]byte, error) { return store.Get(ref) })}
	taskRoot := filepath.Join(root, "tasks")
	if err := os.Mkdir(taskRoot, 0700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "journal.json")
	journal, err := sandboxnode.OpenFileJournal(journalPath, 32, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile := sandboxpolicy.Profile{Driver: "oci", NodePool: "development", Network: "deny", ImageDigest: "busybox@sha256:" + strings.Repeat("a", 64), CPUs: 1, MemoryMiB: 128, PIDs: 32, TimeoutSeconds: 60, MaxOutputBytes: 1024, ScratchMiB: 32, MaxArtifactBytes: 1 << 20}
	service := &StagingService{Stager: sandboxnode.Stager{Root: taskRoot, Cache: cache, Limits: lim}, Journal: journal, Revision: "9", Profile: profile, MaxActive: 1}
	request := StageRequest{TaskID: "task-one", UserID: "alice", SessionID: "session-one", PolicyRevision: "9", Profile: profile, SkillRef: ref, Deadline: time.Now().Add(10 * time.Minute).UTC().Truncate(time.Second), Snapshot: snapshot}
	return service, request, journalPath
}
func TestSignedLargeStageOnlyOnceAndCleanup(t *testing.T) {
	service, request, _ := stagingFixture(t)
	key := []byte(strings.Repeat("k", 32))
	handler, err := NewStagingHandler(key, service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Prepare(context.Background(), request)
	if err != nil || first != second {
		t.Fatalf("duplicate stage: %+v %+v %v", first, second, err)
	}
	entries, err := os.ReadDir(service.Stager.Root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("duplicate task dirs: %d %v", len(entries), err)
	}
	changed := request
	changed.Snapshot = append([]byte(nil), request.Snapshot...)
	changed.Snapshot[len(changed.Snapshot)/2] ^= 1
	if _, err := client.Prepare(context.Background(), changed); err == nil {
		t.Fatal("changed task input accepted")
	}
	if err := client.Dispose(context.Background(), first.LeaseID, "mallory", false); err == nil {
		t.Fatal("cross-user dispose accepted")
	}
	if err := client.Dispose(context.Background(), first.LeaseID, "alice", true); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(service.Stager.Root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("orphan after cancel: %d %v", len(entries), err)
	}
	if _, err := client.Prepare(context.Background(), request); err == nil {
		t.Fatal("canceled task re-prepared")
	}
	response, err := http.Post(server.URL+"/v1/tasks/execute", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("command route accidentally active: %d", response.StatusCode)
	}
}
func TestStageAuthenticationPolicyBoundsAndRestart(t *testing.T) {
	service, request, journalPath := stagingFixture(t)
	key := []byte(strings.Repeat("k", 32))
	handler, err := NewStagingHandler(key, service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := NewClient(server.URL, []byte(strings.Repeat("z", 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Prepare(context.Background(), request); err == nil {
		t.Fatal("wrong signature accepted")
	}
	altered := request
	altered.Profile.MemoryMiB = 256
	if _, err := client.Prepare(context.Background(), altered); err == nil {
		t.Fatal("client-chosen profile accepted")
	}
	altered = request
	altered.PolicyRevision = "10"
	if _, err := client.Prepare(context.Background(), altered); err == nil {
		t.Fatal("stale revision accepted")
	}
	receipt, err := client.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := sandboxnode.OpenFileJournal(journalPath, 32, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &StagingService{Stager: service.Stager, Journal: journal, Revision: "9", Profile: service.Profile, MaxActive: 1}
	restartedHandler, err := NewStagingHandler(key, restarted)
	if err != nil {
		t.Fatal(err)
	}
	secondServer := httptest.NewServer(restartedHandler)
	defer secondServer.Close()
	next, err := NewClient(secondServer.URL, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Prepare(context.Background(), request); err == nil {
		t.Fatal("process restart resumed staged lease without reconciliation")
	}
	if err := client.Dispose(context.Background(), receipt.LeaseID, "alice", false); err != nil {
		t.Fatal(err)
	}
}
func TestStageReplayAndOversizeAreRejected(t *testing.T) {
	service, request, _ := stagingFixture(t)
	key := []byte(strings.Repeat("k", 32))
	handler, err := NewStagingHandler(key, service)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	makeRequest := func(data []byte) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/tasks/prepare", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	req := makeRequest(data)
	signer, err := sandboxnode.NewTaskSigner(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Sign(req, data); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("first request: %d %s", res.Code, res.Body.String())
	}
	replay := makeRequest(data)
	replay.Header = req.Header.Clone()
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, replay)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("replay accepted: %d", res.Code)
	}
	oversized := makeRequest(make([]byte, MaxStagingBody+1))
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, oversized)
	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize accepted: %d", res.Code)
	}
	recorded := service.records[request.TaskID]
	if err := service.dispose(StageControl{LeaseID: recorded.receipt.LeaseID, UserID: "alice"}, false); err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineReapsInputAndTerminalControlIsIdempotent(t *testing.T) {
	service, request, _ := stagingFixture(t)
	key := []byte(strings.Repeat("k", 32))
	handler, err := NewStagingHandler(key, service)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(server.URL, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if removed, err := service.ReapExpired(request.Deadline.Add(-time.Second)); err != nil || removed != 0 {
		t.Fatalf("premature reaping: %d %v", removed, err)
	}
	if removed, err := service.ReapExpired(request.Deadline.Add(time.Second)); err != nil || removed != 1 {
		t.Fatalf("deadline not reaped: %d %v", removed, err)
	}
	if err := client.Dispose(context.Background(), receipt.LeaseID, "alice", true); err != nil {
		t.Fatal(err)
	}
	if err := client.Dispose(context.Background(), receipt.LeaseID, "alice", true); err != nil {
		t.Fatalf("idempotent cancellation failed: %v", err)
	}
	entries, err := os.ReadDir(service.Stager.Root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("orphan after expiry: %d %v", len(entries), err)
	}
}

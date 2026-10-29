package sandboxbroker

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"claude-codex/internal/backend/agentruntime/sandboxartifact"
	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxpolicy"
	"claude-codex/internal/backend/agentruntime/sandboxstaging"
	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

func TestBrokerStagesOnceThroughSignedHTTPAndDisposes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux Broker only")
	}
	broker, req, _ := testBroker(t)
	root := t.TempDir()
	private := filepath.Join(root, "tasks")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	cache := &sandboxartifact.Cache{Dir: filepath.Join(root, "cache"), Limits: sandboxtransfer.DevLimits(), Source: sandboxartifact.FetchFunc(func(ctx context.Context, ref sandboxartifact.Ref) ([]byte, error) { return broker.Artifacts.Get(ref) })}
	journal, err := sandboxnode.OpenFileJournal(filepath.Join(root, "journal.json"), 16, 64, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := profileDoc()
	node := &sandboxstaging.StagingService{Stager: sandboxnode.Stager{Root: private, Cache: cache, Limits: sandboxtransfer.DevLimits()}, Journal: journal, Revision: "5", Profile: doc.Profiles["isolated"], MaxActive: 1}
	handler, err := sandboxstaging.NewStagingHandler([]byte(strings.Repeat("x", 32)), node)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	pool := doc.NodePools["dev"]
	pool.Endpoint = server.URL
	doc.NodePools["dev"] = pool
	broker.Policy = staticPolicy{sandboxpolicy.Revision{ID: 5, Document: doc}}
	broker.Node = StagingTransport{}
	ctx, _, release, err := broker.PrepareSkill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	task := ctx.Value(preparedKey{}).(*preparedTask)
	snap, err := journal.Snapshot(task.leaseID, req.UserID)
	if err != nil || snap.State != sandboxnode.LeasePrepared {
		t.Fatalf("stage missing: %+v %v", snap, err)
	}
	req.Command = "echo test"
	if _, err := broker.ExecuteSkillCommand(ctx, req); err == nil {
		t.Fatal("executed without Node command handler")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	snap, err = journal.Snapshot(task.leaseID, req.UserID)
	if err != nil || snap.State != sandboxnode.LeaseDisposed {
		t.Fatalf("lease not disposed: %+v %v", snap, err)
	}
	entries, err := os.ReadDir(private)
	if err != nil || len(entries) != 0 {
		t.Fatalf("task input not cleaned: %d %v", len(entries), err)
	}
}

package sandboxpolicy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/sandboxnode"
)

type fixedActive struct{ revision Revision }

func (f fixedActive) Active(context.Context) (Revision, error) { return f.revision, nil }

func TestSimulationBrokerUsesPolicyRevisionAndSecretReference(t *testing.T) {
	key := strings.Repeat("z", 32)
	journal, err := sandboxnode.OpenFileJournal(filepath.Join(t.TempDir(), "jobs.json"), 4, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	sim, err := sandboxnode.NewSimulationHandler([]byte(key), journal, "7")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := sandboxnode.NewProbeHandler([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/sim/", sim)
	mux.Handle("/", probe)
	node := httptest.NewServer(mux)
	defer node.Close()
	doc := testDocument()
	doc.NodePools["wsl-dev"] = NodePool{Endpoint: node.URL, CredentialRef: "sandbox_dev", Environment: "development", MaxConcurrency: 1}
	broker := SimulationBroker{Policy: fixedActive{Revision{ID: 7, Document: doc}}, ResolveCredential: func(_ context.Context, ref string) (string, error) {
		if ref != "sandbox_dev" {
			t.Fatalf("credential ref=%q", ref)
		}
		return key, nil
	}}
	first, err := broker.Prepare(context.Background(), "job", "alice", "idempotent", time.Now().Add(time.Minute))
	if err != nil || first.PolicyRevision != "7" {
		t.Fatalf("prepare=%+v %v", first, err)
	}
	again, err := broker.Prepare(context.Background(), "job", "alice", "idempotent", first.Deadline)
	if err != nil || again.LeaseID != first.LeaseID {
		t.Fatalf("retry=%+v %v", again, err)
	}
	broker.Policy = fixedActive{Revision{ID: 8, Document: doc}}
	if _, err = broker.Prepare(context.Background(), "other", "alice", "other", time.Now().Add(time.Minute)); err == nil {
		t.Fatal("stale node policy revision accepted")
	}
}

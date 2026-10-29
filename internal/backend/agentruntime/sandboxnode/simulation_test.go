package sandboxnode

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedSimulationContract(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	journal, err := OpenFileJournal(filepath.Join(t.TempDir(), "leases.json"), 4, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewSimulationHandler(key, journal, "revision-1")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewProbeClient(server.URL, string(key), nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := PrepareSpec{JobID: "test-job", UserID: "alice", PolicyRevision: "revision-1", IdempotencyKey: "idempotent", Deadline: time.Now().Add(time.Minute)}
	first, err := client.SimulatePrepare(context.Background(), spec)
	if err != nil || first.LeaseID == "" {
		t.Fatalf("prepare=%+v %v", first, err)
	}
	again, err := client.SimulatePrepare(context.Background(), spec)
	if err != nil || again.LeaseID != first.LeaseID {
		t.Fatalf("idempotent retry=%+v %v", again, err)
	}
	spec.PolicyRevision = "stale"
	if _, err := client.SimulatePrepare(context.Background(), spec); err == nil {
		t.Fatal("stale revision accepted")
	}
	executed, err := client.SimulateExec(context.Background(), first.LeaseID, "alice")
	if err != nil || executed.Status != "simulated" || executed.CodeExecuted {
		t.Fatalf("execution=%+v %v", executed, err)
	}
	empty, err := client.SimulateCollect(context.Background(), first.LeaseID, "alice")
	if err != nil || !empty {
		t.Fatalf("collect=%v %v", empty, err)
	}
	events, _, err := client.SimulateEvents(context.Background(), first.LeaseID, "alice", 0)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v %v", events, err)
	}
	canceled, err := client.SimulateCancel(context.Background(), first.LeaseID, "alice")
	if err != nil || canceled.State != LeaseCanceled {
		t.Fatalf("cancel=%+v %v", canceled, err)
	}
	disposed, err := client.SimulateDispose(context.Background(), first.LeaseID, "alice")
	if err != nil || disposed.State != LeaseDisposed {
		t.Fatalf("dispose=%+v %v", disposed, err)
	}
	if _, err := client.SimulateCancel(context.Background(), first.LeaseID, "bob"); err == nil {
		t.Fatal("cross-user cancel succeeded")
	}
	raw := []byte(`{"job_id":"another","user_id":"alice","policy_revision":"revision-1","idempotency_key":"key","deadline":"2030-01-01T00:00:00Z"}`)
	req := httptest.NewRequest(http.MethodPost, server.URL+"/v1/sim/prepare", bytes.NewReader(raw))
	signer, _ := NewTaskSigner(key, nil)
	if err := signer.Sign(req, raw); err != nil {
		t.Fatal(err)
	}
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, req)
	replay := httptest.NewRequest(http.MethodPost, server.URL+"/v1/sim/prepare", bytes.NewReader(raw))
	replay.Header = req.Header.Clone()
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusUnauthorized {
		t.Fatalf("replayed request=%d", replayResponse.Code)
	}
	commandBody := []byte(`{"lease_id":"` + first.LeaseID + `","user_id":"alice","command":"rm -rf /"}`)
	injected := httptest.NewRequest(http.MethodPost, server.URL+"/v1/sim/exec", bytes.NewReader(commandBody))
	if err := signer.Sign(injected, commandBody); err != nil {
		t.Fatal(err)
	}
	injectedResponse := httptest.NewRecorder()
	handler.ServeHTTP(injectedResponse, injected)
	if injectedResponse.Code != http.StatusBadRequest {
		t.Fatalf("simulator accepted command field=%d", injectedResponse.Code)
	}
	tampered := httptest.NewRequest(http.MethodGet, server.URL+"/v1/sim/events?lease_id="+first.LeaseID+"&user_id=alice&cursor=0", nil)
	if err := signer.Sign(tampered, nil); err != nil {
		t.Fatal(err)
	}
	tampered.URL.RawQuery = "lease_id=" + first.LeaseID + "&user_id=bob&cursor=0"
	tamperedResponse := httptest.NewRecorder()
	handler.ServeHTTP(tamperedResponse, tampered)
	if tamperedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("query tampering accepted=%d", tamperedResponse.Code)
	}
}

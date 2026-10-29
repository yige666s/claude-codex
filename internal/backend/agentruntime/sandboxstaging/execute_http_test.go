//go:build linux

package sandboxstaging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-codex/internal/backend/agentruntime/sandboxnode"
	"claude-codex/internal/backend/agentruntime/sandboxoci"
)

func signedDevelopmentRequest(t *testing.T, key []byte, path string, body any) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1"+path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	signer, err := sandboxnode.NewTaskSigner(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Sign(request, data); err != nil {
		t.Fatal(err)
	}
	return request
}
func TestDevelopmentExecutionHTTPRequiresSignedLeaseAndBounds(t *testing.T) {
	worker, service, docker, request := setupRecoveredWorker(t)
	receipt, err := service.prepare(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	defer func() {
		if err := service.dispose(control, true); err != nil {
			t.Error(err)
		}
	}()
	key := []byte(strings.Repeat("k", 32))
	handler, err := NewDevelopmentExecutionHandler(key, worker)
	if err != nil {
		t.Fatal(err)
	}
	req := signedDevelopmentRequest(t, key, "/v1/dev/execute", ExecuteRequest{StageControl: control, ExecutionID: strings.Repeat("a", 32), Script: []byte("echo fake")})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("execute rejected: %d %s", response.Code, response.Body.String())
	}
	if len(docker.scripts) != 1 {
		t.Fatalf("command calls=%d", len(docker.scripts))
	}
	replay := httptest.NewRequest(http.MethodPost, req.URL.String(), bytes.NewReader([]byte("replayed-body")))
	replay.Header = req.Header.Clone()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, replay)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("signature replay accepted: %d", response.Code)
	}
	unauth := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/v1/dev/execute", bytes.NewReader([]byte("{}")))
	unauth.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, unauth)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned command accepted: %d", response.Code)
	}
	foreign := signedDevelopmentRequest(t, key, "/v1/dev/execute", ExecuteRequest{StageControl: StageControl{LeaseID: receipt.LeaseID, UserID: "mallory"}, ExecutionID: strings.Repeat("b", 32), Script: []byte("echo denied")})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, foreign)
	if response.Code != http.StatusConflict || len(docker.scripts) != 1 {
		t.Fatalf("cross-user command reached worker: %d, %d", response.Code, len(docker.scripts))
	}
	oversized := signedDevelopmentRequest(t, key, "/v1/dev/execute", ExecuteRequest{StageControl: control, ExecutionID: strings.Repeat("c", 32), Script: bytes.Repeat([]byte("x"), MaxExecuteBody)})
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, oversized)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized command accepted: %d", response.Code)
	}
	collect := signedDevelopmentRequest(t, key, "/v1/dev/collect", control)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, collect)
	if response.Code != http.StatusOK {
		t.Fatalf("collect failed: %d %s", response.Code, response.Body.String())
	}
	var got CollectReply
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || len(got.Files) != 1 || string(got.Files[0].Content) != "result" {
		t.Fatalf("collect response: %+v %v", got, err)
	}
	if _, err := NewDevelopmentExecutionHandler(key, &DevelopmentWorker{}); err == nil {
		t.Fatal("unrecovered worker accepted")
	}
}

func TestRecoveredDevelopmentHandlerStagesExecutesCollectsAndStops(t *testing.T) {
	service, request, journalPath := stagingFixture(t)
	docker := &workerDocker{archive: fakeWorkerOutput(t)}
	recovery, err := ReconcileDevelopmentStartup(context.Background(), service.Stager, journalPath, docker)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recovery.Close() })
	service.Journal = recovery.Journal
	worker := &DevelopmentWorker{Recovery: recovery, Service: service, Driver: sandboxoci.SessionDriver{
		Planner: sandboxoci.StagedMountPlanner{SkillCacheRoot: service.Stager.Cache.Dir, TaskRoot: service.Stager.Root}, Runner: docker,
	}}
	key := []byte(strings.Repeat("j", 32))
	handler, err := NewRecoveredDevelopmentHandler(key, worker)
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
	control := StageControl{LeaseID: receipt.LeaseID, UserID: request.UserID}
	defer func() { _ = client.Dispose(context.Background(), receipt.LeaseID, request.UserID, true) }()
	for _, cmd := range []string{"echo first", "echo second"} {
		body, marshalErr := json.Marshal(ExecuteRequest{StageControl: control, ExecutionID: strings.Repeat(string(rune('a'+len(docker.scripts))), 32), Script: []byte(cmd)})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		httpReq, requestErr := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/v1/dev/execute", bytes.NewReader(body))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		signer, signerErr := sandboxnode.NewTaskSigner(key, nil)
		if signerErr != nil {
			t.Fatal(signerErr)
		}
		if err := signer.Sign(httpReq, body); err != nil {
			t.Fatal(err)
		}
		response, doErr := http.DefaultClient.Do(httpReq)
		if doErr != nil {
			t.Fatal(doErr)
		}
		var got ExecuteReply
		decodeErr := json.NewDecoder(response.Body).Decode(&got)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || got.Result.Stdout != "ok" {
			t.Fatalf("execute %d %+v %v", response.StatusCode, got, decodeErr)
		}
	}
	collectBytes, err := json.Marshal(control)
	if err != nil {
		t.Fatal(err)
	}
	collectReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/v1/dev/collect", bytes.NewReader(collectBytes))
	if err != nil {
		t.Fatal(err)
	}
	collectReq.Header.Set("Content-Type", "application/json")
	signer, err := sandboxnode.NewTaskSigner(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Sign(collectReq, collectBytes); err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(collectReq)
	if err != nil {
		t.Fatal(err)
	}
	var got CollectReply
	decodeErr := json.NewDecoder(response.Body).Decode(&got)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeErr != nil || len(got.Files) != 1 || string(got.Files[0].Content) != "result" {
		t.Fatalf("collected %d %+v %v", response.StatusCode, got, decodeErr)
	}
	if err := client.Dispose(context.Background(), receipt.LeaseID, request.UserID, false); err != nil {
		t.Fatal(err)
	}
	if len(docker.names) != 0 || len(docker.scripts) != 2 {
		t.Fatalf("development session leaked or commands omitted: %+v", docker)
	}
	if err := worker.Recovery.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Prepare(context.Background(), request); err == nil {
		t.Fatal("admission survived recovery shutdown")
	}
}

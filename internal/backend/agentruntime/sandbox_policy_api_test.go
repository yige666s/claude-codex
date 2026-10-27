package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claude-codex/internal/backend/sandboxpolicy"
)

type testSandboxPolicyRepo struct {
	active sandboxpolicy.Revision
	draft  sandboxpolicy.Revision
}

func (r *testSandboxPolicyRepo) List(context.Context, int) ([]sandboxpolicy.Revision, error) {
	return []sandboxpolicy.Revision{r.active}, nil
}
func (r *testSandboxPolicyRepo) Active(context.Context) (sandboxpolicy.Revision, error) {
	return r.active, nil
}
func (r *testSandboxPolicyRepo) Draft(_ context.Context, d sandboxpolicy.Document, actor string) (sandboxpolicy.Revision, error) {
	if err := d.Validate(); err != nil {
		return sandboxpolicy.Revision{}, err
	}
	r.draft = sandboxpolicy.Revision{ID: 2, Document: d, State: "draft", Actor: actor}
	return r.draft, nil
}
func (r *testSandboxPolicyRepo) Publish(_ context.Context, id, expected int64, actor string) (sandboxpolicy.Revision, error) {
	if expected != r.active.ID || id != r.draft.ID {
		return sandboxpolicy.Revision{}, sandboxpolicy.ErrRevisionConflict
	}
	r.active = r.draft
	r.active.State = "active"
	return r.active, nil
}
func (r *testSandboxPolicyRepo) Rollback(_ context.Context, id, expected int64, actor string) (sandboxpolicy.Revision, error) {
	return sandboxpolicy.Revision{}, errors.New("not implemented in fake")
}

func TestAdminSandboxPolicyRoutesFailClosed(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: t.TempDir(), TurnTimeout: time.Minute}, NewFileSessionStore(t.TempDir()), NewFileMemoryService(t.TempDir()), nil, nil)
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("test-admin-token")
	repo := &testSandboxPolicyRepo{active: sandboxpolicy.Revision{ID: 1, Document: sandboxpolicy.DefaultDocument(), State: "active"}}
	server.SetSandboxPolicyStore(repo)
	call := func(path, body string, admin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("X-User-ID", "alice")
		req.Header.Set("Content-Type", "application/json")
		if admin {
			req.Header.Set("X-Admin-Token", "test-admin-token")
		}
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}
	if rec := call("/v1/admin/ops/sandbox-policy/validate", `{"enabled":false}`, false); rec.Code != http.StatusForbidden {
		t.Fatalf("unprivileged admin API=%d", rec.Code)
	}
	invalid := `{"enabled":true,"default_profile":"host","profiles":{"host":{"driver":"native","node_pool":"dev","network":"host"}},"node_pools":{"dev":{"endpoint":"http://127.0.0.1:18080","credential_ref":"test","environment":"development","max_concurrency":1}}}`
	if rec := call("/v1/admin/ops/sandbox-policy/validate", invalid, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("native policy accepted=%d: %s", rec.Code, rec.Body.String())
	}
	if rec := call("/v1/admin/ops/sandbox-policy/drafts", `{"enabled":false,"profiles":{},"routes":[],"node_pools":{}}`, true); rec.Code != http.StatusCreated {
		t.Fatalf("safe draft=%d: %s", rec.Code, rec.Body.String())
	}
	if rec := call("/v1/admin/ops/sandbox-policy/publish", `{"revision":2,"expected_active":99}`, true); rec.Code != http.StatusConflict {
		t.Fatalf("CAS=%d: %s", rec.Code, rec.Body.String())
	}
	if rec := call("/v1/admin/ops/sandbox-policy/publish", `{"revision":2,"expected_active":1}`, true); rec.Code != http.StatusOK {
		t.Fatalf("publish=%d: %s", rec.Code, rec.Body.String())
	}
	rec := call("/v1/admin/ops/sandbox-policy/preview", `{"origin":"repo_release"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview=%d: %s", rec.Code, rec.Body.String())
	}
	var result struct {
		Routed         bool `json:"routed"`
		SimulationOnly bool `json:"simulation_only"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Routed || !result.SimulationOnly {
		t.Fatalf("preview=%+v, %v", result, err)
	}
}

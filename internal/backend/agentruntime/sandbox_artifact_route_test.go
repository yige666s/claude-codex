package agentruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxtransfer"
)

func TestSandboxArtifactRouteRequiresDedicatedToken(t *testing.T) {
	root := t.TempDir()
	skill := filepath.Join(root, "demo")
	if err := os.Mkdir(skill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("fake skill"), 0600); err != nil {
		t.Fatal(err)
	}
	store := sandboxartifact.Store{Dir: filepath.Join(t.TempDir(), "store"), Builder: sandboxtransfer.Builder{SkillRoots: []string{root}, Limits: sandboxtransfer.DevLimits()}}
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 32)
	handler, err := sandboxartifact.Handler(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, nil, nil)
	path := "/v1/internal/sandbox/v1/skills/demo/" + ref.Digest
	request := httptest.NewRequest(http.MethodGet, path, nil)
	without := httptest.NewRecorder()
	server.ServeHTTP(without, request)
	if without.Code == http.StatusOK {
		t.Fatal("artifact route enabled without configuration")
	}
	server.SetSandboxArtifactHandler(handler)
	without = httptest.NewRecorder()
	server.ServeHTTP(without, request)
	if without.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request=%d", without.Code)
	}
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	withToken := httptest.NewRecorder()
	server.ServeHTTP(withToken, request)
	if withToken.Code != http.StatusOK || withToken.Header().Get("X-Skill-Digest") != ref.Digest || sandboxartifact.Digest(withToken.Body.Bytes()) != ref.Digest {
		t.Fatalf("artifact route rejected authenticated version: %d", withToken.Code)
	}
}

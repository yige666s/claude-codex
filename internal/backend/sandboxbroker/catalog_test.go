package sandboxbroker

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/backend/sandboxartifact"
	"claude-codex/internal/backend/sandboxtransfer"
)

func TestPublishedCatalogBindsIDAndRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "skills")
	skill := filepath.Join(root, "demo")
	if err := os.MkdirAll(skill, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("demo"), 0600); err != nil {
		t.Fatal(err)
	}
	store := sandboxartifact.Store{Dir: filepath.Join(t.TempDir(), "store"), Builder: sandboxtransfer.Builder{SkillRoots: []string{root}, Limits: sandboxtransfer.DevLimits()}}
	published, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	catalog := PublishedCatalog{Store: store, Roots: map[string]string{"demo": skill}}
	req := agentruntime.RemoteSkillRequest{SkillName: "demo", SkillRoot: skill}
	got, err := catalog.Resolve(context.Background(), req)
	if err != nil || got != published {
		t.Fatalf("published ref: %+v %v", got, err)
	}
	req.SkillRoot = filepath.Join(root, "other")
	if _, err := catalog.Resolve(context.Background(), req); err == nil {
		t.Fatal("allowed unregistered source")
	}
	req.SkillRoot = skill
	req.SkillName = "other"
	if _, err := catalog.Resolve(context.Background(), req); err == nil {
		t.Fatal("allowed same root with different id")
	}
}

func TestRealUserRouterOnlyPublishedSkills(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux control plane")
	}
	broker, request, _ := testBroker(t)
	published := PublishedCatalog{Store: broker.Artifacts, Roots: map[string]string{request.SkillName: request.SkillRoot}}
	broker.SkillCatalog = published
	if !broker.RemoteSkillEnabled(context.Background(), request.UserID, request.SkillName) {
		t.Fatal("published Skill not routed")
	}
	if broker.RemoteSkillEnabled(context.Background(), request.UserID, "unsupported") {
		t.Fatal("unpublished Skill routed")
	}
	if broker.RemoteSkillEnabled(context.Background(), "other", request.SkillName) {
		t.Fatal("other user routed")
	}
}

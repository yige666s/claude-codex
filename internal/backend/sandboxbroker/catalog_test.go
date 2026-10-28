package sandboxbroker

import (
	"context"
	"os"
	"path/filepath"
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

package run

import (
	"path/filepath"
	"strings"
	"testing"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/harness/skills"
)

func TestRemoteSkillAllowlistRequiresAdminPublication(t *testing.T) {
	base := t.TempDir()
	manager := skills.NewSkillManager()
	local := &skills.SkillDefinition{Name: "docx", UserInvocable: true, Source: skills.SourceFile, SkillRoot: filepath.Join(base, "docx")}
	if err := manager.RegisterLoadedSkills([]*skills.SkillDefinition{local}); err != nil {
		t.Fatal(err)
	}
	unpublished := agentruntime.NewPublishedSkillCatalog(manager, nil, false)
	if _, err := remotePublishedSkillRoots(manager, unpublished, []string{"docx"}); err == nil || !strings.Contains(err.Error(), "published in Admin") {
		t.Fatalf("unpublished allowlist accepted: %v", err)
	}
	published := agentruntime.NewPublishedSkillCatalog(manager, []string{"docx"}, false)
	roots, err := remotePublishedSkillRoots(manager, published, []string{"docx"})
	if err != nil || roots["docx"] != local.SkillRoot {
		t.Fatalf("published skill rejected: %+v %v", roots, err)
	}
	if _, err := remotePublishedSkillRoots(manager, published, []string{"missing"}); err == nil {
		t.Fatal("missing skill accepted")
	}
}

package agentruntime

import (
	"testing"
	"time"

	"claude-codex/internal/harness/skills"
)

func TestSkillRegistryRecordReadsProductFieldsFromMetadata(t *testing.T) {
	record := skillRegistryRecordFromDefinition(&skills.SkillDefinition{
		Name:        "demo",
		DisplayName: "Demo",
		Description: "Demo skill",
		Metadata: map[string]any{
			"product": map[string]any{
				"version":  "1.2.3",
				"category": "Documents",
				"icon":     "DOC",
			},
		},
	}, time.Now().UTC())

	if record.Version != "1.2.3" || record.Category != "Documents" || record.Icon != "DOC" {
		t.Fatalf("product fields were not read from metadata: %#v", record)
	}
	product, ok := record.Metadata["product"].(map[string]any)
	if !ok || product["category"] != "Documents" {
		t.Fatalf("raw product metadata was not preserved: %#v", record.Metadata)
	}
}

func TestSkillRegistryRecordTopLevelVersionOverridesMetadataVersion(t *testing.T) {
	record := skillRegistryRecordFromDefinition(&skills.SkillDefinition{
		Name:    "demo",
		Version: "2.0.0",
		Metadata: map[string]any{
			"version": "1.0.0",
		},
	}, time.Now().UTC())

	if record.Version != "2.0.0" {
		t.Fatalf("top-level version should win, got %q", record.Version)
	}
}

func TestSkillCodePublishesUserInvocableSkillsByDefault(t *testing.T) {
	if !skillCodePublishesByDefault(&skills.SkillDefinition{Name: "diagram", UserInvocable: true}) {
		t.Fatal("user-invocable code-loaded skill should publish on first registry sync")
	}
	if skillCodePublishesByDefault(&skills.SkillDefinition{Name: "internal", UserInvocable: false}) {
		t.Fatal("non-user-invocable skill should not publish on first registry sync")
	}
	if skillCodePublishesByDefault(&skills.SkillDefinition{Name: "hidden", UserInvocable: true, IsHidden: true}) {
		t.Fatal("hidden skill should not publish on first registry sync")
	}
}

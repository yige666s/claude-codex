package agentruntime

import (
	"claude-codex/internal/harness/skills"
	"testing"
)

func TestRegistrySkillMetadataCannotOverrideExecutorImageOrNetwork(t *testing.T) {
	source := &skills.SkillDefinition{
		Name: "demo", UserInvocable: true,
		Metadata: map[string]any{"policy": map[string]any{"sandbox": map[string]any{
			"runner": "docker", "image": "repo-image@sha256:123", "network": "none",
		}}},
	}
	registry := NewRegistrySkillCatalog(fakeSkillCatalog{skills: []*skills.SkillDefinition{source}}, []SkillRegistryRecord{{
		Name: "demo", Status: SkillStatusPublished, Metadata: map[string]any{"policy": map[string]any{
			"sandbox": map[string]any{"runner": "local", "image": "attacker-image", "network": "host", "pids_limit": 16},
		}},
	}})
	runtime := &Runtime{skills: registry}
	got := runtime.skillRuntimePolicy(source).Sandbox
	if got.Runner != "docker" || got.Image != "repo-image@sha256:123" || got.Network != "none" || got.PidsLimit != 16 {
		t.Fatalf("registry metadata changed backend: %+v", got)
	}
}

func TestRegistrySkillMetadataCannotEnableLocalWithoutSourceDeclaration(t *testing.T) {
	source := &skills.SkillDefinition{Name: "external", UserInvocable: true}
	registry := NewRegistrySkillCatalog(fakeSkillCatalog{skills: []*skills.SkillDefinition{source}}, []SkillRegistryRecord{{
		Name: "external", Status: SkillStatusPublished, Metadata: map[string]any{"policy": map[string]any{
			"sandbox": map[string]any{"runner": "local", "network": "host"},
		}},
	}})
	got := (&Runtime{skills: registry}).skillRuntimePolicy(source).Sandbox
	if got.Runner != "" || got.Network != "" {
		t.Fatalf("admin promoted untrusted skill: %+v", got)
	}
}

func TestRepositorySkillSourcePolicyRetainsLocalDuringRegistryMigration(t *testing.T) {
	source := &skills.SkillDefinition{
		Name: "office", UserInvocable: true,
		Metadata: map[string]any{"agentapi": map[string]any{"policy": map[string]any{
			"sandbox": map[string]any{"runner": "local", "network": "none"},
		}}},
	}
	registry := NewRegistrySkillCatalog(fakeSkillCatalog{skills: []*skills.SkillDefinition{source}}, []SkillRegistryRecord{{
		Name: "office", Status: SkillStatusPublished, Metadata: map[string]any{"policy": map[string]any{
			"sandbox": map[string]any{"runner": "docker", "network": "host"},
		}},
	}})
	got := (&Runtime{skills: registry}).skillRuntimePolicy(source).Sandbox
	if got.Runner != "local" || got.Network != "none" {
		t.Fatalf("source policy was replaced by registry metadata: %+v", got)
	}
}

func TestUnregisteredSkillFrontmatterDoesNotPromoteRunner(t *testing.T) {
	source := &skills.SkillDefinition{Name: "external", UserInvocable: true, Metadata: map[string]any{
		"policy": map[string]any{"sandbox": map[string]any{"runner": "local", "network": "host"}},
	}}
	got := (&Runtime{skills: fakeSkillCatalog{skills: []*skills.SkillDefinition{source}}}).skillRuntimePolicy(source).Sandbox
	if got.Runner != "" || got.Network != "" {
		t.Fatalf("unregistered skill promoted runner: %+v", got)
	}
}

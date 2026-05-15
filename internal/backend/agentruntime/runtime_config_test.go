package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestLLMGovernanceConfigModelPatchBindsVertexLocation(t *testing.T) {
	model := "gemini-3.1-flash-lite"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:               "vertex",
		Model:                  "gemini-2.5-flash",
		VertexLocation:         "us-central1",
		ModelRoutes:            "default=gemini-2.5-flash,skill:vertex-image-artifact=gemini-2.5-flash",
		MaxAttempts:            2,
		RetryBackoff:           300 * time.Millisecond,
		ChatTimeout:            time.Minute,
		SkillTimeout:           90 * time.Second,
		FailureThreshold:       3,
		CircuitBreakerCooldown: time.Minute,
	}, LLMGovernanceConfigPatch{Model: &model})
	if err != nil {
		t.Fatalf("apply model patch: %v", err)
	}
	if updated.Provider != "vertex" || updated.Model != "gemini-3.1-flash-lite" || updated.VertexLocation != "global" {
		t.Fatalf("unexpected runtime model config: %#v", updated)
	}
	if updated.ModelRoutes != "default=gemini-3.1-flash-lite,skill:vertex-image-artifact=gemini-2.5-flash" {
		t.Fatalf("model routes = %q", updated.ModelRoutes)
	}
}

func TestLLMGovernanceConfigRejectsUnsupportedModel(t *testing.T) {
	model := "gemini-1.5-flash"
	if _, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{}, LLMGovernanceConfigPatch{Model: &model}); err == nil {
		t.Fatal("expected unsupported model error")
	}
}

func TestLLMGovernanceConfigRejectsMismatchedVertexLocation(t *testing.T) {
	model := "gemini-3.1-flash-lite"
	location := "us-central1"
	_, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{}, LLMGovernanceConfigPatch{
		Model:          &model,
		VertexLocation: &location,
	})
	if err == nil {
		t.Fatal("expected mismatched location error")
	}
}

func TestLLMGovernanceConfigManagerPreservesRuntimeDefaultsWhenLoadingOldPayload(t *testing.T) {
	store := &memoryRuntimeConfigStore{config: LLMGovernanceConfig{MaxAttempts: 3}}
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{
		Provider:       "vertex",
		Model:          "gemini-2.5-flash",
		VertexLocation: "us-central1",
		ModelRoutes:    "default=gemini-2.5-flash",
	}, store)
	if err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	loaded := manager.Get()
	if loaded.Provider != "vertex" || loaded.Model != "gemini-2.5-flash" || loaded.VertexLocation != "us-central1" {
		t.Fatalf("missing runtime defaults after old payload load: %#v", loaded)
	}
	if loaded.MaxAttempts != 3 {
		t.Fatalf("max attempts = %d, want 3", loaded.MaxAttempts)
	}
}

type memoryRuntimeConfigStore struct {
	config LLMGovernanceConfig
}

func (m *memoryRuntimeConfigStore) LoadLLMGovernanceConfig(context.Context) (LLMGovernanceConfig, bool, error) {
	return m.config, true, nil
}

func (m *memoryRuntimeConfigStore) SaveLLMGovernanceConfig(_ context.Context, config LLMGovernanceConfig) error {
	m.config = config
	return nil
}

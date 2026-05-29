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

func TestLLMGovernanceConfigModelPatchCanSwitchToShortAPI(t *testing.T) {
	model := "google/gemini-3.1-pro-preview"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:       "vertex",
		Model:          "gemini-2.5-flash",
		VertexLocation: "us-central1",
		ModelRoutes:    "default=gemini-2.5-flash,chat:complex=gemini-2.5-pro",
	}, LLMGovernanceConfigPatch{Model: &model})
	if err != nil {
		t.Fatalf("apply shortapi model patch: %v", err)
	}
	if updated.Provider != "shortapi" || updated.Model != model || updated.VertexLocation != "" {
		t.Fatalf("unexpected shortapi runtime model config: %#v", updated)
	}
	if updated.ModelRoutes != "default=google/gemini-3.1-pro-preview,chat:complex=gemini-2.5-pro" {
		t.Fatalf("model routes = %q", updated.ModelRoutes)
	}
}

func TestLLMGovernanceConfigProviderPatchAllowsShortAPIAlias(t *testing.T) {
	provider := "short"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{Provider: "vertex"}, LLMGovernanceConfigPatch{Provider: &provider})
	if err != nil {
		t.Fatalf("apply shortapi provider patch: %v", err)
	}
	if updated.Provider != "shortapi" {
		t.Fatalf("provider = %q, want shortapi", updated.Provider)
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

func TestLLMGovernanceConfigManagerLoadsModelCatalogFromStore(t *testing.T) {
	store := &memoryRuntimeConfigStore{
		config: LLMGovernanceConfig{Model: "openai/gpt-5.4-mini"},
		models: []LLMModelOption{
			{ID: "openai/gpt-5.4-mini", Label: "GPT 5.4 Mini", Provider: "openai"},
		},
	}
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{}, store)
	if err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	status := manager.StatusMap()
	models, ok := status["allowed_models"].([]LLMModelOption)
	if !ok || len(models) != 1 || models[0].ID != "openai/gpt-5.4-mini" {
		t.Fatalf("allowed models were not loaded from store: %#v", status["allowed_models"])
	}
	if got := manager.Get(); got.Provider != "openai" || got.Model != "openai/gpt-5.4-mini" {
		t.Fatalf("config was not normalized with stored catalog: %#v", got)
	}
}

func TestLLMGovernanceConfigManagerSeedsDefaultModelCatalog(t *testing.T) {
	store := &memoryRuntimeConfigStore{configOK: true, modelsOK: false}
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{Model: "gemini-2.5-flash"}, store)
	if err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(store.models) == 0 {
		t.Fatal("expected default model catalog to be seeded")
	}
}

type memoryRuntimeConfigStore struct {
	config   LLMGovernanceConfig
	configOK bool
	models   []LLMModelOption
	modelsOK bool
}

func (m *memoryRuntimeConfigStore) LoadLLMGovernanceConfig(context.Context) (LLMGovernanceConfig, bool, error) {
	return m.config, m.configOK || m.config != (LLMGovernanceConfig{}), nil
}

func (m *memoryRuntimeConfigStore) SaveLLMGovernanceConfig(_ context.Context, config LLMGovernanceConfig) error {
	m.config = config
	m.configOK = true
	return nil
}

func (m *memoryRuntimeConfigStore) LoadLLMModelCatalog(context.Context) ([]LLMModelOption, bool, error) {
	return m.models, m.modelsOK || len(m.models) > 0, nil
}

func (m *memoryRuntimeConfigStore) SaveLLMModelCatalog(_ context.Context, models []LLMModelOption) error {
	m.models = copyLLMModelOptions(models)
	m.modelsOK = true
	return nil
}

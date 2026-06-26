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

func TestLLMGovernanceConfigModelPatchAllowsGemini35Flash(t *testing.T) {
	model := "gemini-3.5-flash"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:       "vertex",
		Model:          "gemini-2.5-flash",
		VertexLocation: "us-central1",
		ModelRoutes:    "default=gemini-2.5-flash,chat=gemini-2.5-flash",
	}, LLMGovernanceConfigPatch{Model: &model})
	if err != nil {
		t.Fatalf("apply gemini 3.5 flash model patch: %v", err)
	}
	if updated.Provider != "vertex" || updated.Model != "gemini-3.5-flash" || updated.VertexLocation != "global" {
		t.Fatalf("unexpected runtime model config: %#v", updated)
	}
	if updated.ModelRoutes != "default=gemini-3.5-flash,chat=gemini-3.5-flash" {
		t.Fatalf("model routes = %q", updated.ModelRoutes)
	}
}

func TestLLMGovernanceConfigModelPatchUpdatesChatRoutes(t *testing.T) {
	model := "gemini-2.5-pro"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:       "vertex",
		Model:          "gemini-3.1-flash-lite",
		VertexLocation: "global",
		ModelRoutes:    "default=gemini-3.1-flash-lite,chat=gemini-3.1-flash-lite,chat:search=gemini-3.1-flash-lite,skill:vertex-image-artifact=gemini-2.5-flash",
	}, LLMGovernanceConfigPatch{Model: &model})
	if err != nil {
		t.Fatalf("apply model patch: %v", err)
	}
	want := "default=gemini-2.5-pro,chat=gemini-2.5-pro,chat:search=gemini-2.5-pro,skill:vertex-image-artifact=gemini-2.5-flash"
	if updated.ModelRoutes != want {
		t.Fatalf("model routes = %q, want %q", updated.ModelRoutes, want)
	}
	if updated.VertexLocation != "us-central1" {
		t.Fatalf("vertex location = %q, want us-central1", updated.VertexLocation)
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
	if updated.ModelRoutes != "default=google/gemini-3.1-pro-preview,chat:complex=google/gemini-3.1-pro-preview" {
		t.Fatalf("model routes = %q", updated.ModelRoutes)
	}
}

func TestLLMGovernanceConfigModelPatchCanSwitchToNVIDIA(t *testing.T) {
	model := "nvidia/nemotron-3-ultra-550b-a55b"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:       "vertex",
		Model:          "gemini-2.5-flash",
		VertexLocation: "us-central1",
		ModelRoutes:    "default=gemini-2.5-flash,chat:complex=gemini-2.5-pro",
	}, LLMGovernanceConfigPatch{Model: &model})
	if err != nil {
		t.Fatalf("apply nvidia model patch: %v", err)
	}
	if updated.Provider != "nvidia" || updated.Model != model || updated.VertexLocation != "" {
		t.Fatalf("unexpected nvidia runtime model config: %#v", updated)
	}
	if updated.ModelRoutes != "default=nvidia/nemotron-3-ultra-550b-a55b,chat:complex=nvidia/nemotron-3-ultra-550b-a55b" {
		t.Fatalf("model routes = %q", updated.ModelRoutes)
	}
}

func TestLLMGovernanceConfigModelPatchCanSwitchToDeepSeek(t *testing.T) {
	model := "deepseek-chat"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:       "nvidia",
		Model:          "nvidia/nemotron-3-ultra-550b-a55b",
		ModelRoutes:    "default=nvidia/nemotron-3-ultra-550b-a55b,chat=nvidia/nemotron-3-ultra-550b-a55b,chat:complex=nvidia/nemotron-3-ultra-550b-a55b",
		VertexLocation: "us-central1",
	}, LLMGovernanceConfigPatch{Model: &model})
	if err != nil {
		t.Fatalf("apply deepseek model patch: %v", err)
	}
	if updated.Provider != "deepseek" || updated.Model != model || updated.VertexLocation != "" {
		t.Fatalf("unexpected deepseek runtime model config: %#v", updated)
	}
	if updated.ModelRoutes != "default=deepseek-chat,chat=deepseek-chat,chat:complex=deepseek-chat" {
		t.Fatalf("model routes = %q", updated.ModelRoutes)
	}
}

func TestLLMGovernanceConfigLoadPreservesCrossProviderRoutes(t *testing.T) {
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{}, &memoryRuntimeConfigStore{
		config: LLMGovernanceConfig{
			Provider:    "nvidia",
			Model:       "nvidia/nemotron-3-ultra-550b-a55b",
			ModelRoutes: "default=nvidia/nemotron-3-ultra-550b-a55b,chat=deepseek-chat,chat:complex=deepseek-reasoner,skill=nvidia/nemotron-3-ultra-550b-a55b",
		},
	})
	if err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	got := manager.Get()
	want := "default=nvidia/nemotron-3-ultra-550b-a55b,chat=deepseek-chat,chat:complex=deepseek-reasoner,skill=nvidia/nemotron-3-ultra-550b-a55b"
	if got.ModelRoutes != want {
		t.Fatalf("model routes = %q, want %q", got.ModelRoutes, want)
	}
}

func TestLLMGovernanceConfigLoadResetsUnknownRouteModels(t *testing.T) {
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{}, &memoryRuntimeConfigStore{
		config: LLMGovernanceConfig{
			Provider:    "nvidia",
			Model:       "nvidia/nemotron-3-ultra-550b-a55b",
			ModelRoutes: "default=nvidia/nemotron-3-ultra-550b-a55b,chat=removed-model",
		},
	})
	if err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	got := manager.Get()
	want := "default=nvidia/nemotron-3-ultra-550b-a55b,chat=nvidia/nemotron-3-ultra-550b-a55b"
	if got.ModelRoutes != want {
		t.Fatalf("model routes = %q, want %q", got.ModelRoutes, want)
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

func TestLLMGovernanceConfigPatchUpdatesAPIRateLimit(t *testing.T) {
	limit := 25
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		APIRateLimitPerMinute: 60,
	}, LLMGovernanceConfigPatch{APIRateLimitPerMinute: &limit})
	if err != nil {
		t.Fatalf("apply api rate limit patch: %v", err)
	}
	if updated.APIRateLimitPerMinute != 25 {
		t.Fatalf("api rate limit = %d, want 25", updated.APIRateLimitPerMinute)
	}
	status := llmGovernanceConfigStatusMap(updated)
	if status["api_rate_limit_per_minute"] != 25 {
		t.Fatalf("status api_rate_limit_per_minute = %#v", status["api_rate_limit_per_minute"])
	}
}

func TestLLMGovernanceConfigRejectsNegativeAPIRateLimit(t *testing.T) {
	limit := -1
	if _, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{}, LLMGovernanceConfigPatch{APIRateLimitPerMinute: &limit}); err == nil {
		t.Fatal("expected negative api rate limit error")
	}
}

func TestLLMGovernanceConfigMissingAPIRateLimitPayloadUsesDefault(t *testing.T) {
	loaded := llmGovernanceConfigFromPayload(llmGovernanceConfigPayload{Model: "deepseek-chat"})
	defaults := LLMGovernanceConfig{Model: "deepseek-chat", APIRateLimitPerMinute: 60}
	merged := mergeLLMGovernanceConfigDefaults(defaults, loaded)
	if merged.APIRateLimitPerMinute != 60 {
		t.Fatalf("api rate limit = %d, want default 60", merged.APIRateLimitPerMinute)
	}
}

func TestLLMGovernanceConfigZeroAPIRateLimitPayloadIsExplicit(t *testing.T) {
	zero := 0
	loaded := llmGovernanceConfigFromPayload(llmGovernanceConfigPayload{Model: "deepseek-chat", APIRateLimitPerMinute: &zero})
	defaults := LLMGovernanceConfig{Model: "deepseek-chat", APIRateLimitPerMinute: 60}
	merged := mergeLLMGovernanceConfigDefaults(defaults, loaded)
	if merged.APIRateLimitPerMinute != 0 {
		t.Fatalf("api rate limit = %d, want explicit zero", merged.APIRateLimitPerMinute)
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
	if !ok {
		t.Fatalf("allowed models were not loaded from store: %#v", status["allowed_models"])
	}
	if _, ok := llmModelOptionFor("openai/gpt-5.4-mini", models); !ok {
		t.Fatalf("stored model was not preserved: %#v", models)
	}
	if got := manager.Get(); got.Provider != "openai" || got.Model != "openai/gpt-5.4-mini" {
		t.Fatalf("config was not normalized with stored catalog: %#v", got)
	}
}

func TestLLMGovernanceConfigManagerMergesNewDefaultModelsIntoStoredCatalog(t *testing.T) {
	store := &memoryRuntimeConfigStore{
		models: []LLMModelOption{
			{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash", Provider: "vertex", VertexLocation: "us-central1"},
			{ID: "openai/gpt-5.4-mini", Label: "GPT 5.4 Mini", Provider: "openai"},
		},
	}
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{}, store)
	if err := manager.Load(context.Background()); err != nil {
		t.Fatalf("load config: %v", err)
	}
	status := manager.StatusMap()
	models, ok := status["allowed_models"].([]LLMModelOption)
	if !ok {
		t.Fatalf("allowed models were not typed as model options: %#v", status["allowed_models"])
	}
	if _, ok := llmModelOptionFor("gemini-3.5-flash", models); !ok {
		t.Fatalf("gemini-3.5-flash was not merged into allowed models: %#v", models)
	}
	if _, ok := llmModelOptionFor("openai/gpt-5.4-mini", models); !ok {
		t.Fatalf("custom stored model was not preserved: %#v", models)
	}
	if _, ok := llmModelOptionFor("gemini-3.5-flash", store.models); !ok {
		t.Fatalf("merged default model was not persisted: %#v", store.models)
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

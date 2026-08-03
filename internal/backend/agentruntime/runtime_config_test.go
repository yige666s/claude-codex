package agentruntime

import (
	"context"
	"errors"
	"strings"
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

func TestLLMGovernanceConfigModelPatchResetsStaleSubmittedRoutesOnProviderSwitch(t *testing.T) {
	model := "nvidia/nemotron-3-ultra-550b-a55b"
	routes := "default=deepseek-chat,chat=deepseek-chat,chat:normal=deepseek-chat,skill=deepseek-chat"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{
		Provider:    "deepseek",
		Model:       "deepseek-chat",
		ModelRoutes: routes,
	}, LLMGovernanceConfigPatch{
		Model:       &model,
		ModelRoutes: &routes,
	})
	if err != nil {
		t.Fatalf("apply nvidia model patch with stale routes: %v", err)
	}
	want := "default=nvidia/nemotron-3-ultra-550b-a55b,chat=nvidia/nemotron-3-ultra-550b-a55b,chat:normal=nvidia/nemotron-3-ultra-550b-a55b,skill=nvidia/nemotron-3-ultra-550b-a55b"
	if updated.ModelRoutes != want {
		t.Fatalf("model routes = %q, want %q", updated.ModelRoutes, want)
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

func TestLLMGovernanceConfigPatchUpdatesParallelBranchBudget(t *testing.T) {
	maxBranches := 3
	timeoutMS := int64(45000)
	maxTools := 5
	maxSources := 7
	maxTokens := 1800
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{}, LLMGovernanceConfigPatch{
		MaxParallelBranches:     &maxBranches,
		ParallelBranchTimeoutMS: &timeoutMS,
		ParallelMaxToolCalls:    &maxTools,
		ParallelMaxSources:      &maxSources,
		ParallelMaxTokens:       &maxTokens,
	})
	if err != nil {
		t.Fatalf("applyLLMGovernanceConfigPatch() error = %v", err)
	}
	if updated.MaxParallelBranches != maxBranches ||
		updated.ParallelBranchTimeout != 45*time.Second ||
		updated.ParallelMaxToolCalls != maxTools ||
		updated.ParallelMaxSources != maxSources ||
		updated.ParallelMaxTokens != maxTokens {
		t.Fatalf("parallel branch budget not applied: %#v", updated)
	}
	payload := llmGovernanceConfigToPayload(updated)
	roundTrip := llmGovernanceConfigFromPayload(payload).normalized()
	if roundTrip.MaxParallelBranches != maxBranches || roundTrip.ParallelBranchTimeout != 45*time.Second || roundTrip.ParallelMaxSources != maxSources {
		t.Fatalf("parallel branch budget did not round-trip: %#v", roundTrip)
	}
}

func TestLLMGovernanceConfigPatchUpdatesLoopGovernance(t *testing.T) {
	maxLoopDurationMS := int64(120000)
	maxLoopActions := 5
	maxBranchCount := 4
	maxBranchConcurrency := 2
	evaluatorTimeoutMS := int64(45000)
	conflictTimeoutMS := int64(30000)
	maxSourcesPerBranch := 9
	searchQualityThreshold := 0.72
	automaticTriggerEnabled := true
	riskyWriteApprovalMode := "block"
	updated, err := applyLLMGovernanceConfigPatch(LLMGovernanceConfig{}, LLMGovernanceConfigPatch{
		MaxLoopDurationMS:       &maxLoopDurationMS,
		MaxLoopActions:          &maxLoopActions,
		MaxBranchCount:          &maxBranchCount,
		MaxBranchConcurrency:    &maxBranchConcurrency,
		EvaluatorTimeoutMS:      &evaluatorTimeoutMS,
		ConflictTimeoutMS:       &conflictTimeoutMS,
		MaxSourcesPerBranch:     &maxSourcesPerBranch,
		SearchQualityThreshold:  &searchQualityThreshold,
		AutomaticTriggerEnabled: &automaticTriggerEnabled,
		RiskyWriteApprovalMode:  &riskyWriteApprovalMode,
	})
	if err != nil {
		t.Fatalf("applyLLMGovernanceConfigPatch() error = %v", err)
	}
	if updated.MaxLoopDuration != 2*time.Minute ||
		updated.MaxLoopActions != maxLoopActions ||
		updated.MaxBranchCount != maxBranchCount ||
		updated.MaxBranchConcurrency != maxBranchConcurrency ||
		updated.EvaluatorTimeout != 45*time.Second ||
		updated.ConflictTimeout != 30*time.Second ||
		updated.MaxSourcesPerBranch != maxSourcesPerBranch ||
		updated.SearchQualityThreshold != searchQualityThreshold ||
		!updated.AutomaticTriggerEnabled ||
		updated.RiskyWriteApprovalMode != riskyWriteApprovalMode {
		t.Fatalf("loop governance not applied: %#v", updated)
	}
	status := llmGovernanceConfigStatusMap(updated)
	if status["max_loop_actions"] != maxLoopActions || status["risky_write_approval_mode"] != riskyWriteApprovalMode {
		t.Fatalf("loop governance status missing fields: %#v", status)
	}
	roundTrip := llmGovernanceConfigFromPayload(llmGovernanceConfigToPayload(updated)).normalized()
	if roundTrip.MaxLoopDuration != 2*time.Minute ||
		roundTrip.MaxBranchConcurrency != maxBranchConcurrency ||
		roundTrip.SearchQualityThreshold != searchQualityThreshold ||
		!roundTrip.AutomaticTriggerEnabled ||
		roundTrip.RiskyWriteApprovalMode != riskyWriteApprovalMode {
		t.Fatalf("loop governance did not round-trip: %#v", roundTrip)
	}
}

func TestRuntimeDeepAgentJobPolicyUsesGovernanceConfig(t *testing.T) {
	runtime := &Runtime{config: RuntimeConfig{
		LLMGovernanceProvider: func() LLMGovernanceConfig {
			return LLMGovernanceConfig{
				ChatTimeout:     30 * time.Second,
				SkillTimeout:    45 * time.Second,
				MaxLoopDuration: 2 * time.Minute,
				MaxLoopActions:  4,
			}
		},
	}}
	policy := runtime.deepAgentJobPolicy()
	if policy.MaxActions != 4 || policy.MaxDuration != 2*time.Minute {
		t.Fatalf("policy did not use governance max loop config: %#v", policy)
	}
	if policy.StepTimeout != 2*time.Minute {
		t.Fatalf("step timeout = %v, want capped max loop duration", policy.StepTimeout)
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

func TestLLMGovernanceConfigManagerSyncsStartupConfigOverStoredConfig(t *testing.T) {
	store := &memoryRuntimeConfigStore{
		config: LLMGovernanceConfig{
			Provider:    "deepseek",
			Model:       "deepseek-chat",
			ModelRoutes: "default=deepseek-chat,chat=deepseek-chat",
		},
		configOK: true,
	}
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{
		Provider:    "simple",
		Model:       "simple",
		ModelRoutes: "default=simple,chat=simple",
	}, store)
	if err := manager.LoadAndSyncStartupConfig(context.Background()); err != nil {
		t.Fatalf("sync startup config: %v", err)
	}
	loaded := manager.Get()
	if loaded.Provider != "simple" || loaded.Model != "simple" {
		t.Fatalf("manager config = %#v, want startup simple config", loaded)
	}
	if store.config.Provider != "simple" || store.config.Model != "simple" {
		t.Fatalf("stored config = %#v, want startup simple config persisted", store.config)
	}
	if store.config.ModelRoutes != "default=simple,chat=simple" {
		t.Fatalf("stored model routes = %q, want startup routes", store.config.ModelRoutes)
	}
}

func TestLLMGovernanceConfigManagerRejectsInvalidUpdateBeforePersist(t *testing.T) {
	store := &memoryRuntimeConfigStore{
		config: LLMGovernanceConfig{
			Provider:    "simple",
			Model:       "simple",
			ModelRoutes: "default=simple,chat=simple",
		},
		configOK: true,
	}
	manager := NewLLMGovernanceConfigManager(store.config, store)
	manager.SetValidator(func(context.Context, LLMGovernanceConfig) error {
		return errors.New("llm credential is required")
	})
	model := "deepseek-chat"
	if _, err := manager.Update(context.Background(), LLMGovernanceConfigPatch{Model: &model}); err == nil {
		t.Fatal("expected validator error")
	}
	if got := manager.Get(); got.Provider != "simple" || got.Model != "simple" {
		t.Fatalf("manager config changed after rejected update: %#v", got)
	}
	if store.config.Provider != "simple" || store.config.Model != "simple" {
		t.Fatalf("stored config changed after rejected update: %#v", store.config)
	}
}

func TestLLMGovernanceConfigManagerReportsModelAvailability(t *testing.T) {
	manager := NewLLMGovernanceConfigManager(LLMGovernanceConfig{
		Provider:    "simple",
		Model:       "simple",
		ModelRoutes: "default=simple,chat=simple",
	}, nil)
	manager.SetValidator(func(_ context.Context, config LLMGovernanceConfig) error {
		if config.Provider == "deepseek" {
			return errors.New("llm credential is required for provider \"deepseek\"; set DEEPSEEK_API_KEY or AGENT_API_LLM_API_KEY and recreate AgentAPI")
		}
		return nil
	})

	status := manager.StatusMap()
	availability, ok := status["model_availability"].([]LLMModelAvailability)
	if !ok {
		t.Fatalf("model availability was not typed: %#v", status["model_availability"])
	}
	byID := make(map[string]LLMModelAvailability, len(availability))
	for _, item := range availability {
		byID[item.ID] = item
	}
	if !byID["simple"].Available {
		t.Fatalf("simple model should be available: %#v", byID["simple"])
	}
	deepseek := byID["deepseek-chat"]
	if deepseek.Available || deepseek.Provider != "deepseek" || !strings.Contains(deepseek.Reason, "DEEPSEEK_API_KEY") {
		t.Fatalf("unexpected deepseek availability: %#v", deepseek)
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

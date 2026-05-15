package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/tools"
)

func TestBuildLLMConfigOpenAIAndCustom(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	openai, err := buildLLMConfig("openai", "", "", "", "", 30)
	if err != nil {
		t.Fatalf("build openai config: %v", err)
	}
	if openai.Provider != "openai" || openai.APIKey != "openai-key" || openai.BaseURL == "" {
		t.Fatalf("unexpected openai config: %#v", openai)
	}

	custom, err := buildLLMConfig("custom", "local-model", "custom-key", "", "http://localhost:11434/v1", 30)
	if err != nil {
		t.Fatalf("build custom config: %v", err)
	}
	if custom.Provider != "custom" || custom.Model != "local-model" || custom.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("unexpected custom config: %#v", custom)
	}
}

func TestBuildLLMConfigVertexUsesTokenEnv(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "vertex-token")
	cfg, err := buildLLMConfig("vertex", "gemini-1.5-flash", "", "", "", 30)
	if err != nil {
		t.Fatalf("build vertex config: %v", err)
	}
	if cfg.Provider != "vertex" || cfg.Token != "vertex-token" {
		t.Fatalf("unexpected vertex config: %#v", cfg)
	}
}

func TestBuildLLMConfigVertexAllowsApplicationCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS_JSON", `{"client_email":"agentapi@example.iam.gserviceaccount.com","private_key":"-----BEGIN PRIVATE KEY-----\n-----END PRIVATE KEY-----"}`)
	cfg, err := buildLLMConfig("vertex", "gemini-1.5-flash", "", "", "", 30)
	if err != nil {
		t.Fatalf("build vertex config with application credentials: %v", err)
	}
	if cfg.Provider != "vertex" || cfg.Token != "" {
		t.Fatalf("unexpected vertex config: %#v", cfg)
	}
}

func TestBuildLLMConfigQwenUsesDashScopeEnv(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "dashscope-key")
	cfg, err := buildLLMConfig("qwen", "", "", "", "", 30)
	if err != nil {
		t.Fatalf("build qwen config: %v", err)
	}
	if cfg.Provider != "qwen" || cfg.APIKey != "dashscope-key" || cfg.Model != "qwen-plus" {
		t.Fatalf("unexpected qwen config: %#v", cfg)
	}
	if !strings.Contains(cfg.BaseURL, "dashscope.aliyuncs.com/compatible-mode/v1") {
		t.Fatalf("unexpected qwen base url: %q", cfg.BaseURL)
	}
}

func TestLLMConfigReadinessCheckValidatesVertexProject(t *testing.T) {
	t.Setenv("VERTEX_ACCESS_TOKEN", "vertex-token")
	t.Setenv("VERTEX_PROJECT_ID", "")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	cfg, err := buildLLMConfig("vertex", "gemini-2.5-pro", "", "", "", 30)
	if err != nil {
		t.Fatalf("build vertex config: %v", err)
	}
	err = llmConfigReadinessCheck(cfg)(context.Background())
	if err == nil || !strings.Contains(err.Error(), "project ID") {
		t.Fatalf("expected project ID readiness error, got %v", err)
	}
	t.Setenv("VERTEX_PROJECT_ID", "project-1")
	if err := llmConfigReadinessCheck(cfg)(context.Background()); err != nil {
		t.Fatalf("readiness should pass with project ID: %v", err)
	}
}

func TestParseLLMFallbacksAndModelRoutes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")
	fallbacks := parseLLMFallbacks("openai:gpt-4o-mini", 30)
	if len(fallbacks) != 1 || fallbacks[0].Provider != "openai" || fallbacks[0].Model != "gpt-4o-mini" {
		t.Fatalf("unexpected fallbacks %#v", fallbacks)
	}

	routes := parseModelRoutes("default=gemini-2.5-pro,skill:vertex-image-artifact=gemini-2.5-pro")
	if routes["default"] != "gemini-2.5-pro" || routes["skill:vertex-image-artifact"] != "gemini-2.5-pro" {
		t.Fatalf("unexpected routes %#v", routes)
	}
}

func TestLoadSkillsUsesExplicitSkillDirs(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo skill\nuser-invocable: true\n---\n\nDemo body\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	manager := loadSkills([]string{filepath.Join(root, "skills")})
	if _, ok := manager.GetSkill("demo"); !ok {
		t.Fatal("expected explicit skill dir to load demo skill")
	}
}

func TestConsumerChatRegistryHidesFilesystemTools(t *testing.T) {
	registry := buildRegistry(t.TempDir(), skills.NewSkillManager(), true, nil, 0, nil, consumerChatToolNames())
	names := descriptorNameSet(registry)

	for _, hidden := range []string{"Read", "Glob", "Grep", "Write", "Edit", "Bash"} {
		if names[hidden] {
			t.Fatalf("consumer chat registry exposed internal tool %s: %#v", hidden, names)
		}
	}
	for _, visible := range []string{"WebSearch", "WebFetch", "Skill"} {
		if !names[visible] {
			t.Fatalf("consumer chat registry should expose %s: %#v", visible, names)
		}
	}
}

func TestSkillScopedRegistryUsesSkillPolicy(t *testing.T) {
	global := allowedToolNames(true)
	scope := agentruntime.Scope{SkillScoped: true, AllowedTools: []string{"Read", "Grep", "Bash"}}
	allowed := effectiveAllowedToolNames(global, scope)
	registry := buildRegistry(t.TempDir(), skills.NewSkillManager(), true, nil, 0, nil, allowed)
	names := descriptorNameSet(registry)

	for _, visible := range []string{"Read", "Grep", "Bash"} {
		if !names[visible] {
			t.Fatalf("skill-scoped registry should expose %s: %#v", visible, names)
		}
	}
	for _, hidden := range []string{"Glob", "WebSearch", "WebFetch", "Write", "Edit"} {
		if names[hidden] {
			t.Fatalf("skill-scoped registry exposed unrequested tool %s: %#v", hidden, names)
		}
	}
}

func TestSkillScopedRegistryDefaultsToNoToolsWithoutPolicy(t *testing.T) {
	allowed := effectiveAllowedToolNames(allowedToolNames(true), agentruntime.Scope{SkillScoped: true})
	registry := buildRegistry(t.TempDir(), skills.NewSkillManager(), true, nil, 0, nil, allowed)
	if names := descriptorNameSet(registry); len(names) != 0 {
		t.Fatalf("skill-scoped registry without an explicit policy should expose no tools: %#v", names)
	}
}

func TestBuildMessageContextCacheSelectsBackends(t *testing.T) {
	cache, client := buildMessageContextCache("memory", "", time.Hour)
	if _, ok := cache.(*agentruntime.MemorySessionContextCache); !ok {
		t.Fatalf("expected memory cache, got %T", cache)
	}
	if client != nil {
		t.Fatalf("memory cache should not create redis client")
	}

	cache, client = buildMessageContextCache("none", "", time.Hour)
	if _, ok := cache.(agentruntime.NoopSessionContextCache); !ok {
		t.Fatalf("expected noop cache, got %T", cache)
	}
	if client != nil {
		t.Fatalf("noop cache should not create redis client")
	}

	cache, client = buildMessageContextCache("redis", "redis://localhost:6379/1?prefix=agentapi:message:ctx", time.Hour)
	if _, ok := cache.(*agentruntime.RedisSessionContextCache); !ok {
		t.Fatalf("expected redis cache, got %T", cache)
	}
	if client == nil {
		t.Fatal("redis cache should create redis client")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
}

func TestBuildMessageSequenceAllocatorSelectsBackends(t *testing.T) {
	allocator, client := buildMessageSequenceAllocator("sql", "")
	if allocator != nil {
		t.Fatalf("sql sequence backend should not create allocator, got %T", allocator)
	}
	if client != nil {
		t.Fatalf("sql sequence backend should not create redis client")
	}

	allocator, client = buildMessageSequenceAllocator("redis", "redis://localhost:6379/1?prefix=agentapi:message:seq")
	if _, ok := allocator.(*agentruntime.RedisMessageSequenceAllocator); !ok {
		t.Fatalf("expected redis sequence allocator, got %T", allocator)
	}
	if client == nil {
		t.Fatal("redis sequence allocator should create redis client")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}
}

func TestMessageEventsBackendMode(t *testing.T) {
	cases := []struct {
		backend     string
		wantKafka   bool
		wantLocal   bool
		description string
	}{
		{backend: "local", wantLocal: true, description: "local keeps in-process vector indexing"},
		{backend: "kafka", wantKafka: true, description: "kafka publishes only to kafka"},
		{backend: "dual", wantKafka: true, wantLocal: true, description: "dual publishes to both"},
		{backend: "none", description: "none disables message events"},
	}
	for _, tc := range cases {
		gotKafka, gotLocal := messageEventsBackendMode(tc.backend)
		if gotKafka != tc.wantKafka || gotLocal != tc.wantLocal {
			t.Fatalf("%s: got kafka=%t local=%t", tc.description, gotKafka, gotLocal)
		}
	}
}

func descriptorNameSet(registry interface{ Descriptors() []tools.Descriptor }) map[string]bool {
	out := map[string]bool{}
	for _, descriptor := range registry.Descriptors() {
		out[descriptor.Name] = true
	}
	return out
}

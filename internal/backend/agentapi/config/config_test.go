package config

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestDefaultReadsEnvironmentFallbacks(t *testing.T) {
	t.Setenv("AGENT_API_SQL_MAX_OPEN_CONNS", "37")
	t.Setenv("AGENT_API_STORE_BACKEND", "sql")
	t.Setenv("AGENT_API_LLM_PROVIDER", "openai")
	t.Setenv("AGENT_API_MODEL", "gemini-2.5-pro")
	t.Setenv("AGENT_API_PLUGIN_DIR", "/tmp/plugins")
	t.Setenv("AGENT_API_OBJECT_TIMEOUT", "3s")
	t.Setenv("AGENT_API_TIMEZONE", "Asia/Shanghai")
	t.Setenv("AGENT_API_LOCALE", "zh-CN")
	t.Setenv("AGENT_API_LIVE_VOICE_NAME", "Kore")
	t.Setenv("AGENT_API_LIVE_LANGUAGE_CODE", "zh-CN")
	t.Setenv("AGENT_API_DEEP_AGENT_V2_ENABLED", "true")
	t.Setenv("AGENT_API_DEEP_AGENT_V2_SHADOW_ROUTE", "true")
	t.Setenv("AGENT_API_DEEP_RESEARCH_ORCHESTRATOR_WORKER_ENABLED", "true")
	t.Setenv("AGENT_API_DEEP_RESEARCH_WORKER_BACKEND", "harness_agent")
	t.Setenv("AGENT_API_DEEP_RESEARCH_MAX_WORKERS", "6")
	t.Setenv("AGENT_API_DEEP_RESEARCH_MAX_CONCURRENCY", "2")
	t.Setenv("AGENT_API_DEEP_RESEARCH_WORKER_TIMEOUT", "7m")
	t.Setenv("AGENT_API_DEEP_RESEARCH_TOTAL_TIMEOUT", "21m")
	t.Setenv("AGENT_API_DEEP_RESEARCH_MAX_RETRIES", "3")
	t.Setenv("AGENT_API_DEEP_RESEARCH_REPLAN_ENABLED", "true")
	t.Setenv("AGENT_API_DEEP_RESEARCH_MAX_REPLANS", "4")
	t.Setenv("AGENT_API_DEEP_RESEARCH_REPLAN_EVERY_BATCHES", "2")
	t.Setenv("AGENT_API_DEEP_RESEARCH_FALLBACK_LEGACY", "false")
	t.Setenv("AGENT_API_DEEP_RESEARCH_REQUIRE_SOURCES", "false")
	t.Setenv("AGENT_API_DEEP_RESEARCH_MIN_SUCCESSFUL_WORKERS", "2")
	t.Setenv("AGENT_API_MEMORY_POLICY_PATH", "/tmp/memory-policy.yaml")
	t.Setenv("AGENT_API_MEMORY_POLICY_VERSION", "memory-v9")
	t.Setenv("AGENT_API_MEMORY_POLICY_RELOAD_INTERVAL", "30s")
	t.Setenv("AGENT_API_MEMORY_POLICY_STRICT_EVAL", "true")

	cfg := Default()

	if cfg.SQLMaxOpen != 37 {
		t.Fatalf("SQLMaxOpen = %d, want 37", cfg.SQLMaxOpen)
	}
	if cfg.StoreBackend != "sql" {
		t.Fatalf("StoreBackend = %q, want sql", cfg.StoreBackend)
	}
	if cfg.PluginDir != "/tmp/plugins" {
		t.Fatalf("PluginDir = %q, want /tmp/plugins", cfg.PluginDir)
	}
	if cfg.LLMProvider != "openai" {
		t.Fatalf("LLMProvider = %q, want openai", cfg.LLMProvider)
	}
	if cfg.Model != "gemini-2.5-pro" {
		t.Fatalf("Model = %q, want gemini-2.5-pro", cfg.Model)
	}
	if cfg.ObjectTimeout != 3*time.Second {
		t.Fatalf("ObjectTimeout = %s, want 3s", cfg.ObjectTimeout)
	}
	if cfg.Timezone != "Asia/Shanghai" {
		t.Fatalf("Timezone = %q, want Asia/Shanghai", cfg.Timezone)
	}
	if cfg.Locale != "zh-CN" {
		t.Fatalf("Locale = %q, want zh-CN", cfg.Locale)
	}
	if cfg.LiveVoiceName != "Kore" {
		t.Fatalf("LiveVoiceName = %q, want Kore", cfg.LiveVoiceName)
	}
	if cfg.LiveLanguageCode != "zh-CN" {
		t.Fatalf("LiveLanguageCode = %q, want zh-CN", cfg.LiveLanguageCode)
	}
	if !cfg.DeepAgentV2Enabled || !cfg.DeepAgentV2ShadowRoute {
		t.Fatalf("deep agent v2 flags not loaded: enabled=%v shadow=%v", cfg.DeepAgentV2Enabled, cfg.DeepAgentV2ShadowRoute)
	}
	if !cfg.DeepResearchOrchestratorWorkerEnabled ||
		cfg.DeepResearchWorkerBackend != "harness_agent" ||
		cfg.DeepResearchMaxWorkers != 6 ||
		cfg.DeepResearchMaxConcurrency != 2 ||
		cfg.DeepResearchWorkerTimeout != 7*time.Minute ||
		cfg.DeepResearchTotalTimeout != 21*time.Minute ||
		cfg.DeepResearchMaxRetries != 3 ||
		!cfg.DeepResearchReplanEnabled ||
		cfg.DeepResearchMaxReplans != 4 ||
		cfg.DeepResearchReplanEveryBatches != 2 ||
		cfg.DeepResearchFallbackLegacy ||
		cfg.DeepResearchRequireSources ||
		cfg.DeepResearchMinSuccessfulWorkers != 2 {
		t.Fatalf("deep research flags not loaded: %#v", cfg)
	}
	if cfg.MemoryPolicyPath != "/tmp/memory-policy.yaml" ||
		cfg.MemoryPolicyVersion != "memory-v9" ||
		cfg.MemoryPolicyReloadInterval != 30*time.Second ||
		!cfg.MemoryPolicyStrictEval {
		t.Fatalf("memory policy flags not loaded: %#v", cfg)
	}
}

func TestDefaultReadsLLMModelAlias(t *testing.T) {
	t.Setenv("AGENT_API_LLM_MODEL", "gemini-2.5-flash")

	cfg := Default()

	if cfg.Model != "gemini-2.5-flash" {
		t.Fatalf("Model = %q, want gemini-2.5-flash", cfg.Model)
	}
}

func TestDefaultEnablesMemoryPolicyReloadWhenPathIsConfigured(t *testing.T) {
	t.Setenv("AGENT_API_MEMORY_POLICY_PATH", "/tmp/memory-policy.json")

	cfg := Default()

	if cfg.MemoryPolicyReloadInterval != 30*time.Second {
		t.Fatalf("MemoryPolicyReloadInterval = %s, want 30s", cfg.MemoryPolicyReloadInterval)
	}
}

func TestDefaultDisablesRateLimiting(t *testing.T) {
	cfg := Default()

	if cfg.RateLimitBackend != "none" {
		t.Fatalf("RateLimitBackend = %q, want none", cfg.RateLimitBackend)
	}
	if cfg.OperationRateLimits != "" {
		t.Fatalf("OperationRateLimits = %q, want empty", cfg.OperationRateLimits)
	}
}

func TestBindFlagsOverridesConfig(t *testing.T) {
	cfg := Default()
	command := &cobra.Command{
		Use: "agentapi-test",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Validate()
		},
	}
	BindFlags(command, &cfg)
	command.SetArgs([]string{
		"--addr", ":9090",
		"--sql-max-open-conns", "11",
		"--live-enabled",
		"--live-voice-name", "Puck",
		"--live-language-code", "en-US",
		"--object-timeout", "4s",
		"--plugin-dir", "/opt/plugins",
		"--mcp-servers", `{"name":"notes","transport":"sdk"}`,
		"--timezone", "UTC",
		"--locale", "en-US",
		"--memory-policy-path", "/tmp/policy.json",
		"--memory-policy-version", "memory-v10",
		"--memory-policy-reload-interval", "45s",
		"--memory-policy-strict-eval",
	})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if cfg.Addr != ":9090" {
		t.Fatalf("Addr = %q, want :9090", cfg.Addr)
	}
	if cfg.SQLMaxOpen != 11 {
		t.Fatalf("SQLMaxOpen = %d, want 11", cfg.SQLMaxOpen)
	}
	if !cfg.LiveEnabled {
		t.Fatal("LiveEnabled = false, want true")
	}
	if cfg.PluginDir != "/opt/plugins" {
		t.Fatalf("PluginDir = %q, want /opt/plugins", cfg.PluginDir)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "notes" || cfg.MCPServers[0].Transport != "sdk" {
		t.Fatalf("MCPServers = %#v, want notes/sdk", cfg.MCPServers)
	}
	if cfg.LiveVoiceName != "Puck" {
		t.Fatalf("LiveVoiceName = %q, want Puck", cfg.LiveVoiceName)
	}
	if cfg.LiveLanguageCode != "en-US" {
		t.Fatalf("LiveLanguageCode = %q, want en-US", cfg.LiveLanguageCode)
	}
	if cfg.ObjectTimeout != 4*time.Second {
		t.Fatalf("ObjectTimeout = %s, want 4s", cfg.ObjectTimeout)
	}
	if cfg.Timezone != "UTC" {
		t.Fatalf("Timezone = %q, want UTC", cfg.Timezone)
	}
	if cfg.Locale != "en-US" {
		t.Fatalf("Locale = %q, want en-US", cfg.Locale)
	}
	if cfg.MemoryPolicyPath != "/tmp/policy.json" ||
		cfg.MemoryPolicyVersion != "memory-v10" ||
		cfg.MemoryPolicyReloadInterval != 45*time.Second ||
		!cfg.MemoryPolicyStrictEval {
		t.Fatalf("memory policy flag overrides not loaded: %#v", cfg)
	}
}

func TestValidateRequiresAddress(t *testing.T) {
	cfg := Default()
	cfg.Addr = " "

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want addr error")
	}
}

func TestValidateParsesMCPServersJSON(t *testing.T) {
	cfg := Default()
	cfg.MCPServersJSON = `[{"name":"notes","transport":"stdio","command":["echo","hello"]}]`

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(cfg.MCPServers) != 1 {
		t.Fatalf("MCPServers len = %d, want 1", len(cfg.MCPServers))
	}
	if cfg.MCPServers[0].Name != "notes" {
		t.Fatalf("MCPServers[0].Name = %q, want notes", cfg.MCPServers[0].Name)
	}
}

func TestValidateRejectsInvalidMCPServersJSON(t *testing.T) {
	cfg := Default()
	cfg.MCPServersJSON = `not-json`

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want JSON parse failure")
	}
}

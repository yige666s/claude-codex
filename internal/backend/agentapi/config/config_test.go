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
	t.Setenv("AGENT_API_OBJECT_TIMEOUT", "3s")
	t.Setenv("AGENT_API_TIMEZONE", "Asia/Shanghai")
	t.Setenv("AGENT_API_LOCALE", "zh-CN")
	t.Setenv("AGENT_API_LIVE_VOICE_NAME", "Kore")
	t.Setenv("AGENT_API_LIVE_LANGUAGE_CODE", "zh-CN")
	t.Setenv("AGENT_API_DEEP_AGENT_V2_ENABLED", "true")
	t.Setenv("AGENT_API_DEEP_AGENT_V2_SHADOW_ROUTE", "true")

	cfg := Default()

	if cfg.SQLMaxOpen != 37 {
		t.Fatalf("SQLMaxOpen = %d, want 37", cfg.SQLMaxOpen)
	}
	if cfg.StoreBackend != "sql" {
		t.Fatalf("StoreBackend = %q, want sql", cfg.StoreBackend)
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
}

func TestDefaultReadsLLMModelAlias(t *testing.T) {
	t.Setenv("AGENT_API_LLM_MODEL", "gemini-2.5-flash")

	cfg := Default()

	if cfg.Model != "gemini-2.5-flash" {
		t.Fatalf("Model = %q, want gemini-2.5-flash", cfg.Model)
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
		"--timezone", "UTC",
		"--locale", "en-US",
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
}

func TestValidateRequiresAddress(t *testing.T) {
	cfg := Default()
	cfg.Addr = " "

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want addr error")
	}
}

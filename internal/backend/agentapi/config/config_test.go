package config

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestDefaultReadsEnvironmentFallbacks(t *testing.T) {
	t.Setenv("AGENT_API_SQL_MAX_OPEN_CONNS", "37")
	t.Setenv("AGENT_API_LLM_PROVIDER", "openai")
	t.Setenv("AGENT_API_OBJECT_TIMEOUT", "3s")

	cfg := Default()

	if cfg.SQLMaxOpen != 37 {
		t.Fatalf("SQLMaxOpen = %d, want 37", cfg.SQLMaxOpen)
	}
	if cfg.LLMProvider != "openai" {
		t.Fatalf("LLMProvider = %q, want openai", cfg.LLMProvider)
	}
	if cfg.ObjectTimeout != 3*time.Second {
		t.Fatalf("ObjectTimeout = %s, want 3s", cfg.ObjectTimeout)
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
		"--object-timeout", "4s",
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
	if cfg.ObjectTimeout != 4*time.Second {
		t.Fatalf("ObjectTimeout = %s, want 4s", cfg.ObjectTimeout)
	}
}

func TestValidateRequiresAddress(t *testing.T) {
	cfg := Default()
	cfg.Addr = " "

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want addr error")
	}
}

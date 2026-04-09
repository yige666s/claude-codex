package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ding/claude-code/claude-go/internal/app/config"
)

func TestSetConfigValue_ExtendedKeys(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  string
		assert func(t *testing.T, cfg *config.Config)
	}{
		{
			name:  "secret store",
			key:   "secret_store",
			value: "keychain",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.SecretStore != "keychain" {
					t.Fatalf("expected secret_store to be updated, got %q", cfg.SecretStore)
				}
			},
		},
		{
			name:  "plugin dir",
			key:   "plugin_dir",
			value: "/tmp/plugins",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.PluginDir != "/tmp/plugins" {
					t.Fatalf("expected plugin_dir to be updated, got %q", cfg.PluginDir)
				}
			},
		},
		{
			name:  "bridge secret",
			key:   "bridge_secret",
			value: "bridge-secret",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if cfg.BridgeSecret != "bridge-secret" {
					t.Fatalf("expected bridge_secret to be updated, got %q", cfg.BridgeSecret)
				}
			},
		},
		{
			name:  "telemetry insecure",
			key:   "telemetry.insecure",
			value: "true",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				if !cfg.Telemetry.Insecure {
					t.Fatal("expected telemetry.insecure to be true")
				}
			},
		},
		{
			name:  "oauth scopes",
			key:   "oauth.scopes",
			value: "openid, profile , email",
			assert: func(t *testing.T, cfg *config.Config) {
				t.Helper()
				want := []string{"openid", "profile", "email"}
				if !reflect.DeepEqual(cfg.OAuth.Scopes, want) {
					t.Fatalf("expected oauth.scopes %v, got %v", want, cfg.OAuth.Scopes)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			if err := setConfigValue(&cfg, tt.key, tt.value); err != nil {
				t.Fatalf("setConfigValue(%q): %v", tt.key, err)
			}
			tt.assert(t, &cfg)
		})
	}
}

func TestSplitAndTrimCSV(t *testing.T) {
	got := splitAndTrimCSV(" a, ,b,c ,, d ")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestParseMCPServerArgs_RejectsIncompleteFlags(t *testing.T) {
	tests := [][]string{
		{"--url"},
		{"--"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			_, err := parseMCPServerArgs("demo", args)
			if err == nil {
				t.Fatal("expected error for incomplete MCP args")
			}
			if !strings.Contains(err.Error(), "usage: /mcp add demo") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

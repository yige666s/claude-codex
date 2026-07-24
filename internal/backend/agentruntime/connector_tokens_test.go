package agentruntime

import (
	"strings"
	"testing"
)

func TestProtectConnectorSecretRequiresEncryptionKey(t *testing.T) {
	t.Setenv("AGENT_API_CONNECTOR_TOKEN_SECRET", "")

	protected, err := protectConnectorSecret("oauth-access-token")
	if err == nil {
		t.Fatal("protectConnectorSecret() error = nil, want missing encryption key error")
	}
	if protected != "" {
		t.Fatalf("protectConnectorSecret() = %q, want no persisted value", protected)
	}
	if !strings.Contains(err.Error(), "AGENT_API_CONNECTOR_TOKEN_SECRET") {
		t.Fatalf("protectConnectorSecret() error = %q, want configuration guidance", err)
	}
}

func TestProtectConnectorSecretEncryptsWhenKeyConfigured(t *testing.T) {
	t.Setenv("AGENT_API_CONNECTOR_TOKEN_SECRET", "test-only-connector-token-key")

	protected, err := protectConnectorSecret("oauth-access-token")
	if err != nil {
		t.Fatalf("protectConnectorSecret() error = %v", err)
	}
	if !strings.HasPrefix(protected, "aesgcm:") {
		t.Fatalf("protectConnectorSecret() = %q, want aesgcm ciphertext", protected)
	}

	plain, err := unprotectConnectorSecret(protected)
	if err != nil {
		t.Fatalf("unprotectConnectorSecret() error = %v", err)
	}
	if plain != "oauth-access-token" {
		t.Fatalf("unprotectConnectorSecret() = %q, want original token", plain)
	}
}

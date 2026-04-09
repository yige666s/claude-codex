package oauth

import (
	"testing"
	"time"
)

func TestGenerateCodeVerifier(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(verifier) == 0 {
		t.Error("expected non-empty verifier")
	}
}

func TestGenerateCodeChallenge(t *testing.T) {
	verifier := "test_verifier"
	challenge := GenerateCodeChallenge(verifier)
	if len(challenge) == 0 {
		t.Error("expected non-empty challenge")
	}

	// Same verifier should produce same challenge
	challenge2 := GenerateCodeChallenge(verifier)
	if challenge != challenge2 {
		t.Error("expected same challenge for same verifier")
	}
}

func TestGenerateState(t *testing.T) {
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(state) == 0 {
		t.Error("expected non-empty state")
	}

	// Should generate different states
	state2, _ := GenerateState()
	if state == state2 {
		t.Error("expected different states")
	}
}

func TestGeneratePKCEParams(t *testing.T) {
	params, err := GeneratePKCEParams()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if params.CodeVerifier == "" {
		t.Error("expected non-empty code verifier")
	}
	if params.CodeChallenge == "" {
		t.Error("expected non-empty code challenge")
	}
	if params.State == "" {
		t.Error("expected non-empty state")
	}

	// Verify challenge matches verifier
	expectedChallenge := GenerateCodeChallenge(params.CodeVerifier)
	if params.CodeChallenge != expectedChallenge {
		t.Error("challenge does not match verifier")
	}
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "single scope",
			input:    "profile",
			expected: []string{"profile"},
		},
		{
			name:     "multiple scopes",
			input:    "profile organization claude_ai:inference",
			expected: []string{"profile", "organization", "claude_ai:inference"},
		},
		{
			name:     "extra spaces",
			input:    "profile  organization",
			expected: []string{"profile", "organization"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseScopes(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d scopes, got %d", len(tt.expected), len(result))
				return
			}
			for i, scope := range result {
				if scope != tt.expected[i] {
					t.Errorf("expected scope %s, got %s", tt.expected[i], scope)
				}
			}
		})
	}
}

func TestShouldUseClaudeAIAuth(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		expected bool
	}{
		{
			name:     "has inference scope",
			scopes:   []string{"profile", "claude_ai:inference"},
			expected: true,
		},
		{
			name:     "no inference scope",
			scopes:   []string{"profile", "organization"},
			expected: false,
		},
		{
			name:     "empty scopes",
			scopes:   []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUseClaudeAIAuth(tt.scopes)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBuildAuthURL(t *testing.T) {
	config := &OAuthConfig{
		ClientID:             "test_client",
		ConsoleAuthorizeURL:  "https://console.anthropic.com/oauth/authorize",
		ClaudeAIAuthorizeURL: "https://claude.ai/oauth/authorize",
		ManualRedirectURL:    "https://example.com/callback",
	}

	opts := &AuthURLOptions{
		CodeChallenge: "test_challenge",
		State:         "test_state",
		Port:          8080,
		IsManual:      false,
	}

	url := BuildAuthURL(config, opts)

	// Check that URL contains required parameters
	if url == "" {
		t.Error("expected non-empty URL")
	}

	// Should use console URL by default
	if len(url) < len(config.ConsoleAuthorizeURL) {
		t.Error("URL too short")
	}
}

func TestAuthCodeListener(t *testing.T) {
	t.Run("start and get port", func(t *testing.T) {
		listener := NewAuthCodeListener("/callback")
		port, err := listener.Start(0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if port == 0 {
			t.Error("expected non-zero port")
		}

		if listener.GetPort() != port {
			t.Error("GetPort() returned different port")
		}

		listener.Close()
	})

	t.Run("close listener", func(t *testing.T) {
		listener := NewAuthCodeListener("/callback")
		_, err := listener.Start(0)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		err = listener.Close()
		if err != nil {
			t.Errorf("expected no error on close, got %v", err)
		}

		// Second close should be safe
		err = listener.Close()
		if err != nil {
			t.Errorf("expected no error on second close, got %v", err)
		}
	})
}

func TestIsTokenExpired(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name      string
		expiresAt int64
		expected  bool
	}{
		{
			name:      "expired",
			expiresAt: now - 3600,
			expected:  true,
		},
		{
			name:      "expires soon",
			expiresAt: now + 60,
			expected:  true,
		},
		{
			name:      "not expired",
			expiresAt: now + 3600,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsTokenExpired(tt.expiresAt)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestHasProfileScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		expected bool
	}{
		{
			name:     "has profile",
			scopes:   []string{"profile", "organization"},
			expected: true,
		},
		{
			name:     "no profile",
			scopes:   []string{"organization"},
			expected: false,
		},
		{
			name:     "empty",
			scopes:   []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasProfileScope(tt.scopes)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClient(t *testing.T) {
	config := &OAuthConfig{
		ClientID:   "test_client",
		TokenURL:   "https://api.anthropic.com/oauth/token",
		BaseAPIURL: "https://api.anthropic.com",
	}

	client := NewClient(config)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.config != config {
		t.Error("client config not set correctly")
	}
}

func TestService(t *testing.T) {
	config := &OAuthConfig{
		ClientID:             "test_client",
		TokenURL:             "https://api.anthropic.com/oauth/token",
		ConsoleAuthorizeURL:  "https://console.anthropic.com/oauth/authorize",
		ClaudeAIAuthorizeURL: "https://claude.ai/oauth/authorize",
		ManualRedirectURL:    "https://example.com/callback",
		ConsoleSuccessURL:    "https://console.anthropic.com/success",
		ClaudeAISuccessURL:   "https://claude.ai/success",
		BaseAPIURL:           "https://api.anthropic.com",
	}

	service := NewService(config)
	if service == nil {
		t.Fatal("expected non-nil service")
	}
	if service.config != config {
		t.Error("service config not set correctly")
	}

	t.Run("handle manual auth code", func(t *testing.T) {
		// Generate PKCE params first
		params, err := GeneratePKCEParams()
		if err != nil {
			t.Fatalf("failed to generate PKCE params: %v", err)
		}
		service.pkceParams = params

		err = service.HandleManualAuthCode("test_code", params.State)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("handle manual auth code with wrong state", func(t *testing.T) {
		params, _ := GeneratePKCEParams()
		service.pkceParams = params

		err := service.HandleManualAuthCode("test_code", "wrong_state")
		if err == nil {
			t.Error("expected error for wrong state")
		}
	})

	t.Run("cleanup", func(t *testing.T) {
		service.Cleanup()
		if service.listener != nil {
			t.Error("expected listener to be nil after cleanup")
		}
	})
}

func TestTokenExchangeResponse(t *testing.T) {
	response := &TokenExchangeResponse{
		AccessToken:  "access_token",
		RefreshToken: "refresh_token",
		ExpiresIn:    3600,
		Scope:        "profile organization",
	}

	if response.AccessToken != "access_token" {
		t.Error("access token not set correctly")
	}
	if response.ExpiresIn != 3600 {
		t.Error("expires_in not set correctly")
	}
}

func TestOAuthTokens(t *testing.T) {
	tokens := &OAuthTokens{
		AccessToken:      "access_token",
		RefreshToken:     "refresh_token",
		ExpiresAt:        time.Now().Unix() + 3600,
		Scopes:           []string{"profile", "organization"},
		SubscriptionType: SubscriptionTypePro,
		RateLimitTier:    RateLimitTierPro,
	}

	if tokens.AccessToken != "access_token" {
		t.Error("access token not set correctly")
	}
	if len(tokens.Scopes) != 2 {
		t.Error("scopes not set correctly")
	}
	if tokens.SubscriptionType != SubscriptionTypePro {
		t.Error("subscription type not set correctly")
	}
}

# OAuth Service

The OAuth service provides OAuth 2.0 authentication with PKCE (Proof Key for Code Exchange) for the Claude Code Go implementation.

## Features

### 1. OAuth 2.0 with PKCE
- **Authorization code flow**: Standard OAuth 2.0 authorization code grant
- **PKCE support**: SHA-256 code challenge for enhanced security
- **State parameter**: CSRF protection with random state generation
- **Token exchange**: Exchange authorization code for access/refresh tokens
- **Token refresh**: Automatic token refresh with scope expansion

### 2. Dual Flow Support
- **Automatic flow**: Opens browser, captures redirect on localhost
- **Manual flow**: User manually copies and pastes authorization code
- **Flexible redirect**: Supports both localhost and manual redirect URLs

### 3. Auth Code Listener
- **Localhost server**: Temporary HTTP server for OAuth callbacks
- **OS-assigned port**: Automatic port selection to avoid conflicts
- **State validation**: CSRF protection with state parameter checking
- **Success redirect**: Automatic browser redirect after authentication
- **Error handling**: Graceful error handling with redirect

### 4. Profile Management
- **Profile fetching**: Retrieve user profile information
- **Subscription detection**: Identify subscription type and tier
- **Rate limit info**: Extract rate limit tier from profile
- **Account storage**: Store account UUID and email

### 5. Multi-Provider Support
- **Console auth**: Anthropic Console authentication
- **Claude.ai auth**: Claude.ai specific authentication
- **Scope management**: Different scopes for different providers
- **Inference-only tokens**: Long-lived inference-only tokens

## Core Components

### types.go
Defines core types and constants:
- `OAuthTokens` - Access and refresh tokens with metadata
- `TokenExchangeResponse` - Token exchange response
- `ProfileResponse` - User profile information
- `OAuthConfig` - OAuth configuration
- `PKCEParams` - PKCE flow parameters
- Subscription and rate limit types

### crypto.go
PKCE cryptographic functions:
- `GenerateCodeVerifier()` - Generate random code verifier
- `GenerateCodeChallenge()` - Generate SHA-256 code challenge
- `GenerateState()` - Generate random state parameter
- `GeneratePKCEParams()` - Generate all PKCE parameters at once

### listener.go
Auth code listener implementation:
- `AuthCodeListener` - Localhost HTTP server for OAuth callbacks
- `Start()` - Start listener on specified port
- `WaitForAuthorization()` - Wait for authorization code
- `HandleSuccessRedirect()` - Redirect browser to success page
- `HandleErrorRedirect()` - Redirect browser to error page
- `BuildAuthURL()` - Build OAuth authorization URL
- `ParseScopes()` - Parse space-separated scope string

### client.go
OAuth client implementation:
- `Client` - OAuth HTTP client
- `ExchangeCodeForTokens()` - Exchange code for tokens
- `RefreshToken()` - Refresh access token
- `FetchProfile()` - Fetch user profile
- `FetchProfileInfo()` - Fetch subscription and rate limit info
- `IsTokenExpired()` - Check if token is expired
- `HasProfileScope()` - Check if profile scope is present

### service.go
OAuth service orchestration:
- `Service` - OAuth service coordinator
- `StartOAuthFlow()` - Start OAuth authorization flow
- `HandleManualAuthCode()` - Handle manually entered auth code
- `Cleanup()` - Clean up resources

## Configuration

### OAuth Config

```go
type OAuthConfig struct {
    ClientID              string // OAuth client ID
    TokenURL              string // Token exchange endpoint
    ConsoleAuthorizeURL   string // Console authorization URL
    ClaudeAIAuthorizeURL  string // Claude.ai authorization URL
    ConsoleSuccessURL     string // Console success redirect URL
    ClaudeAISuccessURL    string // Claude.ai success redirect URL
    ManualRedirectURL     string // Manual flow redirect URL
    BaseAPIURL            string // Base API URL for profile
}
```

### OAuth Scopes

- `claude_ai:inference` - Claude.ai inference access
- `profile` - User profile access
- `organization` - Organization information access

## Usage Examples

### Start OAuth Flow

```go
import "claude-codex/internal/services/oauth"

config := &oauth.OAuthConfig{
    ClientID:             "your_client_id",
    TokenURL:             "https://api.anthropic.com/oauth/token",
    ConsoleAuthorizeURL:  "https://console.anthropic.com/oauth/authorize",
    ClaudeAIAuthorizeURL: "https://claude.ai/oauth/authorize",
    ManualRedirectURL:    "https://example.com/callback",
    ConsoleSuccessURL:    "https://console.anthropic.com/success",
    ClaudeAISuccessURL:   "https://claude.ai/success",
    BaseAPIURL:           "https://api.anthropic.com",
}

service := oauth.NewService(config)
defer service.Cleanup()

tokens, err := service.StartOAuthFlow(
    context.Background(),
    func(manualURL, automaticURL string) error {
        fmt.Println("Manual URL:", manualURL)
        fmt.Println("Automatic URL:", automaticURL)
        // Open browser or display URLs to user
        return nil
    },
    &oauth.OAuthFlowOptions{
        LoginWithClaudeAI: true,
        InferenceOnly:     false,
    },
)
```

### Handle Manual Auth Code

```go
// User manually enters auth code
err := service.HandleManualAuthCode("AUTH_CODE_HERE", state)
```

### Refresh Token

```go
client := oauth.NewClient(config)

newTokens, err := client.RefreshToken(
    context.Background(),
    refreshToken,
    []string{"profile", "organization"},
)
```

### Fetch Profile

```go
profile, err := client.FetchProfile(context.Background(), accessToken)
fmt.Println("Email:", profile.Account.EmailAddress)
fmt.Println("Subscription:", profile.Organization.SubscriptionType)
```

### Check Token Expiry

```go
if oauth.IsTokenExpired(tokens.ExpiresAt) {
    // Refresh token
    newTokens, err := client.RefreshToken(ctx, tokens.RefreshToken, tokens.Scopes)
}
```

### Generate PKCE Parameters

```go
params, err := oauth.GeneratePKCEParams()
fmt.Println("Verifier:", params.CodeVerifier)
fmt.Println("Challenge:", params.CodeChallenge)
fmt.Println("State:", params.State)
```

## Constants

### Subscription Types
- `SubscriptionTypeFree`
- `SubscriptionTypePro`
- `SubscriptionTypeTeam`
- `SubscriptionTypeEnterprise`
- `SubscriptionTypeClaudeAI`
- `SubscriptionTypeClaudeAIPro`
- `SubscriptionTypeClaudeAITeam`

### Rate Limit Tiers
- `RateLimitTierFree`
- `RateLimitTierPro`
- `RateLimitTierTeam`
- `RateLimitTierEnterprise`

### Billing Types
- `BillingTypeSubscription`
- `BillingTypeUsageBased`
- `BillingTypeFree`

### Token Management
- `TokenRefreshBuffer`: 5 minutes before expiry
- `DefaultTokenTimeout`: 15 seconds for HTTP requests

## OAuth Flow

### Automatic Flow
1. Start auth code listener on localhost
2. Generate PKCE parameters (verifier, challenge, state)
3. Build authorization URL with localhost redirect
4. Open browser to authorization URL
5. User authorizes in browser
6. Browser redirects to localhost with auth code
7. Listener captures auth code
8. Exchange code for tokens
9. Fetch profile information
10. Redirect browser to success page
11. Return tokens to caller

### Manual Flow
1. Start auth code listener (for fallback)
2. Generate PKCE parameters
3. Build authorization URL with manual redirect
4. Display URL to user
5. User authorizes in browser
6. User manually copies auth code
7. User pastes auth code into CLI
8. Exchange code for tokens
9. Fetch profile information
10. Return tokens to caller

## Security Features

### PKCE (Proof Key for Code Exchange)
- Code verifier: 32-byte random value, base64url encoded
- Code challenge: SHA-256 hash of verifier, base64url encoded
- Prevents authorization code interception attacks

### State Parameter
- 32-byte random value for CSRF protection
- Validated on callback to prevent CSRF attacks

### Token Refresh
- Automatic refresh before expiry (5 minute buffer)
- Scope expansion support for additional permissions
- Refresh token rotation for enhanced security

## Testing

Run tests:
```bash
go test ./internal/services/oauth/...
```

Run with coverage:
```bash
go test -cover ./internal/services/oauth/...
```

## Integration

The OAuth service integrates with:
- Auth utilities for token storage
- Config management for account info
- Browser utilities for opening URLs
- Analytics for event logging

## Performance Characteristics

- **Listener startup**: < 100ms
- **Token exchange**: 1-3 seconds (network dependent)
- **Profile fetch**: 1-2 seconds (network dependent)
- **Token refresh**: 1-3 seconds (network dependent)
- **Listener cleanup**: < 100ms

## Error Handling

Common errors:
- `failed to start OAuth callback server` - Port already in use
- `no authorization code received` - User denied authorization
- `invalid state parameter` - CSRF attack or state mismatch
- `authentication failed: invalid authorization code` - Code expired or invalid
- `token exchange failed` - Network error or invalid request
- `token refresh failed` - Refresh token expired or revoked

## Next Steps

After OAuth service completion, the next service to refactor is: **analytics service**

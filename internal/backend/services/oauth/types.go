package oauth

import "time"

// SubscriptionType represents the user's subscription level
type SubscriptionType string

const (
	SubscriptionTypeFree         SubscriptionType = "free"
	SubscriptionTypePro          SubscriptionType = "pro"
	SubscriptionTypeTeam         SubscriptionType = "team"
	SubscriptionTypeEnterprise   SubscriptionType = "enterprise"
	SubscriptionTypeClaudeAI     SubscriptionType = "claude_ai"
	SubscriptionTypeClaudeAIPro  SubscriptionType = "claude_ai_pro"
	SubscriptionTypeClaudeAITeam SubscriptionType = "claude_ai_team"
)

// RateLimitTier represents the user's rate limit tier
type RateLimitTier string

const (
	RateLimitTierFree       RateLimitTier = "free"
	RateLimitTierPro        RateLimitTier = "pro"
	RateLimitTierTeam       RateLimitTier = "team"
	RateLimitTierEnterprise RateLimitTier = "enterprise"
)

// BillingType represents the billing type
type BillingType string

const (
	BillingTypeSubscription BillingType = "subscription"
	BillingTypeUsageBased   BillingType = "usage_based"
	BillingTypeFree         BillingType = "free"
)

// OAuthTokens contains OAuth access and refresh tokens
type OAuthTokens struct {
	AccessToken      string           `json:"access_token"`
	RefreshToken     string           `json:"refresh_token"`
	ExpiresAt        int64            `json:"expires_at"`
	Scopes           []string         `json:"scopes"`
	SubscriptionType SubscriptionType `json:"subscription_type,omitempty"`
	RateLimitTier    RateLimitTier    `json:"rate_limit_tier,omitempty"`
	Profile          *ProfileResponse `json:"profile,omitempty"`
	TokenAccount     *TokenAccount    `json:"token_account,omitempty"`
}

// TokenAccount contains account info from token exchange
type TokenAccount struct {
	UUID             string `json:"uuid"`
	EmailAddress     string `json:"email_address"`
	OrganizationUUID string `json:"organization_uuid,omitempty"`
}

// TokenExchangeResponse is the response from token exchange
type TokenExchangeResponse struct {
	AccessToken  string             `json:"access_token"`
	RefreshToken string             `json:"refresh_token"`
	ExpiresIn    int                `json:"expires_in"`
	Scope        string             `json:"scope,omitempty"`
	Account      *TokenAccount      `json:"account,omitempty"`
	Organization *TokenOrganization `json:"organization,omitempty"`
}

// TokenOrganization contains organization info from token exchange
type TokenOrganization struct {
	UUID string `json:"uuid"`
}

// ProfileResponse contains user profile information
type ProfileResponse struct {
	Account      AccountInfo      `json:"account"`
	Organization OrganizationInfo `json:"organization"`
}

// AccountInfo contains account details
type AccountInfo struct {
	UUID         string `json:"uuid"`
	EmailAddress string `json:"email_address"`
	DisplayName  string `json:"display_name,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// OrganizationInfo contains organization details
type OrganizationInfo struct {
	UUID                  string        `json:"uuid,omitempty"`
	BillingType           BillingType   `json:"billing_type,omitempty"`
	HasExtraUsageEnabled  bool          `json:"has_extra_usage_enabled,omitempty"`
	SubscriptionCreatedAt string        `json:"subscription_created_at,omitempty"`
	SubscriptionType      string        `json:"subscription_type,omitempty"`
	RateLimitTier         RateLimitTier `json:"rate_limit_tier,omitempty"`
}

// UserRolesResponse contains user role information
type UserRolesResponse struct {
	Roles []string `json:"roles"`
}

// OAuthConfig contains OAuth configuration
type OAuthConfig struct {
	ClientID             string
	TokenURL             string
	ConsoleAuthorizeURL  string
	ClaudeAIAuthorizeURL string
	ConsoleSuccessURL    string
	ClaudeAISuccessURL   string
	ManualRedirectURL    string
	BaseAPIURL           string
}

// PKCEParams contains PKCE flow parameters
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	State         string
}

// AuthURLOptions contains options for building auth URL
type AuthURLOptions struct {
	CodeChallenge     string
	State             string
	Port              int
	IsManual          bool
	LoginWithClaudeAI bool
	InferenceOnly     bool
	OrgUUID           string
	LoginHint         string
	LoginMethod       string
}

// OAuthFlowOptions contains options for OAuth flow
type OAuthFlowOptions struct {
	LoginWithClaudeAI bool
	InferenceOnly     bool
	ExpiresIn         int
	OrgUUID           string
	LoginHint         string
	LoginMethod       string
	SkipBrowserOpen   bool
}

// ProfileInfo contains subscription and rate limit info
type ProfileInfo struct {
	SubscriptionType SubscriptionType
	RateLimitTier    RateLimitTier
	RawProfile       *ProfileResponse
}

// OAuth scopes
const (
	ClaudeAIInferenceScope  = "user:inference"
	ProfileScope            = "user:profile"
	ConsoleScope            = "org:create_api_key"
	ClaudeAISessionsScope   = "user:sessions:claude_code"
	ClaudeAIMCPServersScope = "user:mcp_servers"
	ClaudeAIFileUploadScope = "user:file_upload"
)

// AllOAuthScopes is the full set of OAuth scopes
var AllOAuthScopes = []string{
	ConsoleScope,
	ProfileScope,
	ClaudeAIInferenceScope,
	ClaudeAISessionsScope,
	ClaudeAIMCPServersScope,
	ClaudeAIFileUploadScope,
}

// ClaudeAIOAuthScopes is the Claude AI specific scopes
var ClaudeAIOAuthScopes = []string{
	ProfileScope,
	ClaudeAIInferenceScope,
	ClaudeAISessionsScope,
	ClaudeAIMCPServersScope,
	ClaudeAIFileUploadScope,
}

// Constants for token management
const (
	// TokenRefreshBuffer is the time before expiry to refresh token
	TokenRefreshBuffer = 5 * time.Minute

	// DefaultTokenTimeout is the default timeout for token operations
	DefaultTokenTimeout = 15 * time.Second
)

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"claude-codex/internal/backend/httpclient"
)

// Client handles OAuth token operations
type Client struct {
	config     *OAuthConfig
	httpClient *http.Client
}

// NewClient creates a new OAuth client
func NewClient(config *OAuthConfig) *Client {
	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: DefaultTokenTimeout,
		},
	}
}

// ExchangeCodeForTokens exchanges an authorization code for tokens
func (c *Client) ExchangeCodeForTokens(
	ctx context.Context,
	authorizationCode string,
	state string,
	codeVerifier string,
	port int,
	useManualRedirect bool,
	expiresIn int,
) (*TokenExchangeResponse, error) {
	requestBody := map[string]interface{}{
		"grant_type":    "authorization_code",
		"code":          authorizationCode,
		"client_id":     c.config.ClientID,
		"code_verifier": codeVerifier,
		"state":         state,
	}

	// Set redirect URI
	if useManualRedirect {
		requestBody["redirect_uri"] = c.config.ManualRedirectURL
	} else {
		requestBody["redirect_uri"] = fmt.Sprintf("http://localhost:%d/callback", port)
	}

	// Set expires_in if provided
	if expiresIn > 0 {
		requestBody["expires_in"] = expiresIn
	}

	status, respBody, _, err := httpclient.New(
		httpclient.WithHTTPClient(c.httpClient),
		httpclient.WithComponent("oauth"),
	).Bytes(ctx, http.MethodPost, c.config.TokenURL, requestBody, httpclient.WithAnyStatus())
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}

	// Check status code
	if status != http.StatusOK {
		if status == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication failed: invalid authorization code")
		}
		return nil, fmt.Errorf("token exchange failed (%d): %s", status, string(respBody))
	}

	// Parse response
	var tokenResponse TokenExchangeResponse
	if err := json.Unmarshal(respBody, &tokenResponse); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResponse, nil
}

// RefreshToken refreshes an OAuth token
func (c *Client) RefreshToken(
	ctx context.Context,
	refreshToken string,
	scopes []string,
) (*OAuthTokens, error) {
	// Use Claude AI scopes if not specified
	if len(scopes) == 0 {
		scopes = ClaudeAIOAuthScopes
	}

	requestBody := map[string]interface{}{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.config.ClientID,
		"scope":         joinScopes(scopes),
	}

	status, respBody, _, err := httpclient.New(
		httpclient.WithHTTPClient(c.httpClient),
		httpclient.WithComponent("oauth"),
	).Bytes(ctx, http.MethodPost, c.config.TokenURL, requestBody, httpclient.WithAnyStatus())
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}

	// Check status code
	if status != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (%d): %s", status, string(respBody))
	}

	// Parse response
	var tokenResponse TokenExchangeResponse
	if err := json.Unmarshal(respBody, &tokenResponse); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Use existing refresh token if new one not provided
	newRefreshToken := tokenResponse.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}

	// Calculate expiry
	expiresAt := time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second).Unix()

	return &OAuthTokens{
		AccessToken:  tokenResponse.AccessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
		Scopes:       ParseScopes(tokenResponse.Scope),
	}, nil
}

// FetchProfile fetches the user profile
func (c *Client) FetchProfile(ctx context.Context, accessToken string) (*ProfileResponse, error) {
	endpoint := fmt.Sprintf("%s/api/oauth/profile", c.config.BaseAPIURL)

	var profile ProfileResponse
	err := httpclient.New(
		httpclient.WithHTTPClient(c.httpClient),
		httpclient.WithComponent("oauth"),
	).JSON(ctx, http.MethodGet, endpoint, nil, &profile,
		httpclient.WithBearer(accessToken),
	)
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return nil, fmt.Errorf("profile fetch failed (%d): %s", statusErr.StatusCode, statusErr.Body)
		}
		return nil, fmt.Errorf("profile request failed: %w", err)
	}

	return &profile, nil
}

// FetchProfileInfo fetches subscription and rate limit info
func (c *Client) FetchProfileInfo(ctx context.Context, accessToken string) (*ProfileInfo, error) {
	profile, err := c.FetchProfile(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	// Extract subscription type and rate limit tier
	subscriptionType := SubscriptionTypeFree
	rateLimitTier := RateLimitTierFree

	if profile.Organization.SubscriptionType != "" {
		subscriptionType = SubscriptionType(profile.Organization.SubscriptionType)
	}

	if profile.Organization.RateLimitTier != "" {
		rateLimitTier = profile.Organization.RateLimitTier
	}

	return &ProfileInfo{
		SubscriptionType: subscriptionType,
		RateLimitTier:    rateLimitTier,
		RawProfile:       profile,
	}, nil
}

// IsTokenExpired checks if a token is expired or about to expire
func IsTokenExpired(expiresAt int64) bool {
	now := time.Now().Unix()
	bufferSeconds := int64(TokenRefreshBuffer.Seconds())
	return expiresAt <= (now + bufferSeconds)
}

// HasProfileScope checks if the scopes include profile scope
func HasProfileScope(scopes []string) bool {
	for _, scope := range scopes {
		if scope == ProfileScope {
			return true
		}
	}
	return false
}

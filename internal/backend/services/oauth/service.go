package oauth

import (
	"context"
	"fmt"
	"time"
)

// Service handles OAuth authentication flows
type Service struct {
	config       *OAuthConfig
	client       *Client
	listener     *AuthCodeListener
	pkceParams   *PKCEParams
	manualCodeCh chan string
}

// NewService creates a new OAuth service
func NewService(config *OAuthConfig) *Service {
	return &Service{
		config:       config,
		client:       NewClient(config),
		manualCodeCh: make(chan string, 1),
	}
}

// StartOAuthFlow starts the OAuth authorization flow
func (s *Service) StartOAuthFlow(
	ctx context.Context,
	authURLHandler func(manualURL string, automaticURL string) error,
	opts *OAuthFlowOptions,
) (*OAuthTokens, error) {
	if opts == nil {
		opts = &OAuthFlowOptions{}
	}

	// Generate PKCE parameters
	pkceParams, err := GeneratePKCEParams()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE params: %w", err)
	}
	s.pkceParams = pkceParams

	// Create and start auth code listener
	s.listener = NewAuthCodeListener("/callback")
	port, err := s.listener.Start(0)
	if err != nil {
		return nil, fmt.Errorf("failed to start listener: %w", err)
	}

	// Build auth URLs
	authOpts := &AuthURLOptions{
		CodeChallenge:     pkceParams.CodeChallenge,
		State:             pkceParams.State,
		Port:              port,
		LoginWithClaudeAI: opts.LoginWithClaudeAI,
		InferenceOnly:     opts.InferenceOnly,
		OrgUUID:           opts.OrgUUID,
		LoginHint:         opts.LoginHint,
		LoginMethod:       opts.LoginMethod,
	}

	manualURL := BuildAuthURL(s.config, &AuthURLOptions{
		CodeChallenge:     authOpts.CodeChallenge,
		State:             authOpts.State,
		Port:              authOpts.Port,
		IsManual:          true,
		LoginWithClaudeAI: authOpts.LoginWithClaudeAI,
		InferenceOnly:     authOpts.InferenceOnly,
		OrgUUID:           authOpts.OrgUUID,
		LoginHint:         authOpts.LoginHint,
		LoginMethod:       authOpts.LoginMethod,
	})

	automaticURL := BuildAuthURL(s.config, &AuthURLOptions{
		CodeChallenge:     authOpts.CodeChallenge,
		State:             authOpts.State,
		Port:              authOpts.Port,
		IsManual:          false,
		LoginWithClaudeAI: authOpts.LoginWithClaudeAI,
		InferenceOnly:     authOpts.InferenceOnly,
		OrgUUID:           authOpts.OrgUUID,
		LoginHint:         authOpts.LoginHint,
		LoginMethod:       authOpts.LoginMethod,
	})

	// Wait for authorization code
	authCode, isAutomatic, err := s.waitForAuthorizationCode(ctx, pkceParams.State, func() error {
		return authURLHandler(manualURL, automaticURL)
	})
	if err != nil {
		s.Cleanup()
		return nil, err
	}

	// Exchange code for tokens
	tokenResponse, err := s.client.ExchangeCodeForTokens(
		ctx,
		authCode,
		pkceParams.State,
		pkceParams.CodeVerifier,
		port,
		!isAutomatic,
		opts.ExpiresIn,
	)
	if err != nil {
		if isAutomatic {
			s.listener.HandleErrorRedirect(s.config.ClaudeAISuccessURL)
		}
		s.Cleanup()
		return nil, err
	}

	// Fetch profile info
	profileInfo, err := s.client.FetchProfileInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		if isAutomatic {
			s.listener.HandleErrorRedirect(s.config.ClaudeAISuccessURL)
		}
		s.Cleanup()
		return nil, err
	}

	// Handle success redirect for automatic flow
	if isAutomatic {
		scopes := ParseScopes(tokenResponse.Scope)
		successURL := s.config.ConsoleSuccessURL
		if ShouldUseClaudeAIAuth(scopes) {
			successURL = s.config.ClaudeAISuccessURL
		}
		s.listener.HandleSuccessRedirect(successURL)
	}

	// Format tokens
	tokens := s.formatTokens(tokenResponse, profileInfo)

	s.Cleanup()
	return tokens, nil
}

// waitForAuthorizationCode waits for auth code from either automatic or manual flow
func (s *Service) waitForAuthorizationCode(
	ctx context.Context,
	state string,
	onReady func() error,
) (string, bool, error) {
	// Channel to receive auth code from either flow
	resultCh := make(chan struct {
		code       string
		isAutomatic bool
		err        error
	}, 1)

	// Start automatic flow
	go func() {
		code, err := s.listener.WaitForAuthorization(ctx, state, onReady)
		resultCh <- struct {
			code       string
			isAutomatic bool
			err        error
		}{code, true, err}
	}()

	// Wait for manual or automatic code
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case manualCode := <-s.manualCodeCh:
		// Manual code received, close listener
		s.listener.Close()
		return manualCode, false, nil
	case result := <-resultCh:
		return result.code, result.isAutomatic, result.err
	}
}

// HandleManualAuthCode handles manually entered auth code
func (s *Service) HandleManualAuthCode(authCode string, state string) error {
	if s.pkceParams == nil || state != s.pkceParams.State {
		return fmt.Errorf("invalid state parameter")
	}

	select {
	case s.manualCodeCh <- authCode:
		return nil
	default:
		return fmt.Errorf("manual code channel full")
	}
}

// formatTokens formats the token response
func (s *Service) formatTokens(
	response *TokenExchangeResponse,
	profileInfo *ProfileInfo,
) *OAuthTokens {
	expiresAt := currentTimeMillis() + int64(response.ExpiresIn)*1000

	tokens := &OAuthTokens{
		AccessToken:      response.AccessToken,
		RefreshToken:     response.RefreshToken,
		ExpiresAt:        expiresAt,
		Scopes:           ParseScopes(response.Scope),
		SubscriptionType: profileInfo.SubscriptionType,
		RateLimitTier:    profileInfo.RateLimitTier,
		Profile:          profileInfo.RawProfile,
	}

	if response.Account != nil {
		tokens.TokenAccount = &TokenAccount{
			UUID:             response.Account.UUID,
			EmailAddress:     response.Account.EmailAddress,
			OrganizationUUID: "",
		}
		if response.Organization != nil {
			tokens.TokenAccount.OrganizationUUID = response.Organization.UUID
		}
	}

	return tokens
}

// Cleanup cleans up resources
func (s *Service) Cleanup() {
	if s.listener != nil {
		s.listener.Close()
		s.listener = nil
	}
}

// currentTimeMillis returns current time in milliseconds
func currentTimeMillis() int64 {
	return timeNow().UnixNano() / 1000000
}

// timeNow returns current time (can be mocked for testing)
var timeNow = func() timeProvider {
	return realTime{}
}

type timeProvider interface {
	UnixNano() int64
}

type realTime struct{}

func (realTime) UnixNano() int64 {
	return time.Now().UnixNano()
}

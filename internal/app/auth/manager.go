package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
	oauthsvc "claude-codex/internal/backend/services/oauth"
)

const (
	defaultClientID             = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultBaseAPIURL           = "https://api.anthropic.com"
	defaultConsoleAuthorizeURL  = "https://platform.claude.com/oauth/authorize"
	defaultClaudeAIAuthorizeURL = "https://claude.com/cai/oauth/authorize"
	defaultTokenURL             = "https://platform.claude.com/v1/oauth/token"
	defaultConsoleSuccessURL    = "https://platform.claude.com/buy_credits?returnUrl=/oauth/code/success%3Fapp%3Dclaude-code"
	defaultClaudeAISuccessURL   = "https://platform.claude.com/oauth/code/success?app=claude-code"
	defaultManualRedirectURL    = "https://platform.claude.com/oauth/code/callback"
)

type URLHandler func(manualURL, automaticURL string) error

type LoginOptions struct {
	LoginWithClaudeAI bool
	InferenceOnly     bool
	OrgUUID           string
	LoginHint         string
	LoginMethod       string
	SkipTrustedDevice bool
}

type Status struct {
	Authenticated      bool
	AccessTokenPresent bool
	ExpiresAt          time.Time
	Scopes             []string
	SubscriptionType   oauthsvc.SubscriptionType
	RateLimitTier      oauthsvc.RateLimitTier
	HasTrustedDevice   bool
}

type Manager struct {
	cfg          config.Config
	store        securestorage.Store
	dataStore    *securestorage.DataStore
	oauthConfig  *oauthsvc.OAuthConfig
	newService   func(*oauthsvc.OAuthConfig) *oauthsvc.Service
	refreshToken func(context.Context, string, []string) (*oauthsvc.OAuthTokens, error)
	httpClient   *http.Client
}

func NewManager(cfg config.Config, store securestorage.Store) (*Manager, error) {
	if store == nil {
		resolved, err := securestorage.NewStoreFromConfig(cfg)
		if err != nil {
			return nil, err
		}
		store = resolved
	}
	securestorage.StartKeychainPrefetch(store)

	return &Manager{
		cfg:         cfg,
		store:       store,
		dataStore:   securestorage.NewDataStore(store),
		oauthConfig: OAuthConfigFromAppConfig(cfg),
		newService:  oauthsvc.NewService,
		refreshToken: func(ctx context.Context, refreshToken string, scopes []string) (*oauthsvc.OAuthTokens, error) {
			return oauthsvc.NewClient(OAuthConfigFromAppConfig(cfg)).RefreshToken(ctx, refreshToken, scopes)
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func OAuthConfigFromAppConfig(cfg config.Config) *oauthsvc.OAuthConfig {
	baseAPIURL := strings.TrimSpace(cfg.APIBaseURL)
	if baseAPIURL == "" {
		baseAPIURL = defaultBaseAPIURL
	}
	tokenURL := strings.TrimSpace(cfg.OAuth.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	authURL := strings.TrimSpace(cfg.OAuth.AuthURL)
	if authURL == "" {
		authURL = defaultClaudeAIAuthorizeURL
	}
	clientID := strings.TrimSpace(cfg.OAuth.ClientID)
	if clientID == "" {
		clientID = defaultClientID
	}
	return &oauthsvc.OAuthConfig{
		ClientID:             clientID,
		TokenURL:             tokenURL,
		ConsoleAuthorizeURL:  defaultConsoleAuthorizeURL,
		ClaudeAIAuthorizeURL: authURL,
		ConsoleSuccessURL:    defaultConsoleSuccessURL,
		ClaudeAISuccessURL:   defaultClaudeAISuccessURL,
		ManualRedirectURL:    defaultManualRedirectURL,
		BaseAPIURL:           baseAPIURL,
	}
}

func (m *Manager) Login(ctx context.Context, handler URLHandler, opts LoginOptions) (*oauthsvc.OAuthTokens, error) {
	if handler == nil {
		return nil, errors.New("login URL handler is required")
	}
	service := m.newService(m.oauthConfig)
	if !opts.LoginWithClaudeAI {
		opts.LoginWithClaudeAI = true
	}
	tokens, err := service.StartOAuthFlow(ctx, handler, &oauthsvc.OAuthFlowOptions{
		LoginWithClaudeAI: opts.LoginWithClaudeAI,
		InferenceOnly:     opts.InferenceOnly,
		OrgUUID:           opts.OrgUUID,
		LoginHint:         opts.LoginHint,
		LoginMethod:       opts.LoginMethod,
	})
	if err != nil {
		return nil, err
	}
	if err := m.SaveOAuthTokens(tokens); err != nil {
		return nil, err
	}
	if !opts.SkipTrustedDevice {
		_ = m.EnrollTrustedDevice(ctx)
	}
	return tokens, nil
}

func (m *Manager) Logout() error {
	if err := m.dataStore.DeleteKey(securestorage.KeyClaudeAIOAuth); err != nil {
		return err
	}
	return m.dataStore.DeleteKey(securestorage.KeyTrustedDeviceToken)
}

func (m *Manager) Status(ctx context.Context) (*Status, error) {
	tokens, err := m.GetOAuthTokens(ctx)
	if err != nil {
		return nil, err
	}
	trustedDeviceToken, err := m.GetTrustedDeviceToken()
	if err != nil {
		return nil, err
	}
	status := &Status{
		Authenticated:    tokens != nil,
		HasTrustedDevice: strings.TrimSpace(trustedDeviceToken) != "",
	}
	if tokens != nil {
		status.AccessTokenPresent = strings.TrimSpace(tokens.AccessToken) != ""
		status.ExpiresAt = time.Unix(tokens.ExpiresAt, 0)
		status.Scopes = append([]string(nil), tokens.Scopes...)
		status.SubscriptionType = tokens.SubscriptionType
		status.RateLimitTier = tokens.RateLimitTier
	}
	return status, nil
}

func (m *Manager) GetAccessToken(ctx context.Context) (string, error) {
	tokens, err := m.GetOAuthTokens(ctx)
	if err != nil {
		return "", err
	}
	if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
		return "", errors.New("not logged in")
	}
	return tokens.AccessToken, nil
}

func (m *Manager) GetOAuthTokens(ctx context.Context) (*oauthsvc.OAuthTokens, error) {
	tokens, err := m.LoadOAuthTokens()
	if err != nil || tokens == nil {
		return tokens, err
	}
	if !oauthsvc.IsTokenExpired(tokens.ExpiresAt) {
		return tokens, nil
	}
	refreshed, err := m.refreshToken(ctx, tokens.RefreshToken, tokens.Scopes)
	if err != nil {
		return nil, err
	}
	if refreshed.SubscriptionType == "" {
		refreshed.SubscriptionType = tokens.SubscriptionType
	}
	if refreshed.RateLimitTier == "" {
		refreshed.RateLimitTier = tokens.RateLimitTier
	}
	if refreshed.Profile == nil {
		refreshed.Profile = tokens.Profile
	}
	if refreshed.TokenAccount == nil {
		refreshed.TokenAccount = tokens.TokenAccount
	}
	if err := m.SaveOAuthTokens(refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func (m *Manager) LoadOAuthTokens() (*oauthsvc.OAuthTokens, error) {
	var tokens oauthsvc.OAuthTokens
	ok, err := m.dataStore.Get(securestorage.KeyClaudeAIOAuth, &tokens)
	if err != nil {
		return nil, nil
	}
	if !ok {
		return nil, nil
	}
	return &tokens, nil
}

func (m *Manager) SaveOAuthTokens(tokens *oauthsvc.OAuthTokens) error {
	if tokens == nil {
		return errors.New("oauth tokens are required")
	}
	return m.dataStore.Set(securestorage.KeyClaudeAIOAuth, tokens)
}

func (m *Manager) GetTrustedDeviceToken() (string, error) {
	var token string
	ok, err := m.dataStore.Get(securestorage.KeyTrustedDeviceToken, &token)
	if err != nil || !ok {
		return "", err
	}
	return token, nil
}

func (m *Manager) BaseAPIURL() string {
	if m == nil || m.oauthConfig == nil {
		return ""
	}
	return m.oauthConfig.BaseAPIURL
}

func (m *Manager) SaveTrustedDeviceToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return m.dataStore.DeleteKey(securestorage.KeyTrustedDeviceToken)
	}
	return m.dataStore.Set(securestorage.KeyTrustedDeviceToken, token)
}

func (m *Manager) ClearTrustedDeviceToken() error {
	return m.SaveTrustedDeviceToken("")
}

func (m *Manager) EnrollTrustedDevice(ctx context.Context) error {
	accessToken, err := m.GetAccessToken(ctx)
	if err != nil {
		return err
	}
	token, err := EnrollTrustedDevice(ctx, m.httpClient, m.oauthConfig.BaseAPIURL, accessToken, runtime.GOOS, hostname())
	if err != nil {
		return err
	}
	return m.SaveTrustedDeviceToken(token)
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "unknown-host"
	}
	return name
}

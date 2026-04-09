package oauth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// AuthCodeListener is a temporary localhost HTTP server that listens for OAuth redirects
type AuthCodeListener struct {
	server           *http.Server
	listener         net.Listener
	port             int
	callbackPath     string
	expectedState    string
	pendingResponse  http.ResponseWriter
	authCodeChan     chan string
	errorChan        chan error
	mu               sync.Mutex
	closed           bool
}

// NewAuthCodeListener creates a new auth code listener
func NewAuthCodeListener(callbackPath string) *AuthCodeListener {
	if callbackPath == "" {
		callbackPath = "/callback"
	}

	return &AuthCodeListener{
		callbackPath: callbackPath,
		authCodeChan: make(chan string, 1),
		errorChan:    make(chan error, 1),
	}
}

// Start starts the listener on the specified port (0 for OS-assigned)
func (l *AuthCodeListener) Start(port int) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.listener != nil {
		return l.port, nil
	}

	// Create listener
	addr := fmt.Sprintf("localhost:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("failed to start OAuth callback server: %w", err)
	}

	l.listener = listener
	l.port = listener.Addr().(*net.TCPAddr).Port

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc(l.callbackPath, l.handleCallback)

	l.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start server in background
	go func() {
		if err := l.server.Serve(l.listener); err != nil && err != http.ErrServerClosed {
			l.errorChan <- err
		}
	}()

	return l.port, nil
}

// GetPort returns the port the listener is running on
func (l *AuthCodeListener) GetPort() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.port
}

// HasPendingResponse checks if there's a pending HTTP response
func (l *AuthCodeListener) HasPendingResponse() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pendingResponse != nil
}

// WaitForAuthorization waits for the authorization code
func (l *AuthCodeListener) WaitForAuthorization(ctx context.Context, state string, onReady func() error) (string, error) {
	l.mu.Lock()
	l.expectedState = state
	l.mu.Unlock()

	// Call onReady callback
	if err := onReady(); err != nil {
		return "", err
	}

	// Wait for auth code or error
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case authCode := <-l.authCodeChan:
		return authCode, nil
	case err := <-l.errorChan:
		return "", err
	}
}

// handleCallback handles the OAuth callback request
func (l *AuthCodeListener) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()
	authCode := query.Get("code")
	state := query.Get("state")

	l.mu.Lock()
	expectedState := l.expectedState
	l.mu.Unlock()

	// Validate auth code
	if authCode == "" {
		http.Error(w, "Authorization code not found", http.StatusBadRequest)
		l.errorChan <- fmt.Errorf("no authorization code received")
		return
	}

	// Validate state
	if state != expectedState {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		l.errorChan <- fmt.Errorf("invalid state parameter")
		return
	}

	// Store pending response for later redirect
	l.mu.Lock()
	l.pendingResponse = w
	l.mu.Unlock()

	// Send auth code
	select {
	case l.authCodeChan <- authCode:
	default:
		// Channel already has a value
	}
}

// HandleSuccessRedirect redirects the browser to the success page
func (l *AuthCodeListener) HandleSuccessRedirect(successURL string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.pendingResponse == nil {
		return
	}

	// Redirect to success page
	http.Redirect(l.pendingResponse, &http.Request{}, successURL, http.StatusFound)
	l.pendingResponse = nil
}

// HandleErrorRedirect redirects the browser to the error page
func (l *AuthCodeListener) HandleErrorRedirect(errorURL string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.pendingResponse == nil {
		return
	}

	// Redirect to error page
	http.Redirect(l.pendingResponse, &http.Request{}, errorURL, http.StatusFound)
	l.pendingResponse = nil
}

// Close closes the listener and server
func (l *AuthCodeListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	l.closed = true

	// Send error redirect if there's a pending response
	if l.pendingResponse != nil {
		// Write a simple response since we can't redirect without a request
		l.pendingResponse.WriteHeader(http.StatusOK)
		io.WriteString(l.pendingResponse, "You can close this window")
		l.pendingResponse = nil
	}

	// Close server
	if l.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.server.Shutdown(ctx)
	}

	// Close listener
	if l.listener != nil {
		l.listener.Close()
	}

	return nil
}

// BuildAuthURL builds the OAuth authorization URL
func BuildAuthURL(config *OAuthConfig, opts *AuthURLOptions) string {
	var authURLBase string
	if opts.LoginWithClaudeAI {
		authURLBase = config.ClaudeAIAuthorizeURL
	} else {
		authURLBase = config.ConsoleAuthorizeURL
	}

	authURL, _ := url.Parse(authURLBase)
	query := authURL.Query()

	query.Set("code", "true")
	query.Set("client_id", config.ClientID)
	query.Set("response_type", "code")

	// Set redirect URI
	if opts.IsManual {
		query.Set("redirect_uri", config.ManualRedirectURL)
	} else {
		query.Set("redirect_uri", fmt.Sprintf("http://localhost:%d/callback", opts.Port))
	}

	// Set scopes
	var scopes []string
	if opts.InferenceOnly {
		scopes = []string{ClaudeAIInferenceScope}
	} else {
		scopes = AllOAuthScopes
	}
	query.Set("scope", joinScopes(scopes))

	// PKCE parameters
	query.Set("code_challenge", opts.CodeChallenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", opts.State)

	// Optional parameters
	if opts.OrgUUID != "" {
		query.Set("orgUUID", opts.OrgUUID)
	}
	if opts.LoginHint != "" {
		query.Set("login_hint", opts.LoginHint)
	}
	if opts.LoginMethod != "" {
		query.Set("login_method", opts.LoginMethod)
	}

	authURL.RawQuery = query.Encode()
	return authURL.String()
}

// joinScopes joins scope strings with space
func joinScopes(scopes []string) string {
	result := ""
	for i, scope := range scopes {
		if i > 0 {
			result += " "
		}
		result += scope
	}
	return result
}

// ParseScopes parses a space-separated scope string
func ParseScopes(scopeString string) []string {
	if scopeString == "" {
		return []string{}
	}

	scopes := []string{}
	current := ""
	for _, ch := range scopeString {
		if ch == ' ' {
			if current != "" {
				scopes = append(scopes, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		scopes = append(scopes, current)
	}
	return scopes
}

// ShouldUseClaudeAIAuth checks if Claude AI auth should be used based on scopes
func ShouldUseClaudeAIAuth(scopes []string) bool {
	for _, scope := range scopes {
		if scope == ClaudeAIInferenceScope {
			return true
		}
	}
	return false
}

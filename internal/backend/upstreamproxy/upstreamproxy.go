package upstreamproxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var globalState = &State{Enabled: false}

// InitUpstreamProxy initializes the upstreamproxy system.
// Called once from init.ts. Safe to call when the feature is off
// or the token file is absent — returns {enabled: false}.
func InitUpstreamProxy(ctx context.Context, opts *InitOptions) (*State, error) {
	if opts == nil {
		opts = &InitOptions{
			TokenPath:    SessionTokenPath,
			SystemCAPath: SystemCABundle,
		}
	}

	// Check if running in CCR remote session
	if os.Getenv("CLAUDE_CODE_REMOTE") != "true" {
		return globalState, nil
	}

	// Check if upstreamproxy is enabled
	if os.Getenv("CCR_UPSTREAM_PROXY_ENABLED") != "true" {
		return globalState, nil
	}

	sessionID := os.Getenv("CLAUDE_CODE_REMOTE_SESSION_ID")
	if sessionID == "" {
		logWarning("[upstreamproxy] CLAUDE_CODE_REMOTE_SESSION_ID unset; proxy disabled")
		return globalState, nil
	}

	// Read session token
	token, err := readToken(opts.TokenPath)
	if err != nil || token == "" {
		logDebug("[upstreamproxy] no session token file; proxy disabled")
		return globalState, nil
	}

	// Set non-dumpable (prevent ptrace)
	setNonDumpable()

	// Get base URL
	baseURL := opts.CCRBaseURL
	if baseURL == "" {
		baseURL = os.Getenv("ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com"
		}
	}

	// Set CA bundle path
	caBundlePath := opts.CABundlePath
	if caBundlePath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			logWarning(fmt.Sprintf("[upstreamproxy] failed to get home dir: %v; proxy disabled", err))
			return globalState, nil
		}
		caBundlePath = filepath.Join(homeDir, ".ccr", "ca-bundle.crt")
	}

	// Download CA bundle
	systemCAPath := opts.SystemCAPath
	if systemCAPath == "" {
		systemCAPath = SystemCABundle
	}

	if err := downloadCABundle(ctx, baseURL, systemCAPath, caBundlePath); err != nil {
		logWarning(fmt.Sprintf("[upstreamproxy] CA bundle download failed: %v; proxy disabled", err))
		return globalState, nil
	}

	// Start relay
	wsURL := strings.Replace(baseURL, "http", "ws", 1) + "/v1/code/upstreamproxy/ws"
	relay, err := StartUpstreamProxyRelay(ctx, &RelayOptions{
		WSUrl:     wsURL,
		SessionID: sessionID,
		Token:     token,
	})
	if err != nil {
		logWarning(fmt.Sprintf("[upstreamproxy] relay start failed: %v; proxy disabled", err))
		return globalState, nil
	}

	// Update global state
	globalState = &State{
		Enabled:      true,
		Port:         relay.Port,
		CABundlePath: caBundlePath,
	}

	logDebug(fmt.Sprintf("[upstreamproxy] enabled on 127.0.0.1:%d", relay.Port))

	// Unlink token file (only after relay is up)
	if err := os.Remove(opts.TokenPath); err != nil {
		logWarning("[upstreamproxy] token file unlink failed")
	}

	return globalState, nil
}

// GetProxyEnv returns environment variables for agent subprocesses.
// Empty when the proxy is disabled.
func GetProxyEnv() map[string]string {
	if !globalState.Enabled {
		return map[string]string{}
	}

	proxyURL := fmt.Sprintf("http://127.0.0.1:%d", globalState.Port)
	noProxy := strings.Join(NoProxyList, ",")

	return map[string]string{
		"HTTPS_PROXY":   proxyURL,
		"https_proxy":   proxyURL,
		"SSL_CERT_FILE": globalState.CABundlePath,
		"NO_PROXY":      noProxy,
		"no_proxy":      noProxy,
	}
}

// IsEnabled returns whether upstreamproxy is enabled
func IsEnabled() bool {
	return globalState.Enabled
}

// GetState returns the current upstreamproxy state
func GetState() *State {
	return globalState
}

// readToken reads the session token from the file
func readToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// setNonDumpable sets PR_SET_DUMPABLE to 0 to block ptrace
func setNonDumpable() {
	// This is a platform-specific operation
	// On Linux, we would use prctl(PR_SET_DUMPABLE, 0)
	// For now, this is a no-op placeholder
	// TODO: Implement using syscall on Linux
	logDebug("[upstreamproxy] setNonDumpable not implemented on this platform")
}

// downloadCABundle downloads the CA certificate and concatenates it with the system bundle
func downloadCABundle(ctx context.Context, baseURL, systemCAPath, outPath string) error {
	// TODO: Implement CA bundle download
	// This requires HTTP client with timeout
	logDebug("[upstreamproxy] downloadCABundle not yet implemented")
	return nil
}

// Logging helpers
func logDebug(msg string) {
	// TODO: Integrate with proper logging system
	fmt.Fprintln(os.Stderr, msg)
}

func logWarning(msg string) {
	// TODO: Integrate with proper logging system
	fmt.Fprintln(os.Stderr, msg)
}

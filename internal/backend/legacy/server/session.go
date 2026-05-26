package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"claude-codex/internal/backend/httpclient"
)

// DirectConnectError represents errors from direct connect operations
type DirectConnectError struct {
	Message string
}

func (e *DirectConnectError) Error() string {
	return e.Message
}

// CreateSessionRequest represents the request to create a session
type CreateSessionRequest struct {
	CWD                        string `json:"cwd"`
	DangerouslySkipPermissions bool   `json:"dangerously_skip_permissions,omitempty"`
}

// CreateDirectConnectSession creates a session on a direct-connect server
// Posts to ${serverUrl}/sessions, validates the response, and returns
// a DirectConnectConfig ready for use
func CreateDirectConnectSession(serverURL, authToken, cwd string, dangerouslySkipPermissions bool) (*DirectConnectConfig, string, error) {
	reqBody := CreateSessionRequest{
		CWD:                        cwd,
		DangerouslySkipPermissions: dangerouslySkipPermissions,
	}

	headers := make(http.Header)
	if authToken != "" {
		headers.Set("Authorization", "Bearer "+authToken)
	}

	var result ConnectResponse
	err := httpclient.New(httpclient.WithComponent("direct_connect")).JSON(context.Background(), http.MethodPost, serverURL+"/sessions", reqBody, &result, httpclient.WithHeaders(headers))
	if err != nil {
		var statusErr *httpclient.StatusError
		if errors.As(err, &statusErr) {
			return nil, "", &DirectConnectError{
				Message: fmt.Sprintf("Failed to create session: %d %s", statusErr.StatusCode, statusErr.Status),
			}
		}
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Failed to connect to server at %s: %v", serverURL, err),
		}
	}

	config := &DirectConnectConfig{
		ServerURL: serverURL,
		SessionID: result.SessionID,
		WsURL:     result.WsURL,
		AuthToken: authToken,
	}

	workDir := ""
	if result.WorkDir != nil {
		workDir = *result.WorkDir
	}

	return config, workDir, nil
}

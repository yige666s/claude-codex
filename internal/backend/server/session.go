package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	CWD                         string `json:"cwd"`
	DangerouslySkipPermissions bool   `json:"dangerously_skip_permissions,omitempty"`
}

// CreateDirectConnectSession creates a session on a direct-connect server
// Posts to ${serverUrl}/sessions, validates the response, and returns
// a DirectConnectConfig ready for use
func CreateDirectConnectSession(serverURL, authToken, cwd string, dangerouslySkipPermissions bool) (*DirectConnectConfig, string, error) {
	reqBody := CreateSessionRequest{
		CWD:                         cwd,
		DangerouslySkipPermissions: dangerouslySkipPermissions,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Failed to marshal request: %v", err),
		}
	}

	req, err := http.NewRequest("POST", serverURL+"/sessions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Failed to create request: %v", err),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Failed to connect to server at %s: %v", serverURL, err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Failed to create session: %d %s", resp.StatusCode, resp.Status),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Failed to read response: %v", err),
		}
	}

	var result ConnectResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, "", &DirectConnectError{
			Message: fmt.Sprintf("Invalid session response: %v", err),
		}
	}

	config := &DirectConnectConfig{
		ServerURL:  serverURL,
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

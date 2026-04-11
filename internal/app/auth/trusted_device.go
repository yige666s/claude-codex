package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type trustedDeviceResponse struct {
	DeviceToken string `json:"device_token"`
	DeviceID    string `json:"device_id,omitempty"`
}

func EnrollTrustedDevice(ctx context.Context, client *http.Client, baseAPIURL, accessToken, platform, host string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("access token is required")
	}
	if client == nil {
		client = http.DefaultClient
	}

	body, err := json.Marshal(map[string]string{
		"display_name": fmt.Sprintf("Claude Go on %s · %s", host, platform),
	})
	if err != nil {
		return "", err
	}

	url := strings.TrimRight(strings.TrimSpace(baseAPIURL), "/") + "/api/auth/trusted_devices"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("trusted device enrollment failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	var decoded trustedDeviceResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", err
	}
	if strings.TrimSpace(decoded.DeviceToken) == "" {
		return "", fmt.Errorf("trusted device enrollment response missing device_token")
	}
	return decoded.DeviceToken, nil
}

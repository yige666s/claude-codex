package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	officialURLs   map[string]struct{}
	officialURLsMu sync.RWMutex
)

func PrefetchOfficialMCPURLs(client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.anthropic.com/mcp-registry/v0/servers?version=latest&visibility=commercial", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var payload struct {
		Servers []struct {
			Server struct {
				Remotes []struct {
					URL string `json:"url"`
				} `json:"remotes"`
			} `json:"server"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	urls := make(map[string]struct{})
	for _, server := range payload.Servers {
		for _, remote := range server.Server.Remotes {
			if normalized := normalizeOfficialURL(remote.URL); normalized != "" {
				urls[normalized] = struct{}{}
			}
		}
	}
	officialURLsMu.Lock()
	officialURLs = urls
	officialURLsMu.Unlock()
	return nil
}

func IsOfficialMCPURL(normalizedURL string) bool {
	officialURLsMu.RLock()
	defer officialURLsMu.RUnlock()
	_, ok := officialURLs[normalizedURL]
	return ok
}

func ResetOfficialMCPURLsForTesting() {
	officialURLsMu.Lock()
	defer officialURLsMu.Unlock()
	officialURLs = nil
}

func normalizeOfficialURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimRight(value, "/")
	if idx := strings.Index(value, "?"); idx >= 0 {
		value = value[:idx]
	}
	return value
}

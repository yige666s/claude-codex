package settingssync

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	"claude-codex/internal/app/settings"
)

const endpointPath = "/api/claude_code/user_settings"

var SyncKeys = struct {
	UserSettings string
	UserMemory   string
}{
	UserSettings: "~/.claude/settings.json",
	UserMemory:   "~/.claude/CLAUDE.md",
}

func ProjectSettingsKey(projectID string) string {
	return "projects/" + projectID + "/.claude/settings.local.json"
}
func ProjectMemoryKey(projectID string) string { return "projects/" + projectID + "/CLAUDE.local.md" }

type UserSyncData struct {
	UserID       string          `json:"userId"`
	Version      int             `json:"version"`
	LastModified string          `json:"lastModified"`
	Checksum     string          `json:"checksum"`
	Content      UserSyncContent `json:"content"`
}

type UserSyncContent struct {
	Entries map[string]string `json:"entries"`
}

type FetchResult struct {
	Success   bool
	Data      *UserSyncData
	IsEmpty   bool
	Error     string
	SkipRetry bool
}

type UploadResult struct {
	Success      bool
	Checksum     string
	LastModified string
	Error        string
}

type Service struct {
	auth       *appauth.Manager
	httpClient *http.Client
	workingDir string
}

func New(cfg config.Config, workingDir string) (*Service, error) {
	manager, err := appauth.NewManager(cfg, nil)
	if err != nil {
		return nil, err
	}
	return &Service{
		auth:       manager,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		workingDir: workingDir,
	}, nil
}

func (s *Service) Fetch(ctx context.Context) FetchResult {
	token, err := s.auth.GetAccessToken(ctx)
	if err != nil {
		return FetchResult{Success: false, Error: "No OAuth token available", SkipRetry: true}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.auth.BaseAPIURL(), "/")+endpointPath, nil)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-codex")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return FetchResult{Success: false, Error: err.Error()}
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return FetchResult{Success: true, IsEmpty: true}
	case http.StatusOK:
	default:
		return FetchResult{Success: false, Error: fmt.Sprintf("settings sync fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	var data UserSyncData
	if err := json.Unmarshal(body, &data); err != nil {
		return FetchResult{Success: false, Error: "invalid settings sync response"}
	}
	return FetchResult{Success: true, Data: &data}
}

func (s *Service) Upload(ctx context.Context, entries map[string]string) UploadResult {
	token, err := s.auth.GetAccessToken(ctx)
	if err != nil {
		return UploadResult{Success: false, Error: "No OAuth token available"}
	}
	payload := map[string]any{"entries": entries}
	body, err := json.Marshal(payload)
	if err != nil {
		return UploadResult{Success: false, Error: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.auth.BaseAPIURL(), "/")+endpointPath, bytes.NewReader(body))
	if err != nil {
		return UploadResult{Success: false, Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-codex")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return UploadResult{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return UploadResult{Success: false, Error: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UploadResult{Success: false, Error: fmt.Sprintf("settings sync upload failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))}
	}
	var parsed struct {
		Checksum     string `json:"checksum"`
		LastModified string `json:"lastModified"`
	}
	_ = json.Unmarshal(respBody, &parsed)
	if parsed.Checksum == "" {
		parsed.Checksum = checksum(entries)
	}
	return UploadResult{Success: true, Checksum: parsed.Checksum, LastModified: parsed.LastModified}
}

func (s *Service) BuildEntries() (map[string]string, error) {
	projectID := settings.ProjectID(s.workingDir)
	entries := make(map[string]string)
	if value, ok := readIfExists(filepath.Join(settings.ClaudeConfigHomeDir(), "settings.json")); ok {
		entries[SyncKeys.UserSettings] = value
	}
	if value, ok := readIfExists(filepath.Join(settings.ClaudeConfigHomeDir(), "CLAUDE.md")); ok {
		entries[SyncKeys.UserMemory] = value
	}
	if value, ok := readIfExists(filepath.Join(s.workingDir, ".claude", "settings.local.json")); ok {
		entries[ProjectSettingsKey(projectID)] = value
	}
	if value, ok := readIfExists(filepath.Join(s.workingDir, "CLAUDE.local.md")); ok {
		entries[ProjectMemoryKey(projectID)] = value
	}
	return entries, nil
}

func (s *Service) ApplyEntries(entries map[string]string) error {
	projectID := settings.ProjectID(s.workingDir)
	for key, value := range entries {
		var path string
		switch key {
		case SyncKeys.UserSettings:
			path = filepath.Join(settings.ClaudeConfigHomeDir(), "settings.json")
		case SyncKeys.UserMemory:
			path = filepath.Join(settings.ClaudeConfigHomeDir(), "CLAUDE.md")
		case ProjectSettingsKey(projectID):
			path = filepath.Join(s.workingDir, ".claude", "settings.local.json")
		case ProjectMemoryKey(projectID):
			path = filepath.Join(s.workingDir, "CLAUDE.local.md")
		default:
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func readIfExists(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func checksum(entries map[string]string) string {
	payload, _ := json.Marshal(entries)
	sum := md5.Sum(payload)
	return hex.EncodeToString(sum[:])
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

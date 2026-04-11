package settingssync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appauth "claude-codex/internal/app/auth"
	"claude-codex/internal/app/config"
	"claude-codex/internal/app/securestorage"
	oauthsvc "claude-codex/internal/backend/services/oauth"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

type fakeStore struct {
	data securestorage.Data
}

func (s *fakeStore) Name() string                      { return "fake" }
func (s *fakeStore) Read() (securestorage.Data, error) { return s.data, nil }
func (s *fakeStore) Write(data securestorage.Data) (securestorage.WriteResult, error) {
	s.data = data
	return securestorage.WriteResult{}, nil
}
func (s *fakeStore) Delete() error { s.data = securestorage.Data{}; return nil }

func TestBuildEntriesAndApplyEntries(t *testing.T) {
	workingDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(workingDir, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"verbose":true}`), 0o644)
	os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("user memory"), 0o644)
	os.WriteFile(filepath.Join(workingDir, ".claude", "settings.local.json"), []byte(`{"fastMode":true}`), 0o644)
	os.WriteFile(filepath.Join(workingDir, "CLAUDE.local.md"), []byte("project memory"), 0o644)

	service, err := New(config.Default(), workingDir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	entries, err := service.BuildEntries()
	if err != nil {
		t.Fatalf("BuildEntries: %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %#v", entries)
	}

	delete(entries, SyncKeys.UserSettings)
	entries[SyncKeys.UserSettings] = `{"verbose":false}`
	if err := service.ApplyEntries(entries); err != nil {
		t.Fatalf("ApplyEntries: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if string(data) != `{"verbose":false}` {
		t.Fatalf("unexpected applied user settings: %s", string(data))
	}
}

func TestFetchAndUpload(t *testing.T) {
	store := &fakeStore{}
	manager, err := appauth.NewManager(config.Default(), store)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := manager.SaveOAuthTokens(&oauthsvc.OAuthTokens{
		AccessToken:  "oauth-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		Scopes:       []string{oauthsvc.ProfileScope},
	}); err != nil {
		t.Fatalf("SaveOAuthTokens: %v", err)
	}
	service := &Service{
		auth:       manager,
		httpClient: &http.Client{},
		workingDir: t.TempDir(),
	}
	service.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		if r.Method == http.MethodGet {
			body, _ := json.Marshal(UserSyncData{
				UserID:       "u1",
				Version:      1,
				LastModified: "2025-01-01T00:00:00Z",
				Checksum:     "sum1",
				Content:      UserSyncContent{Entries: map[string]string{SyncKeys.UserSettings: `{"verbose":true}`}},
			})
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"checksum":"sum2","lastModified":"2025-01-02T00:00:00Z"}`)), Header: make(http.Header)}, nil
	})

	fetch := service.Fetch(context.Background())
	if !fetch.Success || fetch.Data == nil || fetch.Data.Content.Entries[SyncKeys.UserSettings] == "" {
		t.Fatalf("unexpected fetch result: %+v", fetch)
	}
	upload := service.Upload(context.Background(), map[string]string{SyncKeys.UserSettings: `{"verbose":true}`})
	if !upload.Success || upload.Checksum != "sum2" {
		t.Fatalf("unexpected upload result: %+v", upload)
	}
}

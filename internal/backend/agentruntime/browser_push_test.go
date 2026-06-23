package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMemoryBrowserPushStoreUpsertDeduplicatesEndpoint(t *testing.T) {
	store := NewMemoryBrowserPushStore()
	ctx := context.Background()
	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	input := BrowserPushSubscriptionInput{
		Endpoint: "https://push.example/subscription",
		Keys: BrowserPushKeys{
			P256DH: "p256dh",
			Auth:   "auth",
		},
		UserAgent: "Chrome",
	}

	first, err := store.UpsertBrowserPushSubscription(ctx, "alice", input, now)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	input.Keys.Auth = "rotated"
	second, err := store.UpsertBrowserPushSubscription(ctx, "alice", input, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("subscription id changed: %s -> %s", first.ID, second.ID)
	}
	list, err := store.ListEnabledBrowserPushSubscriptions(ctx, "alice", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].AuthSecret != "rotated" {
		t.Fatalf("unexpected subscriptions: %#v", list)
	}
}

func TestBrowserPushConfigRouteReportsDisabledWithoutVAPID(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, nil)
	runtime.SetBrowserPushSender(NewBrowserPushSender(BrowserPushConfig{}))
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/browser-push/config", nil)
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body BrowserPushPublicConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enabled || body.PublicKey != "" {
		t.Fatalf("unexpected config: %#v", body)
	}
}

func TestBrowserPushSubscribeRouteStoresSubscription(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, NewFileSessionStore(t.TempDir()), nil, nil, nil)
	store := NewMemoryBrowserPushStore()
	runtime.SetBrowserPushStore(store)
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/browser-push/subscriptions", strings.NewReader(`{
		"endpoint": "https://push.example/subscription",
		"keys": {"p256dh": "p256dh", "auth": "auth"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	list, err := store.ListEnabledBrowserPushSubscriptions(context.Background(), "alice", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("subscriptions = %d, want 1", len(list))
	}
}

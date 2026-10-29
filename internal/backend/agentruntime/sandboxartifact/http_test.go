package sandboxartifact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-codex/internal/backend/agentruntime/sandboxtransfer"
)

func TestAuthenticatedFetchAndCache(t *testing.T) {
	store, skill := fixture(t)
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("x", 32)
	handler, err := Handler(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	max := int64(sandboxtransfer.HardMaxBytes + sandboxtransfer.HardMaxFiles*4096 + 2048)
	fetcher, err := NewHTTPFetcher(server.URL, token, max, nil)
	if err != nil {
		t.Fatal(err)
	}
	cache := Cache{Dir: t.TempDir(), Limits: sandboxtransfer.DevLimits(), Source: fetcher}
	if _, err := cache.Ensure(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	bad, err := NewHTTPFetcher(server.URL, strings.Repeat("y", 32), max, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Fetch(context.Background(), ref); err == nil {
		t.Fatal("unauthenticated fetch succeeded")
	}
	if _, err := NewHTTPFetcher("http://example.com", token, max, nil); err == nil {
		t.Fatal("remote plain HTTP accepted")
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+r.URL.Path, http.StatusFound)
	}))
	defer redirect.Close()
	noRedirect, err := NewHTTPFetcher(redirect.URL, token, max, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noRedirect.Fetch(context.Background(), ref); err == nil {
		t.Fatal("redirect leaked credential")
	}
}

func TestUnpublishedSkillVersionCannotBeDownloaded(t *testing.T) {
	store, skill := fixture(t)
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("z", 32)
	handler, err := Handler(store, token)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	max := int64(sandboxtransfer.HardMaxBytes + sandboxtransfer.HardMaxFiles*4096 + 2048)
	fetch, err := NewHTTPFetcher(server.URL, token, max, nil)
	if err != nil {
		t.Fatal(err)
	}
	ref.ID = "unpublished"
	if _, err := fetch.Fetch(context.Background(), ref); err == nil {
		t.Fatal("unbound id received published bytes")
	}
}

func TestInternalArtifactSourcePrefix(t *testing.T) {
	store, skill := fixture(t)
	ref, err := store.Publish(context.Background(), "demo", skill)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("p", 32)
	handler, err := Handler(store, key)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/internal/sandbox/v1/skills/", http.StripPrefix("/v1/internal/sandbox", handler))
	server := httptest.NewServer(mux)
	defer server.Close()
	fetch, err := NewHTTPFetcher(server.URL+"/v1/internal/sandbox", key, 16<<20, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetch.Fetch(context.Background(), ref)
	if err != nil || Digest(got) != ref.Digest {
		t.Fatalf("prefixed source failed: %v", err)
	}
	if _, err := NewHTTPFetcher(server.URL+"/other", key, 16<<20, nil); err == nil {
		t.Fatal("arbitrary prefix accepted")
	}
}

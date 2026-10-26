package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-codex/internal/harness/state"
)

func TestSeedBuiltinPromptsPopulatesAdminCatalogWithoutOverwritingEdits(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPromptStore()
	custom := PromptTemplate{ID: PromptIDRuntimeChatBaseBehavior, Name: "Edited by admin", Scope: "custom"}
	if _, err := store.UpsertPrompt(ctx, custom); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: custom.ID, Version: "admin-v2", Content: "custom behavior", Status: PromptStatusPublished}); err != nil {
		t.Fatal(err)
	}
	seeded, err := SeedBuiltinPrompts(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if seeded != len(BuiltinSystemPromptBaselines())-1 {
		t.Fatalf("seeded=%d want=%d", seeded, len(BuiltinSystemPromptBaselines())-1)
	}
	catalog, err := store.ListPrompts(ctx, PromptListFilter{Limit: 300})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != len(BuiltinSystemPromptBaselines()) {
		t.Fatalf("admin catalog has %d, want %d", len(catalog), len(BuiltinSystemPromptBaselines()))
	}
	for _, id := range []string{PromptIDRuntimeMultiAgentCoordinator, PromptIDRuntimeMultiAgentRoute} {
		version, err := store.GetPublishedPromptVersion(ctx, id)
		if err != nil || version.Content == "" {
			t.Fatalf("missing published prompt %s: %#v %v", id, version, err)
		}
	}
	if found, err := store.GetPrompt(ctx, custom.ID); err != nil || found.Name != custom.Name {
		t.Fatalf("custom prompt replaced: %#v %v", found, err)
	}
	if version, err := store.GetPublishedPromptVersion(ctx, custom.ID); err != nil || version.Content != "custom behavior" {
		t.Fatalf("custom version replaced: %#v %v", version, err)
	}
	again, err := SeedBuiltinPrompts(ctx, store)
	if err != nil || again != 0 {
		t.Fatalf("second seed changed prompts: count=%d err=%v", again, err)
	}
}

func TestMultiAgentCoordinatorUsesPublishedAdminPrompt(t *testing.T) {
	store := NewMemoryPromptStore()
	ctx := context.Background()
	_, _ = store.UpsertPrompt(ctx, PromptTemplate{ID: PromptIDRuntimeMultiAgentCoordinator, Name: "Coordinator"})
	_, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: PromptIDRuntimeMultiAgentCoordinator, Version: "admin-v2", Status: PromptStatusPublished, Content: "Admin coordinator: delegate only independent tasks."})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := (SystemPromptAssembler{Resolver: NewPromptResolver(store, nil), Environment: PromptEnvironmentProduction}).BuildChatSnapshot(ctx, ChatSystemPromptInput{Session: state.NewSession(""), MultiAgent: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Content, "Admin coordinator: delegate only independent tasks.") || strings.Contains(snapshot.Content, PromptMultiAgentCoordinator) {
		t.Fatalf("coordinator did not resolve published version: %s", snapshot.Content)
	}
	found := false
	for _, segment := range snapshot.Segments {
		if segment.PromptID == PromptIDRuntimeMultiAgentCoordinator {
			found = segment.Version == "admin-v2" && !segment.Fallback
		}
	}
	if !found {
		t.Fatalf("admin prompt provenance missing: %#v", snapshot.Segments)
	}
	fallback, err := (SystemPromptAssembler{Environment: PromptEnvironmentProduction}).BuildChatSnapshot(ctx, ChatSystemPromptInput{Session: state.NewSession(""), MultiAgent: true})
	if err != nil || !strings.Contains(fallback.Content, PromptMultiAgentCoordinator) {
		t.Fatalf("fallback missing: %v %s", err, fallback.Content)
	}
}

func TestWebSearchAndFailureRecoveryUsePublishedAdminPrompts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPromptStore()
	for _, entry := range []struct{ id, text string }{
		{PromptIDRuntimeChatWebSearch, "Admin search: {{user_request}}"},
		{PromptIDRuntimeFailureRecovery, "Admin recovery: %s / %s"},
	} {
		if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: entry.id, Name: entry.id}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: entry.id, Version: "admin-v2", Status: PromptStatusPublished, Content: entry.text}); err != nil {
			t.Fatal(err)
		}
	}
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	runtime.SetPromptStore(store)
	search, err := runtime.renderManagedPrompt(ctx, PromptIDRuntimeChatWebSearch, "alice", "session", "chat", map[string]any{"user_request": "latest databases"})
	if err != nil || search.Content != "Admin search: latest databases" || search.PromptVersion != "admin-v2" {
		t.Fatalf("search=%#v err=%v", search, err)
	}
	recovery := renderRuntimeDeepAgentPrompt(ctx, runtime, PromptIDRuntimeFailureRecovery, "alice", "session", "my request", "tool error")
	if recovery.Content != "Admin recovery: my request / tool error" || recovery.Metadata.PromptVersion != "admin-v2" {
		t.Fatalf("recovery=%#v", recovery)
	}
}

func TestAdminPromptPageListsSeededMultiAgentPrompts(t *testing.T) {
	store := NewMemoryPromptStore()
	if _, err := SeedBuiltinPrompts(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	server := NewServer(NewRuntime(RuntimeConfig{}, nil, nil, nil, nil), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetPromptStore(store)
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/prompts?scope=runtime/multi_agent&limit=300", nil)
	req.Header.Set("X-User-ID", "admin")
	req.Header.Set("X-Admin-Token", "secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Prompts []PromptTemplate `json:"prompts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Prompts) != 2 {
		t.Fatalf("multi-agent prompts not listed: %#v", payload.Prompts)
	}
	ids := map[string]bool{}
	for _, prompt := range payload.Prompts {
		ids[prompt.ID] = true
	}
	if !ids[PromptIDRuntimeMultiAgentRoute] || !ids[PromptIDRuntimeMultiAgentCoordinator] {
		t.Fatalf("missing route/coordinator: %#v", ids)
	}
}

func TestChatWebSearchExecutesPublishedAdminPrompt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	capture := &captureContentRunner{}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return capture })
	store := NewMemoryPromptStore()
	if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: PromptIDRuntimeChatWebSearch, Name: "Web Search"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: PromptIDRuntimeChatWebSearch, Version: "admin-v2", Status: PromptStatusPublished, Content: "Admin web instruction: {{user_request}}"}); err != nil {
		t.Fatal(err)
	}
	runtime.SetPromptStore(store)
	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Chat(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "Compare releases", AgentMode: AgentModeWebSearch}, &collectSink{}); err != nil {
		t.Fatal(err)
	}
	if capture.prompt != "Admin web instruction: Compare releases" {
		t.Fatalf("runner prompt=%q", capture.prompt)
	}
}

func TestAssetAndImageMemoryUsePublishedAdminPrompts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPromptStore()
	for _, entry := range []struct{ id, instruction string }{
		{PromptIDAssetVisionInsight, "Admin vision insight instruction"},
		{PromptIDMemoryImageExtract, "Admin image memory instruction"},
	} {
		if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: entry.id, Name: entry.id}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreatePromptVersion(ctx, PromptVersion{PromptID: entry.id, Version: "admin-v2", Status: PromptStatusPublished, Content: entry.instruction}); err != nil {
			t.Fatal(err)
		}
	}
	capture := &captureContentRunner{}
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, func(Scope) Runner { return capture })
	runtime.SetPromptStore(store)
	asset := &Artifact{UserID: "alice", SessionID: "session", ContentType: "image/png", Filename: "picture.png"}
	if _, err := runtime.runVisionAssetInsight(ctx, asset, "image/png", []byte("bytes")); err != nil {
		t.Fatal(err)
	}
	if len(capture.blocks) == 0 || capture.blocks[0].Text != "Admin vision insight instruction" {
		t.Fatalf("vision prompt=%#v", capture.blocks)
	}
	// This runner does not return extraction JSON, but the call still must send
	// the published instruction instead of the hard-coded default.
	_, _ = runtime.extractImageMemoryCandidates(ctx, "alice", asset, []byte("bytes"))
	if len(capture.blocks) == 0 || capture.blocks[0].Text != "Admin image memory instruction" {
		t.Fatalf("image memory prompt=%#v", capture.blocks)
	}
}

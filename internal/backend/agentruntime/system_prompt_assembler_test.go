package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

func TestSystemPromptAssemblerBuildsOrderedSnapshot(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryPromptStore()
	if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: PromptIDRuntimeChatBaseBehavior, Name: "Chat Base"}); err != nil {
		t.Fatalf("upsert base prompt: %v", err)
	}
	if _, err := store.CreatePromptVersion(ctx, PromptVersion{
		PromptID: PromptIDRuntimeChatBaseBehavior,
		Version:  "registry-v1",
		Status:   PromptStatusPublished,
		Content:  "<chat-base-behavior>registry base</chat-base-behavior>",
	}); err != nil {
		t.Fatalf("create base prompt version: %v", err)
	}
	session := state.NewSession("")
	session.AddSystemContext("<personalization>\nPrefer concise Chinese answers.\n</personalization>")
	session.AddSystemContext(memoryContextMarker + "\nUser likes trail running.\n</memory>")

	snapshot, err := (SystemPromptAssembler{
		Resolver:    NewPromptResolver(store, nil),
		Environment: PromptEnvironmentProduction,
		RuntimeMode: "chat",
	}).BuildChatSnapshot(ctx, ChatSystemPromptInput{
		UserID:         "alice",
		SessionID:      "session-1",
		Session:        session,
		ConnectorLines: "- GitHub: alice; policy=read_only; evidence=repo; mcp_server=http; mcp_tools=github_search",
		Temporal:       "<temporal-context>\nCurrent date: 2026-07-07\n</temporal-context>",
		Locale:         "<locale-context>\nLocale: zh-CN\n</locale-context>",
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	if snapshot.Hash == "" || !strings.Contains(snapshot.Content, `hash="`+snapshot.Hash+`"`) {
		t.Fatalf("snapshot hash missing from content: %#v", snapshot)
	}
	assertOrderedContains(t, snapshot.Content,
		"registry base",
		"Selected external connector context:",
		"<consumer-security>",
		"<locale-context>",
		"<personalization>",
		"<memory>",
		"<temporal-context>",
	)
	if snapshot.Segments[0].PromptID != PromptIDRuntimeChatBaseBehavior || snapshot.Segments[0].Version != "registry-v1" || snapshot.Segments[0].Fallback {
		t.Fatalf("base segment should use registry version: %#v", snapshot.Segments[0])
	}
	if !strings.Contains(snapshot.Content, `cache="long"`) || !strings.Contains(snapshot.Content, `cache="none"`) {
		t.Fatalf("snapshot should expose segment cache policies: %s", snapshot.Content)
	}
}

func TestSystemPromptAssemblerProductionFallbackWhenRegistryUnavailable(t *testing.T) {
	ctx := context.Background()
	session := state.NewSession("")
	snapshot, err := (SystemPromptAssembler{
		Resolver:    NewPromptResolver(unavailablePromptStore{MemoryPromptStore: NewMemoryPromptStore()}, nil),
		Environment: PromptEnvironmentProduction,
		RuntimeMode: "chat",
	}).BuildChatSnapshot(ctx, ChatSystemPromptInput{
		UserID:    "alice",
		SessionID: "session-1",
		Session:   session,
		Temporal:  "<temporal-context>\nCurrent date: 2026-07-07\n</temporal-context>",
		Locale:    "<locale-context>\nLocale: zh-CN\n</locale-context>",
	})
	if err != nil {
		t.Fatalf("production fallback should succeed: %v", err)
	}
	if !strings.Contains(snapshot.Content, PromptConsumerSecuritySystemContext) || !strings.Contains(snapshot.Content, PromptChatBaseBehavior) {
		t.Fatalf("snapshot should include builtin fallbacks: %s", snapshot.Content)
	}
}

func TestRuntimeChatInjectsSnapshotAndPromptMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runner := &promptMetadataCaptureRunner{}
	runtime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute, Timezone: "Asia/Shanghai", Locale: "zh-CN"},
		NewFileSessionStore(root),
		nil,
		nil,
		func(Scope) Runner { return runner },
	)
	store := NewMemoryPromptStore()
	if _, err := store.UpsertPrompt(ctx, PromptTemplate{ID: PromptIDRuntimeChatConsumerSecurity, Name: "Consumer Security"}); err != nil {
		t.Fatalf("upsert consumer prompt: %v", err)
	}
	if _, err := store.CreatePromptVersion(ctx, PromptVersion{
		PromptID: PromptIDRuntimeChatConsumerSecurity,
		Version:  "registry-safety",
		Status:   PromptStatusPublished,
		Content:  "<consumer-security>registry safety</consumer-security>",
	}); err != nil {
		t.Fatalf("create consumer prompt version: %v", err)
	}
	runtime.SetPromptStore(store)

	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := runtime.Chat(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "hello"}, &collectSink{}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if runner.metadata.PromptID != PromptIDRuntimeChatSystemPromptSnapshot || runner.metadata.PromptVersion != systemPromptSnapshotVersion || runner.metadata.PromptHash == "" {
		t.Fatalf("runner should receive snapshot prompt metadata: %#v", runner.metadata)
	}
	if !messagesContainRuntimeContext(runner.sessionMessages, systemPromptSnapshotMarker) || !messagesContainRuntimeContext(runner.sessionMessages, "registry safety") {
		t.Fatalf("runner session should receive assembled snapshot with registry prompt: %#v", runner.sessionMessages)
	}
	persisted, err := runtime.GetSession(ctx, "alice", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if messagesContainRuntimeContext(persisted.Messages, systemPromptSnapshotMarker) || messagesContainRuntimeContext(persisted.Messages, temporalContextMarker) || messagesContainRuntimeContext(persisted.Messages, localeContextMarker) {
		t.Fatalf("assembled snapshot should not be persisted: %#v", persisted.Messages)
	}
}

type unavailablePromptStore struct {
	*MemoryPromptStore
}

func (s unavailablePromptStore) ListPromptExperiments(context.Context, PromptExperimentFilter) ([]PromptExperiment, error) {
	return nil, errors.New("prompt registry unavailable")
}

type promptMetadataCaptureRunner struct {
	metadata        PromptMetadata
	sessionMessages []state.Message
}

func (r *promptMetadataCaptureRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.metadata = promptMetadataFromContext(ctx)
	r.sessionMessages = append([]state.Message(nil), session.Messages...)
	session.AddUserMessage(prompt)
	session.AddAssistantMessage("ok")
	return engine.Result{Output: "ok", Session: session}, nil
}

func (r *promptMetadataCaptureRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.Run(ctx, session, prompt)
}

func assertOrderedContains(t *testing.T, content string, needles ...string) {
	t.Helper()
	last := -1
	for _, needle := range needles {
		index := strings.Index(content, needle)
		if index < 0 {
			t.Fatalf("content missing %q: %s", needle, content)
		}
		if index <= last {
			t.Fatalf("%q should appear after previous needle in: %s", needle, content)
		}
		last = index
	}
}

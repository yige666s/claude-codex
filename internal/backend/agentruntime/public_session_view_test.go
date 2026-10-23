package agentruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestPublicSessionViewCollapsesIntermediateAgentMessages(t *testing.T) {
	now := time.Now().UTC()
	session := &state.Session{ID: "session-1", Messages: []state.Message{
		{Role: state.MessageRoleUser, Content: "Compare SQLite and PostgreSQL", RunID: "job-1", CreatedAt: now},
		{Role: state.MessageRoleAssistant, Content: "I will start two agents.", RunID: "job-1", ToolCalls: []state.ToolCall{{ID: "call-1", Name: "Agent"}}, CreatedAt: now},
		{Role: "tool", ToolOutput: `{"task_id":"task-1"}`, RunID: "job-1", CreatedAt: now},
		{Role: state.MessageRoleAssistant, Content: "Both agents are running.", RunID: "job-1", ToolCalls: []state.ToolCall{{ID: "call-2", Name: "TaskOutput"}}, StructuredOutputs: []json.RawMessage{json.RawMessage(`{"id":"card-1"}`)}, CreatedAt: now},
		{Role: "tool", ToolOutput: "research result", RunID: "job-1", CreatedAt: now},
		// Older model streams can emit a text-only progress message too.
		{Role: state.MessageRoleAssistant, Content: "I will now compare the findings.", RunID: "job-1", CreatedAt: now},
		{Role: state.MessageRoleAssistant, Content: "Final comparison and recommendation.", RunID: "job-1", CreatedAt: now},
		{Role: state.MessageRoleUser, Content: "What about backups?", RunID: "run-2", CreatedAt: now},
		{Role: state.MessageRoleAssistant, Content: "Backups differ.", RunID: "run-2", CreatedAt: now},
	}}

	public := publicSessionView(session)
	if len(public.Messages) != 4 {
		t.Fatalf("public messages = %#v, want two user requests and two final answers", public.Messages)
	}
	if public.Messages[1].Content != "Final comparison and recommendation." || public.Messages[3].Content != "Backups differ." {
		t.Fatalf("intermediate assistant text leaked to public transcript: %#v", public.Messages)
	}
	if len(public.Messages[1].StructuredOutputs) != 1 || string(public.Messages[1].StructuredOutputs[0]) != `{"id":"card-1"}` {
		t.Fatalf("structured output lost when collapsing run: %#v", public.Messages[1])
	}
	if len(session.Messages) != 9 || session.Messages[1].Hidden || session.Messages[1].Content != "I will start two agents." {
		t.Fatal("public projection changed the internal model transcript")
	}
}

func TestPublicSessionViewOmitsUnfinishedToolCallWithNoAnswer(t *testing.T) {
	session := &state.Session{Messages: []state.Message{
		{Role: state.MessageRoleUser, Content: "Investigate", RunID: "job-1"},
		{Role: state.MessageRoleAssistant, Content: "Starting investigation", RunID: "job-1", ToolCalls: []state.ToolCall{{ID: "call", Name: "Agent"}}},
	}}
	public := publicSessionView(session)
	if len(public.Messages) != 1 || public.Messages[0].Role != state.MessageRoleUser {
		t.Fatalf("unfinished tool call should stay in job activity, got %#v", public.Messages)
	}
}

func TestSessionHTTPReturnsOnlyFinalMultiAgentAnswer(t *testing.T) {
	runtime := testRuntime(t)
	ctx := context.Background()
	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	session.Messages = append(session.Messages,
		state.Message{Role: state.MessageRoleUser, Content: "Compare two databases", RunID: "job-1"},
		state.Message{Role: state.MessageRoleAssistant, Content: "I will start two agents", RunID: "job-1", ToolCalls: []state.ToolCall{{ID: "call-1", Name: "Agent"}}},
		state.Message{Role: state.MessageRoleAssistant, Content: "Both agents have started", RunID: "job-1"},
		state.Message{Role: state.MessageRoleAssistant, Content: "Final comparison", RunID: "job-1"},
	)
	if err := runtime.sessions.Save(ctx, "alice", session); err != nil {
		t.Fatal(err)
	}
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/"+session.ID, nil)
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET session: %d %s", rec.Code, rec.Body.String())
	}
	var public state.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Messages) != 2 || public.Messages[1].Content != "Final comparison" {
		t.Fatalf("GET session exposed intermediate replies: %#v", public.Messages)
	}
}

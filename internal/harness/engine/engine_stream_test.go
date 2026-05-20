package engine

import (
	"context"
	"strings"
	"testing"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	publictypes "claude-codex/internal/public/types"
)

type streamPlanner struct{}

func (streamPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (Plan, error) {
	return Plan{AssistantText: "fallback", StopReason: "end_turn"}, nil
}

func (streamPlanner) StreamNext(_ context.Context, _ *state.Session, _ []toolkit.Descriptor, onChunk func(string)) (Plan, error) {
	onChunk("hel")
	onChunk("lo")
	return Plan{AssistantText: "hello", StopReason: "end_turn"}, nil
}

func TestRunStreamEmitsTokenChunksAndRecordsFinalMessage(t *testing.T) {
	eng := NewWithDir(streamPlanner{}, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 1, t.TempDir())
	session := state.NewSession(t.TempDir())
	var chunks []string
	result, err := eng.RunStream(context.Background(), session, "say hi", func(token string) {
		chunks = append(chunks, token)
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	if strings.Join(chunks, "") != "hello" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q", result.Output)
	}
	if got := session.Messages[len(session.Messages)-1].Content; got != "hello" {
		t.Fatalf("last assistant message = %q", got)
	}
}

func TestRunContentStreamPreservesContentBlocksAndStreamsChunks(t *testing.T) {
	eng := NewWithDir(streamPlanner{}, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 1, t.TempDir())
	session := state.NewSession(t.TempDir())
	prompt := []publictypes.ContentBlock{
		{Type: "text", Text: "summarize this"},
		{Type: "file", Source: map[string]any{"type": "base64", "media_type": "text/plain", "data": "SGVsbG8="}},
	}
	var chunks []string
	result, err := eng.RunContentStream(context.Background(), session, prompt, func(token string) {
		chunks = append(chunks, token)
	})
	if err != nil {
		t.Fatalf("RunContentStream() error = %v", err)
	}
	if strings.Join(chunks, "") != "hello" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q", result.Output)
	}
	if len(session.Messages) < 2 {
		t.Fatalf("expected user and assistant messages, got %d", len(session.Messages))
	}
	user := session.Messages[len(session.Messages)-2]
	if user.Role != state.MessageRoleUser || len(user.ContentBlocks) != len(prompt) {
		t.Fatalf("user content blocks were not preserved: %#v", user)
	}
	if got := session.Messages[len(session.Messages)-1].Content; got != "hello" {
		t.Fatalf("last assistant message = %q", got)
	}
}

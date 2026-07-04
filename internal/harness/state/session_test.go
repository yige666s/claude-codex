package state

import (
	"encoding/json"
	"testing"
)

func TestAddToolResultIsHiddenFromUserTranscript(t *testing.T) {
	session := NewSession(t.TempDir())

	session.AddToolResult("tool-1", "Artifact", json.RawMessage(`{"file_path":"result.png"}`), `{"id":"artifact-1"}`)

	if len(session.Messages) != 1 {
		t.Fatalf("expected one message, got %#v", session.Messages)
	}
	message := session.Messages[0]
	if message.Role != "tool" || !message.Hidden {
		t.Fatalf("expected hidden tool result, got %#v", message)
	}
	if message.ToolOutput != `{"id":"artifact-1"}` {
		t.Fatalf("unexpected tool output: %q", message.ToolOutput)
	}
}

func TestAddToolResultNormalizesInvalidToolInput(t *testing.T) {
	session := NewSession(t.TempDir())

	session.AddToolResult("tool-1", "Artifact", json.RawMessage(`{"file_path":`), `{"id":"artifact-1"}`)

	if len(session.Messages) != 1 {
		t.Fatalf("expected one message, got %#v", session.Messages)
	}
	if got := string(session.Messages[0].ToolInput); got != `{}` {
		t.Fatalf("expected invalid tool input to be normalized, got %q", got)
	}
	if _, err := json.Marshal(session); err != nil {
		t.Fatalf("session with normalized tool input should marshal: %v", err)
	}
}

func TestAddAssistantMessageWithToolsNormalizesInvalidToolInput(t *testing.T) {
	session := NewSession(t.TempDir())

	session.AddAssistantMessageWithTools("", []ToolCall{{
		ID:    "tool-1",
		Name:  "Bash",
		Input: json.RawMessage(`{"command":`),
	}})

	if len(session.Messages) != 1 {
		t.Fatalf("expected one message, got %#v", session.Messages)
	}
	if got := string(session.Messages[0].ToolCalls[0].Input); got != `{}` {
		t.Fatalf("expected invalid assistant tool input to be normalized, got %q", got)
	}
	if _, err := json.Marshal(session); err != nil {
		t.Fatalf("session with normalized assistant tool call should marshal: %v", err)
	}
}

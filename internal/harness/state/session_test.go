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

package queryhelpers

import (
	"testing"
	"time"

	publictypes "claude-codex/internal/public/types"
)

func TestIsResultSuccessful(t *testing.T) {
	assistant := &publictypes.Message{
		Type:      publictypes.MessageTypeAssistant,
		Timestamp: time.Now(),
		Content:   []publictypes.ContentBlock{{Type: "text", Text: "done"}},
	}
	if !IsResultSuccessful(assistant, "") {
		t.Fatal("expected assistant text message to be successful")
	}

	userTool := &publictypes.Message{
		Type:      publictypes.MessageTypeUser,
		Timestamp: time.Now(),
		Content:   []publictypes.ContentBlock{{Type: "tool_result", Content: "ok"}},
	}
	if !IsResultSuccessful(userTool, "") {
		t.Fatal("expected user tool result message to be successful")
	}

	promptOnly := &publictypes.Message{
		Type:      publictypes.MessageTypeUser,
		Timestamp: time.Now(),
		Content:   []publictypes.ContentBlock{{Type: "text", Text: "hello"}},
	}
	if !IsResultSuccessful(promptOnly, "end_turn") {
		t.Fatal("expected end_turn carve-out to be successful")
	}
}

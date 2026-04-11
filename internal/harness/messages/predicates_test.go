package messages

import "testing"

func TestIsHumanTurn(t *testing.T) {
	user := CreateUserMessage(CreateUserMessageOptions{Content: "hello"})
	if !IsHumanTurn(user) {
		t.Fatal("expected plain user turn to be human")
	}

	meta := CreateUserMessage(CreateUserMessageOptions{Content: "hello", IsMeta: true})
	if IsHumanTurn(meta) {
		t.Fatal("expected meta user turn to be excluded")
	}

	toolResult := CreateUserMessage(CreateUserMessageOptions{Content: "tool", ToolUseResult: "tool"})
	if IsHumanTurn(toolResult) {
		t.Fatal("expected tool-result user turn to be excluded")
	}
}

package messages

// IsHumanTurn returns true for genuine user-authored turns and excludes
// meta/system-like user messages and tool-result user messages.
func IsHumanTurn(msg Message) bool {
	userMsg, ok := msg.(*UserMessage)
	if !ok || userMsg == nil {
		return false
	}
	return userMsg.Type == "user" && !userMsg.IsMeta && userMsg.ToolUseResult == nil
}

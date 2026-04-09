package messages

// FilterOptions represents options for filtering messages
type FilterOptions struct {
	// IncludeToolUse includes messages with tool use
	IncludeToolUse bool
	// IncludeToolResults includes tool result messages
	IncludeToolResults bool
	// IncludeSynthetic includes synthetic messages
	IncludeSynthetic bool
	// IncludeEmpty includes empty messages
	IncludeEmpty bool
	// IncludeMeta includes meta messages
	IncludeMeta bool
	// IncludeVirtual includes virtual messages
	IncludeVirtual bool
	// MinLength filters messages by minimum text length
	MinLength int
	// MaxLength filters messages by maximum text length
	MaxLength int
}

// DefaultFilterOptions returns default filter options (include everything)
func DefaultFilterOptions() FilterOptions {
	return FilterOptions{
		IncludeToolUse:     true,
		IncludeToolResults: true,
		IncludeSynthetic:   true,
		IncludeEmpty:       true,
		IncludeMeta:        true,
		IncludeVirtual:     true,
		MinLength:          0,
		MaxLength:          0, // 0 means no limit
	}
}

// FilterMessages filters messages based on the provided options
func FilterMessages(messages []Message, opts FilterOptions) []Message {
	var result []Message

	for _, msg := range messages {
		if !shouldIncludeMessage(msg, opts) {
			continue
		}
		result = append(result, msg)
	}

	return result
}

func shouldIncludeMessage(msg Message, opts FilterOptions) bool {
	// Check tool use
	if !opts.IncludeToolUse && IsToolUseMessage(msg) {
		return false
	}

	// Check tool results
	if !opts.IncludeToolResults && IsToolResultMessage(msg) {
		return false
	}

	// Check synthetic messages
	if !opts.IncludeSynthetic && IsSyntheticMessage(msg) {
		return false
	}

	// Check empty messages
	if !opts.IncludeEmpty && !IsNotEmptyMessage(msg) {
		return false
	}

	// Check meta messages
	if !opts.IncludeMeta {
		switch m := msg.(type) {
		case *UserMessage:
			if m.IsMeta {
				return false
			}
		case *AssistantMessage:
			if m.IsMeta {
				return false
			}
		}
	}

	// Check virtual messages
	if !opts.IncludeVirtual {
		switch m := msg.(type) {
		case *UserMessage:
			if m.IsVirtual {
				return false
			}
		case *AssistantMessage:
			if m.IsVirtual {
				return false
			}
		}
	}

	// Check text length
	text := ExtractTextContent(msg)
	textLen := len(text)

	if opts.MinLength > 0 && textLen < opts.MinLength {
		return false
	}

	if opts.MaxLength > 0 && textLen > opts.MaxLength {
		return false
	}

	return true
}

// FilterUnresolvedToolUses removes tool use messages that don't have corresponding tool results
func FilterUnresolvedToolUses(messages []Message) []Message {
	// Build a set of resolved tool use IDs
	resolvedToolUseIDs := make(map[string]bool)
	for _, msg := range messages {
		if IsToolResultMessage(msg) {
			toolUseID := GetToolResultID(msg)
			if toolUseID != "" {
				resolvedToolUseIDs[toolUseID] = true
			}
		}
	}

	// Filter out unresolved tool uses
	var result []Message
	for _, msg := range messages {
		if IsToolUseMessage(msg) {
			toolUseID := GetToolUseID(msg)
			if toolUseID != "" && !resolvedToolUseIDs[toolUseID] {
				continue // Skip unresolved tool use
			}
		}
		result = append(result, msg)
	}

	return result
}

// FilterWhitespaceOnlyMessages removes messages that only contain whitespace
func FilterWhitespaceOnlyMessages(messages []Message) []Message {
	var result []Message

	for _, msg := range messages {
		text := ExtractTextContent(msg)
		// Keep message if it has non-whitespace content
		if len(trimWhitespace(text)) > 0 {
			result = append(result, msg)
		}
	}

	return result
}

func trimWhitespace(s string) string {
	// Simple whitespace trimming
	start := 0
	end := len(s)

	for start < end && isWhitespace(s[start]) {
		start++
	}

	for end > start && isWhitespace(s[end-1]) {
		end--
	}

	return s[start:end]
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// FilterByRole filters messages by role
func FilterByRole(messages []Message, role MessageRole) []Message {
	var result []Message

	for _, msg := range messages {
		switch msg.(type) {
		case *UserMessage:
			if role == RoleUser {
				result = append(result, msg)
			}
		case *AssistantMessage:
			if role == RoleAssistant {
				result = append(result, msg)
			}
		}
	}

	return result
}

// FilterByTimeRange filters messages within a time range
func FilterByTimeRange(messages []Message, startTime, endTime string) []Message {
	var result []Message

	for _, message := range messages {
		timestamp := message.GetTimestamp()
		if timestamp >= startTime && timestamp <= endTime {
			result = append(result, message)
		}
	}

	return result
}

// GroupMessagesByToolUse groups tool use messages with their results
type ToolUseGroup struct {
	ToolUseMessage   *AssistantMessage
	ToolResultMessage *UserMessage
	ToolUseID        string
}

// GroupByToolUse groups tool use messages with their corresponding results
func GroupByToolUse(messages []Message) []ToolUseGroup {
	var groups []ToolUseGroup
	toolUseMap := make(map[string]*AssistantMessage)

	// First pass: collect tool use messages
	for _, msg := range messages {
		if assistantMsg, ok := msg.(*AssistantMessage); ok {
			if IsToolUseMessage(assistantMsg) {
				toolUseID := GetToolUseID(assistantMsg)
				if toolUseID != "" {
					toolUseMap[toolUseID] = assistantMsg
				}
			}
		}
	}

	// Second pass: match with tool results
	for _, msg := range messages {
		if userMsg, ok := msg.(*UserMessage); ok {
			if IsToolResultMessage(userMsg) {
				toolUseID := GetToolResultID(userMsg)
				if toolUseID != "" {
					if toolUseMsg, exists := toolUseMap[toolUseID]; exists {
						groups = append(groups, ToolUseGroup{
							ToolUseMessage:   toolUseMsg,
							ToolResultMessage: userMsg,
							ToolUseID:        toolUseID,
						})
						delete(toolUseMap, toolUseID)
					}
				}
			}
		}
	}

	return groups
}

// CountMessagesByRole counts messages by role
func CountMessagesByRole(messages []Message) map[MessageRole]int {
	counts := make(map[MessageRole]int)

	for _, msg := range messages {
		switch msg.(type) {
		case *UserMessage:
			counts[RoleUser]++
		case *AssistantMessage:
			counts[RoleAssistant]++
		}
	}

	return counts
}

// GetMessagesByUUIDs retrieves messages by their UUIDs
func GetMessagesByUUIDs(messages []Message, uuids []string) []Message {
	uuidSet := make(map[string]bool)
	for _, uuid := range uuids {
		uuidSet[uuid] = true
	}

	var result []Message
	for _, msg := range messages {
		if uuidSet[msg.GetUUID()] {
			result = append(result, msg)
		}
	}

	return result
}

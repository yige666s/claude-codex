package agentruntime

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"claude-codex/internal/harness/state"
)

func (s *FileSessionStore) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []MessageSearchResult{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	sessions, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	matches := make([]MessageSearchResult, 0)
	for _, session := range sessions {
		if session == nil {
			continue
		}
		for index, message := range session.Messages {
			if message.Hidden || message.Role == "tool" {
				continue
			}
			searchable := messageSearchContent(message.Content, message.ToolOutput, query)
			if !messageSearchMatches(searchable, query) {
				continue
			}
			matches = append(matches, MessageSearchResult{
				SessionID:    session.ID,
				MessageIndex: index,
				Role:         message.Role,
				Content:      searchable,
				Snippet:      messageSearchSnippet(searchable, query, 160),
				SessionTitle: searchSessionTitle(session),
				CreatedAt:    message.CreatedAt,
			})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	if offset >= len(matches) {
		return []MessageSearchResult{}, nil
	}
	end := offset + limit
	if end > len(matches) {
		end = len(matches)
	}
	return matches[offset:end], nil
}

func messageSearchMatches(content, query string) bool {
	return strings.Contains(strings.ToLower(content), strings.ToLower(strings.TrimSpace(query)))
}

func messageSearchContent(content, toolOutput, query string) string {
	if messageSearchMatches(content, query) {
		return content
	}
	if messageSearchMatches(toolOutput, query) {
		return toolOutput
	}
	return firstNonEmptyString(content, toolOutput)
}

func messageSearchSnippet(content, query string, maxRunes int) string {
	content = strings.TrimSpace(content)
	query = strings.TrimSpace(query)
	if content == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	matchByte := strings.Index(strings.ToLower(content), strings.ToLower(query))
	matchRune := 0
	if matchByte >= 0 {
		matchRune = utf8.RuneCountInString(content[:matchByte])
	}
	start := matchRune - maxRunes/3
	if start < 0 {
		start = 0
	}
	if start+maxRunes > len(runes) {
		start = len(runes) - maxRunes
	}
	end := start + maxRunes
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if end < len(runes) {
		suffix = "..."
	}
	return prefix + strings.TrimSpace(string(runes[start:end])) + suffix
}

func searchSessionTitle(session *state.Session) string {
	if session == nil {
		return ""
	}
	if strings.TrimSpace(session.Description) != "" {
		return strings.TrimSpace(session.Description)
	}
	for _, message := range session.Messages {
		if message.Hidden || message.Role != "user" || strings.TrimSpace(message.Content) == "" {
			continue
		}
		return truncateSearchTitle(message.Content)
	}
	return session.ID
}

func truncateSearchTitle(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maxRunes = 64
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func escapeLikePattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

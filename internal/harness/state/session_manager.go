package state

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionManager handles advanced session operations
type SessionManager struct {
	home string
}

// NewSessionManager creates a new session manager
func NewSessionManager(home string) *SessionManager {
	return &SessionManager{home: home}
}

// SearchSessions searches sessions by content, tags, or description
func (sm *SessionManager) SearchSessions(query string, options SearchOptions) ([]*Session, error) {
	sessions, err := sm.ListSessions(options)
	if err != nil {
		return nil, err
	}

	if query == "" {
		return sessions, nil
	}

	query = strings.ToLower(query)
	var results []*Session

	for _, session := range sessions {
		if sm.matchesQuery(session, query) {
			results = append(results, session)
		}
	}

	return results, nil
}

// SearchOptions defines search and filter options
type SearchOptions struct {
	Tags         []string
	Archived     bool
	IncludeArchived bool
	SortBy       string // "updated", "created", "usage"
	Limit        int
	Offset       int
}

// ListSessions lists all sessions with optional filtering
func (sm *SessionManager) ListSessions(options SearchOptions) ([]*Session, error) {
	dir := filepath.Join(sm.home, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Session{}, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		id := trimSessionFilename(entry.Name())
		session, err := LoadSession(sm.home, id)
		if err != nil {
			continue // Skip corrupted sessions
		}

		// Apply filters
		if !options.IncludeArchived && session.Archived {
			continue
		}

		if len(options.Tags) > 0 && !sm.hasAnyTag(session, options.Tags) {
			continue
		}

		sessions = append(sessions, session)
	}

	// Sort sessions
	sm.sortSessions(sessions, options.SortBy)

	// Apply pagination
	if options.Limit > 0 {
		start := options.Offset
		end := start + options.Limit
		if start >= len(sessions) {
			return []*Session{}, nil
		}
		if end > len(sessions) {
			end = len(sessions)
		}
		sessions = sessions[start:end]
	}

	return sessions, nil
}

// CleanupSessions removes old archived sessions
func (sm *SessionManager) CleanupSessions(olderThan time.Duration) (int, error) {
	sessions, err := sm.ListSessions(SearchOptions{IncludeArchived: true})
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-olderThan)
	count := 0

	for _, session := range sessions {
		if session.Archived && session.UpdatedAt.Before(cutoff) {
			if err := sm.DeleteSession(session.ID); err != nil {
				continue
			}
			count++
		}
	}

	return count, nil
}

// DeleteSession permanently deletes a session
func (sm *SessionManager) DeleteSession(id string) error {
	path := filepath.Join(sm.home, "sessions", id+".json")
	return os.Remove(path)
}

// MergeSessions merges multiple sessions into one
func (sm *SessionManager) MergeSessions(sessionIDs []string, description string) (*Session, error) {
	if len(sessionIDs) < 2 {
		return nil, fmt.Errorf("need at least 2 sessions to merge")
	}

	// Load all sessions
	sessions := make([]*Session, len(sessionIDs))
	for i, id := range sessionIDs {
		session, err := LoadSession(sm.home, id)
		if err != nil {
			return nil, fmt.Errorf("failed to load session %s: %w", id, err)
		}
		sessions[i] = session
	}

	// Create merged session
	now := time.Now().UTC()
	merged := &Session{
		ID:          newID(),
		WorkingDir:  sessions[0].WorkingDir,
		StartedAt:   now,
		UpdatedAt:   now,
		Description: description,
		Messages:    make([]Message, 0),
		Tags:        make([]string, 0),
		Metadata:    make(map[string]string),
	}

	// Merge messages in chronological order
	allMessages := make([]Message, 0)
	for _, session := range sessions {
		allMessages = append(allMessages, session.Messages...)
	}
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].CreatedAt.Before(allMessages[j].CreatedAt)
	})
	merged.Messages = allMessages

	// Merge tags (unique)
	tagSet := make(map[string]bool)
	for _, session := range sessions {
		for _, tag := range session.Tags {
			tagSet[tag] = true
		}
	}
	for tag := range tagSet {
		merged.Tags = append(merged.Tags, tag)
	}

	// Merge metadata (last wins)
	for _, session := range sessions {
		for k, v := range session.Metadata {
			merged.Metadata[k] = v
		}
	}

	// Recalculate usage
	for _, msg := range merged.Messages {
		if msg.Role == "user" {
			merged.Usage.RecordInput(msg.Content)
		} else if msg.Role == "assistant" {
			merged.Usage.RecordOutput(msg.Content)
		} else if msg.Role == "tool" {
			merged.Usage.RecordOutput(msg.ToolOutput)
		}
	}

	return merged, nil
}

// GetSessionStats returns statistics about sessions
func (sm *SessionManager) GetSessionStats() (*SessionStats, error) {
	sessions, err := sm.ListSessions(SearchOptions{IncludeArchived: true})
	if err != nil {
		return nil, err
	}

	stats := &SessionStats{
		TotalSessions:    len(sessions),
		ArchivedSessions: 0,
		TotalMessages:    0,
		TotalTokens:      0,
		TotalCost:        0,
		TagCounts:        make(map[string]int),
	}

	for _, session := range sessions {
		if session.Archived {
			stats.ArchivedSessions++
		}
		stats.TotalMessages += len(session.Messages)
		stats.TotalTokens += session.Usage.TotalTokens
		stats.TotalCost += session.Usage.EstimatedCostUSD

		for _, tag := range session.Tags {
			stats.TagCounts[tag]++
		}
	}

	return stats, nil
}

// SessionStats contains session statistics
type SessionStats struct {
	TotalSessions    int
	ArchivedSessions int
	TotalMessages    int
	TotalTokens      int
	TotalCost        float64
	TagCounts        map[string]int
}

// Helper methods

func (sm *SessionManager) matchesQuery(session *Session, query string) bool {
	// Search in description
	if strings.Contains(strings.ToLower(session.Description), query) {
		return true
	}

	// Search in tags
	for _, tag := range session.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}

	// Search in messages
	for _, msg := range session.Messages {
		if strings.Contains(strings.ToLower(msg.Content), query) {
			return true
		}
	}

	// Search in metadata
	for k, v := range session.Metadata {
		if strings.Contains(strings.ToLower(k), query) || strings.Contains(strings.ToLower(v), query) {
			return true
		}
	}

	return false
}

func (sm *SessionManager) hasAnyTag(session *Session, tags []string) bool {
	for _, tag := range tags {
		if session.HasTag(tag) {
			return true
		}
	}
	return false
}

func (sm *SessionManager) sortSessions(sessions []*Session, sortBy string) {
	switch sortBy {
	case "created":
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].StartedAt.After(sessions[j].StartedAt)
		})
	case "usage":
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].Usage.TotalTokens > sessions[j].Usage.TotalTokens
		})
	default: // "updated" or empty
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		})
	}
}

// ExportSession exports a session to a file
func (sm *SessionManager) ExportSession(id, outputPath string) error {
	session, err := LoadSession(sm.home, id)
	if err != nil {
		return err
	}

	data, err := session.Export()
	if err != nil {
		return err
	}

	return os.WriteFile(outputPath, data, 0644)
}

// ImportSessionFromFile imports a session from a file
func (sm *SessionManager) ImportSessionFromFile(inputPath string) (*Session, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, err
	}

	session, err := ImportSession(data)
	if err != nil {
		return nil, err
	}

	// Generate new ID to avoid conflicts
	session.ID = newID()
	session.UpdatedAt = time.Now().UTC()

	// Save imported session
	if _, err := session.Save(sm.home); err != nil {
		return nil, err
	}

	return session, nil
}

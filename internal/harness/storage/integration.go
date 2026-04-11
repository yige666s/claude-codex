package storage

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"claude-codex/internal/harness/state"
)

// IntegrateWithStateSession demonstrates integration with existing state.Session
func IntegrateWithStateSession(session *state.Session, homeDir, projectDir string) (*SessionStorage, error) {
	// Create storage for the session
	storage, err := NewSessionStorage(homeDir, session.ID, projectDir)
	if err != nil {
		return nil, err
	}

	// Record all existing messages from the session
	for _, msg := range session.Messages {
		transcriptMsg := convertStateMessageToTranscript(msg, session)
		if err := storage.RecordMessage(transcriptMsg); err != nil {
			return nil, err
		}
	}

	// Set metadata from session
	if session.Description != "" {
		storage.SetCustomTitle(session.Description)
	}

	if len(session.Tags) > 0 {
		// Use first tag as the session tag
		storage.SetTag(session.Tags[0])
	}

	// Flush to ensure everything is written
	if err := storage.Flush(); err != nil {
		return nil, err
	}

	return storage, nil
}

// convertStateMessageToTranscript converts a state.Message to TranscriptMessage
func convertStateMessageToTranscript(msg state.Message, session *state.Session) *TranscriptMessage {
	transcriptMsg := &TranscriptMessage{
		BaseEntry: BaseEntry{
			Type:      EntryType(msg.Role), // user, assistant, tool
			Timestamp: msg.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		},
		UUID:    generateUUID(),
		Role:    msg.Role,
		Content: msg.Content,
		CWD:     session.WorkingDir,
	}

	// Handle tool messages
	if msg.Role == "tool" {
		transcriptMsg.ToolCallID = msg.ToolCallID
		transcriptMsg.ToolName = msg.ToolName
		transcriptMsg.ToolOutput = msg.ToolOutput
		if msg.ToolInput != nil {
			transcriptMsg.ToolInput = msg.ToolInput
		}
	}

	// Handle assistant messages with tool calls
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		toolCalls := make([]ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			toolCalls[i] = ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			}
		}
		transcriptMsg.ToolCalls = toolCalls
	}

	return transcriptMsg
}

// LoadSessionFromStorage loads a session from storage and converts to state.Session
func LoadSessionFromStorage(storage *SessionStorage) (*state.Session, error) {
	entries, err := storage.LoadTranscript()
	if err != nil {
		return nil, err
	}

	// Get metadata
	meta := storage.GetMetadata()

	// Create new session
	session := &state.Session{
		ID:          storage.GetSessionID(),
		WorkingDir:  "", // Will be set from first message
		Messages:    make([]state.Message, 0),
		Tags:        []string{},
		Description: meta.CustomTitle,
	}

	if meta.Tag != "" {
		session.Tags = append(session.Tags, meta.Tag)
	}

	// Convert transcript messages to state messages
	for _, entry := range entries {
		if msg, ok := entry.(*TranscriptMessage); ok {
			stateMsg := convertTranscriptToStateMessage(msg)
			session.Messages = append(session.Messages, stateMsg)

			// Set working dir from first message
			if session.WorkingDir == "" && msg.CWD != "" {
				session.WorkingDir = msg.CWD
			}
		}
	}

	// Set timestamps
	if len(session.Messages) > 0 {
		session.StartedAt = session.Messages[0].CreatedAt
		session.UpdatedAt = session.Messages[len(session.Messages)-1].CreatedAt
	}

	return session, nil
}

// convertTranscriptToStateMessage converts a TranscriptMessage to state.Message
func convertTranscriptToStateMessage(msg *TranscriptMessage) state.Message {
	stateMsg := state.Message{
		Role:      msg.Role,
		Content:   msg.Content,
		CreatedAt: parseTimestamp(msg.Timestamp),
	}

	// Handle tool messages
	if msg.Role == "tool" {
		stateMsg.ToolCallID = msg.ToolCallID
		stateMsg.ToolName = msg.ToolName
		stateMsg.ToolOutput = msg.ToolOutput
		if msg.ToolInput != nil {
			stateMsg.ToolInput = msg.ToolInput
		}
	}

	// Handle assistant messages with tool calls
	if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
		toolCalls := make([]state.ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			toolCalls[i] = state.ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			}
		}
		stateMsg.ToolCalls = toolCalls
	}

	return stateMsg
}

// parseTimestamp parses an ISO 8601 timestamp
func parseTimestamp(ts string) time.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Now().UTC()
	}
	return t
}

// generateUUID generates a random UUID
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b[:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:])
}

// SyncSessionToStorage syncs a state.Session to storage incrementally
func SyncSessionToStorage(session *state.Session, storage *SessionStorage, lastSyncedIndex int) error {
	// Record only new messages since last sync
	for i := lastSyncedIndex; i < len(session.Messages); i++ {
		msg := session.Messages[i]
		transcriptMsg := convertStateMessageToTranscript(msg, session)
		if err := storage.RecordMessage(transcriptMsg); err != nil {
			return err
		}
	}

	// Update metadata if changed
	meta := storage.GetMetadata()
	if session.Description != "" && meta.CustomTitle != session.Description {
		storage.SetCustomTitle(session.Description)
	}

	if len(session.Tags) > 0 && (meta.Tag == "" || meta.Tag != session.Tags[0]) {
		storage.SetTag(session.Tags[0])
	}

	return storage.Flush()
}

// Example usage:
//
//   // Create a session
//   session := state.NewSession("/path/to/project")
//   session.AddUserMessage("Hello")
//   session.AddAssistantMessage("Hi there!")
//
//   // Integrate with storage
//   storage, err := IntegrateWithStateSession(session, homeDir, projectDir)
//   if err != nil {
//       log.Fatal(err)
//   }
//   defer storage.Close()
//
//   // Continue using the session
//   session.AddUserMessage("How are you?")
//
//   // Sync new messages to storage
//   SyncSessionToStorage(session, storage, 2) // 2 = number of previously synced messages
//
//   // Later, load from storage
//   loadedSession, err := LoadSessionFromStorage(storage)

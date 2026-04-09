package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTranscriptWriter(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "transcript-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	transcriptPath := filepath.Join(tmpDir, "test-session.jsonl")
	sessionID := "test-session-123"

	t.Run("AppendEntry", func(t *testing.T) {
		writer := NewTranscriptWriter(transcriptPath, sessionID)
		defer writer.Close()

		msg := &TranscriptMessage{
			BaseEntry: BaseEntry{
				Type:      EntryTypeUser,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			UUID:    "msg-001",
			Role:    "user",
			Content: "Hello, world!",
		}

		if err := writer.AppendEntrySync(msg); err != nil {
			t.Fatalf("Failed to append entry: %v", err)
		}

		// Verify file was created
		if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
			t.Fatal("Transcript file was not created")
		}
	})

	t.Run("ReadTranscript", func(t *testing.T) {
		reader := NewTranscriptReader(transcriptPath)
		entries, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("Failed to read transcript: %v", err)
		}

		if len(entries) != 1 {
			t.Fatalf("Expected 1 entry, got %d", len(entries))
		}

		msg, ok := entries[0].(*TranscriptMessage)
		if !ok {
			t.Fatal("Entry is not a TranscriptMessage")
		}

		if msg.Content != "Hello, world!" {
			t.Errorf("Expected content 'Hello, world!', got '%s'", msg.Content)
		}
	})

	t.Run("BatchWrites", func(t *testing.T) {
		batchPath := filepath.Join(tmpDir, "batch-test.jsonl")
		writer := NewTranscriptWriter(batchPath, sessionID)
		defer writer.Close()

		// Write multiple entries
		for i := 0; i < 10; i++ {
			msg := &TranscriptMessage{
				BaseEntry: BaseEntry{
					Type:      EntryTypeUser,
					SessionID: sessionID,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				},
				UUID:    "msg-" + string(rune('0'+i)),
				Role:    "user",
				Content: "Message " + string(rune('0'+i)),
			}
			if err := writer.AppendEntry(msg); err != nil {
				t.Fatalf("Failed to append entry %d: %v", i, err)
			}
		}

		// Flush to ensure all writes complete
		if err := writer.Flush(); err != nil {
			t.Fatalf("Failed to flush: %v", err)
		}

		// Read and verify
		reader := NewTranscriptReader(batchPath)
		entries, err := reader.ReadAll()
		if err != nil {
			t.Fatalf("Failed to read transcript: %v", err)
		}

		if len(entries) != 10 {
			t.Fatalf("Expected 10 entries, got %d", len(entries))
		}
	})

	t.Run("Flush", func(t *testing.T) {
		flushPath := filepath.Join(tmpDir, "flush-test.jsonl")
		writer := NewTranscriptWriter(flushPath, sessionID)

		msg := &TranscriptMessage{
			BaseEntry: BaseEntry{
				Type:      EntryTypeUser,
				SessionID: sessionID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
			UUID:    "msg-flush",
			Role:    "user",
			Content: "Flush test",
		}

		if err := writer.AppendEntry(msg); err != nil {
			t.Fatalf("Failed to append entry: %v", err)
		}

		// Flush should complete all pending writes
		if err := writer.Flush(); err != nil {
			t.Fatalf("Failed to flush: %v", err)
		}

		writer.Close()

		// Verify data was written
		data, err := os.ReadFile(flushPath)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}

		if len(data) == 0 {
			t.Fatal("File is empty after flush")
		}
	})
}

func TestSessionStorage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "session-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	homeDir := filepath.Join(tmpDir, "home")
	projectDir := filepath.Join(tmpDir, "project")

	if err := os.MkdirAll(homeDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatal(err)
	}

	t.Run("CreateSession", func(t *testing.T) {
		storage, err := NewSessionStorage(homeDir, "test-session", projectDir)
		if err != nil {
			t.Fatalf("Failed to create session storage: %v", err)
		}
		defer storage.Close()

		if storage.GetSessionID() != "test-session" {
			t.Errorf("Expected session ID 'test-session', got '%s'", storage.GetSessionID())
		}
	})

	t.Run("RecordMessage", func(t *testing.T) {
		storage, err := NewSessionStorage(homeDir, "msg-test", projectDir)
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()

		msg := &TranscriptMessage{
			UUID:    "msg-001",
			Role:    "user",
			Content: "Test message",
		}

		if err := storage.RecordMessageSync(msg); err != nil {
			t.Fatalf("Failed to record message: %v", err)
		}

		// Verify message was written
		entries, err := storage.LoadTranscript()
		if err != nil {
			t.Fatalf("Failed to load transcript: %v", err)
		}

		if len(entries) != 1 {
			t.Fatalf("Expected 1 entry, got %d", len(entries))
		}
	})

	t.Run("SetMetadata", func(t *testing.T) {
		storage, err := NewSessionStorage(homeDir, "meta-test", projectDir)
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()

		if err := storage.SetCustomTitle("Test Title"); err != nil {
			t.Fatalf("Failed to set title: %v", err)
		}

		if err := storage.SetTag("test-tag"); err != nil {
			t.Fatalf("Failed to set tag: %v", err)
		}

		if err := storage.SetAgentName("TestAgent"); err != nil {
			t.Fatalf("Failed to set agent name: %v", err)
		}

		if err := storage.Flush(); err != nil {
			t.Fatal(err)
		}

		// Load and verify metadata
		entries, err := storage.LoadTranscript()
		if err != nil {
			t.Fatalf("Failed to load transcript: %v", err)
		}

		meta := storage.GetMetadata()
		if meta.CustomTitle != "Test Title" {
			t.Errorf("Expected title 'Test Title', got '%s'", meta.CustomTitle)
		}
		if meta.Tag != "test-tag" {
			t.Errorf("Expected tag 'test-tag', got '%s'", meta.Tag)
		}
		if meta.AgentName != "TestAgent" {
			t.Errorf("Expected agent name 'TestAgent', got '%s'", meta.AgentName)
		}

		// Verify entries were written
		if len(entries) != 3 {
			t.Fatalf("Expected 3 metadata entries, got %d", len(entries))
		}
	})

	t.Run("LinkPR", func(t *testing.T) {
		storage, err := NewSessionStorage(homeDir, "pr-test", projectDir)
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()

		if err := storage.LinkPR(123, "https://github.com/owner/repo/pull/123", "owner/repo"); err != nil {
			t.Fatalf("Failed to link PR: %v", err)
		}

		if err := storage.Flush(); err != nil {
			t.Fatal(err)
		}

		meta := storage.GetMetadata()
		if meta.PRNumber != 123 {
			t.Errorf("Expected PR number 123, got %d", meta.PRNumber)
		}
		if meta.PRRepository != "owner/repo" {
			t.Errorf("Expected repository 'owner/repo', got '%s'", meta.PRRepository)
		}
	})

	t.Run("ReAppendMetadata", func(t *testing.T) {
		storage, err := NewSessionStorage(homeDir, "reappend-test", projectDir)
		if err != nil {
			t.Fatal(err)
		}
		defer storage.Close()

		// Set metadata
		storage.SetCustomTitle("Original Title")
		storage.SetTag("original-tag")
		storage.Flush()

		// Add some messages
		for i := 0; i < 5; i++ {
			msg := &TranscriptMessage{
				UUID:    "msg-" + string(rune('0'+i)),
				Role:    "user",
				Content: "Message",
			}
			storage.RecordMessage(msg)
		}
		storage.Flush()

		// Re-append metadata
		if err := storage.ReAppendMetadata(); err != nil {
			t.Fatalf("Failed to re-append metadata: %v", err)
		}

		storage.Flush()

		// Verify metadata is still correct
		entries, err := storage.LoadTranscript()
		if err != nil {
			t.Fatal(err)
		}

		// Should have: 2 initial metadata + 5 messages + 2 re-appended metadata
		if len(entries) < 7 {
			t.Fatalf("Expected at least 7 entries, got %d", len(entries))
		}

		meta := storage.GetMetadata()
		if meta.CustomTitle != "Original Title" {
			t.Errorf("Expected title 'Original Title', got '%s'", meta.CustomTitle)
		}
	})
}

func TestSnapshotManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "snapshot-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	homeDir := filepath.Join(tmpDir, "home")
	if err := os.MkdirAll(homeDir, 0700); err != nil {
		t.Fatal(err)
	}

	manager := NewSnapshotManager(homeDir)
	sessionID := "test-session"

	t.Run("CreateSnapshot", func(t *testing.T) {
		entries := []Entry{
			&TranscriptMessage{
				BaseEntry: BaseEntry{
					Type:      EntryTypeUser,
					SessionID: sessionID,
				},
				UUID:    "msg-001",
				Role:    "user",
				Content: "Hello",
			},
			&MetadataEntry{
				BaseEntry: BaseEntry{
					Type:      EntryTypeCustomTitle,
					SessionID: sessionID,
				},
				CustomTitle: "Test Snapshot",
			},
		}

		snapshot, err := manager.CreateSnapshot(sessionID, entries)
		if err != nil {
			t.Fatalf("Failed to create snapshot: %v", err)
		}

		if snapshot.SessionID != sessionID {
			t.Errorf("Expected session ID '%s', got '%s'", sessionID, snapshot.SessionID)
		}

		if snapshot.MessageCount != 1 {
			t.Errorf("Expected 1 message, got %d", snapshot.MessageCount)
		}

		if snapshot.Metadata.CustomTitle != "Test Snapshot" {
			t.Errorf("Expected title 'Test Snapshot', got '%s'", snapshot.Metadata.CustomTitle)
		}
	})

	t.Run("SaveAndLoadSnapshot", func(t *testing.T) {
		snapshot := &Snapshot{
			SessionID:    sessionID,
			Timestamp:    time.Now().UTC(),
			MessageCount: 2,
			Metadata: &SessionMetadata{
				CustomTitle: "Saved Snapshot",
				Tag:         "test",
			},
			Messages: []TranscriptMessage{
				{
					BaseEntry: BaseEntry{
						Type:      EntryTypeUser,
						SessionID: sessionID,
					},
					UUID:    "msg-001",
					Role:    "user",
					Content: "Message 1",
				},
				{
					BaseEntry: BaseEntry{
						Type:      EntryTypeAssistant,
						SessionID: sessionID,
					},
					UUID:    "msg-002",
					Role:    "assistant",
					Content: "Response 1",
				},
			},
		}

		path, err := manager.SaveSnapshot(snapshot)
		if err != nil {
			t.Fatalf("Failed to save snapshot: %v", err)
		}

		loaded, err := manager.LoadSnapshot(path)
		if err != nil {
			t.Fatalf("Failed to load snapshot: %v", err)
		}

		if loaded.SessionID != sessionID {
			t.Errorf("Expected session ID '%s', got '%s'", sessionID, loaded.SessionID)
		}

		if loaded.MessageCount != 2 {
			t.Errorf("Expected 2 messages, got %d", loaded.MessageCount)
		}

		if loaded.Metadata.CustomTitle != "Saved Snapshot" {
			t.Errorf("Expected title 'Saved Snapshot', got '%s'", loaded.Metadata.CustomTitle)
		}
	})

	t.Run("ListSnapshots", func(t *testing.T) {
		// Create multiple snapshots
		for i := 0; i < 3; i++ {
			snapshot := &Snapshot{
				SessionID:    sessionID,
				Timestamp:    time.Now().UTC().Add(time.Duration(i) * time.Second),
				MessageCount: i,
				Metadata:     &SessionMetadata{},
			}
			if _, err := manager.SaveSnapshot(snapshot); err != nil {
				t.Fatalf("Failed to save snapshot %d: %v", i, err)
			}
		}

		snapshots, err := manager.ListSnapshots(sessionID)
		if err != nil {
			t.Fatalf("Failed to list snapshots: %v", err)
		}

		if len(snapshots) < 3 {
			t.Errorf("Expected at least 3 snapshots, got %d", len(snapshots))
		}
	})

	t.Run("GetLatestSnapshot", func(t *testing.T) {
		latest, err := manager.GetLatestSnapshot(sessionID)
		if err != nil {
			t.Fatalf("Failed to get latest snapshot: %v", err)
		}

		if latest.SessionID != sessionID {
			t.Errorf("Expected session ID '%s', got '%s'", sessionID, latest.SessionID)
		}
	})

	t.Run("CompactSnapshot", func(t *testing.T) {
		snapshot := &Snapshot{
			SessionID:    sessionID,
			Timestamp:    time.Now().UTC(),
			MessageCount: 10,
			Metadata:     &SessionMetadata{CustomTitle: "Compact Test"},
			Messages:     make([]TranscriptMessage, 10),
		}

		for i := 0; i < 10; i++ {
			snapshot.Messages[i] = TranscriptMessage{
				BaseEntry: BaseEntry{
					Type:      EntryTypeUser,
					SessionID: sessionID,
				},
				UUID:    "msg-" + string(rune('0'+i)),
				Content: "Message",
			}
		}

		compacted := manager.CompactSnapshot(snapshot, 5)
		if len(compacted.Messages) != 5 {
			t.Errorf("Expected 5 messages after compaction, got %d", len(compacted.Messages))
		}

		if compacted.Metadata.CustomTitle != "Compact Test" {
			t.Error("Metadata was not preserved during compaction")
		}
	})

	t.Run("DeleteSnapshot", func(t *testing.T) {
		snapshot := &Snapshot{
			SessionID:    "delete-test",
			Timestamp:    time.Now().UTC(),
			MessageCount: 0,
			Metadata:     &SessionMetadata{},
		}

		path, err := manager.SaveSnapshot(snapshot)
		if err != nil {
			t.Fatal(err)
		}

		if err := manager.DeleteSnapshot(path); err != nil {
			t.Fatalf("Failed to delete snapshot: %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("Snapshot file still exists after deletion")
		}
	})
}

func TestJSONLFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "jsonl-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	transcriptPath := filepath.Join(tmpDir, "test.jsonl")

	t.Run("ValidJSONL", func(t *testing.T) {
		writer := NewTranscriptWriter(transcriptPath, "test")
		defer writer.Close()

		// Write various entry types
		entries := []Entry{
			&TranscriptMessage{
				BaseEntry: BaseEntry{Type: EntryTypeUser, SessionID: "test"},
				UUID:      "msg-1",
				Content:   "User message",
			},
			&MetadataEntry{
				BaseEntry:   BaseEntry{Type: EntryTypeCustomTitle, SessionID: "test"},
				CustomTitle: "Title",
			},
			&PRLinkEntry{
				BaseEntry:    BaseEntry{Type: EntryTypePRLink, SessionID: "test"},
				PRNumber:     42,
				PRUrl:        "https://github.com/test/repo/pull/42",
				PRRepository: "test/repo",
			},
		}

		for _, entry := range entries {
			if err := writer.AppendEntrySync(entry); err != nil {
				t.Fatalf("Failed to write entry: %v", err)
			}
		}

		// Read and verify each line is valid JSON
		data, err := os.ReadFile(transcriptPath)
		if err != nil {
			t.Fatal(err)
		}

		lines := 0
		for i, line := range []byte(string(data)) {
			if line == '\n' {
				lines++
			} else if i == 0 || data[i-1] == '\n' {
				// Start of a line, verify it's valid JSON
				end := i
				for end < len(data) && data[end] != '\n' {
					end++
				}
				var obj map[string]interface{}
				if err := json.Unmarshal(data[i:end], &obj); err != nil {
					t.Errorf("Invalid JSON at line %d: %v", lines+1, err)
				}
			}
		}

		if lines != 3 {
			t.Errorf("Expected 3 lines, got %d", lines)
		}
	})
}

func TestConcurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "concurrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	transcriptPath := filepath.Join(tmpDir, "concurrent.jsonl")
	writer := NewTranscriptWriter(transcriptPath, "test")
	defer writer.Close()

	// Write from multiple goroutines
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				msg := &TranscriptMessage{
					BaseEntry: BaseEntry{
						Type:      EntryTypeUser,
						SessionID: "test",
					},
					UUID:    "msg-" + string(rune('0'+id)) + "-" + string(rune('0'+j)),
					Content: "Concurrent message",
				}
				writer.AppendEntry(msg)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	if err := writer.Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}

	// Verify all messages were written
	reader := NewTranscriptReader(transcriptPath)
	entries, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read transcript: %v", err)
	}

	if len(entries) != 100 {
		t.Errorf("Expected 100 entries, got %d", len(entries))
	}
}

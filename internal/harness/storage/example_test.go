package storage_test

import (
	"fmt"
	"log"
	"path/filepath"

	"claude-codex/internal/harness/storage"
)

// Example demonstrates basic usage of the session storage system
func Example_basicUsage() {
	// Create temp directories for the example
	tmpDir := "/tmp/claude-example"
	homeDir := filepath.Join(tmpDir, ".claude-codex")
	projectDir := filepath.Join(tmpDir, "workspace", "my-project")
	sessionID := "session-123"

	store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Record a user message
	userMsg := &storage.TranscriptMessage{
		UUID:    "msg-001",
		Role:    "user",
		Content: "Hello, how can you help me?",
		CWD:     "/Users/username/project",
	}
	if err := store.RecordMessage(userMsg); err != nil {
		log.Fatal(err)
	}

	// Record an assistant message
	assistantMsg := &storage.TranscriptMessage{
		UUID:       "msg-002",
		ParentUUID: "msg-001",
		Role:       "assistant",
		Content:    "I can help you with coding tasks!",
	}
	if err := store.RecordMessage(assistantMsg); err != nil {
		log.Fatal(err)
	}

	// Set session metadata
	store.SetCustomTitle("My First Session")
	store.SetTag("tutorial")
	store.SetAgentName("CodeHelper")

	// Flush to ensure all writes complete
	if err := store.Flush(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Session saved successfully")
	// Output: Session saved successfully
}

// Example demonstrates loading and resuming a session
func Example_loadSession() {
	tmpDir := "/tmp/claude-example"
	homeDir := filepath.Join(tmpDir, ".claude-codex")
	projectDir := filepath.Join(tmpDir, "workspace", "my-project")
	sessionID := "session-123"

	// Create storage instance
	store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Load existing transcript
	entries, err := store.LoadTranscript()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Loaded %d entries\n", len(entries))

	// Get metadata
	meta := store.GetMetadata()
	if meta.CustomTitle != "" {
		fmt.Printf("Session title: %s\n", meta.CustomTitle)
	}

	// Continue the conversation
	newMsg := &storage.TranscriptMessage{
		UUID:    "msg-003",
		Role:    "user",
		Content: "Can you help me debug this code?",
	}
	store.RecordMessage(newMsg)
}

// Example demonstrates snapshot management
func Example_snapshots() {
	tmpDir := "/tmp/claude-example"
	homeDir := filepath.Join(tmpDir, ".claude-codex")
	projectDir := filepath.Join(tmpDir, "workspace", "my-project")
	sessionID := "session-123"

	// Create session storage
	store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Record some messages
	for i := 0; i < 5; i++ {
		msg := &storage.TranscriptMessage{
			UUID:    fmt.Sprintf("msg-%03d", i),
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		}
		store.RecordMessage(msg)
	}
	store.Flush()

	// Create a snapshot
	snapshotMgr := storage.NewSnapshotManager(homeDir)
	entries, _ := store.LoadTranscript()
	snapshot, err := snapshotMgr.CreateSnapshot(sessionID, entries)
	if err != nil {
		log.Fatal(err)
	}

	// Save the snapshot
	path, err := snapshotMgr.SaveSnapshot(snapshot)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Snapshot saved to: %s\n", path)

	// List all snapshots for this session
	snapshots, err := snapshotMgr.ListSnapshots(sessionID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d snapshots\n", len(snapshots))

	// Get the latest snapshot
	latest, err := snapshotMgr.GetLatestSnapshot(sessionID)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Latest snapshot has %d messages\n", latest.MessageCount)
}

// Example demonstrates concurrent writes
func Example_concurrentWrites() {
	tmpDir := "/tmp/claude-example"
	homeDir := filepath.Join(tmpDir, ".claude-codex")
	projectDir := filepath.Join(tmpDir, "workspace", "my-project")
	sessionID := "session-concurrent"

	store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Write from multiple goroutines (safe due to internal locking)
	done := make(chan bool)
	for i := 0; i < 3; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				msg := &storage.TranscriptMessage{
					UUID:    fmt.Sprintf("msg-%d-%d", id, j),
					Role:    "user",
					Content: fmt.Sprintf("Message from goroutine %d", id),
				}
				store.RecordMessage(msg)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Flush ensures all writes complete
	store.Flush()

	fmt.Println("Concurrent writes completed")
	// Output: Concurrent writes completed
}

// Example demonstrates working with different entry types
func Example_entryTypes() {
	tmpDir := "/tmp/claude-example"
	homeDir := filepath.Join(tmpDir, ".claude-codex")
	projectDir := filepath.Join(tmpDir, "workspace", "my-project")
	sessionID := "session-types"

	store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// User message
	store.RecordMessage(&storage.TranscriptMessage{
		UUID:    "msg-001",
		Role:    "user",
		Content: "Hello",
	})

	// Assistant message with tool calls
	store.RecordMessage(&storage.TranscriptMessage{
		UUID:    "msg-002",
		Role:    "assistant",
		Content: "I'll help you with that.",
		ToolCalls: []storage.ToolCall{
			{
				ID:    "call-001",
				Name:  "read_file",
				Input: []byte(`{"path": "/path/to/file"}`),
			},
		},
	})

	// Tool result
	store.RecordMessage(&storage.TranscriptMessage{
		UUID:       "msg-003",
		Role:       "tool",
		ToolCallID: "call-001",
		ToolName:   "read_file",
		ToolOutput: "File contents here...",
	})

	// Set metadata
	store.SetCustomTitle("Example Session")
	store.SetTag("example")
	store.SetAgentColor("#FF5733")

	// Link to a PR
	store.LinkPR(42, "https://github.com/owner/repo/pull/42", "owner/repo")

	// Set worktree state
	store.SetWorktreeState(&storage.WorktreeSession{
		OriginalCWD:    "/Users/username/project",
		WorktreePath:   "/Users/username/project/.worktrees/feature-branch",
		WorktreeBranch: "feature-branch",
	})

	store.Flush()

	fmt.Println("Various entry types recorded")
	// Output: Various entry types recorded
}

// Example demonstrates reading transcript tail for metadata
func Example_readTail() {
	tmpDir := "/tmp/claude-example"
	homeDir := filepath.Join(tmpDir, ".claude-codex")
	projectDir := filepath.Join(tmpDir, "workspace", "my-project")
	sessionID := "session-123"

	store, err := storage.NewSessionStorage(homeDir, sessionID, projectDir)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	transcriptPath := store.GetTranscriptPath()
	reader := storage.NewTranscriptReader(transcriptPath)

	// Read last 64KB for quick metadata access
	tail, err := reader.ReadTail(storage.LiteReadBufSize)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Read %d bytes from tail\n", len(tail))
}

// Example demonstrates session listing and management
func Example_sessionManagement() {
	homeDir := "/tmp/claude-example/.claude-codex"
	projectDir := "/tmp/claude-example/workspace/my-project"

	// List all sessions
	sessions, err := storage.ListSessions(homeDir, projectDir)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d sessions\n", len(sessions))

	// Check if a specific session exists
	sessionID := "session-123"
	if storage.SessionExists(homeDir, projectDir, sessionID) {
		fmt.Printf("Session %s exists\n", sessionID)
	}

	// Delete a session
	if err := storage.DeleteSession(homeDir, projectDir, sessionID); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Session deleted")
}

// Example demonstrates snapshot compaction
func Example_snapshotCompaction() {
	homeDir := "/tmp/claude-example/.claude-codex"
	sessionID := "session-123"

	snapshotMgr := storage.NewSnapshotManager(homeDir)

	// Load a snapshot
	snapshots, _ := snapshotMgr.ListSnapshots(sessionID)
	if len(snapshots) == 0 {
		return
	}

	snapshot, err := snapshotMgr.LoadSnapshot(snapshots[0])
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Original snapshot has %d messages\n", snapshot.MessageCount)

	// Compact to keep only last 50 messages
	compacted := snapshotMgr.CompactSnapshot(snapshot, 50)

	fmt.Printf("Compacted snapshot has %d messages\n", compacted.MessageCount)

	// Save the compacted version
	path, err := snapshotMgr.SaveSnapshot(compacted)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Compacted snapshot saved to: %s\n", path)
}

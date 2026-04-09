package memory

import (
	"context"
	"testing"
)

func TestStorage(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	t.Run("SaveAndLoadMemory", func(t *testing.T) {
		mem := &Memory{
			Name:        "Test Memory",
			Description: "A test memory entry",
			Type:        MemoryTypeUser,
			Content:     "This is the content of the memory.\n\nIt has multiple lines.",
			FilePath:    "test_memory.md",
		}

		// Save memory
		if err := storage.SaveMemory(mem); err != nil {
			t.Fatalf("Failed to save memory: %v", err)
		}

		// Load memory
		loaded, err := storage.LoadMemory("test_memory.md")
		if err != nil {
			t.Fatalf("Failed to load memory: %v", err)
		}

		// Verify fields
		if loaded.Name != mem.Name {
			t.Errorf("Name mismatch: got %q, want %q", loaded.Name, mem.Name)
		}
		if loaded.Description != mem.Description {
			t.Errorf("Description mismatch: got %q, want %q", loaded.Description, mem.Description)
		}
		if loaded.Type != mem.Type {
			t.Errorf("Type mismatch: got %q, want %q", loaded.Type, mem.Type)
		}
		if loaded.Content != mem.Content {
			t.Errorf("Content mismatch: got %q, want %q", loaded.Content, mem.Content)
		}
	})

	t.Run("SaveAndLoadIndex", func(t *testing.T) {
		index := &MemoryIndex{
			Entries: []MemoryIndexEntry{
				{
					Title:       "Memory 1",
					FilePath:    "memory1.md",
					Description: "First memory",
				},
				{
					Title:       "Memory 2",
					FilePath:    "memory2.md",
					Description: "Second memory",
				},
			},
		}

		// Save index
		if err := storage.SaveIndex(index); err != nil {
			t.Fatalf("Failed to save index: %v", err)
		}

		// Load index
		loaded, err := storage.LoadIndex()
		if err != nil {
			t.Fatalf("Failed to load index: %v", err)
		}

		// Verify entries
		if len(loaded.Entries) != len(index.Entries) {
			t.Fatalf("Entry count mismatch: got %d, want %d", len(loaded.Entries), len(index.Entries))
		}

		for i, entry := range loaded.Entries {
			expected := index.Entries[i]
			if entry.Title != expected.Title {
				t.Errorf("Entry %d title mismatch: got %q, want %q", i, entry.Title, expected.Title)
			}
			if entry.FilePath != expected.FilePath {
				t.Errorf("Entry %d path mismatch: got %q, want %q", i, entry.FilePath, expected.FilePath)
			}
			if entry.Description != expected.Description {
				t.Errorf("Entry %d description mismatch: got %q, want %q", i, entry.Description, expected.Description)
			}
		}
	})

	t.Run("ListMemories", func(t *testing.T) {
		// Create multiple memories
		memories := []*Memory{
			{
				Name:        "User Memory",
				Description: "User info",
				Type:        MemoryTypeUser,
				Content:     "User is a developer",
				FilePath:    "user.md",
			},
			{
				Name:        "Feedback Memory",
				Description: "User feedback",
				Type:        MemoryTypeFeedback,
				Content:     "User prefers concise responses",
				FilePath:    "feedback.md",
			},
		}

		for _, mem := range memories {
			if err := storage.SaveMemory(mem); err != nil {
				t.Fatalf("Failed to save memory: %v", err)
			}
		}

		// List all memories
		listed, err := storage.ListMemories()
		if err != nil {
			t.Fatalf("Failed to list memories: %v", err)
		}

		// Should have at least the 2 we just created (plus test_memory.md from previous test)
		if len(listed) < 2 {
			t.Errorf("Expected at least 2 memories, got %d", len(listed))
		}
	})

	t.Run("DeleteMemory", func(t *testing.T) {
		mem := &Memory{
			Name:        "Temp Memory",
			Description: "To be deleted",
			Type:        MemoryTypeProject,
			Content:     "This will be deleted",
			FilePath:    "temp.md",
		}

		// Save and then delete
		if err := storage.SaveMemory(mem); err != nil {
			t.Fatalf("Failed to save memory: %v", err)
		}

		if err := storage.DeleteMemory("temp.md"); err != nil {
			t.Fatalf("Failed to delete memory: %v", err)
		}

		// Verify it's gone
		_, err := storage.LoadMemory("temp.md")
		if err == nil {
			t.Error("Expected error loading deleted memory, got nil")
		}
	})
}

func TestExtractor(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultSessionMemoryConfig()
	extractor := NewExtractor(tmpDir, config)

	t.Run("ShouldExtract_NotInitialized", func(t *testing.T) {
		// Should not extract before initialization threshold (default: 10000)
		if extractor.ShouldExtract(9999, 5) {
			t.Error("Should not extract before initialization threshold")
		}

		// Should extract at/after initialization threshold
		if !extractor.ShouldExtract(10000, 5) {
			t.Error("Should extract after initialization threshold")
		}
	})

	t.Run("ShouldExtract_Initialized", func(t *testing.T) {
		// Mark as initialized
		extractor.MarkExtractionComplete(10000, "msg1")

		// Should not extract immediately after
		if extractor.ShouldExtract(11000, 1) {
			t.Error("Should not extract immediately after previous extraction")
		}

		// Should extract after thresholds met (default: +5000 tokens, 3 tool calls)
		if !extractor.ShouldExtract(21000, 5) {
			t.Error("Should extract after token and tool call thresholds met")
		}
	})

	t.Run("ExtractionInProgress", func(t *testing.T) {
		extractor.MarkExtractionStart()

		// Should not extract while in progress
		if extractor.ShouldExtract(50000, 10) {
			t.Error("Should not extract while extraction in progress")
		}

		extractor.MarkExtractionComplete(50000, "msg2")

		// Should be able to extract after completion
		if !extractor.ShouldExtract(61000, 5) {
			t.Error("Should extract after previous extraction completed")
		}
	})
}

func TestManager(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultSessionMemoryConfig()
	manager := NewManager(tmpDir, config)

	t.Run("SaveAndLoadMemory", func(t *testing.T) {
		mem := &Memory{
			Name:        "Test Memory",
			Description: "A test",
			Type:        MemoryTypeUser,
			Content:     "Test content",
			FilePath:    "test.md",
		}

		// Save memory
		if err := manager.SaveMemory(mem); err != nil {
			t.Fatalf("Failed to save memory: %v", err)
		}

		// Verify index was updated
		index, err := manager.GetIndex()
		if err != nil {
			t.Fatalf("Failed to get index: %v", err)
		}

		found := false
		for _, entry := range index.Entries {
			if entry.FilePath == "test.md" {
				found = true
				if entry.Title != mem.Name {
					t.Errorf("Index title mismatch: got %q, want %q", entry.Title, mem.Name)
				}
				break
			}
		}
		if !found {
			t.Error("Memory not found in index")
		}

		// Load memory
		loaded, err := manager.LoadMemory("test.md")
		if err != nil {
			t.Fatalf("Failed to load memory: %v", err)
		}
		if loaded.Name != mem.Name {
			t.Errorf("Name mismatch: got %q, want %q", loaded.Name, mem.Name)
		}
	})

	t.Run("SearchMemories", func(t *testing.T) {
		// Create searchable memories
		memories := []*Memory{
			{
				Name:        "Go Programming",
				Description: "Go language tips",
				Type:        MemoryTypeUser,
				Content:     "User loves Go programming",
				FilePath:    "go.md",
			},
			{
				Name:        "Python Tips",
				Description: "Python best practices",
				Type:        MemoryTypeUser,
				Content:     "User also uses Python",
				FilePath:    "python.md",
			},
		}

		for _, mem := range memories {
			if err := manager.SaveMemory(mem); err != nil {
				t.Fatalf("Failed to save memory: %v", err)
			}
		}

		// Search for "Go"
		results, err := manager.SearchMemories("Go")
		if err != nil {
			t.Fatalf("Failed to search: %v", err)
		}

		if len(results) == 0 {
			t.Error("Expected to find memories with 'Go', got none")
		}
	})

	t.Run("FilterByType", func(t *testing.T) {
		// Create memory of different type
		mem := &Memory{
			Name:        "Project Info",
			Description: "Project details",
			Type:        MemoryTypeProject,
			Content:     "Project uses microservices",
			FilePath:    "project.md",
		}

		if err := manager.SaveMemory(mem); err != nil {
			t.Fatalf("Failed to save memory: %v", err)
		}

		// Filter by project type
		results, err := manager.FilterByType(MemoryTypeProject)
		if err != nil {
			t.Fatalf("Failed to filter: %v", err)
		}

		found := false
		for _, m := range results {
			if m.FilePath == "project.md" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find project memory")
		}
	})

	t.Run("DeleteMemory", func(t *testing.T) {
		mem := &Memory{
			Name:        "To Delete",
			Description: "Will be deleted",
			Type:        MemoryTypeReference,
			Content:     "Delete me",
			FilePath:    "delete.md",
		}

		// Save
		if err := manager.SaveMemory(mem); err != nil {
			t.Fatalf("Failed to save memory: %v", err)
		}

		// Delete
		if err := manager.DeleteMemory("delete.md"); err != nil {
			t.Fatalf("Failed to delete memory: %v", err)
		}

		// Verify it's gone from index
		index, err := manager.GetIndex()
		if err != nil {
			t.Fatalf("Failed to get index: %v", err)
		}

		for _, entry := range index.Entries {
			if entry.FilePath == "delete.md" {
				t.Error("Deleted memory still in index")
			}
		}
	})
}

func TestExtractionIntegration(t *testing.T) {
	t.Run("SetAgentManager", func(t *testing.T) {
		tmpDir := t.TempDir()
		extractor := NewExtractor(tmpDir, DefaultSessionMemoryConfig())

		// Should start with nil agent manager
		if extractor.agentManager != nil {
			t.Error("Expected nil agent manager initially")
		}

		// Set agent manager (using nil for test)
		extractor.SetAgentManager(nil)

		// Verify it was set
		extractor.mu.RLock()
		manager := extractor.agentManager
		extractor.mu.RUnlock()

		if manager != nil {
			t.Error("Expected nil after setting nil manager")
		}
	})

	t.Run("ExtractMemories_NoAgentManager", func(t *testing.T) {
		tmpDir := t.TempDir()
		extractor := NewExtractor(tmpDir, DefaultSessionMemoryConfig())

		result, err := extractor.ExtractMemories(context.Background(), "test conversation")
		if err == nil {
			t.Error("Expected error when agent manager not set")
		}
		if result.Success {
			t.Error("Expected failure when agent manager not set")
		}
	})
}

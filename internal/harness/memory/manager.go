package memory

import (
	"fmt"
	"strings"
)

// Manager provides high-level memory operations
type Manager struct {
	storage   *Storage
	extractor *Extractor
}

// NewManager creates a new memory manager
func NewManager(memoryDir string, config SessionMemoryConfig) *Manager {
	return &Manager{
		storage:   NewStorage(memoryDir),
		extractor: NewExtractor(memoryDir, config),
	}
}

// SaveMemory saves a memory and updates the index
func (m *Manager) SaveMemory(mem *Memory) error {
	// Save memory file
	if err := m.storage.SaveMemory(mem); err != nil {
		return err
	}

	// Update index
	return m.updateIndex(mem)
}

// updateIndex adds or updates a memory in the index
func (m *Manager) updateIndex(mem *Memory) error {
	index, err := m.storage.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	// Check if entry already exists
	found := false
	for i, entry := range index.Entries {
		if entry.FilePath == mem.FilePath {
			// Update existing entry
			index.Entries[i] = MemoryIndexEntry{
				Title:       mem.Name,
				FilePath:    mem.FilePath,
				Description: mem.Description,
			}
			found = true
			break
		}
	}

	// Add new entry if not found
	if !found {
		index.Entries = append(index.Entries, MemoryIndexEntry{
			Title:       mem.Name,
			FilePath:    mem.FilePath,
			Description: mem.Description,
		})
	}

	return m.storage.SaveIndex(index)
}

// LoadMemory loads a memory by file path
func (m *Manager) LoadMemory(filePath string) (*Memory, error) {
	return m.storage.LoadMemory(filePath)
}

// ListMemories returns all memories
func (m *Manager) ListMemories() ([]*Memory, error) {
	return m.storage.ListMemories()
}

// SearchMemories searches memories by keyword
func (m *Manager) SearchMemories(query string) ([]*Memory, error) {
	allMemories, err := m.storage.ListMemories()
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var results []*Memory

	for _, mem := range allMemories {
		// Search in name, description, and content
		if strings.Contains(strings.ToLower(mem.Name), query) ||
			strings.Contains(strings.ToLower(mem.Description), query) ||
			strings.Contains(strings.ToLower(mem.Content), query) {
			results = append(results, mem)
		}
	}

	return results, nil
}

// FilterByType returns memories of a specific type
func (m *Manager) FilterByType(memType MemoryType) ([]*Memory, error) {
	allMemories, err := m.storage.ListMemories()
	if err != nil {
		return nil, err
	}

	var results []*Memory
	for _, mem := range allMemories {
		if mem.Type == memType {
			results = append(results, mem)
		}
	}

	return results, nil
}

// DeleteMemory removes a memory and updates the index
func (m *Manager) DeleteMemory(filePath string) error {
	// Delete file
	if err := m.storage.DeleteMemory(filePath); err != nil {
		return err
	}

	// Update index
	index, err := m.storage.LoadIndex()
	if err != nil {
		return fmt.Errorf("failed to load index: %w", err)
	}

	// Remove from index
	newEntries := make([]MemoryIndexEntry, 0, len(index.Entries))
	for _, entry := range index.Entries {
		if entry.FilePath != filePath {
			newEntries = append(newEntries, entry)
		}
	}
	index.Entries = newEntries

	return m.storage.SaveIndex(index)
}

// GetExtractor returns the extractor instance
func (m *Manager) GetExtractor() *Extractor {
	return m.extractor
}

// GetIndex returns the current memory index
func (m *Manager) GetIndex() (*MemoryIndex, error) {
	return m.storage.LoadIndex()
}

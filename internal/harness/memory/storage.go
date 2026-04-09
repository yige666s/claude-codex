package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Storage handles reading and writing memory files
type Storage struct {
	memoryDir string
}

// NewStorage creates a new storage instance
func NewStorage(memoryDir string) *Storage {
	return &Storage{
		memoryDir: memoryDir,
	}
}

// EnsureMemoryDir creates the memory directory if it doesn't exist
func (s *Storage) EnsureMemoryDir() error {
	return os.MkdirAll(s.memoryDir, 0755)
}

// SaveMemory writes a memory to disk as a markdown file with frontmatter
func (s *Storage) SaveMemory(mem *Memory) error {
	if err := s.EnsureMemoryDir(); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}

	// Update timestamp
	mem.UpdatedAt = time.Now()
	if mem.CreatedAt.IsZero() {
		mem.CreatedAt = mem.UpdatedAt
	}

	// Build file path
	filePath := filepath.Join(s.memoryDir, mem.FilePath)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create memory subdirectory: %w", err)
	}

	// Build frontmatter
	frontmatter := map[string]interface{}{
		"name":        mem.Name,
		"description": mem.Description,
		"type":        string(mem.Type),
	}

	frontmatterBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	// Build complete file content
	content := fmt.Sprintf("---\n%s---\n\n%s", string(frontmatterBytes), mem.Content)

	// Write to file
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write memory file: %w", err)
	}

	return nil
}

// LoadMemory reads a memory from disk
func (s *Storage) LoadMemory(relPath string) (*Memory, error) {
	filePath := filepath.Join(s.memoryDir, relPath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory file: %w", err)
	}

	return s.parseMemoryFile(relPath, data)
}

// parseMemoryFile parses a memory file with frontmatter
func (s *Storage) parseMemoryFile(relPath string, data []byte) (*Memory, error) {
	content := string(data)

	// Check for frontmatter
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("memory file missing frontmatter")
	}

	// Find end of frontmatter
	endIdx := strings.Index(content[4:], "\n---\n")
	if endIdx == -1 {
		return nil, fmt.Errorf("memory file has malformed frontmatter")
	}
	endIdx += 4

	// Extract frontmatter and content
	frontmatterStr := content[4:endIdx]
	bodyContent := strings.TrimSpace(content[endIdx+5:])

	// Parse frontmatter
	var frontmatter struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Type        string `yaml:"type"`
	}

	if err := yaml.Unmarshal([]byte(frontmatterStr), &frontmatter); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Get file timestamps
	fileInfo, err := os.Stat(filepath.Join(s.memoryDir, relPath))
	if err != nil {
		return nil, fmt.Errorf("failed to stat memory file: %w", err)
	}

	return &Memory{
		Name:        frontmatter.Name,
		Description: frontmatter.Description,
		Type:        MemoryType(frontmatter.Type),
		Content:     bodyContent,
		FilePath:    relPath,
		CreatedAt:   fileInfo.ModTime(), // Approximation
		UpdatedAt:   fileInfo.ModTime(),
	}, nil
}

// LoadIndex reads the MEMORY.md index file
func (s *Storage) LoadIndex() (*MemoryIndex, error) {
	indexPath := filepath.Join(s.memoryDir, "MEMORY.md")

	data, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		// Index doesn't exist yet, return empty index
		return &MemoryIndex{Entries: []MemoryIndexEntry{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	return s.parseIndexFile(data)
}

// parseIndexFile parses MEMORY.md content
func (s *Storage) parseIndexFile(data []byte) (*MemoryIndex, error) {
	content := string(data)
	lines := strings.Split(content, "\n")

	var entries []MemoryIndexEntry

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "- [") {
			continue
		}

		// Parse format: - [Title](file.md) — description
		entry, err := s.parseIndexLine(line)
		if err != nil {
			// Skip malformed lines
			continue
		}
		entries = append(entries, entry)
	}

	return &MemoryIndex{Entries: entries}, nil
}

// parseIndexLine parses a single index line
func (s *Storage) parseIndexLine(line string) (MemoryIndexEntry, error) {
	// Format: - [Title](file.md) — description
	line = strings.TrimPrefix(line, "- ")

	// Extract title
	titleStart := strings.Index(line, "[")
	titleEnd := strings.Index(line, "]")
	if titleStart == -1 || titleEnd == -1 {
		return MemoryIndexEntry{}, fmt.Errorf("malformed title")
	}
	title := line[titleStart+1 : titleEnd]

	// Extract file path
	pathStart := strings.Index(line, "(")
	pathEnd := strings.Index(line, ")")
	if pathStart == -1 || pathEnd == -1 {
		return MemoryIndexEntry{}, fmt.Errorf("malformed path")
	}
	filePath := line[pathStart+1 : pathEnd]

	// Extract description (after —)
	description := ""
	if dashIdx := strings.Index(line, "—"); dashIdx != -1 {
		description = strings.TrimSpace(line[dashIdx+len("—"):])
	}

	return MemoryIndexEntry{
		Title:       title,
		FilePath:    filePath,
		Description: description,
	}, nil
}

// SaveIndex writes the MEMORY.md index file
func (s *Storage) SaveIndex(index *MemoryIndex) error {
	if err := s.EnsureMemoryDir(); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}

	var lines []string
	for _, entry := range index.Entries {
		line := fmt.Sprintf("- [%s](%s)", entry.Title, entry.FilePath)
		if entry.Description != "" {
			line += " — " + entry.Description
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n") + "\n"
	indexPath := filepath.Join(s.memoryDir, "MEMORY.md")

	if err := os.WriteFile(indexPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

// ListMemories returns all memory files in the directory
func (s *Storage) ListMemories() ([]*Memory, error) {
	var memories []*Memory

	err := filepath.Walk(s.memoryDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non-markdown files
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Skip MEMORY.md index file
		if filepath.Base(path) == "MEMORY.md" {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(s.memoryDir, path)
		if err != nil {
			return err
		}

		// Load memory
		mem, err := s.LoadMemory(relPath)
		if err != nil {
			// Skip files that can't be parsed
			return nil
		}

		memories = append(memories, mem)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list memories: %w", err)
	}

	return memories, nil
}

// DeleteMemory removes a memory file
func (s *Storage) DeleteMemory(relPath string) error {
	filePath := filepath.Join(s.memoryDir, relPath)
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete memory file: %w", err)
	}
	return nil
}

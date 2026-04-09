package memdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	MaxMemoryFiles       = 200
	FrontmatterMaxLines  = 30
)

// MemoryHeader represents metadata from a memory file's frontmatter
type MemoryHeader struct {
	Filename    string
	FilePath    string
	MtimeMs     int64
	Description string
	Type        string
}

// Frontmatter represents the YAML frontmatter in a memory file
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Type        string `yaml:"type"`
}

// ScanMemoryFiles scans a memory directory for .md files and reads their frontmatter
// Returns headers sorted newest-first (capped at MAX_MEMORY_FILES)
func ScanMemoryFiles(memoryDir string, ctx context.Context) ([]MemoryHeader, error) {
	var headers []MemoryHeader

	err := filepath.WalkDir(memoryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Only process .md files, skip MEMORY.md
		if !d.IsDir() && strings.HasSuffix(path, ".md") && filepath.Base(path) != AutoMemEntrypointName {
			header, err := readMemoryHeader(path, memoryDir)
			if err == nil {
				headers = append(headers, header)
			}
		}

		return nil
	})

	if err != nil && err != context.Canceled {
		return nil, err
	}

	// Sort by mtime (newest first)
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].MtimeMs > headers[j].MtimeMs
	})

	// Cap at MaxMemoryFiles
	if len(headers) > MaxMemoryFiles {
		headers = headers[:MaxMemoryFiles]
	}

	return headers, nil
}

// readMemoryHeader reads the frontmatter from a memory file
func readMemoryHeader(filePath, memoryDir string) (MemoryHeader, error) {
	// Get file info for mtime
	info, err := os.Stat(filePath)
	if err != nil {
		return MemoryHeader{}, err
	}

	// Read first N lines for frontmatter
	content, err := readFileLines(filePath, FrontmatterMaxLines)
	if err != nil {
		return MemoryHeader{}, err
	}

	// Parse frontmatter
	frontmatter, err := parseFrontmatter(content)
	if err != nil {
		// If frontmatter parsing fails, still return basic info
		frontmatter = Frontmatter{}
	}

	// Get relative path from memory dir
	relPath, err := filepath.Rel(memoryDir, filePath)
	if err != nil {
		relPath = filepath.Base(filePath)
	}

	return MemoryHeader{
		Filename:    relPath,
		FilePath:    filePath,
		MtimeMs:     info.ModTime().UnixMilli(),
		Description: frontmatter.Description,
		Type:        frontmatter.Type,
	}, nil
}

// readFileLines reads the first N lines from a file
func readFileLines(filePath string, maxLines int) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	return strings.Join(lines, "\n"), nil
}

// parseFrontmatter extracts YAML frontmatter from content
func parseFrontmatter(content string) (Frontmatter, error) {
	var fm Frontmatter

	// Check for frontmatter delimiters
	if !strings.HasPrefix(content, "---\n") {
		return fm, fmt.Errorf("no frontmatter found")
	}

	// Find end delimiter
	endIdx := strings.Index(content[4:], "\n---")
	if endIdx == -1 {
		return fm, fmt.Errorf("frontmatter not closed")
	}

	// Extract frontmatter content
	fmContent := content[4 : 4+endIdx]

	// Parse YAML
	err := yaml.Unmarshal([]byte(fmContent), &fm)
	if err != nil {
		return fm, err
	}

	return fm, nil
}

// FormatMemoryManifest formats memory headers as a text manifest
// One line per file with [type] filename (timestamp): description
func FormatMemoryManifest(memories []MemoryHeader) string {
	var lines []string

	for _, m := range memories {
		tag := ""
		if m.Type != "" {
			tag = fmt.Sprintf("[%s] ", m.Type)
		}

		ts := time.UnixMilli(m.MtimeMs).Format(time.RFC3339)

		if m.Description != "" {
			lines = append(lines, fmt.Sprintf("- %s%s (%s): %s", tag, m.Filename, ts, m.Description))
		} else {
			lines = append(lines, fmt.Sprintf("- %s%s (%s)", tag, m.Filename, ts))
		}
	}

	return strings.Join(lines, "\n")
}

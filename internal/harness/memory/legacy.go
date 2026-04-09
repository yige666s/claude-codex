package memory

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LegacyManager provides the old simple line-based memory API for backward compatibility
type LegacyManager struct {
	home string
}

// NewLegacyManager creates a legacy memory manager
func NewLegacyManager(home string) *LegacyManager {
	return &LegacyManager{home: home}
}

func (m *LegacyManager) memDir() string {
	return filepath.Join(m.home, "memory")
}

func (m *LegacyManager) filePath(name string) string {
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	return filepath.Join(m.memDir(), name)
}

// List returns all memory files
func (m *LegacyManager) List() ([]string, error) {
	entries, err := os.ReadDir(m.memDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return files, nil
}

// Read returns the content of a memory file
func (m *LegacyManager) Read(name string) (string, error) {
	data, err := os.ReadFile(m.filePath(name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Append adds a line to a memory file
func (m *LegacyManager) Append(name, content string) error {
	path := m.filePath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "- %s\n", content)
	return err
}

// Delete removes a specific line from a memory file (1-indexed)
func (m *LegacyManager) Delete(name string, lineNum int) error {
	path := m.filePath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return fmt.Errorf("line %d out of range (1-%d)", lineNum, len(lines))
	}

	lines = append(lines[:lineNum-1], lines[lineNum:]...)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// Edit replaces a specific line in a memory file (1-indexed)
func (m *LegacyManager) Edit(name string, lineNum int, newContent string) error {
	path := m.filePath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	if lineNum < 1 || lineNum > len(lines) {
		return fmt.Errorf("line %d out of range (1-%d)", lineNum, len(lines))
	}

	lines[lineNum-1] = "- " + newContent
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// Search searches across all memory files for a query
func (m *LegacyManager) Search(query string) (map[string][]int, error) {
	files, err := m.List()
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	results := make(map[string][]int)

	for _, name := range files {
		path := m.filePath(name)
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			if strings.Contains(strings.ToLower(scanner.Text()), query) {
				results[name] = append(results[name], lineNum)
			}
		}
		f.Close()
	}

	return results, nil
}

// DeleteFile removes an entire memory file
func (m *LegacyManager) DeleteFile(name string) error {
	return os.Remove(m.filePath(name))
}

// Size returns the total size of all memory files in bytes
func (m *LegacyManager) Size() (int64, error) {
	files, err := m.List()
	if err != nil {
		return 0, err
	}

	var total int64
	for _, name := range files {
		info, err := os.Stat(m.filePath(name))
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}

package migrations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileVersionManager manages migration version using a file
type FileVersionManager struct {
	filePath string
	mu       sync.RWMutex
}

// NewFileVersionManager creates a new file-based version manager
func NewFileVersionManager(configDir string) *FileVersionManager {
	return &FileVersionManager{
		filePath: filepath.Join(configDir, "migration_version.json"),
	}
}

// versionData represents the version file structure
type versionData struct {
	Version int `json:"version"`
}

// GetCurrentVersion returns the current migration version
func (m *FileVersionManager) GetCurrentVersion(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if file exists
	if _, err := os.Stat(m.filePath); os.IsNotExist(err) {
		return 0, nil // No migrations run yet
	}

	// Read file
	data, err := os.ReadFile(m.filePath)
	if err != nil {
		return 0, fmt.Errorf("failed to read version file: %w", err)
	}

	// Parse JSON
	var vd versionData
	if err := json.Unmarshal(data, &vd); err != nil {
		return 0, fmt.Errorf("failed to parse version file: %w", err)
	}

	return vd.Version, nil
}

// SetCurrentVersion sets the current migration version
func (m *FileVersionManager) SetCurrentVersion(ctx context.Context, version int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create version data
	vd := versionData{Version: version}
	data, err := json.MarshalIndent(vd, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal version data: %w", err)
	}

	// Write file
	if err := os.WriteFile(m.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write version file: %w", err)
	}

	return nil
}

// NoOpAnalyticsLogger is a no-op analytics logger for testing
type NoOpAnalyticsLogger struct{}

func (l *NoOpAnalyticsLogger) LogEvent(ctx context.Context, event string, metadata map[string]interface{}) {
	// No-op
}

func (l *NoOpAnalyticsLogger) LogError(ctx context.Context, err error) {
	// No-op
}

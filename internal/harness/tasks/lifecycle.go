package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// RuntimeSnapshotPath returns the process-local task snapshot path for a
// workspace. The path is stable per working directory so CLI restarts can mark
// interrupted background agents as terminal instead of silently forgetting them.
func RuntimeSnapshotPath(home string, workingDir string) string {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		home = "."
	}
	abs := workingDir
	if abs == "" {
		abs, _ = os.Getwd()
	}
	if resolved, err := filepath.Abs(abs); err == nil {
		abs = resolved
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return filepath.Join(home, "task-snapshots", hex.EncodeToString(sum[:8])+".json")
}

// LoadSnapshotIfExists restores a task snapshot when present. Missing files are
// treated as an empty task set.
func (m *TaskManager) LoadSnapshotIfExists(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return m.LoadSnapshot(path)
}

// SaveRuntimeSnapshot persists the current runtime task state to the workspace
// snapshot file.
func (m *TaskManager) SaveRuntimeSnapshot(home string, workingDir string) error {
	return m.SaveSnapshot(RuntimeSnapshotPath(home, workingDir))
}

// LoadRuntimeSnapshot restores the workspace snapshot file if it exists.
func (m *TaskManager) LoadRuntimeSnapshot(home string, workingDir string) error {
	return m.LoadSnapshotIfExists(RuntimeSnapshotPath(home, workingDir))
}

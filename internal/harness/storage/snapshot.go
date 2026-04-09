package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Snapshot represents a point-in-time snapshot of a session
type Snapshot struct {
	SessionID   string                 `json:"session_id"`
	Timestamp   time.Time              `json:"timestamp"`
	MessageCount int                   `json:"message_count"`
	Metadata    *SessionMetadata       `json:"metadata"`
	Messages    []TranscriptMessage    `json:"messages,omitempty"`
	CustomData  map[string]interface{} `json:"custom_data,omitempty"`
}

// SnapshotManager manages session snapshots
type SnapshotManager struct {
	snapshotDir string
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(homeDir string) *SnapshotManager {
	return &SnapshotManager{
		snapshotDir: filepath.Join(homeDir, "snapshots"),
	}
}

// CreateSnapshot creates a snapshot of the current session state
func (sm *SnapshotManager) CreateSnapshot(sessionID string, entries []Entry) (*Snapshot, error) {
	snapshot := &Snapshot{
		SessionID:   sessionID,
		Timestamp:   time.Now().UTC(),
		Metadata:    &SessionMetadata{},
		Messages:    make([]TranscriptMessage, 0),
		CustomData:  make(map[string]interface{}),
	}

	// Extract messages and metadata from entries
	for _, entry := range entries {
		switch e := entry.(type) {
		case *TranscriptMessage:
			snapshot.Messages = append(snapshot.Messages, *e)
			snapshot.MessageCount++

		case *MetadataEntry:
			if e.CustomTitle != "" {
				snapshot.Metadata.CustomTitle = e.CustomTitle
			}
			if e.AITitle != "" {
				snapshot.Metadata.AITitle = e.AITitle
			}
			if e.LastPrompt != "" {
				snapshot.Metadata.LastPrompt = e.LastPrompt
			}
			if e.Tag != "" {
				snapshot.Metadata.Tag = e.Tag
			}
			if e.AgentName != "" {
				snapshot.Metadata.AgentName = e.AgentName
			}
			if e.AgentColor != "" {
				snapshot.Metadata.AgentColor = e.AgentColor
			}
			if e.AgentSetting != "" {
				snapshot.Metadata.AgentSetting = e.AgentSetting
			}
			if e.Mode != "" {
				snapshot.Metadata.Mode = e.Mode
			}

		case *PRLinkEntry:
			snapshot.Metadata.PRNumber = e.PRNumber
			snapshot.Metadata.PRUrl = e.PRUrl
			snapshot.Metadata.PRRepository = e.PRRepository

		case *WorktreeStateEntry:
			snapshot.Metadata.WorktreeState = e.WorktreeSession
		}
	}

	return snapshot, nil
}

// SaveSnapshot saves a snapshot to disk
func (sm *SnapshotManager) SaveSnapshot(snapshot *Snapshot) (string, error) {
	// Ensure snapshot directory exists
	if err := os.MkdirAll(sm.snapshotDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	// Generate snapshot filename with timestamp
	filename := fmt.Sprintf("%s-%s.json",
		snapshot.SessionID,
		snapshot.Timestamp.Format("20060102-150405"))
	path := filepath.Join(sm.snapshotDir, filename)

	// Marshal snapshot to JSON
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("failed to write snapshot: %w", err)
	}

	return path, nil
}

// LoadSnapshot loads a snapshot from disk
func (sm *SnapshotManager) LoadSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snapshot, nil
}

// ListSnapshots lists all snapshots for a session
func (sm *SnapshotManager) ListSnapshots(sessionID string) ([]string, error) {
	entries, err := os.ReadDir(sm.snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var snapshots []string
	prefix := sessionID + "-"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > len(prefix) && name[:len(prefix)] == prefix && filepath.Ext(name) == ".json" {
			snapshots = append(snapshots, filepath.Join(sm.snapshotDir, name))
		}
	}

	return snapshots, nil
}

// DeleteSnapshot deletes a snapshot file
func (sm *SnapshotManager) DeleteSnapshot(path string) error {
	return os.Remove(path)
}

// DeleteAllSnapshots deletes all snapshots for a session
func (sm *SnapshotManager) DeleteAllSnapshots(sessionID string) error {
	snapshots, err := sm.ListSnapshots(sessionID)
	if err != nil {
		return err
	}

	for _, path := range snapshots {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

// GetLatestSnapshot returns the most recent snapshot for a session
func (sm *SnapshotManager) GetLatestSnapshot(sessionID string) (*Snapshot, error) {
	snapshots, err := sm.ListSnapshots(sessionID)
	if err != nil {
		return nil, err
	}

	if len(snapshots) == 0 {
		return nil, fmt.Errorf("no snapshots found for session %s", sessionID)
	}

	// Snapshots are named with timestamps, so the last one alphabetically is the latest
	latestPath := snapshots[len(snapshots)-1]
	return sm.LoadSnapshot(latestPath)
}

// RestoreFromSnapshot restores a session from a snapshot
func (sm *SnapshotManager) RestoreFromSnapshot(snapshot *Snapshot, storage *SessionStorage) error {
	// Record all messages from the snapshot
	for _, msg := range snapshot.Messages {
		msgCopy := msg
		if err := storage.RecordMessage(&msgCopy); err != nil {
			return fmt.Errorf("failed to restore message: %w", err)
		}
	}

	// Restore metadata
	meta := snapshot.Metadata
	if meta.CustomTitle != "" {
		if err := storage.SetCustomTitle(meta.CustomTitle); err != nil {
			return err
		}
	}
	if meta.Tag != "" {
		if err := storage.SetTag(meta.Tag); err != nil {
			return err
		}
	}
	if meta.AgentName != "" {
		if err := storage.SetAgentName(meta.AgentName); err != nil {
			return err
		}
	}
	if meta.AgentColor != "" {
		if err := storage.SetAgentColor(meta.AgentColor); err != nil {
			return err
		}
	}
	if meta.LastPrompt != "" {
		if err := storage.SetLastPrompt(meta.LastPrompt); err != nil {
			return err
		}
	}
	if meta.PRNumber > 0 {
		if err := storage.LinkPR(meta.PRNumber, meta.PRUrl, meta.PRRepository); err != nil {
			return err
		}
	}
	if meta.WorktreeState != nil {
		if err := storage.SetWorktreeState(meta.WorktreeState); err != nil {
			return err
		}
	}

	return storage.Flush()
}

// CompactSnapshot creates a compacted snapshot with only essential data
func (sm *SnapshotManager) CompactSnapshot(snapshot *Snapshot, keepLastN int) *Snapshot {
	compacted := &Snapshot{
		SessionID:   snapshot.SessionID,
		Timestamp:   time.Now().UTC(),
		Metadata:    snapshot.Metadata,
		CustomData:  snapshot.CustomData,
	}

	// Keep only the last N messages
	if len(snapshot.Messages) > keepLastN {
		compacted.Messages = snapshot.Messages[len(snapshot.Messages)-keepLastN:]
	} else {
		compacted.Messages = snapshot.Messages
	}
	compacted.MessageCount = len(compacted.Messages)

	return compacted
}

// SnapshotInfo provides summary information about a snapshot
type SnapshotInfo struct {
	Path         string    `json:"path"`
	SessionID    string    `json:"session_id"`
	Timestamp    time.Time `json:"timestamp"`
	MessageCount int       `json:"message_count"`
	FileSize     int64     `json:"file_size"`
	Title        string    `json:"title,omitempty"`
	Tag          string    `json:"tag,omitempty"`
}

// GetSnapshotInfo returns summary information about a snapshot without loading all data
func (sm *SnapshotManager) GetSnapshotInfo(path string) (*SnapshotInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	snapshot, err := sm.LoadSnapshot(path)
	if err != nil {
		return nil, err
	}

	info := &SnapshotInfo{
		Path:         path,
		SessionID:    snapshot.SessionID,
		Timestamp:    snapshot.Timestamp,
		MessageCount: snapshot.MessageCount,
		FileSize:     stat.Size(),
	}

	if snapshot.Metadata != nil {
		if snapshot.Metadata.CustomTitle != "" {
			info.Title = snapshot.Metadata.CustomTitle
		} else if snapshot.Metadata.AITitle != "" {
			info.Title = snapshot.Metadata.AITitle
		}
		info.Tag = snapshot.Metadata.Tag
	}

	return info, nil
}

// ListSnapshotInfos returns summary information for all snapshots of a session
func (sm *SnapshotManager) ListSnapshotInfos(sessionID string) ([]*SnapshotInfo, error) {
	paths, err := sm.ListSnapshots(sessionID)
	if err != nil {
		return nil, err
	}

	infos := make([]*SnapshotInfo, 0, len(paths))
	for _, path := range paths {
		info, err := sm.GetSnapshotInfo(path)
		if err != nil {
			// Skip snapshots that can't be read
			continue
		}
		infos = append(infos, info)
	}

	return infos, nil
}
